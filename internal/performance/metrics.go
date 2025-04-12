// internal/performance/metrics.go
package performance

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rcrowley/go-metrics"

	"smps/pkg/logger"
)

// MetricsRegistry 指标注册表
var MetricsRegistry = metrics.NewRegistry()

// Metrics 性能指标收集器
type Metrics struct {
	// 消息处理总数
	MessageCount metrics.Counter

	// 消息处理速率
	MessageRate metrics.Meter

	// 消息处理延迟
	MessageLatency metrics.Timer

	// 消息处理错误数
	ErrorCount metrics.Counter

	// 消息处理错误率
	ErrorRate metrics.Meter

	// 活跃连接数
	ActiveConnections metrics.Gauge

	// 并发处理消息数
	ConcurrentMessages int64

	// 启动时间
	StartTime time.Time
}

// 全局指标实例
var (
	globalMetrics     *Metrics
	globalMetricsOnce sync.Once
)

// GetMetrics 获取全局指标实例
func GetMetrics() *Metrics {
	globalMetricsOnce.Do(func() {
		globalMetrics = NewMetrics()
	})
	return globalMetrics
}

// NewMetrics 创建性能指标收集器
func NewMetrics() *Metrics {
	m := &Metrics{
		MessageCount:      metrics.NewCounter(),
		MessageRate:       metrics.NewMeter(),
		MessageLatency:    metrics.NewTimer(),
		ErrorCount:        metrics.NewCounter(),
		ErrorRate:         metrics.NewMeter(),
		ActiveConnections: metrics.NewGauge(),
		StartTime:         time.Now(),
	}

	// 注册指标
	MetricsRegistry.Register("messages.count", m.MessageCount)
	MetricsRegistry.Register("messages.rate", m.MessageRate)
	MetricsRegistry.Register("messages.latency", m.MessageLatency)
	MetricsRegistry.Register("errors.count", m.ErrorCount)
	MetricsRegistry.Register("errors.rate", m.ErrorRate)
	MetricsRegistry.Register("connections.active", m.ActiveConnections)

	return m
}

// RecordMessage 记录消息处理
func (m *Metrics) RecordMessage(startTime time.Time) {
	m.MessageCount.Inc(1)
	m.MessageRate.Mark(1)
	m.MessageLatency.UpdateSince(startTime)
	atomic.AddInt64(&m.ConcurrentMessages, -1)
}

// BeginMessageProcessing 开始消息处理
func (m *Metrics) BeginMessageProcessing() time.Time {
	atomic.AddInt64(&m.ConcurrentMessages, 1)
	return time.Now()
}

// RecordError 记录错误
func (m *Metrics) RecordError() {
	m.ErrorCount.Inc(1)
	m.ErrorRate.Mark(1)
}

// SetActiveConnections 设置活跃连接数
func (m *Metrics) SetActiveConnections(count int64) {
	m.ActiveConnections.Update(count)
}

// GetConcurrentMessages 获取当前并发处理消息数
func (m *Metrics) GetConcurrentMessages() int64 {
	return atomic.LoadInt64(&m.ConcurrentMessages)
}

// StartMetricsReporter 启动指标报告器
func StartMetricsReporter(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			LogMetrics()
		}
	}()
}

// LogMetrics 记录指标日志
func LogMetrics() {
	m := GetMetrics()

	// 获取系统运行时间
	uptime := time.Since(m.StartTime).Truncate(time.Second)

	// 输出关键指标
	logger.Info(fmt.Sprintf("--- 性能指标 (运行时间: %s) ---", uptime))
	logger.Info(fmt.Sprintf("消息总数: %d, 消息速率: %.2f/s",
		m.MessageCount.Count(), m.MessageRate.Rate1()))
	logger.Info(fmt.Sprintf("平均延迟: %.2fms, 延迟99分位: %.2fms",
		float64(m.MessageLatency.Mean())/float64(time.Millisecond),
		float64(m.MessageLatency.Percentile(0.99))/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("错误总数: %d, 错误率: %.2f/s",
		m.ErrorCount.Count(), m.ErrorRate.Rate1()))
	logger.Info(fmt.Sprintf("活跃连接数: %d, 并发处理消息数: %d",
		m.ActiveConnections.Value(), m.GetConcurrentMessages()))
}

// MetricsMiddleware 中间件函数，用于记录性能指标
type MetricsMiddleware struct {
	metrics *Metrics
}

// NewMetricsMiddleware 创建指标中间件
func NewMetricsMiddleware() *MetricsMiddleware {
	return &MetricsMiddleware{
		metrics: GetMetrics(),
	}
}

// Process 处理消息并记录指标
func (m *MetricsMiddleware) Process(next func(interface{}) error) func(interface{}) error {
	return func(msg interface{}) error {
		// 记录开始时间
		startTime := m.metrics.BeginMessageProcessing()

		// 调用下一个处理器
		err := next(msg)

		// 记录指标
		m.metrics.RecordMessage(startTime)

		// 如果有错误，记录错误指标
		if err != nil {
			m.metrics.RecordError()
		}

		return err
	}
}
