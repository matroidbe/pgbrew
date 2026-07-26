package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/matroidbe/pgbrew/internal/bottle"
	"github.com/matroidbe/pgbrew/internal/builder"
	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/matroidbe/pgbrew/internal/sysdeps"
	"github.com/spf13/cobra"
)

var bottleOutDir string

var bottleCmd = &cobra.Command{
	Use:   "bottle <path>",
	Short: "Build a prebuilt artifact from an extension",
	Long: `Builds an extension and packages the result into a bottle: a tarball of the
installable files plus a manifest recording what they are for.

Installing a bottle needs no compiler, no Rust toolchain and none of the
extension's build-time libraries — only a matching PostgreSQL major version,
OS and architecture.

  pgx bottle ./pg_solid
  pgx bottle ./pg_solid -o dist/

The result can be published (a GitHub release, an object store, a file share)
and installed with:

  pgx install --bottle <file-or-url>`,
	Args: cobra.ExactArgs(1),
	RunE: runBottle,
}

func init() {
	bottleCmd.Flags().StringVarP(&bottleOutDir, "out-dir", "o", ".", "Directory to write the bottle into")
	bottleCmd.Flags().StringSliceVar(&features, "features", nil, "Additional Cargo features to enable (pgrx only)")
	bottleCmd.Flags().BoolVar(&installDeps, "install-deps", false, "Install missing system dependencies")
	bottleCmd.Flags().StringVar(&depsVia, "deps-via", "", depsViaHelp)
	bottleCmd.Flags().BoolVar(&skipDepChecks, "skip-dep-check", false, "Skip the system dependency check")
	bottleCmd.Flags().BoolVar(&skipToolchainCheck, "skip-toolchain-check", false, "Skip the cargo configuration toolchain check")
}

