// pkg/logger/logger.go
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LogLevel 日志级别类型
type LogLevel int

// 日志级别常量
const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarningLevel
	ErrorLevel
	CriticalLevel
	FatalLevel
)

// 日志级别名称映射
var levelNames = map[LogLevel]string{
	DebugLevel:    "DEBUG",
	InfoLevel:     "INFO",
	WarningLevel:  "WARNING",
	ErrorLevel:    "ERROR",
	CriticalLevel: "CRITICAL",
	FatalLevel:    "FATAL",
}

// LogConfig 日志配置
type LogConfig struct {
	// 日志级别
	Level LogLevel

	// 日志格式
	Format string

	// 日志输出位置: "console", "file", "both"
	Output string

	// 日志文件路径 (当Output为"file"或"both"时)
	FilePath string

	// 日志文件大小限制（MB）
	MaxSize int

	// 最大文件数（用于轮转）
	MaxFiles int

	// 是否启用调用位置信息
	EnableCaller bool

	// 是否打印时间戳
	EnableTimestamp bool

	// 是否启用颜色输出 (console)
	EnableColor bool
}

// Logger 自定义日志器结构体
type Logger struct {
	name      string
	level     LogLevel
	formatter string
	out       io.Writer
	file      *os.File
	logger    *log.Logger
	mu        sync.Mutex
	config    LogConfig
}

// 默认日志格式
var defaultFormat = "[%{time}] [%{level}] [%{module}] %{file}:%{line} %{message}"

// 默认logger配置
var defaultConfig = LogConfig{
	Level:           InfoLevel,
	Format:          defaultFormat,
	Output:          "console",
	EnableCaller:    true,
	EnableTimestamp: true,
	EnableColor:     true,
	MaxSize:         100, // MB
	MaxFiles:        5,
}

// New 创建一个新的日志器实例
func New(name string, level LogLevel, out io.Writer) (*Logger, error) {
	if out == nil {
		out = os.Stderr
	}

	logger := &Logger{
		name:      name,
		level:     level,
		formatter: defaultFormat,
		out:       out,
		logger:    log.New(out, "", 0),
		config:    defaultConfig,
	}

	return logger, nil
}

// NewFileLogger 创建一个文件日志器
func NewFileLogger(name string, level LogLevel, filePath string) (*Logger, error) {
	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("无法创建日志目录: %v", err)
	}

	// 打开或创建日志文件
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("无法打开日志文件: %v", err)
	}

	logger := &Logger{
		name:      name,
		level:     level,
		formatter: defaultFormat,
		out:       file,
		file:      file,
		logger:    log.New(file, "", 0),
		config: LogConfig{
			Level:           level,
			Format:          defaultFormat,
			Output:          "file",
			FilePath:        filePath,
			EnableCaller:    true,
			EnableTimestamp: true,
			EnableColor:     false,
			MaxSize:         100,
			MaxFiles:        5,
		},
	}

	return logger, nil
}

// SetLogLevel 设置日志级别
func (l *Logger) SetLogLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
	l.config.Level = level
}

// SetFormat 设置日志格式
func (l *Logger) SetFormat(format string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.formatter = format
	l.config.Format = format
}

// Close 关闭日志器
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// 不同级别的ANSI颜色
var levelColors = map[LogLevel]string{
	DebugLevel:    "\033[36m", // 青色
	InfoLevel:     "\033[32m", // 绿色
	WarningLevel:  "\033[33m", // 黄色
	ErrorLevel:    "\033[31m", // 红色
	CriticalLevel: "\033[35m", // 紫色
	FatalLevel:    "\033[41m", // 红底
}

// 重置ANSI颜色
const colorReset = "\033[0m"

// 格式化并记录日志
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	// 获取调用者信息
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		file = "???"
		line = 0
	}

	// 提取文件名
	fileParts := strings.Split(file, "/")
	file = fileParts[len(fileParts)-1]

	// 格式化消息
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	// 应用格式化模板
	output := l.formatter
	if l.config.EnableTimestamp {
		output = strings.Replace(output, "%{time}", time.Now().Format("2006-01-02 15:04:05.000"), -1)
	} else {
		output = strings.Replace(output, "%{time}", "", -1)
	}

	levelName := levelNames[level]
	output = strings.Replace(output, "%{level}", levelName, -1)
	output = strings.Replace(output, "%{module}", l.name, -1)

	if l.config.EnableCaller {
		output = strings.Replace(output, "%{file}", file, -1)
		output = strings.Replace(output, "%{line}", fmt.Sprintf("%d", line), -1)
	} else {
		output = strings.Replace(output, "%{file}:", "", -1)
		output = strings.Replace(output, "%{line}", "", -1)
	}

	output = strings.Replace(output, "%{message}", message, -1)

	// 添加颜色（如果启用）
	if l.config.EnableColor && l.config.Output != "file" {
		color, exists := levelColors[level]
		if exists {
			output = color + output + colorReset
		}
	}

	// 写入日志
	l.logger.Output(0, output)

	// 如果是fatal级别，程序终止
	if level == FatalLevel {
		os.Exit(1)
	}
}

