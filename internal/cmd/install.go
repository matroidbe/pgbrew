package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/matroidbe/pgbrew/internal/github"
	"github.com/matroidbe/pgbrew/internal/pgrx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install <source>",
	Short: "Install a PostgreSQL extension",
	Long: `Install a pgrx-based PostgreSQL extension from GitHub or a local directory.

Examples:
  pgx install github.com/supabase/pg_graphql
  pgx install github.com/supabase/pg_graphql@v1.5.0
  pgx install github.com/user/repo/extensions/myext@main
  pgx install ./pg_hello
  pgx install /path/to/extension`,
	Args: cobra.ExactArgs(1),
	RunE: runInstall,
}

func runInstall(cmd *cobra.Command, args []string) error {
	source := args[0]

	var extDir string
	var cleanupDir string

	// Check if source is a local path
	if isLocalPath(source) {
		absPath, err := filepath.Abs(source)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}

		// Verify directory exists
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return fmt.Errorf("directory not found: %s", absPath)
		}

		extDir = absPath
		fmt.Printf("Installing from %s...\n", absPath)
	} else {
		// Parse GitHub URL
		repo, subpath, version, err := github.ParseURL(source)
		if err != nil {
			return fmt.Errorf("invalid source: %w", err)
		}

		fmt.Printf("Installing from %s...\n", source)

		// Clone repository to temp directory
		tmpDir, err := os.MkdirTemp("", "pgbrew-*")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		cleanupDir = tmpDir

		if version != "" {
			fmt.Printf("Cloning %s@%s...\n", repo, version)
		} else {
			fmt.Printf("Cloning %s...\n", repo)
		}
		if err := github.Clone(repo, tmpDir, version); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}

		// Determine extension directory
		extDir = tmpDir
		if subpath != "" {
			extDir = filepath.Join(tmpDir, subpath)
		}
	}

	// Cleanup temp directory if created
	if cleanupDir != "" {
		defer os.RemoveAll(cleanupDir)
	}

	// Verify it's a pgrx project
	if !pgrx.IsProject(extDir) {
		return fmt.Errorf("not a pgrx project: %s", extDir)
	}

	// Get extension name from Cargo.toml
	extName, err := pgrx.GetExtensionName(extDir)
	if err != nil {
		return fmt.Errorf("failed to get extension name: %w", err)
	}

	fmt.Printf("Building %s...\n", extName)

	// Build and install with pgrx
	if err := pgrx.Install(extDir); err != nil {
		return fmt.Errorf("failed to install extension: %w", err)
	}

	// Get version from Cargo.toml
	version, _ := pgrx.GetVersion(extDir)
	if version == "" {
		version = "unknown"
	}

	// Get PostgreSQL version
	pgVersion := getPgVersion()

	// Record installation
	entry := cellar.Entry{
		Name:      extName,
		Version:   version,
		Source:    source,
		PgVersion: pgVersion,
	}
	if err := cellar.Add(entry); err != nil {
		return fmt.Errorf("failed to record installation: %w", err)
	}

	fmt.Printf("\n✓ Successfully installed %s %s\n", extName, version)
	fmt.Printf("  Run: CREATE EXTENSION %s;\n", extName)

	return nil
}

func getPgVersion() string {
	cmd := exec.Command(getPgConfigPath(), "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Parse "PostgreSQL 16.0" -> "16"
	parts := strings.Fields(string(output))
	if len(parts) >= 2 {
		version := parts[1]
		if idx := strings.Index(version, "."); idx > 0 {
			return version[:idx]
		}
		return version
	}
	return ""
}

// isLocalPath checks if the source is a local filesystem path
func isLocalPath(source string) bool {
	// Starts with ./ or ../ or /
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "/") {
		return true
	}
	// Check if it's an existing directory (handles bare names like "pg_hello")
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return true
	}
	return false
}
