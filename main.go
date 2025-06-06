package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nsa/internal/config"
	"nsa/internal/logger"
	"nsa/internal/mongodb"
	"nsa/internal/nsq"
	"nsa/internal/server"
)

// main 程序入口点
func main() {
	// 加载配置
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化日志
	logger := logger.New(cfg.Logging)
	logger.Info("Starting NSA service...")

	// 初始化MongoDB连接
	mongoClient, err := mongodb.NewClient(cfg.MongoDB)
	if err != nil {
		logger.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer mongoClient.Disconnect()

	// 初始化NSQ消费者管理器
	nsqManager := nsq.NewManager(cfg.NSQ, logger)

	// 初始化HTTP服务器
	httpServer := server.New(cfg, logger, mongoClient, nsqManager)

	// 启动HTTP服务器
	go func() {
		logger.Infof("Starting HTTP server on port %d", cfg.Server.Port)
		if err := httpServer.Start(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down NSA service...")

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 停止NSQ消费者
	nsqManager.Stop()

	// 停止HTTP服务器
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Errorf("HTTP server forced to shutdown: %v", err)
	}

	logger.Info("NSA service stopped")
}
