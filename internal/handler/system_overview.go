package handler

import (
	"bufio"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"draarl/internal/buildinfo"
	"github.com/gin-gonic/gin"
)

var (
	systemOverviewStartedAt = time.Now()
	cpuSampleMu             sync.Mutex
	previousCPUSample       cpuSample
)

type cpuSample struct {
	total uint64
	idle  uint64
}

type systemOverviewResponse struct {
	Hostname      string        `json:"hostname"`
	OS            string        `json:"os"`
	Architecture  string        `json:"architecture"`
	CPUCores      int           `json:"cpu_cores"`
	CPUUsage      *float64      `json:"cpu_usage_percent,omitempty"`
	LoadAverage   []float64     `json:"load_average,omitempty"`
	Goroutines    int           `json:"goroutines"`
	GoVersion     string        `json:"go_version"`
	ServerVersion string        `json:"server_version"`
	BuildTime     string        `json:"build_time"`
	UptimeSeconds int64         `json:"uptime_seconds"`
	Memory        systemMemory  `json:"memory"`
	Disk          systemDisk    `json:"disk"`
	Process       systemProcess `json:"process"`
	SampledAt     time.Time     `json:"sampled_at"`
}

type systemMemory struct {
	Available      bool    `json:"available"`
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

type systemDisk struct {
	Available   bool    `json:"available"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type systemProcess struct {
	AllocBytes     uint64 `json:"alloc_bytes"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes uint64 `json:"heap_inuse_bytes"`
	SysBytes       uint64 `json:"sys_bytes"`
	NumGC          uint32 `json:"num_gc"`
}

// GetSystemOverview returns deliberately coarse host/process telemetry for the
// administrator dashboard. It never reads configuration values or command-line
// arguments, and keeps filesystem details out of the response.
func GetSystemOverview(c *gin.Context) {
	hostname, _ := os.Hostname()
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	response := systemOverviewResponse{
		Hostname:      strings.TrimSpace(hostname),
		OS:            runtime.GOOS,
		Architecture:  runtime.GOARCH,
		CPUCores:      runtime.NumCPU(),
		CPUUsage:      readCPUUsagePercent(),
		LoadAverage:   readLoadAverage(),
		Goroutines:    runtime.NumGoroutine(),
		GoVersion:     runtime.Version(),
		ServerVersion: buildinfo.VersionString(),
		BuildTime:     buildinfo.BuildTimeString(),
		UptimeSeconds: int64(time.Since(systemOverviewStartedAt).Seconds()),
		Memory:        readSystemMemory(),
		Disk:          readSystemDisk(),
		Process: systemProcess{
			AllocBytes: memStats.Alloc, HeapAllocBytes: memStats.HeapAlloc,
			HeapInuseBytes: memStats.HeapInuse, SysBytes: memStats.Sys, NumGC: memStats.NumGC,
		},
		SampledAt: time.Now().UTC(),
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "success", "data": response})
}

func readCPUUsagePercent() *float64 {
	total, idle, ok := readCPUJiffies()
	if !ok {
		return nil
	}
	cpuSampleMu.Lock()
	defer cpuSampleMu.Unlock()
	current := cpuSample{total: total, idle: idle}
	previous := previousCPUSample
	previousCPUSample = current
	if previous.total == 0 || total <= previous.total || idle < previous.idle {
		return nil
	}
	totalDelta := total - previous.total
	idleDelta := idle - previous.idle
	if idleDelta > totalDelta {
		idleDelta = totalDelta
	}
	usage := (1 - float64(idleDelta)/float64(totalDelta)) * 100
	if usage < 0 {
		usage = 0
	} else if usage > 100 {
		usage = 100
	}
	return &usage
}

func readCPUJiffies() (total, idle uint64, ok bool) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && len(line) == 0 {
		return 0, 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var values []uint64
	for _, field := range fields[1:] {
		value, parseErr := strconv.ParseUint(field, 10, 64)
		if parseErr != nil {
			return 0, 0, false
		}
		values = append(values, value)
		total += value
	}
	// Linux reports idle and iowait after user/system/nice. Treat iowait as idle.
	idle = values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return total, idle, true
}

func readLoadAverage() []float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil
	}
	load := make([]float64, 0, 3)
	for _, field := range fields[:3] {
		value, parseErr := strconv.ParseFloat(field, 64)
		if parseErr != nil {
			return nil
		}
		load = append(load, value)
	}
	return load
}

func readSystemMemory() systemMemory {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return systemMemory{}
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			continue
		}
		// /proc/meminfo values are KiB unless explicitly stated otherwise.
		values[strings.TrimSuffix(fields[0], ":")] = value * 1024
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if total == 0 {
		return systemMemory{}
	}
	if available > total {
		available = total
	}
	used := total - available
	return systemMemory{Available: true, TotalBytes: total, UsedBytes: used, AvailableBytes: available, UsedPercent: float64(used) * 100 / float64(total)}
}
