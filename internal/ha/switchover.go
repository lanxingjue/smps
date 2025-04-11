// internal/ha/switchover.go
package ha

import (
	"fmt"
	"sync"
	"time"

	"smps/pkg/logger"
)

// Switchover 主备切换管理器
type Switchover struct {
	node           *Node
	isRunning      bool
	isSwitching    bool
	mu             sync.Mutex
	stopCh         chan struct{}
	wg             sync.WaitGroup
	lastSwitchTime time.Time
}

// NewSwitchover 创建主备切换管理器
func NewSwitchover(node *Node) *Switchover {
	return &Switchover{
		node:           node,
		stopCh:         make(chan struct{}),
		lastSwitchTime: time.Time{}, // 零时间表示从未切换过
	}
}

// Start 启动主备切换管理器
func (s *Switchover) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return nil
	}

	s.isRunning = true
	logger.Info(fmt.Sprintf("节点 %s 主备切换管理器已启动", s.node.id))
	return nil
}

// Stop 停止主备切换管理器
func (s *Switchover) Stop() {
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
	logger.Info(fmt.Sprintf("节点 %s 主备切换管理器已停止", s.node.id))
}

// Switchover 触发主备切换
func (s *Switchover) Switchover(isFailover bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning || s.isSwitching {
		return fmt.Errorf("无法执行切换，系统状态不允许")
	}

	// 检查最后切换时间，避免频繁切换
	if !s.lastSwitchTime.IsZero() && time.Since(s.lastSwitchTime) < 30*time.Second {
		return fmt.Errorf("切换太频繁，请稍后再试")
	}

	// 检查当前角色
	currentRole := s.node.GetRole()

	// 记录切换开始
	s.isSwitching = true
	s.node.clusterState = ClusterStateSwitching
	s.lastSwitchTime = time.Now()

	// 更新节点状态
	oldState := s.node.GetState()
	s.node.setState(StateSwitching)

	// 执行切换前回调
	if s.node.config.Callbacks.BeforeSwitchover != nil {
		if err := s.node.config.Callbacks.BeforeSwitchover(); err != nil {
			// 回调失败，恢复状态
			s.isSwitching = false
			s.node.setState(oldState)
			s.node.clusterState = ClusterStateNormal
			return fmt.Errorf("切换前回调失败: %v", err)
		}
	}

	// 切换类型日志
	if isFailover {
		logger.Warning(fmt.Sprintf("节点 %s 开始执行故障转移", s.node.id))
	} else {
		logger.Info(fmt.Sprintf("节点 %s 开始执行手动切换", s.node.id))
	}

	// 启动切换协程
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			s.isSwitching = false
			s.mu.Unlock()
		}()

		var err error
		var success bool

		// 根据当前角色执行不同的切换逻辑
		if currentRole == RoleMaster {
			// 主节点降级为备节点
			err = s.switchToSlave()
			success = err == nil
		} else {
			// 备节点升级为主节点
			err = s.switchToMaster()
			success = err == nil
		}

		// 切换完成后恢复状态
		s.node.setState(StateNormal)
		s.node.clusterState = ClusterStateNormal

		// 执行切换后回调
		if s.node.config.Callbacks.AfterSwitchover != nil {
			s.node.config.Callbacks.AfterSwitchover(success)
		}

		if err != nil {
			logger.Error(fmt.Sprintf("主备切换失败: %v", err))
		} else {
			logger.Info(fmt.Sprintf("主备切换成功，当前角色: %s", s.node.GetRole()))
		}
	}()

	return nil
}

// switchToMaster 切换为主节点
func (s *Switchover) switchToMaster() error {
	// 检查是否已经有主节点
	if s.node.HasValidMaster() {
		return fmt.Errorf("已存在有效的主节点，无法切换")
	}

	// 更改角色为主节点
	if err := s.node.SetRole(RoleMaster); err != nil {
		return fmt.Errorf("设置主节点角色失败: %v", err)
	}

	// 如果配置了VIP，则获取VIP
	if s.node.config.VIPConfig != nil && s.node.config.VIPConfig.Enabled {
		if err := s.node.acquireVIP(); err != nil {
			logger.Warning(fmt.Sprintf("获取VIP失败: %v", err))
		}
	}

	// 等待一定时间确保切换稳定
	time.Sleep(2 * time.Second)

	return nil
}

// switchToSlave 切换为备节点
func (s *Switchover) switchToSlave() error {
	// 释放VIP（如果有）
	if s.node.config.VIPConfig != nil && s.node.config.VIPConfig.Enabled {
		if err := s.node.releaseVIP(); err != nil {
			logger.Warning(fmt.Sprintf("释放VIP失败: %v", err))
		}
	}

	// 更改角色为备节点
	if err := s.node.SetRole(RoleSlave); err != nil {
		return fmt.Errorf("设置备节点角色失败: %v", err)
	}

	// 等待一定时间确保切换稳定
	time.Sleep(2 * time.Second)

	return nil
}

// IsSwitching 是否正在切换
func (s *Switchover) IsSwitching() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.isSwitching
}

// GetLastSwitchTime 获取最后切换时间
func (s *Switchover) GetLastSwitchTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.lastSwitchTime
}
