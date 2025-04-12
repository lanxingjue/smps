// internal/ha/config.go
package ha

import (
	"time"
)

// Role 节点角色
type Role string

const (
	// RoleMaster 主节点
	RoleMaster Role = "MASTER"

	// RoleSlave 备节点
	RoleSlave Role = "SLAVE"

	// RoleUnknown 未知角色
	RoleUnknown Role = "UNKNOWN"
)

// NodeState 节点状态
type NodeState string

const (
	// StateNormal 正常
	StateNormal NodeState = "NORMAL"

	// StateAbnormal 异常
	StateAbnormal NodeState = "ABNORMAL"

	// StateDown 宕机
	StateDown NodeState = "DOWN"

	// StateStarting 启动中
	StateStarting NodeState = "STARTING"

	// StateSwitching 切换中
	StateSwitching NodeState = "SWITCHING"

	// StateUnknown 未知
	StateUnknown NodeState = "UNKNOWN"
)

// ElectionState 选举状态
type ElectionState string

const (
	// ElectionIdle 空闲
	ElectionIdle ElectionState = "IDLE"

	// ElectionInProgress 选举中
	ElectionInProgress ElectionState = "IN_PROGRESS"

	// ElectionComplete 选举完成
	ElectionComplete ElectionState = "COMPLETE"
)

// ClusterState 集群状态
type ClusterState string

const (
	// ClusterStateNormal 正常
	ClusterStateNormal ClusterState = "NORMAL"

	// ClusterStateDegraded 降级
	ClusterStateDegraded ClusterState = "DEGRADED"

	// ClusterStateInvalid 无效
	ClusterStateInvalid ClusterState = "INVALID"

	// ClusterStateSwitching 切换中
	ClusterStateSwitching ClusterState = "SWITCHING"
)

// Config 高可用配置
type Config struct {

	// 添加启用字段
	Enabled bool // 是否启用高可用功能
	// 本节点ID
	NodeID string

	// 初始角色，默认为SLAVE
	InitialRole Role

	// 集群ID
	ClusterID string

	// 节点列表
	Nodes []NodeConfig

	// 心跳间隔
	HeartbeatInterval time.Duration

	// 心跳超时
	HeartbeatTimeout time.Duration

	// 选举超时
	ElectionTimeout time.Duration

	// 选举优先级，数值越大优先级越高
	ElectionPriority int

	// 自动故障切换
	AutoFailover bool

	// 自动故障切换等待时间
	FailoverWaitTime time.Duration

	// 选举仲裁方法: quorum, priority
	ElectionMethod string

	// 选举仲裁数量(quorum)
	QuorumSize int

	// 数据库配置
	DBConfig interface{}

	// 外部回调函数
	Callbacks Callbacks

	// VIP配置（如果有）
	VIPConfig *VIPConfig
}

// NodeConfig 节点配置
type NodeConfig struct {
	// 节点ID
	ID string

	// 节点地址
	Address string

	// 节点管理端口
	ManagementPort int

	// 节点数据同步端口
	SyncPort int

	// 节点优先级
	Priority int
}

// VIPConfig 虚拟IP配置
type VIPConfig struct {
	// 是否启用VIP
	Enabled bool

	// VIP地址
	Address string

	// 网卡设备
	Interface string

	// 子网掩码
	Netmask string
}

// Callbacks 回调函数集
type Callbacks struct {
	// 角色变更回调
	OnRoleChange func(oldRole, newRole Role) error

	// 状态变更回调
	OnStateChange func(oldState, newState NodeState) error

	// 切换前回调
	BeforeSwitchover func() error

	// 切换后回调
	AfterSwitchover func(success bool) error
}

// NewConfig 创建默认配置
func NewConfig() *Config {
	return &Config{
		Enabled:           false, // 默认不启用
		NodeID:            "node1",
		InitialRole:       RoleSlave,
		ClusterID:         "smps-cluster",
		HeartbeatInterval: 1 * time.Second,
		HeartbeatTimeout:  3 * time.Second,
		ElectionTimeout:   5 * time.Second,
		ElectionPriority:  100,
		AutoFailover:      true,
		FailoverWaitTime:  10 * time.Second,
		ElectionMethod:    "quorum",
		QuorumSize:        2,
		Callbacks: Callbacks{
			OnRoleChange:     func(oldRole, newRole Role) error { return nil },
			OnStateChange:    func(oldState, newState NodeState) error { return nil },
			BeforeSwitchover: func() error { return nil },
			AfterSwitchover:  func(success bool) error { return nil },
		},
		VIPConfig: &VIPConfig{
			Enabled: false,
		},
	}
}
