package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smps/api"
	"smps/internal/auth"
	"smps/internal/client"
	"smps/internal/config"
	"smps/internal/database"
	"smps/internal/dispatcher"
	"smps/internal/ha"
	"smps/internal/performance"
	"smps/internal/processor"
	"smps/internal/protocol"
	"smps/internal/server"
	"smps/internal/tracer"
	"smps/pkg/logger"
)

// // 添加在main.go文件中，import部分下方

// // 定义两种不同类型的适配器，分别处理Server和Processor
// type serverAdapter struct {
// 	server *server.Server
// }

// type processorAdapter struct {
// 	processor *processor.ResponseProcessor
// }

// // Server适配器实现HandleMessage方法
// func (a *serverAdapter) HandleMessage(msg *protocol.Message) error {
// 	return a.server.HandleMessage(msg)
// }

// // Server适配器实现HandlerName方法(缺失的方法)
// func (a *serverAdapter) HandlerName() string {
// 	return "SMMC服务器"
// }

// // Processor适配器实现HandleMessage方法(缺失的方法)
// func (a *processorAdapter) HandleMessage(msg *protocol.Message) error {
// 	// 调用ProcessResponse方法来处理消息
// 	_, err := a.processor.ProcessResponse(msg)
// 	return err
// }

// // Processor适配器实现HandlerName方法
// func (a *processorAdapter) HandlerName() string {
// 	return "响应处理器"
// }

// 创建适配器函数，将MessageDispatcher.Dispatch的void调用转为返回error
func dispatchAdapter(dispatcher *dispatcher.MessageDispatcher, msg *protocol.Message) error {
	dispatcher.Dispatch(msg)
	return nil
}

// 创建适配器函数，将[]byte转换为protocol.Message
func convertMessageAdapter(data []byte) (*protocol.Message, error) {
	return protocol.ParseMessage(data)
}

// 创建适配器函数，模拟TraceMessage方法
func traceMessageAdapter(tracer *tracer.ProtocolTracer, msg []byte, isIncoming bool) []byte {
	if !tracer.IsEnabled() {
		return msg
	}

	// 解析消息
	parsedMsg, err := protocol.ParseMessage(msg)
	if err != nil {
		logger.Error(fmt.Sprintf("解析消息失败: %v", err))
		return msg
	}

	// 根据方向调用不同的方法
	if isIncoming {
		tracer.TraceIncoming(parsedMsg, "unknown") // 这里可以从上下文获取systemID
	} else {
		tracer.TraceOutgoing(parsedMsg, "unknown")
	}

	return msg
}

