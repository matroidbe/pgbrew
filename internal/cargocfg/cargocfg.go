// Package cargocfg reads a Rust project's cargo configuration and checks that
// the tools it names are actually installed.
//
// A project's `.cargo/config.toml` can require a specific linker, a compiler
// wrapper, or environment paths. Those requirements are invisible to a generic
// toolchain check: pgbrew's `doctor` can report Rust, cargo, cargo-pgrx and a C
// compiler all present, and the build still fails immediately because the
// project asked for `mold` or `sccache` and neither is installed.
//
// This package closes that gap by reading what the project actually asks for.
package cargocfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the slice of cargo configuration that names external tools.
type Config struct {
	Build  BuildSection             `toml:"build"`
	Target map[string]TargetSection `toml:"target"`
	Env    map[string]EnvValue      `toml:"env"`
}

// BuildSection is cargo's [build] table.
type BuildSection struct {
	RustcWrapper string `toml:"rustc-wrapper"`
	Rustflags    Flags  `toml:"rustflags"`
	Target       string `toml:"target"`
}

// TargetSection is a [target.<triple>] table.
type TargetSection struct {
	Linker    string `toml:"linker"`
	Runner    string `toml:"runner"`
	Rustflags Flags  `toml:"rustflags"`
}

// Flags is a cargo flag list, which may be written as an array or as a single
// space-separated string.
type Flags []string

// UnmarshalTOML accepts either form.
func (f *Flags) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		*f = strings.Fields(v)
	case []interface{}:
		out := make(Flags, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("rustflags entry is not a string: %v", item)
			}
			out = append(out, s)
		}
		*f = out
	default:
		return fmt.Errorf("rustflags must be a string or an array of strings")
	}
	return nil
}

// EnvValue is an [env] entry, which may be a bare string or a table.
type EnvValue struct {
	Value    string
	Force    bool
	Relative bool
}

// UnmarshalTOML accepts either form.
func (e *EnvValue) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		e.Value = v
	case map[string]interface{}:
		if raw, ok := v["value"]; ok {
			s, ok := raw.(string)
			if !ok {
				return fmt.Errorf("env value is not a string")
			}
			e.Value = s
		}
		if raw, ok := v["force"]; ok {
			e.Force, _ = raw.(bool)
		}
		if raw, ok := v["relative"]; ok {
			e.Relative, _ = raw.(bool)
		}
	default:
		return fmt.Errorf("env entry must be a string or a table")
	}
	return nil
}

// ConfigFile is a parsed cargo config together with where it came from, so a
// problem can be reported against the file that caused it.
type ConfigFile struct {
	Path   string
	Config Config
}

// Discover finds the cargo config files that apply to a directory.
//
// Cargo reads `.cargo/config.toml` in the directory and every ancestor, plus
// one in CARGO_HOME, with the nearest taking precedence. They are returned
// nearest-first.
func Discover(dir string) ([]ConfigFile, error) {
	var files []ConfigFile

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	appendConfig := func(path string) error {
		if seen[path] {
			return nil
		}
		seen[path] = true

		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		var cfg Config
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		files = append(files, ConfigFile{Path: path, Config: cfg})
		return nil
	}

	for current := abs; ; {
		// `config.toml` is the modern name; `config` is still honoured.
		for _, name := range []string{"config.toml", "config"} {
			if err := appendConfig(filepath.Join(current, ".cargo", name)); err != nil {
				return nil, err
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	if home := cargoHome(); home != "" {
		for _, name := range []string{"config.toml", "config"} {
			if err := appendConfig(filepath.Join(home, name)); err != nil {
				return nil, err
			}
		}
	}

	return files, nil
}

func cargoHome() string {
	if home := os.Getenv("CARGO_HOME"); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cargo")
	}
	return ""
}
