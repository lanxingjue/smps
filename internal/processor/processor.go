// internal/processor/processor.go
package processor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"smps/internal/protocol"
	"smps/pkg/logger"
)

// ResponseConfig 响应处理器配置
type ResponseConfig struct {
	MessageTimeout  time.Duration // 消息处理超时
	CleanupInterval time.Duration // 清理间隔
	MaxTrackers     int           // 最大跟踪器数量
}

// DefaultResponseConfig 默认配置
func DefaultResponseConfig() *ResponseConfig {
	return &ResponseConfig{
		MessageTimeout:  2 * time.Second,  // 2秒默认超时
		CleanupInterval: 10 * time.Second, // 10秒清理一次
		MaxTrackers:     10000,            // 最多同时跟踪10000条消息
	}
}

// ResponseProcessor 响应处理器
type ResponseProcessor struct {
	config   *ResponseConfig
	trackers map[string]*ResponseTracker
	mutex    sync.RWMutex
	done     chan struct{}

	// 统计信息
	stats struct {
		messagesProcessed uint64
		interceptCount    uint64
		allowCount        uint64
		timeoutCount      uint64
	}
}

// NewResponseProcessor 创建响应处理器
func NewResponseProcessor(config *ResponseConfig) *ResponseProcessor {
	if config == nil {
		config = DefaultResponseConfig()
	}

	return &ResponseProcessor{
		config:   config,
		trackers: make(map[string]*ResponseTracker),
		done:     make(chan struct{}),
	}
}

// Start 启动处理器
func (p *ResponseProcessor) Start(ctx context.Context) error {
	// 启动清理协程
	go p.cleanupLoop(ctx)

	logger.Info(fmt.Sprintf("响应处理器已启动, 超时=%v, 清理间隔=%v",
		p.config.MessageTimeout, p.config.CleanupInterval))

	return nil
}

// cleanupLoop 定期清理过期跟踪器
func (p *ResponseProcessor) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanup()
		case <-ctx.Done():
			return
		}
	}
}

// cleanup 清理过期跟踪器
func (p *ResponseProcessor) cleanup() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	expireTime := time.Now().Add(-p.config.MessageTimeout * 5) // 保留一段时间方便调试
	var expiredCount int

	for id, tracker := range p.trackers {
		// 如果跟踪器已完成且足够老，或者显著超时，则删除
		if (tracker.IsDone() && tracker.createTime.Before(expireTime)) ||
			tracker.GetAge() > p.config.MessageTimeout*10 {
			delete(p.trackers, id)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		logger.Debug(fmt.Sprintf("清理了 %d 个过期跟踪器, 当前剩余 %d 个",
			expiredCount, len(p.trackers)))
	}
}

// TrackMessage 开始跟踪消息
func (p *ResponseProcessor) TrackMessage(messageID string, sources []string) (*ResponseTracker, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 检查是否超过最大跟踪器数量
	if len(p.trackers) >= p.config.MaxTrackers {
		return nil, fmt.Errorf("超过最大跟踪器数量限制 (%d)", p.config.MaxTrackers)
	}

	// 检查是否已存在
	if _, exists := p.trackers[messageID]; exists {
		return nil, fmt.Errorf("消息 ID %s 已在跟踪中", messageID)
	}

	// 创建新跟踪器
	tracker := NewResponseTracker(messageID, sources, p.config.MessageTimeout)
	p.trackers[messageID] = tracker

	return tracker, nil
}

// ProcessResponse 处理响应
func (p *ResponseProcessor) ProcessResponse(response *protocol.Message) (*Decision, error) {
	// 解析响应消息
	messageID := fmt.Sprintf("%d", response.Header.SequenceNumber)
	sourceID := "unknown"

	// 解析决策动作类型
	var action ActionType
	if response.Header.CommandStatus == protocol.SM_OK {
		action = ActionAllow
	} else if response.Header.CommandStatus == protocol.SM_REJECT {
		action = ActionIntercept
	} else {
		action = ActionUnknown
	}

	// 创建决策
	decision := NewDecision(messageID, action, sourceID, "外部网元响应")

	// 查找对应的跟踪器
	p.mutex.RLock()
	tracker, exists := p.trackers[messageID]
	p.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("找不到消息 ID %s 的跟踪器", messageID)
	}

	// 添加决策
	if added := tracker.AddDecision(decision); !added {
		return nil, fmt.Errorf("无法添加决策，跟踪器可能已完成")
	}

	// 如果跟踪器已完成，返回最终决策
	if tracker.IsDone() {
		return tracker.finalDecision, nil
	}

	return nil, nil
}

// GetDecision 获取消息的决策
func (p *ResponseProcessor) GetDecision(messageID string, wait bool) (*Decision, error) {
	// 查找对应的跟踪器
	p.mutex.RLock()
	tracker, exists := p.trackers[messageID]
	p.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("找不到消息 ID %s 的跟踪器", messageID)
	}

	// 如果不等待并且跟踪器未完成，返回错误
	if !wait && !tracker.IsDone() {
		return nil, fmt.Errorf("消息 ID %s 的决策尚未完成", messageID)
	}

	// 等待完成
	return tracker.WaitForCompletion(), nil
}

// UpdateStats 更新统计信息
func (p *ResponseProcessor) updateStats(decision *Decision) {
	if decision.Action == ActionIntercept {
		p.stats.interceptCount++
	} else if decision.Action == ActionAllow {
		p.stats.allowCount++
	}

	if decision.IsTimeout {
		p.stats.timeoutCount++
	}

	p.stats.messagesProcessed++
}

// GetStats 获取统计信息
func (p *ResponseProcessor) GetStats() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return map[string]interface{}{
		"messages_processed": p.stats.messagesProcessed,
		"intercept_count":    p.stats.interceptCount,
		"allow_count":        p.stats.allowCount,
		"timeout_count":      p.stats.timeoutCount,
		"active_trackers":    len(p.trackers),
	}
}

// Stop 停止处理器
func (p *ResponseProcessor) Stop() {
	close(p.done)
}

// 添加Handle方法
func (p *ResponseProcessor) Handle(ctx context.Context, msg *protocol.Message) error {
	return p.HandleMessage(ctx, msg)
}

// 添加HandleMessage方法
func (p *ResponseProcessor) HandleMessage(ctx context.Context, msg *protocol.Message) error {
	// 示例实现：处理消息并记录日志
	logger.Info(fmt.Sprintf("处理消息: %v", msg))
	decision, err := p.ProcessResponse(msg)
	if err != nil {
		logger.Error(fmt.Sprintf("处理消息时出错: %v", err))
		return err
	}

	logger.Info(fmt.Sprintf("决策结果: %v", decision))
	return nil
}

// 如果没有HandlerName方法，也需要添加
func (p *ResponseProcessor) HandlerName() string {
	return "响应处理器"
}
