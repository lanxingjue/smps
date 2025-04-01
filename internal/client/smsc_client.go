// internal/client/smsc_client.go  smsc客户端实现
package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"smps/internal/protocol"
	"smps/pkg/logger"
)

// Config 客户端配置
type Config struct {
	Address           string        // SMSC服务器地址
	SystemID          string        // 系统ID
	Password          string        // 密码
	EnquireInterval   time.Duration // 心跳间隔
	ResponseTimeout   time.Duration // 响应超时
	ReconnectInterval time.Duration // 重连间隔
	MaxRetries        int           // 最大重试次数
	BackoffFactor     float64       // 退避系数
}

// SMSCClient SMSC客户端
type SMSCClient struct {
	config      *Config
	conn        net.Conn
	sequenceNum uint32
	// 状态管理
	status       int32 // 0:断开 1:连接中 2:已连接
	lastActivity int64 // 最后活动时间戳
	// 上下文控制
	ctx    context.Context
	cancel context.CancelFunc
	// 同步
	sendMutex sync.Mutex
	// 统计
	stats struct {
		sentMessages     uint64
		receivedMessages uint64
		reconnectCount   uint64
		errors           uint64
	}
}

// NewSMSCClient 创建新的SMSC客户端
func NewSMSCClient(config *Config) *SMSCClient {
	return &SMSCClient{
		config:      config,
		status:      0,
		sequenceNum: 1,
	}
}

// Start 启动客户端
func (c *SMSCClient) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// 初始连接
	if err := c.connect(); err != nil {
		return err
	}

	// 启动读取循环
	go c.readLoop()

	return nil
}

// Stop 停止客户端
func (c *SMSCClient) Stop() {
	if c.cancel != nil {
		c.cancel()
	}

	c.close()
}

// connect 建立连接并进行绑定
func (c *SMSCClient) connect() error {
	if atomic.LoadInt32(&c.status) == 2 {
		return nil // 已连接
	}

	atomic.StoreInt32(&c.status, 1) // 连接中

	// 建立TCP连接
	conn, err := net.DialTimeout("tcp", c.config.Address, 5*time.Second)
	if err != nil {
		atomic.StoreInt32(&c.status, 0)
		return fmt.Errorf("连接失败: %v", err)
	}

	c.conn = conn

	// 发送绑定请求
	if err := c.sendBind(); err != nil {
		c.conn.Close()
		atomic.StoreInt32(&c.status, 0)
		return fmt.Errorf("绑定失败: %v", err)
	}

	logger.Info("已成功连接到SMSC服务器")
	atomic.StoreInt64(&c.lastActivity, time.Now().UnixNano())

	return nil
}

// sendBind 发送绑定请求
func (c *SMSCClient) sendBind() error {
	// 构造绑定消息
	payload := []byte(c.config.SystemID + "\x00" + c.config.Password + "\x00" + "smpp_proxy\x00\x34")
	bindMsg := protocol.NewMessage(protocol.BIND_RECEIVER, 0, atomic.AddUint32(&c.sequenceNum, 1), payload)

	// 发送绑定请求
	if err := c.sendMessage(bindMsg); err != nil {
		return err
	}

	// 等待绑定响应
	resp, err := c.readWithTimeout(5 * time.Second)
	if err != nil {
		return err
	}

	if resp.Header.CommandID != protocol.BIND_RECEIVER_RESP {
		return fmt.Errorf("预期绑定响应，收到命令ID: %d", resp.Header.CommandID)
	}

	if resp.Header.CommandStatus != protocol.SMPP_ESME_ROK {
		return fmt.Errorf("绑定失败，状态码: %d", resp.Header.CommandStatus)
	}

	atomic.StoreInt32(&c.status, 2) // 已连接
	logger.Info("成功绑定到SMSC服务器")

	return nil
}

// sendMessage 发送消息
func (c *SMSCClient) sendMessage(msg *protocol.Message) error {
	if c.conn == nil {
		return errors.New("未连接")
	}

	c.sendMutex.Lock()
	defer c.sendMutex.Unlock()

	// 设置写入超时
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}

	// 发送消息
	_, err := c.conn.Write(msg.Bytes())
	if err != nil {
		return err
	}

	atomic.AddUint64(&c.stats.sentMessages, 1)
	atomic.StoreInt64(&c.lastActivity, time.Now().UnixNano())

	return nil
}

// SendEnquireLink 发送链路查询
func (c *SMSCClient) SendEnquireLink() error {
	msg := protocol.NewMessage(protocol.ENQUIRE_LINK, 0, atomic.AddUint32(&c.sequenceNum, 1), nil)
	return c.sendMessage(msg)
}

