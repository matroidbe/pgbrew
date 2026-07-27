package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/matroidbe/pgbrew/internal/cargocfg"
)

// skipToolchainCheck bypasses the cargo configuration check.
var skipToolchainCheck bool

// resolveCargoToolchain reconciles a Rust project's cargo configuration with
// this machine, and returns the environment overrides the build needs.
//
// These requirements are invisible to a generic toolchain check. A project can
// name a linker, a compiler wrapper or an environment path in
// `.cargo/config.toml`, and none of that shows up in "is Rust installed". The
// result is a build that fails within seconds of starting, after `doctor` has
// already reported everything as fine.
//
// Most of what it finds is not a missing tool but a config written for another
// operating system: a committed `.cargo/config.toml` applies to every clone,
// and cargo has no host predicate for `[build]` or `[env]`, so a Linux
// project's compiler cache and libclang path arrive on every Mac that clones
// it. Where the setting can be pointed at this machine instead, pgbrew points
// it there and says so; the build only stops for what is genuinely missing.
func resolveCargoToolchain(dir string) ([]string, error) {
	if skipToolchainCheck {
		return nil, nil
	}

	files, err := cargocfg.Discover(dir)
	if err != nil {
		// A malformed cargo config is cargo's to complain about, and it will do
		// so far better than we can. Do not pre-empt it with a worse message.
		fmt.Fprintf(os.Stderr, "Warning: could not read cargo configuration: %v\n", err)
		return nil, nil
	}

	checker := cargocfg.NewChecker()
	fixes, blocking := checker.Remedies(checker.Check(files))

	if len(fixes) > 0 {
		fmt.Println("\nThis project's cargo configuration was written for another machine.")
		fmt.Println("Adjusting the build environment:")
		for _, fix := range fixes {
			fmt.Printf("  → %s\n", fix)
		}
	}

	if len(blocking) == 0 {
		return cargocfg.EnvPairs(fixes), nil
	}

	var b strings.Builder
	b.WriteString("\nThis project's cargo configuration requires tools that are not installed:\n\n")
	for _, issue := range blocking {
		fmt.Fprintf(&b, "  ✗ %s\n", issue)
	}
	b.WriteString("\nThe build would fail almost immediately. Install them, or bypass this\n")
	b.WriteString("check with --skip-toolchain-check.\n")

	return nil, fmt.Errorf("%s", b.String())
}

// reportCargoToolchain prints the cargo configuration status for doctor and
// reports whether the project can be built here.
//
// A setting pgbrew will work around is reported as such rather than as a
// failure, because it is not one: doctor's job is to predict whether the build
// will run, and it will.
func reportCargoToolchain(dir string) bool {
	fmt.Println()
	fmt.Printf("Cargo configuration required by %s:\n", dir)

	files, err := cargocfg.Discover(dir)
	if err != nil {
		fmt.Printf("✗ %v\n", err)
		return false
	}
	if len(files) == 0 {
		fmt.Println("  (no .cargo/config.toml found)")
		return true
	}

	checker := cargocfg.NewChecker()
	fixes, blocking := checker.Remedies(checker.Check(files))

	for _, fix := range fixes {
		fmt.Printf("→ %s\n", fix)
	}
	for _, issue := range blocking {
		fmt.Printf("✗ %s\n", issue)
	}

	if len(blocking) > 0 {
		return false
	}
	if len(fixes) == 0 {
		fmt.Printf("✓ all tools required by %d config file(s) are installed\n", len(files))
	} else {
		fmt.Printf("✓ buildable — pgbrew adjusts %d setting(s) written for another machine\n", len(fixes))
	}
	return true
}
