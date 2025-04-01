package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lanxingjue/smps/pkg/logger"

	"gopkg.in/yaml.v2"
)

// Config 系统配置
type Config struct {
	Server struct {
		ListenAddress  string `yaml:"listen_address"`
		MaxConnections int    `yaml:"max_connections"`
	} `yaml:"server"`
	Client struct {
		SMSCAddress string `yaml:"smsc_address"`
		SystemID    string `yaml:"system_id"`
		Password    string `yaml:"password"`
	} `yaml:"client"`
}

func main() {
	// 解析命令行参数
	configFile := flag.String("config", "config/config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	logger.Init("smps")
	logger.Info("SMPS服务启动中...")
	logger.Info(fmt.Sprintf("服务器配置: 监听地址=%s, 最大连接数=%d",
		config.Server.ListenAddress,
		config.Server.MaxConnections))

	// 启动健康检查API
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("SMPS服务正常运行"))
	})

	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			logger.Error("HTTP服务启动失败:", err)
		}
	}()

	// 等待中断信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	logger.Info("接收到关闭信号，正在关闭服务...")
}

// 加载配置
func loadConfig(file string) (*Config, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var config Config
	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
