package github

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ParseURL parses a GitHub URL and returns the repository, optional subpath, and version.
// Examples:
//   - github.com/user/repo -> ("github.com/user/repo", "", "")
//   - github.com/user/repo@v1.0.0 -> ("github.com/user/repo", "", "v1.0.0")
//   - github.com/user/repo/path/to/ext -> ("github.com/user/repo", "path/to/ext", "")
//   - github.com/user/repo/path/to/ext@v1.0.0 -> ("github.com/user/repo", "path/to/ext", "v1.0.0")
func ParseURL(url string) (repo string, subpath string, version string, err error) {
	// Remove https:// prefix if present
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")

	// Extract version if present (after @)
	if idx := strings.LastIndex(url, "@"); idx != -1 {
		version = url[idx+1:]
		url = url[:idx]
	}

	// Must start with github.com
	if !strings.HasPrefix(url, "github.com/") {
		return "", "", "", fmt.Errorf("only GitHub repositories are supported")
	}

	parts := strings.Split(url, "/")
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("invalid GitHub URL: expected github.com/user/repo")
	}

	// First 3 parts are github.com/user/repo
	repo = strings.Join(parts[:3], "/")

	// Remaining parts are the subpath
	if len(parts) > 3 {
		subpath = strings.Join(parts[3:], "/")
	}

	return repo, subpath, version, nil
}

// Clone clones a GitHub repository to the specified directory.
// If ref is provided (tag, branch, or commit), it checks out that ref.
//
// A private repository is cloned with the token from GH_TOKEN or GITHUB_TOKEN
// when one is set (see WithAuth), and otherwise with whatever credentials git
// already has.
func Clone(repo string, dir string, ref string) error {
	url := "https://" + repo + ".git"
	env := WithAuth(os.Environ())

	args := []string{"clone", url, dir}
	if ref == "" {
		args = []string{"clone", "--depth", "1", url, dir}
	}
	if err := runGit(env, args...); err != nil {
		return fmt.Errorf("git clone failed: %w%s", err, authHint(err))
	}
	if ref != "" {
		if err := runGit(env, "-C", dir, "checkout", ref); err != nil {
			return fmt.Errorf("git checkout %s failed: %w", ref, err)
		}
	}
	return nil
}

func runGit(env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Env = append(env,
		// Fail instead of prompting: pgbrew is often run where nobody can
		// answer a username prompt, and a hung image build explains nothing.
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, string(output))
	}
	return nil
}

// authHint explains the likely fix when a clone fails because the repository
// is private (GitHub answers "not found" rather than "forbidden" for those).
func authHint(err error) string {
	msg := err.Error()
	if !strings.Contains(msg, "could not read Username") &&
		!strings.Contains(msg, "not found") &&
		!strings.Contains(msg, "Authentication failed") {
		return ""
	}
	if Token() != "" {
		return "\nA token is set but was refused: check that it can read this repository."
	}
	return "\nIf the repository is private, set GH_TOKEN (or GITHUB_TOKEN) to a token that can read it,\n" +
		"e.g. GH_TOKEN=$(gh auth token) pgx install ..."
}
