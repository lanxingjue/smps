// cmd/db_test/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smps/internal/auth"
	"smps/internal/database"
	"smps/internal/tracer"
	"smps/pkg/logger"
)

// Authenticator represents the authentication manager.
type Authenticator struct {
	accounts map[string]*auth.Account
}

// Range iterates over all accounts and applies the given function.
func (a *Authenticator) Range(f func(systemID string, account *auth.Account) bool) {
	for systemID, account := range a.accounts {
		if !f(systemID, account) {
			break
		}
	}
}

func main() {
	// 解析命令行参数
	host := flag.String("host", "localhost", "数据库主机")
	port := flag.Int("port", 3306, "数据库端口")
	user := flag.String("user", "root", "数据库用户名")
	password := flag.String("password", "", "数据库密码")
	dbName := flag.String("db", "smps", "数据库名称")
	flag.Parse()

	// 初始化日志
	logger.Init("db_test")

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建数据库配置
	dbConfig := database.NewConfig()
	dbConfig.Host = *host
	dbConfig.Port = *port
	dbConfig.Username = *user
	dbConfig.Password = *password
	dbConfig.Database = *dbName

	// 创建数据库管理器
	dbManager := database.NewManager(dbConfig)

	// 连接数据库
	if err := dbManager.Connect(); err != nil {
		logger.Fatal(fmt.Sprintf("连接数据库失败: %v", err))
	}

	// 获取数据库连接
	db := dbManager.DB()

	// 执行数据库迁移
	if err := database.Migrate(db); err != nil {
		logger.Fatal(fmt.Sprintf("数据库迁移失败: %v", err))
	}

	// 创建认证器并加载账户
	authenticator := auth.NewAuthenticator(db, false)
	if err := authenticator.LoadAccountsFromDB(); err != nil {
		logger.Error(fmt.Sprintf("从数据库加载账户失败: %v", err))
	}

	// 创建示例账户
	testAccount := &auth.Account{
		SystemID:      "test_account",
		Password:      "test_password",
		InterfaceType: "SMPP-M",
		Protocol:      "TCPIP",
		EncodingType:  "SMPP-M",
		FlowControl:   1000,
		IPAddresses:   []string{"192.168.1.100", "192.168.1.101"},
	}

	// 保存账户到数据库
	if err := authenticator.SaveAccount(testAccount); err != nil {
		logger.Error(fmt.Sprintf("保存账户失败: %v", err))
	} else {
		logger.Info("测试账户保存成功")
	}

	// 创建协议跟踪器
	config := tracer.NewTracerConfig()
	protocolTracer := tracer.NewProtocolTracer(config, db)

	// 加载跟踪配置
	if err := protocolTracer.LoadConfigFromDB(); err != nil {
		logger.Error(fmt.Sprintf("加载跟踪配置失败: %v", err))
	}

	// 加载跟踪号码
	if err := protocolTracer.LoadTracedNumbersFromDB(); err != nil {
		logger.Error(fmt.Sprintf("加载跟踪号码失败: %v", err))
	}

	// 添加测试跟踪号码
	if err := protocolTracer.SaveTracedNumber("13800138000"); err != nil {
		logger.Error(fmt.Sprintf("保存跟踪号码失败: %v", err))
	} else {
		logger.Info("测试跟踪号码保存成功")
	}

	// 创建数据库备份
	backupOptions := database.DefaultBackupOptions()
	backupFile, err := database.Backup(dbConfig, backupOptions)
	if err != nil {
		logger.Error(fmt.Sprintf("创建数据库备份失败: %v", err))
	} else {
		logger.Info(fmt.Sprintf("数据库备份成功，文件: %s", backupFile))
	}

	// 定期输出统计信息
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				accountCount := 0
				authenticator.Range(func(systemID string, account *auth.Account) bool {
					accountCount++
					return true
				})

				logger.Info(fmt.Sprintf("数据库状态: 连接正常=%v, 账户数=%d",
					dbManager.CheckConnection(), accountCount))

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

	// 关闭数据库连接
	if err := dbManager.Close(); err != nil {
		logger.Error(fmt.Sprintf("关闭数据库连接失败: %v", err))
	}

	logger.Info("数据库测试完成")
}
