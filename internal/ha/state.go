// internal/ha/state.go
package ha

import (
	"fmt"
	"sync"
	"time"

	"smps/pkg/logger"
)

// StateManager 状态管理器
type StateManager struct {
	node      *Node
	isRunning bool
	mu        sync.Mutex
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// NewStateManager 创建状态管理器
func NewStateManager(node *Node) *StateManager {
	return &StateManager{
		node:   node,
		stopCh: make(chan struct{}),
	}
}

// Start 启动状态管理器
func (s *StateManager) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return nil
	}

	s.isRunning = true

	// 启动状态监控协程
	s.wg.Add(1)
	go s.monitorState()

	logger.Info(fmt.Sprintf("节点 %s 状态管理器已启动", s.node.id))
	return nil
}

// Stop 停止状态管理器
func (s *StateManager) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}

	// 关闭停止通道
	close(s.stopCh)

	// 等待所有协程退出
	s.wg.Wait()

	s.isRunning = false
	logger.Info(fmt.Sprintf("节点 %s 状态管理器已停止", s.node.id))
}

// monitorState 监控状态
func (s *StateManager) monitorState() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.node.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkClusterState()
		}
	}
}

// checkClusterState 检查集群状态
func (s *StateManager) checkClusterState() {
	// 当前节点的角色和状态
	role := s.node.GetRole()
	state := s.node.GetState()

	// 主节点检查逻辑
	if role == RoleMaster {
		// 主节点健康检查
		if state != StateNormal {
			logger.Warning(fmt.Sprintf("主节点 %s 状态异常: %s", s.node.id, state))

			// 如果主节点异常，可以主动降级为备节点
			if state == StateAbnormal {
				logger.Warning(fmt.Sprintf("主节点 %s 主动降级为备节点", s.node.id))
				s.node.SetRole(RoleSlave)
			}
		}
	} else if role == RoleSlave {
		// 备节点检查是否有主节点
		if !s.node.HasValidMaster() {
			logger.Warning(fmt.Sprintf("备节点 %s 未检测到有效的主节点", s.node.id))

			// 如果自动故障切换启用，尝试选举为主节点
			if s.node.config.AutoFailover && state == StateNormal {
				logger.Info(fmt.Sprintf("备节点 %s 尝试启动选举", s.node.id))
				s.node.election.StartElection()
			}
		}
	}

	// 更新集群状态
	s.updateClusterState()
}

// updateClusterState 更新集群状态
func (s *StateManager) updateClusterState() {
	// 获取所有节点状态
	s.node.peersMu.RLock()
	peers := make(map[string]*PeerNode)
	for id, peer := range s.node.peers {
		peers[id] = peer
	}
	s.node.peersMu.RUnlock()

	// 统计正常节点数量
	normalNodes := 0
	if s.node.GetState() == StateNormal {
		normalNodes++
	}

	for _, peer := range peers {
		if peer.State == StateNormal {
			normalNodes++
		}
	}

	// 统计主节点数量
	masterCount := 0
	if s.node.GetRole() == RoleMaster {
		masterCount++
	}

	for _, peer := range peers {
		if peer.Role == RoleMaster {
			masterCount++
		}
	}

	// 更新集群状态
	var newState ClusterState

	if masterCount == 0 {
		newState = ClusterStateInvalid
	} else if masterCount > 1 {
		newState = ClusterStateInvalid
		logger.Error(fmt.Sprintf("检测到多个主节点: %d", masterCount))
	} else if normalNodes < len(peers)+1 { // +1 是本节点
		newState = ClusterStateDegraded
	} else {
		newState = ClusterStateNormal
	}

	// 如果集群状态发生变化，记录日志
	if s.node.clusterState != newState {
		logger.Info(fmt.Sprintf("集群状态从 %s 变更为 %s", s.node.clusterState, newState))
		s.node.clusterState = newState
	}
}

// GetClusterState 获取集群状态
func (s *StateManager) GetClusterState() ClusterState {
	return s.node.clusterState
}
