package server

import (
	"fmt"
	"sync"
	"time"

	"smps/pkg/logger"
)

// SessionManager 会话管理器
type SessionManager struct {
	sessions      sync.Map
	nextID        uint64
	cleanupTicker *time.Ticker
	done          chan struct{}
}

// NewSessionManager 创建新的会话管理器
func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		nextID: 1,
		done:   make(chan struct{}),
	}

	// 启动会话清理器
	sm.startCleaner(5 * time.Minute)

	return sm
}

// startCleaner 启动会话清理器
func (m *SessionManager) startCleaner(interval time.Duration) {
	m.cleanupTicker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-m.cleanupTicker.C:
				m.cleanupSessions()
			case <-m.done:
				m.cleanupTicker.Stop()
				return
			}
		}
	}()
}

// cleanupSessions 清理过期会话
func (m *SessionManager) cleanupSessions() {
	threshold := 24 * time.Hour

	m.sessions.Range(func(key, value interface{}) bool {
		session := value.(*Session)

		// 清理已关闭或长时间不活跃的会话
		if session.Status == 3 || session.IdleTime() > threshold {
			session.Conn.Close()
			m.sessions.Delete(key)
			logger.Info(fmt.Sprintf("清理过期会话: ID=%d, SystemID=%s, 空闲时间=%v",
				session.ID, session.SystemID, session.IdleTime()))
		}

		return true
	})
}

// Add 添加会话
func (m *SessionManager) Add(session *Session) {
	m.sessions.Store(session.ID, session)
}

// Get 获取会话
func (m *SessionManager) Get(id uint64) (*Session, bool) {
	value, ok := m.sessions.Load(id)
	if !ok {
		return nil, false
	}
	return value.(*Session), true
}

// Remove 移除会话
func (m *SessionManager) Remove(id uint64) {
	m.sessions.Delete(id)
}

// Range 遍历所有会话
func (m *SessionManager) Range(f func(*Session) bool) {
	m.sessions.Range(func(key, value interface{}) bool {
		return f(value.(*Session))
	})
}

// Count 获取会话数量
func (m *SessionManager) Count() int {
	count := 0
	m.sessions.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// CloseAll 关闭所有会话
func (m *SessionManager) CloseAll() {
	m.sessions.Range(func(key, value interface{}) bool {
		session := value.(*Session)
		session.Conn.Close()
		session.Status = 3 // Session_Closed
		m.sessions.Delete(key)
		return true
	})
}

// GetBySystemID 根据系统ID获取会话
func (m *SessionManager) GetBySystemID(systemID string) []*Session {
	var result []*Session
	m.sessions.Range(func(_, value interface{}) bool {
		session := value.(*Session)
		if session.SystemID == systemID {
			result = append(result, session)
		}
		return true
	})
	return result
}

// GetActiveSessions 获取所有活动会话
func (m *SessionManager) GetActiveSessions() []*Session {
	var activeSessions []*Session
	m.sessions.Range(func(_, value interface{}) bool {
		session := value.(*Session)
		activeSessions = append(activeSessions, session)
		return true
	})
	return activeSessions
}

// CloseSession 关闭指定的会话
func (m *SessionManager) CloseSession(sessionID uint64) error {
	value, ok := m.sessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("会话ID %d 不存在", sessionID)
	}

	session := value.(*Session)
	if err := session.Conn.Close(); err != nil {
		return err
	}

	session.Status = 3 // Session_Closed
	m.sessions.Delete(sessionID)
	return nil
}

// Stop 停止会话管理器
func (m *SessionManager) Stop() {
	close(m.done)
	m.CloseAll()
}
