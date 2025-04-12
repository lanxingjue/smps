// cmd/stresstest/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"

	"smps/internal/client"
	"smps/internal/protocol"
	"smps/pkg/logger"
)

// 压力测试配置
type StressTestConfig struct {
	// SMSC配置
	SMSCAddr string
	SystemID string
	Password string

	// 测试参数
	InitialClients    int           // 初始客户端数
	MaxClients        int           // 最大客户端数
	ClientIncrement   int           // 每轮增加的客户端数
	IncrementInterval time.Duration // 增加客户端的间隔
	MessageRate       int           // 每秒消息数
	TestDuration      time.Duration // 测试持续时间
	MessageSize       int           // 消息大小(字节)

	// 测试目标
	TargetCPU     float64 // 目标CPU使用率
	TargetLatency float64 // 目标延迟(ms)

	// 报告间隔
	ReportInterval time.Duration
}

// 测试指标
type StressTestMetrics struct {
	// 发送计数器
	SendCounter metrics.Counter

	// 发送速率
	SendRate metrics.Meter

	// 响应延迟
	ResponseLatency metrics.Timer

	// 错误计数器
	ErrorCounter metrics.Counter

	// 客户端数
	ClientGauge metrics.Gauge

	// 系统指标
	CPUUsage float64
	MemUsage float64
}

// 创建测试指标
func NewStressTestMetrics() *StressTestMetrics {
	return &StressTestMetrics{
		SendCounter:     metrics.NewCounter(),
		SendRate:        metrics.NewMeter(),
		ResponseLatency: metrics.NewTimer(),
		ErrorCounter:    metrics.NewCounter(),
		ClientGauge:     metrics.NewGauge(),
	}
}

func main() {
	// 解析命令行参数
	cfg := parseFlags()

	// 初始化日志
	logger.Init("smps_stresstest")

	// 初始化随机数生成器
	rand.Seed(time.Now().UnixNano())

	// 创建测试指标
	metrics := NewStressTestMetrics()

	// The rest of the implementation is similar to the benchmark tool,
	// but with the added logic to gradually increase the load

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动指标报告器
	go reportMetrics(metrics, cfg.ReportInterval)

	// 启动系统监控
	go monitorSystem(metrics, 2*time.Second)

	// 创建客户端管理通道
	clientCh := make(chan struct{}, cfg.MaxClients)

	// 启动初始客户端
	var wg sync.WaitGroup
	metrics.ClientGauge.Update(int64(cfg.InitialClients))

	logger.Info(fmt.Sprintf("开始压力测试，初始并发=%d, 最大并发=%d, 增量=%d, 增量间隔=%s",
		cfg.InitialClients, cfg.MaxClients, cfg.ClientIncrement, cfg.IncrementInterval))

	// 启动初始客户端
	for i := 0; i < cfg.InitialClients; i++ {
		wg.Add(1)
		go runClient(ctx, &wg, cfg, metrics, i, clientCh)
	}

	// 定时增加客户端数量
	if cfg.ClientIncrement > 0 && cfg.MaxClients > cfg.InitialClients {
		go incrementClients(ctx, &wg, cfg, metrics, clientCh)
	}

	// 设置测试计时器
	var timer *time.Timer
	if cfg.TestDuration > 0 {
		timer = time.NewTimer(cfg.TestDuration)
	}

	// 注册信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待测试完成或中断
	select {
	case <-timer.C:
		logger.Info(fmt.Sprintf("测试时间到达 %s，正在停止测试...", cfg.TestDuration))
	case sig := <-sigCh:
		logger.Info(fmt.Sprintf("收到信号 %s，正在停止测试...", sig))
	}

	// 取消上下文，终止所有客户端
	cancel()

	// 等待所有客户端停止
	wg.Wait()

	// 输出最终结果
	printFinalResults(metrics, cfg)

	logger.Info("压力测试已完成")
}

// 解析命令行参数
func parseFlags() *StressTestConfig {
	cfg := &StressTestConfig{}

	flag.StringVar(&cfg.SMSCAddr, "addr", "localhost:2775", "SMSC服务器地址")
	flag.StringVar(&cfg.SystemID, "system", "test", "系统ID")
	flag.StringVar(&cfg.Password, "password", "test", "密码")
	flag.IntVar(&cfg.InitialClients, "initial", 10, "初始客户端数")
	flag.IntVar(&cfg.MaxClients, "max", 100, "最大客户端数")
	flag.IntVar(&cfg.ClientIncrement, "increment", 10, "每轮增加的客户端数")
	flag.DurationVar(&cfg.IncrementInterval, "interval", 30*time.Second, "增加客户端的间隔")
	flag.IntVar(&cfg.MessageRate, "rate", 10, "每客户端每秒消息数")
	flag.DurationVar(&cfg.TestDuration, "duration", 5*time.Minute, "测试持续时间")
	flag.IntVar(&cfg.MessageSize, "size", 140, "消息大小(字节)")
	flag.Float64Var(&cfg.TargetCPU, "cpu", 80.0, "目标CPU使用率(百分比)")
	flag.Float64Var(&cfg.TargetLatency, "latency", 100.0, "目标延迟(毫秒)")
	flag.DurationVar(&cfg.ReportInterval, "report", 5*time.Second, "报告间隔")

	flag.Parse()

	return cfg
}

