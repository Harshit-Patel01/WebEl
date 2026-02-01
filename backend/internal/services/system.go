package services

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"go.uber.org/zap"
)

type SystemStats struct {
	CPU      float64 `json:"cpu"`
	RAM      float64 `json:"ram"`
	RAMUsed  string  `json:"ram_used"`
	RAMTotal string  `json:"ram_total"`
	Temp     float64 `json:"temp"`
	Disk     string  `json:"disk"`
	DiskUsed string  `json:"disk_used"`
	Uptime   string  `json:"uptime"`
}

type SystemInfo struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	Model    string `json:"model"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	Arch     string `json:"arch"`
}

type ServiceStatus struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // active, inactive, failed, unknown
	Description string `json:"description,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

type SystemService struct {
	runner *exec.Runner
	logger *zap.Logger
}

func NewSystemService(runner *exec.Runner, logger *zap.Logger) *SystemService {
	return &SystemService{runner: runner, logger: logger}
}

func (s *SystemService) GetStats() (*SystemStats, error) {
	stats := &SystemStats{}

	if runtime.GOOS != "linux" {
		stats.Uptime = "N/A (not Linux)"
		return stats, nil
	}

	// CPU usage
	stats.CPU = readCPU()

	// RAM
	memTotal, memAvailable := readMem()
	if memTotal > 0 {
		used := memTotal - memAvailable
		stats.RAM = (float64(used) / float64(memTotal)) * 100
		stats.RAMUsed = formatBytes(used * 1024)
		stats.RAMTotal = formatBytes(memTotal * 1024)
	}

	// Temperature
	if data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			stats.Temp = v / 1000
		}
	}

	// Disk
	stats.Disk, stats.DiskUsed = readDisk(s.runner)

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			secs, _ := strconv.ParseFloat(fields[0], 64)
			d := time.Duration(secs) * time.Second
			stats.Uptime = fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
		}
	}

	return stats, nil
}

func (s *SystemService) GetInfo() (*SystemInfo, error) {
	info := &SystemInfo{
		Arch: runtime.GOARCH,
	}

	// Hostname
	if data, err := os.ReadFile("/etc/hostname"); err == nil {
		info.Hostname = strings.TrimSpace(string(data))
	} else {
		info.Hostname, _ = os.Hostname()
	}

	// IP
	result, err := s.runner.Run(context.Background(), exec.RunOpts{
		JobType: "system_ip",
		Command: "/usr/bin/hostname",
		Args:    []string{"-I"},
		Timeout: 5 * time.Second,
	})
	if err == nil {
		for _, line := range result.Lines {
			if line.Stream == "stdout" {
				fields := strings.Fields(line.Text)
				if len(fields) > 0 {
					info.IP = fields[0]
				}
			}
		}
	}

	// Model
	if data, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		info.Model = strings.TrimRight(string(data), "\x00\n")
	}

	// OS
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				info.OS = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}

	// Kernel
	result, err = s.runner.Run(context.Background(), exec.RunOpts{
		JobType: "system_kernel",
		Command: "/usr/bin/uname",
		Args:    []string{"-r"},
		Timeout: 5 * time.Second,
	})
	if err == nil {
		for _, line := range result.Lines {
			if line.Stream == "stdout" {
				info.Kernel = strings.TrimSpace(line.Text)
			}
		}
	}

	return info, nil
}

