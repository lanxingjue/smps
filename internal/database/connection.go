// internal/database/connection.go
package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"smps/pkg/logger"
)

// Manager 数据库连接管理器
type Manager struct {
	config    *Config
	db        *sql.DB
	mu        sync.Mutex
	connected bool
}

// NewManager 创建数据库管理器
func NewManager(config *Config) *Manager {
	if config == nil {
		config = NewConfig()
	}

	return &Manager{
		config:    config,
		connected: false,
	}
}

// Connect 连接数据库
func (m *Manager) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return nil
	}

	// 打开数据库连接
	db, err := sql.Open(m.config.Driver, m.config.DSN())
	if err != nil {
		return fmt.Errorf("打开数据库连接失败: %v", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(m.config.MaxOpenConns)
	db.SetMaxIdleConns(m.config.MaxIdleConns)
	db.SetConnMaxLifetime(m.config.ConnMaxLifetime)

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("数据库连接测试失败: %v", err)
	}

	m.db = db
	m.connected = true

	logger.Info(fmt.Sprintf("数据库连接成功: %s@%s:%d/%s",
		m.config.Username, m.config.Host, m.config.Port, m.config.Database))

	return nil
}

// DB 获取数据库连接
func (m *Manager) DB() *sql.DB {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.db
}

// Close 关闭数据库连接
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected || m.db == nil {
		return nil
	}

	err := m.db.Close()
	if err != nil {
		return fmt.Errorf("关闭数据库连接失败: %v", err)
	}

	m.connected = false
	m.db = nil

	logger.Info("数据库连接已关闭")
	return nil
}

// CheckConnection 检查数据库连接状态
func (m *Manager) CheckConnection() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected || m.db == nil {
		return false
	}

	err := m.db.Ping()
	if err != nil {
		logger.Error(fmt.Sprintf("数据库连接检查失败: %v", err))
		return false
	}

	return true
}
