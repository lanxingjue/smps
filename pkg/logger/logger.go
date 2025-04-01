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

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
	FATAL
)

var (
	logInstance *Logger
	once        sync.Once
)

type Logger struct {
	*log.Logger
	level      LogLevel
	file       *os.File
	callDepth  int
	rotateSize int64
	mu         sync.Mutex
}

// Init 初始化日志系统（带文件输出）
func Init(prefix string) {
	once.Do(func() {
		// 创建日志目录
		logDir := "logs"
		if err := os.MkdirAll(logDir, 0755); err != nil {
			log.Fatalf("创建日志目录失败: %v", err)
		}

		// 生成日志文件名
		logFile := filepath.Join(logDir, fmt.Sprintf("%s.log",
			time.Now().Format("20060102-150405")))

		// 打开日志文件
		file, err := os.OpenFile(logFile,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalf("打开日志文件失败: %v", err)
		}

		// 创建多输出源
		multiWriter := io.MultiWriter(os.Stdout, file)

		logInstance = &Logger{
			Logger:     log.New(multiWriter, "", 0),
			level:      INFO,
			file:       file,
			callDepth:  2,
			rotateSize: 10 << 20, // 10MB
		}

		log.SetOutput(multiWriter) // 重定向标准库log
	})
}

// 带颜色输出的日志方法
func (l *Logger) log(level string, color string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 获取调用者信息
	_, file, line, ok := runtime.Caller(l.callDepth)
	if !ok {
		file = "???"
		line = 0
	} else {
		file = filepath.Base(file)
	}

	// 构建日志前缀
	timeStr := time.Now().Format("2006-01-02 15:04:05.000")
	prefix := fmt.Sprintf("\033[%sm%s [%s] %s:%d\033[0m",
		color,
		level,
		timeStr,
		file,
		line)

	// 格式化日志内容
	msg := fmt.Sprintln(v...)
	msg = strings.TrimSuffix(msg, "\n")

	l.Output(0, fmt.Sprintf("%s %s", prefix, msg))
}

// Info 信息级别日志（蓝色）
func Info(v ...interface{}) {
	if logInstance.level <= INFO {
		logInstance.log("INFO", "34", v...)
	}
}

// Error 错误级别日志（红色）
func Error(v ...interface{}) {
	if logInstance.level <= ERROR {
		logInstance.log("ERROR", "31", v...)
	}
}

// Debug 调试日志（绿色）
func Debug(v ...interface{}) {
	if logInstance.level <= DEBUG {
		logInstance.log("DEBUG", "32", v...)
	}
}

// Fatal 致命错误日志（品红）并退出
func Fatal(v ...interface{}) {
	logInstance.log("FATAL", "35", v...)
	os.Exit(1)
}

// 自动日志轮转检查
func (l *Logger) checkRotate() {
	if l.file == nil {
		return
	}

	// 检查文件大小
	stat, err := l.file.Stat()
	if err != nil {
		return
	}

	if stat.Size() > l.rotateSize {
		l.mu.Lock()
		defer l.mu.Unlock()

		// 关闭旧文件
		l.file.Close()

		// 创建新日志文件
		newFile := filepath.Join("logs",
			fmt.Sprintf("%s.log", time.Now().Format("20060102-150405")))

		file, err := os.OpenFile(newFile,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Printf("日志轮转失败: %v", err)
			return
		}

		// 更新输出目标
		l.file = file
		l.SetOutput(io.MultiWriter(os.Stdout, file))
	}
}
