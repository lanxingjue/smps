// internal/database/config.go
package database

import (
	"fmt"
	"time"
)

// Config 数据库配置
type Config struct {
	Driver          string        // 数据库驱动
	Host            string        // 主机地址
	Port            int           // 端口
	Username        string        // 用户名
	Password        string        // 密码
	Database        string        // 数据库名
	Parameters      string        // 连接参数
	MaxOpenConns    int           // 最大打开连接数
	MaxIdleConns    int           // 最大空闲连接数
	ConnMaxLifetime time.Duration // 连接最大生存时间
}

// NewConfig 创建数据库配置
func NewConfig() *Config {
	return &Config{
		Driver:          "mysql",
		Host:            "localhost",
		Port:            3306,
		Username:        "root",
		Password:        "",
		Database:        "smps",
		Parameters:      "parseTime=true&charset=utf8mb4&loc=Local",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 1 * time.Hour,
	}
}

// DSN 获取数据源名称
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?%s",
		c.Username,
		c.Password,
		c.Host,
		c.Port,
		c.Database,
		c.Parameters,
	)
}
