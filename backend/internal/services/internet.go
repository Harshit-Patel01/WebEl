package services

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"go.uber.org/zap"
)

type InternetCheck struct {
	DNSResolution  *CheckResult `json:"dns_resolution"`
	CloudflarePing *CheckResult `json:"cloudflare_ping"`
	DownloadSpeed  *CheckResult `json:"download_speed"`
}

type CheckResult struct {
	Success bool   `json:"success"`
	Value   string `json:"value"`
	Error   string `json:"error,omitempty"`
}

type InternetService struct {
	runner *exec.Runner
	logger *zap.Logger
}

func NewInternetService(runner *exec.Runner, logger *zap.Logger) *InternetService {
	return &InternetService{runner: runner, logger: logger}
}

func (s *InternetService) RunChecks(ctx context.Context) (*InternetCheck, error) {
	check := &InternetCheck{}

	// DNS Resolution Check
	check.DNSResolution = s.checkDNS(ctx)

	// Cloudflare Ping Check
	check.CloudflarePing = s.checkPing(ctx)

	// Download Speed Test
	check.DownloadSpeed = s.checkDownloadSpeed(ctx)

	return check, nil
}

func (s *InternetService) checkDNS(ctx context.Context) *CheckResult {
	start := time.Now()

	resolver := &net.Resolver{}
	_, err := resolver.LookupHost(ctx, "cloudflare.com")

	elapsed := time.Since(start)

	if err != nil {
		return &CheckResult{
			Success: false,
			Value:   "0ms",
			Error:   err.Error(),
		}
	}

	return &CheckResult{
		Success: true,
		Value:   fmt.Sprintf("%dms", elapsed.Milliseconds()),
	}
}

func (s *InternetService) checkPing(ctx context.Context) *CheckResult {
	start := time.Now()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", "https://1.1.1.1", nil)
	if err != nil {
		return &CheckResult{
			Success: false,
			Value:   "0ms",
			Error:   err.Error(),
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return &CheckResult{
			Success: false,
			Value:   "0ms",
			Error:   err.Error(),
		}
	}
	defer resp.Body.Close()

	elapsed := time.Since(start)

	return &CheckResult{
		Success: true,
		Value:   fmt.Sprintf("%dms", elapsed.Milliseconds()),
	}
}

func (s *InternetService) checkDownloadSpeed(ctx context.Context) *CheckResult {
	// Download a 10MB test file from Cloudflare
	testURL := "https://speed.cloudflare.com/__down?bytes=10000000"

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return &CheckResult{
			Success: false,
			Value:   "0 Mbps",
			Error:   err.Error(),
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return &CheckResult{
			Success: false,
			Value:   "0 Mbps",
			Error:   err.Error(),
		}
	}
	defer resp.Body.Close()

	// Read the response body
	written, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return &CheckResult{
			Success: false,
			Value:   "0 Mbps",
			Error:   err.Error(),
		}
	}

	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		elapsed = 0.001
	}

	// Calculate speed in Mbps
	bytesPerSecond := float64(written) / elapsed
	mbps := (bytesPerSecond * 8) / 1000000

	return &CheckResult{
		Success: true,
		Value:   fmt.Sprintf("%.1f Mbps", mbps),
	}
}

// Alternative ping using system ping command
func (s *InternetService) checkPingCommand(ctx context.Context) *CheckResult {
	result, err := s.runner.Run(ctx, exec.RunOpts{
		JobType: "internet_ping",
		Command: "ping",
		Args:    []string{"-c", "3", "1.1.1.1"},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		return &CheckResult{
			Success: false,
			Value:   "0ms",
			Error:   "ping failed",
		}
	}

	// Parse ping output to get average time
	for _, line := range result.Lines {
		if line.Stream == "stdout" && strings.Contains(line.Text, "avg") {
			// Example: rtt min/avg/max/mdev = 10.123/15.456/20.789/5.123 ms
			parts := strings.Split(line.Text, "=")
			if len(parts) == 2 {
				times := strings.Split(strings.TrimSpace(parts[1]), "/")
				if len(times) >= 2 {
					avgStr := strings.TrimSpace(times[1])
					if avg, err := strconv.ParseFloat(avgStr, 64); err == nil {
						return &CheckResult{
							Success: true,
							Value:   fmt.Sprintf("%.0fms", avg),
						}
					}
				}
			}
		}
	}

	return &CheckResult{
		Success: true,
		Value:   "< 50ms",
	}
}