// Debug 记录调试级别日志
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DebugLevel, format, args...)
}

// Info 记录信息级别日志
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(InfoLevel, format, args...)
}

// Warning 记录警告级别日志
func (l *Logger) Warning(format string, args ...interface{}) {
	l.log(WarningLevel, format, args...)
}

// Warn 警告级别别名
func (l *Logger) Warn(format string, args ...interface{}) {
	l.Warning(format, args...)
}

// Error 记录错误级别日志
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ErrorLevel, format, args...)
}

// Critical 记录严重错误级别日志
func (l *Logger) Critical(format string, args ...interface{}) {
	l.log(CriticalLevel, format, args...)
}

// Fatal 记录致命错误并终止程序
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(FatalLevel, format, args...)
	// 注意: 程序将在log方法中终止
}

// 全局默认logger实例
var (
	defaultLogger *Logger
	loggerMu      sync.Mutex
	initialized   bool
)

// Init 初始化日志系统
func Init(name string) {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	if initialized {
		return
	}

	// 创建默认logger
	var err error
	defaultLogger, err = New(name, InfoLevel, os.Stderr)
	if err != nil {
		panic(fmt.Sprintf("初始化日志系统失败: %v", err))
	}

	initialized = true
}

// InitWithConfig 使用配置初始化日志系统
func InitWithConfig(name string, config LogConfig) error {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	if initialized {
		// 如果已初始化，更新配置
		if defaultLogger != nil {
			defaultLogger.config = config
			defaultLogger.level = config.Level
			defaultLogger.formatter = config.Format

			// 处理输出
			switch config.Output {
			case "file":
				if config.FilePath != "" {
					file, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
					if err != nil {
						return fmt.Errorf("无法打开日志文件: %v", err)
					}

					// 关闭之前的文件（如果有）
					if defaultLogger.file != nil {
						defaultLogger.file.Close()
					}

					defaultLogger.out = file
					defaultLogger.file = file
					defaultLogger.logger = log.New(file, "", 0)
				}
			case "console":
				// 关闭之前的文件（如果有）
				if defaultLogger.file != nil {
					defaultLogger.file.Close()
					defaultLogger.file = nil
				}
				defaultLogger.out = os.Stderr
				defaultLogger.logger = log.New(os.Stderr, "", 0)
			case "both":
				if config.FilePath != "" {
					file, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
					if err != nil {
						return fmt.Errorf("无法打开日志文件: %v", err)
					}

					// 关闭之前的文件（如果有）
					if defaultLogger.file != nil {
						defaultLogger.file.Close()
					}

					// 创建多输出
					multiWriter := io.MultiWriter(os.Stderr, file)
					defaultLogger.out = multiWriter
					defaultLogger.file = file
					defaultLogger.logger = log.New(multiWriter, "", 0)
				}
			}
		}
		return nil
	}

	// 创建新的logger
	var logger *Logger
	var err error

	switch config.Output {
	case "file":
		if config.FilePath == "" {
			return fmt.Errorf("日志文件路径未指定")
		}
		logger, err = NewFileLogger(name, config.Level, config.FilePath)
	case "both":
		if config.FilePath == "" {
			return fmt.Errorf("日志文件路径未指定")
		}
		// 创建文件logger
		logger, err = NewFileLogger(name, config.Level, config.FilePath)
		if err != nil {
			return err
		}
		// 设置为多输出
		multiWriter := io.MultiWriter(os.Stderr, logger.file)
		logger.out = multiWriter
		logger.logger = log.New(multiWriter, "", 0)
	default: // "console" 或其他默认
		logger, err = New(name, config.Level, os.Stderr)
	}

	if err != nil {
		return fmt.Errorf("初始化日志系统失败: %v", err)
	}

	logger.config = config
	defaultLogger = logger
	initialized = true

	return nil
}

// GetLogger 获取默认日志器
func GetLogger() *Logger {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	if !initialized {
		Init("default")
	}

	return defaultLogger
}

// SetLevel 设置全局日志级别
func SetLevel(level LogLevel) {
	logger := GetLogger()
	logger.SetLogLevel(level)
}

// 以下是全局函数，使用默认日志器

// Debug 全局调试日志
func Debug(format string, args ...interface{}) {
	logger := GetLogger()
	logger.Debug(format, args...)
}

// Info 全局信息日志
func Info(format string, args ...interface{}) {
	logger := GetLogger()
	logger.Info(format, args...)
}

// Warning 全局警告日志
func Warning(format string, args ...interface{}) {
	logger := GetLogger()
	logger.Warning(format, args...)
}

// Warn 全局警告日志别名
func Warn(format string, args ...interface{}) {
	logger := GetLogger()
	logger.Warning(format, args...)
}

// Error 全局错误日志
func Error(format string, args ...interface{}) {
	logger := GetLogger()
	logger.Error(format, args...)
}

// Critical 全局严重错误日志
func Critical(format string, args ...interface{}) {
	logger := GetLogger()
	logger.Critical(format, args...)
}

// Fatal 全局致命错误日志
func Fatal(format string, args ...interface{}) {
	logger := GetLogger()
	logger.Fatal(format, args...)
	// 注意: 程序将终止
}
