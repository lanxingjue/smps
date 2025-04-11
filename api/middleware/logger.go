// api/middleware/logger.go
package middleware

import (
	"bytes"
	"time"

	"github.com/gin-gonic/gin"

	"smps/pkg/logger"
)

// Logger 日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// 创建响应体的副本
		bodyWriter := &bodyWriterWrapper{
			ResponseWriter: c.Writer,
			body:           bytes.NewBufferString(""),
		}
		c.Writer = bodyWriter

		// 处理请求
		c.Next()

		// 记录响应时间
		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()

		if raw != "" {
			path = path + "?" + raw
		}

		// 构建日志信息
		logInfo := map[string]interface{}{
			"status":     statusCode,
			"latency":    latency,
			"client_ip":  clientIP,
			"method":     method,
			"path":       path,
			"user_agent": c.Request.UserAgent(),
		}

		// 记录请求日志
		if statusCode >= 400 {
			// 错误日志记录更多详情
			logInfo["body"] = bodyWriter.body.String()
			logger.Error("HTTP请求错误", logInfo)
		} else {
			logger.Info("HTTP请求", logInfo)
		}
	}
}

// 响应体包装器，用于捕获响应体
type bodyWriterWrapper struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyWriterWrapper) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyWriterWrapper) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}
