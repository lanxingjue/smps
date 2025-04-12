// cmd/benchmark/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rcrowley/go-metrics"

	"smps/internal/client"
	"smps/internal/protocol"
	"smps/pkg/logger"
)

// 测试配置
type BenchmarkConfig struct {
	// SMSC配置
	SMSCAddr string
	SystemID string
	Password string

	// 测试参数
	ConcurrentClients int
	MessageRate       int
	TestDuration      time.Duration
	MessageSize       int

	// 测试类型
	TestType string // submit, deliver

	// 报告间隔
	ReportInterval time.Duration
}

// 测试指标
type BenchmarkMetrics struct {
	// 发送计数器
	SubmitCounter metrics.Counter

	// 发送速率
	SubmitRate metrics.Meter

	// 接收计数器
	DeliverCounter metrics.Counter

	// 接收速率
	DeliverRate metrics.Meter

	// 响应延迟
	ResponseLatency metrics.Timer

	// 错误计数器
	ErrorCounter metrics.Counter

	// 并发客户端数
	ClientGauge metrics.Gauge
}

// 创建测试指标
func NewBenchmarkMetrics() *BenchmarkMetrics {
	return &BenchmarkMetrics{
		SubmitCounter:   metrics.NewCounter(),
		SubmitRate:      metrics.NewMeter(),
		DeliverCounter:  metrics.NewCounter(),
		DeliverRate:     metrics.NewMeter(),
		ResponseLatency: metrics.NewTimer(),
		ErrorCounter:    metrics.NewCounter(),
		ClientGauge:     metrics.NewGauge(),
	}
}

func main() {
	// 解析命令行参数
	cfg := parseFlags()

	// 初始化日志
	logger.Init("smps_benchmark")

	// 创建测试指标
	metrics := NewBenchmarkMetrics()

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动指标报告器
	go reportMetrics(metrics, cfg.ReportInterval)

	// 启动测试
	var wg sync.WaitGroup
	metrics.ClientGauge.Update(int64(cfg.ConcurrentClients))

	logger.Info(fmt.Sprintf("开始性能测试，类型=%s, 并发=%d, 速率=%d msg/s, 持续=%s",
		cfg.TestType, cfg.ConcurrentClients, cfg.MessageRate, cfg.TestDuration))

	// 启动客户端
	for i := 0; i < cfg.ConcurrentClients; i++ {
		wg.Add(1)
		go runClient(ctx, &wg, cfg, metrics, i)
	}

	// 设置定时器
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
	printFinalResults(metrics)

	logger.Info("测试已完成")
}

// 解析命令行参数
func parseFlags() *BenchmarkConfig {
	cfg := &BenchmarkConfig{}

	flag.StringVar(&cfg.SMSCAddr, "addr", "localhost:2775", "SMSC服务器地址")
	flag.StringVar(&cfg.SystemID, "system", "test", "系统ID")
	flag.StringVar(&cfg.Password, "password", "test", "密码")
	flag.IntVar(&cfg.ConcurrentClients, "clients", 10, "并发客户端数")
	flag.IntVar(&cfg.MessageRate, "rate", 100, "每秒消息数")
	flag.DurationVar(&cfg.TestDuration, "duration", 60*time.Second, "测试持续时间")
	flag.IntVar(&cfg.MessageSize, "size", 140, "消息大小(字节)")
	flag.StringVar(&cfg.TestType, "type", "submit", "测试类型: submit/deliver")
	flag.DurationVar(&cfg.ReportInterval, "interval", 5*time.Second, "报告间隔")

	flag.Parse()

	return cfg
}

