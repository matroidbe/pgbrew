package bottle

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FromStagingDir collects a bottle's files from a staged install tree.
//
// Both builders can produce one: `cargo pgrx package` writes a tree mirroring
// the target's absolute paths, and `make install DESTDIR=...` does the same for
// PGXS. Either way the files sit under stageRoot at the PostgreSQL install
// paths, and the job here is to strip those prefixes so what remains is
// relocatable.
func FromStagingDir(stageRoot, pkgLibDir, shareDir string) (map[string][]byte, error) {
	files := map[string][]byte{}

	sources := []struct {
		dir    string
		prefix string
	}{
		{filepath.Join(stageRoot, pkgLibDir), LibDir},
		{filepath.Join(stageRoot, shareDir), ShareDir},
	}

	for _, source := range sources {
		if err := collect(source.dir, source.prefix, files); err != nil {
			return nil, err
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no installable files found under %s "+
			"(looked in %s and %s)", stageRoot,
			filepath.Join(stageRoot, pkgLibDir), filepath.Join(stageRoot, shareDir))
	}
	return files, nil
}

// collect walks a staged directory into archive entries under prefix.
func collect(dir, prefix string, files map[string][]byte) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		// A extension may install only a library, or only SQL.
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Only regular files: a symlink in a staged tree would be resolved
		// against the build machine's filesystem, which is not the target's.
		if !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		name := path.Join(prefix, filepath.ToSlash(rel))
		if err := validArchivePath(name); err != nil {
			return err
		}
		files[name] = content
		return nil
	})
}

// FilesFor filters staged files down to one extension.
//
// A staged tree can hold files from a previous build of a different extension,
// and shipping those in someone else's bottle would overwrite unrelated files
// on install.
func FilesFor(extension string, files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for name, content := range files {
		if BelongsTo(extension, name) {
			out[name] = content
		}
	}
	return out
}

// BelongsTo reports whether a file (an archive path, or a file name in an
// installed tree) is part of the named extension.
//
// PostgreSQL's own naming rules make this decidable: the library is
// <name>.so/.dylib, the control file is <name>.control, and SQL scripts are
// <name>--<version>.sql or <name>--<from>--<to>.sql.
func BelongsTo(extension, archivePath string) bool {
	base := path.Base(archivePath)
	stem := strings.TrimSuffix(base, path.Ext(base))

	if stem == extension {
		return true
	}
	// <name>--1.0.sql, <name>--1.0--1.1.sql
	if strings.HasPrefix(stem, extension+"--") {
		return true
	}
	// Some builds emit a versioned library, e.g. name-1.2.3.so
	if strings.HasPrefix(stem, extension+"-") {
		return true
	}
	return false
}

// ControlVersion returns default_version from the extension's control file
// among a bottle's files, or "" when there is none. The staged control file is
// the one the build generated, so it holds a real version even when the
// source only had a template.
func ControlVersion(extension string, files map[string][]byte) string {
	return controlValue(files[path.Join(ShareDir, "extension", extension+".control")], "default_version")
}

// ModuleLibraries adds the libraries an extension's control file names in
// module_pathname but whose names do not match the extension's: plpython3u
// loads '$libdir/plpython3'. FilesFor selects by name, so without this the
// bottle would ship the SQL and none of the code. staged holds every file of
// the staged install; files is the selection so far, which is extended.
func ModuleLibraries(extension string, files, staged map[string][]byte) {
	module := controlValue(files[path.Join(ShareDir, "extension", extension+".control")], "module_pathname")
	module = strings.TrimPrefix(module, "$libdir/")
	if module == "" || strings.Contains(module, "/") {
		return
	}
	for name, content := range staged {
		if !strings.HasPrefix(name, LibDir+"/") || strings.Count(name, "/") != 1 {
			continue
		}
		base := path.Base(name)
		if strings.TrimSuffix(base, path.Ext(base)) == module {
			files[name] = content
		}
	}
}

// controlValue returns one key's value from a control file, unquoted.
func controlValue(content []byte, key string) string {
	for _, line := range strings.Split(string(content), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `'"`)
		}
	}
	return ""
}
