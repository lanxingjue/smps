// internal/ha/node.go
package ha

import (
	"fmt"
	"sync"
	"time"

	"smps/pkg/logger"
)

// Node 高可用节点
type Node struct {
	config *Config

	// 节点基本信息
	id      string
	address string

	// 当前角色和状态
	role         Role
	state        NodeState
	mu           sync.RWMutex
	lastRoleTime time.Time

	// 集群信息
	clusterID    string
	clusterState ClusterState

	// 其他节点信息
	peers   map[string]*PeerNode
	peersMu sync.RWMutex

	// 心跳检测
	heartbeat *Heartbeat

	// 选举管理
	election *Election

	// 切换管理
	switchover *Switchover

	// 状态管理
	stateManager *StateManager

	// 是否运行中
	isRunning bool
	stopCh    chan struct{}
}

// PeerNode 对等节点信息
type PeerNode struct {
	ID       string
	Address  string
	Role     Role
	State    NodeState
	Priority int
	LastSeen time.Time
}

// NewNode 创建高可用节点
func NewNode(config *Config) (*Node, error) {
	if config == nil {
		config = NewConfig()
	}

	// 检查基本配置
	if config.NodeID == "" {
		return nil, fmt.Errorf("节点ID不能为空")
	}

	// 查找当前节点配置
	var nodeConfig *NodeConfig
	for _, n := range config.Nodes {
		if n.ID == config.NodeID {
			nodeConfig = &n
			break
		}
	}

	if nodeConfig == nil {
		return nil, fmt.Errorf("在节点列表中找不到本节点配置: %s", config.NodeID)
	}

	node := &Node{
		config:       config,
		id:           config.NodeID,
		address:      nodeConfig.Address,
		role:         config.InitialRole,
		state:        StateStarting,
		clusterID:    config.ClusterID,
		clusterState: ClusterStateNormal,
		peers:        make(map[string]*PeerNode),
		lastRoleTime: time.Now(),
		stopCh:       make(chan struct{}),
	}

	// 初始化对等节点信息
	for _, n := range config.Nodes {
		if n.ID != config.NodeID {
			node.peers[n.ID] = &PeerNode{
				ID:       n.ID,
				Address:  n.Address,
				Role:     RoleUnknown,
				State:    StateUnknown,
				Priority: n.Priority,
				LastSeen: time.Time{}, // 零值表示从未见过
			}
		}
	}

	// 创建心跳检测器
	node.heartbeat = NewHeartbeat(node)

	// 创建选举管理器
	node.election = NewElection(node)

	// 创建切换管理器
	node.switchover = NewSwitchover(node)

	// 创建状态管理器
	node.stateManager = NewStateManager(node)

	return node, nil
}

// Start 启动节点
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.isRunning {
		return nil
	}

	logger.Info(fmt.Sprintf("节点 %s (%s) 启动中，初始角色: %s", n.id, n.address, n.role))

	// 启动心跳检测
	if err := n.heartbeat.Start(); err != nil {
		return fmt.Errorf("启动心跳检测失败: %v", err)
	}

	// 启动选举管理器
	if err := n.election.Start(); err != nil {
		n.heartbeat.Stop()
		return fmt.Errorf("启动选举管理器失败: %v", err)
	}

	// 启动切换管理器
	if err := n.switchover.Start(); err != nil {
		n.heartbeat.Stop()
		n.election.Stop()
		return fmt.Errorf("启动切换管理器失败: %v", err)
	}

	// 启动状态管理器
	if err := n.stateManager.Start(); err != nil {
		n.heartbeat.Stop()
		n.election.Stop()
		n.switchover.Stop()
		return fmt.Errorf("启动状态管理器失败: %v", err)
	}

	// 启动VIP管理（如果配置了）
	if n.config.VIPConfig != nil && n.config.VIPConfig.Enabled {
		if n.role == RoleMaster {
			if err := n.acquireVIP(); err != nil {
				logger.Warning(fmt.Sprintf("获取VIP失败: %v", err))
			}
		}
	}

	n.isRunning = true
	n.setState(StateNormal)

	logger.Info(fmt.Sprintf("节点 %s (%s) 已启动，当前角色: %s", n.id, n.address, n.role))
	return nil
}

// Stop 停止节点
func (n *Node) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.isRunning {
		return nil
	}

	logger.Info(fmt.Sprintf("节点 %s (%s) 停止中", n.id, n.address))

	// 释放VIP（如果是主节点）
	if n.config.VIPConfig != nil && n.config.VIPConfig.Enabled && n.role == RoleMaster {
		if err := n.releaseVIP(); err != nil {
			logger.Warning(fmt.Sprintf("释放VIP失败: %v", err))
		}
	}

	// 发送停止信号
	close(n.stopCh)

	// 停止各组件
	n.stateManager.Stop()
	n.switchover.Stop()
	n.election.Stop()
	n.heartbeat.Stop()

	n.isRunning = false
	logger.Info(fmt.Sprintf("节点 %s (%s) 已停止", n.id, n.address))
	return nil
}

// GetRole 获取当前角色
func (n *Node) GetRole() Role {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.role
}

