package cellar

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// useSudo controls whether cellar operations use sudo
var useSudo bool

// SetUseSudo sets whether cellar operations should use sudo
func SetUseSudo(sudo bool) {
	useSudo = sudo
}

// Entry represents an installed extension.
type Entry struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Source      string    `json:"source"`
	PgVersion   string    `json:"pg_version"`
	BuildSystem string    `json:"build_system,omitempty"` // "pgrx", "pgxs" or "cmake"
	InstalledAt time.Time `json:"installed_at"`

	// Features and BuildVars are the --features and --build-var a source
	// install was built with, so `pgx update` rebuilds it the same way.
	Features  []string `json:"features,omitempty"`
	BuildVars []string `json:"build_vars,omitempty"`
}

// Cellar manages installed extensions.
type Cellar struct {
	Entries []Entry `json:"entries"`
}

func getCellarPath() (string, error) {
	// Get extension directory from pg_config
	pgConfig := os.Getenv("PG_CONFIG")
	if pgConfig == "" {
		pgConfig = "pg_config"
	}

	cmd := exec.Command(pgConfig, "--sharedir")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get sharedir from pg_config: %w", err)
	}

	shareDir := strings.TrimSpace(string(output))
	extDir := filepath.Join(shareDir, "extension")

	return filepath.Join(extDir, ".pgbrew.json"), nil
}

func load() (*Cellar, error) {
	path, err := getCellarPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Cellar{Entries: []Entry{}}, nil
		}
		return nil, err
	}

	var c Cellar
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}

	return &c, nil
}

func save(c *Cellar) error {
	path, err := getCellarPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	if useSudo {
		// Use sudo tee to write the file
		cmd := exec.Command("sudo", "tee", path)
		cmd.Stdin = strings.NewReader(string(data))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to write cellar file with sudo: %w", err)
		}
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Add adds or updates an extension entry.
func Add(entry Entry) error {
	c, err := load()
	if err != nil {
		return err
	}

	// Already recorded — same version for the same PostgreSQL and build
	// system — so there is nothing to write. Where it was installed from
	// does not matter. Skipping the write lets a reinstall over an existing
	// install run without write access to the extension directory.
	for _, e := range c.Entries {
		if e.Name == entry.Name && e.Version == entry.Version &&
			e.PgVersion == entry.PgVersion && e.BuildSystem == entry.BuildSystem &&
			sameOrigin(e.Source, entry.Source) && slices.Equal(e.Features, entry.Features) &&
			slices.Equal(e.BuildVars, entry.BuildVars) {
			return nil
		}
	}

	entry.InstalledAt = time.Now()

	// Update existing or append new
	found := false
	for i, e := range c.Entries {
		if e.Name == entry.Name {
			c.Entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		c.Entries = append(c.Entries, entry)
	}

	return save(c)
}

// List returns all installed extensions.
func List() ([]Entry, error) {
	c, err := load()
	if err != nil {
		return nil, err
	}
	return c.Entries, nil
}

// Get returns a specific extension by name.
func Get(name string) (*Entry, error) {
	c, err := load()
	if err != nil {
		return nil, err
	}

	for _, e := range c.Entries {
		if e.Name == name {
			return &e, nil
		}
	}

	return nil, fmt.Errorf("extension not found: %s", name)
}

// Remove removes an extension from the cellar.
func Remove(name string) error {
	c, err := load()
	if err != nil {
		return err
	}

	found := false
	newEntries := make([]Entry, 0, len(c.Entries))
	for _, e := range c.Entries {
		if e.Name == name {
			found = true
			continue
		}
		newEntries = append(newEntries, e)
	}

	if !found {
		return fmt.Errorf("extension not found: %s", name)
	}

	c.Entries = newEntries
	return save(c)
}

// sameOrigin reports whether two installs came from the same kind of place.
// The same bottle fetched from another path or URL is the same install; a
// bottle replaced by a source build (or one source by another) is not, and
// `pgx update` needs to know which it now is.
func sameOrigin(a, b string) bool {
	return a == b || (IsBottleSource(a) && IsBottleSource(b))
}

// IsBottleSource reports whether a recorded source is a bottle (a file or an
// http(s) URL) rather than something pgx builds.
func IsBottleSource(source string) bool {
	return strings.HasSuffix(source, ".tar.gz") ||
		strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}