// 运行客户端
func runClient(ctx context.Context, wg *sync.WaitGroup, cfg *BenchmarkConfig, metrics *BenchmarkMetrics, clientID int) {
	defer wg.Done()

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
	interval := time.Second / time.Duration(cfg.MessageRate/cfg.ConcurrentClients)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	// 创建消息发送器
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 生成测试消息
	var msg []byte
	if cfg.TestType == "submit" {
		msg = generateSubmitSM(cfg.MessageSize, clientID)
	} else {
		msg = generateDeliverSM(cfg.MessageSize, clientID)
	}

	// 开始发送消息
	for {
		select {
		case <-ctx.Done():
			// 上下文取消，停止测试
			return

		case <-ticker.C:
			// 发送消息
			startTime := time.Now()

			var err error
			if cfg.TestType == "submit" {
				// 提交短信
				_, err = smscClient.Send(msg)
				if err == nil {
					metrics.SubmitCounter.Inc(1)
					metrics.SubmitRate.Mark(1)
				}
			} else {
				// 投递短信(模拟)
				_, err = smscClient.Send(msg)
				if err == nil {
					metrics.DeliverCounter.Inc(1)
					metrics.DeliverRate.Mark(1)
				}
			}

			// 记录延迟
			metrics.ResponseLatency.UpdateSince(startTime)

			// 处理错误
			if err != nil {
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

// 生成SUBMIT_SM消息
func generateSubmitSM(size int, clientID int) []byte {
	// 简化示例，实际应根据SMPP协议格式生成消息
	// 创建消息头
	header := protocol.Header{
		CommandLength:  uint32(16 + size),
		CommandID:      protocol.SUBMIT_SM,
		CommandStatus:  0,
		SequenceNumber: uint32(clientID*1000000 + time.Now().Nanosecond()%1000000),
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

// 生成DELIVER_SM消息
func generateDeliverSM(size int, clientID int) []byte {
	// 基本逻辑与SUBMIT_SM类似，但CommandID不同
	// 创建消息头
	header := protocol.Header{
		CommandLength:  uint32(16 + size),
		CommandID:      protocol.DELIVER_SM,
		CommandStatus:  0,
		SequenceNumber: uint32(clientID*1000000 + time.Now().Nanosecond()%1000000),
	}

	// 创建消息体 (简化示例)
	payload := make([]byte, size)
	for i := 0; i < size; i++ {
		payload[i] = byte(65 + (i % 26)) // A-Z循环
	}

	// 创建完整消息
	msg := protocol.NewMessage(protocol.DELIVER_SM, 0, header.SequenceNumber, payload)

	return msg.Bytes()
}

// 定期报告指标
func reportMetrics(metrics *BenchmarkMetrics, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		printMetrics(metrics)
	}
}

// 打印指标
func printMetrics(metrics *BenchmarkMetrics) {
	logger.Info("=== 性能测试指标 ===")

	// 打印发送指标
	submitCount := metrics.SubmitCounter.Count()
	submitRate := metrics.SubmitRate.Rate1()
	logger.Info(fmt.Sprintf("发送总数: %d, 发送速率: %.2f msg/s", submitCount, submitRate))

	// 打印接收指标
	deliverCount := metrics.DeliverCounter.Count()
	deliverRate := metrics.DeliverRate.Rate1()
	logger.Info(fmt.Sprintf("接收总数: %d, 接收速率: %.2f msg/s", deliverCount, deliverRate))

	// 打印延迟指标
	avgLatency := metrics.ResponseLatency.Mean() / float64(time.Millisecond)
	p95Latency := metrics.ResponseLatency.Percentile(0.95) / float64(time.Millisecond)
	p99Latency := metrics.ResponseLatency.Percentile(0.99) / float64(time.Millisecond)
	logger.Info(fmt.Sprintf("平均延迟: %.2f ms, P95延迟: %.2f ms, P99延迟: %.2f ms",
		avgLatency, p95Latency, p99Latency))

	// 打印错误指标
	errorCount := metrics.ErrorCounter.Count()
	errorRate := float64(0)
	if submitCount+deliverCount > 0 {
		errorRate = float64(errorCount) / float64(submitCount+deliverCount) * 100
	}
	logger.Info(fmt.Sprintf("错误总数: %d, 错误率: %.2f%%", errorCount, errorRate))
}

// 打印最终结果
func printFinalResults(metrics *BenchmarkMetrics) {
	logger.Info("=== 测试最终结果 ===")

	// 计算总体指标
	totalMessages := metrics.SubmitCounter.Count() + metrics.DeliverCounter.Count()
	avgRate := float64(totalMessages) / metrics.ResponseLatency.Rate1()

	logger.Info(fmt.Sprintf("总消息数: %d", totalMessages))
	logger.Info(fmt.Sprintf("平均速率: %.2f msg/s", avgRate))
	logger.Info(fmt.Sprintf("最小延迟: %.2f ms", metrics.ResponseLatency.Min()/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("最大延迟: %.2f ms", metrics.ResponseLatency.Max()/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("平均延迟: %.2f ms", metrics.ResponseLatency.Mean()/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("P50延迟: %.2f ms", metrics.ResponseLatency.Percentile(0.5)/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("P90延迟: %.2f ms", metrics.ResponseLatency.Percentile(0.9)/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("P95延迟: %.2f ms", metrics.ResponseLatency.Percentile(0.95)/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("P99延迟: %.2f ms", metrics.ResponseLatency.Percentile(0.99)/float64(time.Millisecond)))
	logger.Info(fmt.Sprintf("错误总数: %d", metrics.ErrorCounter.Count()))

	// 计算错误率
	errorRate := float64(0)
	if totalMessages > 0 {
		errorRate = float64(metrics.ErrorCounter.Count()) / float64(totalMessages) * 100
	}
	logger.Info(fmt.Sprintf("错误率: %.2f%%", errorRate))
}
