package cargocfg

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// FindLibclang locates a directory holding the libclang shared library.
//
// pgrx builds bindings with bindgen, which needs libclang. A project that
// pins LIBCLANG_PATH in its cargo config pins it to wherever libclang lives
// on the machine the config was written on — `/usr/lib/llvm-20/lib` is a
// Debian path and exists nowhere else. Pointing the build at the local copy
// is what makes such a config portable.
func FindLibclang() (string, bool) {
	for _, dir := range libclangCandidates() {
		if dir == "" {
			continue
		}
		if hasLibclang(dir) {
			return dir, true
		}
	}
	return "", false
}

// libclangCandidates lists the directories to try, best first.
func libclangCandidates() []string {
	if runtime.GOOS == "darwin" {
		return darwinLibclangCandidates()
	}
	return linuxLibclangCandidates()
}

// darwinLibclangCandidates prefers Apple's libclang over Homebrew's.
//
// Apple's copy ships with the SDK whose headers PostgreSQL was built against,
// and it is the one bindgen finds on its own when nothing is pinned — so
// choosing it reproduces the behaviour of an unpinned build. Homebrew's LLVM
// is often several major versions ahead of what pgrx-pg-sys expects, which is
// its own source of miscompiled bindings, so it is only the fallback.
func darwinLibclangCandidates() []string {
	var dirs []string

	if developer, err := exec.Command("xcode-select", "-p").Output(); err == nil {
		root := strings.TrimSpace(string(developer))
		if root != "" {
			dirs = append(dirs,
				filepath.Join(root, "usr", "lib"),
				filepath.Join(root, "Toolchains", "XcodeDefault.xctoolchain", "usr", "lib"),
			)
		}
	}

	dirs = append(dirs,
		"/Library/Developer/CommandLineTools/usr/lib",
		"/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/lib",
	)

	if prefix, ok := brewPrefix("llvm"); ok {
		dirs = append(dirs, filepath.Join(prefix, "lib"))
	}

	return dirs
}

// linuxLibclangCandidates prefers the newest versioned LLVM install.
func linuxLibclangCandidates() []string {
	var dirs []string
	dirs = append(dirs, versionedLLVMLibDirs()...)
	dirs = append(dirs,
		"/usr/lib/llvm/lib",
		"/usr/local/lib",
		"/usr/lib64",
		"/usr/lib",
	)
	if prefix, ok := brewPrefix("llvm"); ok {
		dirs = append(dirs, filepath.Join(prefix, "lib"))
	}
	return dirs
}

// versionedLLVMLibDirs returns /usr/lib*/llvm-<n>/lib, newest version first.
func versionedLLVMLibDirs() []string {
	type candidate struct {
		dir     string
		version int
	}
	var found []candidate

	for _, pattern := range []string{"/usr/lib/llvm-*", "/usr/lib64/llvm-*", "/usr/lib/llvm/*"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			version := 0
			if _, rest, ok := strings.Cut(filepath.Base(match), "-"); ok {
				version, _ = strconv.Atoi(rest)
			} else {
				version, _ = strconv.Atoi(filepath.Base(match))
			}
			found = append(found, candidate{dir: filepath.Join(match, "lib"), version: version})
		}
	}

	// Newest first: a project pinning libclang is usually reaching for a
	// recent one, and an older copy is the more likely mismatch.
	sort.SliceStable(found, func(i, j int) bool { return found[i].version > found[j].version })

	dirs := make([]string, 0, len(found))
	for _, c := range found {
		dirs = append(dirs, c.dir)
	}
	return dirs
}

// hasLibclang reports whether a directory holds a loadable libclang.
func hasLibclang(dir string) bool {
	for _, name := range []string{"libclang.dylib", "libclang.so"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	// Distributions frequently ship only the versioned soname.
	matches, err := filepath.Glob(filepath.Join(dir, "libclang.so.*"))
	return err == nil && len(matches) > 0
}

// brewPrefix asks Homebrew where a formula is installed.
func brewPrefix(formula string) (string, bool) {
	output, err := exec.Command("brew", "--prefix", formula).Output()
	if err != nil {
		return "", false
	}
	prefix := strings.TrimSpace(string(output))
	if prefix == "" {
		return "", false
	}
	if _, err := os.Stat(prefix); err != nil {
		return "", false
	}
	return prefix, true
}
