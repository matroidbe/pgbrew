package pgrx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// NeedsSharedPreload checks if the extension uses background workers
// which require shared_preload_libraries configuration.
func NeedsSharedPreload(dir string) bool {
	return checkDirForBgWorker(filepath.Join(dir, "src"))
}

func checkDirForBgWorker(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			// Check for parallel_worker or bgworker directory names
			name := strings.ToLower(entry.Name())
			if strings.Contains(name, "worker") || strings.Contains(name, "bgw") {
				return true
			}
			// Recurse into subdirectories
			if checkDirForBgWorker(fullPath) {
				return true
			}
		} else if strings.HasSuffix(entry.Name(), ".rs") {
			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}

			// Look for background worker indicators in pgrx
			contentStr := string(content)
			if strings.Contains(contentStr, "BackgroundWorker") ||
				strings.Contains(contentStr, "bgworker") ||
				strings.Contains(contentStr, "RegisterDynamicBackgroundWorker") ||
				strings.Contains(contentStr, "SharedMemoryInit") ||
				strings.Contains(contentStr, "max_parallel_workers") ||
				strings.Contains(contentStr, "ParallelWorker") {
				return true
			}
		}
	}

	return false
}

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

	content := string(data)

	// Check if using workspace dependency (pgrx.workspace = true)
	if strings.Contains(content, "pgrx.workspace") {
		// Look for workspace Cargo.toml in parent directories
		parentDir := filepath.Dir(dir)
		for parentDir != "/" && parentDir != "." {
			workspaceCargo := filepath.Join(parentDir, "Cargo.toml")
			if workspaceData, err := os.ReadFile(workspaceCargo); err == nil {
				workspaceContent := string(workspaceData)
				// Check if this is a workspace with pgrx dependency
				if strings.Contains(workspaceContent, "[workspace") {
					version := extractPgrxVersion(workspaceContent)
					if version != "" {
						return version, nil
					}
				}
			}
			parentDir = filepath.Dir(parentDir)
		}
	}

	// Try to extract version directly from this Cargo.toml
	version := extractPgrxVersion(content)
	if version != "" {
		return version, nil
	}

	return "", fmt.Errorf("could not find pgrx version in Cargo.toml")
}

// extractPgrxVersion extracts pgrx version from Cargo.toml content
func extractPgrxVersion(content string) string {
	// Try simple format: pgrx = "0.14.0" or pgrx = "=0.14.0"
	re := regexp.MustCompile(`pgrx\s*=\s*"=?([0-9][^"]*)"`)
	matches := re.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1]
	}

	// Try table format: pgrx = { version = "0.14.0", ... } or { version = "=0.14.0", ... }
	re = regexp.MustCompile(`pgrx\s*=\s*\{[^}]*version\s*=\s*"=?([0-9][^"]*)"`)
	matches = re.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1]
	}

	return ""
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

	// Check if installed version matches required (including partial version matches)
	if err == nil {
		if installed == requiredVersion {
			return nil // Exact match
		}
		// Check partial version match (e.g., "0.12" matches "0.12.9")
		if strings.HasPrefix(installed, requiredVersion+".") || strings.HasPrefix(installed, requiredVersion) && !strings.Contains(requiredVersion, ".") {
			return nil // Partial match (major or major.minor)
		}
		// Check major.minor match
		installedParts := strings.Split(installed, ".")
		requiredParts := strings.Split(requiredVersion, ".")
		if len(installedParts) >= 2 && len(requiredParts) >= 2 {
			if installedParts[0] == requiredParts[0] && installedParts[1] == requiredParts[1] {
				return nil // Same major.minor version
			}
		}
	}

	// Normalize version for cargo install - if it's a partial version like "0.12",
	// we need to use a caret range "^0.12" to let cargo find the latest patch
	installVersion := requiredVersion
	versionParts := strings.Split(requiredVersion, ".")
	if len(versionParts) < 3 {
		// Partial version, use caret range
		installVersion = "^" + requiredVersion
	}

	fmt.Printf("Installing cargo-pgrx %s (current: %s)...\n", requiredVersion, installed)
	cmd := exec.Command("cargo", "install", "cargo-pgrx", "--version", installVersion, "--locked")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install cargo-pgrx %s: %w", requiredVersion, err)
	}

	return nil
}

