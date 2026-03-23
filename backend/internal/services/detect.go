package services

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FrameworkInfo holds auto-detected framework details
type FrameworkInfo struct {
	Name       FrameworkType
	InstallCmd string
	BuildCmd   string
	StartCmd   string
	OutputDir  string
}

// detectFrameworkRules defines the matching rules
var frameworkRules = []struct {
	dep  string
	info FrameworkInfo
}{
	{"next", FrameworkInfo{FrameworkNextJS, "npm install", "npm run build", "npm start", ".next"}},
	{"nuxt", FrameworkInfo{FrameworkNuxtJS, "npm install", "npm run build", "node .output/server/index.mjs", ".output"}},
	{"@sveltejs/kit", FrameworkInfo{FrameworkSvelte, "npm install", "npm run build", "node build/index.js", "build"}},
	{"gatsby", FrameworkInfo{FrameworkReact, "npm install", "npm run build", "", "public"}}, // Gatsby uses React
	{"@angular/core", FrameworkInfo{FrameworkAngular, "npm install", "npm run build", "", "dist"}},
	{"vue", FrameworkInfo{FrameworkVue, "npm install", "npm run build", "", "dist"}},
	{"react", FrameworkInfo{FrameworkReact, "npm install", "npm run build", "", "dist"}},
	{"vite", FrameworkInfo{FrameworkVite, "npm install", "npm run build", "", "dist"}},
	{"express", FrameworkInfo{FrameworkExpress, "npm install", "", "node index.js", ""}},
	{"fastify", FrameworkInfo{FrameworkFastify, "npm install", "", "node server.js", ""}},
}

// DetectFramework reads package.json and returns FrameworkInfo
func DetectFramework(workDir string) (*FrameworkInfo, error) {
	data, err := os.ReadFile(filepath.Join(workDir, "package.json"))
	if err != nil {
		return nil, err
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	allDeps := make(map[string]string)
	for k, v := range pkg.Dependencies {
		allDeps[k] = v
	}
	for k, v := range pkg.DevDependencies {
		allDeps[k] = v
	}

	for _, rule := range frameworkRules {
		if _, ok := allDeps[rule.dep]; ok {
			info := rule.info
			return &info, nil
		}
	}

	return nil, nil
}

// DetectLanguage acts as a fallback for non-Node projects
func DetectLanguage(workDir string) FrameworkType {
	checks := []struct {
		file string
		lang FrameworkType
	}{
		{"requirements.txt", FrameworkFlask}, // Fallback Python -> Flask for now
		{"pyproject.toml", FrameworkFlask},
		{"Pipfile", FrameworkFlask},
		{"go.mod", FrameworkGo},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(workDir, c.file)); err == nil {
			return c.lang
		}
	}
	return FrameworkUnknown
}

// ResolveCmd resolves the final command by prioritizing user override
func ResolveCmd(userOverride, detected string) string {
	if userOverride != "" {
		return userOverride
	}
	return detected
}
