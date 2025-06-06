package nsq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"nsa/internal/config"
	"nsa/internal/logger"
	"nsa/internal/models"
	"nsa/internal/workflow"

	"github.com/nsqio/go-nsq"
)

// Manager NSQ管理器
type Manager struct {
	config    config.NSQConfig
	logger    logger.Logger
	consumers map[string]*Consumer
	mu        sync.RWMutex
	executor  *workflow.Executor
	ctx       context.Context
	cancel    context.CancelFunc
}

// Consumer NSQ消费者
type Consumer struct {
	consumer *nsq.Consumer
	topic    string
	channel  string
	handler  *MessageHandler
}

// MessageHandler 消息处理器
type MessageHandler struct {
	logger   logger.Logger
	executor *workflow.Executor
	topic    string
	channel  string
}

// NewManager 创建新的NSQ管理器
func NewManager(cfg config.NSQConfig, logger logger.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config:    cfg,
		logger:    logger,
		consumers: make(map[string]*Consumer),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// SetExecutor 设置工作流执行器
func (m *Manager) SetExecutor(executor *workflow.Executor) {
	m.executor = executor
}

// AddConsumer 添加消费者
func (m *Manager) AddConsumer(topic, channel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", topic, channel)
	if _, exists := m.consumers[key]; exists {
		return fmt.Errorf("consumer for topic %s channel %s already exists", topic, channel)
	}

	// 创建NSQ配置
	nsqConfig := nsq.NewConfig()
	nsqConfig.DefaultRequeueDelay = 0
	nsqConfig.MaxBackoffDuration = time.Minute
	nsqConfig.MaxInFlight = 1000
	nsqConfig.HeartbeatInterval = 30 * time.Second
	nsqConfig.ReadTimeout = 60 * time.Second
	nsqConfig.WriteTimeout = time.Second
	nsqConfig.MsgTimeout = 60 * time.Second

	// 创建消费者
	consumer, err := nsq.NewConsumer(topic, channel, nsqConfig)
	if err != nil {
		return fmt.Errorf("failed to create NSQ consumer: %v", err)
	}

	// 创建消息处理器
	handler := &MessageHandler{
		logger:   m.logger,
		executor: m.executor,
		topic:    topic,
		channel:  channel,
	}

	// 设置处理器
	consumer.AddHandler(handler)

	// 连接到NSQ
	if err := consumer.ConnectToNSQLookupds(m.config.LookupdAddresses); err != nil {
		consumer.Stop()
		return fmt.Errorf("failed to connect to NSQ lookupd: %v", err)
	}

	// 保存消费者
	m.consumers[key] = &Consumer{
		consumer: consumer,
		topic:    topic,
		channel:  channel,
		handler:  handler,
	}

	m.logger.Infof("NSQ consumer added for topic: %s, channel: %s", topic, channel)
	return nil
}

// RemoveConsumer 移除消费者
func (m *Manager) RemoveConsumer(topic, channel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", topic, channel)
	consumer, exists := m.consumers[key]
	if !exists {
		return fmt.Errorf("consumer for topic %s channel %s not found", topic, channel)
	}

	// 停止消费者
	consumer.consumer.Stop()
	<-consumer.consumer.StopChan

	// 删除消费者
	delete(m.consumers, key)

	m.logger.Infof("NSQ consumer removed for topic: %s, channel: %s", topic, channel)
	return nil
}

// ListConsumers 列出所有消费者
func (m *Manager) ListConsumers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var consumers []string
	for key := range m.consumers {
		consumers = append(consumers, key)
	}
	return consumers
}

// Stop 停止所有消费者
func (m *Manager) Stop() {
	m.logger.Info("Stopping NSQ manager...")

	// 取消上下文
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	// 停止所有消费者
	for key, consumer := range m.consumers {
		m.logger.Infof("Stopping consumer: %s", key)
		consumer.consumer.Stop()
		<-consumer.consumer.StopChan
	}

	// 清空消费者映射
	m.consumers = make(map[string]*Consumer)
	m.logger.Info("NSQ manager stopped")
}

