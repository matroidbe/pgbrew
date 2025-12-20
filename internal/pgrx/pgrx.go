package pgrx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// NeedsSharedPreload checks if the extension uses background workers
// which require shared_preload_libraries configuration.
func NeedsSharedPreload(dir string) bool {
	return checkDirForBgWorker(filepath.Join(dir, "src"))
}

func checkDirForBgWorker(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			// Check for parallel_worker or bgworker directory names
			name := strings.ToLower(entry.Name())
			if strings.Contains(name, "worker") || strings.Contains(name, "bgw") {
				return true
			}
			// Recurse into subdirectories
			if checkDirForBgWorker(fullPath) {
				return true
			}
		} else if strings.HasSuffix(entry.Name(), ".rs") {
			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}

			// Look for background worker indicators in pgrx
			contentStr := string(content)
			if strings.Contains(contentStr, "BackgroundWorker") ||
				strings.Contains(contentStr, "bgworker") ||
				strings.Contains(contentStr, "RegisterDynamicBackgroundWorker") ||
				strings.Contains(contentStr, "SharedMemoryInit") ||
				strings.Contains(contentStr, "max_parallel_workers") ||
				strings.Contains(contentStr, "ParallelWorker") {
				return true
			}
		}
	}

	return false
}

// IsProject checks if the directory contains a pgrx-based Cargo project.
func IsProject(dir string) bool {
	cargoPath := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(cargoPath)
	if err != nil {
		return false
	}

	// Check if pgrx is a dependency
	content := string(data)
	return strings.Contains(content, "pgrx")
}

// GetExtensionName extracts the package name from Cargo.toml.
func GetExtensionName(dir string) (string, error) {
	cargoPath := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(cargoPath)
	if err != nil {
		return "", err
	}

	// Simple regex to find name = "..."
	re := regexp.MustCompile(`name\s*=\s*"([^"]+)"`)
	matches := re.FindSubmatch(data)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not find package name in Cargo.toml")
	}

	return string(matches[1]), nil
}

// GetVersion extracts the version from Cargo.toml.
func GetVersion(dir string) (string, error) {
	cargoPath := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(cargoPath)
	if err != nil {
		return "", err
	}

	// Simple regex to find version = "..."
	re := regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
	matches := re.FindSubmatch(data)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not find version in Cargo.toml")
	}

	return string(matches[1]), nil
}

// GetPgrxVersion extracts the pgrx version from Cargo.toml.
func GetPgrxVersion(dir string) (string, error) {
	cargoPath := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(cargoPath)
	if err != nil {
		return "", err
	}

	content := string(data)

	// Check if using workspace dependency (pgrx.workspace = true)
	if strings.Contains(content, "pgrx.workspace") {
		// Look for workspace Cargo.toml in parent directories
		parentDir := filepath.Dir(dir)
		for parentDir != "/" && parentDir != "." {
			workspaceCargo := filepath.Join(parentDir, "Cargo.toml")
			if workspaceData, err := os.ReadFile(workspaceCargo); err == nil {
				workspaceContent := string(workspaceData)
				// Check if this is a workspace with pgrx dependency
				if strings.Contains(workspaceContent, "[workspace") {
					version := extractPgrxVersion(workspaceContent)
					if version != "" {
						return version, nil
					}
				}
			}
			parentDir = filepath.Dir(parentDir)
		}
	}

	// Try to extract version directly from this Cargo.toml
	version := extractPgrxVersion(content)
	if version != "" {
		return version, nil
	}

	return "", fmt.Errorf("could not find pgrx version in Cargo.toml")
}

// extractPgrxVersion extracts pgrx version from Cargo.toml content
func extractPgrxVersion(content string) string {
	// Try simple format: pgrx = "0.14.0" or pgrx = "=0.14.0"
	re := regexp.MustCompile(`pgrx\s*=\s*"=?([0-9][^"]*)"`)
	matches := re.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1]
	}

	// Try table format: pgrx = { version = "0.14.0", ... } or { version = "=0.14.0", ... }
	re = regexp.MustCompile(`pgrx\s*=\s*\{[^}]*version\s*=\s*"=?([0-9][^"]*)"`)
	matches = re.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1]
	}

	return ""
}

// GetInstalledPgrxVersion returns the installed cargo-pgrx version.
func GetInstalledPgrxVersion() (string, error) {
	cmd := exec.Command("cargo", "pgrx", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output: "cargo-pgrx 0.12.9"
	parts := strings.Fields(string(output))
	if len(parts) >= 2 {
		return parts[1], nil
	}
	return "", fmt.Errorf("could not parse cargo-pgrx version")
}

// EnsurePgrxVersion installs the required cargo-pgrx version if needed.
func EnsurePgrxVersion(requiredVersion string) error {
	installed, err := GetInstalledPgrxVersion()

	// Check if installed version matches required (including partial version matches)
	if err == nil {
		if installed == requiredVersion {
			return nil // Exact match
		}
		// Check partial version match (e.g., "0.12" matches "0.12.9")
		if strings.HasPrefix(installed, requiredVersion+".") || strings.HasPrefix(installed, requiredVersion) && !strings.Contains(requiredVersion, ".") {
			return nil // Partial match (major or major.minor)
		}
		// Check major.minor match
		installedParts := strings.Split(installed, ".")
		requiredParts := strings.Split(requiredVersion, ".")
		if len(installedParts) >= 2 && len(requiredParts) >= 2 {
			if installedParts[0] == requiredParts[0] && installedParts[1] == requiredParts[1] {
				return nil // Same major.minor version
			}
		}
	}

	// Normalize version for cargo install - if it's a partial version like "0.12",
	// we need to use a caret range "^0.12" to let cargo find the latest patch
	installVersion := requiredVersion
	versionParts := strings.Split(requiredVersion, ".")
	if len(versionParts) < 3 {
		// Partial version, use caret range
		installVersion = "^" + requiredVersion
	}

	fmt.Printf("Installing cargo-pgrx %s (current: %s)...\n", requiredVersion, installed)
	cmd := exec.Command("cargo", "install", "cargo-pgrx", "--version", installVersion, "--locked")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install cargo-pgrx %s: %w", requiredVersion, err)
	}

	return nil
}

// Install builds and installs the extension using cargo pgrx install.
func Install(dir string) error {
	// Check pgrx version compatibility
	requiredVersion, err := GetPgrxVersion(dir)
	if err == nil && requiredVersion != "" {
		if err := EnsurePgrxVersion(requiredVersion); err != nil {
			return err
		}
	}

	// Build command args
	args := []string{"pgrx", "install", "--release"}

	// Pass pg_config path if PG_CONFIG is set
	if pgConfig := os.Getenv("PG_CONFIG"); pgConfig != "" {
		args = append(args, "--pg-config", pgConfig)
	}

	// Run cargo pgrx install
	cmd := exec.Command("cargo", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cargo pgrx install failed: %w", err)
	}

	return nil
}