func (s *SystemService) GetServiceStatus(name string) (*ServiceStatus, error) {
	result, err := s.runner.Run(context.Background(), exec.RunOpts{
		JobType: "service_status",
		Command: "/usr/bin/systemctl",
		Args:    []string{"is-active", name},
		Timeout: 5 * time.Second,
	})

	status := "unknown"
	if err == nil {
		for _, line := range result.Lines {
			if line.Stream == "stdout" {
				status = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	svcStatus := &ServiceStatus{Name: name, Status: status}

	// Get uptime info if active
	if status == "active" {
		uptimeResult, _ := s.runner.Run(context.Background(), exec.RunOpts{
			JobType: "service_uptime",
			Command: "/usr/bin/systemctl",
			Args:    []string{"show", name, "--property=ActiveEnterTimestamp", "--no-pager"},
			Timeout: 5 * time.Second,
		})
		if uptimeResult != nil {
			for _, line := range uptimeResult.Lines {
				if line.Stream == "stdout" && strings.HasPrefix(line.Text, "ActiveEnterTimestamp=") {
					ts := strings.TrimPrefix(line.Text, "ActiveEnterTimestamp=")
					if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", strings.TrimSpace(ts)); err == nil {
						d := time.Since(t)
						if d.Hours() >= 24 {
							svcStatus.Uptime = fmt.Sprintf("%dd %dh", int(d.Hours()/24), int(d.Hours())%24)
						} else if d.Hours() >= 1 {
							svcStatus.Uptime = fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
						} else {
							svcStatus.Uptime = fmt.Sprintf("%dm", int(d.Minutes()))
						}
					}
				}
			}
		}
	}

	return svcStatus, nil
}

func (s *SystemService) GetAllServiceStatuses() ([]ServiceStatus, error) {
	services := []string{"nginx", "cloudflared"}

	// Also check for opendeploy app services
	entries, _ := os.ReadDir("/etc/systemd/system")
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "opendeploy-app-") && strings.HasSuffix(e.Name(), ".service") {
			name := strings.TrimSuffix(e.Name(), ".service")
			services = append(services, name)
		}
	}

	var statuses []ServiceStatus
	for _, svc := range services {
		st, _ := s.GetServiceStatus(svc)
		if st != nil {
			statuses = append(statuses, *st)
		}
	}
	return statuses, nil
}

func (s *SystemService) RestartService(ctx context.Context, name string, jobID string) (*exec.ExecResult, error) {
	result, err := s.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID,
		JobType: "service_restart",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "restart", name},
		Timeout: 30 * time.Second,
	})
	return result, err
}

func (s *SystemService) StartService(ctx context.Context, name string) error {
	_, err := s.runner.Run(ctx, exec.RunOpts{
		JobType: "service_start",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "start", name},
		Timeout: 15 * time.Second,
	})
	return err
}

func (s *SystemService) StopService(ctx context.Context, name string) error {
	_, err := s.runner.Run(ctx, exec.RunOpts{
		JobType: "service_stop",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "stop", name},
		Timeout: 15 * time.Second,
	})
	return err
}

func (s *SystemService) GetJournalLogs(service string, lines int) ([]LogEntry, error) {
	if lines <= 0 {
		lines = 100
	}

	result, err := s.runner.Run(context.Background(), exec.RunOpts{
		JobType: "journal_logs",
		Command: "/usr/bin/journalctl",
		Args:    []string{"-u", service, "-n", strconv.Itoa(lines), "--no-pager", "--output=short-iso"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	var entries []LogEntry
	for _, line := range result.Lines {
		if line.Stream != "stdout" || line.Text == "" || strings.HasPrefix(line.Text, "--") {
			continue
		}
		entries = append(entries, LogEntry{
			Timestamp: line.Timestamp.Format(time.RFC3339),
			Level:     string(line.Level),
			Message:   line.Text,
		})
	}
	return entries, nil
}

// helpers

func readCPU() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 5 {
		return 0
	}
	var vals [4]float64
	for i := 1; i < 5; i++ {
		v, _ := strconv.ParseFloat(fields[i], 64)
		vals[i-1] = v
	}
	total := vals[0] + vals[1] + vals[2] + vals[3]
	if total == 0 {
		return 0
	}
	return ((total - vals[3]) / total) * 100
}

func readMem() (total, available int64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			available = val
		}
	}
	return
}

func readDisk(runner *exec.Runner) (total, used string) {
	result, err := runner.Run(context.Background(), exec.RunOpts{
		JobType: "disk_usage",
		Command: "/usr/bin/df",
		Args:    []string{"-h", "/"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return "unknown", "unknown"
	}
	for _, line := range result.Lines {
		if line.Stream == "stdout" && strings.HasPrefix(line.Text, "/") {
			fields := strings.Fields(line.Text)
			if len(fields) >= 4 {
				return fields[1], fields[2]
			}
		}
	}
	return "unknown", "unknown"
}

func formatBytes(kb int64) string {
	if kb >= 1048576 {
		return fmt.Sprintf("%.1fGB", float64(kb)/1048576)
	}
	return fmt.Sprintf("%.0fMB", float64(kb)/1024)
}
