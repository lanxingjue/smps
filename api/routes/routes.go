// api/routes/routes.go
package routes

import (
	"database/sql"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"smps/api/handlers"
	"smps/api/middleware"
	"smps/internal/auth"
	"smps/internal/client"
	"smps/internal/dispatcher"
	"smps/internal/processor"
	"smps/internal/server"
	"smps/internal/tracer"
)

// SetupRoutes 设置路由
func SetupRoutes(
	engine *gin.Engine,
	staticDir string,
	templatesDir string,
	db *sql.DB,
	authenticator *auth.Authenticator,
	smmcServer *server.Server,
	smscClient *client.SMSCClient,
	dispatcher *dispatcher.MessageDispatcher,
	respProcessor *processor.ResponseProcessor,
	protocolTracer *tracer.ProtocolTracer,
) {
	// 设置静态文件目录
	engine.Static("/static", staticDir)

	// 加载HTML模板
	engine.LoadHTMLGlob(filepath.Join(templatesDir, "*.html"))

	// 创建处理器
	authHandler := handlers.NewAuthHandler(db)
	sessionHandler := handlers.NewSessionHandler(smmcServer)
	configHandler := handlers.NewSMMCConfigHandler(db, authenticator)
	traceHandler := handlers.NewTraceHandler(protocolTracer, db)
	statsHandler := handlers.NewStatsHandler(smmcServer, smscClient, dispatcher, respProcessor)

	// JWT中间件
	jwtMiddleware := middleware.JWTAuth()

	// 公共路由
	engine.GET("/", func(c *gin.Context) {
		c.Redirect(301, "/dashboard")
	})

	// 登录页面
	engine.GET("/login", func(c *gin.Context) {
		c.HTML(200, "login.html", gin.H{
			"title": "登录 - SMPS管理系统",
		})
	})

	// 需要认证的页面路由
	authorized := engine.Group("/")
	authorized.Use(jwtMiddleware)
	{
		authorized.GET("/dashboard", func(c *gin.Context) {
			c.HTML(200, "dashboard.html", gin.H{
				"title": "仪表盘 - SMPS管理系统",
				"user":  c.GetString("username"),
			})
		})

		authorized.GET("/sessions", func(c *gin.Context) {
			c.HTML(200, "sessions.html", gin.H{
				"title": "会话管理 - SMPS管理系统",
				"user":  c.GetString("username"),
			})
		})

		authorized.GET("/configs", func(c *gin.Context) {
			c.HTML(200, "configs.html", gin.H{
				"title": "配置管理 - SMPS管理系统",
				"user":  c.GetString("username"),
			})
		})

		authorized.GET("/traces", func(c *gin.Context) {
			c.HTML(200, "traces.html", gin.H{
				"title": "协议跟踪 - SMPS管理系统",
				"user":  c.GetString("username"),
			})
		})
	}

	// API路由
	api := engine.Group("/api")
	{
		// 认证API
		auth := api.Group("/auth")
		{
			auth.POST("/login", authHandler.Login)
			auth.POST("/logout", authHandler.Logout)
		}

		// 需要认证的API
		authorized := api.Group("/")
		authorized.Use(jwtMiddleware)
		{
			// 会话管理API
			sessions := authorized.Group("/sessions")
			{
				sessions.GET("", sessionHandler.ListSessions)
				sessions.GET("/:id", sessionHandler.GetSession)
				sessions.DELETE("/:id", sessionHandler.CloseSession)
			}

			// SMMC配置API
			configs := authorized.Group("/configs")
			{
				configs.GET("", configHandler.ListConfigs)
				configs.POST("", configHandler.CreateConfig)
				configs.GET("/:id", configHandler.GetConfig)
				configs.PUT("/:id", configHandler.UpdateConfig)
				configs.DELETE("/:id", configHandler.DeleteConfig)
			}

			// 协议跟踪API
			traces := authorized.Group("/traces")
			{
				traces.GET("", traceHandler.ListTraces)
				traces.POST("/settings", traceHandler.UpdateSettings)
				traces.GET("/settings", traceHandler.GetSettings)
				traces.POST("/numbers", traceHandler.AddTracedNumber)
				traces.DELETE("/numbers/:number", traceHandler.RemoveTracedNumber)
			}

			// 统计信息API
			stats := authorized.Group("/stats")
			{
				stats.GET("", statsHandler.GetStats)
				stats.GET("/realtime", statsHandler.GetRealtimeStats)
			}
		}
	}
}
