package handlers

import (
	"context"
	"net/http"
	"time"

	"nsa/internal/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ListDataSources 获取数据源列表
func ListDataSources(ctx *Context) gin.HandlerFunc {
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

		collection := ctx.MongoClient.GetDatabase().Collection("datasources")
		ctxDB, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 构建查询条件
		filter := bson.M{}
		if name := c.Query("name"); name != "" {
			filter["name"] = bson.M{"$regex": name, "$options": "i"}
		}
		if dbType := c.Query("type"); dbType != "" {
			filter["type"] = dbType
		}

		// 获取总数
		total, err := collection.CountDocuments(ctxDB, filter)
		if err != nil {
			ctx.Logger.Errorf("Failed to count datasources: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to count datasources",
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
			ctx.Logger.Errorf("Failed to find datasources: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to find datasources",
			})
			return
		}
		defer cursor.Close(ctxDB)

		var datasources []models.DataSource
		if err := cursor.All(ctxDB, &datasources); err != nil {
			ctx.Logger.Errorf("Failed to decode datasources: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to decode datasources",
			})
			return
		}

		// 隐藏密码字段
		for i := range datasources {
			datasources[i].Password = "****"
		}

		response := PaginationResponse{
			Total:    total,
			Page:     req.Page,
			PageSize: req.PageSize,
			Data:     datasources,
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    response,
		})
	}
}

// GetDataSource 获取单个数据源
func GetDataSource(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid datasource ID",
			})
			return
		}

		collection := ctx.MongoClient.GetDatabase().Collection("datasources")
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var datasource models.DataSource
		err = collection.FindOne(ctxDB, bson.M{"_id": objectID}).Decode(&datasource)
		if err != nil {
			ctx.Logger.Errorf("Failed to find datasource: %v", err)
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Datasource not found",
			})
			return
		}

		// 隐藏密码字段
		datasource.Password = "****"

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    datasource,
		})
	}
}

// CreateDataSource 创建数据源
func CreateDataSource(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		var datasource models.DataSource
		if err := c.ShouldBindJSON(&datasource); err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid request format",
			})
			return
		}

		// 验证必填字段
		if datasource.Name == "" || datasource.Type == "" || datasource.Host == "" {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Name, type, and host are required",
			})
			return
		}

		// 验证数据库类型
		validTypes := []string{"mysql", "postgresql", "sqlserver", "oracle", "mongodb"}
		validType := false
		for _, vt := range validTypes {
			if datasource.Type == vt {
				validType = true
				break
			}
		}
		if !validType {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid database type",
			})
			return
		}

		// 设置默认值
		if datasource.Port == 0 {
			switch datasource.Type {
			case "mysql":
				datasource.Port = 3306
			case "postgresql":
				datasource.Port = 5432
			case "sqlserver":
				datasource.Port = 1433
			case "oracle":
				datasource.Port = 1521
			case "mongodb":
				datasource.Port = 27017
			}
		}

		if datasource.MaxIdle == 0 {
			datasource.MaxIdle = 10
		}
		if datasource.MaxOpen == 0 {
			datasource.MaxOpen = 100
		}
		if datasource.MaxLifetime == 0 {
			datasource.MaxLifetime = 3600 // 1小时
		}

		// 设置创建时间
		datasource.CreatedAt = time.Now()
		datasource.UpdatedAt = time.Now()

		// 检查名称是否已存在
		collection := ctx.MongoClient.GetDatabase().Collection("datasources")
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		existingCount, err := collection.CountDocuments(ctxDB, bson.M{"name": datasource.Name})
		if err != nil {
			ctx.Logger.Errorf("Failed to check existing datasource: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to check existing datasource",
			})
			return
		}

		if existingCount > 0 {
			c.JSON(http.StatusConflict, Response{
				Code:    409,
				Message: "Datasource with same name already exists",
			})
			return
		}

		// 插入数据库
		result, err := collection.InsertOne(ctxDB, datasource)
		if err != nil {
			ctx.Logger.Errorf("Failed to create datasource: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to create datasource",
			})
			return
		}

		datasource.ID = result.InsertedID.(primitive.ObjectID)

		// 添加到数据源管理器
		if err := ctx.DataSourceMgr.AddDataSource(&datasource); err != nil {
			ctx.Logger.Errorf("Failed to add datasource to manager: %v", err)
			// 不返回错误，因为数据已经保存到数据库
		}

		// 隐藏密码字段
		datasource.Password = "****"

		ctx.Logger.Infof("Datasource created: %s", datasource.Name)
		c.JSON(http.StatusCreated, Response{
			Code:    201,
			Message: "Datasource created successfully",
			Data:    datasource,
		})
	}
}

