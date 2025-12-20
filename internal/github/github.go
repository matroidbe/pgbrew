package github

import (
	"fmt"
	"os/exec"
	"strings"
)

// ParseURL parses a GitHub URL and returns the repository and optional subpath.
// Examples:
//   - github.com/user/repo -> ("github.com/user/repo", "")
//   - github.com/user/repo/path/to/ext -> ("github.com/user/repo", "path/to/ext")
func ParseURL(url string) (repo string, subpath string, err error) {
	// Remove https:// prefix if present
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")

	// Must start with github.com
	if !strings.HasPrefix(url, "github.com/") {
		return "", "", fmt.Errorf("only GitHub repositories are supported")
	}

	parts := strings.Split(url, "/")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("invalid GitHub URL: expected github.com/user/repo")
	}

	// First 3 parts are github.com/user/repo
	repo = strings.Join(parts[:3], "/")

	// Remaining parts are the subpath
	if len(parts) > 3 {
		subpath = strings.Join(parts[3:], "/")
	}

	return repo, subpath, nil
}

// Clone clones a GitHub repository to the specified directory.
func Clone(repo string, dir string) error {
	url := "https://" + repo + ".git"
	cmd := exec.Command("git", "clone", "--depth", "1", url, dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %s\n%s", err, string(output))
	}
	return nil
}
