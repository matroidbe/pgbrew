package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matroidbe/pgbrew/internal/builder"
	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/matroidbe/pgbrew/internal/github"
	"github.com/matroidbe/pgbrew/internal/sysdeps"
	"github.com/spf13/cobra"

	// Register builders
	_ "github.com/matroidbe/pgbrew/internal/builder"
)

var useSudo bool
var features []string

var installCmd = &cobra.Command{
	Use:   "install <source>",
	Short: "Install a PostgreSQL extension",
	Long: `Install a PostgreSQL extension from GitHub or a local directory.

Supported extension types:
  - pgrx (Rust): Projects with Cargo.toml containing pgrx dependency
  - pgxs (C):    Projects with Makefile using PGXS and a .control file

Examples:
  pgx install github.com/pgvector/pgvector
  pgx install github.com/supabase/pg_graphql
  pgx install github.com/supabase/pg_graphql@v1.5.0
  pgx install github.com/user/repo/extensions/myext@main
  pgx install ./pg_hello
  pgx install /path/to/extension
  pgx install --sudo github.com/pgvector/pgvector  # Install with sudo for system PostgreSQL
  pgx install --features my_feature ./my_ext       # Enable additional Cargo features (pgrx)
  pgx install --bottle pg_solid-0.2.0-pg16-linux-amd64.tar.gz   # Install a prebuilt artifact
  pgx install --bottle https://example.com/bottles/pg_solid-0.2.0-pg16-linux-amd64.tar.gz`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstall,
}

var bottleSource string

func init() {
	installCmd.Flags().StringVar(&bottleSource, "bottle", "", "Install from a prebuilt bottle (file path or http(s) URL) instead of building")
	installCmd.Flags().BoolVar(&useSudo, "sudo", false, "Use sudo for installation (needed for system PostgreSQL)")
	installCmd.Flags().StringSliceVar(&features, "features", nil, "Additional Cargo features to enable (pgrx only, comma-separated)")
	installCmd.Flags().BoolVar(&installDeps, "install-deps", false, "Install missing system dependencies with the platform package manager")
	installCmd.Flags().StringVar(&depsVia, "deps-via", "", depsViaHelp)
	installCmd.Flags().BoolVar(&skipDepChecks, "skip-dep-check", false, "Skip the system dependency check")
	installCmd.Flags().BoolVar(&skipToolchainCheck, "skip-toolchain-check", false, "Skip the cargo configuration toolchain check")
	installCmd.Flags().BoolVar(&configureServer, "configure", false, "Write the PostgreSQL configuration the extension declares (conf.d drop-in)")
}

func runInstall(cmd *cobra.Command, args []string) error {
	// A bottle is a prebuilt artifact: no source, no toolchain, no build. It
	// carries its own identity, so no source argument is needed.
	if bottleSource != "" {
		if len(args) > 0 {
			return fmt.Errorf("--bottle installs a prebuilt artifact; do not also pass a source")
		}
		return installFromBottle(bottleSource)
	}

	if len(args) != 1 {
		return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
	}
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

	// Detect the appropriate builder for this project
	b, err := builder.DetectBuilder(extDir)
	if err != nil {
		return err
	}

	fmt.Printf("Detected %s project\n", b.Name())

	// Get extension name
	extName, err := b.GetExtensionName(extDir)
	if err != nil {
		return fmt.Errorf("failed to get extension name: %w", err)
	}

	// Reconcile the tools this project's own cargo configuration asks for with
	// what this machine has. These requirements are invisible to a generic
	// toolchain check, and an unmet one fails the build within seconds of
	// starting it. Returns the overrides that retarget settings written for a
	// different operating system.
	var toolchainEnv []string
	if b.Name() == "pgrx" {
		toolchainEnv, err = resolveCargoToolchain(extDir)
		if err != nil {
			return err
		}
	}

	// Check the extension's declared native dependencies before building, so a
	// missing library is reported as such instead of as a compiler or linker
	// error. Returns the environment that tells the build where they are.
	depEnv, err := resolveSystemDeps(extDir)
	if err != nil {
		return err
	}

	fmt.Printf("Building %s...\n", extName)

	// Build and install
	pgConfig := getPgConfigPath()
	opts := builder.InstallOptions{
		PgConfig: pgConfig,
		UseSudo:  useSudo,
		Features: features,
		// Declared dependencies come last so an extension's own manifest wins
		// over pgbrew's guess at where a pinned tool should point.
		Env: append(toolchainEnv, depEnv...),
	}
	if err := b.Install(extDir, opts); err != nil {
		return fmt.Errorf("failed to install extension: %w", err)
	}

	// Set sudo mode for cellar operations
	cellar.SetUseSudo(useSudo)

	// Get version
	version, _ := b.GetVersion(extDir)
	if version == "" {
		version = "unknown"
	}

	// Get PostgreSQL version
	pgVersion := getPgVersion()

	// Record installation
	entry := cellar.Entry{
		Name:        extName,
		Version:     version,
		Source:      source,
		PgVersion:   pgVersion,
		BuildSystem: b.Name(),
	}
	if err := cellar.Add(entry); err != nil {
		return fmt.Errorf("failed to record installation: %w", err)
	}

	fmt.Printf("\n✓ Successfully installed %s %s\n", extName, version)
	fmt.Printf("  Run: CREATE EXTENSION %s;\n", extName)

	// Apply (or report) the PostgreSQL configuration the extension declares.
	// A declaration is authoritative; the source heuristic is only a fallback
	// for extensions that have not declared anything.
	manifest, mErr := sysdeps.Load(extDir)
	declared := mErr == nil && !manifest.Postgres.IsZero()
	if declared {
		if err := handlePostgresConfig(planFromManifest(extName, manifest.Postgres)); err != nil {
			return err
		}
	} else {
		// No declaration: fall back to inferring from the source. Note this is
		// version-independent — a library registering a background worker in
		// _PG_init must be preloaded on every PostgreSQL version.
		preloadWarning(extName, b.NeedsSharedPreload(extDir))
	}

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
