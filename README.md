# NSA - NSQ 订阅端消息处理服务

NSA (NSQ Subscriber Agent) 是一个基于 Golang 开发的通用 NSQ 订阅端消息处理服务。该服务通过配置监听订阅消息，收到消息后根据预配置的处理流程进行逐步处理。

## 功能特性

- **多数据源支持**: 支持 MySQL、PostgreSQL、SQL Server、Oracle、MongoDB 等多种数据库类型
- **工作流引擎**: 内置轻量级工作流执行器，支持顺序任务执行
- **多种节点类型**: 
  - HTTP Client 节点：支持 HTTP 请求处理
  - DB Client 节点：支持 SQL 查询和执行
  - JS Function 节点：基于 QuickJS 的 JavaScript 执行器
- **日志管理**: 支持本地日志和 Graylog 远程日志
- **管理界面**: 提供 Web 管理界面
- **简化架构**: 采用顺序执行模式，易于理解和调试
- **幂等性保证**: 确保相同参数下处理结果一致
- **详细日志**: 记录每个处理步骤的详细日志

## 项目结构

```
NSA/
├── main.go                              # 主入口文件
├── go.mod                               # Go 模块文件
├── config.json                          # 配置文件
├── README.md                            # 项目说明
├── internal/
│   ├── config/
│   │   └── config.go                    # 配置管理
│   ├── logger/
│   │   └── logger.go                    # 日志管理
│   ├── mongodb/
│   │   └── client.go                    # MongoDB 客户端
│   ├── models/
│   │   └── models.go                    # 数据模型
│   ├── datasource/
│   │   └── manager.go                   # 数据源管理
│   ├── workflow/
│   │   ├── actions.go                   # 工作流动作
│   │   └── executor.go                  # 工作流执行器
│   ├── nsq/
│   │   └── manager.go                   # NSQ 管理
│   └── server/
│       ├── server.go                    # HTTP 服务器
│       └── handlers/
│           ├── context.go               # 处理器上下文
│           ├── auth.go                  # 认证处理器
│           ├── workflow.go              # 工作流处理器
│           ├── datasource.go            # 数据源处理器
│           └── common.go                # 通用处理器
└── logs/                                # 日志目录
```

## 快速开始

### 1. 环境要求

- Go 1.19+
- MongoDB 4.0+
- NSQ 1.0+

### 2. 安装依赖

```bash
go mod tidy
```

### 3. 配置文件

编辑 `config.json` 文件，配置 MongoDB 连接、NSQ 地址等信息：

```json
{
  "server": {
    "port": 8080,
    "host": "0.0.0.0"
  },
  "mongodb": {
    "uri": "mongodb://localhost:27017",
    "database": "nsa"
  },
  "logging": {
    "level": "info",
    "format": "json",
    "local": {
      "enabled": true,
      "path": "./logs"
    },
    "graylog": {
      "enabled": false,
      "host": "localhost",
      "port": 12201
    }
  },
  "admin": {
    "enabled": true,
    "username": "admin",
    "password": "admin123",
    "jwt_secret": "your-jwt-secret-key",
    "static_path": "./web"
  },
  "nsq": {
    "lookupd_addresses": ["localhost:4161"],
    "nsqd_addresses": ["localhost:4150"]
  }
}
```

### 4. 启动服务

```bash
go run main.go
```

服务启动后，可以通过以下地址访问：

- API 接口: `http://localhost:8080`
- 健康检查: `http://localhost:8080/health`
- 管理界面: `http://localhost:8080/admin` (如果启用)

## API 接口

### 认证接口

- `POST /api/auth/login` - 用户登录
- `POST /api/auth/logout` - 用户登出
- `GET /api/auth/user` - 获取当前用户信息

### 工作流管理

- `GET /api/workflows` - 获取工作流列表
- `GET /api/workflows/:id` - 获取单个工作流
- `POST /api/workflows` - 创建工作流
- `PUT /api/workflows/:id` - 更新工作流
- `DELETE /api/workflows/:id` - 删除工作流
- `POST /api/workflows/:id/enable` - 启用工作流
- `POST /api/workflows/:id/disable` - 禁用工作流

### 数据源管理

- `GET /api/datasources` - 获取数据源列表
- `GET /api/datasources/:id` - 获取单个数据源
- `POST /api/datasources` - 创建数据源
- `PUT /api/datasources/:id` - 更新数据源
- `DELETE /api/datasources/:id` - 删除数据源
- `POST /api/datasources/:id/test` - 测试数据源连接

### 执行日志

- `GET /api/logs` - 获取执行日志列表
- `GET /api/logs/:id` - 获取单个执行日志

### NSQ 管理