// UpdateDataSource 更新数据源
func UpdateDataSource(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid datasource ID",
			})
			return
		}

		var datasource models.DataSource
		if err := c.ShouldBindJSON(&datasource); err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid request format",
			})
			return
		}

		// 获取原有数据源
		collection := ctx.MongoClient.GetDatabase().Collection("datasources")
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var originalDS models.DataSource
		err = collection.FindOne(ctxDB, bson.M{"_id": objectID}).Decode(&originalDS)
		if err != nil {
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Datasource not found",
			})
			return
		}

		// 如果密码是****，保持原密码
		if datasource.Password == "****" {
			datasource.Password = originalDS.Password
		}

		// 设置更新时间
		datasource.UpdatedAt = time.Now()
		datasource.CreatedAt = originalDS.CreatedAt

		// 更新数据库
		update := bson.M{"$set": datasource}
		result, err := collection.UpdateOne(ctxDB, bson.M{"_id": objectID}, update)
		if err != nil {
			ctx.Logger.Errorf("Failed to update datasource: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to update datasource",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Datasource not found",
			})
			return
		}

		// 从数据源管理器中移除旧的连接
		ctx.DataSourceMgr.RemoveDataSource(originalDS.Name)

		// 添加新的连接
		datasource.ID = objectID
		if err := ctx.DataSourceMgr.AddDataSource(&datasource); err != nil {
			ctx.Logger.Errorf("Failed to update datasource in manager: %v", err)
		}

		// 隐藏密码字段
		datasource.Password = "****"

		ctx.Logger.Infof("Datasource updated: %s", datasource.Name)
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Datasource updated successfully",
			Data:    datasource,
		})
	}
}

// DeleteDataSource 删除数据源
func DeleteDataSource(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid datasource ID",
			})
			return
		}

		collection := ctx.MongoClient.GetDatabase().Collection("datasources")
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// 获取数据源信息
		var datasource models.DataSource
		err = collection.FindOne(ctxDB, bson.M{"_id": objectID}).Decode(&datasource)
		if err != nil {
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Datasource not found",
			})
			return
		}

		// 删除数据库记录
		result, err := collection.DeleteOne(ctxDB, bson.M{"_id": objectID})
		if err != nil {
			ctx.Logger.Errorf("Failed to delete datasource: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to delete datasource",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Datasource not found",
			})
			return
		}

		// 从数据源管理器中移除
		ctx.DataSourceMgr.RemoveDataSource(datasource.Name)

		ctx.Logger.Infof("Datasource deleted: %s", datasource.Name)
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Datasource deleted successfully",
		})
	}
}

// TestDataSource 测试数据源连接
func TestDataSource(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid datasource ID",
			})
			return
		}

		collection := ctx.MongoClient.GetDatabase().Collection("datasources")
		ctxDB, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var datasource models.DataSource
		err = collection.FindOne(ctxDB, bson.M{"_id": objectID}).Decode(&datasource)
		if err != nil {
			c.JSON(http.StatusNotFound, Response{
				Code:    404,
				Message: "Datasource not found",
			})
			return
		}

		// 测试连接
		start := time.Now()
		err = ctx.DataSourceMgr.AddDataSource(&datasource)
		duration := time.Since(start)

		if err != nil {
			ctx.Logger.Errorf("Datasource connection test failed: %v", err)
			c.JSON(http.StatusOK, Response{
				Code:    200,
				Message: "Connection test completed",
				Data: map[string]interface{}{
					"success":  false,
					"error":    err.Error(),
					"duration": duration.String(),
				},
			})
			return
		}

		// 移除测试连接
		ctx.DataSourceMgr.RemoveDataSource(datasource.Name)

		ctx.Logger.Infof("Datasource connection test successful: %s", datasource.Name)
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Connection test completed",
			Data: map[string]interface{}{
				"success":  true,
				"duration": duration.String(),
			},
		})
	}
}
