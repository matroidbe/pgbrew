package builder

import (
	"fmt"
)

// InstallOptions contains options for the Install method.
type InstallOptions struct {
	PgConfig string // Path to pg_config
	UseSudo  bool   // Use sudo for installation
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

	// NeedsSharedPreload checks if the extension requires shared_preload_libraries
	NeedsSharedPreload(dir string) bool
}

// registeredBuilders holds all available builders in priority order
var registeredBuilders []Builder

// Register adds a builder to the registry
func Register(b Builder) {
	registeredBuilders = append(registeredBuilders, b)
}

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
