package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/matroidbe/pgbrew/internal/cellar"
)

func touch(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rel(root string, files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i], _ = filepath.Rel(root, f)
	}
	slices.Sort(out)
	return out
}

// The library on macOS is a .dylib (PostgreSQL 16+), and some builds version
// it; uninstall used to look for <name>.so alone and left both behind.
func TestInstalledFilesFindsDylibsAndVersionedLibraries(t *testing.T) {
	root := t.TempDir()
	lib, ext := filepath.Join(root, "lib"), filepath.Join(root, "share", "extension")
	touch(t,
		filepath.Join(lib, "timescaledb.dylib"),
		filepath.Join(lib, "timescaledb-2.30.2.dylib"),
		filepath.Join(lib, "timescaledb-tsl-2.30.2.dylib"),
		filepath.Join(lib, "timescaledb_toolkit.dylib"), // another extension
		filepath.Join(ext, "timescaledb.control"),
		filepath.Join(ext, "timescaledb--2.30.2.sql"),
		filepath.Join(ext, "timescaledb--2.30.1--2.30.2.sql"),
		filepath.Join(ext, "timescaledb_toolkit.control"),
	)

	got := rel(root, installedFiles("timescaledb", nil, lib, ext))
	want := []string{
		"lib/timescaledb-2.30.2.dylib", "lib/timescaledb-tsl-2.30.2.dylib", "lib/timescaledb.dylib",
		"share/extension/timescaledb--2.30.1--2.30.2.sql", "share/extension/timescaledb--2.30.2.sql",
		"share/extension/timescaledb.control",
	}
	if !slices.Equal(got, want) {
		t.Errorf("installedFiles = %v\nwant %v", got, want)
	}
}

// A bottle install recorded what it placed; uninstall removes that, and never
// anything outside the PostgreSQL directories however the record reads.
func TestInstalledFilesUsesTheRecordedFilesInsideThePostgresDirs(t *testing.T) {
	root := t.TempDir()
	lib, ext := filepath.Join(root, "lib"), filepath.Join(root, "share", "extension")
	outside := filepath.Join(root, "home", "vector.dylib")
	touch(t,
		filepath.Join(lib, "vector.dylib"),
		filepath.Join(ext, "vector.control"),
		filepath.Join(ext, "vector--0.8.7.sql"),
		outside,
	)
	entry := &cellar.Entry{Name: "vector", Files: []string{
		filepath.Join(lib, "vector.dylib"),
		filepath.Join(ext, "vector.control"),
		filepath.Join(ext, "vector--0.8.6.sql"), // recorded, since removed
		outside,
		filepath.Join(lib, "..", "lib", "..", "vector.dylib"),
	}}

	got := rel(root, installedFiles("vector", entry, lib, ext))
	want := []string{"lib/vector.dylib", "share/extension/vector.control"}
	if !slices.Equal(got, want) {
		t.Errorf("installedFiles = %v\nwant %v", got, want)
	}
}
