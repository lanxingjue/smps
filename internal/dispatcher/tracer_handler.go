// internal/dispatcher/tracer_handler.go
package dispatcher

import (
	"context"

	"smps/internal/protocol"
	"smps/internal/tracer"
)

// TracerHandler 集成跟踪器到分发流程
type TracerHandler struct {
	tracer *tracer.ProtocolTracer
}

// NewTracerHandler 创建跟踪器处理器
func NewTracerHandler(tracer *tracer.ProtocolTracer) *TracerHandler {
	return &TracerHandler{
		tracer: tracer,
	}
}

// HandlerName 返回处理器名称
func (h *TracerHandler) HandlerName() string {
	return "TracerHandler"
}

// Handle 处理消息
func (h *TracerHandler) Handle(ctx context.Context, msg *protocol.Message) error {
	// 跟踪接收的消息
	h.tracer.TraceIncoming(msg, "SMSC")
	return nil
}
