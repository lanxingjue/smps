// internal/server/smmc_server.go
package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"smps/internal/auth"
	// "smps/internal/dispatcher"
	"smps/internal/protocol"
	"smps/pkg/logger"
)

// ServerConfig 服务器配置
//
//	type ServerConfig struct {
//		ListenAddress          string        // 监听地址
//		MaxConnections         int           // 最大连接数
//		ReadTimeout            time.Duration // 读取超时
//		WriteTimeout           time.Duration // 写入超时
//		HeartbeatInterval      time.Duration // 心跳间隔
//		HeartbeatMissThreshold int           // 心跳丢失阈值
//	}
type ServerConfig struct {
	ListenAddress          string        `yaml:"listen_address"`
	MaxConnections         int           `yaml:"max_connections"`
	ReadTimeout            time.Duration `yaml:"read_timeout"`
	WriteTimeout           time.Duration `yaml:"write_timeout"`
	HeartbeatInterval      time.Duration `yaml:"heartbeat_interval"`
	HeartbeatMissThreshold int           `yaml:"heartbeat_miss_threshold"`
}

// 添加消息处理函数和拦截器类型
type MessageHandler func(msg []byte) error
type MessageInterceptor func(msg []byte, isIncoming bool) []byte

// Server SMMC服务器
type Server struct {
	config        *ServerConfig
	authenticator *auth.Authenticator
	sessionMgr    *SessionManager
	listener      net.Listener
	// 上下文控制
	ctx    context.Context
	cancel context.CancelFunc
	// 统计
	stats struct {
		activeConnections int64
		totalConnections  uint64
		receivedMessages  uint64
		sentMessages      uint64
		authFailures      uint64
	}
	// 同步
	wg sync.WaitGroup
	// ...现有字段保持不变

	messageHandler      MessageHandler
	messageInterceptors []MessageInterceptor
}

// NewServer 创建新的服务器
func NewServer(config *ServerConfig, authenticator *auth.Authenticator) *Server {
	return &Server{
		config:        config,
		authenticator: authenticator,
		sessionMgr:    NewSessionManager(),
	}
}

// Start 启动服务器
func (s *Server) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	// 验证配置
	if s.config.HeartbeatInterval <= 0 {
		s.config.HeartbeatInterval = 30 * time.Second // 设置默认值
		logger.Warning("心跳间隔配置无效(<=0)，设置为默认值30秒")
	}

	if s.config.ReadTimeout <= 0 {
		s.config.ReadTimeout = 5 * time.Second
		logger.Warning("读取超时配置无效(<=0)，设置为默认值5秒")
	}

	if s.config.WriteTimeout <= 0 {
		s.config.WriteTimeout = 5 * time.Second
		logger.Warning("写入超时配置无效(<=0)，设置为默认值5秒")
	}

	if s.config.ListenAddress == "" {
		s.config.ListenAddress = "0.0.0.0:2775" // 设置默认值
		logger.Warning("监听地址为空，设置为默认值0.0.0.0:2775")
	}

	// 创建监听器
	listener, err := net.Listen("tcp", s.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("监听失败: %v", err)
	}

	s.listener = listener
	logger.Info(fmt.Sprintf("服务器监听于 %s", s.config.ListenAddress))

	// 启动接受连接循环
	s.wg.Add(1)
	go s.acceptLoop()

	// 启动心跳检测
	s.wg.Add(1)
	go s.heartbeatLoop()

	return nil
}

// Stop 停止服务器
func (s *Server) Stop() {
	if s.cancel != nil {
		s.cancel()
	}

	if s.listener != nil {
		s.listener.Close()
	}

	// 关闭所有会话
	s.sessionMgr.CloseAll()

	// 等待所有协程结束
	s.wg.Wait()
	logger.Info("服务器已停止")
}

