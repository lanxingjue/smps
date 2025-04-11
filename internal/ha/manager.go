// internal/ha/manager.go
package ha

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"smps/pkg/logger"
)

// Manager 高可用管理器
type Manager struct {
	node       *Node
	config     *Config
	httpServer *http.Server
	isRunning  bool
	mu         sync.Mutex
}

// NewManager 创建高可用管理器
func NewManager(config *Config) (*Manager, error) {
	// 创建节点
	node, err := NewNode(config)
	if err != nil {
		return nil, err
	}

	return &Manager{
		node:   node,
		config: config,
	}, nil
}

// Start 启动高可用管理器
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return nil
	}

	// 启动节点
	if err := m.node.Start(); err != nil {
		return fmt.Errorf("启动节点失败: %v", err)
	}

	// 启动HTTP管理服务器
	if err := m.startHTTPServer(); err != nil {
		m.node.Stop()
		return fmt.Errorf("启动HTTP管理服务器失败: %v", err)
	}

	m.isRunning = true

	logger.Info(fmt.Sprintf("高可用管理器已启动，节点ID: %s, 角色: %s",
		m.node.id, m.node.GetRole()))

	return nil
}

// Stop 停止高可用管理器
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	// 停止HTTP服务器
	if m.httpServer != nil {
		m.httpServer.Close()
	}

	// 停止节点
	m.node.Stop()

	m.isRunning = false
	logger.Info("高可用管理器已停止")

	return nil
}

// GetNode 获取节点
func (m *Manager) GetNode() *Node {
	return m.node
}

// GetRole 获取角色
func (m *Manager) GetRole() Role {
	return m.node.GetRole()
}

// GetState 获取状态
func (m *Manager) GetState() NodeState {
	return m.node.GetState()
}

// SwitchToMaster 切换为主节点
func (m *Manager) SwitchToMaster() error {
	if m.node.GetRole() == RoleMaster {
		return nil
	}

	return m.node.switchover.Switchover(false)
}

// SwitchToSlave 切换为备节点
func (m *Manager) SwitchToSlave() error {
	if m.node.GetRole() == RoleSlave {
		return nil
	}

	return m.node.switchover.Switchover(false)
}

// startHTTPServer 启动HTTP管理服务器
func (m *Manager) startHTTPServer() error {
	// 查找本节点配置
	var nodeConfig *NodeConfig
	for _, n := range m.config.Nodes {
		if n.ID == m.node.id {
			nodeConfig = &n
			break
		}
	}

	if nodeConfig == nil {
		return fmt.Errorf("找不到本节点配置")
	}

	// 创建HTTP服务器
	mux := http.NewServeMux()

	// 状态接口
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]interface{}{
			"node_id":          m.node.id,
			"role":             m.node.GetRole(),
			"state":            m.node.GetState(),
			"cluster_state":    m.node.clusterState,
			"last_role_change": m.node.lastRoleTime.Format(time.RFC3339),
			"peers":            m.getPeersInfo(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// 手动切换接口
	mux.HandleFunc("/switch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Role string `json:"role"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("解析请求失败: %v", err), http.StatusBadRequest)
			return
		}

		var err error
		switch req.Role {
		case "master":
			err = m.SwitchToMaster()
		case "slave":
			err = m.SwitchToSlave()
		default:
			http.Error(w, "无效的角色，请使用 'master' 或 'slave'", http.StatusBadRequest)
			return
		}

		if err != nil {
			http.Error(w, fmt.Sprintf("切换失败: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "切换请求已接受",
			"role":   string(m.node.GetRole()),
		})
	})

	// 启动HTTP服务器
	addr := fmt.Sprintf("%s:%d", nodeConfig.Address, nodeConfig.ManagementPort+1)
	m.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := m.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(fmt.Sprintf("HTTP管理服务器错误: %v", err))
		}
	}()

	logger.Info(fmt.Sprintf("HTTP管理服务器已启动，监听地址: %s", addr))

	return nil
}

// getPeersInfo 获取对等节点信息
func (m *Manager) getPeersInfo() map[string]interface{} {
	m.node.peersMu.RLock()
	defer m.node.peersMu.RUnlock()

	result := make(map[string]interface{})

	for id, peer := range m.node.peers {
		result[id] = map[string]interface{}{
			"address":   peer.Address,
			"role":      peer.Role,
			"state":     peer.State,
			"priority":  peer.Priority,
			"last_seen": peer.LastSeen.Format(time.RFC3339),
		}
	}

	return result
}
