// Package sysdeps discovers and installs the native libraries a PostgreSQL
// extension needs at build time.
//
// pgbrew already manages the toolchain (Rust, cargo-pgrx, a C compiler) and the
// extension source. What it had no concept of was a *native library dependency*
// — something like OpenCASCADE for pg_solid, which must already be installed
// before the extension can compile against it. Without that, the failure mode is
// a wall of linker errors with no diagnosis.
//
// An extension declares its native dependencies in a manifest, either a
// standalone `pgbrew.toml` or a `[package.metadata.pgbrew]` table inside
// `Cargo.toml`. pgbrew then probes for each one, reports what is missing with
// the exact install command for the platform, and (with --install-deps) runs it.
package sysdeps

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ManifestFile is the standalone manifest filename, checked before Cargo.toml.
const ManifestFile = "pgbrew.toml"

// Manifest is the pgbrew section of an extension's metadata.
type Manifest struct {
	SystemDependencies []Dependency `toml:"system_dependencies"`

	// Postgres declares the PostgreSQL configuration the extension needs in
	// order to actually work once installed.
	Postgres PostgresSection `toml:"postgresql"`
}

// PostgresSection declares the PostgreSQL configuration an extension requires.
//
// Installing the files is only half of it: an extension registering a
// background worker does nothing unless its library is preloaded, and most
// carry GUC settings that need to be set for it to behave.
//
// Declaring this beats inferring it. The previous approach grepped the source
// for "BackgroundWorker", which cannot work for a C extension and cannot work
// at all for a prebuilt bottle, where there is no source to grep.
type PostgresSection struct {
	// SharedPreloadLibraries says the extension must be in
	// shared_preload_libraries, which requires a server restart.
	SharedPreloadLibraries bool `toml:"shared_preload_libraries"`

	// Library overrides the name added to shared_preload_libraries. Defaults to
	// the extension name, which is almost always right.
	Library string `toml:"library"`

	// Settings are GUCs to set for the extension.
	Settings map[string]string `toml:"settings"`

	// RestartRequired forces a restart even without a preload requirement, for
	// settings that cannot be applied by a reload.
	RestartRequired bool `toml:"restart_required"`
}

// IsZero reports whether the extension declares no PostgreSQL configuration.
func (p PostgresSection) IsZero() bool {
	return !p.SharedPreloadLibraries && !p.RestartRequired && len(p.Settings) == 0
}

// NeedsRestart reports whether applying this configuration requires a restart
// rather than a reload. shared_preload_libraries is only read at server start.
func (p PostgresSection) NeedsRestart() bool {
	return p.SharedPreloadLibraries || p.RestartRequired
}

// PreloadName returns the library name to add to shared_preload_libraries.
func (p PostgresSection) PreloadName(extension string) string {
	if !p.SharedPreloadLibraries {
		return ""
	}
	if p.Library != "" {
		return p.Library
	}
	return extension
}

// Dependency is one native library the extension needs in order to build.
type Dependency struct {
	// Name is the human-readable library name, e.g. "opencascade".
	Name string `toml:"name"`

	// Header is a representative header path relative to an include directory,
	// e.g. "opencascade/Standard_Version.hxx". Its presence identifies a prefix.
	Header string `toml:"header"`

	// Library is a link name without the "lib" prefix or extension, e.g.
	// "TKernel" matches libTKernel.so / .dylib / .a.
	Library string `toml:"library"`

	// MinVersion, when set, is the lowest acceptable version, e.g. "7.6".
	MinVersion string `toml:"min_version"`

	// Version describes how to read the installed version out of a header.
	Version *VersionSpec `toml:"version"`

	// BrewFormula is the Homebrew formula name, used both to install the
	// dependency and to locate it via `brew --prefix`. Defaults to Name.
	BrewFormula string `toml:"brew_formula"`

	// Prefixes are extra installation prefixes to search, ahead of the defaults.
	Prefixes []string `toml:"prefixes"`

	// EnvVars are environment variables that, if already set by the user, point
	// directly at an installation prefix. Checked before anything else.
	EnvVars []string `toml:"env_vars"`

	// Env maps environment variables to export into the build. Values may use
	// the placeholders {prefix}, {include} and {lib}.
	Env map[string]string `toml:"env"`

	// Packages lists the package name(s) per platform package manager.
	Packages Packages `toml:"packages"`
}

