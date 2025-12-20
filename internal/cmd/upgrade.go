package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade pgx to the latest version",
	Long:  `Download and install the latest version of pgx from GitHub.`,
	RunE:  runUpgrade,
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	fmt.Println("Upgrading pgx...")

	// Check for Go
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("Go is required but not installed. Install from https://go.dev/dl/")
	}

	// Check for Git
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("Git is required but not installed")
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pgbrew-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Clone repository
	repo := "https://github.com/matroidbe/pgbrew.git"
	fmt.Println("Fetching latest version...")
	cloneCmd := exec.Command("git", "clone", "--depth", "1", repo, tmpDir)
	if output, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone repository: %s\n%s", err, string(output))
	}

	// Build
	fmt.Println("Building...")
	buildCmd := exec.Command("go", "build", "-o", "pgx", "./cmd/pgx")
	buildCmd.Dir = tmpDir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build: %s\n%s", err, string(output))
	}

	// Find current executable path
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find current executable: %w", err)
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Replace current executable
	newExe := filepath.Join(tmpDir, "pgx")
	fmt.Printf("Installing to %s...\n", currentExe)

	// On Linux, we can't overwrite a running binary directly.
	// Rename the old one first, then copy the new one in place.
	oldExe := currentExe + ".old"

	// Remove any previous .old file
	os.Remove(oldExe)

	// Rename current executable to .old
	if err := os.Rename(currentExe, oldExe); err != nil {
		return fmt.Errorf("failed to rename current executable: %w", err)
	}

	// Copy new executable into place (can't use Rename across filesystems)
	if err := copyFile(newExe, currentExe); err != nil {
		// Try to restore the old one
		os.Rename(oldExe, currentExe)
		return fmt.Errorf("failed to install new executable: %w", err)
	}

	// Clean up old executable
	os.Remove(oldExe)

	fmt.Println("✓ pgx upgraded successfully!")
	return nil
}

// copyFile copies a file from src to dst with executable permissions
func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0755)
}
