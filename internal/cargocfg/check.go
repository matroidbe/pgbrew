package cargocfg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Issue is a tool or path a cargo config requires that is not present.
type Issue struct {
	// Source is the config file that asked for it.
	Source string
	// Field is the setting, e.g. "build.rustc-wrapper".
	Field string
	// Value is what the setting names.
	Value string
	// Problem describes what is wrong, in one line.
	Problem string
	// Hint is how to fix it, when there is an obvious fix.
	Hint string
}

func (i Issue) String() string {
	s := fmt.Sprintf("%s = %q (%s): %s", i.Field, i.Value, filepath.Base(i.Source), i.Problem)
	if i.Hint != "" {
		s += "\n    " + i.Hint
	}
	return s
}

// Checker resolves tools and paths. The fields are injectable so the checks can
// be tested without depending on what happens to be installed.
type Checker struct {
	// HostTriple is the target triple cargo will build for, e.g.
	// "x86_64-unknown-linux-gnu".
	HostTriple string
	// LookPath resolves an executable name.
	LookPath func(string) (string, error)
	// Exists reports whether a filesystem path exists.
	Exists func(string) bool
	// LookupEnv reads an environment variable, used to honour overrides.
	LookupEnv func(string) (string, bool)
	// FindLibclang locates this machine's libclang, used when a config pins
	// LIBCLANG_PATH to a directory that only exists on the machine it was
	// written on. Nil means ask the real system.
	FindLibclang func() (string, bool)
}

// NewChecker returns a Checker wired to the real system.
func NewChecker() *Checker {
	return &Checker{
		HostTriple:   HostTriple(),
		LookPath:     exec.LookPath,
		Exists:       func(p string) bool { _, err := os.Stat(p); return err == nil },
		LookupEnv:    os.LookupEnv,
		FindLibclang: FindLibclang,
	}
}

// Check inspects the discovered config files and reports what is missing.
//
// Deliberately conservative — a false positive would block a build that would
// have worked, which is worse than the problem being solved. So it only reports
// things that are unambiguously required for a native build on this host:
//
//   - Settings under a [target.<triple>] table that is not this host's are
//     skipped; they belong to a cross-compilation that is not happening.
//   - Settings under a [target.'cfg(...)'] table are skipped; evaluating cargo's
//     cfg expressions is out of scope, and guessing could be wrong either way.
//   - A setting the environment already overrides is skipped, because the
//     environment wins and the config value is not what will be used.
func (c *Checker) Check(files []ConfigFile) []Issue {
	var issues []Issue

	// Nearest config wins, so only the first definition of each setting is live.
	seenWrapper := false
	seenLinker := false

	for _, file := range files {
		cfg := file.Config

		if cfg.Build.RustcWrapper != "" && !seenWrapper {
			seenWrapper = true
			// RUSTC_WRAPPER in the environment overrides the config, including
			// when it is set to empty to disable the wrapper entirely.
			if _, overridden := c.LookupEnv("RUSTC_WRAPPER"); !overridden {
				if issue := c.checkTool(file.Path, "build.rustc-wrapper", cfg.Build.RustcWrapper); issue != nil {
					issue.Hint = fmt.Sprintf("install it, or bypass with RUSTC_WRAPPER=\"\"")
					issues = append(issues, *issue)
				}
			}
		}

		// [build] rustflags apply to every target.
		issues = append(issues, c.checkRustflags(file.Path, "build.rustflags", cfg.Build.Rustflags)...)

		for triple, target := range cfg.Target {
			if !c.appliesToHost(triple) {
				continue
			}
			if target.Linker != "" && !seenLinker {
				seenLinker = true
				if issue := c.checkTool(file.Path, "target."+triple+".linker", target.Linker); issue != nil {
					issues = append(issues, *issue)
				}
			}
			field := "target." + triple + ".rustflags"
			issues = append(issues, c.checkRustflags(file.Path, field, target.Rustflags)...)
		}

		for name, value := range cfg.Env {
			if issue := c.checkEnvPath(file.Path, name, value); issue != nil {
				issues = append(issues, *issue)
			}
		}
	}

	return issues
}

