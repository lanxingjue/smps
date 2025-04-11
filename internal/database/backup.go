// internal/database/backup.go
package database

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"smps/pkg/logger"
)

// BackupOptions 备份选项
type BackupOptions struct {
	BackupDir     string   // 备份目录
	Compress      bool     // 是否压缩
	IncludeData   bool     // 是否包含数据
	IncludeSchema bool     // 是否包含表结构
	Tables        []string // 要备份的表，空表示所有表
}

// DefaultBackupOptions 默认备份选项
func DefaultBackupOptions() *BackupOptions {
	return &BackupOptions{
		BackupDir:     "backups",
		Compress:      true,
		IncludeData:   true,
		IncludeSchema: true,
		Tables:        []string{},
	}
}

// Backup 执行数据库备份
func Backup(config *Config, options *BackupOptions) (string, error) {
	if options == nil {
		options = DefaultBackupOptions()
	}

	// 创建备份目录
	if err := os.MkdirAll(options.BackupDir, 0755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %v", err)
	}

	// 生成备份文件名
	timestamp := time.Now().Format("20060102_150405")
	backupFile := filepath.Join(options.BackupDir, fmt.Sprintf("smps_backup_%s.sql", timestamp))

	// 构建mysqldump命令
	args := []string{
		"-h", config.Host,
		"-P", fmt.Sprintf("%d", config.Port),
		"-u", config.Username,
	}

	if config.Password != "" {
		args = append(args, fmt.Sprintf("-p%s", config.Password))
	}

	if options.IncludeSchema && !options.IncludeData {
		args = append(args, "--no-data")
	}

	if !options.IncludeSchema && options.IncludeData {
		args = append(args, "--no-create-info")
	}

	args = append(args, config.Database)

	// 添加要备份的表
	if len(options.Tables) > 0 {
		args = append(args, options.Tables...)
	}

	// 执行mysqldump
	cmd := exec.Command("mysqldump", args...)
	outFile, err := os.Create(backupFile)
	if err != nil {
		return "", fmt.Errorf("创建备份文件失败: %v", err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("执行mysqldump失败: %v", err)
	}

	// 如果需要压缩
	if options.Compress {
		compressedFile := backupFile + ".gz"
		compressCmd := exec.Command("gzip", "-f", backupFile)
		if err := compressCmd.Run(); err != nil {
			logger.Error(fmt.Sprintf("压缩备份文件失败: %v", err))
			// 返回未压缩的文件
			return backupFile, nil
		}

		return compressedFile, nil
	}

	return backupFile, nil
}

// Restore 从备份文件恢复数据库
func Restore(config *Config, backupFile string) error {
	// 检查文件是否存在
	_, err := os.Stat(backupFile)
	if err != nil {
		return fmt.Errorf("备份文件不存在: %v", err)
	}

	var cmd *exec.Cmd

	// 处理压缩文件
	if filepath.Ext(backupFile) == ".gz" {
		cmd = exec.Command("bash", "-c",
			fmt.Sprintf("gunzip -c %s | mysql -h%s -P%d -u%s -p%s %s",
				backupFile, config.Host, config.Port, config.Username, config.Password, config.Database))
	} else {
		// 构建mysql命令
		args := []string{
			"-h", config.Host,
			"-P", fmt.Sprintf("%d", config.Port),
			"-u", config.Username,
		}

		if config.Password != "" {
			args = append(args, fmt.Sprintf("-p%s", config.Password))
		}

		args = append(args, config.Database)

		cmd = exec.Command("mysql", args...)

		inFile, err := os.Open(backupFile)
		if err != nil {
			return fmt.Errorf("打开备份文件失败: %v", err)
		}
		defer inFile.Close()

		cmd.Stdin = inFile
	}

	// 执行恢复
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("恢复数据库失败: %v", err)
	}

	logger.Info(fmt.Sprintf("数据库从%s恢复成功", backupFile))
	return nil
}
