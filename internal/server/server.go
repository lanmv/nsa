package server

import (
	"context"
	"fmt"
	"net/http"

	"nsa/internal/config"
	"nsa/internal/datasource"
	"nsa/internal/logger"
	"nsa/internal/mongodb"
	"nsa/internal/nsq"
	"nsa/internal/server/handlers"
	"nsa/internal/workflow"

	"github.com/gin-gonic/gin"
)

// Server HTTP服务器
type Server struct {
	config        *config.Config
	logger        logger.Logger
	mongoClient   *mongodb.Client
	nsqManager    *nsq.Manager
	dataSourceMgr *datasource.Manager
	executor      *workflow.Executor
	router        *gin.Engine
	httpServer    *http.Server
}

// New 创建新的HTTP服务器
func New(cfg *config.Config, logger logger.Logger, mongoClient *mongodb.Client, nsqManager *nsq.Manager) *Server {
	// 设置Gin模式
	gin.SetMode(cfg.Server.Mode)

	// 创建数据源管理器
	dataSourceMgr := datasource.NewManager()

	// 创建工作流执行器
	executor := workflow.NewExecutor(logger, mongoClient, dataSourceMgr)

	// 设置NSQ管理器的执行器
	nsqManager.SetExecutor(executor)

	server := &Server{
		config:        cfg,
		logger:        logger,
		mongoClient:   mongoClient,
		nsqManager:    nsqManager,
		dataSourceMgr: dataSourceMgr,
		executor:      executor,
	}

	// 初始化路由
	server.setupRoutes()

	return server
}

// setupRoutes 设置路由
func (s *Server) setupRoutes() {
	s.router = gin.New()

	// 添加中间件
	s.router.Use(gin.Logger())
	s.router.Use(gin.Recovery())
	s.router.Use(s.corsMiddleware())

	// 创建处理器
	handlerCtx := &handlers.Context{
		Config:        s.config,
		Logger:        s.logger,
		MongoClient:   s.mongoClient,
		NSQManager:    s.nsqManager,
		DataSourceMgr: s.dataSourceMgr,
		Executor:      s.executor,
	}

	// 健康检查
	s.router.GET("/health", handlers.HealthCheck(handlerCtx))

	// API路由组
	api := s.router.Group("/api/v1")
	{
		// 认证中间件
		api.Use(handlers.AuthMiddleware(handlerCtx))

		// 工作流管理
		workflows := api.Group("/workflows")
		{
			workflows.GET("", handlers.ListWorkflows(handlerCtx))
			workflows.POST("", handlers.CreateWorkflow(handlerCtx))
			workflows.GET("/:id", handlers.GetWorkflow(handlerCtx))
			workflows.PUT("/:id", handlers.UpdateWorkflow(handlerCtx))
			workflows.DELETE("/:id", handlers.DeleteWorkflow(handlerCtx))
			workflows.POST("/:id/enable", handlers.EnableWorkflow(handlerCtx))
			workflows.POST("/:id/disable", handlers.DisableWorkflow(handlerCtx))
		}

		// 数据源管理
		datasources := api.Group("/datasources")
		{
			datasources.GET("", handlers.ListDataSources(handlerCtx))
			datasources.POST("", handlers.CreateDataSource(handlerCtx))
			datasources.GET("/:id", handlers.GetDataSource(handlerCtx))
			datasources.PUT("/:id", handlers.UpdateDataSource(handlerCtx))
			datasources.DELETE("/:id", handlers.DeleteDataSource(handlerCtx))
			datasources.POST("/:id/test", handlers.TestDataSource(handlerCtx))
		}

		// 执行日志
		logs := api.Group("/logs")
		{
			logs.GET("/executions", handlers.ListExecutionLogs(handlerCtx))
			logs.GET("/executions/:id", handlers.GetExecutionLog(handlerCtx))
		}

		// NSQ管理
		nsqAPI := api.Group("/nsq")
		{
			nsqAPI.GET("/consumers", handlers.ListNSQConsumers(handlerCtx))
			nsqAPI.GET("/stats", handlers.GetNSQStats(handlerCtx))
			nsqAPI.POST("/reload", handlers.ReloadNSQConsumers(handlerCtx))
		}

		// 系统信息
		system := api.Group("/system")
		{
			system.GET("/info", handlers.GetSystemInfo(handlerCtx))
			system.GET("/metrics", handlers.GetMetrics(handlerCtx))
		}
	}

	// 认证路由
	auth := s.router.Group("/auth")
	{
		auth.POST("/login", handlers.Login(handlerCtx))
		auth.POST("/logout", handlers.Logout(handlerCtx))
		auth.GET("/me", handlers.AuthMiddleware(handlerCtx), handlers.GetCurrentUser(handlerCtx))
	}

	// 静态文件服务（如果启用了GUI）
	if s.config.Admin.GUIEnabled {
		s.router.Static("/static", "./web/static")
		s.router.StaticFile("/", "./web/index.html")
		s.router.StaticFile("/favicon.ico", "./web/favicon.ico")
		// 处理SPA路由
		s.router.NoRoute(func(c *gin.Context) {
			c.File("./web/index.html")
		})
	}
}

// corsMiddleware CORS中间件
func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Start 启动HTTP服务器
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.Server.Port),
		Handler: s.router,
	}

	s.logger.Infof("Starting HTTP server on port %d", s.config.Server.Port)
	return s.httpServer.ListenAndServe()
}

// Shutdown 优雅关闭HTTP服务器
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP server...")

	// 停止工作流执行器
	s.executor.Stop()

	// 关闭数据源连接
	s.dataSourceMgr.Close()

	// 关闭HTTP服务器
	return s.httpServer.Shutdown(ctx)
}
