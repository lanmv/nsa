package mongodb

import (
	"context"
	"time"

	"nsa/internal/config"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Client MongoDB客户端
type Client struct {
	client     *mongo.Client
	database   *mongo.Database
	collection *mongo.Collection
	config     config.MongoDBConfig
}

// NewClient 创建新的MongoDB客户端
func NewClient(cfg config.MongoDBConfig) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(cfg.DSN)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	// 测试连接
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	database := client.Database(cfg.Database)
	collection := database.Collection(cfg.Collection)

	return &Client{
		client:     client,
		database:   database,
		collection: collection,
		config:     cfg,
	}, nil
}

// GetClient 获取原始MongoDB客户端
func (c *Client) GetClient() *mongo.Client {
	return c.client
}

// GetDatabase 获取数据库实例
func (c *Client) GetDatabase() *mongo.Database {
	return c.database
}

// GetCollection 获取集合实例
func (c *Client) GetCollection() *mongo.Collection {
	return c.collection
}

// Disconnect 断开连接
func (c *Client) Disconnect() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.client.Disconnect(ctx)
}
