// internal/performance/limiter.go
package performance

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/time/rate"

	"smps/pkg/logger"
)

// RateLimiter 速率限制器
type RateLimiter struct {
	// 限流器映射
	limiters map[string]*rate.Limiter

	// 默认限流器
	defaultLimiter *rate.Limiter

	// 互斥锁
	mu sync.RWMutex

	// 启用状态
	enabled bool
}

// NewRateLimiter 创建速率限制器
func NewRateLimiter(defaultRPS float64) *RateLimiter {
	return &RateLimiter{
		limiters:       make(map[string]*rate.Limiter),
		defaultLimiter: rate.NewLimiter(rate.Limit(defaultRPS), int(defaultRPS)),
		enabled:        true,
	}
}

// SetClientLimit 设置客户端限制
func (r *RateLimiter) SetClientLimit(clientID string, rps float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.limiters[clientID] = rate.NewLimiter(rate.Limit(rps), int(rps))
	logger.Info(fmt.Sprintf("为客户端 %s 设置限流: %.2f RPS", clientID, rps))
}

// RemoveClientLimit 移除客户端限制
func (r *RateLimiter) RemoveClientLimit(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.limiters, clientID)
	logger.Info(fmt.Sprintf("移除客户端 %s 的限流设置", clientID))
}

// Allow 检查是否允许请求
func (r *RateLimiter) Allow(clientID string) bool {
	if !r.enabled {
		return true
	}

	r.mu.RLock()
	limiter, exists := r.limiters[clientID]
	r.mu.RUnlock()

	if !exists {
		return r.defaultLimiter.Allow()
	}

	return limiter.Allow()
}

// Wait 等待直到允许请求
func (r *RateLimiter) Wait(ctx context.Context, clientID string) error {
	if !r.enabled {
		return nil
	}

	r.mu.RLock()
	limiter, exists := r.limiters[clientID]
	r.mu.RUnlock()

	if !exists {
		return r.defaultLimiter.Wait(ctx)
	}

	return limiter.Wait(ctx)
}

// SetEnabled 设置启用状态
func (r *RateLimiter) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.enabled = enabled
	logger.Info(fmt.Sprintf("速率限制器状态设置为: %v", enabled))
}

// IsEnabled 检查是否启用
func (r *RateLimiter) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.enabled
}

// 全局速率限制器
var (
	globalLimiter     *RateLimiter
	globalLimiterOnce sync.Once
)

// GetRateLimiter 获取全局速率限制器
func GetRateLimiter() *RateLimiter {
	globalLimiterOnce.Do(func() {
		globalLimiter = NewRateLimiter(1000) // 默认每秒1000请求
	})
	return globalLimiter
}
