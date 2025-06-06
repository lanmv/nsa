package handlers

import (
	"nsa/internal/config"
	"nsa/internal/datasource"
	"nsa/internal/logger"
	"nsa/internal/mongodb"
	"nsa/internal/nsq"
	"nsa/internal/workflow"
)

// Context 处理器上下文
type Context struct {
	Config        *config.Config
	Logger        logger.Logger
	MongoClient   *mongodb.Client
	NSQManager    *nsq.Manager
	DataSourceMgr *datasource.Manager
	Executor      *workflow.Executor
}

// Response 统一响应结构
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// PaginationRequest 分页请求
type PaginationRequest struct {
	Page     int `form:"page" json:"page"`
	PageSize int `form:"page_size" json:"page_size"`
}

// PaginationResponse 分页响应
type PaginationResponse struct {
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Data     interface{} `json:"data"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	User      User   `json:"user"`
}

// User 用户信息
type User struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}
