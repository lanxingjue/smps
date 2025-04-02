// internal/auth/account.go
package auth

import (
	"time"
)

// AccountManager 账户管理器
type AccountManager struct {
	authenticator  *Authenticator
	reloadInterval time.Duration
	done           chan struct{}
}

// NewAccountManager 创建账户管理器
func NewAccountManager(authenticator *Authenticator, reloadInterval time.Duration) *AccountManager {
	return &AccountManager{
		authenticator:  authenticator,
		reloadInterval: reloadInterval,
		done:           make(chan struct{}),
	}
}

// Start 启动账户管理器
func (m *AccountManager) Start() error {
	// 初始加载
	if err := m.authenticator.LoadAccounts(); err != nil {
		return err
	}

	// 定期重新加载
	go m.reloadLoop()
	return nil
}

// Stop 停止账户管理器
func (m *AccountManager) Stop() {
	close(m.done)
}

// reloadLoop 定期重新加载账户
func (m *AccountManager) reloadLoop() {
	ticker := time.NewTicker(m.reloadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.authenticator.LoadAccounts(); err != nil {
				// 记录错误但不中断循环
			}
		case <-m.done:
			return
		}
	}
}

// CreateAccount 创建账户（用于测试）
func (m *AccountManager) CreateTestAccount(systemID, password string) {
	account := &Account{
		SystemID:      systemID,
		Password:      password,
		InterfaceType: "SMPP-M",
		Protocol:      "TCPIP",
		EncodingType:  "SMPP-M",
		FlowControl:   1000,
	}
	m.authenticator.RegisterAccount(account)
}
