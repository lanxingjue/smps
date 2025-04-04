// internal/tracer/logger.go
package tracer

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"smps/internal/protocol"
	"smps/pkg/logger"
)

// Direction 消息方向
type Direction string

const (
	// DirectionIncoming 接收方向
	DirectionIncoming Direction = "IN"

	// DirectionOutgoing 发送方向
	DirectionOutgoing Direction = "OUT"
)

// ProtocolLog 协议日志条目
type ProtocolLog struct {
	Timestamp   time.Time
	Direction   Direction
	MessageID   uint32
	CommandName string
	SourceAddr  string
	DestAddr    string
	Content     string
	RawData     []byte
	SystemID    string
	Status      uint32
}

// ProtocolLogger 协议日志记录器
type ProtocolLogger struct {
	config  *TracerConfig
	decoder *ContentDecoder
	storage ProtocolStorage

	// 缓存最近的日志
	recentLogs    []*ProtocolLog
	recentLogsMu  sync.RWMutex
	maxRecentLogs int
}

// NewProtocolLogger 创建新的协议日志记录器
func NewProtocolLogger(config *TracerConfig, storage ProtocolStorage) *ProtocolLogger {
	if storage == nil {
		storage = &MemoryProtocolStorage{
			logs: make([]*ProtocolLog, 0, 1000),
		}
	}

	return &ProtocolLogger{
		config:        config,
		decoder:       NewContentDecoder(),
		storage:       storage,
		recentLogs:    make([]*ProtocolLog, 0, 100),
		maxRecentLogs: 100,
	}
}

// LogProtocol 记录协议消息
func (l *ProtocolLogger) LogProtocol(msg *protocol.Message, direction Direction, systemID string) {
	// 检查是否启用跟踪
	if !l.config.IsEnabled() {
		return
	}

	// 提取号码信息
	sourceAddr, destAddr, content, err := l.extractMessageInfo(msg)
	if err != nil {
		logger.Error(fmt.Sprintf("提取消息信息失败: %v", err))
	}

	// 检查是否需要跟踪此号码
	if !l.shouldTraceMessage(sourceAddr, destAddr) {
		return
	}

	// 创建日志条目
	log := &ProtocolLog{
		Timestamp:   time.Now(),
		Direction:   direction,
		MessageID:   msg.Header.SequenceNumber,
		CommandName: protocol.GetCommandName(msg.Header.CommandID),
		SourceAddr:  sourceAddr,
		DestAddr:    destAddr,
		Content:     content,
		RawData:     msg.Bytes(),
		SystemID:    systemID,
		Status:      msg.Header.CommandStatus,
	}

	// 记录到存储
	if l.config.LogToDatabase {
		if err := l.storage.Store(log); err != nil {
			logger.Error(fmt.Sprintf("存储协议日志失败: %v", err))
		}
	}

	// 添加到最近日志
	l.addToRecentLogs(log)

	// 输出到日志
	if direction == DirectionIncoming {
		logger.Info(fmt.Sprintf("IN <- [%s] Seq=%d, From=%s, To=%s",
			log.CommandName, log.MessageID, log.SourceAddr, log.DestAddr))
	} else {
		logger.Info(fmt.Sprintf("OUT -> [%s] Seq=%d, Status=%d",
			log.CommandName, log.MessageID, log.Status))
	}

	// 如果开启内容解析且有内容，记录内容
	if l.config.IsParseContentEnabled() && content != "" {
		logger.Info(fmt.Sprintf("SMS Content [Seq=%d]: %s", log.MessageID, content))
	}
}