// acceptLoop 接受连接循环
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		// 检查上下文是否取消
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// 检查是否达到最大连接数
		if s.config.MaxConnections > 0 && atomic.LoadInt64(&s.stats.activeConnections) >= int64(s.config.MaxConnections) {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 接受连接
		conn, err := s.listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				// 临时错误，稍后重试
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// 非临时错误，退出循环
			logger.Error(fmt.Sprintf("接受连接失败: %v", err))
			return
		}

		// 处理连接
		atomic.AddInt64(&s.stats.activeConnections, 1)
		atomic.AddUint64(&s.stats.totalConnections, 1)
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// heartbeatLoop 心跳检测循环
func (s *Server) heartbeatLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkSessionHeartbeats()
		}
	}
}

// checkSessionHeartbeats 检查所有会话的心跳
func (s *Server) checkSessionHeartbeats() {
	s.sessionMgr.Range(func(session *Session) bool {
		// 只检查已连接的会话
		if session.Status != protocol.Session_Connected {
			return true
		}

		// 检查是否超过空闲阈值
		if session.IdleTime() > s.config.HeartbeatInterval {
			// 发送心跳
			if err := s.SendEnquireLink(session); err != nil {
				session.HeartbeatMisses++
				logger.Error(fmt.Sprintf("发送心跳失败 (会话ID: %d, 系统ID: %s, 错误次数: %d/%d): %v",
					session.ID, session.SystemID, session.HeartbeatMisses, s.config.HeartbeatMissThreshold, err))

				// 达到最大错误次数，关闭会话
				if session.HeartbeatMisses >= s.config.HeartbeatMissThreshold {
					logger.Error(fmt.Sprintf("心跳检测失败次数过多，关闭会话 (ID: %d, 系统ID: %s)",
						session.ID, session.SystemID))
					s.sessionMgr.CloseSession(session.ID)
				}
			} else {
				// 心跳发送成功，重置错误计数
				session.HeartbeatMisses = 0
			}
		}

		return true
	})
}

// handleConnection 处理连接
func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		atomic.AddInt64(&s.stats.activeConnections, -1)
		s.wg.Done()
	}()

	// 创建会话
	session := NewSession(conn)

	// 设置读取超时
	if err := conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
		logger.Error(fmt.Sprintf("设置读取超时失败: %v", err))
		return
	}

	// 等待绑定请求
	msg, err := s.readMessage(conn)
	if err != nil {
		logger.Error(fmt.Sprintf("读取绑定请求失败: %v", err))
		return
	}

	// 验证是否为绑定请求
	if msg.Header.CommandID != protocol.BIND_RECEIVER {
		logger.Error(fmt.Sprintf("预期绑定请求，收到命令ID: %d", msg.Header.CommandID))
		s.SendBindResponse(conn, msg.Header.SequenceNumber, protocol.E_OTHERERR, "")
		return
	}

	// 解析绑定请求
	systemID, password, err := parseBindRequest(msg.Payload)
	if err != nil {
		logger.Error(fmt.Sprintf("解析绑定请求失败: %v", err))
		s.SendBindResponse(conn, msg.Header.SequenceNumber, protocol.E_OTHERERR, "")
		return
	}

	// 获取客户端IP和端口
	clientAddr := conn.RemoteAddr().(*net.TCPAddr)
	clientIP := clientAddr.IP
	clientPort := clientAddr.Port

	// 验证身份
	authenticated, err := s.authenticator.Authenticate(systemID, password, clientIP, clientPort)
	if err != nil || !authenticated {
		logger.Error(fmt.Sprintf("认证失败: %v", err))
		atomic.AddUint64(&s.stats.authFailures, 1)
		s.SendBindResponse(conn, msg.Header.SequenceNumber, protocol.E_PASSWORD, "")
		return
	}

	// 获取账户信息以设置会话属性
	account, _ := s.authenticator.GetAccount(systemID)

	// 认证成功，发送绑定响应
	if err := s.SendBindResponse(conn, msg.Header.SequenceNumber, protocol.E_SUCCESS, systemID); err != nil {
		logger.Error(fmt.Sprintf("发送绑定响应失败: %v", err))
		return
	}

	// 更新会话信息
	session.SystemID = systemID
	session.Status = protocol.Session_Connected
	session.Protocol = account.Protocol
	session.InterfaceType = account.InterfaceType
	session.EncodingType = account.EncodingType
	session.FlowControl = account.FlowControl

	// 注册会话
	s.sessionMgr.Add(session)

	logger.Info(fmt.Sprintf("新会话已建立 (ID: %d, 系统ID: %s, 来源: %s:%d)",
		session.ID, session.SystemID, session.RemoteAddr, session.RemotePort))

	// 处理会话消息
	s.handleSession(session)
}