// 运行客户端
func runClient(ctx context.Context, wg *sync.WaitGroup, cfg *StressTestConfig, metrics *StressTestMetrics, clientID int, clientCh chan struct{}) {
	defer wg.Done()
	defer func() {
		select {
		case <-clientCh:
			// 从通道中移除客户端
		default:
			// 通道可能已关闭
		}
	}()

	// 将客户端添加到通道
	clientCh <- struct{}{}

	// 创建SMSC客户端配置
	smscConfig := &client.Config{
		Address:           cfg.SMSCAddr,
		SystemID:          fmt.Sprintf("%s_%d", cfg.SystemID, clientID),
		Password:          cfg.Password,
		EnquireInterval:   30 * time.Second,
		ResponseTimeout:   5 * time.Second,
		ReconnectInterval: 5 * time.Second,
		MaxRetries:        3,
	}

	// 创建SMSC客户端
	smscClient := client.NewSMSCClient(smscConfig)

	// 连接到SMSC
	if err := smscClient.Connect(ctx); err != nil {
		logger.Error(fmt.Sprintf("客户端%d连接失败: %v", clientID, err))
		metrics.ErrorCounter.Inc(1)
		return
	}
	defer smscClient.Close()

	logger.Info(fmt.Sprintf("客户端%d已连接到SMSC", clientID))

	// 计算消息间隔
	interval := time.Second / time.Duration(cfg.MessageRate)

	// 创建消息发送器
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 生成测试消息
	msg := generateTestMessage(cfg.MessageSize, clientID)

	// 开始发送消息
	for {
		select {
		case <-ctx.Done():
			// 上下文取消，停止测试
			return

		case <-ticker.C:
			// 发送消息
			startTime := time.Now()

			// 提交短信
			_, err := smscClient.Send(msg)

			// 记录延迟
			metrics.ResponseLatency.UpdateSince(startTime)

			if err == nil {
				metrics.SendCounter.Inc(1)
				metrics.SendRate.Mark(1)
			} else {
				metrics.ErrorCounter.Inc(1)
				logger.Error(fmt.Sprintf("客户端%d发送消息失败: %v", clientID, err))

				// 重新连接
				smscClient.Close()
				if err := smscClient.Connect(ctx); err != nil {
					logger.Error(fmt.Sprintf("客户端%d重连失败: %v", clientID, err))
					return
				}
			}
		}
	}
}

// 生成测试消息
func generateTestMessage(size int, clientID int) []byte {
	// 创建消息头
	header := protocol.Header{
		CommandLength:  uint32(16 + size),
		CommandID:      protocol.SUBMIT_SM,
		CommandStatus:  0,
		SequenceNumber: uint32(clientID*1000000 + rand.Intn(1000000)),
	}

	// 创建消息体 (简化示例)
	payload := make([]byte, size)
	for i := 0; i < size; i++ {
		payload[i] = byte(65 + (i % 26)) // A-Z循环
	}

	// 创建完整消息
	msg := protocol.NewMessage(protocol.SUBMIT_SM, 0, header.SequenceNumber, payload)

	return msg.Bytes()
}

// 增加客户端数量
func incrementClients(ctx context.Context, wg *sync.WaitGroup, cfg *StressTestConfig, metrics *StressTestMetrics, clientCh chan struct{}) {
	ticker := time.NewTicker(cfg.IncrementInterval)
	defer ticker.Stop()

	clientID := cfg.InitialClients

	for range ticker.C {
		select {
		case <-ctx.Done():
			return
		default:
			// 检查是否已达最大客户端数
			currentClients := metrics.ClientGauge.Value()
			if currentClients >= int64(cfg.MaxClients) {
				return
			}

			// 检查是否达到目标CPU使用率
			if metrics.CPUUsage >= cfg.TargetCPU {
				logger.Warning(fmt.Sprintf("CPU使用率达到目标(%.2f%%)，停止增加客户端", metrics.CPUUsage))
				return
			}

			// 检查是否达到目标延迟
			avgLatency := metrics.ResponseLatency.Mean() / float64(time.Millisecond)
			if avgLatency >= cfg.TargetLatency {
				logger.Warning(fmt.Sprintf("平均延迟达到目标(%.2fms)，停止增加客户端", avgLatency))
				return
			}

			// 计算新增客户端数量
			increment := cfg.ClientIncrement
			if currentClients+int64(increment) > int64(cfg.MaxClients) {
				increment = int(int64(cfg.MaxClients) - currentClients)
			}

			// 启动新客户端
			for i := 0; i < increment; i++ {
				wg.Add(1)
				go runClient(ctx, wg, cfg, metrics, clientID, clientCh)
				clientID++
			}

			// 更新客户端计数
			metrics.ClientGauge.Update(currentClients + int64(increment))
			logger.Info(fmt.Sprintf("增加%d个客户端，当前总数: %d", increment, currentClients+int64(increment)))
		}
	}
}