func main() {
	// 解析命令行参数
	configFile := flag.String("config", "config/config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		fmt.Printf("加载配置文件失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	logger.Init(cfg.Log.LogFile)
	logger.Info("SMPS短信代理服务平台启动中...")

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 连接数据库
	dbManager := database.NewManager(cfg.Database)
	if err := dbManager.Connect(); err != nil {
		logger.Fatal(fmt.Sprintf("连接数据库失败: %v", err))
	}
	defer dbManager.Close()

	// 获取数据库连接
	db := dbManager.DB()

	// 执行数据库迁移
	if err := database.Migrate(db); err != nil {
		logger.Fatal(fmt.Sprintf("数据库迁移失败: %v", err))
	}

	// 创建认证器
	authenticator := auth.NewAuthenticator(db, cfg.Auth.IPWhitelist)
	if err := authenticator.LoadAccountsFromDB(); err != nil {
		logger.Error(fmt.Sprintf("从数据库加载账户失败: %v", err))
	}
	//保留，但没有用认证器
	// authenticator := auth.NewAuthenticator(db, false)

	// 创建SMMC服务器（接收外部网元连接）
	smmcServer := server.NewServer(cfg.SMMC, authenticator)

	// 创建SMSC客户端（连接短信中心）
	smscClient := client.NewSMSCClient(cfg.SMSC)

	// 创建消息分发器
	messageDispatcher := dispatcher.NewMessageDispatcher(
		cfg.Dispatcher.QueueSize,
		cfg.Dispatcher.Workers,
		cfg.Dispatcher.Timeout,
	)

	// 创建响应处理器
	respProcessor := processor.NewResponseProcessor(cfg.Processor)

	// 创建协议跟踪器
	tracerConfig := tracer.NewTracerConfig()
	protocolTracer := tracer.NewProtocolTracer(tracerConfig, db)
	if err := protocolTracer.LoadTracedNumbersFromDB(); err != nil {
		logger.Error(fmt.Sprintf("加载跟踪号码失败: %v", err))
	}
	if err := protocolTracer.LoadConfigFromDB(); err != nil {
		logger.Error(fmt.Sprintf("加载跟踪配置失败: %v", err))
	}

	// 初始化性能分析器
	profiler := performance.NewProfiler()
	if cfg.Performance.Enabled {
		if err := profiler.Start(); err != nil {
			logger.Error(fmt.Sprintf("启动性能分析器失败: %v", err))
		}
		defer profiler.Stop()

		// 启动性能指标收集
		metrics := performance.GetMetrics()
		performance.StartMetricsReporter(30 * time.Second)

		// 启动HTTP分析服务器
		if cfg.Performance.ProfilerHTTP {
			performance.InitProfilerServer(cfg.Performance.HTTPAddr)
		}

		// 创建优化建议生成器
		optimizer := performance.NewOptimizer(metrics, profiler)
		optimizer.StartOptimizationReporter(5 * time.Minute)
	}

	// 连接组件
	// // SMSC客户端连接到消息分发器
	// smscClient.SetMessageHandler(func(msg []byte) error {
	// 	message, err := convertMessageAdapter(msg)
	// 	if err != nil {
	// 		logger.Error(fmt.Sprintf("解析消息失败: %v", err))
	// 		return err
	// 	}
	// 	// 使用适配器调用Dispatch
	// 	return dispatchAdapter(messageDispatcher, message)
	// })

	// 改进的SMSC客户端消息处理适配器
	smscClient.SetMessageHandler(func(msg []byte) error {
		// 解析消息
		message, err := protocol.ParseMessage(msg)
		if err != nil {
			logger.Error(fmt.Sprintf("解析消息失败: %v", err))
			return err
		}

		// 记录接收到的消息
		if message.Header.CommandID == protocol.DELIVER_SM {
			logger.Info(fmt.Sprintf("SMSC客户端接收到DELIVER_SM消息，序列号: %d，准备分发",
				message.Header.SequenceNumber))
		}

		// 分发消息
		return messageDispatcher.Dispatch(message)
	})

	// 消息分发器连接到SMMC服务器和响应处理器
	// 替换AddHandler为RegisterHandler
	messageDispatcher.RegisterHandler(smmcServer)
	// // 替换SetResponseHandler为RegisterHandler
	messageDispatcher.RegisterHandler(respProcessor)
	// 改为使用适配器
	// // 改为使用专门的适配器
	// messageDispatcher.RegisterHandler(&serverAdapter{server: smmcServer})
	// messageDispatcher.RegisterHandler(&processorAdapter{processor: respProcessor})
	// SMMC服务器连接到消息分发器
	smmcServer.SetMessageHandler(func(msg []byte) error {
		message, err := convertMessageAdapter(msg)
		if err != nil {
			logger.Error(fmt.Sprintf("解析消息失败: %v", err))
			return err
		}
		// 使用适配器调用Dispatch
		return dispatchAdapter(messageDispatcher, message)
	})

	// 响应处理器连接到SMSC客户端
	// 省略SetSender调用，使用现有接口或替代方案

	// 添加协议跟踪
	smmcServer.AddMessageInterceptor(func(msg []byte, isIncoming bool) []byte {
		return traceMessageAdapter(protocolTracer, msg, isIncoming)
	})

	smscClient.AddMessageInterceptor(func(msg []byte, isIncoming bool) []byte {
		return traceMessageAdapter(protocolTracer, msg, isIncoming)
	})

	// 启动组件
	logger.Info("正在启动SMSC客户端...")
	// 使用Start方法而不是Connect
	if err := smscClient.Start(ctx); err != nil {
		logger.Error(fmt.Sprintf("启动SMSC客户端失败: %v", err))
	}

	logger.Info("正在启动消息分发器...")
	if err := messageDispatcher.Start(ctx); err != nil {
		logger.Error(fmt.Sprintf("启动消息分发器失败: %v", err))
	}

	logger.Info("正在启动响应处理器...")
	if err := respProcessor.Start(ctx); err != nil {
		logger.Error(fmt.Sprintf("启动响应处理器失败: %v", err))
	}

	logger.Info("正在启动SMMC服务器...")
	if err := smmcServer.Start(ctx); err != nil {
		logger.Error(fmt.Sprintf("启动SMMC服务器失败: %v", err))
		cancel()
		os.Exit(1)
	}

	// 如果启用高可用，启动高可用管理器
	var haManager *ha.Manager
	if cfg.HA != nil && cfg.HA.Enabled {
		logger.Info("正在启动高可用管理器...")
		haManager, err = ha.NewManager(cfg.HA)
		if err != nil {
			logger.Error(fmt.Sprintf("创建高可用管理器失败: %v", err))
		} else {
			if err := haManager.Start(); err != nil {
				logger.Error(fmt.Sprintf("启动高可用管理器失败: %v", err))
			}
		}
	}

	// 启动Web管理界面
	var webServer *api.Server
	if cfg.Web.Enabled {
		logger.Info("正在启动Web管理界面...")
		webConfig := &api.ServerConfig{
			ListenAddr:   cfg.Web.ListenAddr,
			StaticDir:    cfg.Web.StaticDir,
			TemplatesDir: cfg.Web.TemplatesDir,
			SessionTTL:   cfg.Web.SessionTTL,
			Debug:        cfg.Web.Debug,
		}

		webServer = api.NewServer(
			webConfig,
			db,
			authenticator,
			smmcServer,
			smscClient,
			messageDispatcher,
			respProcessor,
			protocolTracer,
		)

		go func() {
			if err := webServer.Start(); err != nil {
				logger.Error(fmt.Sprintf("启动Web管理界面失败: %v", err))
			}
		}()
	}

	// 输出启动成功消息
	logger.Info(fmt.Sprintf("SMPS短信代理服务平台已启动，版本: %s", cfg.Version))
	logger.Info(fmt.Sprintf("SMMC监听地址: %s", cfg.SMMC.ListenAddress))
	if cfg.Web.Enabled {
		logger.Info(fmt.Sprintf("Web管理界面地址: %s", cfg.Web.ListenAddr))
	}

	// 注册信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	sig := <-sigCh
	logger.Info(fmt.Sprintf("接收到信号 %s，开始优雅关闭...", sig))

	// 关闭组件
	if webServer != nil {
		logger.Info("正在关闭Web管理界面...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		webServer.Stop(shutdownCtx)
	}

	if haManager != nil {
		logger.Info("正在关闭高可用管理器...")
		haManager.Stop()
	}

	// 发送取消信号
	cancel()

	// 优雅关闭各组件
	logger.Info("正在关闭SMMC服务器...")
	smmcServer.Stop()

	logger.Info("正在关闭响应处理器...")
	respProcessor.Stop()

	logger.Info("正在关闭消息分发器...")
	messageDispatcher.Stop()

	logger.Info("正在关闭SMSC客户端...")
	// 使用Stop方法而不是Close
	smscClient.Stop()

	logger.Info("SMPS短信代理服务平台已关闭")
}
