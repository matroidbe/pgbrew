package builder

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// CMakeBuilder implements the Builder interface for C extensions built with
// CMake, such as TimescaleDB. CMake configures into a build directory, so the
// project's own install rules (which take their paths from pg_config) do the
// placing, exactly as a PGXS `make install` does.
type CMakeBuilder struct{}

// cmakeBuildDir is where pgx configures a CMake project, inside its source
// tree. Kept apart from the conventional ./build so a developer's own build
// directory is never reused with pgx's settings, or the other way round.
const cmakeBuildDir = "build-pgx"

func (b *CMakeBuilder) Name() string {
	return "cmake"
}

// Detect checks for a CMakeLists.txt that builds against pg_config, with a
// control file (or the template CMake generates one from) beside it.
func (b *CMakeBuilder) Detect(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "CMakeLists.txt"))
	if err != nil || !strings.Contains(strings.ToLower(string(data)), "pg_config") {
		return false
	}
	_, err = controlSource(dir)
	return err == nil
}

// controlSource finds the project's control file, or the .control.in template
// CMake configures it from.
func controlSource(dir string) (string, error) {
	for _, pattern := range []string{"*.control", "*.control.in"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("no .control or .control.in file in %s", dir)
}

// GetExtensionName takes the name from the control file or its template.
func (b *CMakeBuilder) GetExtensionName(dir string) (string, error) {
	src, err := controlSource(dir)
	if err != nil {
		return filepath.Base(dir), nil
	}
	base := filepath.Base(src)
	return strings.TrimSuffix(strings.TrimSuffix(base, ".in"), ".control"), nil
}

// GetVersion reads default_version from the control file. A template's
// version is a CMake placeholder (TimescaleDB: '@PROJECT_VERSION_MOD@'), so
// then it is read from the control file the configure step generated, which
// only exists once the project has been built.
func (b *CMakeBuilder) GetVersion(dir string) (string, error) {
	src, err := controlSource(dir)
	if err != nil {
		return "", err
	}
	if version, err := parseControlVersion(src); err == nil && !strings.Contains(version, "@") {
		return version, nil
	}

	name, _ := b.GetExtensionName(dir)
	generated := ""
	filepath.WalkDir(filepath.Join(dir, cmakeBuildDir), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == name+".control" {
			generated = p
			return fs.SkipAll
		}
		return nil
	})
	if generated == "" {
		return "", fmt.Errorf("%s has a templated version and has not been configured yet", src)
	}
	return parseControlVersion(generated)
}

// configure runs the CMake configure and build steps.
func (b *CMakeBuilder) configure(dir string, opts InstallOptions, buildEnv []string) error {
	pgConfig := opts.PgConfig
	if pgConfig == "" {
		pgConfig = os.Getenv("PG_CONFIG")
	}
	if pgConfig == "" {
		pgConfig = "pg_config"
	}
	if _, err := exec.LookPath("cmake"); err != nil {
		return fmt.Errorf("cmake is required to build this extension and is not on PATH")
	}

	// Start from an empty build directory: a cache left by an earlier run
	// would keep its old settings and ignore new -D values.
	buildDir := filepath.Join(dir, cmakeBuildDir)
	if err := os.RemoveAll(buildDir); err != nil {
		return err
	}

	configureArgs := []string{"-S", dir, "-B", buildDir,
		"-DPG_CONFIG=" + pgConfig, "-DCMAKE_BUILD_TYPE=Release"}
	for _, v := range opts.BuildVars {
		configureArgs = append(configureArgs, "-D"+v)
	}

	fmt.Println("Running cmake...")
	if err := runIn(dir, buildEnv, "cmake", configureArgs...); err != nil {
		return fmt.Errorf("cmake configure failed: %w", err)
	}
	fmt.Println("Running cmake --build...")
	if err := runIn(dir, buildEnv, "cmake", "--build", buildDir, "--parallel"); err != nil {
		return fmt.Errorf("cmake build failed: %w", err)
	}
	return nil
}

// Install configures, builds and installs the extension into the PostgreSQL
// installation pg_config describes.
func (b *CMakeBuilder) Install(dir string, opts InstallOptions) error {
	buildEnv := sysdeps.ApplyEnv(os.Environ(), opts.Env)
	if err := b.configure(dir, opts, buildEnv); err != nil {
		return err
	}

	fmt.Println("Running cmake --install...")
	installArgs := []string{"--install", filepath.Join(dir, cmakeBuildDir)}
	var err error
	if opts.UseSudo {
		preserve := append([]string{"PATH", "HOME"}, sysdeps.EnvKeys(opts.Env)...)
		err = runIn(dir, buildEnv, "sudo", append([]string{"--preserve-env=" + strings.Join(preserve, ","), "cmake"}, installArgs...)...)
	} else {
		err = runIn(dir, buildEnv, "cmake", installArgs...)
	}
	if err != nil {
		return fmt.Errorf("cmake install failed: %w", err)
	}
	return nil
}

// Package builds the extension and stages it under a temporary DESTDIR,
// leaving the live PostgreSQL installation untouched. Returns the tree's root.
func (b *CMakeBuilder) Package(dir string, opts InstallOptions) (string, error) {
	buildEnv := sysdeps.ApplyEnv(os.Environ(), opts.Env)
	if err := b.configure(dir, opts, buildEnv); err != nil {
		return "", err
	}

	stageRoot, err := os.MkdirTemp("", "pgbrew-stage-*")
	if err != nil {
		return "", err
	}
	// cmake --install honours DESTDIR from the environment, the same staging
	// convention as `make install DESTDIR=...`.
	stageEnv := append(buildEnv, "DESTDIR="+stageRoot)
	if err := runIn(dir, stageEnv, "cmake", "--install", filepath.Join(dir, cmakeBuildDir)); err != nil {
		return "", fmt.Errorf("staged cmake install failed: %w", err)
	}
	return stageRoot, nil
}

// NeedsSharedPreload checks the control file and the C sources for signs that
// the library must be loaded at server start. A CMake project's control file
// rarely says so, while registering a background worker in _PG_init always
// means it.
func (b *CMakeBuilder) NeedsSharedPreload(dir string) bool {
	if src, err := controlSource(dir); err == nil {
		if data, err := os.ReadFile(src); err == nil && strings.Contains(strings.ToLower(string(data)), "shared_preload") {
			return true
		}
	}
	found := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return fs.SkipAll
		}
		if d.IsDir() && (d.Name() == cmakeBuildDir || d.Name() == "test" || d.Name() == ".git") {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".c") {
			if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), "RegisterBackgroundWorker(") {
				found = true
			}
		}
		return nil
	})
	return found
}

// runIn runs a command in dir with env, streaming its output.
func runIn(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
