// Package bottle handles prebuilt extension artifacts.
//
// Building a PostgreSQL extension from source costs the user a Rust toolchain,
// cargo-pgrx, any native libraries it links against, and several minutes of
// compiling. None of that is inherent: the output is a handful of files, and
// the same files work on every machine with a matching PostgreSQL major
// version, OS and architecture.
//
// A bottle is those files plus a manifest saying what they are for, so an
// install becomes a download, a checksum check and a copy. This is Homebrew's
// bottle model, and the source build remains as the fallback.
//
// A bottle is inherently keyed by (extension version, PostgreSQL major, OS,
// arch): a .so is loaded into a running PostgreSQL backend and must agree with
// it on ABI, so there is no such thing as one artifact for all targets.
package bottle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// FormatVersion is the bottle layout version. A bottle declaring a version this
// build does not understand is refused rather than half-read.
const FormatVersion = 1

// ManifestName is the manifest's path inside the archive.
const ManifestName = "bottle.json"

// Directories inside a bottle, mapped at install time to the target's real
// pkglibdir and sharedir. Storing files by role rather than by absolute path is
// what makes a bottle relocatable across PostgreSQL installations.
const (
	LibDir   = "lib"
	ShareDir = "share"
)

// Manifest describes a bottle's contents and what it is built for.
type Manifest struct {
	FormatVersion int `json:"format_version"`

	Name    string `json:"name"`
	Version string `json:"version"`

	// PgMajor is the PostgreSQL major version, e.g. "16".
	PgMajor string `json:"pg_major"`
	// OS and Arch are Go's GOOS/GOARCH values.
	OS   string `json:"os"`
	Arch string `json:"arch"`

	// BuildSystem is the builder that produced it ("pgrx" or "pgxs").
	BuildSystem string `json:"build_system,omitempty"`

	// Files maps each archive path to its SHA-256, so tampering or truncation
	// is caught before anything is written into a PostgreSQL directory.
	Files map[string]string `json:"files"`
}

// Filename is the conventional bottle filename for a manifest.
//
// The target is in the name because it has to be: a consumer picks a bottle by
// matching it, and a mismatched one must be impossible to select by accident.
func (m Manifest) Filename() string {
	return fmt.Sprintf("%s-%s-pg%s-%s-%s.tar.gz", m.Name, m.Version, m.PgMajor, m.OS, m.Arch)
}

// Target renders the target triple for display.
func (m Manifest) Target() string {
	return fmt.Sprintf("pg%s-%s-%s", m.PgMajor, m.OS, m.Arch)
}

// Matches reports whether this bottle is usable for the given target.
func (m Manifest) Matches(pgMajor, goos, arch string) bool {
	return m.PgMajor == pgMajor && m.OS == goos && m.Arch == arch
}

// Validate rejects a manifest that could not be installed safely.
func (m Manifest) Validate() error {
	if m.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported bottle format version %d (this pgbrew understands %d)",
			m.FormatVersion, FormatVersion)
	}
	if m.Name == "" {
		return fmt.Errorf("bottle manifest has no name")
	}
	if m.PgMajor == "" || m.OS == "" || m.Arch == "" {
		return fmt.Errorf("bottle manifest does not say what target it is for")
	}
	if len(m.Files) == 0 {
		return fmt.Errorf("bottle manifest lists no files")
	}
	for name := range m.Files {
		if err := validArchivePath(name); err != nil {
			return err
		}
	}
	return nil
}

// validArchivePath rejects paths that would escape the install directories.
//
// A bottle is downloaded from a network location and then unpacked into
// privileged directories, so an entry like "../../etc/cron.d/x" has to be
// impossible rather than merely unlikely.
func validArchivePath(name string) error {
	if name == ManifestName {
		return nil
	}
	clean := path.Clean(name)
	if clean != name {
		return fmt.Errorf("bottle contains a non-normalised path: %q", name)
	}
	if path.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return fmt.Errorf("bottle contains an unsafe path: %q", name)
	}
	root, _, _ := strings.Cut(clean, "/")
	if root != LibDir && root != ShareDir {
		return fmt.Errorf("bottle contains a path outside %s/ and %s/: %q", LibDir, ShareDir, name)
	}
	return nil
}

