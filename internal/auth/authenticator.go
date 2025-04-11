// internal/auth/authenticator.go  认证器
package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"smps/pkg/logger"
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

// Range iterates over all accounts and applies the given function.
func (a *Authenticator) Range(f func(systemID string, account *Account) bool) {
	for systemID, account := range a.accounts {
		if !f(systemID, account) {
			break
		}
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

// LoadAccountsFromDB 从数据库加载账户
func (a *Authenticator) LoadAccountsFromDB() error {
	if a.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 查询外部网元配置
	rows, err := a.db.Query(`
		SELECT system_id, password, interface_type, protocol, encoding_type, flow_control, is_active
		FROM external_smmc
		WHERE is_active = 1
	`)
	if err != nil {
		return fmt.Errorf("查询外部网元配置失败: %v", err)
	}
	defer rows.Close()

	// 临时保存加载的账户
	accounts := make(map[string]*Account)

	// 遍历结果集
	for rows.Next() {
		var acc Account
		var isActive bool

		if err := rows.Scan(
			&acc.SystemID,
			&acc.Password,
			&acc.InterfaceType,
			&acc.Protocol,
			&acc.EncodingType,
			&acc.FlowControl,
			&isActive,
		); err != nil {
			return fmt.Errorf("扫描账户数据失败: %v", err)
		}

		if isActive {
			accounts[acc.SystemID] = &acc
		}
	}

	// 加载账户IP限制
	for _, acc := range accounts {
		ipRows, err := a.db.Query(`
			SELECT ip_address
			FROM account_ips
			WHERE system_id = ?
		`, acc.SystemID)
		if err != nil {
			return fmt.Errorf("查询账户IP限制失败: %v", err)
		}

		var ips []string
		for ipRows.Next() {
			var ip string
			if err := ipRows.Scan(&ip); err != nil {
				ipRows.Close()
				return fmt.Errorf("扫描IP数据失败: %v", err)
			}
			ips = append(ips, ip)
		}
		ipRows.Close()

		acc.IPAddresses = ips
	}

	// 更新账户映射
	a.mu.Lock()
	a.accounts = accounts
	a.mu.Unlock()

	logger.Info(fmt.Sprintf("从数据库加载了%d个账户配置", len(accounts)))
	return nil
}

// SaveAccount 保存账户到数据库
func (a *Authenticator) SaveAccount(account *Account) error {
	if a.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 开始事务
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务失败: %v", err)
	}

	// 检查账户是否已存在
	var count int
	err = tx.QueryRow("SELECT COUNT(*) FROM external_smmc WHERE system_id = ?", account.SystemID).Scan(&count)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("检查账户是否存在失败: %v", err)
	}

	if count > 0 {
		// 更新现有账户
		_, err = tx.Exec(`
			UPDATE external_smmc
			SET password = ?, interface_type = ?, protocol = ?, encoding_type = ?, flow_control = ?
			WHERE system_id = ?
		`,
			account.Password,
			account.InterfaceType,
			account.Protocol,
			account.EncodingType,
			account.FlowControl,
			account.SystemID,
		)
	} else {
		// 插入新账户
		_, err = tx.Exec(`
			INSERT INTO external_smmc
			(system_id, password, interface_type, protocol, encoding_type, flow_control, is_active)
			VALUES (?, ?, ?, ?, ?, ?, 1)
		`,
			account.SystemID,
			account.Password,
			account.InterfaceType,
			account.Protocol,
			account.EncodingType,
			account.FlowControl,
		)
	}

	if err != nil {
		tx.Rollback()
		return fmt.Errorf("保存账户失败: %v", err)
	}

	// 删除现有IP绑定
	_, err = tx.Exec("DELETE FROM account_ips WHERE system_id = ?", account.SystemID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("删除现有IP绑定失败: %v", err)
	}

	// 插入新的IP绑定
	for _, ip := range account.IPAddresses {
		_, err = tx.Exec("INSERT INTO account_ips (system_id, ip_address) VALUES (?, ?)",
			account.SystemID, ip)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("插入IP绑定失败: %v", err)
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %v", err)
	}

	// 更新内存中的账户
	a.RegisterAccount(account)

	return nil
}

// DeleteAccount 从数据库删除账户
func (a *Authenticator) DeleteAccount(systemID string) error {
	if a.db == nil {
		return errors.New("数据库连接未初始化")
	}

	// 删除账户
	_, err := a.db.Exec("DELETE FROM external_smmc WHERE system_id = ?", systemID)
	if err != nil {
		return fmt.Errorf("删除账户失败: %v", err)
	}

	// 从内存中移除
	a.mu.Lock()
	delete(a.accounts, systemID)
	a.mu.Unlock()

	return nil
}
