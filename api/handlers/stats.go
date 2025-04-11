// api/handlers/stats.go
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"smps/internal/client"
	"smps/internal/dispatcher"
	"smps/internal/processor"
	"smps/internal/server"
)

// StatsHandler 统计信息处理器
type StatsHandler struct {
	server        *server.Server
	client        *client.SMSCClient
	dispatcher    *dispatcher.MessageDispatcher
	respProcessor *processor.ResponseProcessor
}

// NewStatsHandler 创建统计信息处理器
func NewStatsHandler(
	server *server.Server,
	client *client.SMSCClient,
	dispatcher *dispatcher.MessageDispatcher,
	respProcessor *processor.ResponseProcessor,
) *StatsHandler {
	return &StatsHandler{
		server:        server,
		client:        client,
		dispatcher:    dispatcher,
		respProcessor: respProcessor,
	}
}

// SystemStatus 系统状态
type SystemStatus struct {
	ServerStatus struct {
		ActiveConnections int    `json:"active_connections"`
		TotalConnections  uint64 `json:"total_connections"`
		ReceivedMessages  uint64 `json:"received_messages"`
		SentMessages      uint64 `json:"sent_messages"`
		AuthFailures      uint64 `json:"auth_failures"`
	} `json:"server_status"`

	ClientStatus struct {
		Status           string `json:"status"`
		SentMessages     uint64 `json:"sent_messages"`
		ReceivedMessages uint64 `json:"received_messages"`
		ReconnectCount   uint64 `json:"reconnect_count"`
		Errors           uint64 `json:"errors"`
	} `json:"client_status"`

	DispatcherStatus struct {
		Dispatched    uint64  `json:"dispatched"`
		Errors        uint64  `json:"errors"`
		AvgTimeMs     float64 `json:"avg_time_ms"`
		QueueSize     int     `json:"queue_size"`
		QueueCapacity int     `json:"queue_capacity"`
	} `json:"dispatcher_status"`

	ProcessorStatus struct {
		MessagesProcessed uint64 `json:"messages_processed"`
		InterceptCount    uint64 `json:"intercept_count"`
		AllowCount        uint64 `json:"allow_count"`
		TimeoutCount      uint64 `json:"timeout_count"`
		ActiveTrackers    int    `json:"active_trackers"`
	} `json:"processor_status"`

	SystemTime string `json:"system_time"`
}

// GetStats 获取统计信息
func (h *StatsHandler) GetStats(c *gin.Context) {
	status := SystemStatus{}

	// 填充服务器状态
	if h.server != nil {
		serverStats := h.server.GetStats()
		status.ServerStatus.ActiveConnections = int(serverStats["active_connections"].(int64))
		status.ServerStatus.TotalConnections = serverStats["total_connections"].(uint64)
		status.ServerStatus.ReceivedMessages = serverStats["received_messages"].(uint64)
		status.ServerStatus.SentMessages = serverStats["sent_messages"].(uint64)
		status.ServerStatus.AuthFailures = serverStats["auth_failures"].(uint64)
	}

	// 填充客户端状态
	if h.client != nil {
		status.ClientStatus.Status = h.client.GetStatus()
		clientStats := h.client.GetStats()
		status.ClientStatus.SentMessages = clientStats["sent_messages"]
		status.ClientStatus.ReceivedMessages = clientStats["received_messages"]
		status.ClientStatus.ReconnectCount = clientStats["reconnect_count"]
		status.ClientStatus.Errors = clientStats["errors"]
	}

	// 填充分发器状态
	if h.dispatcher != nil {
		dispatcherStats := h.dispatcher.GetStats()
		status.DispatcherStatus.Dispatched = dispatcherStats["dispatched"].(uint64)
		status.DispatcherStatus.Errors = dispatcherStats["errors"].(uint64)
		status.DispatcherStatus.AvgTimeMs = dispatcherStats["avg_time_ms"].(float64)
		status.DispatcherStatus.QueueSize = dispatcherStats["queue_size"].(int)
		status.DispatcherStatus.QueueCapacity = dispatcherStats["queue_capacity"].(int)
	}

	// 填充处理器状态
	if h.respProcessor != nil {
		processorStats := h.respProcessor.GetStats()
		status.ProcessorStatus.MessagesProcessed = processorStats["messages_processed"].(uint64)
		status.ProcessorStatus.InterceptCount = processorStats["intercept_count"].(uint64)
		status.ProcessorStatus.AllowCount = processorStats["allow_count"].(uint64)
		status.ProcessorStatus.TimeoutCount = processorStats["timeout_count"].(uint64)
		status.ProcessorStatus.ActiveTrackers = processorStats["active_trackers"].(int)
	}

	// 填充系统时间
	status.SystemTime = time.Now().Format(time.RFC3339)

	c.JSON(http.StatusOK, status)
}

// GetRealtimeStats 获取实时统计信息
func (h *StatsHandler) GetRealtimeStats(c *gin.Context) {
	// 简化版实时统计
	stats := map[string]interface{}{
		"active_connections":  0,
		"messages_per_second": 0,
		"cpu_usage":           0,
		"memory_usage":        0,
	}

	if h.server != nil {
		serverStats := h.server.GetStats()
		stats["active_connections"] = serverStats["active_connections"]
	}

	c.JSON(http.StatusOK, stats)
}