- `GET /api/nsq/consumers` - 获取 NSQ 消费者列表
- `GET /api/nsq/stats` - 获取 NSQ 统计信息
- `POST /api/nsq/reload` - 重新加载 NSQ 消费者

### 系统信息

- `GET /api/system/info` - 获取系统信息
- `GET /api/system/metrics` - 获取系统指标

## 工作流配置

### 工作流结构

工作流采用顺序执行模式，任务按照在配置中定义的顺序依次执行。每个任务的输出可以作为后续任务的输入。

```json
{
  "name": "示例工作流",
  "description": "处理用户注册消息",
  "topic": "user.register",
  "channel": "nsa",
  "enabled": true,
  "workflow_config": {
    "name": "user_register_workflow",
    "vars": [
      {
        "name": "user_data",
        "value": "{{.message}}"
      }
    ],
    "tasks": [
      {
        "name": "validate_user",
        "action": "js_function",
        "params": {
          "code": "function validate(data) { return data.email && data.username; }"
        }
      },
      {
        "name": "save_user",
        "action": "db_client",
        "params": {
          "datasource": "main_db",
          "operation": "execute",
          "sql": "INSERT INTO users (username, email) VALUES (?, ?)",
          "args": ["{{.user_data.username}}", "{{.user_data.email}}"]
        }
      }
    ]
  }
}
```

### 节点类型

所有节点（任务）按照配置中的顺序依次执行。每个节点可以通过模板变量访问前面节点的执行结果和工作流变量。

#### 1. HTTP Client 节点

```json
{
  "name": "call_api",
  "action": "http_client",
  "params": {
    "method": "POST",
    "url": "https://api.example.com/webhook",
    "headers": {
      "Content-Type": "application/json"
    },
    "body": "{{.message}}"
  }
}
```

#### 2. DB Client 节点

```json
{
  "name": "query_user",
  "action": "db_client",
  "params": {
    "datasource": "main_db",
    "operation": "query",
    "sql": "SELECT * FROM users WHERE id = ?",
    "args": ["{{.user_id}}"]
  }
}
```

#### 3. JS Function 节点

```json
{
  "name": "process_data",
  "action": "js_function",
  "params": {
    "code": "function process(data) { return { processed: true, timestamp: Date.now(), data: data }; }"
  }
}
```

## 数据源配置

### MySQL 数据源

```json
{
  "name": "mysql_main",
  "type": "mysql",
  "host": "localhost",
  "port": 3306,
  "database": "myapp",
  "username": "root",
  "password": "password",
  "max_idle": 10,
  "max_open": 100,
  "max_lifetime": 3600
}
```

### PostgreSQL 数据源

```json
{
  "name": "postgres_main",
  "type": "postgresql",
  "host": "localhost",
  "port": 5432,
  "database": "myapp",
  "username": "postgres",
  "password": "password",
  "ssl_mode": "disable"
}
```

### MongoDB 数据源

```json
{
  "name": "mongo_logs",
  "type": "mongodb",
  "host": "localhost",
  "port": 27017,
  "database": "logs",
  "username": "admin",
  "password": "password"
}
```

## 部署

### Docker 部署

```dockerfile
FROM golang:1.19-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o nsa main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/nsa .
COPY --from=builder /app/config.json .

CMD ["./nsa"]
```

### 构建和运行

```bash
# 构建镜像
docker build -t nsa:latest .

# 运行容器
docker run -d \
  --name nsa \
  -p 8080:8080 \
  -v $(pwd)/config.json:/root/config.json \
  -v $(pwd)/logs:/root/logs \
  nsa:latest
```

## 监控和日志

### 日志级别

- `debug`: 调试信息
- `info`: 一般信息
- `warn`: 警告信息
- `error`: 错误信息

### 指标监控

服务提供以下监控指标：

- NSQ 消费者状态
- 工作流执行统计
- 数据源连接状态
- 系统资源使用情况

## 故障排除

### 常见问题

1. **MongoDB 连接失败**
   - 检查 MongoDB 服务是否启动
   - 验证连接字符串是否正确
   - 确认网络连通性

2. **NSQ 消费者无法启动**
   - 检查 NSQ 服务是否运行
   - 验证 lookupd 和 nsqd 地址
   - 确认 topic 和 channel 配置

3. **工作流执行失败**
   - 查看执行日志获取详细错误信息
   - 检查数据源连接状态
   - 验证工作流配置语法
   - 由于采用顺序执行，可以通过日志确定具体在哪个任务失败

### 日志查看

```bash
# 查看服务日志
tail -f logs/nsa.log

# 查看特定工作流日志
grep "workflow_id" logs/nsa.log
```

## 贡献

欢迎提交 Issue 和 Pull Request 来改进项目。

## 许可证

MIT License