// Packages holds per-package-manager package names for a dependency.
type Packages struct {
	Apt    []string `toml:"apt"`
	Dnf    []string `toml:"dnf"`
	Pacman []string `toml:"pacman"`
	Apk    []string `toml:"apk"`
	Zypper []string `toml:"zypper"`
	Brew   []string `toml:"brew"`
}

// VersionSpec says how to extract a version from a C/C++ header, by reading
// integer `#define` macros. This mirrors how the libraries themselves publish
// their version (OCCT's Standard_Version.hxx, for instance).
type VersionSpec struct {
	// Header is the version header, relative to the include directory. Defaults
	// to the dependency's Header.
	Header string `toml:"header"`
	// Major, Minor and Patch are macro names. Minor and Patch are optional.
	Major string `toml:"major"`
	Minor string `toml:"minor"`
	Patch string `toml:"patch"`
}

// Formula returns the Homebrew formula name for this dependency.
func (d Dependency) Formula() string {
	if d.BrewFormula != "" {
		return d.BrewFormula
	}
	return d.Name
}

// cargoManifest is the slice of Cargo.toml we care about. Every other key is
// left undecoded, so workspace inheritance and the rest of the manifest are
// simply ignored.
type cargoManifest struct {
	Package struct {
		Metadata struct {
			Pgbrew Manifest `toml:"pgbrew"`
		} `toml:"metadata"`
	} `toml:"package"`
}

// Load reads an extension's system-dependency manifest from dir.
//
// A standalone pgbrew.toml wins; otherwise [package.metadata.pgbrew] in
// Cargo.toml is used. An extension that declares neither has no native
// dependencies as far as pgbrew is concerned, which is reported as an empty
// manifest rather than an error — the overwhelming majority of extensions.
func Load(dir string) (*Manifest, error) {
	standalone := filepath.Join(dir, ManifestFile)
	if data, err := os.ReadFile(standalone); err == nil {
		var m Manifest
		if _, err := toml.Decode(string(data), &m); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", standalone, err)
		}
		if err := m.validate(ManifestFile); err != nil {
			return nil, err
		}
		return &m, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", standalone, err)
	}

	cargoPath := filepath.Join(dir, "Cargo.toml")
	if data, err := os.ReadFile(cargoPath); err == nil {
		var c cargoManifest
		if _, err := toml.Decode(string(data), &c); err != nil {
			// A Cargo.toml we cannot parse is not necessarily our problem —
			// cargo itself will report it far better than we can.
			return &Manifest{}, nil
		}
		m := c.Package.Metadata.Pgbrew
		if err := m.validate("Cargo.toml [package.metadata.pgbrew]"); err != nil {
			return nil, err
		}
		return &m, nil
	}

	return &Manifest{}, nil
}

// validate rejects manifests that would silently misbehave.
func (m Manifest) validate(source string) error {
	for i, dep := range m.SystemDependencies {
		if dep.Name == "" {
			return fmt.Errorf("%s: system_dependencies[%d] is missing `name`", source, i)
		}
		if dep.Header == "" && dep.Library == "" {
			return fmt.Errorf(
				"%s: system dependency %q needs at least one of `header` or `library` to probe for",
				source, dep.Name)
		}
		if dep.Version != nil && dep.Version.Major == "" {
			return fmt.Errorf(
				"%s: system dependency %q has a [version] block without `major`",
				source, dep.Name)
		}
		if dep.MinVersion != "" {
			if _, err := ParseVersion(dep.MinVersion); err != nil {
				return fmt.Errorf(
					"%s: system dependency %q has an unparseable min_version %q",
					source, dep.Name, dep.MinVersion)
			}
		}
	}
	return nil
}

// IsEmpty reports whether the manifest declares no native dependencies.
func (m Manifest) IsEmpty() bool {
	return len(m.SystemDependencies) == 0
}
