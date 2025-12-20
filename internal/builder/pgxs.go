package builder

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// PgxsBuilder implements the Builder interface for C extensions using PGXS Makefiles.
type PgxsBuilder struct{}

func init() {
	Register(&PgxsBuilder{})
}

func (b *PgxsBuilder) Name() string {
	return "pgxs"
}

// Detect checks if this is a PGXS-based C extension project.
// It looks for a Makefile containing PGXS and a .control file.
func (b *PgxsBuilder) Detect(dir string) bool {
	// Check for Makefile with PGXS
	makefilePath := filepath.Join(dir, "Makefile")
	if !hasPGXS(makefilePath) {
		return false
	}

	// Check for at least one .control file
	controlFiles, _ := filepath.Glob(filepath.Join(dir, "*.control"))
	return len(controlFiles) > 0
}

// hasPGXS checks if a Makefile includes PGXS
func hasPGXS(makefilePath string) bool {
	data, err := os.ReadFile(makefilePath)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "PGXS") || strings.Contains(content, "pgxs")
}

// GetExtensionName extracts the extension name from the .control file.
func (b *PgxsBuilder) GetExtensionName(dir string) (string, error) {
	// Find .control files
	controlFiles, err := filepath.Glob(filepath.Join(dir, "*.control"))
	if err != nil || len(controlFiles) == 0 {
		// Fallback to directory name
		return filepath.Base(dir), nil
	}

	// Use the first .control file's name (without extension)
	controlFile := controlFiles[0]
	baseName := filepath.Base(controlFile)
	return strings.TrimSuffix(baseName, ".control"), nil
}

// GetVersion extracts the default_version from the .control file.
func (b *PgxsBuilder) GetVersion(dir string) (string, error) {
	// Find .control files
	controlFiles, err := filepath.Glob(filepath.Join(dir, "*.control"))
	if err != nil || len(controlFiles) == 0 {
		return "", fmt.Errorf("no .control file found")
	}

	// Parse the control file for default_version
	controlFile := controlFiles[0]
	return parseControlVersion(controlFile)
}

// parseControlVersion extracts default_version from a .control file
func parseControlVersion(controlPath string) (string, error) {
	file, err := os.Open(controlPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Match: default_version = '0.8.0' or default_version = "0.8.0"
	re := regexp.MustCompile(`default_version\s*=\s*['"]([^'"]+)['"]`)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)
		if len(matches) >= 2 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("default_version not found in %s", controlPath)
}

// Install builds and installs the extension using make.
func (b *PgxsBuilder) Install(dir string, pgConfig string) error {
	// Determine pg_config path
	if pgConfig == "" {
		pgConfig = os.Getenv("PG_CONFIG")
	}
	if pgConfig == "" {
		pgConfig = "pg_config"
	}

	// Build make arguments
	// Override CC to use the system's default gcc, since PostgreSQL's pg_config
	// may reference a specific version (e.g., gcc-12) that isn't installed
	makeArgs := []string{
		"PG_CONFIG=" + pgConfig,
		"CC=gcc",
	}

	// Run make clean (ignore errors - may not have been built before)
	cleanArgs := append([]string{"clean"}, makeArgs...)
	cleanCmd := exec.Command("make", cleanArgs...)
	cleanCmd.Dir = dir
	cleanCmd.Run() // Ignore errors

	// Run make
	fmt.Println("Running make...")
	makeCmd := exec.Command("make", makeArgs...)
	makeCmd.Dir = dir
	makeCmd.Stdout = os.Stdout
	makeCmd.Stderr = os.Stderr
	if err := makeCmd.Run(); err != nil {
		return fmt.Errorf("make failed: %w", err)
	}

	// Run make install
	fmt.Println("Running make install...")
	installArgs := append([]string{"install"}, makeArgs...)
	installCmd := exec.Command("make", installArgs...)
	installCmd.Dir = dir
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("make install failed: %w", err)
	}

	return nil
}

// NeedsSharedPreload checks if the extension requires shared_preload_libraries.
// For C extensions, we check the .control file for mentions of shared_preload_libraries.
func (b *PgxsBuilder) NeedsSharedPreload(dir string) bool {
	// Check .control files
	controlFiles, _ := filepath.Glob(filepath.Join(dir, "*.control"))
	for _, controlFile := range controlFiles {
		data, err := os.ReadFile(controlFile)
		if err != nil {
			continue
		}
		content := strings.ToLower(string(data))
		if strings.Contains(content, "shared_preload") {
			return true
		}
	}

	// Check Makefile for background worker indicators
	makefilePath := filepath.Join(dir, "Makefile")
	data, err := os.ReadFile(makefilePath)
	if err != nil {
		return false
	}
	content := strings.ToLower(string(data))
	return strings.Contains(content, "bgworker") || strings.Contains(content, "background_worker")
}
