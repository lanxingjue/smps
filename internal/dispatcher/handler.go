// internal/dispatcher/handler.go  处理器接口
package dispatcher

import (
	"context"
	"smps/internal/protocol"
	"time"
)

// MessageHandler 定义消息处理器接口
type MessageHandler interface {
	// Handle 处理消息
	Handle(ctx context.Context, msg *protocol.Message) error

	// HandlerName 返回处理器名称，用于日志和调试
	HandlerName() string
}

// MessageProcessor 定义消息处理流程接口
type MessageProcessor interface {
	// Process 处理消息，返回处理结果
	Process(ctx context.Context, msg *protocol.Message) (*ProcessResult, error)
}

// ProcessResult 定义消息处理结果
type ProcessResult struct {
	Action    string            // 处理动作：拦截/放行
	Message   *protocol.Message // 处理后的消息
	Timestamp int64             // 处理时间戳
	Handler   string            // 处理器名称
}

// NewProcessResult 创建处理结果
func NewProcessResult(action string, msg *protocol.Message, handler string) *ProcessResult {
	return &ProcessResult{
		Action:    action,
		Message:   msg,
		Timestamp: time.Now().UnixNano(),
		Handler:   handler,
	}
}
