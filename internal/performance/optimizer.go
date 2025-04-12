// internal/performance/optimizer.go
package performance

import (
	"fmt"
	"runtime"
	"sort"
	"time"

	"smps/pkg/logger"
)

// Optimizer 性能优化建议生成器
type Optimizer struct {
	metrics   *Metrics
	profiler  *Profiler
	limiters  map[string]*RateLimiter
	threshold struct {
		highLatency       time.Duration
		highErrorRate     float64
		highConcurrency   int64
		highCPUUsage      float64
		highMemoryUsage   float64
		lowConnectionRate float64
	}
}

// NewOptimizer 创建优化建议生成器
func NewOptimizer(metrics *Metrics, profiler *Profiler) *Optimizer {
	opt := &Optimizer{
		metrics:  metrics,
		profiler: profiler,
		limiters: make(map[string]*RateLimiter),
	}

	// 设置默认阈值
	opt.threshold.highLatency = 100 * time.Millisecond
	opt.threshold.highErrorRate = 0.01 // 1%
	opt.threshold.highConcurrency = int64(runtime.NumCPU() * 100)
	opt.threshold.highCPUUsage = 80.0
	opt.threshold.highMemoryUsage = 80.0
	opt.threshold.lowConnectionRate = 0.5

	return opt
}

// GenerateOptimizations 生成优化建议
func (o *Optimizer) GenerateOptimizations() []string {
	var suggestions []string

	// 根据指标生成建议
	suggestions = append(suggestions, o.analyzeLatency()...)
	suggestions = append(suggestions, o.analyzeErrorRate()...)
	suggestions = append(suggestions, o.analyzeConcurrency()...)
	suggestions = append(suggestions, o.analyzeResourceUsage()...)
	suggestions = append(suggestions, o.analyzeConnectionRate()...)

	return suggestions
}

// analyzeLatency 分析延迟
func (o *Optimizer) analyzeLatency() []string {
	var suggestions []string

	// 获取延迟指标
	avgLatency := o.metrics.MessageLatency.Mean()
	p95Latency := o.metrics.MessageLatency.Percentile(0.95)
	p99Latency := o.metrics.MessageLatency.Percentile(0.99)

	if time.Duration(avgLatency) > o.threshold.highLatency {
		suggestions = append(suggestions, fmt.Sprintf(
			"平均延迟(%.2fms)超过阈值(%.2fms)，考虑优化消息处理流程",
			float64(avgLatency)/float64(time.Millisecond),
			float64(o.threshold.highLatency)/float64(time.Millisecond)))
	}

	// 检查P95与平均延迟的比值，如果过高，可能存在长尾问题
	if float64(p95Latency)/float64(avgLatency) > 3.0 {
		suggestions = append(suggestions, fmt.Sprintf(
			"P95延迟(%.2fms)与平均延迟(%.2fms)比值过高，存在长尾延迟问题，考虑检查GC暂停或外部依赖",
			float64(p95Latency)/float64(time.Millisecond),
			float64(avgLatency)/float64(time.Millisecond)))
	}

	// 检查P99与P95的比值，如果过高，可能存在极端情况
	if float64(p99Latency)/float64(p95Latency) > 2.0 {
		suggestions = append(suggestions, fmt.Sprintf(
			"P99延迟(%.2fms)与P95延迟(%.2fms)比值过高，存在极端延迟问题，考虑增加超时处理",
			float64(p99Latency)/float64(time.Millisecond),
			float64(p95Latency)/float64(time.Millisecond)))
	}

	return suggestions
}

// analyzeErrorRate 分析错误率
func (o *Optimizer) analyzeErrorRate() []string {
	var suggestions []string

	// 计算错误率
	messageCount := o.metrics.MessageCount.Count()
	errorCount := o.metrics.ErrorCount.Count()

	if messageCount == 0 {
		return suggestions
	}

	errorRate := float64(errorCount) / float64(messageCount)

	if errorRate > o.threshold.highErrorRate {
		suggestions = append(suggestions, fmt.Sprintf(
			"错误率(%.2f%%)超过阈值(%.2f%%)，需要检查错误日志并修复潜在问题",
			errorRate*100,
			o.threshold.highErrorRate*100))
	}

	// 检查错误率的变化趋势
	errorRate1m := o.metrics.ErrorRate.Rate1()
	errorRate5m := o.metrics.ErrorRate.Rate5()

	if errorRate1m > errorRate5m*1.5 {
		suggestions = append(suggestions, fmt.Sprintf(
			"最近1分钟错误率(%.2f/s)相比5分钟平均(%.2f/s)显著上升，建议检查最近的变更",
			errorRate1m,
			errorRate5m))
	}

	return suggestions
}

