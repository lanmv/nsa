package datasource

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"nsa/internal/models"

	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/godror/godror"
	_ "github.com/lib/pq"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Manager 数据源管理器
type Manager struct {
	mu          sync.RWMutex
	sqlDBs      map[string]*sql.DB
	mongoDBs    map[string]*mongo.Client
	dataSources map[string]*models.DataSource
}

// NewManager 创建新的数据源管理器
func NewManager() *Manager {
	return &Manager{
		sqlDBs:      make(map[string]*sql.DB),
		mongoDBs:    make(map[string]*mongo.Client),
		dataSources: make(map[string]*models.DataSource),
	}
}

// AddDataSource 添加数据源
func (m *Manager) AddDataSource(ds *models.DataSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 保存数据源配置
	m.dataSources[ds.Name] = ds

	// 根据类型创建连接
	switch ds.Type {
	case "mysql", "postgresql", "sqlserver", "oracle":
		return m.createSQLConnection(ds)
	case "mongodb":
		return m.createMongoConnection(ds)
	default:
		return fmt.Errorf("unsupported database type: %s", ds.Type)
	}
}

// GetSQLDB 获取SQL数据库连接
func (m *Manager) GetSQLDB(name string) (*sql.DB, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	db, exists := m.sqlDBs[name]
	if !exists {
		return nil, fmt.Errorf("datasource %s not found", name)
	}
	return db, nil
}

// GetMongoDB 获取MongoDB连接
func (m *Manager) GetMongoDB(name string) (*mongo.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.mongoDBs[name]
	if !exists {
		return nil, fmt.Errorf("datasource %s not found", name)
	}
	return client, nil
}

// RemoveDataSource 移除数据源
func (m *Manager) RemoveDataSource(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 关闭SQL连接
	if db, exists := m.sqlDBs[name]; exists {
		db.Close()
		delete(m.sqlDBs, name)
	}

	// 关闭MongoDB连接
	if client, exists := m.mongoDBs[name]; exists {
		client.Disconnect(nil)
		delete(m.mongoDBs, name)
	}

	// 删除配置
	delete(m.dataSources, name)
	return nil
}

// ListDataSources 列出所有数据源
func (m *Manager) ListDataSources() []*models.DataSource {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*models.DataSource
	for _, ds := range m.dataSources {
		result = append(result, ds)
	}
	return result
}

// Close 关闭所有连接
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 关闭所有SQL连接
	for _, db := range m.sqlDBs {
		db.Close()
	}

	// 关闭所有MongoDB连接
	for _, client := range m.mongoDBs {
		client.Disconnect(nil)
	}
}

// createSQLConnection 创建SQL数据库连接
func (m *Manager) createSQLConnection(ds *models.DataSource) error {
	var dsn string

	switch ds.Type {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			ds.Username, ds.Password, ds.Host, ds.Port, ds.Database)
	case "postgresql":
		sslMode := "disable"
		if ds.SSL {
			sslMode = "require"
		}
		dsn = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			ds.Host, ds.Port, ds.Username, ds.Password, ds.Database, sslMode)
	case "sqlserver":
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			ds.Username, ds.Password, ds.Host, ds.Port, ds.Database)
	case "oracle":
		dsn = fmt.Sprintf("%s/%s@%s:%d/%s",
			ds.Username, ds.Password, ds.Host, ds.Port, ds.Database)
	default:
		return fmt.Errorf("unsupported SQL database type: %s", ds.Type)
	}

	db, err := sql.Open(ds.Type, dsn)
	if err != nil {
		return err
	}

	// 配置连接池
	db.SetMaxIdleConns(ds.MaxIdle)
	db.SetMaxOpenConns(ds.MaxOpen)
	db.SetConnMaxLifetime(time.Duration(ds.MaxLifetime) * time.Second)

	// 测试连接
	if err := db.Ping(); err != nil {
		db.Close()
		return err
	}

	m.sqlDBs[ds.Name] = db
	return nil
}

// createMongoConnection 创建MongoDB连接
func (m *Manager) createMongoConnection(ds *models.DataSource) error {
	dsn := fmt.Sprintf("mongodb://%s:%s@%s:%d/%s",
		ds.Username, ds.Password, ds.Host, ds.Port, ds.Database)

	clientOptions := options.Client().ApplyURI(dsn)
	client, err := mongo.Connect(nil, clientOptions)
	if err != nil {
		return err
	}

	// 测试连接
	if err := client.Ping(nil, nil); err != nil {
		client.Disconnect(nil)
		return err
	}

	m.mongoDBs[ds.Name] = client
	return nil
}