// getPgMajorVersion returns the PostgreSQL major version from pg_config.
func getPgMajorVersion(pgConfig string) (string, error) {
	cmd := exec.Command(pgConfig, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output: "PostgreSQL 16.0" or "PostgreSQL 16.4"
	parts := strings.Fields(string(output))
	if len(parts) < 2 {
		return "", fmt.Errorf("could not parse pg_config version")
	}
	verStr := parts[1]
	if idx := strings.Index(verStr, "."); idx > 0 {
		return verStr[:idx], nil
	}
	return verStr, nil
}

// isPgrxInitialized checks if pgrx is initialized for the given PostgreSQL version.
func isPgrxInitialized(pgMajorVersion string) bool {
	cmd := exec.Command("cargo", "pgrx", "info", "pg-config", "pg"+pgMajorVersion)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	result := strings.TrimSpace(string(output))
	return result != "" && !strings.Contains(result, "not managed")
}

// EnsurePgrxInit ensures pgrx is initialized for the current PostgreSQL version.
func EnsurePgrxInit(pgConfig string) error {
	pgMajorVersion, err := getPgMajorVersion(pgConfig)
	if err != nil {
		return fmt.Errorf("could not determine PostgreSQL version: %w", err)
	}

	if isPgrxInitialized(pgMajorVersion) {
		return nil
	}

	fmt.Printf("Initializing pgrx for pg%s...\n", pgMajorVersion)
	arg := fmt.Sprintf("--pg%s=%s", pgMajorVersion, pgConfig)
	cmd := exec.Command("cargo", "pgrx", "init", arg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to initialize pgrx for pg%s: %w", pgMajorVersion, err)
	}

	fmt.Printf("pgrx initialized for pg%s\n", pgMajorVersion)
	return nil
}

// hasMakefileWithInstall checks if the directory has a custom Makefile
// with an install target (but not a PGXS makefile).
func hasMakefileWithInstall(dir string) bool {
	content, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return false
	}
	s := string(content)
	// Has install target but is NOT a PGXS makefile
	return strings.Contains(s, "install:") && !strings.Contains(s, "PGXS")
}

// InstallOptions contains options for the Install function.
type InstallOptions struct {
	PgConfig string   // Path to pg_config
	UseSudo  bool     // Use sudo for installation
	Features []string // Additional Cargo features to enable
	Env      []string // Extra KEY=VALUE pairs for the build (system dependency locations)
}

// resolveTargetDir finds the Cargo target directory for the project.
// For workspace members, the target dir is at the workspace root.
func resolveTargetDir(dir string) string {
	// Ask cargo for the target directory
	cmd := exec.Command("cargo", "metadata", "--format-version=1", "--no-deps")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err == nil {
		// Extract target_directory from JSON (simple string search to avoid json dependency)
		s := string(output)
		marker := `"target_directory":"`
		if idx := strings.Index(s, marker); idx >= 0 {
			rest := s[idx+len(marker):]
			if end := strings.Index(rest, `"`); end >= 0 {
				return rest[:end]
			}
		}
	}
	// Fallback: target/ in the given directory
	return filepath.Join(dir, "target")
}

// findDepsDir returns the path to target/<profile>/deps if it exists.
func findDepsDir(dir, profile string) string {
	targetDir := resolveTargetDir(dir)
	depsDir := filepath.Join(targetDir, profile, "deps")
	if info, err := os.Stat(depsDir); err == nil && info.IsDir() {
		return depsDir
	}
	return ""
}

// withLDPath returns env with depsDir prepended to LD_LIBRARY_PATH.
func withLDPath(env []string, depsDir string) []string {
	ldPath := depsDir
	if existing := os.Getenv("LD_LIBRARY_PATH"); existing != "" {
		ldPath = depsDir + ":" + existing
	}
	return updateEnv(env, "LD_LIBRARY_PATH", ldPath)
}

