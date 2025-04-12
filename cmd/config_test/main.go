// cmd/config_test/main.go
package main

import (
	"fmt"
	"os"

	"smps/internal/config"
)

func main() {
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("配置加载成功!\n")
	fmt.Printf("版本: %s\n", cfg.Version)
	fmt.Printf("SMMC监听地址: %s\n", cfg.SMMC.ListenAddress)
	fmt.Printf("处理器超时设置: %v\n", cfg.Processor.MessageTimeout)
}
