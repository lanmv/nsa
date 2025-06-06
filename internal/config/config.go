package config

import (
	"encoding/json"
	"os"
)

// Config 应用配置结构
type Config struct {
	Server  ServerConfig  `json:"server"`
	MongoDB MongoDBConfig `json:"mongodb"`
	Logging LoggingConfig `json:"logging"`
	Admin   AdminConfig   `json:"admin"`
	NSQ     NSQConfig     `json:"nsq"`
}

// ServerConfig HTTP服务器配置
type ServerConfig struct {
	Port int    `json:"port"`
	Mode string `json:"mode"`
}

// MongoDBConfig MongoDB配置
type MongoDBConfig struct {
	DSN        string `json:"dsn"`
	Database   string `json:"database"`
	Collection string `json:"collection"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level     string          `json:"level"`
	LocalLogs LocalLogsConfig `json:"local_logs"`
	Graylog   GraylogConfig   `json:"graylog"`
}

// LocalLogsConfig 本地日志配置
type LocalLogsConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

// GraylogConfig Graylog配置
type GraylogConfig struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// AdminConfig 管理界面配置
type AdminConfig struct {
	GUIEnabled bool   `json:"gui_enabled"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	JWTSecret  string `json:"jwt_secret"`
}

// NSQConfig NSQ配置
type NSQConfig struct {
	LookupdAddresses []string `json:"lookupd_addresses"`
	NSQDAddresses    []string `json:"nsqd_addresses"`
}

// Load 从文件加载配置
func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// Save 保存配置到文件
func (c *Config) Save(filename string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}
