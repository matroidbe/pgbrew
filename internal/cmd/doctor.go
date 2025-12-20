package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var fixFlag bool

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
	Long: `Verifies that all required tools are installed for building and installing PostgreSQL extensions.

Use --fix to automatically install missing user-space tools (Rust, cargo-pgrx, pgrx init).
System packages (git, make, gcc) require manual installation with sudo.`,
	Run: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&fixFlag, "fix", false, "Attempt to install missing prerequisites")
}

func runDoctor(cmd *cobra.Command, args []string) {
	fmt.Println("Checking system prerequisites...")
	fmt.Println()

	allOk := true
	fixedCount := 0

	// Check Rust
	rustOk := checkCommand("rustc", "--version")
	if rustOk {
		version := getCommandOutput("rustc", "--version")
		fmt.Printf("✓ Rust: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ Rust: not installed")
		if fixFlag {
			if err := installRust(); err != nil {
				fmt.Printf("  ✗ Failed: %v\n", err)
			} else {
				rustOk = true
				fixedCount++
			}
		} else {
			fmt.Println("  Install: https://rustup.rs/")
		}
		allOk = false
	}

	// Check Cargo (comes with Rust)
	cargoOk := checkCommand("cargo", "--version")
	if cargoOk {
		version := getCommandOutput("cargo", "--version")
		fmt.Printf("✓ Cargo: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ Cargo: not installed")
		if !rustOk {
			fmt.Println("  (Will be installed with Rust)")
		}
		allOk = false
	}

	// Check cargo-pgrx
	pgrxOk := checkCommand("cargo", "pgrx", "--version")
	if pgrxOk {
		version := getCommandOutput("cargo", "pgrx", "--version")
		fmt.Printf("✓ cargo-pgrx: %s\n", strings.TrimSpace(version))
	} else {
		fmt.Println("✗ cargo-pgrx: not installed")
		if fixFlag && cargoOk {
			if err := installCargoPgrx(); err != nil {
				fmt.Printf("  ✗ Failed: %v\n", err)
			} else {
				pgrxOk = true
				fixedCount++
			}
		} else if !fixFlag {
			fmt.Println("  Install: cargo install cargo-pgrx")
		} else {
			fmt.Println("  (Requires Cargo to be installed first)")
		}
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
		fmt.Printf("  Install: %s\n", getInstallHint("postgresql"))
		fmt.Println("  Or set PG_CONFIG=/path/to/pg_config")
		allOk = false
	}

	// Check if pgrx is initialized for this PostgreSQL version
	if pgMajorVersion != "" && pgrxOk {
		pgrxPgConfig := getCommandOutput("cargo", "pgrx", "info", "pg-config", "pg"+pgMajorVersion)
		pgrxPgConfig = strings.TrimSpace(pgrxPgConfig)
		if pgrxPgConfig == "" || strings.Contains(pgrxPgConfig, "not managed") {
			fmt.Printf("✗ pgrx not initialized for pg%s\n", pgMajorVersion)
			if fixFlag {
				if err := initPgrx(pgMajorVersion, pgConfigPath); err != nil {
					fmt.Printf("  ✗ Failed: %v\n", err)
				} else {
					fixedCount++
				}
			} else {
				fmt.Printf("  Run: cargo pgrx init --pg%s=%s\n", pgMajorVersion, pgConfigPath)
			}
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
		fmt.Printf("  Install: %s\n", getInstallHint("git"))
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
		fmt.Printf("  Install: %s\n", getInstallHint("build-essential"))
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
		fmt.Printf("  Install: %s\n", getInstallHint("build-essential"))
	}

	fmt.Println()
	if fixFlag && fixedCount > 0 {
		fmt.Printf("Fixed %d issue(s).\n", fixedCount)
	}
	if allOk || (fixFlag && fixedCount > 0) {
		fmt.Println("All prerequisites satisfied!")
	} else {
		fmt.Println("Some prerequisites are missing. Please install them before continuing.")
		if !fixFlag {
			fmt.Println("Run 'pgx doctor --fix' to auto-install Rust toolchain components.")
		}
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

// installRust installs Rust via rustup
func installRust() error {
	fmt.Println("  → Installing Rust via rustup...")

	// Download and run rustup installer with -y for non-interactive
	cmd := exec.Command("sh", "-c", "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install Rust: %w", err)
	}

	// Source cargo env for current session
	cargoEnv := os.ExpandEnv("$HOME/.cargo/env")
	if _, err := os.Stat(cargoEnv); err == nil {
		os.Setenv("PATH", os.ExpandEnv("$HOME/.cargo/bin")+":"+os.Getenv("PATH"))
	}

	fmt.Println("  ✓ Rust installed successfully")
	fmt.Println("  Note: Run 'source ~/.cargo/env' or restart your shell to use Rust")
	return nil
}

// installCargoPgrx installs cargo-pgrx
func installCargoPgrx() error {
	fmt.Println("  → Installing cargo-pgrx...")

	cmd := exec.Command("cargo", "install", "cargo-pgrx", "--locked")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install cargo-pgrx: %w", err)
	}

	fmt.Println("  ✓ cargo-pgrx installed successfully")
	return nil
}

// initPgrx initializes pgrx for the given PostgreSQL version
func initPgrx(pgMajorVersion, pgConfigPath string) error {
	fmt.Printf("  → Initializing pgrx for pg%s...\n", pgMajorVersion)

	arg := fmt.Sprintf("--pg%s=%s", pgMajorVersion, pgConfigPath)
	cmd := exec.Command("cargo", "pgrx", "init", arg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to initialize pgrx: %w", err)
	}

	fmt.Printf("  ✓ pgrx initialized for pg%s\n", pgMajorVersion)
	return nil
}

// getInstallHint returns the install command for system packages based on OS
func getInstallHint(pkg string) string {
	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf("brew install %s", pkg)
	case "linux":
		// Check for common distros
		if _, err := os.Stat("/etc/debian_version"); err == nil {
			return fmt.Sprintf("sudo apt install %s", pkg)
		}
		if _, err := os.Stat("/etc/redhat-release"); err == nil {
			return fmt.Sprintf("sudo dnf install %s", pkg)
		}
		if _, err := os.Stat("/etc/arch-release"); err == nil {
			return fmt.Sprintf("sudo pacman -S %s", pkg)
		}
		return fmt.Sprintf("Install %s using your package manager", pkg)
	default:
		return fmt.Sprintf("Install %s using your package manager", pkg)
	}
}
