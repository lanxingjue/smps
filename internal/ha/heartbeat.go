// internal/ha/heartbeat.go
package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"smps/pkg/logger"
)

// HeartbeatMessage 心跳消息
type HeartbeatMessage struct {
	NodeID     string    `json:"node_id"`
	ClusterID  string    `json:"cluster_id"`
	Role       Role      `json:"role"`
	State      NodeState `json:"state"`
	Timestamp  int64     `json:"timestamp"`
	Generation int64     `json:"generation"`
}

// Heartbeat 心跳检测器
type Heartbeat struct {
	node       *Node
	listener   net.Listener
	isRunning  bool
	mu         sync.Mutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
	generation int64
}

// NewHeartbeat 创建心跳检测器
func NewHeartbeat(node *Node) *Heartbeat {
	return &Heartbeat{
		node:       node,
		stopCh:     make(chan struct{}),
		generation: time.Now().Unix(),
	}
}

// Start 启动心跳检测
func (h *Heartbeat) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.isRunning {
		return nil
	}

	// 查找本节点配置
	var nodeConfig *NodeConfig
	for _, n := range h.node.config.Nodes {
		if n.ID == h.node.id {
			nodeConfig = &n
			break
		}
	}

	if nodeConfig == nil {
		return fmt.Errorf("找不到本节点配置")
	}

	// 启动心跳监听
	addr := fmt.Sprintf("%s:%d", nodeConfig.Address, nodeConfig.ManagementPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("启动心跳监听失败: %v", err)
	}

	h.listener = listener
	h.isRunning = true

	// 启动心跳接收协程
	h.wg.Add(1)
	go h.receiveHeartbeats()

	// 启动心跳发送协程
	h.wg.Add(1)
	go h.sendHeartbeats()

	logger.Info(fmt.Sprintf("节点 %s 心跳检测已启动，监听地址: %s", h.node.id, addr))

	return nil
}

// Stop 停止心跳检测
func (h *Heartbeat) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.isRunning {
		return
	}

	// 关闭停止通道
	close(h.stopCh)

	// 关闭监听器
	if h.listener != nil {
		h.listener.Close()
	}

	// 等待所有协程退出
	h.wg.Wait()

	h.isRunning = false
	logger.Info(fmt.Sprintf("节点 %s 心跳检测已停止", h.node.id))
}

// receiveHeartbeats 接收心跳
func (h *Heartbeat) receiveHeartbeats() {
	defer h.wg.Done()

	for {
		// 检查是否停止
		select {
		case <-h.stopCh:
			return
		default:
			// 继续处理
		}

		// 接受连接
		conn, err := h.listener.Accept()
		if err != nil {
			select {
			case <-h.stopCh:
				return
			default:
				logger.Error(fmt.Sprintf("接受心跳连接失败: %v", err))
				time.Sleep(time.Second)
				continue
			}
		}

		// 处理连接
		go func(conn net.Conn) {
			defer conn.Close()

			// 设置读取超时
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))

			// 解码心跳消息
			decoder := json.NewDecoder(conn)
			var msg HeartbeatMessage
			if err := decoder.Decode(&msg); err != nil {
				logger.Error(fmt.Sprintf("解码心跳消息失败: %v", err))
				return
			}

			// 验证集群ID
			if msg.ClusterID != h.node.clusterID {
				logger.Warning(fmt.Sprintf("收到不同集群的心跳消息: %s", msg.ClusterID))
				return
			}

			// 更新对等节点信息
			h.node.UpdatePeerNode(msg.NodeID, msg.Role, msg.State)

			// 响应心跳
			response := HeartbeatMessage{
				NodeID:     h.node.id,
				ClusterID:  h.node.clusterID,
				Role:       h.node.GetRole(),
				State:      h.node.GetState(),
				Timestamp:  time.Now().Unix(),
				Generation: h.generation,
			}

			// 设置写入超时
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

			// 编码响应
			encoder := json.NewEncoder(conn)
			if err := encoder.Encode(response); err != nil {
				logger.Error(fmt.Sprintf("编码心跳响应失败: %v", err))
				return
			}
		}(conn)
	}
}

// sendHeartbeats 发送心跳
func (h *Heartbeat) sendHeartbeats() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.node.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			// 向所有对等节点发送心跳
			h.sendHeartbeatToAllPeers()
		}
	}
}

// sendHeartbeatToAllPeers 向所有对等节点发送心跳
func (h *Heartbeat) sendHeartbeatToAllPeers() {
	// 获取节点列表
	h.node.peersMu.RLock()
	peers := make(map[string]*PeerNode)
	for id, peer := range h.node.peers {
		peers[id] = peer
	}
	h.node.peersMu.RUnlock()

	// 准备心跳消息
	msg := HeartbeatMessage{
		NodeID:     h.node.id,
		ClusterID:  h.node.clusterID,
		Role:       h.node.GetRole(),
		State:      h.node.GetState(),
		Timestamp:  time.Now().Unix(),
		Generation: h.generation,
	}

	// 向每个对等节点发送心跳
	for id, peer := range peers {
		go h.sendHeartbeatToPeer(id, peer, msg)
	}
}

// sendHeartbeatToPeer 向指定对等节点发送心跳
func (h *Heartbeat) sendHeartbeatToPeer(id string, peer *PeerNode, msg HeartbeatMessage) {
	// 查找对等节点配置
	var peerConfig *NodeConfig
	for _, n := range h.node.config.Nodes {
		if n.ID == id {
			peerConfig = &n
			break
		}
	}

	if peerConfig == nil {
		logger.Error(fmt.Sprintf("找不到对等节点配置: %s", id))
		return
	}

	// 连接对等节点
	addr := fmt.Sprintf("%s:%d", peerConfig.Address, peerConfig.ManagementPort)

	// 设置连接超时
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 创建拨号器
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		// 更新节点状态为异常
		h.node.UpdatePeerNode(id, peer.Role, StateAbnormal)
		return
	}
	defer conn.Close()

	// 设置写入超时
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))

	// 发送心跳消息
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		logger.Error(fmt.Sprintf("发送心跳消息失败: %v", err))
		return
	}

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// 接收响应
	decoder := json.NewDecoder(conn)
	var response HeartbeatMessage
	if err := decoder.Decode(&response); err != nil {
		logger.Error(fmt.Sprintf("接收心跳响应失败: %v", err))
		return
	}

	// 更新对等节点信息
	h.node.UpdatePeerNode(id, response.Role, response.State)
}
