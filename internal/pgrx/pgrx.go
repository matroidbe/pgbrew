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

// Install builds and installs the extension using cargo pgrx install.
func Install(dir string) error {
	// Run cargo pgrx install
	cmd := exec.Command("cargo", "pgrx", "install", "--release")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cargo pgrx install failed: %w", err)
	}

	return nil
}
