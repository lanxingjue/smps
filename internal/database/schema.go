// internal/database/schema.go
package database

import (
	"database/sql"
	"fmt"

	"smps/pkg/logger"
)

// CreateTables 创建所有表
func CreateTables(db *sql.DB) error {
	// 创建外部网元配置表
	if err := createExternalSMMCTable(db); err != nil {
		return err
	}

	// 创建用户表
	if err := createUsersTable(db); err != nil {
		return err
	}

	// 创建IP白名单表
	if err := createIPWhitelistTable(db); err != nil {
		return err
	}

	// 创建跟踪号码表
	if err := createTracedNumbersTable(db); err != nil {
		return err
	}

	// 创建协议日志表
	if err := createProtocolLogsTable(db); err != nil {
		return err
	}

	// 创建系统配置表
	if err := createSystemConfigTable(db); err != nil {
		return err
	}

	// 创建账号IP表
	if err := createAccountIPsTable(db); err != nil {
		return err
	}

	return nil
}

// createExternalSMMCTable 创建外部网元配置表
func createExternalSMMCTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS external_smmc (
			id INT AUTO_INCREMENT PRIMARY KEY,
			interface_name VARCHAR(50) NOT NULL,
			system_id VARCHAR(50) NOT NULL,
			password VARCHAR(255) NOT NULL,
			ip_address VARCHAR(50) NOT NULL,
			port INT NOT NULL,
			interface_type VARCHAR(50) NOT NULL,
			protocol VARCHAR(50) NOT NULL,
			encoding_type VARCHAR(50),
			flow_control INT DEFAULT 1000,
			is_active BOOLEAN DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY (system_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("创建external_smmc表失败: %v", err)
	}

	logger.Info("external_smmc表创建或已存在")
	return nil
}

// createUsersTable 创建用户表
func createUsersTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(50) NOT NULL,
			password VARCHAR(255) NOT NULL,
			role VARCHAR(50) NOT NULL,
			email VARCHAR(100),
			full_name VARCHAR(100),
			is_active BOOLEAN DEFAULT 1,
			last_login TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY (username)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("创建users表失败: %v", err)
	}

	// 检查默认管理员用户是否存在
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin'").Scan(&count)
	if err != nil {
		return fmt.Errorf("检查管理员用户失败: %v", err)
	}

	// 如果不存在，创建默认管理员用户
	if count == 0 {
		// 在实际应用中，应使用bcrypt等方式加密密码
		_, err = db.Exec("INSERT INTO users (username, password, role) VALUES (?, ?, ?)",
			"admin", "admin123", "admin")
		if err != nil {
			return fmt.Errorf("创建默认管理员用户失败: %v", err)
		}
		logger.Info("已创建默认管理员用户: admin/admin123")
	}

	logger.Info("users表创建或已存在")
	return nil
}

// createIPWhitelistTable 创建IP白名单表
func createIPWhitelistTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS ip_whitelist (
			id INT AUTO_INCREMENT PRIMARY KEY,
			ip_address VARCHAR(50) NOT NULL,
			description VARCHAR(255),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY (ip_address)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("创建ip_whitelist表失败: %v", err)
	}

	logger.Info("ip_whitelist表创建或已存在")
	return nil
}

// createTracedNumbersTable 创建跟踪号码表
func createTracedNumbersTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS traced_numbers (
			id INT AUTO_INCREMENT PRIMARY KEY,
			number VARCHAR(50) NOT NULL,
			description VARCHAR(255),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY (number)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("创建traced_numbers表失败: %v", err)
	}

	logger.Info("traced_numbers表创建或已存在")
	return nil
}

// createProtocolLogsTable 创建协议日志表
func createProtocolLogsTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS protocol_logs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			direction VARCHAR(10) NOT NULL,
			message_id INT NOT NULL,
			command_name VARCHAR(50) NOT NULL,
			source_addr VARCHAR(50),
			dest_addr VARCHAR(50),
			content TEXT,
			raw_data BLOB,
			system_id VARCHAR(50),
			status INT,
			INDEX (timestamp),
			INDEX (source_addr),
			INDEX (dest_addr),
			INDEX (system_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("创建protocol_logs表失败: %v", err)
	}

	logger.Info("protocol_logs表创建或已存在")
	return nil
}

// createSystemConfigTable 创建系统配置表
func createSystemConfigTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS system_config (
			id INT AUTO_INCREMENT PRIMARY KEY,
			config_key VARCHAR(100) NOT NULL,
			config_value TEXT,
			description VARCHAR(255),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY (config_key)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("创建system_config表失败: %v", err)
	}

	// 初始化默认配置
	defaults := map[string]string{
		"trace_enabled":      "true",
		"parse_content":      "true",
		"heartbeat_interval": "60",
		"response_timeout":   "2000",
		"flow_control":       "1000",
	}

	// 插入默认配置（如果不存在）
	for key, value := range defaults {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM system_config WHERE config_key = ?", key).Scan(&count)
		if err != nil {
			return fmt.Errorf("检查配置键%s失败: %v", key, err)
		}

		if count == 0 {
			_, err = db.Exec("INSERT INTO system_config (config_key, config_value) VALUES (?, ?)",
				key, value)
			if err != nil {
				return fmt.Errorf("插入默认配置%s失败: %v", key, err)
			}
		}
	}

	logger.Info("system_config表创建或已存在")
	return nil
}

// createAccountIPsTable 创建账号IP表
func createAccountIPsTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS account_ips (
			id INT AUTO_INCREMENT PRIMARY KEY,
			system_id VARCHAR(50) NOT NULL,
			ip_address VARCHAR(50) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY (system_id, ip_address),
			FOREIGN KEY (system_id) REFERENCES external_smmc(system_id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("创建account_ips表失败: %v", err)
	}

	logger.Info("account_ips表创建或已存在")
	return nil
}
