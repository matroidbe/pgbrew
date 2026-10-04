package cellar

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeShareDir points getCellarPath (via PG_CONFIG) at a temp sharedir.
func fakeShareDir(t *testing.T) string {
	t.Helper()
	share := t.TempDir()
	if err := os.MkdirAll(filepath.Join(share, "extension"), 0o755); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(t.TempDir(), "pg_config")
	script := "#!/bin/sh\necho " + share + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PG_CONFIG", stub)
	return share
}

// Recording an install that is already recorded — same extension, version,
// PostgreSQL major and build system — writes nothing, so a reinstall over an
// existing install works without write access to the extension directory.
// Where the bottle came from (a URL at image build, a local file later) does
// not make it a different install.
func TestAddSkipsAnAlreadyRecordedInstall(t *testing.T) {
	share := fakeShareDir(t)
	first := Entry{Name: "pg_kafka", Version: "0.3.1", Source: "https://example/pg_kafka.tar.gz", PgVersion: "18", BuildSystem: "pgrx"}
	if err := Add(first); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	// As in Docker after a root install: neither the record nor its
	// directory is writable by the user re-running pgx.
	ext := filepath.Join(share, "extension")
	record := filepath.Join(ext, ".pgbrew.json")
	if err := os.Chmod(record, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ext, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ext, 0o755); os.Chmod(record, 0o644) })

	again := first
	again.Source = "/bottles/pg_kafka.tar.gz"
	if err := Add(again); err != nil {
		t.Fatalf("re-recording the same install should not write: %v", err)
	}
}

// A different version is a new install and is recorded.
func TestAddRecordsANewVersion(t *testing.T) {
	fakeShareDir(t)
	if err := Add(Entry{Name: "pg_kafka", Version: "0.3.0", PgVersion: "18", BuildSystem: "pgrx"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(Entry{Name: "pg_kafka", Version: "0.3.1", PgVersion: "18", BuildSystem: "pgrx"}); err != nil {
		t.Fatal(err)
	}
	entries, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Version != "0.3.1" {
		t.Errorf("entries = %+v, want one pg_kafka 0.3.1", entries)
	}
}
