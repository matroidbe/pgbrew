package bottle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleManifest() Manifest {
	return Manifest{
		Name:        "pg_solid",
		Version:     "0.2.0",
		PgMajor:     "16",
		OS:          "linux",
		Arch:        "amd64",
		BuildSystem: "pgrx",
	}
}

func sampleFiles() map[string][]byte {
	return map[string][]byte{
		"lib/pg_solid.so":                     []byte("ELF-ish bytes"),
		"share/extension/pg_solid.control":    []byte("default_version = '0.2.0'\n"),
		"share/extension/pg_solid--0.2.0.sql": []byte("CREATE FUNCTION ...;\n"),
	}
}

func createBottle(t *testing.T, meta Manifest, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Create(&buf, meta, files); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	data := createBottle(t, sampleManifest(), sampleFiles())

	b, err := Open(bytes.NewReader(data), "test")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if b.Manifest.Name != "pg_solid" || b.Manifest.Version != "0.2.0" {
		t.Errorf("manifest = %+v", b.Manifest)
	}
	if b.Manifest.FormatVersion != FormatVersion {
		t.Errorf("format version = %d", b.Manifest.FormatVersion)
	}
	for name, want := range sampleFiles() {
		if got := b.Contents[name]; !bytes.Equal(got, want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestCreateIsDeterministic(t *testing.T) {
	// Same inputs must give the same bytes, so a published checksum is stable.
	a := createBottle(t, sampleManifest(), sampleFiles())
	b := createBottle(t, sampleManifest(), sampleFiles())
	if !bytes.Equal(a, b) {
		t.Error("Create is not deterministic for identical input")
	}
}

func TestFilenameAndTarget(t *testing.T) {
	m := sampleManifest()
	if got := m.Filename(); got != "pg_solid-0.2.0-pg16-linux-amd64.tar.gz" {
		t.Errorf("Filename() = %q", got)
	}
	if got := m.Target(); got != "pg16-linux-amd64" {
		t.Errorf("Target() = %q", got)
	}
}

func TestMatches(t *testing.T) {
	m := sampleManifest()
	if !m.Matches("16", "linux", "amd64") {
		t.Error("should match its own target")
	}
	// A .so is loaded into a running backend; a mismatch on any axis is fatal
	// at load time, so none of these may be treated as usable.
	for _, tc := range [][3]string{
		{"17", "linux", "amd64"},
		{"16", "darwin", "amd64"},
		{"16", "linux", "arm64"},
	} {
		if m.Matches(tc[0], tc[1], tc[2]) {
			t.Errorf("must not match %v", tc)
		}
	}
}

func TestDetectsTamperedContent(t *testing.T) {
	// The whole point of the checksums: a bottle arrives over a network and is
	// unpacked into privileged directories.
	files := sampleFiles()
	data := createBottle(t, sampleManifest(), files)

	tampered := tamper(t, data, "lib/pg_solid.so", []byte("malicious payload"))
	_, err := Open(bytes.NewReader(tampered), "test")
	if err == nil {
		t.Fatal("expected tampering to be detected")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want a checksum mismatch", err)
	}
}

// tamper rewrites one file's content inside a bottle, leaving the manifest
// (and therefore its checksums) untouched.
func tamper(t *testing.T, data []byte, target string, replacement []byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)

	var out bytes.Buffer
	gw := gzip.NewWriter(&out)
	tw := tar.NewWriter(gw)

	for {
		header, err := tr.Next()
		if err != nil {
			break
		}
		content := make([]byte, header.Size)
		if _, err := tr.Read(content); err != nil && header.Size > 0 {
			// short reads are fine for this fixture
			_ = err
		}
		if header.Name == target {
			content = replacement
			header.Size = int64(len(content))
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gw.Close()
	return out.Bytes()
}

func TestRejectsUnsafePaths(t *testing.T) {
	// A path traversal would write outside pkglibdir/sharedir as root.
	for _, bad := range []string{
		"../../etc/cron.d/pwn",
		"/etc/passwd",
		"lib/../../escape",
		"etc/passwd",
	} {
		var buf bytes.Buffer
		err := Create(&buf, sampleManifest(), map[string][]byte{bad: []byte("x")})
		if err == nil {
			t.Errorf("Create accepted unsafe path %q", bad)
		}
	}
}

func TestRejectsPathsOutsideLibAndShare(t *testing.T) {
	var buf bytes.Buffer
	if err := Create(&buf, sampleManifest(), map[string][]byte{"bin/rogue": []byte("x")}); err == nil {
		t.Error("Create accepted a path outside lib/ and share/")
	}
}

func TestRejectsUnknownFormatVersion(t *testing.T) {
	data := createBottle(t, sampleManifest(), sampleFiles())
	// Rewrite the manifest to claim a future format.
	future := rewriteManifest(t, data, func(m *Manifest) { m.FormatVersion = 99 })

	_, err := Open(bytes.NewReader(future), "test")
	if err == nil || !strings.Contains(err.Error(), "unsupported bottle format") {
		t.Errorf("error = %v, want an unsupported-format error", err)
	}
}

func TestRejectsManifestListingAMissingFile(t *testing.T) {
	data := createBottle(t, sampleManifest(), sampleFiles())
	extra := rewriteManifest(t, data, func(m *Manifest) {
		m.Files["lib/not-actually-here.so"] = strings.Repeat("0", 64)
	})
	_, err := Open(bytes.NewReader(extra), "test")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %v, want a missing-file error", err)
	}
}

func TestRejectsUnlistedFile(t *testing.T) {
	// A file smuggled in alongside a valid manifest must not be installed.
	data := createBottle(t, sampleManifest(), sampleFiles())
	smuggled := appendFile(t, data, "lib/extra.so", []byte("surprise"))

	_, err := Open(bytes.NewReader(smuggled), "test")
	if err == nil || !strings.Contains(err.Error(), "does not list") {
		t.Errorf("error = %v, want an unlisted-file error", err)
	}
}

func rewriteManifest(t *testing.T, data []byte, mutate func(*Manifest)) []byte {
	t.Helper()
	b, err := Open(bytes.NewReader(data), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	m := b.Manifest
	mutate(&m)
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return replaceEntry(t, data, ManifestName, encoded)
}

func appendFile(t *testing.T, data []byte, name string, content []byte) []byte {
	t.Helper()
	return rebuild(t, data, func(tw *tar.Writer) {
		if err := writeTarFile(tw, name, content); err != nil {
			t.Fatal(err)
		}
	}, "")
}

func replaceEntry(t *testing.T, data []byte, name string, content []byte) []byte {
	t.Helper()
	return rebuild(t, data, nil, name, content)
}

func rebuild(t *testing.T, data []byte, extra func(*tar.Writer), replaceName string, replaceWith ...[]byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)

	var out bytes.Buffer
	gw := gzip.NewWriter(&out)
	tw := tar.NewWriter(gw)

	for {
		header, err := tr.Next()
		if err != nil {
			break
		}
		var content bytes.Buffer
		if _, err := content.ReadFrom(tr); err != nil {
			t.Fatal(err)
		}
		body := content.Bytes()
		if replaceName != "" && header.Name == replaceName && len(replaceWith) > 0 {
			body = replaceWith[0]
		}
		if err := writeTarFile(tw, header.Name, body); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		extra(tw)
	}
	tw.Close()
	gw.Close()
	return out.Bytes()
}

func TestOpenRejectsNonBottle(t *testing.T) {
	if _, err := Open(strings.NewReader("not a gzip archive at all"), "junk"); err == nil {
		t.Error("expected an error for a non-archive")
	}

	// A valid gzip/tar without a manifest is not a bottle.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	writeTarFile(tw, "lib/whatever.so", []byte("x"))
	tw.Close()
	gw.Close()

	_, err := Open(bytes.NewReader(buf.Bytes()), "nomanifest")
	if err == nil || !strings.Contains(err.Error(), ManifestName) {
		t.Errorf("error = %v, want a missing-manifest error", err)
	}
}

func TestInstallPaths(t *testing.T) {
	data := createBottle(t, sampleManifest(), sampleFiles())
	b, err := Open(bytes.NewReader(data), "test")
	if err != nil {
		t.Fatal(err)
	}

	paths, err := b.InstallPaths("/usr/lib/postgresql/16/lib", "/usr/share/postgresql/16")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"lib/pg_solid.so":                     "/usr/lib/postgresql/16/lib/pg_solid.so",
		"share/extension/pg_solid.control":    "/usr/share/postgresql/16/extension/pg_solid.control",
		"share/extension/pg_solid--0.2.0.sql": "/usr/share/postgresql/16/extension/pg_solid--0.2.0.sql",
	}
	for k, v := range want {
		if paths[k] != v {
			t.Errorf("%s -> %s, want %s", k, paths[k], v)
		}
	}
}

func TestInstallWritesFiles(t *testing.T) {
	data := createBottle(t, sampleManifest(), sampleFiles())
	b, err := Open(bytes.NewReader(data), "test")
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	shareDir := filepath.Join(root, "share")

	written, err := b.Install(libDir, shareDir, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(written) != 3 {
		t.Errorf("wrote %d files, want 3", len(written))
	}

	so := filepath.Join(libDir, "pg_solid.so")
	content, err := os.ReadFile(so)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "ELF-ish bytes" {
		t.Errorf("content = %q", content)
	}
	info, err := os.Stat(so)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("shared library should be executable, mode = %v", info.Mode())
	}

	control := filepath.Join(shareDir, "extension", "pg_solid.control")
	if _, err := os.Stat(control); err != nil {
		t.Errorf("control file not installed: %v", err)
	}
}

func TestFromStagingDir(t *testing.T) {
	// Mirrors `cargo pgrx package`: a tree containing the target's absolute
	// install paths.
	stage := t.TempDir()
	pkgLib := "/usr/lib/postgresql/16/lib"
	share := "/usr/share/postgresql/16"

	mustWrite(t, filepath.Join(stage, pkgLib, "pg_solid.so"), "so")
	mustWrite(t, filepath.Join(stage, share, "extension", "pg_solid.control"), "control")
	mustWrite(t, filepath.Join(stage, share, "extension", "pg_solid--0.2.0.sql"), "sql")

	files, err := FromStagingDir(stage, pkgLib, share)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"lib/pg_solid.so",
		"share/extension/pg_solid.control",
		"share/extension/pg_solid--0.2.0.sql",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s; got %v", want, keys(files))
		}
	}
}

