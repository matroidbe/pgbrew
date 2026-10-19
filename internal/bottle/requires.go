package bottle

import (
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
)

// LinkedLibraries returns the absolute paths a macOS Mach-O file links,
// leaving out the operating system's own (/usr/lib, /System) and relative
// references (@rpath, @loader_path), which travel with whatever loads it.
// On any other OS it returns nothing: a Linux bottle names sonames, which the
// loader searches for, so there is no path to check.
func LinkedLibraries(file string) ([]string, error) {
	out, err := exec.Command("otool", "-L", file).Output()
	if err != nil {
		return nil, err
	}
	// A dylib (what pgrx builds) lists its own install name first, which is
	// the build machine's target/ path; a bundle (what PGXS builds) has none.
	// It names the file itself, not something it needs.
	self := ""
	if id, err := exec.Command("otool", "-D", file).Output(); err == nil {
		if lines := strings.Split(strings.TrimSpace(string(id)), "\n"); len(lines) > 1 {
			self = strings.TrimSpace(lines[1])
		}
	}
	return parseOtoolL(string(out), self), nil
}

// parseOtoolL extracts the libraries from `otool -L` output, leaving out the
// file's own install name, the OS's libraries and relative references.
func parseOtoolL(out, self string) []string {
	var libs []string
	for _, line := range strings.Split(out, "\n")[1:] {
		lib, _, _ := strings.Cut(strings.TrimSpace(line), " (")
		if lib == self || !strings.HasPrefix(lib, "/") ||
			strings.HasPrefix(lib, "/usr/lib/") || strings.HasPrefix(lib, "/System/") {
			continue
		}
		libs = append(libs, lib)
	}
	return libs
}

var homebrewPath = regexp.MustCompile(`^(?:/opt/homebrew|/usr/local)/(?:opt|Cellar)/([^/]+)/`)

// HomebrewFormula names the formula a Homebrew path belongs to, or "".
func HomebrewFormula(lib string) string {
	if m := homebrewPath.FindStringSubmatch(lib); m != nil {
		return m[1]
	}
	return ""
}

// RuntimeRequirements collects what a bottle's libraries link from outside
// the operating system. stagedLib maps each archive path under lib/ to the
// staged file it was read from; only those are inspected.
func RuntimeRequirements(stagedLib map[string]string) ([]RuntimeLibrary, error) {
	seen := map[string]bool{}
	var reqs []RuntimeLibrary
	names := make([]string, 0, len(stagedLib))
	for name := range stagedLib {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ext := path.Ext(name)
		if ext != ".dylib" && ext != ".so" {
			continue
		}
		libs, err := LinkedLibraries(stagedLib[name])
		if err != nil {
			return nil, err
		}
		for _, lib := range libs {
			if !seen[lib] {
				seen[lib] = true
				reqs = append(reqs, RuntimeLibrary{Path: lib, Formula: HomebrewFormula(lib)})
			}
		}
	}
	return reqs, nil
}

// MissingRequirements returns the required libraries that are not on this
// machine.
func MissingRequirements(reqs []RuntimeLibrary) []RuntimeLibrary {
	var missing []RuntimeLibrary
	for _, r := range reqs {
		if _, err := os.Stat(r.Path); err != nil {
			missing = append(missing, r)
		}
	}
	return missing
}
