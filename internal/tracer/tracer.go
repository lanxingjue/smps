// internal/tracer/tracer.go
package tracer

import (
	"database/sql"
	"fmt"

	"smps/internal/protocol"
	"smps/pkg/logger"
)

// ProtocolTracer 协议跟踪器
type ProtocolTracer struct {
	config *TracerConfig
	logger *ProtocolLogger
	db     *sql.DB
}

// NewProtocolTracer 创建新的协议跟踪器
func NewProtocolTracer(config *TracerConfig, db *sql.DB) *ProtocolTracer {
	var storage ProtocolStorage

	if db != nil && config.LogToDatabase {
		storage = NewDatabaseProtocolStorage(db)
	} else {
		storage = &MemoryProtocolStorage{
			logs: make([]*ProtocolLog, 0, 1000),
		}
	}

	return &ProtocolTracer{
		config: config,
		logger: NewProtocolLogger(config, storage),
		db:     db,
	}
}

// TraceIncoming 跟踪接收的消息
func (t *ProtocolTracer) TraceIncoming(msg *protocol.Message, systemID string) {
	// 检查是否启用跟踪
	if !t.config.IsEnabled() {
		return
	}

	t.logger.LogProtocol(msg, DirectionIncoming, systemID)
}

// TraceOutgoing 跟踪发送的消息
func (t *ProtocolTracer) TraceOutgoing(msg *protocol.Message, systemID string) {
	// 检查是否启用跟踪
	if !t.config.IsEnabled() {
		return
	}

	t.logger.LogProtocol(msg, DirectionOutgoing, systemID)
}

// AddTracedNumber 添加跟踪号码
func (t *ProtocolTracer) AddTracedNumber(number string) {
	t.config.AddTracedNumber(number)
	logger.Info(fmt.Sprintf("添加跟踪号码: %s", number))
}

// RemoveTracedNumber 移除跟踪号码
func (t *ProtocolTracer) RemoveTracedNumber(number string) {
	t.config.RemoveTracedNumber(number)
	logger.Info(fmt.Sprintf("移除跟踪号码: %s", number))
}

// SetParseContent 设置是否解析短信内容
func (t *ProtocolTracer) SetParseContent(parse bool) {
	t.config.SetParseContent(parse)
	logger.Info(fmt.Sprintf("设置内容解析: %v", parse))
}

// SetEnabled 设置是否启用跟踪
func (t *ProtocolTracer) SetEnabled(enabled bool) {
	t.config.SetEnabled(enabled)
	logger.Info(fmt.Sprintf("设置跟踪状态: %v", enabled))
}

// QueryLogs 查询日志
func (t *ProtocolTracer) QueryLogs(options QueryOptions) ([]*ProtocolLog, error) {
	return t.logger.storage.Query(options)
}

// GetRecentLogs 获取最近日志
func (t *ProtocolTracer) GetRecentLogs() []*ProtocolLog {
	return t.logger.GetRecentLogs()
}

// ClearLogs 清除日志
func (t *ProtocolTracer) ClearLogs() error {
	return t.logger.storage.Clear()
}

// IsEnabled 检查跟踪是否启用
func (t *ProtocolTracer) IsEnabled() bool {
	return t.config.IsEnabled()
}

// IsParseContentEnabled 检查内容解析是否启用
func (t *ProtocolTracer) IsParseContentEnabled() bool {
	return t.config.IsParseContentEnabled()
}
