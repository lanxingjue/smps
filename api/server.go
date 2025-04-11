// api/server.go
package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"smps/api/middleware"
	"smps/api/routes"
	"smps/internal/auth"
	"smps/internal/client"
	"smps/internal/dispatcher"
	"smps/internal/processor"
	"smps/internal/server"
	"smps/internal/tracer"
	"smps/pkg/logger"
)

// ServerConfig Web服务器配置
type ServerConfig struct {
	ListenAddr   string        // 监听地址
	StaticDir    string        // 静态文件目录
	TemplatesDir string        // 模板目录
	SessionTTL   time.Duration // 会话有效期
	Debug        bool          // 调试模式
}

// DefaultServerConfig 默认配置
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		ListenAddr:   ":8080",
		StaticDir:    "web/assets",
		TemplatesDir: "web/templates",
		SessionTTL:   24 * time.Hour,
		Debug:        false,
	}
}

// Server Web服务器
type Server struct {
	config         *ServerConfig
	engine         *gin.Engine
	httpServer     *http.Server
	db             *sql.DB
	authenticator  *auth.Authenticator
	smmcServer     *server.Server
	smscClient     *client.SMSCClient
	dispatcher     *dispatcher.MessageDispatcher
	respProcessor  *processor.ResponseProcessor
	protocolTracer *tracer.ProtocolTracer
}

// NewServer 创建新的Web服务器
func NewServer(
	config *ServerConfig,
	db *sql.DB,
	authenticator *auth.Authenticator,
	smmcServer *server.Server,
	smscClient *client.SMSCClient,
	dispatcher *dispatcher.MessageDispatcher,
	respProcessor *processor.ResponseProcessor,
	protocolTracer *tracer.ProtocolTracer,
) *Server {
	if config == nil {
		config = DefaultServerConfig()
	}

	// 设置Gin模式
	if config.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建Gin引擎
	engine := gin.New()

	// 使用日志和恢复中间件
	engine.Use(middleware.Logger())
	engine.Use(gin.Recovery())

	return &Server{
		config:         config,
		engine:         engine,
		db:             db,
		authenticator:  authenticator,
		smmcServer:     smmcServer,
		smscClient:     smscClient,
		dispatcher:     dispatcher,
		respProcessor:  respProcessor,
		protocolTracer: protocolTracer,
	}
}

// Start 启动Web服务器
func (s *Server) Start() error {
	// 初始化路由
	routes.SetupRoutes(
		s.engine,
		s.config.StaticDir,
		s.config.TemplatesDir,
		s.db,
		s.authenticator,
		s.smmcServer,
		s.smscClient,
		s.dispatcher,
		s.respProcessor,
		s.protocolTracer,
	)

	// 创建HTTP服务器
	s.httpServer = &http.Server{
		Addr:         s.config.ListenAddr,
		Handler:      s.engine,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 启动HTTP服务器
	logger.Info(fmt.Sprintf("Web服务器监听于 %s", s.config.ListenAddr))
	return s.httpServer.ListenAndServe()
}

// Stop 停止Web服务器
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
