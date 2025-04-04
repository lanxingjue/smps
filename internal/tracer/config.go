// internal/tracer/config.go
package tracer

import (
	"sync"
)

// TracerConfig 跟踪器配置
type TracerConfig struct {
	// 是否启用跟踪
	Enabled bool

	// 是否解析短信内容
	ParseContent bool

	// 跟踪号码配置
	TracedNumbers map[string]bool

	// 跟踪源地址（发送方号码）
	TraceSourceAddr bool

	// 跟踪目标地址（接收方号码）
	TraceDestAddr bool

	// 记录协议信息到数据库
	LogToDatabase bool

	// 互斥锁，保护配置修改
	mu sync.RWMutex
}

// NewTracerConfig 创建跟踪器默认配置
func NewTracerConfig() *TracerConfig {
	return &TracerConfig{
		Enabled:         true,
		ParseContent:    true,
		TracedNumbers:   make(map[string]bool),
		TraceSourceAddr: true,
		TraceDestAddr:   true,
		LogToDatabase:   false,
	}
}

// AddTracedNumber 添加跟踪号码
func (c *TracerConfig) AddTracedNumber(number string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.TracedNumbers[number] = true
}

// RemoveTracedNumber 移除跟踪号码
func (c *TracerConfig) RemoveTracedNumber(number string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.TracedNumbers, number)
}

// ShouldTrace 是否需要跟踪指定号码
func (c *TracerConfig) ShouldTrace(number string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 如果未启用跟踪，直接返回false
	if !c.Enabled {
		return false
	}

	// 如果跟踪号码列表为空，跟踪所有号码
	if len(c.TracedNumbers) == 0 {
		return true
	}

	// 检查号码是否在跟踪列表中
	_, ok := c.TracedNumbers[number]
	return ok
}

// SetParseContent 设置是否解析短信内容
func (c *TracerConfig) SetParseContent(parse bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ParseContent = parse
}

// IsParseContentEnabled 检查是否启用内容解析
func (c *TracerConfig) IsParseContentEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.ParseContent
}

// SetEnabled 设置是否启用跟踪
func (c *TracerConfig) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Enabled = enabled
}

// IsEnabled 检查跟踪是否启用
func (c *TracerConfig) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Enabled
}
