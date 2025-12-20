package cellar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry represents an installed extension.
type Entry struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Source      string    `json:"source"`
	PgVersion   string    `json:"pg_version"`
	BuildSystem string    `json:"build_system,omitempty"` // "pgrx" or "pgxs"
	InstalledAt time.Time `json:"installed_at"`
}

// Cellar manages installed extensions.
type Cellar struct {
	Entries []Entry `json:"entries"`
}

func getCellarPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pgbrew", "installed.json"), nil
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

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
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
