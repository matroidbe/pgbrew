package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

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

	// Check pg_config
	if checkCommand("pg_config", "--version") {
		version := getCommandOutput("pg_config", "--version")
		fmt.Printf("✓ PostgreSQL: %s\n", strings.TrimSpace(version))

		// Show additional info
		pgLibDir := getCommandOutput("pg_config", "--pkglibdir")
		pgShareDir := getCommandOutput("pg_config", "--sharedir")
		fmt.Printf("  Library dir: %s\n", strings.TrimSpace(pgLibDir))
		fmt.Printf("  Share dir: %s\n", strings.TrimSpace(pgShareDir))
	} else {
		fmt.Println("✗ PostgreSQL: pg_config not found")
		fmt.Println("  Install PostgreSQL or add pg_config to PATH")
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
