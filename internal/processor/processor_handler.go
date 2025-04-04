// internal/processor/processor_handler.go
package processor

import (
	"context"
	"fmt"
	"sync/atomic"

	"smps/internal/protocol"
	"smps/internal/server"
	"smps/pkg/logger"
)

// ProcessorHandler 将响应处理器集成到分发流程中
type ProcessorHandler struct {
	processor *ResponseProcessor
	server    *server.Server

	// 统计
	processedCount uint64
}

// NewProcessorHandler 创建处理器处理器
func NewProcessorHandler(processor *ResponseProcessor, server *server.Server) *ProcessorHandler {
	return &ProcessorHandler{
		processor: processor,
		server:    server,
	}
}

// HandlerName 返回处理器名称
func (h *ProcessorHandler) HandlerName() string {
	return "ProcessorHandler"
}

// Handle 处理消息
func (h *ProcessorHandler) Handle(ctx context.Context, msg *protocol.Message) error {
	// 只处理DELIVER_SM消息
	if msg.Header.CommandID != protocol.DELIVER_SM {
		return nil
	}

	messageID := fmt.Sprintf("%d", msg.Header.SequenceNumber)

	// 获取活跃会话列表
	sessions := h.server.GetSessionManager().GetActiveSessions()
	if len(sessions) == 0 {
		// 没有活跃会话，直接放行
		logger.Info(fmt.Sprintf("消息[%s]: 没有活跃会话，直接放行", messageID))
		return nil
	}

	// 提取所有会话ID
	var sources []string
	for _, session := range sessions {
		sources = append(sources, session.SystemID)
	}

	// 开始跟踪消息
	tracker, err := h.processor.TrackMessage(messageID, sources)
	if err != nil {
		logger.Error(fmt.Sprintf("跟踪消息[%s]失败: %v", messageID, err))
		return err
	}

	// 在后台等待决策
	go func() {
		// 等待决策完成
		decision := tracker.WaitForCompletion()

		// 更新计数
		atomic.AddUint64(&h.processedCount, 1)

		// 记录最终决策
		logger.Info(fmt.Sprintf("最终决策: %s", decision.String()))

		// 返回决策给SMSC
		response := createResponseByDecision(msg, decision)
		if err := sendResponseToSMSC(response); err != nil {
			logger.Error(fmt.Sprintf("发送响应失败: %v", err))
		}
	}()

	return nil
}

// 根据决策创建响应
func createResponseByDecision(msg *protocol.Message, decision *Decision) *protocol.Message {
	var status uint32
	if decision.Action == ActionIntercept {
		status = protocol.SM_REJECT
	} else {
		status = protocol.SM_OK
	}

	return protocol.CreateDeliverSMResponse(msg.Header.SequenceNumber, status)
}

// 发送响应到SMSC
func sendResponseToSMSC(response *protocol.Message) error {
	// 这里应该集成实际的SMSC客户端来发送响应
	// 在实际实现中，应该调用SMSC客户端的发送方法
	// 例如: smscClient.Send(response)

	// 这里仅作为示例，记录响应信息
	logger.Info(fmt.Sprintf("模拟发送响应到SMSC: 序列号=%d, 状态=%d",
		response.Header.SequenceNumber, response.Header.CommandStatus))

	return nil
}