// analyzeConcurrency 分析并发度
func (o *Optimizer) analyzeConcurrency() []string {
	var suggestions []string

	// 获取当前并发处理消息数
	concurrentMessages := o.metrics.GetConcurrentMessages()

	if concurrentMessages > o.threshold.highConcurrency {
		suggestions = append(suggestions, fmt.Sprintf(
			"并发处理消息数(%d)超过阈值(%d)，考虑增加处理线程或优化处理逻辑",
			concurrentMessages,
			o.threshold.highConcurrency))
	}

	// 获取消息处理速率
	messageRate := o.metrics.MessageRate.Rate1()

	// 比较并发数与处理速率的关系
	if concurrentMessages > 0 && messageRate > 0 {
		ratio := float64(concurrentMessages) / messageRate
		if ratio > 10.0 {
			suggestions = append(suggestions, fmt.Sprintf(
				"并发消息数与处理速率比值(%.2f)过高，表明消息处理速度较慢，需要优化处理逻辑",
				ratio))
		}
	}

	return suggestions
}

// analyzeResourceUsage 分析资源使用
func (o *Optimizer) analyzeResourceUsage() []string {
	var suggestions []string

	// 这里假设我们从系统获取了CPU和内存使用率
	// 在实际实现中，需要从系统指标中获取这些信息
	cpuUsage := 0.0 // 示例值
	memUsage := 0.0 // 示例值

	if cpuUsage > o.threshold.highCPUUsage {
		suggestions = append(suggestions, fmt.Sprintf(
			"CPU使用率(%.2f%%)超过阈值(%.2f%%)，考虑优化CPU密集型操作或增加CPU资源",
			cpuUsage,
			o.threshold.highCPUUsage))
	}

	if memUsage > o.threshold.highMemoryUsage {
		suggestions = append(suggestions, fmt.Sprintf(
			"内存使用率(%.2f%%)超过阈值(%.2f%%)，检查内存泄漏或优化内存使用",
			memUsage,
			o.threshold.highMemoryUsage))
	}

	// 检查系统参数
	numCPU := runtime.NumCPU()
	maxProcs := runtime.GOMAXPROCS(0)

	if maxProcs < numCPU {
		suggestions = append(suggestions, fmt.Sprintf(
			"GOMAXPROCS(%d)小于可用CPU数(%d)，考虑增加GOMAXPROCS以充分利用CPU资源",
			maxProcs,
			numCPU))
	}

	return suggestions
}

// analyzeConnectionRate 分析连接利用率
func (o *Optimizer) analyzeConnectionRate() []string {
	var suggestions []string

	// 获取活跃连接数
	activeConnections := o.metrics.ActiveConnections.Value()

	// 获取消息速率
	messageRate := o.metrics.MessageRate.Rate1()

	// 如果连接数大于0，计算每个连接的平均消息速率
	if activeConnections > 0 {
		connRate := messageRate / float64(activeConnections)

		if connRate < o.threshold.lowConnectionRate {
			suggestions = append(suggestions, fmt.Sprintf(
				"每连接消息速率(%.2f/s)低于阈值(%.2f/s)，连接可能未被充分利用",
				connRate,
				o.threshold.lowConnectionRate))
		}
	}

	return suggestions
}

// LogOptimizations 记录优化建议
func (o *Optimizer) LogOptimizations() {
	suggestions := o.GenerateOptimizations()

	if len(suggestions) == 0 {
		logger.Info("性能分析完成，系统运行正常，无优化建议")
		return
	}

	// 按字母顺序排序建议
	sort.Strings(suggestions)

	logger.Warning("系统性能优化建议:")
	for i, suggestion := range suggestions {
		logger.Warning(fmt.Sprintf("%d. %s", i+1, suggestion))
	}
}

// StartOptimizationReporter 启动优化建议报告器
func (o *Optimizer) StartOptimizationReporter(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			o.LogOptimizations()
		}
	}()

	logger.Info(fmt.Sprintf("性能优化建议报告器已启动，报告间隔: %s", interval))
}
