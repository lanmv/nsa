package handlers

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"nsa/internal/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// HealthCheck 健康检查
func HealthCheck(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查MongoDB连接
		ctxDB, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := ctx.MongoClient.GetClient().Ping(ctxDB, nil)
		mongoStatus := "healthy"
		if err != nil {
			mongoStatus = "unhealthy"
		}

		// 获取NSQ消费者状态
		nsqConsumers := ctx.NSQManager.ListConsumers()

		health := map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now(),
			"version":   "1.0.0",
			"services": map[string]interface{}{
				"mongodb": mongoStatus,
				"nsq": map[string]interface{}{
					"consumers_count": len(nsqConsumers),
					"consumers":       nsqConsumers,
				},
			},
		}

		statusCode := http.StatusOK
		if mongoStatus == "unhealthy" {
			health["status"] = "unhealthy"
			statusCode = http.StatusServiceUnavailable
		}

		c.JSON(statusCode, Response{
			Code:    statusCode,
			Message: "Health check completed",
			Data:    health,
		})
	}
}

// GetSystemInfo 获取系统信息
func GetSystemInfo(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		systemInfo := map[string]interface{}{
			"version":    "1.0.0",
			"go_version": runtime.Version(),
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
			"cpu_count":  runtime.NumCPU(),
			"goroutines": runtime.NumGoroutine(),
			"memory": map[string]interface{}{
				"alloc":       bToMb(m.Alloc),
				"total_alloc": bToMb(m.TotalAlloc),
				"sys":         bToMb(m.Sys),
				"gc_runs":     m.NumGC,
			},
			"uptime": time.Since(startTime).String(),
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    systemInfo,
		})
	}
}

// GetMetrics 获取系统指标
func GetMetrics(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取NSQ统计信息
		nsqStats := ctx.NSQManager.GetConsumerStats()

		// 获取工作流统计
		workflowStats, err := getWorkflowStats(ctx)
		if err != nil {
			ctx.Logger.Errorf("Failed to get workflow stats: %v", err)
			workflowStats = map[string]interface{}{"error": "Failed to get stats"}
		}

		// 获取执行日志统计
		executionStats, err := getExecutionStats(ctx)
		if err != nil {
			ctx.Logger.Errorf("Failed to get execution stats: %v", err)
			executionStats = map[string]interface{}{"error": "Failed to get stats"}
		}

		metrics := map[string]interface{}{
			"timestamp":     time.Now(),
			"nsq_consumers": nsqStats,
			"workflows":     workflowStats,
			"executions":    executionStats,
			"data_sources":  len(ctx.DataSourceMgr.ListDataSources()),
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    metrics,
		})
	}
}

// ListExecutionLogs 获取执行日志列表
func ListExecutionLogs(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req PaginationRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid query parameters",
			})
			return
		}

		// 设置默认值
		if req.Page <= 0 {
			req.Page = 1
		}
		if req.PageSize <= 0 {
			req.PageSize = 50
		}

		collection := ctx.MongoClient.GetDatabase().Collection("execution_logs")
		ctxDB, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 构建查询条件
		filter := bson.M{}
		if workflowID := c.Query("workflow_id"); workflowID != "" {
			if objectID, err := primitive.ObjectIDFromHex(workflowID); err == nil {
				filter["workflow_id"] = objectID
			}
		}
		if instanceID := c.Query("instance_id"); instanceID != "" {
			filter["instance_id"] = instanceID
		}
		if status := c.Query("status"); status != "" {
			filter["status"] = status
		}

		// 获取总数
		total, err := collection.CountDocuments(ctxDB, filter)
		if err != nil {
			ctx.Logger.Errorf("Failed to count execution logs: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to count execution logs",
			})
			return
		}

		// 查询数据
		opts := options.Find()
		opts.SetSkip(int64((req.Page - 1) * req.PageSize))
		opts.SetLimit(int64(req.PageSize))
		opts.SetSort(bson.D{{"created_at", -1}})

		cursor, err := collection.Find(ctxDB, filter, opts)
		if err != nil {
			ctx.Logger.Errorf("Failed to find execution logs: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to find execution logs",
			})
			return
		}
		defer cursor.Close(ctxDB)

		var logs []models.ExecutionLog
		if err := cursor.All(ctxDB, &logs); err != nil {
			ctx.Logger.Errorf("Failed to decode execution logs: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to decode execution logs",
			})
			return
		}

		response := PaginationResponse{
			Total:    total,
			Page:     req.Page,
			PageSize: req.PageSize,
			Data:     logs,
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    response,
		})
	}
}

