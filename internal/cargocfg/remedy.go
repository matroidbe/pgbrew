package cargocfg

import (
	"fmt"
	"path/filepath"
)

// Remedy is an environment override that makes a build work despite a cargo
// configuration written for a different machine.
//
// A committed `.cargo/config.toml` is shared by everyone who clones the repo,
// but cargo has no way to make `[build]` or `[env]` conditional on the host —
// there is no `cfg(target_os)` for either. So a Linux-authored config names a
// Linux compiler cache and a Linux libclang, and every macOS clone inherits
// them. Reporting that as "install these tools" is not a fix: the tools are
// not what the build needs here, and installing `/usr/lib/llvm-20/lib` on a
// Mac is not a thing anyone can do.
//
// Where the setting is one whose intent survives being pointed elsewhere,
// pgbrew points it elsewhere and says so. Cargo's `[env]` entries yield to the
// real environment unless marked `force`, and RUSTC_WRAPPER in the environment
// overrides `build.rustc-wrapper`, so an override is all it takes.
type Remedy struct {
	// Issue is the configuration problem this neutralises.
	Issue Issue
	// Key and Value are the environment override to apply to the build.
	Key   string
	Value string
	// Reason is a one-line explanation, addressed to the user.
	Reason string
}

// EnvPair renders the override as a KEY=VALUE pair.
func (r Remedy) EnvPair() string { return r.Key + "=" + r.Value }

func (r Remedy) String() string {
	return fmt.Sprintf("%s = %q (%s): %s", r.Issue.Field, r.Issue.Value, filepath.Base(r.Issue.Source), r.Reason)
}

// compilerCaches are the rustc wrappers that are, by definition, transparent:
// they cache compilation and hand back what the compiler would have produced.
// Dropping one changes how long the build takes and nothing else.
//
// Only these are auto-bypassed. A wrapper pgbrew does not recognise may be
// doing something the build depends on — injecting flags, shimming a
// cross-compiler — and silently dropping it would produce a build that
// succeeds while being wrong. That is worse than stopping.
var compilerCaches = map[string]bool{
	"sccache": true,
	"ccache":  true,
}

// Remedies splits issues into the ones pgbrew can work around by adjusting the
// build environment, and the ones that genuinely block the build.
func (c *Checker) Remedies(issues []Issue) (fixes []Remedy, blocking []Issue) {
	for _, issue := range issues {
		if remedy, ok := c.remedy(issue); ok {
			fixes = append(fixes, remedy)
			continue
		}
		// An issue that survives has to carry advice someone can act on.
		if hint := unfixableHint(issue); hint != "" {
			issue.Hint = hint
		}
		blocking = append(blocking, issue)
	}
	return fixes, blocking
}

// remedy returns the environment override that neutralises an issue.
func (c *Checker) remedy(issue Issue) (Remedy, bool) {
	switch {
	case issue.Field == "build.rustc-wrapper":
		if !compilerCaches[filepath.Base(issue.Value)] {
			return Remedy{}, false
		}
		return Remedy{
			Issue: issue,
			Key:   "RUSTC_WRAPPER",
			Value: "",
			Reason: fmt.Sprintf(
				"%s is not installed; building without the compiler cache (slower, same result)",
				issue.Value),
		}, true

	case issue.Field == "env.LIBCLANG_PATH":
		dir, ok := c.findLibclang()
		if !ok {
			return Remedy{}, false
		}
		return Remedy{
			Issue: issue,
			Key:   "LIBCLANG_PATH",
			Value: dir,
			Reason: fmt.Sprintf(
				"%s does not exist here; using this machine's libclang at %s",
				issue.Value, dir),
		}, true
	}

	return Remedy{}, false
}

// findLibclang resolves libclang through the injected hook when there is one,
// so the classification can be tested without depending on what is installed.
func (c *Checker) findLibclang() (string, bool) {
	if c.FindLibclang != nil {
		return c.FindLibclang()
	}
	return FindLibclang()
}

// EnvPairs renders remedies as the KEY=VALUE pairs a build environment takes.
func EnvPairs(fixes []Remedy) []string {
	pairs := make([]string, 0, len(fixes))
	for _, fix := range fixes {
		pairs = append(pairs, fix.EnvPair())
	}
	return pairs
}

// unfixableHint improves the advice on an issue nothing could be done about.
//
// The generic hint tells the reader to install whatever provides the path.
// When the path is a libclang that was never going to exist on this machine,
// that advice sends them looking for a Debian directory on a Mac. Returns ""
// to leave the original hint alone.
func unfixableHint(issue Issue) string {
	switch {
	case issue.Field == "env.LIBCLANG_PATH":
		return "no libclang found on this machine — install LLVM (`brew install llvm` " +
			"or `apt install libclang-dev`), or set LIBCLANG_PATH to a directory containing it"
	case issue.Field == "build.rustc-wrapper":
		return fmt.Sprintf(
			"install %s, or bypass it with RUSTC_WRAPPER=\"\" (pgbrew only bypasses wrappers "+
				"it knows to be compiler caches)", issue.Value)
	}
	return ""
}
