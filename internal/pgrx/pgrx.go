package pgrx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

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

	// Look for pgrx = "X.Y.Z" or pgrx = "=X.Y.Z" or pgrx = { version = "X.Y.Z", ... }
	content := string(data)

	// Try simple format: pgrx = "0.14.0" or pgrx = "=0.14.0"
	// The =? makes the leading = optional
	re := regexp.MustCompile(`pgrx\s*=\s*"=?([0-9][^"]*)"`)
	matches := re.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1], nil
	}

	// Try table format: pgrx = { version = "0.14.0", ... } or { version = "=0.14.0", ... }
	re = regexp.MustCompile(`pgrx\s*=\s*\{[^}]*version\s*=\s*"=?([0-9][^"]*)"`)
	matches = re.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1], nil
	}

	return "", fmt.Errorf("could not find pgrx version in Cargo.toml")
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
	if err == nil && installed == requiredVersion {
		return nil // Already have the right version
	}

	fmt.Printf("Installing cargo-pgrx %s (current: %s)...\n", requiredVersion, installed)
	cmd := exec.Command("cargo", "install", "cargo-pgrx", "--version", requiredVersion, "--locked")
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
