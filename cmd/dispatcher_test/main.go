// cmd/dispatcher_test/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smps/internal/auth"
	"smps/internal/dispatcher"
	"smps/internal/protocol"
	"smps/internal/server"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	// configFile := flag.String("config", "config/config.yaml", "配置文件路径")
	queueSize := flag.Int("queue-size", 1000, "消息队列大小")
	workerCount := flag.Int("workers", 5, "工作线程数")
	flag.Parse()

	// 初始化日志
	logger.Init("smps_dispatcher_test")

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建认证器
	authenticator := auth.NewAuthenticator(nil, false)

	// 添加测试账户
	account := &auth.Account{
		SystemID:      "test",
		Password:      "password",
		InterfaceType: "SMPP-M",
		Protocol:      "TCPIP",
		EncodingType:  "SMPP-M",
		FlowControl:   1000,
	}
	authenticator.RegisterAccount(account)

	// 创建服务器配置
	serverConfig := &server.ServerConfig{
		ListenAddress:          ":2775",
		MaxConnections:         100,
		ReadTimeout:            30 * time.Second,
		WriteTimeout:           30 * time.Second,
		HeartbeatInterval:      5 * time.Second,
		HeartbeatMissThreshold: 3,
	}

	// 创建SMMC服务器
	smscServer := server.NewServer(serverConfig, authenticator)

	// 启动服务器
	if err := smscServer.Start(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("启动SMMC服务器失败: %v", err))
	}

	// 创建消息分发器
	msgDispatcher := dispatcher.NewMessageDispatcher(*queueSize, *workerCount, 2*time.Second)

	// 注册日志处理器
	logHandler := dispatcher.NewLoggingHandler()
	msgDispatcher.RegisterHandler(logHandler)

	// 注册广播处理器
	broadcastHandler := dispatcher.NewBroadcastHandler(smscServer, 0)
	msgDispatcher.RegisterHandler(broadcastHandler)

	// 启动分发器
	if err := msgDispatcher.Start(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("启动消息分发器失败: %v", err))
	}

	// 创建客户端配置
	// clientConfig := &client.Config{
	// 	Address:           "localhost:2775",
	// 	SystemID:          "test",
	// 	Password:          "password",
	// 	EnquireInterval:   60 * time.Second,
	// 	ResponseTimeout:   5 * time.Second,
	// 	ReconnectInterval: 5 * time.Second,
	// 	MaxRetries:        5,
	// 	BackoffFactor:     1.5,
	// }

	// 模拟消息生成器
	go func() {
		for i := 0; i < 10; i++ {
			// 等待几秒让系统稳定
			time.Sleep(5 * time.Second)

			// 构造测试消息
			sourceAddr := "10086"
			destAddr := fmt.Sprintf("1380013%04d", i)
			content := fmt.Sprintf("这是测试消息 #%d", i+1)

			// 创建DELIVER_SM消息
			payload := []byte(fmt.Sprintf("\x00%s\x00%s\x00%s\x00", sourceAddr, destAddr, content))
			msg := protocol.NewMessage(protocol.DELIVER_SM, 0, uint32(i+1), payload)

			logger.Info(fmt.Sprintf("生成测试消息 #%d: 发送方=%s, 接收方=%s",
				i+1, sourceAddr, destAddr))

			// 分发消息
			msgDispatcher.Dispatch(msg)

			// 简单模拟负载
			if i%3 == 0 {
				// 每3条消息快速发送一批
				for j := 0; j < 5; j++ {
					subMsg := protocol.NewMessage(protocol.DELIVER_SM, 0, uint32(100+i*10+j), payload)
					msgDispatcher.Dispatch(subMsg)
				}
			}
		}
	}()

	// 定期打印状态信息
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stats := msgDispatcher.GetStats()
				logger.Info(fmt.Sprintf("分发器状态: 已分发=%d, 错误=%d, 平均时间=%.2fms, 队列大小=%d/%d",
					stats["dispatched"],
					stats["errors"],
					stats["avg_time_ms"],
					stats["queue_size"],
					stats["queue_capacity"]))

				serverStats := smscServer.GetStats()
				logger.Info(fmt.Sprintf("服务器状态: 活动连接=%d, 已接收=%d, 已发送=%d",
					serverStats["active_connections"],
					serverStats["received_messages"],
					serverStats["sent_messages"]))

			case <-ctx.Done():
				return
			}
		}
	}()

	// 注册信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	<-sigCh
	logger.Info("接收到关闭信号，开始优雅关闭...")

	// 优雅关闭
	msgDispatcher.Stop()
	smscServer.Stop()

	logger.Info("系统已关闭")
}
