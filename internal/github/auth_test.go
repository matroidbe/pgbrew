package github

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func TestWithAuthNoToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	in := []string{"PATH=/bin"}
	if got := WithAuth(in); len(got) != 1 {
		t.Fatalf("no token must leave env unchanged, got %v", got)
	}
}

func TestWithAuthHeaderScopedToGitHub(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "secret")
	m := envMap(WithAuth([]string{"PATH=/bin"}))
	if m["GIT_CONFIG_COUNT"] != "1" {
		t.Fatalf("GIT_CONFIG_COUNT = %q", m["GIT_CONFIG_COUNT"])
	}
	if m["GIT_CONFIG_KEY_0"] != "http.https://github.com/.extraheader" {
		t.Fatalf("key = %q", m["GIT_CONFIG_KEY_0"])
	}
	want := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:secret"))
	if m["GIT_CONFIG_VALUE_0"] != want {
		t.Fatalf("value = %q", m["GIT_CONFIG_VALUE_0"])
	}
}

func TestWithAuthPrefersGHToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh")
	t.Setenv("GITHUB_TOKEN", "actions")
	if Token() != "gh" {
		t.Fatalf("Token() = %q, want GH_TOKEN", Token())
	}
}

func TestWithAuthKeepsExistingGitConfig(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret")
	m := envMap(WithAuth([]string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.autocrlf",
		"GIT_CONFIG_VALUE_0=false",
	}))
	if m["GIT_CONFIG_COUNT"] != "2" || m["GIT_CONFIG_KEY_0"] != "core.autocrlf" ||
		m["GIT_CONFIG_KEY_1"] != "http.https://github.com/.extraheader" {
		t.Fatalf("existing entry clobbered: %v", m)
	}
}

// git itself must read the header from the environment — the whole mechanism
// depends on it, and it needs git >= 2.31.
func TestWithAuthGitSeesHeader(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GH_TOKEN", "secret")
	cmd := exec.Command("git", "config", "--get", "http.https://github.com/.extraheader")
	cmd.Dir = t.TempDir()
	cmd.Env = WithAuth(append(os.Environ(), "HOME="+filepath.Join(cmd.Dir)))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git config: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "Authorization: Basic ") {
		t.Fatalf("git saw %q", out)
	}
}
