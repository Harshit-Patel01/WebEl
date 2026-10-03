package exec

import (
	"regexp"
	"strconv"
	"strings"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var progressRegex = regexp.MustCompile(`(?i)(\d{1,3})%`)

// StripANSI removes ANSI color codes from terminal output.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// DetectLevel determines the log level from line content.
func DetectLevel(line string) LogLevel {
	lower := strings.ToLower(line)

	// Error patterns
	for _, p := range []string{"error", "err ", "fatal", "panic", "failed", "failure"} {
		if strings.Contains(lower, p) {
			return LevelError
		}
	}

	// Warning patterns
	for _, p := range []string{"warn", "warning", "deprecated"} {
		if strings.Contains(lower, p) {
			return LevelWarn
		}
	}

	// Success patterns
	for _, p := range []string{"✓", "success", "done", "complete", "compiled successfully", "ready"} {
		if strings.Contains(lower, p) {
			return LevelOK
		}
	}

	return LevelInfo
}

// DetectProgress extracts progress percentage and phase from build output.
func DetectProgress(line string) (percent int, phase string, ok bool) {
	lower := strings.ToLower(line)

	// npm/pip style: "[=====>    ] 45%"
	matches := progressRegex.FindStringSubmatch(line)
	if len(matches) >= 2 {
		if pct, err := strconv.Atoi(matches[1]); err == nil && pct >= 0 && pct <= 100 {
			phase = detectPhase(lower)
			return pct, phase, true
		}
	}

	return 0, "", false
}

func detectPhase(line string) string {
	switch {
	case strings.Contains(line, "clone"), strings.Contains(line, "cloning"):
		return "clone"
	case strings.Contains(line, "install"), strings.Contains(line, "resolving"), strings.Contains(line, "fetching"):
		return "install"
	case strings.Contains(line, "build"), strings.Contains(line, "compil"), strings.Contains(line, "bundl"):
		return "build"
	case strings.Contains(line, "done"), strings.Contains(line, "complete"), strings.Contains(line, "success"):
		return "done"
	default:
		return "unknown"
	}
}

// DetectGitError parses known git error patterns into structured error events.
func DetectGitError(line string) (code string, message string, ok bool) {
	lower := strings.ToLower(line)

	switch {
	case strings.Contains(lower, "repository not found"):
		return "repo_not_found", "Repository not found. Check the URL and access permissions.", true
	case strings.Contains(lower, "authentication failed"):
		return "auth_failed", "Authentication failed. Check your credentials or token.", true
	case strings.Contains(lower, "could not resolve host"):
		return "dns_failed", "Could not resolve hostname. Check your internet connection.", true
	case strings.Contains(lower, "permission denied"):
		return "permission_denied", "Permission denied. Check repository access rights.", true
	case strings.Contains(lower, "not a git repository"):
		return "not_git_repo", "Target directory is not a git repository.", true
	default:
		return "", "", false
	}
}
