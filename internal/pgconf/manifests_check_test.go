package pgconf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// manifestRoots lists where real extension manifests live. Each entry is either
// a directory of extensions (scanned for children carrying a pgbrew.toml) or a
// single extension directory. Every one is optional — these are sibling
// checkouts, not dependencies, so a machine without them simply checks fewer
// manifests instead of failing.
func manifestRoots() (scan []string, single []string) {
	scan = []string{envOr("PG_EXTENSIONS_DIR", "/home/user/pg_extensions/extensions")}
	single = []string{filepath.Join(envOr("EIDOS_DIR", "/home/user/eidos"), "eidos-pg")}
	return scan, single
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestRealExtensionManifests validates the pgbrew manifests shipped by the
// sibling checkouts, when they are available. It is a guard against a manifest
// that parses in isolation but resolves to something nonsensical — declaring
// preloading yet producing no library name, for instance.
//
// Skipped when no checkout is present, so it never fails elsewhere.
func TestRealExtensionManifests(t *testing.T) {
	scanRoots, singleDirs := manifestRoots()

	var dirs []string
	for _, root := range scanRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Logf("skipping %s: not available", root)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, filepath.Join(root, entry.Name()))
			}
		}
	}
	dirs = append(dirs, singleDirs...)

	checked := 0
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, sysdeps.ManifestFile)); err != nil {
			continue
		}
		checked++
		checkManifest(t, dir)
	}

	if checked == 0 {
		t.Skip("no manifests found")
	}
	t.Logf("checked %d manifests", checked)
}

func checkManifest(t *testing.T, dir string) {
	t.Helper()
	ext := filepath.Base(dir)

	m, err := sysdeps.Load(dir)
	if err != nil {
		t.Errorf("%s: manifest does not parse: %v", ext, err)
		return
	}

	plan := Plan{
		Extension:       ext,
		PreloadLibrary:  m.Postgres.PreloadName(ext),
		Settings:        m.Postgres.Settings,
		RestartRequired: m.Postgres.RestartRequired,
	}
	t.Logf("%-14s deps=%d preload=%q settings=%d restart=%v",
		ext, len(m.SystemDependencies), plan.PreloadLibrary, len(plan.Settings), plan.NeedsRestart())

	if m.Postgres.SharedPreloadLibraries && plan.PreloadLibrary == "" {
		t.Errorf("%s: declares preloading but resolves to no library name", ext)
	}
	if m.Postgres.SharedPreloadLibraries && !plan.NeedsRestart() {
		t.Errorf("%s: preloading must imply a restart", ext)
	}

	// A preloaded library name reaches postgresql.conf verbatim and is then
	// dlopen'd. Anything a shell or the GUC parser would mangle is a fault.
	if lib := plan.PreloadLibrary; lib != "" {
		for _, c := range lib {
			ok := c == '_' || c == '-' ||
				(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if !ok {
				t.Errorf("%s: preload library %q contains %q, which is not valid in a library name",
					ext, lib, c)
				break
			}
		}
	}

	// Rendering must produce a config PostgreSQL can parse back.
	if len(plan.Settings) > 0 {
		rendered := RenderDropIn(ext, plan.Settings)
		for key := range plan.Settings {
			if _, ok := FindSetting(rendered, key); !ok {
				t.Errorf("%s: setting %q does not survive rendering", ext, key)
			}
		}
	}
}

// eidos_pg is the case that motivated making the .control file authoritative:
// its Cargo package is `eidos-pg` but the library PostgreSQL loads is
// `eidos_pg`. Preloading the hyphenated name would leave the server unable to
// start, so the manifest states the underscored one explicitly.
func TestEidosPgPreloadsUnderscoredLibrary(t *testing.T) {
	_, singleDirs := manifestRoots()
	dir := singleDirs[0]
	if _, err := os.Stat(filepath.Join(dir, sysdeps.ManifestFile)); err != nil {
		t.Skipf("eidos-pg not available at %s", dir)
	}

	m, err := sysdeps.Load(dir)
	if err != nil {
		t.Fatalf("manifest does not parse: %v", err)
	}
	if !m.Postgres.SharedPreloadLibraries {
		t.Fatal("eidos_pg registers a background worker in _PG_init; it must declare preloading")
	}
	// Passing the directory name deliberately: it is the hyphenated form, so a
	// manifest relying on the default would produce the wrong library here.
	if got := m.Postgres.PreloadName("eidos-pg"); got != "eidos_pg" {
		t.Errorf("preload library is %q, want %q", got, "eidos_pg")
	}
}
