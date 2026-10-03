package services

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
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
