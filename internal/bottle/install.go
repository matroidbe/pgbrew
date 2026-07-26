package bottle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Install writes the bottle's files into a PostgreSQL installation.
//
// Everything has already been checksum-verified by Open, so by the time
// anything reaches a privileged directory it is known to be what the manifest
// described.
func (b *Bottle) Install(pkgLibDir, shareDir string, useSudo bool) ([]string, error) {
	paths, err := b.InstallPaths(pkgLibDir, shareDir)
	if err != nil {
		return nil, err
	}

	// Deterministic order, so the output reads the same way every time.
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)

	written := make([]string, 0, len(names))
	for _, name := range names {
		dest := paths[name]
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, LibDir+"/") {
			mode = 0o755
		}
		if err := writeFile(dest, b.Contents[name], mode, useSudo); err != nil {
			return written, fmt.Errorf("installing %s: %w", dest, err)
		}
		written = append(written, dest)
	}
	return written, nil
}

// writeFile writes content to dest, elevating if asked.
func writeFile(dest string, content []byte, mode os.FileMode, useSudo bool) error {
	if !useSudo {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, content, mode)
	}

	// Stage in a temp file this process owns, then move it into place with
	// elevated privileges. Piping the bytes through a shell would risk them
	// being mangled, and these are binaries.
	tmp, err := os.CreateTemp("", "pgbrew-bottle-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}

	if err := run("sudo", "mkdir", "-p", filepath.Dir(dest)); err != nil {
		return err
	}
	// `install` preserves the mode and replaces atomically.
	return run("sudo", "install", "-m", fmt.Sprintf("%04o", mode), tmp.Name(), dest)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
