// internal/dispatcher/broadcast.go
package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"smps/internal/protocol"
	"smps/internal/server"
	"smps/pkg/logger"
)

// BroadcastHandler 广播处理器，将消息分发给所有SMMC客户端
type BroadcastHandler struct {
	server        *server.Server
	excludeID     uint64
	maxConcurrent int
	statsInterval time.Duration

	// 统计
	stats struct {
		processed     uint64
		errors        uint64
		broadcastTime int64
	}
}

// NewBroadcastHandler 创建新的广播处理器
func NewBroadcastHandler(server *server.Server, excludeID uint64) *BroadcastHandler {
	return &BroadcastHandler{
		server:        server,
		excludeID:     excludeID,
		maxConcurrent: 5, // 默认最多并发处理5个网元
		statsInterval: 30 * time.Second,
	}
}

// SetMaxConcurrent 设置最大并发度
func (h *BroadcastHandler) SetMaxConcurrent(max int) {
	if max > 0 {
		h.maxConcurrent = max
	}
}

// HandlerName 返回处理器名称
func (h *BroadcastHandler) HandlerName() string {
	return "BroadcastHandler"
}

// Handle 处理消息
func (h *BroadcastHandler) Handle(ctx context.Context, msg *protocol.Message) error {
	startTime := time.Now()

	// 获取所有活跃会话
	sessions := h.server.GetSessionManager().GetActiveSessions()
	if len(sessions) == 0 {
		logger.Info("没有活跃会话，消息不会广播")
		return nil
	}

	logger.Info(fmt.Sprintf("开始广播消息到 %d 个会话, 序列号: %d",
		len(sessions), msg.Header.SequenceNumber))

	// 创建并发信号量
	semaphore := make(chan struct{}, h.maxConcurrent)
	var wg sync.WaitGroup

	// 记录错误
	errorCount := int32(0)

	// 广播消息到所有会话
	for _, session := range sessions {
		// 排除指定会话
		if session.ID == h.excludeID {
			continue
		}

		// 检查会话状态
		if session.Status != protocol.Session_Connected {
			continue
		}

		// 获取信号量
		select {
		case semaphore <- struct{}{}:
			// 获取到信号量，继续处理
		case <-ctx.Done():
			// 上下文取消，退出
			return fmt.Errorf("广播被取消: %v", ctx.Err())
		}

		wg.Add(1)
		go func(s *server.Session) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// 创建消息副本
			newMsg := protocol.CloneMessage(msg)
			if newMsg == nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}

			// 设置新的序列号
			newMsg.Header.SequenceNumber = s.NextSequence()

			// 发送消息
			if err := h.server.SendMessage(s, newMsg); err != nil {
				logger.Error(fmt.Sprintf("向会话 %d 发送消息失败: %v", s.ID, err))
				atomic.AddInt32(&errorCount, 1)
				return
			}

			logger.Debug(fmt.Sprintf("成功向会话 %d (%s) 发送消息", s.ID, s.SystemID))
		}(session)
	}

	// 等待所有广播完成
	wg.Wait()

	// 更新统计信息
	atomic.AddUint64(&h.stats.processed, 1)
	if errorCount > 0 {
		atomic.AddUint64(&h.stats.errors, uint64(errorCount))
	}
	atomic.AddInt64(&h.stats.broadcastTime, time.Since(startTime).Nanoseconds())

	// 定期输出统计信息
	if h.stats.processed%100 == 0 {
		h.logStats()
	}

	// 如果有错误，返回错误信息
	if errorCount > 0 {
		return fmt.Errorf("广播消息时出现 %d 个错误", errorCount)
	}

	return nil
}

// logStats 记录统计信息
func (h *BroadcastHandler) logStats() {
	processed := atomic.LoadUint64(&h.stats.processed)
	errors := atomic.LoadUint64(&h.stats.errors)
	totalTime := atomic.LoadInt64(&h.stats.broadcastTime)

	var avgTime float64
	if processed > 0 {
		avgTime = float64(totalTime) / float64(processed) / float64(time.Millisecond)
	}

	logger.Info(fmt.Sprintf("广播统计: 已处理=%d, 错误=%d, 平均广播时间=%.2fms",
		processed, errors, avgTime))
}
