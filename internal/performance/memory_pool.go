// internal/performance/memory_pool.go
package performance

import (
	"sync"
)

// MessageBufferPool 消息缓冲区内存池
type MessageBufferPool struct {
	pool sync.Pool
	size int
}

// NewMessageBufferPool 创建消息缓冲区内存池
func NewMessageBufferPool(bufferSize int) *MessageBufferPool {
	return &MessageBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, bufferSize)
			},
		},
		size: bufferSize,
	}
}

// Get 获取缓冲区
func (p *MessageBufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

// Put 归还缓冲区
func (p *MessageBufferPool) Put(buf []byte) {
	// 确保归还的缓冲区大小正确
	if cap(buf) >= p.size {
		p.pool.Put(buf[:p.size])
	}
}

// 预先分配的对象池，用于减少GC压力
var (
	// 消息缓冲区池 (8KB)
	MessageBufferPool8K = NewMessageBufferPool(8 * 1024)

	// 小消息缓冲区池 (1KB)
	MessageBufferPool1K = NewMessageBufferPool(1 * 1024)
)