// updateEnv replaces or appends a KEY=VALUE pair in an environment slice.
func updateEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// getPkgLibDir returns the PostgreSQL pkglibdir (where .so files live).
func getPkgLibDir(pgConfig string) (string, error) {
	cmd := exec.Command(pgConfig, "--pkglibdir")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// installCompanionLibs finds shared libraries in depsDir that the extension
// .so depends on and copies them to the PostgreSQL lib directory so they
// are found at runtime. Returns the number of libraries installed.
func installCompanionLibs(extName, depsDir, pgLibDir string, useSudo bool) int {
	extSo := filepath.Join(pgLibDir, extName+".so")
	if _, err := os.Stat(extSo); err != nil {
		return 0
	}

	// Run ldd on the installed extension .so to find its dependencies
	cmd := exec.Command("ldd", extSo)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	installed := 0

	// Parse ldd output for "not found" libraries
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "not found") {
			continue
		}
		// Format: "libxgboost.so => not found"
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		libName := parts[0]

		// Check if the library exists in our deps directory
		srcPath := filepath.Join(depsDir, libName)
		if _, err := os.Stat(srcPath); err != nil {
			continue
		}

		dstPath := filepath.Join(pgLibDir, libName)
		fmt.Printf("  Installing companion lib: %s\n", libName)

		if useSudo {
			cp := exec.Command("sudo", "cp", srcPath, dstPath)
			cp.Stdout = os.Stdout
			cp.Stderr = os.Stderr
			if err := cp.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to copy %s: %v\n", libName, err)
				continue
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to read %s: %v\n", libName, err)
				continue
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to write %s: %v\n", libName, err)
				continue
			}
		}
		installed++
	}

	return installed
}

// ensureLdconfig registers the PostgreSQL lib directory with the system's
// dynamic linker cache so companion shared libraries are found at runtime.
func ensureLdconfig(pgLibDir string, useSudo bool) {
	confPath := "/etc/ld.so.conf.d/postgresql.conf"

	// Check if already configured
	if data, err := os.ReadFile(confPath); err == nil {
		if strings.Contains(string(data), pgLibDir) {
			// Already registered, just refresh the cache
			runLdconfig(useSudo)
			return
		}
	}

	// Write the config file
	fmt.Printf("  Registering %s with ldconfig\n", pgLibDir)
	if useSudo {
		// Use tee to write via sudo
		cmd := exec.Command("sudo", "bash", "-c", fmt.Sprintf("echo '%s' > %s", pgLibDir, confPath))
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to create %s: %v\n", confPath, err)
			return
		}
	} else {
		if err := os.WriteFile(confPath, []byte(pgLibDir+"\n"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to create %s: %v\n", confPath, err)
			return
		}
	}

	runLdconfig(useSudo)
}

// runLdconfig refreshes the system's shared library cache.
func runLdconfig(useSudo bool) {
	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "ldconfig")
	} else {
		cmd = exec.Command("ldconfig")
	}
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: ldconfig failed: %v\n", err)
	}
}

