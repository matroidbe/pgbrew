package sysdeps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pgSolidManifest is the manifest shape pg_solid uses — the motivating case.
const pgSolidManifest = `
[[system_dependencies]]
name = "opencascade"
header = "opencascade/Standard_Version.hxx"
library = "TKernel"
min_version = "7.6"
brew_formula = "opencascade"
env_vars = ["OCCT_ROOT", "OCCT_INCLUDE_DIR", "CASROOT"]

[system_dependencies.version]
major = "OCC_VERSION_MAJOR"
minor = "OCC_VERSION_MINOR"
patch = "OCC_VERSION_MAINTENANCE"

[system_dependencies.env]
OCCT_ROOT = "{prefix}"

[system_dependencies.packages]
apt = ["libocct-foundation-dev", "libocct-data-exchange-dev"]
dnf = ["opencascade-devel"]
brew = ["opencascade"]
`

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadStandaloneManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ManifestFile, pgSolidManifest)

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.SystemDependencies) != 1 {
		t.Fatalf("got %d dependencies, want 1", len(m.SystemDependencies))
	}

	dep := m.SystemDependencies[0]
	if dep.Name != "opencascade" {
		t.Errorf("name = %q", dep.Name)
	}
	if dep.MinVersion != "7.6" {
		t.Errorf("min_version = %q", dep.MinVersion)
	}
	if dep.Library != "TKernel" {
		t.Errorf("library = %q", dep.Library)
	}
	if dep.Version == nil || dep.Version.Major != "OCC_VERSION_MAJOR" {
		t.Errorf("version spec not parsed: %+v", dep.Version)
	}
	if got := dep.Packages.Apt; len(got) != 2 {
		t.Errorf("apt packages = %v", got)
	}
	if dep.Env["OCCT_ROOT"] != "{prefix}" {
		t.Errorf("env = %v", dep.Env)
	}
	if len(dep.EnvVars) != 3 {
		t.Errorf("env_vars = %v", dep.EnvVars)
	}
}

func TestLoadFromCargoMetadata(t *testing.T) {
	dir := t.TempDir()
	// A realistic workspace-member Cargo.toml: inherited keys and unrelated
	// tables must not interfere with decoding our metadata table.
	writeFile(t, dir, "Cargo.toml", `
[package]
name = "pg_solid"
version = "0.2.0"
edition.workspace = true
license.workspace = true

[package.metadata.pgbrew]
[[package.metadata.pgbrew.system_dependencies]]
name = "opencascade"
header = "opencascade/Standard_Version.hxx"
library = "TKernel"
min_version = "7.6"

[lib]
crate-type = ["cdylib", "lib"]

[dependencies]
pgrx.workspace = true
`)

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m.SystemDependencies) != 1 {
		t.Fatalf("got %d dependencies, want 1", len(m.SystemDependencies))
	}
	if m.SystemDependencies[0].Name != "opencascade" {
		t.Errorf("name = %q", m.SystemDependencies[0].Name)
	}
}

func TestStandaloneManifestWinsOverCargo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ManifestFile, `
[[system_dependencies]]
name = "from-pgbrew-toml"
library = "Foo"
`)
	writeFile(t, dir, "Cargo.toml", `
[package]
name = "x"
[[package.metadata.pgbrew.system_dependencies]]
name = "from-cargo-toml"
library = "Bar"
`)

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.SystemDependencies[0].Name != "from-pgbrew-toml" {
		t.Errorf("got %q, want the standalone manifest to win", m.SystemDependencies[0].Name)
	}
}

func TestLoadNoManifestIsEmptyNotError(t *testing.T) {
	// The overwhelming majority of extensions declare nothing. That must not be
	// an error, or pgbrew would break every existing install.
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.IsEmpty() {
		t.Errorf("expected an empty manifest, got %+v", m)
	}
}

func TestLoadCargoWithoutPgbrewMetadataIsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"plain\"\nversion = \"1.0.0\"\n")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.IsEmpty() {
		t.Errorf("expected empty, got %+v", m)
	}
}

func TestLoadUnparseableCargoIsNotOurError(t *testing.T) {
	// cargo will report a broken Cargo.toml far better than we can; we should
	// not pre-empt it with a worse message.
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "this is not = valid toml [[[")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load should tolerate a broken Cargo.toml, got: %v", err)
	}
	if !m.IsEmpty() {
		t.Errorf("expected empty, got %+v", m)
	}
}

func TestLoadRejectsInvalidManifests(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "missing name",
			content: "[[system_dependencies]]\nlibrary = \"Foo\"\n",
			wantErr: "missing `name`",
		},
		{
			name:    "nothing to probe for",
			content: "[[system_dependencies]]\nname = \"foo\"\n",
			wantErr: "at least one of",
		},
		{
			name: "version block without major",
			content: `
[[system_dependencies]]
name = "foo"
library = "Foo"
[system_dependencies.version]
minor = "FOO_MINOR"
`,
			wantErr: "without `major`",
		},
		{
			name:    "unparseable min_version",
			content: "[[system_dependencies]]\nname = \"foo\"\nlibrary = \"Foo\"\nmin_version = \"latest\"\n",
			wantErr: "unparseable min_version",
		},
		{
			name:    "malformed toml",
			content: "[[system_dependencies]\nname =",
			wantErr: "parsing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, ManifestFile, tt.content)
			_, err := Load(dir)
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestFormulaDefaultsToName(t *testing.T) {
	if got := (Dependency{Name: "opencascade"}).Formula(); got != "opencascade" {
		t.Errorf("Formula() = %q", got)
	}
	if got := (Dependency{Name: "occt", BrewFormula: "opencascade"}).Formula(); got != "opencascade" {
		t.Errorf("Formula() = %q, want the explicit formula to win", got)
	}
}