// extractMessageInfo 提取消息信息
func (l *ProtocolLogger) extractMessageInfo(msg *protocol.Message) (sourceAddr, destAddr, content string, err error) {
	// 只处理DELIVER_SM消息
	if msg.Header.CommandID != protocol.DELIVER_SM || len(msg.Payload) < 2 {
		return "", "", "", nil
	}

	// 解析SMPP DELIVER_SM消息
	// 格式: [服务类型\0][源地址TON][源地址NPI][源地址\0][目标地址TON][目标地址NPI][目标地址\0][ESM类][优先级]...

	// 寻找第一个空终止符位置
	serviceTypeEnd := bytes.IndexByte(msg.Payload, 0)
	if serviceTypeEnd < 0 {
		return "", "", "", fmt.Errorf("无效的DELIVER_SM格式: 找不到服务类型终止符")
	}

	// 跳过服务类型、源地址TON和NPI (共3字节)
	sourceAddrStart := serviceTypeEnd + 1 + 2
	if sourceAddrStart >= len(msg.Payload) {
		return "", "", "", fmt.Errorf("无效的DELIVER_SM格式: 负载长度不足")
	}

	// 寻找源地址终止符
	sourceAddrEnd := bytes.IndexByte(msg.Payload[sourceAddrStart:], 0)
	if sourceAddrEnd < 0 {
		return "", "", "", fmt.Errorf("无效的DELIVER_SM格式: 找不到源地址终止符")
	}
	sourceAddrEnd += sourceAddrStart

	// 提取源地址
	sourceAddr = string(msg.Payload[sourceAddrStart:sourceAddrEnd])

	// 跳过源地址终止符、目标地址TON和NPI (共3字节)
	destAddrStart := sourceAddrEnd + 1 + 2
	if destAddrStart >= len(msg.Payload) {
		return sourceAddr, "", "", fmt.Errorf("无效的DELIVER_SM格式: 负载长度不足")
	}

	// 寻找目标地址终止符
	destAddrEnd := bytes.IndexByte(msg.Payload[destAddrStart:], 0)
	if destAddrEnd < 0 {
		return sourceAddr, "", "", fmt.Errorf("无效的DELIVER_SM格式: 找不到目标地址终止符")
	}
	destAddrEnd += destAddrStart

	// 提取目标地址
	destAddr = string(msg.Payload[destAddrStart:destAddrEnd])

	// 跳过各种服务参数，查找短信内容
	// 简化起见，此处假设短信内容在最后，实际SMPP协议中可能需要更复杂的解析
	if l.config.IsParseContentEnabled() && destAddrEnd+10 < len(msg.Payload) {
		// 获取数据编码
		dataCoding := msg.Payload[destAddrEnd+8]

		// 获取消息长度
		messageLength := int(msg.Payload[destAddrEnd+9])

		// 确保有足够的数据
		if destAddrEnd+10+messageLength <= len(msg.Payload) {
			// 提取短信内容
			contentData := msg.Payload[destAddrEnd+10 : destAddrEnd+10+messageLength]

			// 解码短信内容
			content = l.decoder.DecodeContent(contentData, dataCoding)
		}
	}

	return sourceAddr, destAddr, content, nil
}

// shouldTraceMessage 检查是否需要跟踪此消息
func (l *ProtocolLogger) shouldTraceMessage(sourceAddr, destAddr string) bool {
	// 如果没有开启跟踪，直接返回false
	if !l.config.IsEnabled() {
		return false
	}

	// 如果未配置跟踪号码，跟踪所有消息
	if len(l.config.TracedNumbers) == 0 {
		return true
	}

	// 检查源地址和目标地址是否需要跟踪
	if l.config.TraceSourceAddr && l.config.ShouldTrace(sourceAddr) {
		return true
	}

	if l.config.TraceDestAddr && l.config.ShouldTrace(destAddr) {
		return true
	}

	return false
}

// addToRecentLogs 添加到最近日志
func (l *ProtocolLogger) addToRecentLogs(log *ProtocolLog) {
	l.recentLogsMu.Lock()
	defer l.recentLogsMu.Unlock()

	// 添加到最近日志
	l.recentLogs = append(l.recentLogs, log)

	// 如果超过最大数量，移除最旧的
	if len(l.recentLogs) > l.maxRecentLogs {
		l.recentLogs = l.recentLogs[1:]
	}
}

// GetRecentLogs 获取最近日志
func (l *ProtocolLogger) GetRecentLogs() []*ProtocolLog {
	l.recentLogsMu.RLock()
	defer l.recentLogsMu.RUnlock()

	// 复制一份日志
	logs := make([]*ProtocolLog, len(l.recentLogs))
	copy(logs, l.recentLogs)

	return logs
}