// Install builds and installs the extension using cargo pgrx install.
func Install(dir string, opts InstallOptions) error {
	// Determine pg_config path
	pgConfig := opts.PgConfig
	if pgConfig == "" {
		pgConfig = os.Getenv("PG_CONFIG")
	}
	if pgConfig == "" {
		pgConfig = "pg_config"
	}

	// Check if custom Makefile exists with install target
	// This allows pgrx projects to have custom build steps (e.g., venv setup)
	// The Makefile is expected to handle sudo internally based on PG_CONFIG path detection
	if hasMakefileWithInstall(dir) {
		fmt.Println("==> Found Makefile with install target, using make...")
		cmd := exec.Command("make", "install", "PG_CONFIG="+pgConfig)
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = sysdeps.ApplyEnv(os.Environ(), opts.Env)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("make install failed: %w", err)
		}
		return nil
	}

	// Check pgrx version compatibility
	requiredVersion, err := GetPgrxVersion(dir)
	if err == nil && requiredVersion != "" {
		if err := EnsurePgrxVersion(requiredVersion); err != nil {
			return err
		}
	}

	// Ensure pgrx is initialized for this PostgreSQL version
	if err := EnsurePgrxInit(pgConfig); err != nil {
		return err
	}

	// Get PostgreSQL major version
	pgMajorVersion, err := getPgMajorVersion(pgConfig)
	if err != nil {
		return fmt.Errorf("could not determine PostgreSQL version: %w", err)
	}

	// Build command args
	args := []string{"pgrx", "install", "--release"}

	// Pass pg_config path
	args = append(args, "--pg-config", pgConfig)

	// Disable default features and specify only the correct pg version feature
	pgFeature := "pg" + pgMajorVersion
	featureList := pgFeature
	if len(opts.Features) > 0 {
		featureList += "," + strings.Join(opts.Features, ",")
	}
	args = append(args, "--no-default-features", "--features", featureList)

	// Add sudo flag if requested
	if opts.UseSudo {
		args = append(args, "--sudo")
	}

	// Run cargo pgrx install
	cmd := exec.Command("cargo", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start from the current environment plus any system-dependency locations
	// pgbrew discovered, so the extension's build script can find them.
	env := sysdeps.ApplyEnv(os.Environ(), opts.Env)

	// Add target deps directories to LD_LIBRARY_PATH so the pgrx_embed binary
	// can find companion shared libraries (e.g., libxgboost.so) during SQL generation.
	// We include both release/deps (main build) and debug/deps (embed binary build).
	var ldPaths []string
	for _, profile := range []string{"release", "debug"} {
		if depsDir := findDepsDir(dir, profile); depsDir != "" {
			ldPaths = append(ldPaths, depsDir)
		}
	}
	if len(ldPaths) > 0 {
		env = withLDPath(env, strings.Join(ldPaths, ":"))
	}
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cargo pgrx install failed: %w", err)
	}

	// Copy companion shared libraries to the PG lib directory so they
	// are found at runtime (PG's pkglibdir is in the extension load path).
	extName, _ := GetExtensionName(dir)
	pgLibDir, libErr := getPkgLibDir(pgConfig)
	releaseDeps := findDepsDir(dir, "release")
	if extName != "" && libErr == nil && releaseDeps != "" {
		installed := installCompanionLibs(extName, releaseDeps, pgLibDir, opts.UseSudo)
		if installed > 0 {
			ensureLdconfig(pgLibDir, opts.UseSudo)
		}
	}

	return nil
}

// Package builds the extension and stages it into a directory tree, without
// installing it into the live PostgreSQL.
//
// This is what a bottle is made from: `cargo pgrx package` produces a tree
// mirroring the target's absolute install paths, which can be packaged and
// shipped to machines that will never run a compiler.
//
// Returns the root of the staged tree.
func Package(dir string, opts InstallOptions) (string, error) {
	pgConfig := opts.PgConfig
	if pgConfig == "" {
		pgConfig = os.Getenv("PG_CONFIG")
	}
	if pgConfig == "" {
		pgConfig = "pg_config"
	}

	requiredVersion, err := GetPgrxVersion(dir)
	if err == nil && requiredVersion != "" {
		if err := EnsurePgrxVersion(requiredVersion); err != nil {
			return "", err
		}
	}
	if err := EnsurePgrxInit(pgConfig); err != nil {
		return "", err
	}

	pgMajorVersion, err := getPgMajorVersion(pgConfig)
	if err != nil {
		return "", fmt.Errorf("could not determine PostgreSQL version: %w", err)
	}

	pgFeature := "pg" + pgMajorVersion
	featureList := pgFeature
	if len(opts.Features) > 0 {
		featureList += "," + strings.Join(opts.Features, ",")
	}

	args := []string{
		"pgrx", "package",
		"--pg-config", pgConfig,
		"--no-default-features",
		"--features", featureList,
	}

	cmd := exec.Command("cargo", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = sysdeps.ApplyEnv(os.Environ(), opts.Env)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cargo pgrx package failed: %w", err)
	}

	extName, err := GetExtensionName(dir)
	if err != nil {
		return "", err
	}

	// cargo-pgrx stages into <target>/release/<extension>-pg<major>.
	stageRoot := filepath.Join(resolveTargetDir(dir), "release",
		fmt.Sprintf("%s-pg%s", extName, pgMajorVersion))
	if info, err := os.Stat(stageRoot); err != nil || !info.IsDir() {
		return "", fmt.Errorf("expected cargo pgrx to stage into %s, but it is not there", stageRoot)
	}
	return stageRoot, nil
}
