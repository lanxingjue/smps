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
	"smps/internal/server"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	listenAddr := flag.String("listen", ":2775", "服务器监听地址")
	maxConns := flag.Int("max-conns", 100, "最大连接数")
	flag.Parse()

	// 初始化日志
	logger.Init("smps_server")

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
		ListenAddress:          *listenAddr,
		MaxConnections:         *maxConns,
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

	// 注册信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 定期打印状态信息
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stats := smscServer.GetStats()
				logger.Info(fmt.Sprintf("服务器状态: 活动连接=%d, 总连接=%d, 接收消息=%d, 发送消息=%d",
					stats["active_connections"],
					stats["total_connections"],
					stats["received_messages"],
					stats["sent_messages"]))

			case <-ctx.Done():
				return
			}
		}
	}()

	// 等待信号
	<-sigCh
	logger.Info("接收到关闭信号，停止服务器...")

	// 停止服务器
	smscServer.Stop()
	logger.Info("服务器已停止")
}
