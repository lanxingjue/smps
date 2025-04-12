// internal/dispatcher/dispatcher.go  消息分发器
package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"smps/internal/protocol"
	"smps/pkg/logger"
)

// MessageDispatcher 消息分发器
type MessageDispatcher struct {
	handlers   []MessageHandler
	processors []MessageProcessor
	msgQueue   *MessageQueue

	// 统计数据
	stats struct {
		dispatched uint64
		errors     uint64
		totalTime  int64
	}

	// 配置参数
	config struct {
		queueSize       int
		workerCount     int
		dispatchTimeout time.Duration
	}

	// 同步控制
	mu        sync.RWMutex
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
}

// NewMessageDispatcher 创建新的消息分发器
func NewMessageDispatcher(queueSize, workerCount int, dispatchTimeout time.Duration) *MessageDispatcher {
	if queueSize <= 0 {
		queueSize = 1000
	}

	if workerCount <= 0 {
		workerCount = 5
	}

	if dispatchTimeout <= 0 {
		dispatchTimeout = 2 * time.Second
	}

	d := &MessageDispatcher{
		handlers:   make([]MessageHandler, 0),
		processors: make([]MessageProcessor, 0),
	}

	d.config.queueSize = queueSize
	d.config.workerCount = workerCount
	d.config.dispatchTimeout = dispatchTimeout

	d.msgQueue = NewMessageQueue(queueSize)
	return d
}

// Start 启动分发器
func (d *MessageDispatcher) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.isRunning {
		return nil
	}

	d.ctx, d.cancel = context.WithCancel(ctx)
	d.isRunning = true

	// 启动工作线程
	for i := 0; i < d.config.workerCount; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}

	logger.Info(fmt.Sprintf("消息分发器已启动, 队列大小=%d, 工作线程数=%d",
		d.config.queueSize, d.config.workerCount))

	return nil
}

// Stop 停止分发器
func (d *MessageDispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.isRunning {
		return
	}

	logger.Info("正在停止消息分发器...")

	// 取消上下文
	if d.cancel != nil {
		d.cancel()
	}

	// 等待所有工作线程退出
	d.wg.Wait()
	d.isRunning = false

	logger.Info("消息分发器已停止")
}

// RegisterHandler 注册消息处理器
func (d *MessageDispatcher) RegisterHandler(handler MessageHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.handlers = append(d.handlers, handler)
	logger.Info(fmt.Sprintf("已注册消息处理器: %s", handler.HandlerName()))
}

// RegisterProcessor 注册消息处理流程
func (d *MessageDispatcher) RegisterProcessor(processor MessageProcessor) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.processors = append(d.processors, processor)
}

// // Dispatch 异步分发消息
// func (d *MessageDispatcher) Dispatch(msg *protocol.Message) {
// 	// 检查是否已启动
// 	if !d.isRunning {
// 		logger.Error("分发器未启动，无法分发消息")
// 		return
// 	}

//		// 将消息放入队列
//		if err := d.msgQueue.Enqueue(msg); err != nil {
//			logger.Error(fmt.Sprintf("消息入队失败: %v", err))
//			atomic.AddUint64(&d.stats.errors, 1)
//		}
//	}
//
// Dispatch 异步分发消息
func (d *MessageDispatcher) Dispatch(msg interface{}) error {
	// 检查是否已启动
	if !d.isRunning {
		logger.Error("分发器未启动，无法分发消息")
		return fmt.Errorf("分发器未启动")
	}

	var protocolMsg *protocol.Message

	// 根据消息类型进行处理
	switch m := msg.(type) {
	case *protocol.Message:
		protocolMsg = m
	case []byte:
		// 将字节数组转换为Message对象
		var err error
		protocolMsg, err = protocol.ParseMessage(m)
		if err != nil {
			logger.Error(fmt.Sprintf("解析消息失败: %v", err))
			atomic.AddUint64(&d.stats.errors, 1)
			return err
		}
	default:
		logger.Error(fmt.Sprintf("不支持的消息类型: %T", msg))
		return fmt.Errorf("不支持的消息类型: %T", msg)
	}

	// 将消息放入队列
	if err := d.msgQueue.Enqueue(protocolMsg); err != nil {
		logger.Error(fmt.Sprintf("消息入队失败: %v", err))
		atomic.AddUint64(&d.stats.errors, 1)
		return err
	}

	return nil
}

