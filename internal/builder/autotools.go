package builder

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// AutotoolsBuilder implements the Builder interface for C extensions with a
// ./configure script, such as PostGIS. Their PGXS Makefiles only exist after
// configure has run, so the PGXS builder cannot see them; this one runs
// configure (and autogen.sh first, for a git checkout), then make.
type AutotoolsBuilder struct{}

func (b *AutotoolsBuilder) Name() string {
	return "autotools"
}

// Detect looks for a configure script (or the autogen.sh that makes one) that
// mentions pg_config, and a control file or template within three levels:
// PostGIS keeps them under extensions/<name>/.
func (b *AutotoolsBuilder) Detect(dir string) bool {
	script := ""
	for _, name := range []string{"configure", "configure.ac"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			script = string(data)
			break
		}
	}
	if !strings.Contains(strings.ToLower(script), "pg_config") && !strings.Contains(script, "PG_CONFIG") {
		return false
	}
	return len(controlNames(dir)) > 0
}

// controlNames lists the extensions a source tree has control files (or
// templates) for, within three levels and outside .git.
func controlNames(dir string) []string {
	seen := map[string]bool{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			if d.Name() == ".git" || strings.Count(rel, string(filepath.Separator)) >= 3 {
				return fs.SkipDir
			}
			return nil
		}
		name := strings.TrimSuffix(d.Name(), ".in")
		if strings.HasSuffix(name, ".control") {
			seen[strings.TrimSuffix(name, ".control")] = true
		}
		return nil
	})
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	// Shortest first: a project's main extension is the stem of its others
	// (postgis before postgis_raster, postgis_topology, ...).
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) < len(names[j])
		}
		return names[i] < names[j]
	})
	return names
}

// GetExtensionName returns the project's main extension: of several control
// files, the shortest name.
func (b *AutotoolsBuilder) GetExtensionName(dir string) (string, error) {
	names := controlNames(dir)
	if len(names) == 0 {
		return "", fmt.Errorf("no .control or .control.in file in %s", dir)
	}
	return names[0], nil
}

