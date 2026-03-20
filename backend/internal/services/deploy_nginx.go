package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)


func (d *DeployService) applyNginxForDeploy(ctx context.Context, project *state.Project, domain, outputPath string, isBackend bool, frontendHostPort, backendHostPort int) error {
	if d.nginx == nil {
		return fmt.Errorf("nginx service not configured")
	}

	// Validate domain
	if !IsValidDomain(domain) {
		return fmt.Errorf("invalid domain: %s", domain)
	}

	// Generate config filename based on type
	var configName string
	if isBackend {
		configName = fmt.Sprintf("backend-%s", domain)
	} else {
		configName = fmt.Sprintf("frontend-%s", domain)
	}

	d.logger.Info("applying nginx config",
		zap.String("domain", domain),
		zap.String("configName", configName),
		zap.Bool("isBackend", isBackend),
	)

	// Determine proxy settings for backend
	var proxyEnabled bool
	var proxyPort int

	if isBackend {
		if backendHostPort > 0 {
			// Use the host port passed directly from the deploy flow
			proxyEnabled = true
			proxyPort = backendHostPort
			d.logger.Info("using backend proxy",
				zap.String("domain", domain),
				zap.Int("proxyPort", backendHostPort),
			)
		} else {
			// Fallback: look up container from DB (for single-app backend deploys)
			container, err := d.db.GetContainerByProjectID(project.ID)
			if err != nil || container == nil {
				d.logger.Warn("backend deploy but no container found",
					zap.String("projectId", project.ID),
					zap.Error(err),
				)
			} else {
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
	}

	// Determine frontend proxy settings
	var frontendProxyEnabled bool
	var frontendProxyPort int
	if !isBackend && frontendHostPort > 0 {
		frontendProxyEnabled = true
		frontendProxyPort = frontendHostPort
	}

	// Generate nginx config for this specific type (frontend or backend)
	siteCfg := NginxSiteConfig{
		Domain:               domain,
		FrontendPath:         outputPath,
		ProxyEnabled:         proxyEnabled,
		ProxyPort:            proxyPort,
		FrontendProxyEnabled: frontendProxyEnabled,
		FrontendProxyPort:    frontendProxyPort,
	}

	// Generate config content based on type
	var configContent string
	if isBackend {
		configContent = d.nginx.GenerateBackendConfig(siteCfg)
	} else {
		configContent = d.nginx.GenerateFrontendConfig(siteCfg)
	}

	// Write config atomically (use configName instead of domain)
	if err := d.nginx.WriteConfig(configName, configContent); err != nil {
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
		zap.String("configName", configName),
		zap.String("outputPath", outputPath),
		zap.Bool("proxyEnabled", proxyEnabled),
		zap.Int("proxyPort", proxyPort),
		zap.Bool("frontendProxyEnabled", frontendProxyEnabled),
		zap.Int("frontendProxyPort", frontendProxyPort),
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
