// internal/server/session.go
package server

import (
	"net"
	"sync/atomic"
	"time"
)

// Session 表示客户端会话
type Session struct {
	ID           uint64
	SystemID     string
	Status       int
	Conn         net.Conn
	CreatedAt    time.Time
	LastActivity int64
	sequenceNum  uint32
	// lock         sync.Mutex

	// 增强字段
	Protocol      string
	InterfaceType string
	EncodingType  string
	FlowControl   int
	RemoteAddr    string
	RemotePort    int

	// 统计信息
	ReceivedMsgs    uint64
	SentMsgs        uint64
	Errors          uint64
	HeartbeatMisses int
}

// NewSession 创建新会话
func NewSession(conn net.Conn) *Session {
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)

	return &Session{
		ID:           generateSessionID(),
		Conn:         conn,
		Status:       0, // Session_Start
		CreatedAt:    time.Now(),
		LastActivity: time.Now().UnixNano(), // 使用纳秒时间戳而不是time.Time
		sequenceNum:  1,
		RemoteAddr:   remoteAddr.IP.String(),
		RemotePort:   remoteAddr.Port,
	}
}

// NextSequence 获取下一个序列号
func (s *Session) NextSequence() uint32 {
	return atomic.AddUint32(&s.sequenceNum, 1)
}

// UpdateActivity 更新最后活动时间
func (s *Session) UpdateActivity() {
	// atomic.StoreUint64((*uint64)(&s.LastActivity), uint64(time.Now().UnixNano()))
	atomic.StoreInt64(&s.LastActivity, time.Now().UnixNano())
}

// IncrementSent 增加已发送消息计数
func (s *Session) IncrementSent() {
	atomic.AddUint64(&s.SentMsgs, 1)
}

// IncrementReceived 增加已接收消息计数
func (s *Session) IncrementReceived() {
	atomic.AddUint64(&s.ReceivedMsgs, 1)
}

// IncrementErrors 增加错误计数
func (s *Session) IncrementErrors() {
	atomic.AddUint64(&s.Errors, 1)
}

// IdleTime 获取空闲时间
func (s *Session) IdleTime() time.Duration {
	// 将int64纳秒时间戳转为time.Time并计算间隔
	lastActivity := time.Unix(0, atomic.LoadInt64(&s.LastActivity))
	return time.Since(lastActivity)
}

// 全局会话ID计数器
var globalSessionID uint64 = 0

// generateSessionID 生成会话ID
func generateSessionID() uint64 {
	return atomic.AddUint64(&globalSessionID, 1)
}
