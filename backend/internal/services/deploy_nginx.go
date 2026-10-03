package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

const nginxListenPort = 80

var (
	frontendProxyRegex = regexp.MustCompile(`location /\s*\{\s*\n\s*proxy_pass http://[^:\s]+:(\d+);`)
	backendProxyRegex  = regexp.MustCompile(`location /api/\s*\{\s*\n\s*proxy_pass http://[^:\s]+:(\d+);`)
)

func extractPortsFromNginxConfig(content string) (frontendPort, backendPort int) {
	if m := frontendProxyRegex.FindStringSubmatch(content); len(m) == 2 {
		if p, err := strconv.Atoi(m[1]); err == nil {
			frontendPort = p
		}
	}
	if m := backendProxyRegex.FindStringSubmatch(content); len(m) == 2 {
		if p, err := strconv.Atoi(m[1]); err == nil {
			backendPort = p
		}
	}
	return frontendPort, backendPort
}

func (d *DeployService) lookupExistingProxyPorts(domain string) (frontendPort, backendPort int) {
	if d.nginx == nil {
		return 0, 0
	}
	for _, name := range []string{domain, "frontend-" + domain, "backend-" + domain} {
		content, err := d.nginx.ReadConfigFile(name)
		if err != nil || content == "" {
			continue
		}
		fp, bp := extractPortsFromNginxConfig(content)
		if frontendPort == 0 && fp > 0 {
			frontendPort = fp
		}
		if backendPort == 0 && bp > 0 {
			backendPort = bp
		}
	}
	return frontendPort, backendPort
}

func (d *DeployService) applyNginxForDeploy(ctx context.Context, project *state.Project, domain, outputPath string, isBackend bool, frontendHostPort, backendHostPort int) (int, error) {
	if d.nginx == nil {
		return 0, fmt.Errorf("nginx service not configured")
	}

	if !IsValidDomain(domain) {
		return 0, fmt.Errorf("invalid domain: %s", domain)
	}

	configName := domain

	d.logger.Info("applying nginx config",
		zap.String("domain", domain),
		zap.String("configName", configName),
		zap.Bool("isBackend", isBackend),
		zap.Int("listenPort", nginxListenPort),
	)

	var proxyEnabled bool
	var proxyPort int

	if isBackend {
		if backendHostPort > 0 {
			proxyEnabled = true
			proxyPort = backendHostPort
			d.logger.Info("using backend proxy",
				zap.String("domain", domain),
				zap.Int("proxyPort", backendHostPort),
			)
		} else {
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

	var frontendProxyEnabled bool
	var frontendProxyPort int
	if !isBackend && frontendHostPort > 0 {
		frontendProxyEnabled = true
		frontendProxyPort = frontendHostPort
	}
	if isBackend && frontendHostPort > 0 {
		frontendProxyEnabled = true
		frontendProxyPort = frontendHostPort
	}

	if existingFrontend, existingBackend := d.lookupExistingProxyPorts(domain); existingFrontend > 0 || existingBackend > 0 {
		if !frontendProxyEnabled && existingFrontend > 0 && existingFrontend != proxyPort {
			frontendProxyEnabled = true
			frontendProxyPort = existingFrontend
			d.logger.Info("preserving existing frontend proxy",
				zap.String("domain", domain),
				zap.Int("frontendProxyPort", existingFrontend),
			)
		}
		if !proxyEnabled && existingBackend > 0 && existingBackend != frontendProxyPort {
			proxyEnabled = true
			proxyPort = existingBackend
			d.logger.Info("preserving existing backend proxy",
				zap.String("domain", domain),
				zap.Int("proxyPort", existingBackend),
			)
		}
	}

	siteCfg := NginxSiteConfig{
		Domain:               domain,
		FrontendPath:         outputPath,
		ListenPort:           nginxListenPort,
		ProxyEnabled:         proxyEnabled,
		ProxyPort:            proxyPort,
		FrontendProxyEnabled: frontendProxyEnabled,
		FrontendProxyPort:    frontendProxyPort,
	}

	configContent := d.nginx.GenerateConfig(siteCfg)

	if err := d.nginx.WriteConfig(configName, configContent); err != nil {
		return 0, fmt.Errorf("failed to write nginx config: %w", err)
	}

	for _, legacy := range []string{"frontend-" + domain, "backend-" + domain} {
		_ = d.nginx.DeleteConfigFile(legacy)
	}

	testResult, err := d.nginx.TestConfig(ctx)
	if err != nil {
		return 0, fmt.Errorf("nginx config test failed: %w", err)
	}
	if !testResult.Success {
		return 0, fmt.Errorf("nginx config test failed: %s", testResult.Output)
	}

	if err := d.nginx.Reload(ctx); err != nil {
		return 0, fmt.Errorf("failed to reload nginx: %w", err)
	}

	d.logger.Info("nginx configured successfully",
		zap.String("domain", domain),
		zap.String("configName", configName),
		zap.Int("listenPort", nginxListenPort),
		zap.String("outputPath", outputPath),
		zap.Bool("proxyEnabled", proxyEnabled),
		zap.Int("proxyPort", proxyPort),
		zap.Bool("frontendProxyEnabled", frontendProxyEnabled),
		zap.Int("frontendProxyPort", frontendProxyPort),
	)

	return nginxListenPort, nil
}

func parsePortMapping(portMappingsJSON string) (hostPort int, containerPort int, err error) {
	if portMappingsJSON == "" {
		return 0, 0, fmt.Errorf("empty port mappings")
	}

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
