// internal/client/conn_pool.go
// 连接池管理
package client

import (
	"errors"
	"net"
	"sync"
	"time"
)

// ConnectionPool 连接池
type ConnectionPool struct {
	address     string
	maxSize     int
	idleTimeout time.Duration
	dialTimeout time.Duration
	mu          sync.Mutex
	idleConns   []*poolConn
	activeCount int
}

// poolConn 池连接
type poolConn struct {
	conn net.Conn
	pool *ConnectionPool
	// createdAt time.Time
	lastUsed time.Time
}

// NewConnectionPool 创建新的连接池
func NewConnectionPool(address string, maxSize int, idleTimeout, dialTimeout time.Duration) *ConnectionPool {
	pool := &ConnectionPool{
		address:     address,
		maxSize:     maxSize,
		idleTimeout: idleTimeout,
		dialTimeout: dialTimeout,
		idleConns:   make([]*poolConn, 0, maxSize),
	}

	// 启动清理协程
	go pool.cleanupLoop()
	return pool
}

// Get 获取连接
func (p *ConnectionPool) Get() (net.Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 检查是否有空闲连接
	if len(p.idleConns) > 0 {
		// 获取最后一个连接（LIFO）
		conn := p.idleConns[len(p.idleConns)-1]
		p.idleConns = p.idleConns[:len(p.idleConns)-1]

		// 检查连接是否过期
		if time.Since(conn.lastUsed) > p.idleTimeout {
			conn.conn.Close()
			p.activeCount--
			// 创建新连接
			return p.createConn()
		}

		return conn.conn, nil
	}

	// 没有空闲连接，检查是否达到最大连接数
	if p.activeCount >= p.maxSize {
		return nil, errors.New("连接池已满")
	}

	// 创建新连接
	return p.createConn()
}

// Put 归还连接
func (p *ConnectionPool) Put(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 检查连接是否有效
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.SetKeepAlive(true); err != nil {
			conn.Close()
			p.activeCount--
			return
		}
	}

	// 创建池连接
	pConn := &poolConn{
		conn:     conn,
		pool:     p,
		lastUsed: time.Now(),
	}

	// 添加到空闲连接列表
	p.idleConns = append(p.idleConns, pConn)
}

// Close 关闭连接池
func (p *ConnectionPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 关闭所有空闲连接
	for _, conn := range p.idleConns {
		conn.conn.Close()
	}

	p.idleConns = nil
	p.activeCount = 0
}

// createConn 创建新连接
func (p *ConnectionPool) createConn() (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", p.address, p.dialTimeout)
	if err != nil {
		return nil, err
	}

	p.activeCount++
	return conn, nil
}

// cleanupLoop 定期清理过期连接
func (p *ConnectionPool) cleanupLoop() {
	ticker := time.NewTicker(p.idleTimeout / 2)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

// cleanup 清理过期连接
func (p *ConnectionPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.idleConns) == 0 {
		return
	}

	var remaining []*poolConn
	expiredTime := time.Now().Add(-p.idleTimeout)

	for _, conn := range p.idleConns {
		if conn.lastUsed.Before(expiredTime) {
			conn.conn.Close()
			p.activeCount--
		} else {
			remaining = append(remaining, conn)
		}
	}

	p.idleConns = remaining
}