// handleSession 处理会话
func (s *Server) handleSession(session *Session) {
	defer func() {
		session.Status = protocol.Session_Closed
		s.sessionMgr.Remove(session.ID)
		logger.Info(fmt.Sprintf("会话已关闭 (ID: %d, 系统ID: %s)", session.ID, session.SystemID))
	}()

	// 消息处理循环
	for {
		// 设置读取超时
		if err := session.Conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
			logger.Error(fmt.Sprintf("设置读取超时失败: %v", err))
			return
		}

		// 非阻塞检查上下文是否取消
		select {
		case <-s.ctx.Done():
			// 发送解绑请求
			s.SendUnbind(session)
			return
		default:
			// 继续处理
		}

		// 读取消息
		msg, err := s.readMessage(session.Conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// 读取超时，继续
				continue
			}

			logger.Error(fmt.Sprintf("读取消息失败: %v", err))
			return
		}

		atomic.AddUint64(&s.stats.receivedMessages, 1)
		session.IncrementReceived()
		session.UpdateActivity()

		// 处理消息
		switch msg.Header.CommandID {
		case protocol.DELIVER_SM:
			// 处理短信投递
			if err := s.handleDeliverSM(session, msg); err != nil {
				logger.Error(fmt.Sprintf("处理DELIVER_SM失败: %v", err))
			}

		case protocol.ENQUIRE_LINK:
			// 处理链路查询
			if err := s.SendEnquireLinkResponse(session, msg.Header.SequenceNumber); err != nil {
				logger.Error(fmt.Sprintf("发送ENQUIRE_LINK响应失败: %v", err))
			}

		case protocol.UNBIND:
			// 处理解绑请求
			if err := s.SendUnbindResponse(session, msg.Header.SequenceNumber); err != nil {
				logger.Error(fmt.Sprintf("发送UNBIND响应失败: %v", err))
			}
			return

		default:
			logger.Info(fmt.Sprintf("收到未知命令ID: %d", msg.Header.CommandID))
		}
	}
}

// readMessage 读取消息
func (s *Server) readMessage(conn net.Conn) (*protocol.Message, error) {
	// 读取头部
	headerBuf := make([]byte, 16)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		return nil, err
	}

	header, err := protocol.ParseHeader(headerBuf)
	if err != nil {
		return nil, err
	}

	// 检查消息长度是否合理
	if header.CommandLength < 16 || header.CommandLength > 4096 {
		return nil, fmt.Errorf("消息长度异常: %d", header.CommandLength)
	}

	// 读取消息体
	bodyLen := header.CommandLength - 16
	if bodyLen > 0 {
		bodyBuf := make([]byte, bodyLen)
		if _, err := io.ReadFull(conn, bodyBuf); err != nil {
			return nil, err
		}

		msg, err := protocol.ParseMessage(append(headerBuf, bodyBuf...))
		if err != nil {
			return nil, err
		}

		return msg, nil
	}

	// 没有消息体
	msg, err := protocol.ParseMessage(headerBuf)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

// sendMessage 发送消息
func (s *Server) sendMessage(conn net.Conn, msg *protocol.Message) error {
	// 设置写入超时
	if err := conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout)); err != nil {
		return err
	}

	// 发送消息
	_, err := conn.Write(msg.Bytes())
	if err != nil {
		return err
	}

	atomic.AddUint64(&s.stats.sentMessages, 1)
	return nil
}

// SendBindResponse 发送绑定响应
func (s *Server) SendBindResponse(conn net.Conn, sequence uint32, status uint32, systemID string) error {
	msg := protocol.CreateBindResponse(sequence, status, systemID)
	return s.sendMessage(conn, msg)
}

