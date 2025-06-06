package workflow

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nsa/internal/datasource"
	"nsa/internal/logger"
	"nsa/internal/models"

	"github.com/buke/quickjs-go"
)

// ActionContext 动作执行上下文
type ActionContext struct {
	Logger         logger.Logger
	DataSourceMgr  *datasource.Manager
	NSQMessage     *models.NSQMessage
	WorkflowVars   map[string]interface{}
	PreviousOutput map[string]interface{}
}

// HTTPClientAction HTTP客户端动作
type HTTPClientAction struct {
	ctx *ActionContext
}

// NewHTTPClientAction 创建HTTP客户端动作
func NewHTTPClientAction(ctx *ActionContext) *HTTPClientAction {
	return &HTTPClientAction{ctx: ctx}
}

// Name 返回动作名称
func (a *HTTPClientAction) Name() string {
	return "HTTPClientAction"
}

// TaskContext 任务上下文
type TaskContext struct {
	params map[string]interface{}
	output interface{}
}

// GetParams 获取参数
func (tc *TaskContext) GetParams() map[string]interface{} {
	return tc.params
}

// SetOutput 设置输出
func (tc *TaskContext) SetOutput(output interface{}) {
	tc.output = output
}

// GetOutput 获取输出
func (tc *TaskContext) GetOutput() interface{} {
	return tc.output
}

// Run 执行HTTP请求
func (a *HTTPClientAction) Run(ctx context.Context, taskCtx *TaskContext) error {
	params := taskCtx.GetParams()

	// 解析参数
	url, _ := params["url"].(string)
	method, _ := params["method"].(string)
	headers, _ := params["headers"].(map[string]interface{})
	body, _ := params["body"]
	timeout, _ := params["timeout"].(float64)

	if url == "" {
		return fmt.Errorf("url parameter is required")
	}
	if method == "" {
		method = "GET"
	}
	if timeout == 0 {
		timeout = 30
	}

	// 替换模板变量
	url = a.replaceTemplateVars(url)

	// 准备请求体
	var reqBody io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(bodyBytes)
	}

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// 设置请求头
	for key, value := range headers {
		if strValue, ok := value.(string); ok {
			req.Header.Set(key, a.replaceTemplateVars(strValue))
		}
	}

	// 设置默认Content-Type
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	a.ctx.Logger.Infof("Executing HTTP request: %s %s", method, url)

	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// 如果不是JSON，直接返回字符串
		result = map[string]interface{}{
			"body": string(respBody),
		}
	}

	// 添加响应元数据
	result["status_code"] = resp.StatusCode
	result["headers"] = resp.Header

	// 检查HTTP状态码
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// 保存结果
	taskCtx.SetOutput(result)
	a.ctx.Logger.Infof("HTTP request completed successfully with status %d", resp.StatusCode)

	return nil
}

// DBClientAction 数据库客户端动作
type DBClientAction struct {
	ctx *ActionContext
}

// NewDBClientAction 创建数据库客户端动作
func NewDBClientAction(ctx *ActionContext) *DBClientAction {
	return &DBClientAction{ctx: ctx}
}

// Name 返回动作名称
func (a *DBClientAction) Name() string {
	return "DBClientAction"
}

// Run 执行数据库操作
func (a *DBClientAction) Run(ctx context.Context, taskCtx *TaskContext) error {
	params := taskCtx.GetParams()

	// 解析参数
	dataSourceName, _ := params["datasource"].(string)
	sqlQuery, _ := params["sql"].(string)
	queryParams, _ := params["params"].([]interface{})
	operationType, _ := params["operation"].(string) // query, exec

	if dataSourceName == "" {
		return fmt.Errorf("datasource parameter is required")
	}
	if sqlQuery == "" {
		return fmt.Errorf("sql parameter is required")
	}
	if operationType == "" {
		operationType = "query"
	}

	// 替换模板变量
	sqlQuery = a.replaceTemplateVars(sqlQuery)

	// 获取数据库连接
	db, err := a.ctx.DataSourceMgr.GetSQLDB(dataSourceName)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %v", err)
	}

	a.ctx.Logger.Infof("Executing SQL %s: %s", operationType, sqlQuery)

	var result interface{}

	switch operationType {
	case "query":
		result, err = a.executeQuery(db, sqlQuery, queryParams)
	case "exec":
		result, err = a.executeExec(db, sqlQuery, queryParams)
	default:
		return fmt.Errorf("unsupported operation type: %s", operationType)
	}

	if err != nil {
		return err
	}

	// 保存结果
	taskCtx.SetOutput(result)
	a.ctx.Logger.Infof("SQL %s completed successfully", operationType)

	return nil
}

