// cmd/processor_test/main.go
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

	"smps/internal/auth"
	"smps/internal/processor"
	"smps/internal/protocol"
	"smps/internal/server"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	timeoutSec := flag.Int("timeout", 2, "响应超时时间(秒)")
	simClients := flag.Int("clients", 3, "模拟客户端数")
	delayMin := flag.Int("min-delay", 500, "最小响应延迟(毫秒)")
	delayMax := flag.Int("max-delay", 3000, "最大响应延迟(毫秒)")
	interceptRate := flag.Float64("intercept-rate", 0.3, "拦截率(0-1)")
	flag.Parse()

	// 初始化日志
	logger.Init("smps_processor_test")

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建认证器
	authenticator := auth.NewAuthenticator(nil, false)

	// 添加测试账户
	for i := 1; i <= *simClients; i++ {
		account := &auth.Account{
			SystemID:      fmt.Sprintf("test%d", i),
			Password:      "password",
			InterfaceType: "SMPP-M",
			Protocol:      "TCPIP",
			EncodingType:  "SMPP-M",
			FlowControl:   1000,
		}
		authenticator.RegisterAccount(account)
	}

	// 创建服务器配置
	serverConfig := &server.ServerConfig{
		ListenAddress:          ":2775",
		MaxConnections:         100,
		ReadTimeout:            30 * time.Second,
		WriteTimeout:           30 * time.Second,
		HeartbeatInterval:      24 * time.Hour, // 设置为24小时
		HeartbeatMissThreshold: 3,
	}

	// 创建SMMC服务器
	smmcServer := server.NewServer(serverConfig, authenticator)

	// 创建响应处理器配置
	processorConfig := &processor.ResponseConfig{
		MessageTimeout:  time.Duration(*timeoutSec) * time.Second,
		CleanupInterval: 10 * time.Second,
		MaxTrackers:     1000,
	}

	// 创建响应处理器
	responseProcessor := processor.NewResponseProcessor(processorConfig)

	// 启动服务器和处理器
	if err := smmcServer.Start(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("启动SMMC服务器失败: %v", err))
	}

	if err := responseProcessor.Start(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("启动响应处理器失败: %v", err))
	}

	// 模拟客户端会话
	var simSessions []*server.Session
	for i := 1; i <= *simClients; i++ {
		session := &server.Session{
			ID:           uint64(i),
			SystemID:     fmt.Sprintf("test%d", i),
			Status:       protocol.Session_Connected,
			CreatedAt:    time.Now(),
			LastActivity: time.Now().UnixNano(),
		}
		smmcServer.GetSessionManager().Add(session)
		simSessions = append(simSessions, session)
	}

	// 随机数生成器
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// 生成测试消息
	go func() {
		messageID := 1
		for i := 0; i < 20; i++ {
			// 每秒生成一条消息
			time.Sleep(1 * time.Second)

			currentID := messageID
			messageID++

			// 创建消息信息（不需要实际创建消息对象）
			sourceAddr := "10086"
			destAddr := fmt.Sprintf("1380013%04d", currentID)
			// content := fmt.Sprintf("这是测试消息 #%d", currentID)""

			logger.Info(fmt.Sprintf("生成测试消息 #%d: 发送方=%s, 接收方=%s",
				currentID, sourceAddr, destAddr))

			// 提取会话ID作为响应来源
			var sources []string
			for _, session := range simSessions {
				sources = append(sources, session.SystemID)
			}

			// 跟踪消息
			tracker, err := responseProcessor.TrackMessage(fmt.Sprintf("%d", currentID), sources)
			if err != nil {
				logger.Error(fmt.Sprintf("跟踪消息失败: %v", err))
				continue
			}

			// 模拟客户端响应
			var wg sync.WaitGroup
			for _, session := range simSessions {
				wg.Add(1)
				go func(s *server.Session) {
					defer wg.Done()

					// 随机延迟
					delay := time.Duration(r.Intn(*delayMax-*delayMin)+*delayMin) * time.Millisecond
					time.Sleep(delay)

					// 随机决策
					var action processor.ActionType
					if r.Float64() < *interceptRate {
						action = processor.ActionIntercept
					} else {
						action = processor.ActionAllow
					}

					// 创建决策
					decision := processor.NewDecision(
						fmt.Sprintf("%d", currentID),
						action,
						s.SystemID,
						fmt.Sprintf("延迟 %v 的%s决策", delay, action),
					)

					// 添加决策
					if added := tracker.AddDecision(decision); added {
						logger.Info(fmt.Sprintf("添加决策: %s", decision.String()))
					} else {
						logger.Error(fmt.Sprintf("决策添加失败: %s", decision.String()))
					}
				}(session)
			}

			// 不等待所有客户端响应，让超时机制发挥作用

			// 等待决策完成
			go func() {
				decision := tracker.WaitForCompletion()
				logger.Info(fmt.Sprintf("最终决策: %s", decision.String()))
			}()
		}
	}()

	// 定期打印状态信息
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stats := responseProcessor.GetStats()
				logger.Info(fmt.Sprintf("处理器状态: 已处理=%d, 拦截=%d, 放行=%d, 超时=%d, 活跃跟踪器=%d",
					stats["messages_processed"],
					stats["intercept_count"],
					stats["allow_count"],
					stats["timeout_count"],
					stats["active_trackers"]))

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
	responseProcessor.Stop()
	smmcServer.Stop()

	logger.Info("系统已关闭")
}

/*
自包含测试：processor_test 程序内部模拟了：

SMMC服务器

外部网元会话

消息生成

响应处理

测试流程：

程序启动后创建响应处理器和模拟会话

每秒生成一条测试消息

为每条消息创建跟踪器

模拟多个客户端以不同延迟返回决策

验证决策合并规则和超时机制

验证要点：

观察日志中的"最终决策"是否符合规则

检查统计数据中的拦截/放行/超时计数

验证拦截优先和先到先得规则是否生效
*/