// SendEnquireLink 发送链路查询
func (s *Server) SendEnquireLink(session *Session) error {
	msg := protocol.CreateEnquireLink(session.NextSequence())
	err := s.sendMessage(session.Conn, msg)
	if err == nil {
		session.IncrementSent()
	}
	return err
}

// SendEnquireLinkResponse 发送链路查询响应
func (s *Server) SendEnquireLinkResponse(session *Session, sequence uint32) error {
	msg := protocol.CreateEnquireLinkResponse(sequence)
	err := s.sendMessage(session.Conn, msg)
	if err == nil {
		session.IncrementSent()
	}
	return err
}

// SendUnbind 发送解绑请求
func (s *Server) SendUnbind(session *Session) error {
	msg := protocol.NewMessage(protocol.UNBIND, 0, session.NextSequence(), nil)
	err := s.sendMessage(session.Conn, msg)
	if err == nil {
		session.IncrementSent()
	}
	return err
}

// SendUnbindResponse 发送解绑响应
func (s *Server) SendUnbindResponse(session *Session, sequence uint32) error {
	msg := protocol.CreateUnbindResponse(sequence, protocol.UNBIND_OK)
	err := s.sendMessage(session.Conn, msg)
	if err == nil {
		session.IncrementSent()
	}
	return err
}

// BroadcastMessage 广播消息给所有会话
func (s *Server) BroadcastMessage(msg *protocol.Message, excludeSessionID uint64) {
	s.sessionMgr.Range(func(session *Session) bool {
		// 排除发送消息的会话
		if session.ID == excludeSessionID {
			return true
		}

		// 只发送给已连接的会话
		if session.Status != protocol.Session_Connected {
			return true
		}

		// 创建新消息（使用新的序列号）
		newMsg := protocol.NewMessage(
			msg.Header.CommandID,
			msg.Header.CommandStatus,
			session.NextSequence(),
			msg.Payload,
		)

		// 复制TLV参数
		for _, tlv := range msg.TLVs {
			newMsg.AddTLV(tlv.Tag, tlv.Value)
		}

		// 发送消息
		if err := s.sendMessage(session.Conn, newMsg); err != nil {
			logger.Error(fmt.Sprintf("广播消息到会话 %d 失败: %v", session.ID, err))
		} else {
			session.IncrementSent()
		}

		return true
	})
}

// GetStats 获取统计信息
func (s *Server) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"active_connections": atomic.LoadInt64(&s.stats.activeConnections),
		"total_connections":  atomic.LoadUint64(&s.stats.totalConnections),
		"received_messages":  atomic.LoadUint64(&s.stats.receivedMessages),
		"sent_messages":      atomic.LoadUint64(&s.stats.sentMessages),
		"auth_failures":      atomic.LoadUint64(&s.stats.authFailures),
	}
}

// parseBindRequest 解析绑定请求
func parseBindRequest(payload []byte) (string, string, error) {
	if len(payload) < 2 {
		return "", "", errors.New("无效的绑定请求负载")
	}

	// 查找第一个空终止符
	var systemIDEnd int
	for i, b := range payload {
		if b == 0 {
			systemIDEnd = i
			break
		}
	}

	if systemIDEnd == 0 {
		return "", "", errors.New("无效的系统ID")
	}

	// 提取系统ID
	systemID := string(payload[:systemIDEnd])

	// 查找第二个空终止符
	var passwordEnd int
	for i := systemIDEnd + 1; i < len(payload); i++ {
		if payload[i] == 0 {
			passwordEnd = i
			break
		}
	}

	if passwordEnd <= systemIDEnd+1 {
		return "", "", errors.New("无效的密码")
	}

	// 提取密码
	password := string(payload[systemIDEnd+1 : passwordEnd])

	return systemID, password, nil
}

