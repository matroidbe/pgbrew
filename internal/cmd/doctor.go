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
	var pgMajorVersion string
	if checkCommand(pgConfigPath, "--version") {
		version := getCommandOutput(pgConfigPath, "--version")
		fmt.Printf("✓ PostgreSQL: %s\n", strings.TrimSpace(version))

		// Extract major version (e.g., "PostgreSQL 16.0" -> "16")
		parts := strings.Fields(version)
		if len(parts) >= 2 {
			verStr := parts[1]
			if idx := strings.Index(verStr, "."); idx > 0 {
				pgMajorVersion = verStr[:idx]
			} else {
				pgMajorVersion = verStr
			}
		}

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

	// Check if pgrx is initialized for this PostgreSQL version
	if pgMajorVersion != "" && checkCommand("cargo", "pgrx", "--version") {
		pgrxPgConfig := getCommandOutput("cargo", "pgrx", "info", "pg-config", "pg"+pgMajorVersion)
		pgrxPgConfig = strings.TrimSpace(pgrxPgConfig)
		if pgrxPgConfig == "" || strings.Contains(pgrxPgConfig, "not managed") {
			fmt.Printf("✗ pgrx not initialized for pg%s\n", pgMajorVersion)
			fmt.Printf("  Run: cargo pgrx init --pg%s=%s\n", pgMajorVersion, pgConfigPath)
			allOk = false
		} else {
			fmt.Printf("✓ pgrx initialized for pg%s\n", pgMajorVersion)
		}
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
	fmt.Println("C extension support (PGXS):")

	// Check Make
	if checkCommand("make", "--version") {
		version := getCommandOutput("make", "--version")
		// Get just the first line
		if idx := strings.Index(version, "\n"); idx > 0 {
			version = version[:idx]
		}
		fmt.Printf("✓ Make: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ Make: not installed")
		fmt.Println("  Install: apt install build-essential (Debian/Ubuntu)")
	}

	// Check C compiler (gcc or cc)
	ccFound := false
	if checkCommand("gcc", "--version") {
		version := getCommandOutput("gcc", "--version")
		if idx := strings.Index(version, "\n"); idx > 0 {
			version = version[:idx]
		}
		fmt.Printf("✓ GCC: %s\n", strings.TrimSpace(version))
		ccFound = true
	} else if checkCommand("cc", "--version") {
		version := getCommandOutput("cc", "--version")
		if idx := strings.Index(version, "\n"); idx > 0 {
			version = version[:idx]
		}
		fmt.Printf("✓ CC: %s\n", strings.TrimSpace(version))
		ccFound = true
	}
	if !ccFound {
		fmt.Println("✗ C compiler: not installed")
		fmt.Println("  Install: apt install build-essential (Debian/Ubuntu)")
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
