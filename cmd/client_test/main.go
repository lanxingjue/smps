// cmd/client_test/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smps/internal/client"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	address := flag.String("address", "localhost:2775", "SMSC服务器地址")
	systemID := flag.String("system-id", "smps_client", "系统ID")
	password := flag.String("password", "password", "密码")
	flag.Parse()

	// 初始化日志
	logger.Init("smps_client")

	// 创建客户端配置
	config := &client.Config{
		Address:           *address,
		SystemID:          *systemID,
		Password:          *password,
		EnquireInterval:   60 * time.Second,
		ResponseTimeout:   5 * time.Second,
		ReconnectInterval: 5 * time.Second,
		MaxRetries:        5,
		BackoffFactor:     1.5,
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建客户端
	smscClient := client.NewSMSCClient(config)

	// 启动客户端
	logger.Info(fmt.Sprintf("正在连接到 %s...", *address))
	if err := smscClient.Start(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("启动客户端失败: %v", err))
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
				status := smscClient.GetStatus()
				stats := smscClient.GetStats()

				logger.Info(fmt.Sprintf("客户端状态: %s", status))
				logger.Info(fmt.Sprintf("统计信息: 已发送=%d, 已接收=%d, 重连次数=%d",
					stats["sent_messages"],
					stats["received_messages"],
					stats["reconnect_count"]))

			case <-ctx.Done():
				return
			}
		}
	}()

	// 等待信号
	<-sigCh
	log.Println("接收到关闭信号，停止客户端...")

	// 停止客户端
	smscClient.Stop()
	log.Println("客户端已停止")
}