// HandleMessage 实现nsq.Handler接口
func (h *MessageHandler) HandleMessage(message *nsq.Message) error {
	start := time.Now()
	h.logger.Infof("Received NSQ message from topic: %s, channel: %s, attempts: %d",
		h.topic, h.channel, message.Attempts)

	// 解析消息
	nsqMessage, err := h.parseMessage(message)
	if err != nil {
		h.logger.Errorf("Failed to parse NSQ message: %v", err)
		return err
	}

	// 获取工作流配置
	workflowConfig, err := h.executor.GetWorkflowConfig(h.topic, h.channel)
	if err != nil {
		h.logger.Errorf("Failed to get workflow config for topic %s channel %s: %v",
			h.topic, h.channel, err)
		return err
	}

	// 执行工作流
	ctx := context.Background()
	if err := h.executor.Execute(ctx, workflowConfig, nsqMessage); err != nil {
		h.logger.Errorf("Failed to execute workflow: %v", err)
		return err
	}

	duration := time.Since(start)
	h.logger.Infof("NSQ message processed successfully in %v", duration)

	return nil
}

// parseMessage 解析NSQ消息
func (h *MessageHandler) parseMessage(message *nsq.Message) (*models.NSQMessage, error) {
	nsqMessage := &models.NSQMessage{
		Topic:     h.topic,
		Channel:   h.channel,
		Body:      message.Body,
		Timestamp: time.Unix(0, message.Timestamp),
		Attempts:  message.Attempts,
		ID:        string(message.ID[:]),
		Data:      make(map[string]interface{}),
	}

	// 尝试解析JSON消息体
	if len(message.Body) > 0 {
		var data map[string]interface{}
		if err := json.Unmarshal(message.Body, &data); err != nil {
			// 如果不是JSON，将原始数据作为字符串存储
			nsqMessage.Data["raw"] = string(message.Body)
			h.logger.Warnf("Failed to parse message body as JSON, storing as raw string: %v", err)
		} else {
			nsqMessage.Data = data
		}
	}

	return nsqMessage, nil
}

// GetConsumerStats 获取消费者统计信息
func (m *Manager) GetConsumerStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	for key, consumer := range m.consumers {
		consumerStats := consumer.consumer.Stats()
		stats[key] = map[string]interface{}{
			"topic":             consumer.topic,
			"channel":           consumer.channel,
			"connections":       consumerStats.Connections,
			"messages_received": consumerStats.MessagesReceived,
			"messages_finished": consumerStats.MessagesFinished,
			"messages_requeued": consumerStats.MessagesRequeued,
		}
	}

	return stats
}

// ReloadConsumers 重新加载消费者（根据数据库配置）
func (m *Manager) ReloadConsumers(workflowConfigs []*models.WorkflowConfig) error {
	m.logger.Info("Reloading NSQ consumers...")

	// 获取当前需要的消费者
	requiredConsumers := make(map[string]bool)
	for _, config := range workflowConfigs {
		if config.Enabled {
			key := fmt.Sprintf("%s:%s", config.Topic, config.Channel)
			requiredConsumers[key] = true
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 移除不需要的消费者
	for key := range m.consumers {
		if !requiredConsumers[key] {
			consumer := m.consumers[key]
			consumer.consumer.Stop()
			<-consumer.consumer.StopChan
			delete(m.consumers, key)
			m.logger.Infof("Removed consumer: %s", key)
		}
	}

	// 添加新的消费者
	for _, config := range workflowConfigs {
		if config.Enabled {
			key := fmt.Sprintf("%s:%s", config.Topic, config.Channel)
			if _, exists := m.consumers[key]; !exists {
				// 临时解锁以调用AddConsumer
				m.mu.Unlock()
				if err := m.AddConsumer(config.Topic, config.Channel); err != nil {
					m.logger.Errorf("Failed to add consumer %s: %v", key, err)
				}
				m.mu.Lock()
			}
		}
	}

	m.logger.Infof("NSQ consumers reloaded, active consumers: %d", len(m.consumers))
	return nil
}
