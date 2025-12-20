package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// getPgConfigPath returns the path to pg_config, checking PG_CONFIG env var first
func getPgConfigPath() string {
	if pgConfig := os.Getenv("PG_CONFIG"); pgConfig != "" {
		return pgConfig
	}
	return "pg_config"
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system prerequisites",
	Long:  `Verifies that all required tools are installed for building and installing PostgreSQL extensions.`,
	Run:   runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) {
	fmt.Println("Checking system prerequisites...")
	fmt.Println()

	allOk := true

	// Check Rust
	if checkCommand("rustc", "--version") {
		version := getCommandOutput("rustc", "--version")
		fmt.Printf("✓ Rust: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ Rust: not installed")
		fmt.Println("  Install: https://rustup.rs/")
		allOk = false
	}

	// Check Cargo
	if checkCommand("cargo", "--version") {
		version := getCommandOutput("cargo", "--version")
		fmt.Printf("✓ Cargo: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ Cargo: not installed")
		allOk = false
	}

	// Check cargo-pgrx
	if checkCommand("cargo", "pgrx", "--version") {
		version := getCommandOutput("cargo", "pgrx", "--version")
		fmt.Printf("✓ cargo-pgrx: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ cargo-pgrx: not installed")
		fmt.Println("  Install: cargo install cargo-pgrx")
		allOk = false
	}

	// Check pg_config (supports PG_CONFIG env var)
	pgConfigPath := getPgConfigPath()
	if checkCommand(pgConfigPath, "--version") {
		version := getCommandOutput(pgConfigPath, "--version")
		fmt.Printf("✓ PostgreSQL: %s\n", strings.TrimSpace(version))

		// Show additional info
		pgLibDir := getCommandOutput(pgConfigPath, "--pkglibdir")
		pgShareDir := getCommandOutput(pgConfigPath, "--sharedir")
		fmt.Printf("  Library dir: %s\n", strings.TrimSpace(pgLibDir))
		fmt.Printf("  Share dir: %s\n", strings.TrimSpace(pgShareDir))
		if os.Getenv("PG_CONFIG") != "" {
			fmt.Printf("  Using PG_CONFIG: %s\n", pgConfigPath)
		}
	} else {
		fmt.Println("✗ PostgreSQL: pg_config not found")
		fmt.Println("  Install PostgreSQL or add pg_config to PATH")
		fmt.Println("  Or set PG_CONFIG=/path/to/pg_config")
		allOk = false
	}

	// Check Git
	if checkCommand("git", "--version") {
		version := getCommandOutput("git", "--version")
		fmt.Printf("✓ Git: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ Git: not installed")
		allOk = false
	}

	fmt.Println()
	if allOk {
		fmt.Println("All prerequisites satisfied!")
	} else {
		fmt.Println("Some prerequisites are missing. Please install them before continuing.")
	}
}

func checkCommand(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	return cmd.Run() == nil
}

func getCommandOutput(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}
