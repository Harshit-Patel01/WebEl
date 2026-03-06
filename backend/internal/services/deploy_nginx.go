package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

// applyNginxForDeploy generates and applies nginx configuration after a successful deploy
func (d *DeployService) applyNginxForDeploy(ctx context.Context, project *state.Project, domain, outputPath string, isBackend bool) error {
	if d.nginx == nil {
		return fmt.Errorf("nginx service not configured")
	}

	// Validate domain
	if !IsValidDomain(domain) {
		return fmt.Errorf("invalid domain: %s", domain)
	}

	// Determine proxy settings for backend
	var proxyEnabled bool
	var proxyPort int

	if isBackend {
		// Get container info to find the host port
		container, err := d.db.GetContainerByProjectID(project.ID)
		if err != nil || container == nil {
			d.logger.Warn("backend deploy but no container found",
				zap.String("projectId", project.ID),
				zap.Error(err),
			)
		} else {
			// Parse port mappings to get host port
			hostPort, containerPort, parseErr := parsePortMapping(container.PortMappings)
			if parseErr == nil && hostPort > 0 {
				proxyEnabled = true
				proxyPort = hostPort
				d.logger.Info("using backend proxy",
					zap.String("domain", domain),
					zap.Int("hostPort", hostPort),
					zap.Int("containerPort", containerPort),
				)
			}
		}
	}

	// Generate nginx config
	siteCfg := NginxSiteConfig{
		Domain:       domain,
		FrontendPath: outputPath,
		ProxyEnabled: proxyEnabled,
		ProxyPort:    proxyPort,
	}
	configContent := d.nginx.GenerateConfig(siteCfg)

	// Write config atomically
	if err := d.nginx.WriteConfig(domain, configContent); err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

	// Test config before reload
	testResult, err := d.nginx.TestConfig(ctx)
	if err != nil {
		return fmt.Errorf("nginx config test failed: %w", err)
	}
	if !testResult.Success {
		return fmt.Errorf("nginx config test failed: %s", testResult.Output)
	}

	// Reload nginx
	if err := d.nginx.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	d.logger.Info("nginx configured successfully",
		zap.String("domain", domain),
		zap.String("outputPath", outputPath),
		zap.Bool("proxyEnabled", proxyEnabled),
		zap.Int("proxyPort", proxyPort),
	)

	return nil
}

// parsePortMapping extracts host and container ports from the JSON port_mappings field
func parsePortMapping(portMappingsJSON string) (hostPort int, containerPort int, err error) {
	if portMappingsJSON == "" {
		return 0, 0, fmt.Errorf("empty port mappings")
	}

	// Parse JSON: {"host":"8080","container":"3000"}
	var mapping struct {
		Host      string `json:"host"`
		Container string `json:"container"`
	}

	if err := json.Unmarshal([]byte(portMappingsJSON), &mapping); err != nil {
		return 0, 0, fmt.Errorf("failed to parse port mappings: %w", err)
	}

	hostPort, err = strconv.Atoi(mapping.Host)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid host port: %w", err)
	}

	containerPort, err = strconv.Atoi(mapping.Container)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid container port: %w", err)
	}

	return hostPort, containerPort, nil
}