// GetVersion reads default_version from the extension's control file. A
// template's version is a configure placeholder, so then it comes from the
// control file configure generated (only there after a build).
func (b *AutotoolsBuilder) GetVersion(dir string) (string, error) {
	name, err := b.GetExtensionName(dir)
	if err != nil {
		return "", err
	}
	var template, generated string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		switch d.Name() {
		case name + ".control":
			generated = p
		case name + ".control.in":
			template = p
		}
		return nil
	})
	for _, p := range []string{generated, template} {
		if p == "" {
			continue
		}
		if v, err := parseControlVersion(p); err == nil && !strings.Contains(v, "@") {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s has a templated version and has not been configured yet", name)
}

// configureArgs builds configure's arguments. --build-var values pass through:
// "--option[=value]" entries are options, KEY=VALUE entries are configure
// variables. pgx always passes the pg_config it builds against, both as
// --with-pgconfig (PostGIS's spelling; configure only warns about an option it
// does not know) and as PG_CONFIG.
//
// CPPFLAGS and LDFLAGS default to PostgreSQL's own (pg_config --cppflags,
// --ldflags), as a PGXS build gets them. A configure script usually takes only
// the server include directory from pg_config, and that is not enough when
// PostgreSQL's headers include something from another prefix: Homebrew's
// PostgreSQL is built with NLS, so c.h includes Homebrew gettext's libintl.h.
// A --build-var CPPFLAGS=... or LDFLAGS=... replaces the default.
//
// --bindir defaults to PostgreSQL's too. Configure's own default is
// /usr/local/bin, and its generated Makefiles then override PGXS's bindir,
// which PGXS uses to link against the server (-bundle_loader
// $(bindir)/postgres on macOS): PostGIS's topology module fails to link with
// "library '/usr/local/bin/postgres' not found". It is also where PGXS
// installs an extension's programs (shp2pgsql, raster2pgsql).
func configureArgs(pgConfig string, vars []string, cppflags, ldflags, bindir string) []string {
	args := []string{"--with-pgconfig=" + pgConfig, "PG_CONFIG=" + pgConfig}
	given := map[string]bool{}
	for _, v := range vars {
		key, _, _ := strings.Cut(v, "=")
		given[key] = true
	}
	if bindir != "" && !given["--bindir"] {
		args = append(args, "--bindir="+bindir)
	}
	if cppflags != "" && !given["CPPFLAGS"] {
		args = append(args, "CPPFLAGS="+cppflags)
	}
	if ldflags != "" && !given["LDFLAGS"] {
		args = append(args, "LDFLAGS="+ldflags)
	}
	return append(args, vars...)
}

// pgConfigFlag returns one pg_config compiler or linker flag value, or ""
// when it cannot be read. An -isysroot is dropped: it names the macOS SDK of
// the machine PostgreSQL was built on (Homebrew's: MacOSX26.sdk), and pinning
// it while the compiler brings the current SDK's libc++ headers breaks every
// C++ file ("<cstddef> tried including <stddef.h> but didn't find libc++'s").
// The compiler picks the right SDK by itself.
func pgConfigFlag(pgConfig, flag string) string {
	out, err := exec.Command(pgConfig, flag).Output()
	if err != nil {
		return ""
	}
	return withoutSysroot(strings.TrimSpace(string(out)))
}

// withoutSysroot removes "-isysroot <path>" (and "-isysroot<path>") from flags.
func withoutSysroot(flags string) string {
	fields := strings.Fields(flags)
	kept := fields[:0]
	for i := 0; i < len(fields); i++ {
		switch {
		case fields[i] == "-isysroot":
			i++ // and its path
		case strings.HasPrefix(fields[i], "-isysroot"):
		default:
			kept = append(kept, fields[i])
		}
	}
	return strings.Join(kept, " ")
}

// configure runs autogen.sh when there is no configure script yet, then
// configure and make.
func (b *AutotoolsBuilder) configure(dir string, opts InstallOptions, buildEnv []string) error {
	pgConfig := opts.PgConfig
	if pgConfig == "" {
		pgConfig = os.Getenv("PG_CONFIG")
	}
	if pgConfig == "" {
		pgConfig = "pg_config"
	}

	if _, err := os.Stat(filepath.Join(dir, "configure")); err != nil {
		if _, err := os.Stat(filepath.Join(dir, "autogen.sh")); err != nil {
			return fmt.Errorf("%s has no configure script and no autogen.sh to make one", dir)
		}
		fmt.Println("Running autogen.sh...")
		if err := runIn(dir, buildEnv, "sh", "./autogen.sh"); err != nil {
			return fmt.Errorf("autogen.sh failed (it needs autoconf, automake and libtool): %w", err)
		}
	}

	fmt.Println("Running configure...")
	args := configureArgs(pgConfig, opts.BuildVars,
		pgConfigFlag(pgConfig, "--cppflags"), pgConfigFlag(pgConfig, "--ldflags"), pgConfigFlag(pgConfig, "--bindir"))
	if err := runIn(dir, buildEnv, "sh", append([]string{"./configure"}, args...)...); err != nil {
		return fmt.Errorf("configure failed: %w", err)
	}
	fmt.Println("Running make...")
	if err := runIn(dir, buildEnv, "make", "-j"+strconv.Itoa(runtime.NumCPU())); err != nil {
		return fmt.Errorf("make failed: %w", err)
	}
	return nil
}

// Install configures, builds and installs into the PostgreSQL installation
// configure found through pg_config.
func (b *AutotoolsBuilder) Install(dir string, opts InstallOptions) error {
	buildEnv := sysdeps.ApplyEnv(os.Environ(), opts.Env)
	if err := b.configure(dir, opts, buildEnv); err != nil {
		return err
	}
	fmt.Println("Running make install...")
	var err error
	if opts.UseSudo {
		preserve := append([]string{"PATH", "HOME"}, sysdeps.EnvKeys(opts.Env)...)
		err = runIn(dir, buildEnv, "sudo", "--preserve-env="+strings.Join(preserve, ","), "make", "install")
	} else {
		err = runIn(dir, buildEnv, "make", "install")
	}
	if err != nil {
		return fmt.Errorf("make install failed: %w", err)
	}
	return nil
}

// Package builds the extension and stages it under a temporary DESTDIR,
// leaving the live PostgreSQL installation untouched. Returns the tree's root.
func (b *AutotoolsBuilder) Package(dir string, opts InstallOptions) (string, error) {
	buildEnv := sysdeps.ApplyEnv(os.Environ(), opts.Env)
	if err := b.configure(dir, opts, buildEnv); err != nil {
		return "", err
	}
	stageRoot, err := os.MkdirTemp("", "pgbrew-stage-*")
	if err != nil {
		return "", err
	}
	if err := runIn(dir, buildEnv, "make", "install", "DESTDIR="+stageRoot); err != nil {
		return "", fmt.Errorf("staged make install failed: %w", err)
	}
	return stageRoot, nil
}

// NeedsSharedPreload checks the extension's control file (or template).
func (b *AutotoolsBuilder) NeedsSharedPreload(dir string) bool {
	name, err := b.GetExtensionName(dir)
	if err != nil {
		return false
	}
	found := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.Name() == name+".control" || d.Name() == name+".control.in" {
			if data, err := os.ReadFile(p); err == nil && strings.Contains(strings.ToLower(string(data)), "shared_preload") {
				found = true
			}
		}
		return nil
	})
	return found
}
