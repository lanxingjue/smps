package config

import (
	"fmt"
	"io/ioutil"
	"time"

	"gopkg.in/yaml.v2"

	"smps/internal/client"
	"smps/internal/database"
	"smps/internal/ha"
	"smps/internal/processor"
	"smps/internal/server"
)

// Config 系统配置
type Config struct {
	Version     string                    `yaml:"version"`
	Log         LogConfig                 `yaml:"log"`
	Database    *database.Config          `yaml:"database"`
	Auth        AuthConfig                `yaml:"auth"`
	SMSC        *client.Config            `yaml:"smsc"`
	SMMC        *server.ServerConfig      `yaml:"smmc"`
	Dispatcher  DispatcherConfig          `yaml:"dispatcher"`
	Processor   *processor.ResponseConfig `yaml:"processor"`
	Web         WebConfig                 `yaml:"web"`
	HA          *ha.Config                `yaml:"ha"`
	Performance PerformanceConfig         `yaml:"performance"`
}

// LogConfig 日志配置
type LogConfig struct {
	LogFile string `yaml:"log_file"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	IPWhitelist bool `yaml:"ip_whitelist"`
}

// DispatcherConfig 消息分发器配置
type DispatcherConfig struct {
	QueueSize int           `yaml:"queue_size"`
	Workers   int           `yaml:"workers"`
	Timeout   time.Duration `yaml:"timeout"`
}

// WebConfig Web管理界面配置
type WebConfig struct {
	Enabled      bool          `yaml:"enabled"`
	ListenAddr   string        `yaml:"listen_addr"`
	StaticDir    string        `yaml:"static_dir"`
	TemplatesDir string        `yaml:"templates_dir"`
	SessionTTL   time.Duration `yaml:"session_ttl"`
	Debug        bool          `yaml:"debug"`
}

// PerformanceConfig 性能监控配置
type PerformanceConfig struct {
	Enabled       bool          `yaml:"enabled"`
	CPUThreshold  float64       `yaml:"cpu_threshold"`
	MemThreshold  float64       `yaml:"mem_threshold"`
	CheckInterval time.Duration `yaml:"check_interval"`
	OutputDir     string        `yaml:"output_dir"`
	ProfilerHTTP  bool          `yaml:"profiler_http"`
	HTTPAddr      string        `yaml:"http_addr"`
}

// LoadConfig 加载配置文件
func LoadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 将时间单位从秒转换为time.Duration
	config.SMSC.EnquireInterval = time.Duration(config.SMSC.EnquireInterval) * time.Second
	config.SMSC.ResponseTimeout = time.Duration(config.SMSC.ResponseTimeout) * time.Second
	config.SMSC.ReconnectInterval = time.Duration(config.SMSC.ReconnectInterval) * time.Second
	config.SMMC.ReadTimeout = time.Duration(config.SMMC.ReadTimeout) * time.Second
	config.SMMC.WriteTimeout = time.Duration(config.SMMC.WriteTimeout) * time.Second
	config.SMMC.HeartbeatInterval = time.Duration(config.SMMC.HeartbeatInterval) * time.Second
	config.Dispatcher.Timeout = time.Duration(config.Dispatcher.Timeout) * time.Second

	// 确保配置中的时间值有效
	if config.Processor != nil {
		if config.Processor.MessageTimeout <= 0 {
			config.Processor.MessageTimeout = 2 // 设置默认值为2秒
		}

		if config.Processor.CleanupInterval <= 0 {
			config.Processor.CleanupInterval = 10 // 设置默认值为10秒
		}

		config.Processor.MessageTimeout = time.Duration(config.Processor.MessageTimeout) * time.Second
		config.Processor.CleanupInterval = time.Duration(config.Processor.CleanupInterval) * time.Second
	}

	config.Web.SessionTTL = time.Duration(config.Web.SessionTTL) * time.Second
	config.Performance.CheckInterval = time.Duration(config.Performance.CheckInterval) * time.Second

	if config.HA != nil && config.HA.Enabled {
		config.HA.HeartbeatInterval = time.Duration(config.HA.HeartbeatInterval) * time.Second
		config.HA.HeartbeatTimeout = time.Duration(config.HA.HeartbeatTimeout) * time.Second
		config.HA.ElectionTimeout = time.Duration(config.HA.ElectionTimeout) * time.Second
		config.HA.FailoverWaitTime = time.Duration(config.HA.FailoverWaitTime) * time.Second
	}

	return &config, nil
}