// handleDeliverSM 处理DELIVER_SM消息
func (s *Server) handleDeliverSM(session *Session, msg *protocol.Message) error {
	// 处理短信内容
	sourceAddr, destAddr, shortMessage, err := parseDeliverSM(msg.Payload)
	if err != nil {
		logger.Error(fmt.Sprintf("解析DELIVER_SM失败: %v", err))
		return err
	}

	logger.Info(fmt.Sprintf("收到短信 [序列号: %d]:\n 发送方: %s\n 接收方: %s\n 内容: %s",
		msg.Header.SequenceNumber, sourceAddr, destAddr, shortMessage))

	// 创建并发送响应
	resp := protocol.CreateDeliverSMResponse(msg.Header.SequenceNumber, protocol.SM_OK)
	return s.sendMessage(session.Conn, resp)
}

// parseDeliverSM 解析DELIVER_SM消息
func parseDeliverSM(payload []byte) (sourceAddr, destAddr, shortMessage string, err error) {
	// 简化的解析逻辑，实际实现可能需要更复杂的处理
	fields := bytes.Split(payload, []byte{0})
	if len(fields) < 4 {
		return "", "", "", errors.New("无效的DELIVER_SM格式")
	}

	sourceAddr = string(fields[1])
	destAddr = string(fields[2])
	shortMessage = string(fields[3])
	return
}

// GetSessionManager 返回服务器的会话管理器
func (s *Server) GetSessionManager() *SessionManager {
	return s.sessionMgr
}

// SendMessage 向会话发送消息
func (s *Server) SendMessage(session *Session, msg *protocol.Message) error {
	if session == nil || session.Conn == nil {
		return errors.New("会话无效或连接已关闭")
	}

	// 设置写入超时
	if err := session.Conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout)); err != nil {
		return err
	}

	// 发送消息
	_, err := session.Conn.Write(msg.Bytes())
	if err != nil {
		return err
	}

	atomic.AddUint64(&s.stats.sentMessages, 1)
	session.IncrementSent()

	return nil
}

// 添加 SetMessageHandler 方法
func (s *Server) SetMessageHandler(handler MessageHandler) {
	s.messageHandler = handler
}

// 添加 AddMessageInterceptor 方法
func (s *Server) AddMessageInterceptor(interceptor MessageInterceptor) {
	s.messageInterceptors = append(s.messageInterceptors, interceptor)
}

// // HandleMessage 处理来自分发器的消息
// func (s *Server) HandleMessage(msg *protocol.Message) error {
// 	// 对于DELIVER_SM消息，需要转发给客户端
// 	if msg.Header.CommandID == protocol.DELIVER_SM {
// 		logger.Info(fmt.Sprintf("收到DELIVER_SM消息，序列号: %d，准备转发给客户端",
// 			msg.Header.SequenceNumber))

// 		// 获取所有活跃会话
// 		sessions := s.sessionMgr.GetActiveSessions()
// 		if len(sessions) == 0 {
// 			logger.Warning("无活跃会话，无法转发DELIVER_SM消息")
// 			return fmt.Errorf("无活跃会话")
// 		}

// 		// 解析消息内容以进行日志记录
// 		sourceAddr, destAddr, content, _ := protocol.ParseMessageContent(msg)
// 		logger.Info(fmt.Sprintf("转发短信 [序列号: %d]:\n 发送方: %s\n 接收方: %s\n 内容: %s",
// 			msg.Header.SequenceNumber, sourceAddr, destAddr, content))

// 		// 遍历会话并转发消息
// 		var forwardErrors []error
// 		var forwardedCount int

// 		for _, session := range sessions {
// 			// 只向连接状态的客户端转发
// 			if session.Status == protocol.Session_Connected {
// 				// 创建新消息（使用新的序列号）
// 				newMsg := protocol.NewMessage(
// 					msg.Header.CommandID,
// 					msg.Header.CommandStatus,
// 					session.NextSequence(),
// 					msg.Payload,
// 				)

// 				// 添加原消息的TLV参数
// 				for _, tlv := range msg.TLVs {
// 					newMsg.AddTLV(tlv.Tag, tlv.Value)
// 				}

