package github

import (
	"fmt"
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
func Clone(repo string, dir string, ref string) error {
	url := "https://" + repo + ".git"

	if ref == "" {
		// Simple shallow clone of default branch
		cmd := exec.Command("git", "clone", "--depth", "1", url, dir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git clone failed: %s\n%s", err, string(output))
		}
	} else {
		// Clone with specific ref - need full clone for tags/commits
		cmd := exec.Command("git", "clone", url, dir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git clone failed: %s\n%s", err, string(output))
		}

		// Checkout the specific ref
		cmd = exec.Command("git", "-C", dir, "checkout", ref)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git checkout %s failed: %s\n%s", ref, err, string(output))
		}
	}
	return nil
}
