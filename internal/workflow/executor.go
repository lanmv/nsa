package workflow

import (
	"context"
	"fmt"
	"nsa/internal/datasource"
	"nsa/internal/logger"
	"nsa/internal/models"
	"nsa/internal/mongodb"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Task 任务定义
type Task struct {
	ID         string                 `json:"id"`
	ActionName string                 `json:"action_name"`
	DependOn   []string               `json:"depend_on"`
	Params     map[string]interface{} `json:"params"`
	Timeout    time.Duration          `json:"timeout"`
	Retry      *RetryConfig           `json:"retry"`
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxTimes int           `json:"max_times"`
	Interval time.Duration `json:"interval"`
}

// WorkflowInstance 工作流实例
type WorkflowInstance struct {
	ID         string                 `json:"id"`
	WorkflowID string                 `json:"workflow_id"`
	Status     string                 `json:"status"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
	Vars       map[string]interface{} `json:"vars"`
	Results    map[string]interface{} `json:"results"`
}

// Executor 工作流执行器
type Executor struct {
	logger        logger.Logger
	dataSourceMgr *datasource.Manager
	mongoDB       *mongodb.Client
	actions       map[string]Action
}

// Action 动作接口
type Action interface {
	Name() string
	Run(ctx context.Context, taskCtx *TaskContext) error
}

// NewExecutor 创建新的工作流执行器
func NewExecutor(logger logger.Logger, mongoClient *mongodb.Client, dataSourceMgr *datasource.Manager) *Executor {
	executor := &Executor{
		logger:        logger,
		mongoDB:       mongoClient,
		dataSourceMgr: dataSourceMgr,
		actions:       make(map[string]Action),
	}

	// 注册默认动作
	executor.registerDefaultActions()

	return executor
}

// registerDefaultActions 注册默认动作
func (e *Executor) registerDefaultActions() {
	actionCtx := &ActionContext{
		Logger:         e.logger,
		DataSourceMgr:  e.dataSourceMgr,
		WorkflowVars:   make(map[string]interface{}),
		PreviousOutput: make(map[string]interface{}),
	}

	e.RegisterAction(NewHTTPClientAction(actionCtx))
	e.RegisterAction(NewDBClientAction(actionCtx))
	e.RegisterAction(NewJSFunctionAction(actionCtx))
}

// RegisterAction 注册动作
func (e *Executor) RegisterAction(action Action) {
	e.actions[action.Name()] = action
}

// Execute 执行工作流
func (e *Executor) Execute(ctx context.Context, workflowConfig *models.WorkflowConfig, nsqMessage *models.NSQMessage) error {
	e.logger.Infof("Starting workflow execution: %s", workflowConfig.ID)

	// 生成实例ID
	instanceID := primitive.NewObjectID().Hex()

	// 创建工作流实例
	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: workflowConfig.ID.Hex(),
		Status:     "running",
		StartTime:  time.Now(),
		Vars:       e.buildWorkflowVars(workflowConfig, nsqMessage),
		Results:    make(map[string]interface{}),
	}

	// 保存实例
	if err := e.saveWorkflowInstance(instance); err != nil {
		e.logger.Errorf("Failed to save workflow instance: %v", err)
		return err
	}

	// 构建任务列表
	tasks := e.buildTasks(workflowConfig)

	// 执行任务
	go e.executeTasks(ctx, instance, tasks, nsqMessage)

	return nil
}

// buildTasks 构建任务列表
func (e *Executor) buildTasks(workflowConfig *models.WorkflowConfig) []Task {
	var tasks []Task
	for _, taskConfig := range workflowConfig.DAG.Tasks {
		task := Task{
			ID:         taskConfig.ID,
			ActionName: taskConfig.ActionName,
			DependOn:   taskConfig.DependOn,
			Params:     taskConfig.Params,
		}

		// 添加重试配置
		if taskConfig.Retry.Enabled {
			task.Retry = &RetryConfig{
				MaxTimes: taskConfig.Retry.MaxTimes,
				Interval: time.Duration(taskConfig.Retry.Interval) * time.Second,
			}
		}

		// 添加超时配置
		if taskConfig.Timeout > 0 {
			task.Timeout = time.Duration(taskConfig.Timeout) * time.Second
		}

		tasks = append(tasks, task)
	}

	return tasks
}

// executeTasks 执行任务列表
func (e *Executor) executeTasks(ctx context.Context, instance *WorkflowInstance, tasks []Task, nsqMessage *models.NSQMessage) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Errorf("Workflow execution panic: %v", r)
			instance.Status = "failed"
			instance.EndTime = time.Now()
			e.saveWorkflowInstance(instance)
		}
	}()

	// 简单的顺序执行（可以后续扩展为支持依赖关系的并行执行）
	for _, task := range tasks {
		if err := e.executeTask(ctx, &task, instance, nsqMessage); err != nil {
			e.logger.Errorf("Task %s failed: %v", task.ID, err)
			instance.Status = "failed"
			instance.EndTime = time.Now()
			e.saveWorkflowInstance(instance)
			return
		}
	}

	// 所有任务执行成功
	instance.Status = "completed"
	instance.EndTime = time.Now()
	e.saveWorkflowInstance(instance)
	e.logger.Infof("Workflow %s completed successfully", instance.ID)
}

// executeTask 执行单个任务
func (e *Executor) executeTask(ctx context.Context, task *Task, instance *WorkflowInstance, nsqMessage *models.NSQMessage) error {
	e.logger.Infof("Executing task: %s", task.ID)

	// 获取动作
	action, exists := e.actions[task.ActionName]
	if !exists {
		return fmt.Errorf("action %s not found", task.ActionName)
	}

	// 创建任务上下文
	taskCtx := &TaskContext{
		params: task.Params,
	}

	// 执行任务
	var err error
	if task.Retry != nil {
		// 带重试的执行
		for i := 0; i <= task.Retry.MaxTimes; i++ {
			err = action.Run(ctx, taskCtx)
			if err == nil {
				break
			}
			if i < task.Retry.MaxTimes {
				e.logger.Warnf("Task %s failed, retrying in %v: %v", task.ID, task.Retry.Interval, err)
				time.Sleep(task.Retry.Interval)
			}
		}
	} else {
		// 普通执行
		err = action.Run(ctx, taskCtx)
	}

	if err != nil {
		return fmt.Errorf("task %s execution failed: %v", task.ID, err)
	}

	// 保存任务结果
	instance.Results[task.ID] = taskCtx.GetOutput()
	e.logger.Infof("Task %s completed successfully", task.ID)

	return nil
}

// buildWorkflowVars 构建工作流变量
func (e *Executor) buildWorkflowVars(workflowConfig *models.WorkflowConfig, nsqMessage *models.NSQMessage) map[string]interface{} {
	vars := make(map[string]interface{})

	// 添加NSQ消息变量
	if nsqMessage != nil {
		vars["nsq_message"] = nsqMessage
	}

	// 添加工作流配置变量
	for _, varConfig := range workflowConfig.DAG.Vars {
		vars[varConfig.Name] = varConfig.DefaultValue
	}

	return vars
}

// saveWorkflowInstance 保存工作流实例
func (e *Executor) saveWorkflowInstance(instance *WorkflowInstance) error {
	collection := e.mongoDB.GetDatabase().Collection("workflow_instances")

	// 尝试更新，如果不存在则插入
	filter := bson.M{"id": instance.ID}

	_, err := collection.ReplaceOne(context.Background(), filter, instance)
	if err != nil {
		// 如果更新失败，尝试插入
		_, err = collection.InsertOne(context.Background(), instance)
	}

	return err
}

// saveExecutionLog 保存执行日志
func (e *Executor) saveExecutionLog(log *models.ExecutionLog) {
	collection := e.mongoDB.GetDatabase().Collection("execution_logs")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := collection.InsertOne(ctx, log)
	if err != nil {
		e.logger.Errorf("Failed to save execution log: %v", err)
	}
}

// GetWorkflowConfig 获取工作流配置
func (e *Executor) GetWorkflowConfig(topic, channel string) (*models.WorkflowConfig, error) {
	collection := e.mongoDB.GetCollection()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"topic":   topic,
		"channel": channel,
		"enabled": true,
	}

	var config models.WorkflowConfig
	err := collection.FindOne(ctx, filter).Decode(&config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// Stop 停止执行器
func (e *Executor) Stop() {
	e.logger.Info("Stopping workflow executor...")
	// 这里可以添加清理逻辑
}