func runBottle(cmd *cobra.Command, args []string) error {
	extDir, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	if _, err := os.Stat(extDir); err != nil {
		return fmt.Errorf("directory not found: %s", extDir)
	}

	b, err := builder.DetectBuilder(extDir)
	if err != nil {
		return err
	}
	fmt.Printf("Detected %s project\n", b.Name())

	extName, err := b.GetExtensionName(extDir)
	if err != nil {
		return fmt.Errorf("failed to get extension name: %w", err)
	}
	version, _ := b.GetVersion(extDir)
	if version == "" {
		version = "unknown"
	}

	// Building a bottle is building the extension, so it needs the same
	// prerequisites an install does.
	if b.Name() == "pgrx" {
		if err := checkCargoToolchain(extDir); err != nil {
			return err
		}
	}
	depEnv, err := resolveSystemDeps(extDir)
	if err != nil {
		return err
	}

	pgConfig := getPgConfigPath()
	pgMajor := getPgVersion()
	if pgMajor == "" {
		return fmt.Errorf("could not determine the PostgreSQL major version from %s", pgConfig)
	}

	fmt.Printf("Building %s %s for pg%s...\n", extName, version, pgMajor)
	stageRoot, err := b.Package(extDir, builder.InstallOptions{
		PgConfig: pgConfig,
		Features: features,
		Env:      depEnv,
	})
	if err != nil {
		return err
	}

	pkgLibDir, err := pgConfigValue(pgConfig, "--pkglibdir")
	if err != nil {
		return err
	}
	shareDir, err := pgConfigValue(pgConfig, "--sharedir")
	if err != nil {
		return err
	}

	staged, err := bottle.FromStagingDir(stageRoot, pkgLibDir, shareDir)
	if err != nil {
		return err
	}
	files := bottle.FilesFor(extName, staged)
	if len(files) == 0 {
		return fmt.Errorf("nothing belonging to %s was found in the staged tree at %s", extName, stageRoot)
	}

	manifest := bottle.Manifest{
		Name:        extName,
		Version:     version,
		PgMajor:     pgMajor,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		BuildSystem: b.Name(),
	}

	// Carry the declared server configuration inside the bottle. A bottle
	// install has no source tree to read it from, and an extension whose
	// library is never preloaded silently does nothing.
	if src, err := sysdeps.Load(extDir); err == nil && !src.Postgres.IsZero() {
		manifest.Postgres = &bottle.PostgresConfig{
			SharedPreloadLibraries: src.Postgres.SharedPreloadLibraries,
			Library:                src.Postgres.Library,
			Settings:               src.Postgres.Settings,
			RestartRequired:        src.Postgres.RestartRequired,
		}
	}

	if err := os.MkdirAll(bottleOutDir, 0o755); err != nil {
		return err
	}
	outPath := filepath.Join(bottleOutDir, manifest.Filename())

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	if err := bottle.Create(out, manifest, files); err != nil {
		out.Close()
		os.Remove(outPath)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	info, _ := os.Stat(outPath)
	fmt.Printf("\n✓ Wrote %s\n", outPath)
	fmt.Printf("  %s %s for %s, %d file(s)", manifest.Name, manifest.Version, manifest.Target(), len(files))
	if info != nil {
		fmt.Printf(", %.1f MiB", float64(info.Size())/(1<<20))
	}
	fmt.Println()
	for name := range files {
		fmt.Printf("    %s\n", name)
	}
	if src, err := sysdeps.Load(extDir); err == nil {
		if summary := describePlanForBottle(src.Postgres, extName); summary != "" {
			fmt.Print(summary)
		}
	}
	return nil
}

// installFromBottle installs a prebuilt bottle instead of building from source.
func installFromBottle(source string) error {
	b, err := loadBottle(source)
	if err != nil {
		return err
	}

	pgConfig := getPgConfigPath()
	pgMajor := getPgVersion()
	if !b.Manifest.Matches(pgMajor, runtime.GOOS, runtime.GOARCH) {
		return fmt.Errorf(
			"this bottle is built for %s, but this host is pg%s-%s-%s.\n"+
				"A PostgreSQL extension is loaded into the server process, so the major\n"+
				"version, OS and architecture all have to match. Build from source instead.",
			b.Manifest.Target(), pgMajor, runtime.GOOS, runtime.GOARCH)
	}

	pkgLibDir, err := pgConfigValue(pgConfig, "--pkglibdir")
	if err != nil {
		return err
	}
	shareDir, err := pgConfigValue(pgConfig, "--sharedir")
	if err != nil {
		return err
	}

	fmt.Printf("Installing %s %s from a bottle (%s)\n",
		b.Manifest.Name, b.Manifest.Version, b.Manifest.Target())

	written, err := b.Install(pkgLibDir, shareDir, useSudo)
	if err != nil {
		return err
	}
	for _, path := range written {
		fmt.Printf("  %s\n", path)
	}

	cellar.SetUseSudo(useSudo)
	entry := cellar.Entry{
		Name:        b.Manifest.Name,
		Version:     b.Manifest.Version,
		Source:      source,
		PgVersion:   pgMajor,
		BuildSystem: b.Manifest.BuildSystem,
	}
	if err := cellar.Add(entry); err != nil {
		return fmt.Errorf("failed to record installation: %w", err)
	}

	fmt.Printf("\n✓ Successfully installed %s %s\n", b.Manifest.Name, b.Manifest.Version)
	fmt.Printf("  Run: CREATE EXTENSION %s;\n", b.Manifest.Name)

	return handlePostgresConfig(planFromBottle(b.Manifest))
}

// loadBottle reads a bottle from a local path or an http(s) URL.
func loadBottle(source string) (*bottle.Bottle, error) {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return bottle.OpenFile(source)
	}

	fmt.Printf("Downloading %s\n", source)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(source)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", source, resp.Status)
	}
	// The whole bottle is verified before anything is written, so buffering it
	// is deliberate: a partial download must never reach a PostgreSQL directory.
	return bottle.Open(io.LimitReader(resp.Body, 1<<30), source)
}

// pgConfigValue queries pg_config for a path.
func pgConfigValue(pgConfig, flag string) (string, error) {
	out := getCommandOutput(pgConfig, flag)
	value := strings.TrimSpace(out)
	if value == "" {
		return "", fmt.Errorf("could not read %s from %s", flag, pgConfig)
	}
	return value, nil
}
