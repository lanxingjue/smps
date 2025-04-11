// api/handlers/trace.go
package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"smps/internal/tracer"
)

// TraceHandler 协议跟踪处理器
type TraceHandler struct {
	tracer *tracer.ProtocolTracer
	db     *sql.DB
}

// NewTraceHandler 创建协议跟踪处理器
func NewTraceHandler(tracer *tracer.ProtocolTracer, db *sql.DB) *TraceHandler {
	return &TraceHandler{
		tracer: tracer,
		db:     db,
	}
}

// TraceSettings 跟踪设置
type TraceSettings struct {
	Enabled       bool     `json:"enabled"`
	ParseContent  bool     `json:"parse_content"`
	TracedNumbers []string `json:"traced_numbers"`
}

// TraceLogResponse 跟踪日志响应
type TraceLogResponse struct {
	Timestamp   time.Time `json:"timestamp"`
	Direction   string    `json:"direction"`
	MessageID   uint32    `json:"message_id"`
	CommandName string    `json:"command_name"`
	SourceAddr  string    `json:"source_addr"`
	DestAddr    string    `json:"dest_addr"`
	Content     string    `json:"content,omitempty"`
	SystemID    string    `json:"system_id"`
	Status      uint32    `json:"status"`
}

// ListTraces 列出跟踪日志
func (h *TraceHandler) ListTraces(c *gin.Context) {
	// 获取查询参数
	startTimeStr := c.Query("start")
	endTimeStr := c.Query("end")
	sourceAddr := c.Query("source")
	destAddr := c.Query("dest")
	limitStr := c.Query("limit")
	offsetStr := c.Query("offset")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的开始时间格式"})
			return
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结束时间格式"})
			return
		}
	}

	limit := 100 // 默认限制
	if limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的限制参数"})
			return
		}
	}

	offset := 0 // 默认偏移
	if offsetStr != "" {
		offset, err = strconv.Atoi(offsetStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的偏移参数"})
			return
		}
	}

	// 构建查询选项
	options := tracer.QueryOptions{
		StartTime:  startTime,
		EndTime:    endTime,
		SourceAddr: sourceAddr,
		DestAddr:   destAddr,
		Limit:      limit,
		Offset:     offset,
	}

	// 查询日志
	logs, err := h.tracer.QueryLogs(options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为响应格式
	var response []TraceLogResponse
	for _, log := range logs {
		response = append(response, TraceLogResponse{
			Timestamp:   log.Timestamp,
			Direction:   string(log.Direction),
			MessageID:   log.MessageID,
			CommandName: log.CommandName,
			SourceAddr:  log.SourceAddr,
			DestAddr:    log.DestAddr,
			Content:     log.Content,
			SystemID:    log.SystemID,
			Status:      log.Status,
		})
	}

	c.JSON(http.StatusOK, response)
}

// GetSettings 获取跟踪设置
func (h *TraceHandler) GetSettings(c *gin.Context) {
	// 获取跟踪号码
	numbers := []string{}

	// 如果有数据库连接，从数据库获取
	if h.db != nil {
		rows, err := h.db.Query("SELECT number FROM traced_numbers")
		if err == nil {
			defer rows.Close()

			for rows.Next() {
				var number string
				if err := rows.Scan(&number); err == nil {
					numbers = append(numbers, number)
				}
			}
		}
	} else {
		// 从内存获取
		// 这里需要访问Tracer的配置，可能需要添加相应方法
	}

	settings := TraceSettings{
		Enabled:       h.tracer.IsEnabled(),
		ParseContent:  h.tracer.IsParseContentEnabled(),
		TracedNumbers: numbers,
	}

	c.JSON(http.StatusOK, settings)
}

// UpdateSettings 更新跟踪设置
func (h *TraceHandler) UpdateSettings(c *gin.Context) {
	var settings TraceSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新设置
	h.tracer.SetEnabled(settings.Enabled)
	h.tracer.SetParseContent(settings.ParseContent)

	c.JSON(http.StatusOK, settings)
}

// AddTracedNumber 添加跟踪号码
func (h *TraceHandler) AddTracedNumber(c *gin.Context) {
	var request struct {
		Number string `json:"number" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 添加到跟踪器
	h.tracer.AddTracedNumber(request.Number)

	// 如果有数据库连接，保存到数据库
	if h.db != nil {
		_, err := h.db.Exec("INSERT INTO traced_numbers (number) VALUES (?)", request.Number)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "号码已添加到跟踪列表"})
}

// RemoveTracedNumber 移除跟踪号码
func (h *TraceHandler) RemoveTracedNumber(c *gin.Context) {
	number := c.Param("number")

	// 从跟踪器移除
	h.tracer.RemoveTracedNumber(number)

	// 如果有数据库连接，从数据库移除
	if h.db != nil {
		_, err := h.db.Exec("DELETE FROM traced_numbers WHERE number = ?", number)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "号码已从跟踪列表移除"})
}
