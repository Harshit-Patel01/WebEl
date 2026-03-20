package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

type PerformanceOptimizer struct {
	runner *exec.Runner
	db     *state.DB
	logger *zap.Logger
	mu     sync.Mutex
	stats  *BuildStats
}

type BuildStats struct {
	TotalBuilds   int     `json:"total_builds"`
	AverageTime   float64 `json:"average_time"`
	SlowestBuild  float64 `json:"slowest_build"`
	FastestBuild  float64 `json:"fastest_build"`
	LastOptimized string  `json:"last_optimized"`
	Optimizations int     `json:"optimizations"`
}

func NewPerformanceOptimizer(runner *exec.Runner, db *state.DB, logger *zap.Logger) *PerformanceOptimizer {
	return &PerformanceOptimizer{
		runner: runner,
		db:     db,
		logger: logger,
		stats:  &BuildStats{},
	}
}

// RecordBuildStats records build performance metrics
func (p *PerformanceOptimizer) RecordBuildStats(buildDuration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stats.TotalBuilds++
	durationSeconds := buildDuration.Seconds()

	// Update statistics
	if p.stats.TotalBuilds == 1 {
		p.stats.AverageTime = durationSeconds
		p.stats.SlowestBuild = durationSeconds
		p.stats.FastestBuild = durationSeconds
	} else {
		// Moving average
		p.stats.AverageTime = (p.stats.AverageTime*float64(p.stats.TotalBuilds-1) + durationSeconds) / float64(p.stats.TotalBuilds)

		if durationSeconds > p.stats.SlowestBuild {
			p.stats.SlowestBuild = durationSeconds
		}
		if durationSeconds < p.stats.FastestBuild {
			p.stats.FastestBuild = durationSeconds
		}
	}

	p.stats.LastOptimized = time.Now().Format(time.RFC3339)

	// Auto-optimize if builds are consistently slow
	p.autoOptimize()
}

// autoOptimize applies performance optimizations based on build statistics
func (p *PerformanceOptimizer) autoOptimize() {
	// Only optimize if we have enough data and builds are consistently slow
	if p.stats.TotalBuilds < 3 || p.stats.AverageTime < 300 { // Less than 5 minutes average
		return
	}

	// Don't optimize too frequently
	if p.stats.Optimizations >= 3 && time.Since(p.parseTime(p.stats.LastOptimized)) < time.Hour {
		return
	}

	p.logger.Info("Auto-optimizing LXD and system performance",
		zap.Int("total_builds", p.stats.TotalBuilds),
		zap.Float64("avg_time", p.stats.AverageTime),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Clean up LXD cache and system
	p.cleanupLXD(ctx)

	// 2. Optimize system settings
	p.optimizeSystem(ctx)

	// 3. Ensure LXD is properly configured
	p.ensureLXDConfiguration(ctx)

	p.stats.Optimizations++
	p.stats.LastOptimized = time.Now().Format(time.RFC3339)

	p.logger.Info("Auto-optimization completed",
		zap.Int("optimizations", p.stats.Optimizations),
	)
}

func (p *PerformanceOptimizer) cleanupLXD(ctx context.Context) {

	// List all containers to see what's running
	result, err := p.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_list_all",
		Command: "lxc",
		Args:    []string{"list", "--format", "csv", "--columns", "n,st"},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		p.logger.Warn("Failed to list LXD containers", zap.Error(err))
	}

	// For now, just log the container count for diagnostics
	activeContainers := 0
	if result != nil && result.Success {
		for _, line := range result.Lines {
			if line.Stream == "stdout" && strings.TrimSpace(line.Text) != "" {
				activeContainers++
			}
		}
	}
	p.logger.Info("LXD cleanup diagnostics",
		zap.Int("active_containers", activeContainers),
	)
}

func (p *PerformanceOptimizer) optimizeSystem(ctx context.Context) {
	// Set CPU governor to performance
	for i := 0; i < 4; i++ {
		_, err := p.runner.Run(ctx, exec.RunOpts{
			JobType: fmt.Sprintf("cpu_perf_%d", i),
			Command: "sudo",
			Args:    []string{"bash", "-c", fmt.Sprintf("echo 'performance' > /sys/devices/system/cpu/cpu%d/cpufreq/scaling_governor", i)},
			Timeout: 5 * time.Second,
		})
		if err != nil {
			p.logger.Warn(fmt.Sprintf("Failed to optimize CPU%d", i), zap.Error(err))
		}
	}

	// Increase file descriptor limits
	_, err := p.runner.Run(ctx, exec.RunOpts{
		JobType: "increase_fd_limit",
		Command: "sudo",
		Args:    []string{"bash", "-c", "echo '* soft nofile 65536' >> /etc/security/limits.conf && echo '* hard nofile 65536' >> /etc/security/limits.conf"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		p.logger.Warn("Failed to increase file descriptor limits", zap.Error(err))
	}
}

func (p *PerformanceOptimizer) ensureLXDConfiguration(ctx context.Context) {
	// Get system memory
	memResult, err := p.runner.Run(ctx, exec.RunOpts{
		JobType: "check_memory",
		Command: "free",
		Args:    []string{"-h"},
		Timeout: 5 * time.Second,
	})

	if err == nil && memResult != nil {
		totalMemoryGB := 0.0 // Default value
		for _, line := range memResult.Lines {
			if strings.Contains(line.Text, "Mem:") {
				parts := strings.Fields(line.Text)
				if len(parts) >= 2 {
					memStr := strings.TrimSuffix(parts[1], "G")
					if mem, err := strconv.ParseFloat(memStr, 64); err == nil {
						totalMemoryGB = mem
						break
					}
				}
			}
		}

		// Log memory information for diagnostics
		p.logger.Info("System memory information",
			zap.Float64("total_gb", totalMemoryGB),
		)
	}

	// Ensure LXD daemon is running and properly configured
	_, err = p.runner.Run(ctx, exec.RunOpts{
		JobType: "check_lxd_status",
		Command: "sudo",
		Args:    []string{"systemctl", "start", "lxd"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		p.logger.Warn("Failed to ensure LXD is running", zap.Error(err))
	} else {
		p.logger.Info("LXD service ensured to be running")
	}
}

func (p *PerformanceOptimizer) parseTime(timeStr string) time.Time {
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Now()
	}
	return t
}

// GetStats returns current performance statistics
func (p *PerformanceOptimizer) GetStats() *BuildStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return a copy
	stats := *p.stats
	return &stats
}

// ResetStats resets performance statistics
func (p *PerformanceOptimizer) ResetStats() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stats = &BuildStats{}
}

// SaveStats saves statistics to persistent storage
func (p *PerformanceOptimizer) SaveStats() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	statsDir := "/var/lib/opendeploy"
	if err := os.MkdirAll(statsDir, 0755); err != nil {
		return err
	}

	statsFilePath := filepath.Join(statsDir, "performance_stats.json")
	jsonData, err := json.Marshal(p.stats)
	if err != nil {
		return err
	}
	return os.WriteFile(statsFilePath, jsonData, 0644)
}

// LoadStats loads statistics from persistent storage
func (p *PerformanceOptimizer) LoadStats() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	statsFilePath := "/var/lib/opendeploy/performance_stats.json"
	data, err := os.ReadFile(statsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var stats BuildStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return err
	}

	p.stats = &stats
	return nil
}