// readMessage 读取消息
func (c *SMSCClient) readMessage() (*protocol.Message, error) {
	if c.conn == nil {
		return nil, errors.New("未连接")
	}

	// 设置读取超时
	if err := c.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("设置读取超时失败: %v", err)
	}

	// 读取头部
	headerBuf := make([]byte, 16)
	n, err := io.ReadFull(c.conn, headerBuf)
	if err != nil {
		return nil, fmt.Errorf("读取头部错误(读取了%d字节): %v", n, err)
	}

	// 解析头部获取消息长度
	commandLength := binary.BigEndian.Uint32(headerBuf[0:4])

	// 验证消息长度是否合理
	if commandLength < 16 || commandLength > 4096 {
		return nil, fmt.Errorf("消息长度异常: %d", commandLength)
	}

	// 读取消息体
	bodyLen := commandLength - 16
	if bodyLen > 0 {
		bodyBuf := make([]byte, bodyLen)
		n, err := io.ReadFull(c.conn, bodyBuf)
		if err != nil {
			return nil, fmt.Errorf("读取消息体错误(读取了%d/%d字节): %v", n, bodyLen, err)
		}

		msg, err := protocol.ParseMessage(append(headerBuf, bodyBuf...))
		if err != nil {
			return nil, fmt.Errorf("解析消息失败: %v", err)
		}

		atomic.AddUint64(&c.stats.receivedMessages, 1)
		atomic.StoreInt64(&c.lastActivity, time.Now().UnixNano())

		return msg, nil
	}

	// 没有消息体
	msg, err := protocol.ParseMessage(headerBuf)
	if err != nil {
		return nil, fmt.Errorf("解析无体消息失败: %v", err)
	}

	atomic.AddUint64(&c.stats.receivedMessages, 1)
	atomic.StoreInt64(&c.lastActivity, time.Now().UnixNano())

	return msg, nil
}

// readWithTimeout 带超时的读取
func (c *SMSCClient) readWithTimeout(timeout time.Duration) (*protocol.Message, error) {
	if c.conn == nil {
		return nil, errors.New("未连接")
	}

	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}

	return c.readMessage()
}

// readLoop 读取消息循环
func (c *SMSCClient) readLoop() {
	logger.Info("开始消息读取循环")
	defer c.close()

	var (
		N1        int                                        // 链路查询失败计数器
		heartbeat = time.NewTicker(c.config.EnquireInterval) //创建心跳计时器
	)
	defer heartbeat.Stop()

	// 启动心跳goroutine
	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-heartbeat.C:
				log.Printf("发送定期链路查询")
				// 发送心跳
				if err := c.SendEnquireLink(); err != nil {
					N1++
					logger.Error(fmt.Sprintf("发送链路查询失败 (N1=%d/3): %v", N1, err))
					if N1 >= 3 {
						logger.Error("连续三次链路查询失败，断开连接")
						return
					}
				}
			}
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			logger.Info("客户端上下文取消，退出读取循环")
			return

		default:
			// 尝试读取消息
			msg, err := c.readWithTimeout(100 * time.Millisecond)
			if err != nil {
				// 判断是否是超时错误
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 超时是正常的，继续下一次循环
					continue
				}

				logger.Error(fmt.Sprintf("读取错误: %v", err))
				if err := c.reconnect(); err != nil {
					logger.Error(fmt.Sprintf("重连失败: %v", err))
					return
				}
				continue
			}

			// 重置链路查询失败计数
			N1 = 0

			// 处理不同类型的消息
			switch msg.Header.CommandID {
			case protocol.DELIVER_SM:
				logger.Info(fmt.Sprintf("收到DELIVER_SM消息，序列号: %d", msg.Header.SequenceNumber))

				// 这里应该分发给处理器，但目前简单直接回复
				respMsg := protocol.CreateDeliverSMResponse(msg.Header.SequenceNumber, protocol.SM_OK)
				if err := c.sendMessage(respMsg); err != nil {
					logger.Error(fmt.Sprintf("发送DELIVER_SM响应失败: %v", err))
				}

			case protocol.ENQUIRE_LINK_RESP:
				logger.Info(fmt.Sprintf("收到ENQUIRE_LINK_RESP，序列号: %d", msg.Header.SequenceNumber))

			default:
				logger.Info(fmt.Sprintf("收到未知命令ID: %d，序列号: %d",
					msg.Header.CommandID, msg.Header.SequenceNumber))
			}
		}
	}
}

// reconnect 重新连接
func (c *SMSCClient) reconnect() error {
	c.close()

	baseInterval := c.config.ReconnectInterval
	maxRetries := c.config.MaxRetries

	for i := 0; i < maxRetries; i++ {
		// 指数退避
		waitTime := time.Duration(float64(baseInterval) * math.Pow(c.config.BackoffFactor, float64(i)))
		logger.Info(fmt.Sprintf("尝试第%d次重连，等待%.2f秒", i+1, waitTime.Seconds()))

		select {
		case <-time.After(waitTime):
			if err := c.connect(); err == nil {
				atomic.AddUint64(&c.stats.reconnectCount, 1)
				return nil
			}

		case <-c.ctx.Done():
			return errors.New("连接已终止")
		}
	}

	return errors.New("超过最大重试次数")
}

// close 关闭连接
func (c *SMSCClient) close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	atomic.StoreInt32(&c.status, 0)
}

// GetStatus 获取连接状态
func (c *SMSCClient) GetStatus() string {
	status := atomic.LoadInt32(&c.status)
	switch status {
	case 0:
		return "未连接"
	case 1:
		return "连接中"
	case 2:
		return "已连接"
	default:
		return "未知状态"
	}
}

// GetStats 获取统计信息
func (c *SMSCClient) GetStats() map[string]uint64 {
	return map[string]uint64{
		"sent_messages":     atomic.LoadUint64(&c.stats.sentMessages),
		"received_messages": atomic.LoadUint64(&c.stats.receivedMessages),
		"reconnect_count":   atomic.LoadUint64(&c.stats.reconnectCount),
		"errors":            atomic.LoadUint64(&c.stats.errors),
	}
}
