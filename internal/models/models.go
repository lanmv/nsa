package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WorkflowConfig 工作流配置
type WorkflowConfig struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Description string             `bson:"description" json:"description"`
	Topic       string             `bson:"topic" json:"topic"`
	Channel     string             `bson:"channel" json:"channel"`
	Enabled     bool               `bson:"enabled" json:"enabled"`
	DAG         DAGConfig          `bson:"dag" json:"dag"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
}

// DAGConfig DAG配置
type DAGConfig struct {
	ID    string       `bson:"id" json:"id"`
	Name  string       `bson:"name" json:"name"`
	Vars  []DAGVar     `bson:"vars" json:"vars"`
	Tasks []TaskConfig `bson:"tasks" json:"tasks"`
}

// DAGVar DAG变量
type DAGVar struct {
	Name         string      `bson:"name" json:"name"`
	Description  string      `bson:"description" json:"description"`
	DefaultValue interface{} `bson:"default_value" json:"default_value"`
	Type         string      `bson:"type" json:"type"`
}

// TaskConfig 任务配置
type TaskConfig struct {
	ID         string                 `bson:"id" json:"id"`
	Name       string                 `bson:"name" json:"name"`
	ActionName string                 `bson:"action_name" json:"action_name"`
	DependOn   []string               `bson:"depend_on" json:"depend_on"`
	Params     map[string]interface{} `bson:"params" json:"params"`
	Retry      RetryConfig            `bson:"retry" json:"retry"`
	Timeout    int                    `bson:"timeout" json:"timeout"` // 超时时间(秒)
}

// RetryConfig 重试配置
type RetryConfig struct {
	Enabled  bool `bson:"enabled" json:"enabled"`
	MaxTimes int  `bson:"max_times" json:"max_times"`
	Interval int  `bson:"interval" json:"interval"` // 重试间隔(秒)
}

// DataSource 数据源配置
type DataSource struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Type        string             `bson:"type" json:"type"` // mysql, postgresql, sqlserver, oracle, mongodb
	Host        string             `bson:"host" json:"host"`
	Port        int                `bson:"port" json:"port"`
	Database    string             `bson:"database" json:"database"`
	Username    string             `bson:"username" json:"username"`
	Password    string             `bson:"password" json:"password"`
	SSL         bool               `bson:"ssl" json:"ssl"`
	MaxIdle     int                `bson:"max_idle" json:"max_idle"`
	MaxOpen     int                `bson:"max_open" json:"max_open"`
	MaxLifetime int                `bson:"max_lifetime" json:"max_lifetime"` // 连接最大生存时间(秒)
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
}

// ExecutionLog 执行日志
type ExecutionLog struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	WorkflowID primitive.ObjectID `bson:"workflow_id" json:"workflow_id"`
	InstanceID string             `bson:"instance_id" json:"instance_id"`
	TaskID     string             `bson:"task_id" json:"task_id"`
	Status     string             `bson:"status" json:"status"` // pending, running, success, failed, skipped
	Message    string             `bson:"message" json:"message"`
	Input      interface{}        `bson:"input" json:"input"`
	Output     interface{}        `bson:"output" json:"output"`
	Error      string             `bson:"error" json:"error"`
	StartTime  time.Time          `bson:"start_time" json:"start_time"`
	EndTime    time.Time          `bson:"end_time" json:"end_time"`
	Duration   int64              `bson:"duration" json:"duration"` // 执行时间(毫秒)
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
}

// NSQMessage NSQ消息结构
type NSQMessage struct {
	Topic     string                 `json:"topic"`
	Channel   string                 `json:"channel"`
	Body      []byte                 `json:"body"`
	Timestamp time.Time              `json:"timestamp"`
	Attempts  uint16                 `json:"attempts"`
	ID        string                 `json:"id"`
	Data      map[string]interface{} `json:"data"` // 解析后的消息数据
}

// WorkflowInstance 工作流实例
type WorkflowInstance struct {
	ID         string                 `json:"id"`
	WorkflowID primitive.ObjectID     `json:"workflow_id"`
	Status     string                 `json:"status"` // pending, running, success, failed, cancelled
	Message    *NSQMessage            `json:"message"`
	Context    map[string]interface{} `json:"context"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
	CreatedAt  time.Time              `json:"created_at"`
}
