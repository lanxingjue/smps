// internal/processor/decision.go
package processor

import (
	"fmt"
	"time"
)

// ActionType 定义处理动作类型
type ActionType string

const (
	// ActionAllow 允许消息通过
	ActionAllow ActionType = "ALLOW"

	// ActionIntercept 拦截消息
	ActionIntercept ActionType = "INTERCEPT"

	// ActionUnknown 未知动作
	ActionUnknown ActionType = "UNKNOWN"
)

// DecisionSource 决策来源
type DecisionSource struct {
	ID        string    // 来源标识（通常是外部网元ID）
	Timestamp time.Time // 决策时间
	IP        string    // 来源IP
}

// Decision 表示短信处理决策
type Decision struct {
	MessageID string         // 消息ID
	Action    ActionType     // 处理动作
	Source    DecisionSource // 决策来源
	Reason    string         // 决策原因
	IsTimeout bool           // 是否因超时产生的决策
}

// NewDecision 创建决策
func NewDecision(messageID string, action ActionType, sourceID string, reason string) *Decision {
	return &Decision{
		MessageID: messageID,
		Action:    action,
		Source: DecisionSource{
			ID:        sourceID,
			Timestamp: time.Now(),
		},
		Reason:    reason,
		IsTimeout: false,
	}
}

// NewTimeoutDecision 创建超时决策
func NewTimeoutDecision(messageID string) *Decision {
	return &Decision{
		MessageID: messageID,
		Action:    ActionAllow,
		Source: DecisionSource{
			ID:        "system",
			Timestamp: time.Now(),
		},
		Reason:    "处理超时，默认放行",
		IsTimeout: true,
	}
}

// String 输出决策信息
func (d *Decision) String() string {
	return fmt.Sprintf("消息[%s]: 动作=%s, 来源=%s, 时间=%s, 原因=%s, 超时=%v",
		d.MessageID, d.Action, d.Source.ID,
		d.Source.Timestamp.Format("15:04:05.000"),
		d.Reason, d.IsTimeout)
}
