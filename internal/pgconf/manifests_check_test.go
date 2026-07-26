package pgconf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// TestRealExtensionManifests validates the pgbrew manifests shipped by a
// checkout of pg_extensions, when one is available. It is a guard against a
// manifest that parses in isolation but resolves to something nonsensical —
// declaring preloading yet producing no library name, for instance.
//
// Skipped when the checkout is not present, so it never fails elsewhere.
func TestRealExtensionManifests(t *testing.T) {
	root := os.Getenv("PG_EXTENSIONS_DIR")
	if root == "" {
		root = "/home/user/pg_extensions/extensions"
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("pg_extensions not available at %s", root)
	}

	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ext := entry.Name()
		dir := filepath.Join(root, ext)
		if _, err := os.Stat(filepath.Join(dir, "pgbrew.toml")); err != nil {
			continue
		}
		checked++

		m, err := sysdeps.Load(dir)
		if err != nil {
			t.Errorf("%s: manifest does not parse: %v", ext, err)
			continue
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

	if checked == 0 {
		t.Skip("no manifests found")
	}
	t.Logf("checked %d manifests", checked)
}
