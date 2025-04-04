// internal/dispatcher/queue.go  消息队列
package dispatcher

import (
	"errors"
	"sync"
	"time"

	"smps/internal/protocol"
)

// MessageQueue 消息队列
type MessageQueue struct {
	queue    chan *protocol.Message
	mu       sync.RWMutex
	capacity int
}

// NewMessageQueue 创建新的消息队列
func NewMessageQueue(capacity int) *MessageQueue {
	if capacity <= 0 {
		capacity = 1000
	}

	return &MessageQueue{
		queue:    make(chan *protocol.Message, capacity),
		capacity: capacity,
	}
}

// Enqueue 将消息放入队列
func (q *MessageQueue) Enqueue(msg *protocol.Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// 非阻塞方式入队
	select {
	case q.queue <- msg:
		return nil
	default:
		return errors.New("队列已满")
	}
}

// Dequeue 从队列取出消息
func (q *MessageQueue) Dequeue(timeout time.Duration) (*protocol.Message, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// 带超时的出队
	select {
	case msg := <-q.queue:
		return msg, nil
	case <-time.After(timeout):
		return nil, errors.New("队列取消息超时")
	}
}

// Size 获取队列当前大小
func (q *MessageQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return len(q.queue)
}

// Capacity 获取队列容量
func (q *MessageQueue) Capacity() int {
	return q.capacity
}

// Clear 清空队列
func (q *MessageQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	// 清空队列
	for {
		select {
		case <-q.queue:
			// 继续清空
		default:
			// 队列为空，退出
			return
		}
	}
}