// GetExecutionLog 获取单个执行日志
func GetExecutionLog(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid log ID",
			})
			return
		}

		collection := ctx.MongoClient.GetDatabase().Collection("execution_logs")
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var log models.ExecutionLog
		err = collection.FindOne(ctxDB, bson.M{"_id": objectID}).Decode(&log)
		if err != nil {
			ctx.Logger.Errorf("Failed to find execution log: %v", err)
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Execution log not found",
			})
			return
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    log,
		})
	}
}

// ListNSQConsumers 获取NSQ消费者列表
func ListNSQConsumers(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		consumers := ctx.NSQManager.ListConsumers()

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    consumers,
		})
	}
}

// GetNSQStats 获取NSQ统计信息
func GetNSQStats(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats := ctx.NSQManager.GetConsumerStats()

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    stats,
		})
	}
}

// ReloadNSQConsumers 重新加载NSQ消费者
func ReloadNSQConsumers(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取所有启用的工作流
		collection := ctx.MongoClient.GetCollection()
		ctxDB, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cursor, err := collection.Find(ctxDB, bson.M{"enabled": true})
		if err != nil {
			ctx.Logger.Errorf("Failed to find enabled workflows: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to find enabled workflows",
			})
			return
		}
		defer cursor.Close(ctxDB)

		var workflows []*models.WorkflowConfig
		if err := cursor.All(ctxDB, &workflows); err != nil {
			ctx.Logger.Errorf("Failed to decode workflows: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to decode workflows",
			})
			return
		}

		// 重新加载消费者
		if err := ctx.NSQManager.ReloadConsumers(workflows); err != nil {
			ctx.Logger.Errorf("Failed to reload NSQ consumers: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to reload NSQ consumers",
			})
			return
		}

		ctx.Logger.Info("NSQ consumers reloaded successfully")
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "NSQ consumers reloaded successfully",
		})
	}
}

// 辅助函数
var startTime = time.Now()

// bToMb 字节转MB
func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

// getWorkflowStats 获取工作流统计信息
func getWorkflowStats(ctx *Context) (map[string]interface{}, error) {
	collection := ctx.MongoClient.GetCollection()
	ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 总数
	total, err := collection.CountDocuments(ctxDB, bson.M{})
	if err != nil {
		return nil, err
	}

	// 启用的数量
	enabled, err := collection.CountDocuments(ctxDB, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total":    total,
		"enabled":  enabled,
		"disabled": total - enabled,
	}, nil
}

// getExecutionStats 获取执行统计信息
func getExecutionStats(ctx *Context) (map[string]interface{}, error) {
	collection := ctx.MongoClient.GetDatabase().Collection("execution_logs")
	ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 今天的统计
	today := time.Now().Truncate(24 * time.Hour)
	todayFilter := bson.M{"created_at": bson.M{"$gte": today}}

	todayTotal, _ := collection.CountDocuments(ctxDB, todayFilter)
	todaySuccess, _ := collection.CountDocuments(ctxDB, bson.M{
		"created_at": bson.M{"$gte": today},
		"status":     "success",
	})
	todayFailed, _ := collection.CountDocuments(ctxDB, bson.M{
		"created_at": bson.M{"$gte": today},
		"status":     "failed",
	})

	return map[string]interface{}{
		"today": map[string]interface{}{
			"total":   todayTotal,
			"success": todaySuccess,
			"failed":  todayFailed,
		},
	}, nil
}
