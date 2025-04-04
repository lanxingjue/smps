// cmd/tracer_test/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smps/internal/protocol"
	"smps/internal/tracer"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	parseContent := flag.Bool("parse-content", true, "是否解析短信内容")
	flag.Parse()

	// 初始化日志
	logger.Init("smps_tracer_test")

	// 创建上下文
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建跟踪器配置
	config := tracer.NewTracerConfig()
	config.SetParseContent(*parseContent)

	// 添加示例跟踪号码
	config.AddTracedNumber("13800138000")
	config.AddTracedNumber("10086")

	// 创建协议跟踪器
	protocolTracer := tracer.NewProtocolTracer(config, nil)

	// 生成测试消息
	go func() {
		// 等待系统初始化
		time.Sleep(1 * time.Second)

		logger.Info("开始生成测试消息...")

		// 测试1: 标准的短信投递消息 (GSM7编码)
		createAndTraceMessage(protocolTracer, "10086", "13800138000", "这是一条GSM7编码的测试短信", 0x00)

		// 等待1秒
		time.Sleep(1 * time.Second)

		// 测试2: UTF-16编码的短信
		createAndTraceMessage(protocolTracer, "10010", "13900139000", "这是一条UTF-16编码的测试短信，包含中文！", 0x08)

		// 等待1秒
		time.Sleep(1 * time.Second)

		// 测试3: 不在跟踪列表中的号码
		createAndTraceMessage(protocolTracer, "10000", "13700137000", "这条消息应该不会被跟踪", 0x00)

		// 测试4: 链路查询消息
		msg := protocol.CreateEnquireLink(42)
		protocolTracer.TraceOutgoing(msg, "SMSC")
		logger.Info("发送链路查询消息")

		// 打印最近日志
		time.Sleep(2 * time.Second)
		printRecentLogs(protocolTracer)
	}()

	// 注册信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	<-sigCh
	logger.Info("接收到关闭信号，开始优雅关闭...")
}

// 创建并跟踪消息
func createAndTraceMessage(protocolTracer *tracer.ProtocolTracer, sourceAddr, destAddr, content string, dataCoding byte) {
	// 创建服务类型 + 空终止符
	payload := []byte("SMPP\x00")

	// 源地址TON和NPI (1=国际, 1=ISDN)
	payload = append(payload, 0x01, 0x01)

	// 源地址 + 空终止符
	payload = append(payload, []byte(sourceAddr)...)
	payload = append(payload, 0x00)

	// 目标地址TON和NPI (1=国际, 1=ISDN)
	payload = append(payload, 0x01, 0x01)

	// 目标地址 + 空终止符
	payload = append(payload, []byte(destAddr)...)
	payload = append(payload, 0x00)

	// ESM类, 优先级, 投递时间, 有效期, 注册, 替代 (服务参数)
	payload = append(payload, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)

	// 数据编码
	payload = append(payload, dataCoding)

	// 短信内容
	contentBytes := []byte(content)

	// 内容长度
	payload = append(payload, byte(len(contentBytes)))

	// 内容
	payload = append(payload, contentBytes...)

	// 创建DELIVER_SM消息
	msg := protocol.NewMessage(protocol.DELIVER_SM, 0, uint32(time.Now().UnixNano()%1000000), payload)

	// 跟踪消息
	logger.Info(fmt.Sprintf("创建测试消息: 发送方=%s, 接收方=%s", sourceAddr, destAddr))
	protocolTracer.TraceIncoming(msg, "SMSC")

	// 创建响应
	resp := protocol.CreateDeliverSMResponse(msg.Header.SequenceNumber, protocol.SM_OK)
	protocolTracer.TraceOutgoing(resp, "CLIENT")
}

// 打印最近日志
func printRecentLogs(protocolTracer *tracer.ProtocolTracer) {
	logs := protocolTracer.GetRecentLogs()

	logger.Info(fmt.Sprintf("==== 最近日志 (共%d条) ====", len(logs)))

	for i, log := range logs {
		logger.Info(fmt.Sprintf("%d. [%s] %s %s -> %s (%s)",
			i+1,
			log.Timestamp.Format("15:04:05.000"),
			log.Direction,
			log.SourceAddr,
			log.DestAddr,
			log.CommandName,
		))

		if log.Content != "" {
			logger.Info(fmt.Sprintf("   内容: %s", log.Content))
		}
	}
}
