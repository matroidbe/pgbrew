package builder

import (
	"fmt"
	"strings"
)

// InstallOptions contains options for the Install method.
type InstallOptions struct {
	PgConfig string   // Path to pg_config
	UseSudo  bool     // Use sudo for installation
	Features []string // Additional Cargo features to enable (pgrx only)

	// Env carries extra KEY=VALUE pairs for the build, holding the locations of
	// system dependencies pgbrew discovered (see internal/sysdeps). This is how
	// an extension's build script finds a library installed in a Homebrew or
	// otherwise non-standard prefix.
	Env []string

	// BuildVars are KEY=VALUE pairs the user handed to the build system itself:
	// make command-line variables for PGXS, -D cache entries for CMake. Unlike
	// Env they override what the build files assign, which is the only way to
	// replace a path PGXS hard-codes (BISON = /usr/bin/bison).
	BuildVars []string
}

// Builder interface defines operations for building PostgreSQL extensions.
type Builder interface {
	// Name returns the builder type name (e.g., "pgrx", "pgxs")
	Name() string

	// Detect checks if this builder can handle the project in the given directory
	Detect(dir string) bool

	// GetExtensionName extracts the extension name from the project
	GetExtensionName(dir string) (string, error)

	// GetVersion extracts the extension version from the project
	GetVersion(dir string) (string, error)

	// Install builds and installs the extension
	Install(dir string, opts InstallOptions) error

	// Package builds the extension and stages it into a directory tree without
	// touching the live PostgreSQL installation, returning the tree's root.
	// This is what a prebuilt bottle is made from.
	Package(dir string, opts InstallOptions) (string, error)

	// NeedsSharedPreload checks if the extension requires shared_preload_libraries
	NeedsSharedPreload(dir string) bool
}

// registeredBuilders holds all available builders in priority order. The
// order is explicit rather than left to init(), which runs in file-name order:
// a project can carry more than one build system (a PGXS Makefile beside a
// CMakeLists.txt), and the more specific one has to win.
var registeredBuilders = []Builder{&PgrxBuilder{}, &PgxsBuilder{}, &CMakeBuilder{}}

// DetectBuilder finds the appropriate builder for the given directory
func DetectBuilder(dir string) (Builder, error) {
	for _, b := range registeredBuilders {
		if b.Detect(dir) {
			return b, nil
		}
	}
	return nil, fmt.Errorf("unknown project type: no compatible build system found in %s", dir)
}

// ListBuilders returns the names of all registered builders
func ListBuilders() []string {
	names := make([]string, len(registeredBuilders))
	for i, b := range registeredBuilders {
		names[i] = b.Name()
	}
	return names
}

// ValidateBuildVars checks each --build-var is KEY=VALUE with a name both make
// and CMake accept, so a typo fails before anything is cloned or built.
func ValidateBuildVars(vars []string) error {
	for _, v := range vars {
		key, _, ok := strings.Cut(v, "=")
		if !ok {
			return fmt.Errorf("%q: expected KEY=VALUE", v)
		}
		if !validBuildVarName(key) {
			return fmt.Errorf("%q: invalid variable name %q", v, key)
		}
	}
	return nil
}

func validBuildVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