// DispatchSync 同步分发消息
func (d *MessageDispatcher) DispatchSync(ctx context.Context, msg *protocol.Message) error {
	start := time.Now()

	// 复制处理器列表，避免锁争用
	d.mu.RLock()
	handlers := make([]MessageHandler, len(d.handlers))
	copy(handlers, d.handlers)
	d.mu.RUnlock()

	// 创建超时上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, d.config.dispatchTimeout)
	defer cancel()

	// 并发分发给所有处理器
	var wg sync.WaitGroup
	errCh := make(chan error, len(handlers))

	for _, handler := range handlers {
		wg.Add(1)
		go func(h MessageHandler) {
			defer wg.Done()

			if err := h.Handle(timeoutCtx, msg); err != nil {
				errCh <- fmt.Errorf("处理器 %s 处理失败: %v", h.HandlerName(), err)
			}
		}(handler)
	}

	// 等待所有处理器完成或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// 等待完成或超时
	select {
	case <-done:
		// 所有处理器正常完成
	case <-timeoutCtx.Done():
		atomic.AddUint64(&d.stats.errors, 1)
		return fmt.Errorf("分发超时，已分发 %d 毫秒", time.Since(start).Milliseconds())
	}

	// 检查错误
	close(errCh)
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}

	// 更新统计信息
	atomic.AddUint64(&d.stats.dispatched, 1)
	atomic.AddInt64(&d.stats.totalTime, time.Since(start).Nanoseconds())

	if len(errors) > 0 {
		atomic.AddUint64(&d.stats.errors, 1)
		return fmt.Errorf("分发错误: %v", errors)
	}

	return nil
}

// worker 工作线程
func (d *MessageDispatcher) worker(id int) {
	defer d.wg.Done()

	logger.Info(fmt.Sprintf("工作线程 #%d 已启动", id))

	for {
		// 检查上下文是否取消
		select {
		case <-d.ctx.Done():
			logger.Info(fmt.Sprintf("工作线程 #%d 收到退出信号", id))
			return
		default:
			// 继续处理
		}

		// 从队列获取消息
		msg, err := d.msgQueue.Dequeue(100 * time.Millisecond)
		if err != nil {
			// 超时是正常的
			continue
		}

		// 创建请求上下文
		reqCtx, cancel := context.WithTimeout(d.ctx, d.config.dispatchTimeout)

		// 分发消息
		if err := d.DispatchSync(reqCtx, msg); err != nil {
			logger.Error(fmt.Sprintf("工作线程 #%d 分发失败: %v", id, err))
		}

		cancel()
	}
}

// Process 处理消息
func (d *MessageDispatcher) Process(ctx context.Context, msg *protocol.Message) (*ProcessResult, error) {
	// 复制处理器列表，避免锁争用
	d.mu.RLock()
	processors := make([]MessageProcessor, len(d.processors))
	copy(processors, d.processors)
	d.mu.RUnlock()

	// 如果没有处理器，直接返回默认结果
	if len(processors) == 0 {
		return NewProcessResult("ALLOW", msg, "default"), nil
	}

	// 收集所有处理器的结果
	var results []*ProcessResult
	var errs []error

	for _, processor := range processors {
		result, err := processor.Process(ctx, msg)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if result != nil {
			results = append(results, result)
		}
	}

	// 如果没有有效结果，检查是否有错误
	if len(results) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("所有处理器都失败: %v", errs)
		}
		return NewProcessResult("ALLOW", msg, "default"), nil
	}

	// 如果有多个结果，按照优先级规则选择一个
	return selectResult(results), nil
}

// 根据优先级规则选择结果
func selectResult(results []*ProcessResult) *ProcessResult {
	if len(results) == 0 {
		return nil
	}

	// 如果只有一个结果，直接返回
	if len(results) == 1 {
		return results[0]
	}

	// 首先检查是否有"拦截"操作，拦截优先
	var intercepts []*ProcessResult
	for _, res := range results {
		if res.Action == "INTERCEPT" {
			intercepts = append(intercepts, res)
		}
	}

	if len(intercepts) > 0 {
		// 有拦截操作，选择最早的一个
		return getEarliestResult(intercepts)
	}

	// 没有拦截操作，选择最早的允许操作
	return getEarliestResult(results)
}

// 获取时间戳最早的结果
func getEarliestResult(results []*ProcessResult) *ProcessResult {
	if len(results) == 0 {
		return nil
	}

	earliest := results[0]
	for _, res := range results[1:] {
		if res.Timestamp < earliest.Timestamp {
			earliest = res
		}
	}

	return earliest
}

// GetStats 获取统计信息
func (d *MessageDispatcher) GetStats() map[string]interface{} {
	dispatched := atomic.LoadUint64(&d.stats.dispatched)
	errors := atomic.LoadUint64(&d.stats.errors)
	totalTime := atomic.LoadInt64(&d.stats.totalTime)

	var avgTime float64
	if dispatched > 0 {
		avgTime = float64(totalTime) / float64(dispatched) / float64(time.Millisecond)
	}

	return map[string]interface{}{
		"dispatched":     dispatched,
		"errors":         errors,
		"avg_time_ms":    avgTime,
		"queue_size":     d.msgQueue.Size(),
		"queue_capacity": d.msgQueue.Capacity(),
	}
}