// 监控系统资源
func monitorSystem(metrics *StressTestMetrics, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		// 获取CPU使用率
		cpuPercent, err := cpu.Percent(time.Second, false)
		if err == nil && len(cpuPercent) > 0 {
			metrics.CPUUsage = cpuPercent[0]
		}

		// 获取内存使用率
		memInfo, err := mem.VirtualMemory()
		if err == nil {
			metrics.MemUsage = memInfo.UsedPercent
		}
	}
}

// 打印性能指标
func reportMetrics(metrics *StressTestMetrics, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		printMetrics(metrics)
	}
}

// 打印指标
func printMetrics(metrics *StressTestMetrics) {
	logger.Info("=== 压力测试指标 ===")

	// 打印客户端数
	clientCount := metrics.ClientGauge.Value()
	logger.Info(fmt.Sprintf("活跃客户端: %d", clientCount))

	// 打印发送指标
	sendCount := metrics.SendCounter.Count()
	sendRate := metrics.SendRate.Rate1()
	logger.Info(fmt.Sprintf("发送总数: %d, 发送速率: %.2f msg/s", sendCount, sendRate))

	// 打印延迟指标
	avgLatency := metrics.ResponseLatency.Mean() / float64(time.Millisecond)
	p95Latency := metrics.ResponseLatency.Percentile(0.95) / float64(time.Millisecond)
	logger.Info(fmt.Sprintf("平均延迟: %.2f ms, P95延迟: %.2f ms", avgLatency, p95Latency))

	// 打印错误指标
	errorCount := metrics.ErrorCounter.Count()
	errorRate := float64(0)
	if sendCount > 0 {
		errorRate = float64(errorCount) / float64(sendCount) * 100
	}
	logger.Info(fmt.Sprintf("错误总数: %d, 错误率: %.2f%%", errorCount, errorRate))

	// 打印系统指标
	logger.Info(fmt.Sprintf("CPU使用率: %.2f%%, 内存使用率: %.2f%%", metrics.CPUUsage, metrics.MemUsage))
}

// 打印最终结果
func printFinalResults(metrics *StressTestMetrics, cfg *StressTestConfig) {
	logger.Info("=== 压力测试最终结果 ===")

	// 打印客户端数
	maxClients := metrics.ClientGauge.Value()
	logger.Info(fmt.Sprintf("最大客户端数: %d", maxClients))

	// 打印消息指标
	totalMessages := metrics.SendCounter.Count()
	avgRate := metrics.SendRate.Rate1()
	peakRate := metrics.SendRate.RateMean()

	logger.Info(fmt.Sprintf("总消息数: %d", totalMessages))
	logger.Info(fmt.Sprintf("平均速率: %.2f msg/s", avgRate))
	logger.Info(fmt.Sprintf("峰值速率: %.2f msg/s", peakRate))

	// 打印延迟指标
	minLatency := metrics.ResponseLatency.Min() / float64(time.Millisecond)
	maxLatency := metrics.ResponseLatency.Max() / float64(time.Millisecond)
	avgLatency := metrics.ResponseLatency.Mean() / float64(time.Millisecond)
	p50Latency := metrics.ResponseLatency.Percentile(0.5) / float64(time.Millisecond)
	p90Latency := metrics.ResponseLatency.Percentile(0.9) / float64(time.Millisecond)
	p95Latency := metrics.ResponseLatency.Percentile(0.95) / float64(time.Millisecond)
	p99Latency := metrics.ResponseLatency.Percentile(0.99) / float64(time.Millisecond)

	logger.Info(fmt.Sprintf("最小延迟: %.2f ms", minLatency))
	logger.Info(fmt.Sprintf("最大延迟: %.2f ms", maxLatency))
	logger.Info(fmt.Sprintf("平均延迟: %.2f ms", avgLatency))
	logger.Info(fmt.Sprintf("P50延迟: %.2f ms", p50Latency))
	logger.Info(fmt.Sprintf("P90延迟: %.2f ms", p90Latency))
	logger.Info(fmt.Sprintf("P95延迟: %.2f ms", p95Latency))
	logger.Info(fmt.Sprintf("P99延迟: %.2f ms", p99Latency))

	// 打印错误指标
	errorCount := metrics.ErrorCounter.Count()
	errorRate := float64(0)
	if totalMessages > 0 {
		errorRate = float64(errorCount) / float64(totalMessages) * 100
	}
	logger.Info(fmt.Sprintf("错误总数: %d", errorCount))
	logger.Info(fmt.Sprintf("错误率: %.2f%%", errorRate))

	// 打印系统指标
	logger.Info(fmt.Sprintf("峰值CPU使用率: %.2f%%", metrics.CPUUsage))
	logger.Info(fmt.Sprintf("峰值内存使用率: %.2f%%", metrics.MemUsage))

	// 比较与目标的差距
	logger.Info(fmt.Sprintf("目标CPU使用率: %.2f%%, 实际: %.2f%%", cfg.TargetCPU, metrics.CPUUsage))
	logger.Info(fmt.Sprintf("目标延迟: %.2f ms, 实际: %.2f ms", cfg.TargetLatency, avgLatency))
}