// 				// 发送消息
// 				if err := s.SendMessage(session, newMsg); err != nil {
// 					logger.Error(fmt.Sprintf("转发DELIVER_SM消息到会话 %d 失败: %v", session.ID, err))
// 					forwardErrors = append(forwardErrors, err)
// 				} else {
// 					forwardedCount++
// 					logger.Info(fmt.Sprintf("成功转发DELIVER_SM消息到会话 %d", session.ID))
// 				}
// 			}
// 		}

// 		// 检查是否至少转发给了一个客户端
// 		if forwardedCount == 0 {
// 			return fmt.Errorf("消息未能转发给任何客户端: %v", forwardErrors)
// 		}

// 		return nil
// 	}

// 	// 其他类型的消息交给messageHandler处理
// 	if s.messageHandler != nil {
// 		// 转换消息为字节数组
// 		rawMsg := msg.Bytes()

// 		// 应用拦截器
// 		for _, interceptor := range s.messageInterceptors {
// 			rawMsg = interceptor(rawMsg, true)
// 		}

// 		// 调用处理函数
// 		return s.messageHandler(rawMsg)
// 	}

//		return nil
//	}
//
// HandleMessage 处理来自分发器的消息
func (s *Server) HandleMessage(msg *protocol.Message) error {
	// 对于DELIVER_SM消息，需要转发给SMMC客户端
	if msg.Header.CommandID == protocol.DELIVER_SM {
		logger.Info(fmt.Sprintf("准备将DELIVER_SM消息转发给已连接的SMMC客户端，序列号: %d",
			msg.Header.SequenceNumber))

		// 获取所有活跃会话
		sessions := s.sessionMgr.GetActiveSessions()
		if len(sessions) == 0 {
			logger.Warning("无活跃会话，无法转发DELIVER_SM消息")
			return fmt.Errorf("无活跃会话")
		}

		// 获取消息内容用于日志
		sourceAddr, destAddr, content, _ := protocol.ParseMessageContent(msg)
		logger.Info(fmt.Sprintf("转发短信 [序列号: %d] 发送方: %s, 接收方: %s, 内容: %s",
			msg.Header.SequenceNumber, sourceAddr, destAddr, content))

		// 转发计数器
		var forwardedCount int

		// 遍历会话并转发消息
		for _, session := range sessions {
			// 只向已连接的客户端转发
			if session.Status == protocol.Session_Connected {
				// 创建新消息（使用新的序列号）
				newMsg := protocol.NewMessage(
					msg.Header.CommandID,
					msg.Header.CommandStatus,
					session.NextSequence(),
					msg.Payload,
				)

				// 发送消息
				if err := s.SendMessage(session, newMsg); err != nil {
					logger.Error(fmt.Sprintf("转发消息到会话 %d 失败: %v", session.ID, err))
				} else {
					forwardedCount++
					logger.Info(fmt.Sprintf("成功转发DELIVER_SM消息到会话 %d (系统ID: %s)",
						session.ID, session.SystemID))
				}
			}
		}

		logger.Info(fmt.Sprintf("DELIVER_SM消息已转发给 %d 个客户端", forwardedCount))
		return nil
	}

	// 其他类型的消息交给messageHandler处理
	if s.messageHandler != nil {
		// 转换消息为字节数组
		rawMsg := msg.Bytes()

		// 应用拦截器
		for _, interceptor := range s.messageInterceptors {
			rawMsg = interceptor(rawMsg, true)
		}

		// 调用处理函数
		return s.messageHandler(rawMsg)
	}

	return nil
}

// 添加Handle方法
func (s *Server) Handle(ctx context.Context, msg *protocol.Message) error {
	return s.HandleMessage(msg)
}

// 如果没有HandlerName方法，也需要添加
func (s *Server) HandlerName() string {
	return "SMMC服务器"
}

// // AddHandler 添加消息处理器
// func (s *Server) AddHandler(handler dispatcher.MessageHandler) {
// 	logger.Info(fmt.Sprintf("向服务器添加处理器: %s", handler.HandlerName()))
// }
