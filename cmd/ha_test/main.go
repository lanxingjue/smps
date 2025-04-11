// cmd/ha_test/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smps/internal/ha"
	"smps/pkg/logger"
)

func main() {
	// 解析命令行参数
	nodeID := flag.String("node", "node1", "节点ID")
	role := flag.String("role", "slave", "初始角色: master/slave")
	clusterID := flag.String("cluster", "smps-cluster", "集群ID")
	flag.Parse()

	// 初始化日志
	logger.Init("smps_ha_" + *nodeID)

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建配置
	config := ha.NewConfig()
	config.NodeID = *nodeID
	config.ClusterID = *clusterID

	// 设置初始角色
	if *role == "master" {
		config.InitialRole = ha.RoleMaster
	} else {
		config.InitialRole = ha.RoleSlave
	}

	// 配置节点
	config.Nodes = []ha.NodeConfig{
		{
			ID:             "node1",
			Address:        "127.0.0.1",
			ManagementPort: 9101,
			SyncPort:       9201,
			Priority:       100,
		},
		{
			ID:             "node2",
			Address:        "127.0.0.1",
			ManagementPort: 9102,
			SyncPort:       9202,
			Priority:       90,
		},
	}

	// 配置VIP
	config.VIPConfig = &ha.VIPConfig{
		Enabled:   true,
		Address:   "192.168.1.100",
		Interface: "eth0",
		Netmask:   "24",
	}

	// 配置回调函数
	config.Callbacks = ha.Callbacks{
		OnRoleChange: func(oldRole, newRole ha.Role) error {
			logger.Info(fmt.Sprintf("角色变更: %s -> %s", oldRole, newRole))
			return nil
		},
		OnStateChange: func(oldState, newState ha.NodeState) error {
			logger.Info(fmt.Sprintf("状态变更: %s -> %s", oldState, newState))
			return nil
		},
		BeforeSwitchover: func() error {
			logger.Info("准备切换...")
			return nil
		},
		AfterSwitchover: func(success bool) error {
			logger.Info(fmt.Sprintf("切换完成，结果: %t", success))
			return nil
		},
	}

	// 创建高可用管理器
	manager, err := ha.NewManager(config)
	if err != nil {
		logger.Fatal(fmt.Sprintf("创建高可用管理器失败: %v", err))
	}

	// 启动高可用管理器
	if err := manager.Start(); err != nil {
		logger.Fatal(fmt.Sprintf("启动高可用管理器失败: %v", err))
	}

	// 输出初始状态
	logger.Info(fmt.Sprintf("节点 %s 已启动，初始角色: %s", *nodeID, manager.GetRole()))

	// 定期输出状态
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				role := manager.GetRole()
				state := manager.GetState()
				logger.Info(fmt.Sprintf("节点状态: 角色=%s, 状态=%s", role, state))
			case <-ctx.Done():
				return
			}
		}
	}()

	// 注册信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	<-sigCh
	logger.Info("接收到关闭信号，开始优雅关闭...")

	// 停止高可用管理器
	if err := manager.Stop(); err != nil {
		logger.Error(fmt.Sprintf("停止高可用管理器失败: %v", err))
	}

	logger.Info("节点已停止")
}