// appliesToHost reports whether a [target.X] table governs a native build here.
func (c *Checker) appliesToHost(key string) bool {
	// cfg(...) predicates need cargo's own evaluation; do not guess.
	if strings.HasPrefix(key, "cfg(") {
		return false
	}
	return key == c.HostTriple
}

// checkTool verifies an executable named by the config is resolvable.
func (c *Checker) checkTool(source, field, value string) *Issue {
	if value == "" {
		return nil
	}
	// An absolute or relative path is checked as a path; a bare name via PATH.
	if strings.ContainsRune(value, filepath.Separator) {
		if c.Exists(value) {
			return nil
		}
		return &Issue{Source: source, Field: field, Value: value, Problem: "no such file"}
	}
	if _, err := c.LookPath(value); err == nil {
		return nil
	}
	return &Issue{
		Source:  source,
		Field:   field,
		Value:   value,
		Problem: "not found on PATH",
		Hint:    installHint(value),
	}
}

// checkRustflags looks for linker selection inside rustflags.
//
// `-fuse-ld=mold` is the common shape: the linker is named in a flag passed
// through to the C compiler, not in the `linker` setting, so a check that only
// looked at `linker` would miss it entirely — which is exactly how a missing
// mold gets past a toolchain check.
func (c *Checker) checkRustflags(source, field string, flags Flags) []Issue {
	var issues []Issue
	for _, name := range linkerNamesFromFlags(flags) {
		if c.resolvesAsLinker(name) {
			continue
		}
		issues = append(issues, Issue{
			Source:  source,
			Field:   field,
			Value:   "-fuse-ld=" + name,
			Problem: fmt.Sprintf("linker %q not found on PATH", name),
			Hint:    installHint(name),
		})
	}
	return issues
}

// resolvesAsLinker reports whether a `-fuse-ld=` name is usable. Such a name is
// conventionally provided by an `ld.<name>` executable.
func (c *Checker) resolvesAsLinker(name string) bool {
	for _, candidate := range []string{"ld." + name, name} {
		if _, err := c.LookPath(candidate); err == nil {
			return true
		}
	}
	return false
}

// linkerNamesFromFlags extracts the arguments of any -fuse-ld= flag.
func linkerNamesFromFlags(flags Flags) []string {
	const marker = "-fuse-ld="
	var names []string
	for _, flag := range flags {
		idx := strings.Index(flag, marker)
		if idx < 0 {
			continue
		}
		name := flag[idx+len(marker):]
		// The flag often arrives as `link-arg=-fuse-ld=mold`; trim anything
		// after a separator that cannot be part of a linker name.
		if cut := strings.IndexAny(name, " \t,"); cut >= 0 {
			name = name[:cut]
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// checkEnvPath reports an [env] entry that names an absolute path which does
// not exist.
//
// Only absolute paths are checked. A relative value may be resolved against the
// workspace at build time, and a non-path value is not ours to judge.
func (c *Checker) checkEnvPath(source, name string, value EnvValue) *Issue {
	if value.Relative || value.Value == "" || !filepath.IsAbs(value.Value) {
		return nil
	}
	// An entry that is not forced yields to the environment.
	if !value.Force {
		if _, overridden := c.LookupEnv(name); overridden {
			return nil
		}
	}
	if c.Exists(value.Value) {
		return nil
	}
	return &Issue{
		Source:  source,
		Field:   "env." + name,
		Value:   value.Value,
		Problem: "path does not exist",
		Hint:    fmt.Sprintf("install what provides it, or set %s to the right location", name),
	}
}

// installHint suggests how to obtain well-known build tools.
func installHint(tool string) string {
	switch tool {
	case "mold":
		return "install the `mold` package"
	case "lld", "ld.lld":
		return "install the `lld` package"
	case "sccache":
		return "cargo install sccache, or install the `sccache` package"
	case "clang":
		return "install the `clang` package"
	}
	return ""
}

// HostTriple asks rustc which target it builds for by default.
func HostTriple() string {
	output, err := exec.Command("rustc", "-vV").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "host:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