// executeQuery 执行查询操作
func (a *DBClientAction) executeQuery(db *sql.DB, query string, params []interface{}) (interface{}, error) {
	rows, err := db.Query(query, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	defer rows.Close()

	// 获取列名
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %v", err)
	}

	// 准备结果
	var results []map[string]interface{}

	for rows.Next() {
		// 创建扫描目标
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// 扫描行
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}

		// 构建结果映射
		row := make(map[string]interface{})
		for i, col := range columns {
			if values[i] != nil {
				row[col] = values[i]
			} else {
				row[col] = nil
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %v", err)
	}

	return map[string]interface{}{
		"rows":  results,
		"count": len(results),
	}, nil
}

// executeExec 执行写入操作
func (a *DBClientAction) executeExec(db *sql.DB, query string, params []interface{}) (interface{}, error) {
	result, err := db.Exec(query, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute statement: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertId, _ := result.LastInsertId()

	return map[string]interface{}{
		"rows_affected":  rowsAffected,
		"last_insert_id": lastInsertId,
	}, nil
}

// JSFunctionAction JavaScript函数动作
type JSFunctionAction struct {
	ctx *ActionContext
}

// NewJSFunctionAction 创建JavaScript函数动作
func NewJSFunctionAction(ctx *ActionContext) *JSFunctionAction {
	return &JSFunctionAction{ctx: ctx}
}

// Name 返回动作名称
func (a *JSFunctionAction) Name() string {
	return "JSFunctionAction"
}

// Run 执行JavaScript函数
func (a *JSFunctionAction) Run(ctx context.Context, taskCtx *TaskContext) error {
	params := taskCtx.GetParams()

	// 解析参数
	jsCode, _ := params["code"].(string)
	timeout, _ := params["timeout"].(float64)

	if jsCode == "" {
		return fmt.Errorf("code parameter is required")
	}
	if timeout == 0 {
		timeout = 30
	}

	a.ctx.Logger.Infof("Executing JavaScript function")

	// 创建QuickJS运行时
	rt := quickjs.NewRuntime()
	defer rt.Close()

	ctxJS := rt.NewContext()
	defer ctxJS.Close()

	// 设置全局变量
	if err := a.setGlobalVariables(ctxJS); err != nil {
		return fmt.Errorf("failed to set global variables: %v", err)
	}

	// 执行JavaScript代码
	result, err := ctxJS.Eval(jsCode)
	if err != nil {
		return fmt.Errorf("failed to execute JavaScript: %v", err)
	}
	defer result.Free()

	// 获取结果
	var output interface{}
	if result.IsObject() {
		// 转换为Go对象
		jsonStr := result.JSONStringify()
		if err := json.Unmarshal([]byte(jsonStr), &output); err != nil {
			output = jsonStr
		}
	} else {
		output = result.String()
	}

	// 保存结果
	taskCtx.SetOutput(output)
	a.ctx.Logger.Infof("JavaScript function completed successfully")

	return nil
}

// setGlobalVariables 设置JavaScript全局变量
func (a *JSFunctionAction) setGlobalVariables(ctx *quickjs.Context) error {
	// 设置NSQ消息
	if a.ctx.NSQMessage != nil {
		msgJSON, _ := json.Marshal(a.ctx.NSQMessage)
		msgValue := ctx.ParseJSON(string(msgJSON))
		ctx.Globals().Set("nsq_message", msgValue)
		msgValue.Free()
	}

	// 设置工作流变量
	if a.ctx.WorkflowVars != nil {
		varsJSON, _ := json.Marshal(a.ctx.WorkflowVars)
		varsValue := ctx.ParseJSON(string(varsJSON))
		ctx.Globals().Set("workflow_vars", varsValue)
		varsValue.Free()
	}

	// 设置前置节点输出
	if a.ctx.PreviousOutput != nil {
		outputJSON, _ := json.Marshal(a.ctx.PreviousOutput)
		outputValue := ctx.ParseJSON(string(outputJSON))
		ctx.Globals().Set("previous_output", outputValue)
		outputValue.Free()
	}

	// 添加工具函数
	consoleLog := ctx.Function(func(ctx *quickjs.Context, this quickjs.Value, args []quickjs.Value) quickjs.Value {
		if len(args) > 0 {
			a.ctx.Logger.Info("JS Console:", args[0].String())
		}
		return ctx.Null()
	})
	ctx.Globals().Set("console_log", consoleLog)
	consoleLog.Free()

	return nil
}

// replaceTemplateVars 替换模板变量
func (a *HTTPClientAction) replaceTemplateVars(template string) string {
	// 替换NSQ消息变量
	if a.ctx.NSQMessage != nil {
		for key, value := range a.ctx.NSQMessage.Data {
			placeholder := fmt.Sprintf("{{nsq.%s}}", key)
			if strValue, ok := value.(string); ok {
				template = strings.ReplaceAll(template, placeholder, strValue)
			}
		}
	}

	// 替换工作流变量
	for key, value := range a.ctx.WorkflowVars {
		placeholder := fmt.Sprintf("{{%s}}", key)
		if strValue, ok := value.(string); ok {
			template = strings.ReplaceAll(template, placeholder, strValue)
		}
	}

	// 替换前置节点输出
	for key, value := range a.ctx.PreviousOutput {
		placeholder := fmt.Sprintf("{{output.%s}}", key)
		if strValue, ok := value.(string); ok {
			template = strings.ReplaceAll(template, placeholder, strValue)
		}
	}

	return template
}

// replaceTemplateVars 替换模板变量 (DBClientAction)
func (a *DBClientAction) replaceTemplateVars(template string) string {
	// 替换NSQ消息变量
	if a.ctx.NSQMessage != nil {
		for key, value := range a.ctx.NSQMessage.Data {
			placeholder := fmt.Sprintf("{{nsq.%s}}", key)
			if strValue, ok := value.(string); ok {
				template = strings.ReplaceAll(template, placeholder, strValue)
			}
		}
	}

	// 替换工作流变量
	for key, value := range a.ctx.WorkflowVars {
		placeholder := fmt.Sprintf("{{%s}}", key)
		if strValue, ok := value.(string); ok {
			template = strings.ReplaceAll(template, placeholder, strValue)
		}
	}

	// 替换前置节点输出
	for key, value := range a.ctx.PreviousOutput {
		placeholder := fmt.Sprintf("{{output.%s}}", key)
		if strValue, ok := value.(string); ok {
			template = strings.ReplaceAll(template, placeholder, strValue)
		}
	}

	return template
}
