package pgrx

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A pgrx crate's .control file is what PostgreSQL and cargo-pgrx both use, so it
// wins over the Cargo package name whenever the two disagree.
func TestGetExtensionNamePrefersControlFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"eidos-pg\"\n")
	writeFile(t, dir, "eidos_pg.control", "default_version = '0.1.0'\n")

	got, err := GetExtensionName(dir)
	if err != nil {
		t.Fatalf("GetExtensionName: %v", err)
	}
	if got != "eidos_pg" {
		t.Errorf("got %q, want %q — the .control file names the extension", got, "eidos_pg")
	}
}

// Without a .control file the package name is all there is, but Cargo turns
// hyphens into underscores for the library it builds — so the fallback has to
// as well, or it names a file that is never written.
func TestGetExtensionNameNormalisesCargoName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"my-ext\"\nversion = \"1.0.0\"\n")

	got, err := GetExtensionName(dir)
	if err != nil {
		t.Fatalf("GetExtensionName: %v", err)
	}
	if got != "my_ext" {
		t.Errorf("got %q, want %q", got, "my_ext")
	}
}

// The common case: package name and control file already agree.
func TestGetExtensionNameAgreeingSources(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"pg_solid\"\n")
	writeFile(t, dir, "pg_solid.control", "default_version = '0.2.0'\n")

	got, err := GetExtensionName(dir)
	if err != nil {
		t.Fatalf("GetExtensionName: %v", err)
	}
	if got != "pg_solid" {
		t.Errorf("got %q, want %q", got, "pg_solid")
	}
}

func TestGetExtensionNameNoSources(t *testing.T) {
	if _, err := GetExtensionName(t.TempDir()); err == nil {
		t.Error("expected an error when neither a .control file nor Cargo.toml is present")
	}
}

// A supervisor registered in _PG_init must be preloaded, and the detector has to
// see it through the `use pgrx::bgworkers::*` glob import eidos-pg uses.
func TestNeedsSharedPreloadDetectsBackgroundWorker(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, "lib.rs", `
use pgrx::bgworkers::{BackgroundWorkerBuilder, BgWorkerStartTime};
pub extern "C-unwind" fn _PG_init() {
    BackgroundWorkerBuilder::new("eidos_supervisor").load();
}
`)

	if !NeedsSharedPreload(dir) {
		t.Error("a BackgroundWorkerBuilder in _PG_init requires shared_preload_libraries")
	}
}
