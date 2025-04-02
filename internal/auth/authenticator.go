// internal/auth/authenticator.go  认证器
package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sync"
)

// 账户类型
type Account struct {
	SystemID      string
	Password      string
	InterfaceType string
	Protocol      string
	EncodingType  string
	FlowControl   int
	IPAddresses   []string
}

// Authenticator 认证器
type Authenticator struct {
	db             *sql.DB
	accounts       map[string]*Account
	useIPWhitelist bool
	ipWhitelist    *IPWhitelist
	mu             sync.RWMutex
}

// NewAuthenticator 创建新的认证器
func NewAuthenticator(db *sql.DB, useIPWhitelist bool) *Authenticator {
	return &Authenticator{
		db:             db,
		accounts:       make(map[string]*Account),
		useIPWhitelist: useIPWhitelist,
		ipWhitelist:    NewIPWhitelist(),
	}
}

// RegisterAccount 注册账户
func (a *Authenticator) RegisterAccount(account *Account) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.accounts[account.SystemID] = account
}

// LoadAccounts 从数据库加载账户
func (a *Authenticator) LoadAccounts() error {
	// 如果没有连接数据库，使用内存中的账户
	if a.db == nil {
		return nil
	}

	// 从数据库加载账户
	rows, err := a.db.Query("SELECT system_id, password, interface_type, protocol, encoding_type, flow_control FROM accounts WHERE is_active = 1")
	if err != nil {
		return err
	}
	defer rows.Close()

	accounts := make(map[string]*Account)
	for rows.Next() {
		var acc Account
		if err := rows.Scan(&acc.SystemID, &acc.Password, &acc.InterfaceType, &acc.Protocol, &acc.EncodingType, &acc.FlowControl); err != nil {
			return err
		}
		accounts[acc.SystemID] = &acc

		// 加载账户的IP地址
		if err := a.loadAccountIPs(&acc); err != nil {
			return err
		}
	}

	// 原子替换账户映射
	a.mu.Lock()
	a.accounts = accounts
	a.mu.Unlock()

	return nil
}

// loadAccountIPs 加载账户的IP地址
func (a *Authenticator) loadAccountIPs(account *Account) error {
	if a.db == nil {
		return nil
	}

	rows, err := a.db.Query("SELECT ip_address FROM account_ips WHERE system_id = ?", account.SystemID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return err
		}
		ips = append(ips, ip)
	}

	account.IPAddresses = ips
	return nil
}

// Authenticate 验证身份
func (a *Authenticator) Authenticate(systemID, password string, clientIP net.IP, clientPort int) (bool, error) {
	a.mu.RLock()
	account, ok := a.accounts[systemID]
	a.mu.RUnlock()

	if !ok {
		return false, errors.New("未知的系统ID")
	}

	// 检查密码
	if account.Password != password {
		return false, errors.New("密码错误")
	}

	// 检查IP白名单（如果启用）
	if a.useIPWhitelist && !a.ipWhitelist.Check(clientIP) {
		return false, fmt.Errorf("IP %s 不在白名单中", clientIP)
	}

	// 检查账户IP限制
	if len(account.IPAddresses) > 0 {
		allowed := false
		for _, ip := range account.IPAddresses {
			if ip == clientIP.String() {
				allowed = true
				break
			}
		}
		if !allowed {
			return false, fmt.Errorf("账户 %s 不允许从IP %s 连接", systemID, clientIP)
		}
	}

	return true, nil
}

// GetAccount 获取账户信息
func (a *Authenticator) GetAccount(systemID string) (*Account, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	account, ok := a.accounts[systemID]
	return account, ok
}

// GetIPWhitelist 获取IP白名单
func (a *Authenticator) GetIPWhitelist() *IPWhitelist {
	return a.ipWhitelist
}
