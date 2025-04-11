// cmd/web_server/main.go
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"smps/api"
	"smps/internal/auth"
	"smps/internal/client"
	"smps/internal/dispatcher"
	"smps/internal/processor"
	"smps/internal/server"
	"smps/internal/tracer"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	// configFile := flag.String("config", "config/config.yaml", "配置文件路径")
	listenAddr := flag.String("listen", ":8080", "Web服务器监听地址")
	staticDir := flag.String("static", "web/assets", "静态文件目录")
	templatesDir := flag.String("templates", "web/templates", "模板目录")
	dbConn := flag.String("db", "user:password@tcp(localhost:3306)/smps", "数据库连接字符串")
	debug := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	// 初始化日志
	logger.Init("smps_web")

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 连接数据库
	var db *sql.DB
	var err error
	if *dbConn != "" {
		db, err = sql.Open("mysql", *dbConn)
		if err != nil {
			logger.Error(fmt.Sprintf("连接数据库失败: %v", err))
		} else {
			// 测试数据库连接
			if err := db.Ping(); err != nil {
				logger.Error(fmt.Sprintf("数据库连接测试失败: %v", err))
				db = nil
			} else {
				logger.Info("数据库连接成功")
			}
		}
	}

	// 创建认证器
	authenticator := auth.NewAuthenticator(db, false)

	// 加载账户
	if authenticator != nil {
		if err := authenticator.LoadAccounts(); err != nil {
			logger.Error(fmt.Sprintf("加载账户失败: %v", err))
		} else {
			logger.Info("账户加载成功")
		}
	}

	// 创建组件 (模拟，实际项目中应该连接真实的组件)
	// 注意: 实际系统中应该使用第2-6步创建的真实组件而不是这些模拟组件
	smmcServer := createMockSMMCServer(authenticator)
	smscClient := createMockSMSCClient()
	msgDispatcher := createMockDispatcher()
	respProcessor := createMockProcessor()
	protocolTracer := createMockTracer()

	// 启动组件
	if err := smmcServer.Start(ctx); err != nil {
		logger.Error(fmt.Sprintf("启动SMMC服务器失败: %v", err))
	}

	// 创建Web服务器配置
	serverConfig := &api.ServerConfig{
		ListenAddr:   *listenAddr,
		StaticDir:    *staticDir,
		TemplatesDir: *templatesDir,
		SessionTTL:   24 * time.Hour,
		Debug:        *debug,
	}

	// 创建Web服务器
	webServer := api.NewServer(
		serverConfig,
		db,
		authenticator,
		smmcServer,
		smscClient,
		msgDispatcher,
		respProcessor,
		protocolTracer,
	)

	// 启动Web服务器
	go func() {
		logger.Info(fmt.Sprintf("Web服务器正在启动，监听地址: %s", *listenAddr))
		if err := webServer.Start(); err != nil {
			logger.Error(fmt.Sprintf("Web服务器启动失败: %v", err))
		}
	}()

	// 注册信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	<-sigCh
	logger.Info("接收到关闭信号，开始优雅关闭...")

	// 优雅关闭Web服务器
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := webServer.Stop(shutdownCtx); err != nil {
		logger.Error(fmt.Sprintf("Web服务器关闭失败: %v", err))
	}

	// 停止组件
	cancel()

	logger.Info("系统已关闭")
}

// 创建模拟的SMMC服务器 (仅用于演示)
func createMockSMMCServer(authenticator *auth.Authenticator) *server.Server {
	config := &server.ServerConfig{
		ListenAddress:          ":2775",
		MaxConnections:         100,
		ReadTimeout:            30 * time.Second,
		WriteTimeout:           30 * time.Second,
		HeartbeatInterval:      5 * time.Second,
		HeartbeatMissThreshold: 3,
	}

	return server.NewServer(config, authenticator)
}

// 创建模拟的SMSC客户端 (仅用于演示)
func createMockSMSCClient() *client.SMSCClient {
	config := &client.Config{
		Address:           "localhost:2775",
		SystemID:          "test",
		Password:          "password",
		EnquireInterval:   60 * time.Second,
		ResponseTimeout:   5 * time.Second,
		ReconnectInterval: 5 * time.Second,
		MaxRetries:        5,
		BackoffFactor:     1.5,
	}

	return client.NewSMSCClient(config)
}

// 创建模拟的消息分发器 (仅用于演示)
func createMockDispatcher() *dispatcher.MessageDispatcher {
	return dispatcher.NewMessageDispatcher(1000, 5, 2*time.Second)
}

// 创建模拟的响应处理器 (仅用于演示)
func createMockProcessor() *processor.ResponseProcessor {
	config := processor.DefaultResponseConfig()
	return processor.NewResponseProcessor(config)
}

// 创建模拟的协议跟踪器 (仅用于演示)
func createMockTracer() *tracer.ProtocolTracer {
	config := tracer.NewTracerConfig()
	return tracer.NewProtocolTracer(config, nil)
}
