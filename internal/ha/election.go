// internal/ha/election.go
package ha

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"smps/pkg/logger"
)

// ElectionMessage 选举消息
type ElectionMessage struct {
	Type        string `json:"type"`         // VOTE_REQUEST, VOTE_RESPONSE
	NodeID      string `json:"node_id"`      // 节点ID
	ClusterID   string `json:"cluster_id"`   // 集群ID
	Term        int64  `json:"term"`         // 选举轮次
	Priority    int    `json:"priority"`     // 优先级
	VoteGranted bool   `json:"vote_granted"` // 是否投票
}

// Election 选举管理器
type Election struct {
	node          *Node
	isRunning     bool
	state         ElectionState
	term          int64
	votesReceived map[string]bool
	mu            sync.Mutex
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewElection 创建选举管理器
func NewElection(node *Node) *Election {
	return &Election{
		node:          node,
		state:         ElectionIdle,
		term:          0,
		votesReceived: make(map[string]bool),
		stopCh:        make(chan struct{}),
	}
}

// Start 启动选举管理器
func (e *Election) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.isRunning {
		return nil
	}

	e.isRunning = true
	logger.Info(fmt.Sprintf("节点 %s 选举管理器已启动", e.node.id))
	return nil
}

// Stop 停止选举管理器
func (e *Election) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.isRunning {
		return
	}

	// 关闭停止通道
	close(e.stopCh)

	// 等待所有协程退出
	e.wg.Wait()

	e.isRunning = false
	logger.Info(fmt.Sprintf("节点 %s 选举管理器已停止", e.node.id))
}

// StartElection 启动选举
func (e *Election) StartElection() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.isRunning || e.state != ElectionIdle {
		return
	}

	// 如果当前已经是主节点，不需要发起选举
	if e.node.GetRole() == RoleMaster {
		return
	}

	// 增加选举轮次
	e.term++
	e.state = ElectionInProgress
	e.votesReceived = make(map[string]bool)

	// 自己给自己投票
	e.votesReceived[e.node.id] = true

	logger.Info(fmt.Sprintf("节点 %s 启动选举，轮次: %d", e.node.id, e.term))

	// 启动选举协程
	e.wg.Add(1)
	go e.runElection()
}

// runElection 运行选举过程
func (e *Election) runElection() {
	defer e.wg.Done()

	// 创建定时器
	timer := time.NewTimer(e.node.config.ElectionTimeout)
	defer timer.Stop()

	// 向所有对等节点发送选举请求
	e.requestVotesFromAllPeers()

	// 等待选举结果或超时
	select {
	case <-timer.C:
		// 选举超时
		e.mu.Lock()
		defer e.mu.Unlock()

		// 检查是否已经获得足够的选票
		if e.hasQuorum() {
			// 获得多数选票，成为主节点
			logger.Info(fmt.Sprintf("节点 %s 选举成功，轮次: %d", e.node.id, e.term))
			e.state = ElectionComplete
			e.node.SetRole(RoleMaster)
		} else {
			// 未获得足够选票，保持备节点
			logger.Info(fmt.Sprintf("节点 %s 选举超时，未获得足够选票", e.node.id))
			e.state = ElectionIdle
		}

	case <-e.stopCh:
		// 外部停止信号
		return
	}
}

// requestVotesFromAllPeers 向所有对等节点请求投票
func (e *Election) requestVotesFromAllPeers() {
	// 获取节点列表
	e.node.peersMu.RLock()
	peers := make(map[string]*PeerNode)
	for id, peer := range e.node.peers {
		peers[id] = peer
	}
	e.node.peersMu.RUnlock()

	// 创建选举请求消息
	msg := ElectionMessage{
		Type:      "VOTE_REQUEST",
		NodeID:    e.node.id,
		ClusterID: e.node.clusterID,
		Term:      e.term,
		Priority:  e.node.config.ElectionPriority,
	}

	// 向每个对等节点发送选举请求
	for id := range peers {
		go e.requestVoteFromPeer(id, msg)
	}
}

// requestVoteFromPeer 向指定对等节点请求投票
func (e *Election) requestVoteFromPeer(id string, msg ElectionMessage) {
	// 查找对等节点配置
	var peerConfig *NodeConfig
	for _, n := range e.node.config.Nodes {
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
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("连接选举节点失败: %s, %v", addr, err))
		return
	}
	defer conn.Close()

	// 设置写入超时
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))

	// 发送选举请求
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		logger.Error(fmt.Sprintf("发送选举请求失败: %v", err))
		return
	}

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// 接收响应
	decoder := json.NewDecoder(conn)
	var response ElectionMessage
	if err := decoder.Decode(&response); err != nil {
		logger.Error(fmt.Sprintf("接收选举响应失败: %v", err))
		return
	}

	// 处理投票响应
	e.mu.Lock()
	defer e.mu.Unlock()

	// 检查轮次是否一致
	if response.Term != e.term {
		return
	}

	// 记录投票结果
	e.votesReceived[response.NodeID] = response.VoteGranted

	// 检查是否已经获得足够的选票
	if e.hasQuorum() {
		logger.Info(fmt.Sprintf("节点 %s 获得多数选票，成为主节点", e.node.id))
		e.state = ElectionComplete
		e.node.SetRole(RoleMaster)
	}
}

// hasQuorum 是否获得多数投票
func (e *Election) hasQuorum() bool {
	// 计算获得的有效选票
	votes := 0
	for _, granted := range e.votesReceived {
		if granted {
			votes++
		}
	}

	// 计算所需的选票数量
	totalNodes := len(e.node.peers) + 1 // +1 是本节点
	neededVotes := totalNodes/2 + 1     // 多数票

	return votes >= neededVotes
}

// HandleVoteRequest 处理投票请求
func (e *Election) HandleVoteRequest(request ElectionMessage) ElectionMessage {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 创建响应
	response := ElectionMessage{
		Type:        "VOTE_RESPONSE",
		NodeID:      e.node.id,
		ClusterID:   e.node.clusterID,
		Term:        request.Term,
		VoteGranted: false,
	}

	// 检查集群ID是否一致
	if request.ClusterID != e.node.clusterID {
		logger.Warning(fmt.Sprintf("收到不同集群的选举请求: %s", request.ClusterID))
		return response
	}

	// 检查是否应该投票
	shouldVote := false

	// 如果当前节点是主节点且运行正常，不投票
	if e.node.GetRole() == RoleMaster && e.node.GetState() == StateNormal {
		shouldVote = false
	} else if request.Term > e.term {
		// 如果请求的轮次更高，投票
		shouldVote = true
		e.term = request.Term
	} else if request.Term == e.term && request.Priority > e.node.config.ElectionPriority {
		// 如果轮次相同但优先级更高，投票
		shouldVote = true
	}

	response.VoteGranted = shouldVote

	if shouldVote {
		logger.Info(fmt.Sprintf("节点 %s 给节点 %s 投票，轮次: %d", e.node.id, request.NodeID, request.Term))
	} else {
		logger.Info(fmt.Sprintf("节点 %s 拒绝给节点 %s 投票，轮次: %d", e.node.id, request.NodeID, request.Term))
	}

	return response
}