// Bottle is an opened bottle: its manifest and the file contents.
type Bottle struct {
	Manifest Manifest
	// Contents maps archive path to file bytes. Extensions are small — a
	// megabyte or two — so holding them in memory keeps verification and
	// installation atomic: nothing is written until everything checks out.
	Contents map[string][]byte
	// Source is where the bottle came from, for messages.
	Source string
}

// Create writes a bottle containing the given files.
//
// files maps archive paths (lib/..., share/...) to their contents.
func Create(w io.Writer, meta Manifest, files map[string][]byte) error {
	meta.FormatVersion = FormatVersion
	meta.Files = make(map[string]string, len(files))
	for name, content := range files {
		if err := validArchivePath(name); err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		meta.Files[name] = hex.EncodeToString(sum[:])
	}
	if err := meta.Validate(); err != nil {
		return err
	}

	manifestJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	// Manifest first, so a reader can learn what it is holding before it
	// decides whether to read the rest.
	if err := writeTarFile(tw, ManifestName, manifestJSON); err != nil {
		return err
	}

	// Sorted, so the same inputs always produce the same archive.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeTarFile(tw, name, files[name]); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeTarFile(tw *tar.Writer, name string, content []byte) error {
	mode := int64(0o644)
	// A shared library must be executable for some platforms' loaders.
	if strings.HasPrefix(name, LibDir+"/") {
		mode = 0o755
	}
	header := &tar.Header{
		Name:     name,
		Mode:     mode,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(content)
	return err
}

// maxBottleBytes caps how much a bottle may expand to, so a malicious or
// corrupt archive cannot exhaust memory.
const maxBottleBytes = 512 << 20 // 512 MiB

// Open reads and fully verifies a bottle.
//
// Everything is checked before the caller can install anything: the format
// version, the paths, and every file's checksum.
func Open(r io.Reader, source string) (*Bottle, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%s: not a gzip archive: %w", source, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	contents := map[string][]byte{}
	var manifest *Manifest
	var total int64

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		if header.Typeflag != tar.TypeReg {
			// Directories, symlinks and devices have no place in a bottle, and
			// a symlink is a way to write outside the target directory.
			continue
		}

		total += header.Size
		if total > maxBottleBytes {
			return nil, fmt.Errorf("%s: bottle expands to more than %d bytes", source, maxBottleBytes)
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxBottleBytes))
		if err != nil {
			return nil, fmt.Errorf("%s: reading %s: %w", source, header.Name, err)
		}

		if header.Name == ManifestName {
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, fmt.Errorf("%s: invalid %s: %w", source, ManifestName, err)
			}
			manifest = &m
			continue
		}
		contents[header.Name] = data
	}

	if manifest == nil {
		return nil, fmt.Errorf("%s: no %s found; is this a pgbrew bottle?", source, ManifestName)
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}

	b := &Bottle{Manifest: *manifest, Contents: contents, Source: source}
	if err := b.verify(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return b, nil
}

// verify checks that the archive holds exactly the files the manifest promises,
// with the checksums it promises.
func (b *Bottle) verify() error {
	for name, want := range b.Manifest.Files {
		content, ok := b.Contents[name]
		if !ok {
			return fmt.Errorf("bottle is missing %s, which its manifest lists", name)
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("checksum mismatch for %s (manifest %s, actual %s)", name, want, got)
		}
	}
	for name := range b.Contents {
		if _, ok := b.Manifest.Files[name]; !ok {
			return fmt.Errorf("bottle contains %s, which its manifest does not list", name)
		}
	}
	return nil
}

// OpenFile reads a bottle from disk.
func OpenFile(path string) (*Bottle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Open(f, path)
}

// InstallPaths maps a bottle's archive paths to absolute destinations.
func (b *Bottle) InstallPaths(pkgLibDir, shareDir string) (map[string]string, error) {
	out := make(map[string]string, len(b.Contents))
	for name := range b.Contents {
		if err := validArchivePath(name); err != nil {
			return nil, err
		}
		root, rest, _ := strings.Cut(name, "/")
		switch root {
		case LibDir:
			out[name] = filepath.Join(pkgLibDir, filepath.FromSlash(rest))
		case ShareDir:
			out[name] = filepath.Join(shareDir, filepath.FromSlash(rest))
		default:
			return nil, fmt.Errorf("unexpected bottle path %q", name)
		}
	}
	return out, nil
}
