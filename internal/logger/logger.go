package logger

import (
	"fmt"
	"os"
	"path/filepath"

	"nsa/internal/config"

	"github.com/Graylog2/go-gelf/gelf"
	"github.com/sirupsen/logrus"
)

// Logger 日志接口
type Logger interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
}

// LoggerImpl 日志实现
type LoggerImpl struct {
	logger *logrus.Logger
}

// New 创建新的日志实例
func New(cfg config.LoggingConfig) Logger {
	logger := logrus.New()

	// 设置日志级别
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// 设置日志格式
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// 配置本地日志
	if cfg.LocalLogs.Enabled {
		if err := os.MkdirAll(cfg.LocalLogs.Path, 0755); err != nil {
			logger.Errorf("Failed to create log directory: %v", err)
		} else {
			logFile := filepath.Join(cfg.LocalLogs.Path, "nsa.log")
			file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				logger.Errorf("Failed to open log file: %v", err)
			} else {
				logger.SetOutput(file)
			}
		}
	}

	// 配置Graylog
	if cfg.Graylog.Enabled {
		graylogAddr := fmt.Sprintf("%s:%d", cfg.Graylog.Host, cfg.Graylog.Port)
		gelfWriter, err := gelf.NewUDPWriter(graylogAddr)
		if err != nil {
			logger.Errorf("Failed to create Graylog writer: %v", err)
		} else {
			logger.AddHook(&GraylogHook{writer: gelfWriter})
		}
	}

	return &LoggerImpl{logger: logger}
}

// Debug 调试日志
func (l *LoggerImpl) Debug(args ...interface{}) {
	l.logger.Debug(args...)
}

// Debugf 格式化调试日志
func (l *LoggerImpl) Debugf(format string, args ...interface{}) {
	l.logger.Debugf(format, args...)
}

// Info 信息日志
func (l *LoggerImpl) Info(args ...interface{}) {
	l.logger.Info(args...)
}

// Infof 格式化信息日志
func (l *LoggerImpl) Infof(format string, args ...interface{}) {
	l.logger.Infof(format, args...)
}

// Warn 警告日志
func (l *LoggerImpl) Warn(args ...interface{}) {
	l.logger.Warn(args...)
}

// Warnf 格式化警告日志
func (l *LoggerImpl) Warnf(format string, args ...interface{}) {
	l.logger.Warnf(format, args...)
}

// Error 错误日志
func (l *LoggerImpl) Error(args ...interface{}) {
	l.logger.Error(args...)
}

// Errorf 格式化错误日志
func (l *LoggerImpl) Errorf(format string, args ...interface{}) {
	l.logger.Errorf(format, args...)
}

// Fatal 致命错误日志
func (l *LoggerImpl) Fatal(args ...interface{}) {
	l.logger.Fatal(args...)
}

// Fatalf 格式化致命错误日志
func (l *LoggerImpl) Fatalf(format string, args ...interface{}) {
	l.logger.Fatalf(format, args...)
}

// GraylogHook Graylog钩子
type GraylogHook struct {
	writer gelf.Writer
}

// Levels 返回支持的日志级别
func (hook *GraylogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire 发送日志到Graylog
func (hook *GraylogHook) Fire(entry *logrus.Entry) error {
	message := &gelf.Message{
		Version:  "1.1",
		Host:     "nsa-service",
		Short:    entry.Message,
		TimeUnix: float64(entry.Time.Unix()),
		Level:    int32(entry.Level),
		Extra:    make(map[string]interface{}),
	}

	// 添加字段
	for k, v := range entry.Data {
		message.Extra[k] = v
	}

	return hook.writer.WriteMessage(message)
}
