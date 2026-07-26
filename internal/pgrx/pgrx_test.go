package pgrx

import (
	"os"
	"path/filepath"
	"strings"
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

// A package that inherits its version from the workspace has no literal version
// of its own, so scanning the file finds the first dependency's instead. That
// number would then name the cellar entry and the bottle filename.
func TestGetVersionResolvesWorkspaceInheritance(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Cargo.toml", `
[workspace]
members = ["eidos-pg"]

[workspace.package]
version = "1.1.0"
`)
	crate := filepath.Join(root, "eidos-pg")
	if err := os.Mkdir(crate, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, crate, "Cargo.toml", `
[package]
name = "eidos-pg"
version.workspace = true

[dependencies]
socket2 = { version = "0.5", features = ["all"] }
pgrx = "0.16"
`)

	got, err := GetVersion(crate)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got != "1.1.0" {
		t.Errorf("got %q, want %q (0.5 means socket2's version leaked through)", got, "1.1.0")
	}
}

// A literal version in [package] is the common case and must win outright.
func TestGetVersionPrefersLiteralPackageVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", `
[package]
name = "pg_solid"
version = "0.2.0"

[dependencies]
serde = { version = "9.9.9" }
`)

	got, err := GetVersion(dir)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got != "0.2.0" {
		t.Errorf("got %q, want %q", got, "0.2.0")
	}
}

// Inheritance with no workspace above it is a broken manifest; say so rather
// than reporting a dependency's version as the extension's.
func TestGetVersionOrphanedInheritance(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", `
[package]
name = "lonely"
version.workspace = true

[dependencies]
socket2 = { version = "0.5" }
`)

	got, err := GetVersion(dir)
	if err == nil {
		t.Fatalf("expected an error, got version %q", got)
	}
	if !strings.Contains(err.Error(), "workspace") {
		t.Errorf("error should name the cause, got: %v", err)
	}
}

// The name fallback must read [package], not the first `name =` in the file.
func TestGetExtensionNameIgnoresNonPackageNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", `
[[bin]]
name = "helper-binary"

[package]
name = "real-ext"
version = "1.0.0"
`)

	got, err := GetExtensionName(dir)
	if err != nil {
		t.Fatalf("GetExtensionName: %v", err)
	}
	if got != "real_ext" {
		t.Errorf("got %q, want %q", got, "real_ext")
	}
}