func TestFromStagingDirEmptyIsAnError(t *testing.T) {
	// Silently producing an empty bottle would be worse than failing.
	_, err := FromStagingDir(t.TempDir(), "/usr/lib/x", "/usr/share/x")
	if err == nil {
		t.Error("expected an error for an empty staging tree")
	}
}

func TestFilesForFiltersOtherExtensions(t *testing.T) {
	// A shared target directory can hold a previous build of something else;
	// shipping those would overwrite unrelated files on install.
	files := map[string][]byte{
		"lib/pg_solid.so":                     []byte("a"),
		"share/extension/pg_solid.control":    []byte("b"),
		"share/extension/pg_solid--0.2.0.sql": []byte("c"),
		"lib/pg_other.so":                     []byte("d"),
		"share/extension/pg_other.control":    []byte("e"),
	}

	got := FilesFor("pg_solid", files)
	if len(got) != 3 {
		t.Errorf("kept %v, want only the pg_solid files", keys(got))
	}
	for name := range got {
		if strings.Contains(name, "pg_other") {
			t.Errorf("kept another extension's file: %s", name)
		}
	}
}

func TestBelongsTo(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"lib/pg_solid.so", true},
		{"share/extension/pg_solid.control", true},
		{"share/extension/pg_solid--0.2.0.sql", true},
		{"share/extension/pg_solid--0.1.0--0.2.0.sql", true},
		{"lib/pg_other.so", false},
		{"share/extension/pg_other--1.0.sql", false},
	} {
		if got := belongsTo("pg_solid", tc.path); got != tc.want {
			t.Errorf("belongsTo(pg_solid, %q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