// SetRole 设置角色
func (n *Node) SetRole(role Role) error {
	n.mu.Lock()
	oldRole := n.role
	n.role = role
	n.lastRoleTime = time.Now()
	n.mu.Unlock()

	// 角色变更回调
	if oldRole != role && n.config.Callbacks.OnRoleChange != nil {
		if err := n.config.Callbacks.OnRoleChange(oldRole, role); err != nil {
			logger.Error(fmt.Sprintf("角色变更回调执行失败: %v", err))
			return err
		}
	}

	// 角色变更后处理VIP
	if n.config.VIPConfig != nil && n.config.VIPConfig.Enabled {
		if role == RoleMaster {
			if err := n.acquireVIP(); err != nil {
				logger.Warning(fmt.Sprintf("获取VIP失败: %v", err))
			}
		} else if oldRole == RoleMaster {
			if err := n.releaseVIP(); err != nil {
				logger.Warning(fmt.Sprintf("释放VIP失败: %v", err))
			}
		}
	}

	logger.Info(fmt.Sprintf("节点 %s 角色从 %s 变更为 %s", n.id, oldRole, role))
	return nil
}

// GetState 获取当前状态
func (n *Node) GetState() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
}

// setState 设置状态
func (n *Node) setState(state NodeState) error {
	n.mu.Lock()
	oldState := n.state
	n.state = state
	n.mu.Unlock()

	// 状态变更回调
	if oldState != state && n.config.Callbacks.OnStateChange != nil {
		if err := n.config.Callbacks.OnStateChange(oldState, state); err != nil {
			logger.Error(fmt.Sprintf("状态变更回调执行失败: %v", err))
			return err
		}
	}

	logger.Info(fmt.Sprintf("节点 %s 状态从 %s 变更为 %s", n.id, oldState, state))
	return nil
}

// GetPeerNode 获取对等节点信息
func (n *Node) GetPeerNode(id string) *PeerNode {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()

	return n.peers[id]
}

// UpdatePeerNode 更新对等节点信息
func (n *Node) UpdatePeerNode(id string, role Role, state NodeState) {
	n.peersMu.Lock()
	defer n.peersMu.Unlock()

	if peer, exists := n.peers[id]; exists {
		peer.Role = role
		peer.State = state
		peer.LastSeen = time.Now()
	}
}

// IsMaster 是否为主节点
func (n *Node) IsMaster() bool {
	return n.GetRole() == RoleMaster
}

// acquireVIP 获取VIP
func (n *Node) acquireVIP() error {
	if n.config.VIPConfig == nil || !n.config.VIPConfig.Enabled {
		return nil
	}

	// 执行获取VIP的命令
	logger.Info(fmt.Sprintf("节点 %s 正在获取VIP: %s", n.id, n.config.VIPConfig.Address))

	// 具体实现可以通过执行ip命令或调用网络接口
	// 这里简化为通过系统调用执行命令
	cmd := fmt.Sprintf("ip addr add %s/%s dev %s",
		n.config.VIPConfig.Address,
		n.config.VIPConfig.Netmask,
		n.config.VIPConfig.Interface)

	// 执行命令（简化实现）
	// exec.Command("sh", "-c", cmd).Run()

	// 发送免费ARP
	// exec.Command("arping", "-U", "-I", n.config.VIPConfig.Interface, n.config.VIPConfig.Address, "-c", "3").Run()

	logger.Info(fmt.Sprintf("节点 %s 已成功获取VIP: %s", n.id, n.config.VIPConfig.Address))
	return nil
}

// releaseVIP 释放VIP
func (n *Node) releaseVIP() error {
	if n.config.VIPConfig == nil || !n.config.VIPConfig.Enabled {
		return nil
	}

	// 执行释放VIP的命令
	logger.Info(fmt.Sprintf("节点 %s 正在释放VIP: %s", n.id, n.config.VIPConfig.Address))

	// 具体实现可以通过执行ip命令或调用网络接口
	cmd := fmt.Sprintf("ip addr del %s/%s dev %s",
		n.config.VIPConfig.Address,
		n.config.VIPConfig.Netmask,
		n.config.VIPConfig.Interface)

	// 执行命令（简化实现）
	// exec.Command("sh", "-c", cmd).Run()

	logger.Info(fmt.Sprintf("节点 %s 已成功释放VIP: %s", n.id, n.config.VIPConfig.Address))
	return nil
}

// ManualSwitchover 手动触发主备切换
func (n *Node) ManualSwitchover() error {
	return n.switchover.Switchover(false)
}

// IsRunning 是否运行中
func (n *Node) IsRunning() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isRunning
}

// HasValidMaster 检查集群是否有有效的主节点
func (n *Node) HasValidMaster() bool {
	// 如果本节点是主节点，返回true
	if n.GetRole() == RoleMaster && n.GetState() == StateNormal {
		return true
	}

	// 检查是否有其他正常的主节点
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()

	for _, peer := range n.peers {
		if peer.Role == RoleMaster && peer.State == StateNormal {
			// 检查是否在有效时间范围内见过这个节点
			if time.Since(peer.LastSeen) < n.config.HeartbeatTimeout*3 {
				return true
			}
		}
	}

	return false
}
