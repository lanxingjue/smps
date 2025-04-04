// internal/processor/tracker.go
package processor

import (
	"fmt"
	"sync"
	"time"
)

// ResponseTracker 消息响应跟踪器
type ResponseTracker struct {
	messageID       string          // 消息ID
	createTime      time.Time       // 创建时间
	timeout         time.Duration   // 超时时间
	decisions       []*Decision     // 收到的决策
	expectedSources map[string]bool // 期望的响应来源
	finalDecision   *Decision       // 最终决策
	done            bool            // 是否已完成
	mutex           sync.Mutex      // 互斥锁
	completeChan    chan *Decision  // 完成通知通道
}

// NewResponseTracker 创建响应跟踪器
func NewResponseTracker(messageID string, sources []string, timeout time.Duration) *ResponseTracker {
	expectedSources := make(map[string]bool)
	for _, source := range sources {
		expectedSources[source] = false
	}

	return &ResponseTracker{
		messageID:       messageID,
		createTime:      time.Now(),
		timeout:         timeout,
		decisions:       make([]*Decision, 0, len(sources)),
		expectedSources: expectedSources,
		done:            false,
		completeChan:    make(chan *Decision, 1), // 使用缓冲通道
	}
}

// AddDecision 添加决策
func (t *ResponseTracker) AddDecision(decision *Decision) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// 如果已经完成，不再接受新决策
	if t.done {
		return false
	}

	// 检查来源是否在预期列表中
	if _, exists := t.expectedSources[decision.Source.ID]; !exists {
		return false
	}

	// 标记来源已响应
	t.expectedSources[decision.Source.ID] = true

	// 添加决策
	t.decisions = append(t.decisions, decision)

	// 检查是否所有来源都已响应
	allResponded := true
	for _, responded := range t.expectedSources {
		if !responded {
			allResponded = false
			break
		}
	}

	// 如果所有来源都已响应或收到拦截决策，标记为完成
	if allResponded || decision.Action == ActionIntercept {
		t.complete(ApplyDecisionRules(t.decisions))
	}

	return true
}

// WaitForCompletion 等待完成或超时
func (t *ResponseTracker) WaitForCompletion() *Decision {
	timer := time.NewTimer(t.timeout)
	defer timer.Stop()

	select {
	case decision := <-t.completeChan:
		return decision
	case <-timer.C:
		// 超时处理
		t.mutex.Lock()
		defer t.mutex.Unlock()

		if !t.done {
			timeoutDecision := NewTimeoutDecision(t.messageID)
			t.complete(timeoutDecision)
		}

		return t.finalDecision
	}
}

// complete 完成跟踪
func (t *ResponseTracker) complete(decision *Decision) {
	t.done = true
	t.finalDecision = decision

	// 通知等待者
	select {
	case t.completeChan <- decision:
		// 成功发送通知
	default:
		// 通道已满（不应该发生）
	}
}

// IsDone 检查是否已完成
func (t *ResponseTracker) IsDone() bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.done
}

// GetAge 获取存在时间
func (t *ResponseTracker) GetAge() time.Duration {
	return time.Since(t.createTime)
}

// String 返回跟踪器状态字符串
func (t *ResponseTracker) String() string {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	status := "进行中"
	if t.done {
		status = "已完成"
	}

	numResponded := 0
	for _, responded := range t.expectedSources {
		if responded {
			numResponded++
		}
	}

	return fmt.Sprintf("消息[%s]: 状态=%s, 已响应=%d/%d, 年龄=%v",
		t.messageID, status, numResponded,
		len(t.expectedSources), t.GetAge())
}
