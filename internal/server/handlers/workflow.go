package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nsa/internal/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ListWorkflows 获取工作流列表
func ListWorkflows(ctx *Context) gin.HandlerFunc {
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
			req.PageSize = 20
		}

		collection := ctx.MongoClient.GetCollection()
		ctxDB, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 构建查询条件
		filter := bson.M{}
		if topic := c.Query("topic"); topic != "" {
			filter["topic"] = bson.M{"$regex": topic, "$options": "i"}
		}
		if enabled := c.Query("enabled"); enabled != "" {
			filter["enabled"] = enabled == "true"
		}

		// 获取总数
		total, err := collection.CountDocuments(ctxDB, filter)
		if err != nil {
			ctx.Logger.Errorf("Failed to count workflows: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to count workflows",
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
			ctx.Logger.Errorf("Failed to find workflows: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to find workflows",
			})
			return
		}
		defer cursor.Close(ctxDB)

		var workflows []models.WorkflowConfig
		if err := cursor.All(ctxDB, &workflows); err != nil {
			ctx.Logger.Errorf("Failed to decode workflows: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to decode workflows",
			})
			return
		}

		response := PaginationResponse{
			Total:    total,
			Page:     req.Page,
			PageSize: req.PageSize,
			Data:     workflows,
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    response,
		})
	}
}

// GetWorkflow 获取单个工作流
func GetWorkflow(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid workflow ID",
			})
			return
		}

		collection := ctx.MongoClient.GetCollection()
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var workflow models.WorkflowConfig
		err = collection.FindOne(ctxDB, bson.M{"_id": objectID}).Decode(&workflow)
		if err != nil {
			ctx.Logger.Errorf("Failed to find workflow: %v", err)
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Workflow not found",
			})
			return
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    workflow,
		})
	}
}

// CreateWorkflow 创建工作流
func CreateWorkflow(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		var workflow models.WorkflowConfig
		if err := c.ShouldBindJSON(&workflow); err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid request format",
			})
			return
		}

		// 验证必填字段
		if workflow.Name == "" || workflow.Topic == "" || workflow.Channel == "" {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Name, topic, and channel are required",
			})
			return
		}

		// 设置创建时间
		workflow.CreatedAt = time.Now()
		workflow.UpdatedAt = time.Now()

		// 检查topic和channel组合是否已存在
		collection := ctx.MongoClient.GetCollection()
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		existingCount, err := collection.CountDocuments(ctxDB, bson.M{
			"topic":   workflow.Topic,
			"channel": workflow.Channel,
		})
		if err != nil {
			ctx.Logger.Errorf("Failed to check existing workflow: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to check existing workflow",
			})
			return
		}

		if existingCount > 0 {
			c.JSON(http.StatusConflict, Response{
				Code:    409,
				Message: "Workflow with same topic and channel already exists",
			})
			return
		}

		// 插入数据库
		result, err := collection.InsertOne(ctxDB, workflow)
		if err != nil {
			ctx.Logger.Errorf("Failed to create workflow: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to create workflow",
			})
			return
		}

		workflow.ID = result.InsertedID.(primitive.ObjectID)

		// 如果工作流启用，重新加载NSQ消费者
		if workflow.Enabled {
			go ctx.reloadNSQConsumers()
		}

		ctx.Logger.Infof("Workflow created: %s", workflow.Name)
		c.JSON(http.StatusCreated, Response{
			Code:    201,
			Message: "Workflow created successfully",
			Data:    workflow,
		})
	}
}

// UpdateWorkflow 更新工作流
func UpdateWorkflow(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid workflow ID",
			})
			return
		}

		var workflow models.WorkflowConfig
		if err := c.ShouldBindJSON(&workflow); err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid request format",
			})
			return
		}

		// 设置更新时间
		workflow.UpdatedAt = time.Now()

		collection := ctx.MongoClient.GetCollection()
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// 更新数据库
		update := bson.M{"$set": workflow}
		result, err := collection.UpdateOne(ctxDB, bson.M{"_id": objectID}, update)
		if err != nil {
			ctx.Logger.Errorf("Failed to update workflow: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to update workflow",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Workflow not found",
			})
			return
		}

		// 重新加载NSQ消费者
		go ctx.reloadNSQConsumers()

		workflow.ID = objectID
		ctx.Logger.Infof("Workflow updated: %s", workflow.Name)
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Workflow updated successfully",
			Data:    workflow,
		})
	}
}

// DeleteWorkflow 删除工作流
func DeleteWorkflow(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid workflow ID",
			})
			return
		}

		collection := ctx.MongoClient.GetCollection()
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// 删除数据库记录
		result, err := collection.DeleteOne(ctxDB, bson.M{"_id": objectID})
		if err != nil {
			ctx.Logger.Errorf("Failed to delete workflow: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to delete workflow",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Workflow not found",
			})
			return
		}

		// 重新加载NSQ消费者
		go ctx.reloadNSQConsumers()

		ctx.Logger.Infof("Workflow deleted: %s", id)
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Workflow deleted successfully",
		})
	}
}

// EnableWorkflow 启用工作流
func EnableWorkflow(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx.updateWorkflowStatus(c, true)
	}
}

// DisableWorkflow 禁用工作流
func DisableWorkflow(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx.updateWorkflowStatus(c, false)
	}
}

// updateWorkflowStatus 更新工作流状态
func (ctx *Context) updateWorkflowStatus(c *gin.Context, enabled bool) {
	id := c.Param("id")
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "Invalid workflow ID",
		})
		return
	}

	collection := ctx.MongoClient.GetCollection()
	ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 更新状态
	update := bson.M{
		"$set": bson.M{
			"enabled":    enabled,
			"updated_at": time.Now(),
		},
	}

	result, err := collection.UpdateOne(ctxDB, bson.M{"_id": objectID}, update)
	if err != nil {
		ctx.Logger.Errorf("Failed to update workflow status: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "Failed to update workflow status",
		})
		return
	}

	if result.MatchedCount == 0 {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "Workflow not found",
		})
		return
	}

	// 重新加载NSQ消费者
	go ctx.reloadNSQConsumers()

	status := "disabled"
	if enabled {
		status = "enabled"
	}

	ctx.Logger.Infof("Workflow %s: %s", status, id)
	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: fmt.Sprintf("Workflow %s successfully", status),
	})
}

// reloadNSQConsumers 重新加载NSQ消费者
func (ctx *Context) reloadNSQConsumers() {
	// 获取所有启用的工作流
	collection := ctx.MongoClient.GetCollection()
	ctxDB, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := collection.Find(ctxDB, bson.M{"enabled": true})
	if err != nil {
		ctx.Logger.Errorf("Failed to find enabled workflows: %v", err)
		return
	}
	defer cursor.Close(ctxDB)

	var workflows []*models.WorkflowConfig
	if err := cursor.All(ctxDB, &workflows); err != nil {
		ctx.Logger.Errorf("Failed to decode workflows: %v", err)
		return
	}

	// 重新加载消费者
	if err := ctx.NSQManager.ReloadConsumers(workflows); err != nil {
		ctx.Logger.Errorf("Failed to reload NSQ consumers: %v", err)
	}
}
