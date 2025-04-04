// internal/dispatcher/logger.go
package dispatcher

import (
	"context"
	"fmt"

	"smps/internal/protocol"
	"smps/pkg/logger"
)

// LoggingHandler 日志处理器，记录经过的所有消息
type LoggingHandler struct {
	logMessageContent bool
}

// NewLoggingHandler 创建新的日志处理器
func NewLoggingHandler() *LoggingHandler {
	return &LoggingHandler{
		logMessageContent: true,
	}
}

// SetLogMessageContent 设置是否记录消息内容
func (h *LoggingHandler) SetLogMessageContent(enable bool) {
	h.logMessageContent = enable
}

// HandlerName 返回处理器名称
func (h *LoggingHandler) HandlerName() string {
	return "LoggingHandler"
}

// Handle 处理消息
func (h *LoggingHandler) Handle(ctx context.Context, msg *protocol.Message) error {
	commandName := protocol.GetCommandName(msg.Header.CommandID)

	if h.logMessageContent {
		// 解析短信内容
		sourceAddr, destAddr, content, err := protocol.ParseMessageContent(msg)
		if err != nil {
			logger.Info(fmt.Sprintf("消息日志 [命令=%s, 序列号=%d]: 解析内容失败: %v",
				commandName, msg.Header.SequenceNumber, err))
		} else {
			logger.Info(fmt.Sprintf("消息日志 [命令=%s, 序列号=%d]:\n 发送方: %s\n 接收方: %s\n 内容: %s",
				commandName, msg.Header.SequenceNumber, sourceAddr, destAddr, content))
		}
	} else {
		logger.Info(fmt.Sprintf("消息日志 [命令=%s, 序列号=%d, 长度=%d]",
			commandName, msg.Header.SequenceNumber, msg.Header.CommandLength))
	}

	return nil
}
