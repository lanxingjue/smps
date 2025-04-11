// api/handlers/session.go
package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"smps/internal/protocol"
	"smps/internal/server"
)

// SessionHandler 会话处理器
type SessionHandler struct {
	server *server.Server
}

// NewSessionHandler 创建会话处理器
func NewSessionHandler(server *server.Server) *SessionHandler {
	return &SessionHandler{
		server: server,
	}
}

// SessionResponse 会话响应
type SessionResponse struct {
	ID            uint64    `json:"id"`
	SystemID      string    `json:"system_id"`
	Status        string    `json:"status"`
	RemoteAddr    string    `json:"remote_addr"`
	RemotePort    int       `json:"remote_port"`
	CreatedAt     time.Time `json:"created_at"`
	IdleTime      string    `json:"idle_time"`
	Protocol      string    `json:"protocol"`
	InterfaceType string    `json:"interface_type"`
	ReceivedMsgs  uint64    `json:"received_msgs"`
	SentMsgs      uint64    `json:"sent_msgs"`
}

// ListSessions 列出所有会话
func (h *SessionHandler) ListSessions(c *gin.Context) {
	sessions := h.server.GetSessionManager().GetActiveSessions()

	// 转换为响应格式
	var response []SessionResponse
	for _, s := range sessions {
		response = append(response, convertSessionToResponse(s))
	}

	c.JSON(http.StatusOK, response)
}

// GetSession 获取会话详情
func (h *SessionHandler) GetSession(c *gin.Context) {
	idStr := c.Param("id")

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的会话ID"})
		return
	}

	session, exists := h.server.GetSessionManager().Get(id)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}

	c.JSON(http.StatusOK, convertSessionToResponse(session))
}

// CloseSession 关闭会话
func (h *SessionHandler) CloseSession(c *gin.Context) {
	idStr := c.Param("id")

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的会话ID"})
		return
	}

	if err := h.server.GetSessionManager().CloseSession(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "会话已关闭"})
}

// 转换会话到响应格式
func convertSessionToResponse(s *server.Session) SessionResponse {
	// 获取状态文本
	statusText := "未知"
	switch s.Status {
	case protocol.Session_Start:
		statusText = "初始化"
	case protocol.Session_Connected:
		statusText = "已连接"
	case protocol.Session_Disconnect:
		statusText = "已断开"
	case protocol.Session_Closed:
		statusText = "已关闭"
	}

	return SessionResponse{
		ID:            s.ID,
		SystemID:      s.SystemID,
		Status:        statusText,
		RemoteAddr:    s.RemoteAddr,
		RemotePort:    s.RemotePort,
		CreatedAt:     s.CreatedAt,
		IdleTime:      s.IdleTime().String(),
		Protocol:      s.Protocol,
		InterfaceType: s.InterfaceType,
		ReceivedMsgs:  s.ReceivedMsgs,
		SentMsgs:      s.SentMsgs,
	}
}
