// internal/performance/profiler.go
package performance

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"

	"smps/pkg/logger"
)

// Profiler 性能分析器
type Profiler struct {
	// 是否启用性能分析
	Enabled bool

	// CPU使用率阈值（百分比）
	CPUThreshold float64

	// 内存使用率阈值（百分比）
	MemThreshold float64

	// 检查间隔
	CheckInterval time.Duration

	// 输出目录
	OutputDir string

	// 停止通道
	stopCh chan struct{}

	// 是否正在运行
	running bool
}

// NewProfiler 创建性能分析器
func NewProfiler() *Profiler {
	return &Profiler{
		Enabled:       true,
		CPUThreshold:  80.0, // CPU使用率超过80%触发分析
		MemThreshold:  80.0, // 内存使用率超过80%触发分析
		CheckInterval: 30 * time.Second,
		OutputDir:     "profiles",
		stopCh:        make(chan struct{}),
	}
}

// Start 启动性能分析器
func (p *Profiler) Start() error {
	if !p.Enabled {
		return nil
	}

	if p.running {
		return nil
	}

	// 创建输出目录
	if err := os.MkdirAll(p.OutputDir, 0755); err != nil {
		return fmt.Errorf("创建性能分析输出目录失败: %v", err)
	}

	p.running = true

	// 启动性能监控
	go p.monitor()

	logger.Info("性能分析器已启动")
	return nil
}

// Stop 停止性能分析器
func (p *Profiler) Stop() {
	if !p.running {
		return
	}

	close(p.stopCh)
	p.running = false
	logger.Info("性能分析器已停止")
}

// monitor 监控系统性能
func (p *Profiler) monitor() {
	ticker := time.NewTicker(p.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.checkPerformance()
		case <-p.stopCh:
			return
		}
	}
}

// checkPerformance 检查系统性能
func (p *Profiler) checkPerformance() {
	// 获取CPU使用率
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		logger.Error(fmt.Sprintf("获取CPU使用率失败: %v", err))
		return
	}

	// 获取内存使用率
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		logger.Error(fmt.Sprintf("获取内存使用率失败: %v", err))
		return
	}

	cpuUsage := cpuPercent[0]
	memUsage := memInfo.UsedPercent

	// 记录系统资源使用情况
	logger.Info(fmt.Sprintf("系统资源使用: CPU=%.2f%%, 内存=%.2f%%", cpuUsage, memUsage))

	// 检查是否触发性能分析
	if cpuUsage > p.CPUThreshold {
		logger.Warning(fmt.Sprintf("CPU使用率(%.2f%%)超过阈值(%.2f%%), 触发性能分析", cpuUsage, p.CPUThreshold))
		p.captureProfile("cpu")
	}

	if memUsage > p.MemThreshold {
		logger.Warning(fmt.Sprintf("内存使用率(%.2f%%)超过阈值(%.2f%%), 触发性能分析", memUsage, p.MemThreshold))
		p.captureProfile("memory")
	}
}

// captureProfile 捕获性能分析数据
func (p *Profiler) captureProfile(profileType string) {
	timestamp := time.Now().Format("20060102_150405")

	switch profileType {
	case "cpu":
		// 捕获CPU性能分析
		filename := fmt.Sprintf("%s/cpu_profile_%s.pprof", p.OutputDir, timestamp)
		f, err := os.Create(filename)
		if err != nil {
			logger.Error(fmt.Sprintf("创建CPU性能分析文件失败: %v", err))
			return
		}
		defer f.Close()

		logger.Info(fmt.Sprintf("开始CPU性能分析，持续30秒..."))
		if err := pprof.StartCPUProfile(f); err != nil {
			logger.Error(fmt.Sprintf("启动CPU性能分析失败: %v", err))
			return
		}

		// 运行30秒
		time.Sleep(30 * time.Second)
		pprof.StopCPUProfile()
		logger.Info(fmt.Sprintf("CPU性能分析完成，文件: %s", filename))

	case "memory":
		// 捕获内存性能分析
		filename := fmt.Sprintf("%s/heap_profile_%s.pprof", p.OutputDir, timestamp)
		f, err := os.Create(filename)
		if err != nil {
			logger.Error(fmt.Sprintf("创建内存性能分析文件失败: %v", err))
			return
		}
		defer f.Close()

		// 手动触发GC
		runtime.GC()

		if err := pprof.WriteHeapProfile(f); err != nil {
			logger.Error(fmt.Sprintf("写入内存性能分析失败: %v", err))
			return
		}

		logger.Info(fmt.Sprintf("内存性能分析完成，文件: %s", filename))

	default:
		logger.Error(fmt.Sprintf("不支持的性能分析类型: %s", profileType))
	}
}

// InitProfilerServer 初始化性能分析HTTP服务器
func InitProfilerServer(addr string) {
	go func() {
		logger.Info(fmt.Sprintf("性能分析HTTP服务器启动，地址: %s", addr))
		if err := http.ListenAndServe(addr, nil); err != nil {
			logger.Error(fmt.Sprintf("性能分析HTTP服务器启动失败: %v", err))
		}
	}()
}
