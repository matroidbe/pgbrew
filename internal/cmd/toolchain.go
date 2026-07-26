package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/matroidbe/pgbrew/internal/cargocfg"
)

// skipToolchainCheck bypasses the cargo configuration check.
var skipToolchainCheck bool

// checkCargoToolchain verifies that the tools a Rust project's cargo
// configuration asks for are installed.
//
// These requirements are invisible to a generic toolchain check. A project can
// name a linker, a compiler wrapper or an environment path in
// `.cargo/config.toml`, and none of that shows up in "is Rust installed". The
// result is a build that fails within seconds of starting, after `doctor` has
// already reported everything as fine.
func checkCargoToolchain(dir string) error {
	if skipToolchainCheck {
		return nil
	}

	issues, err := cargoToolchainIssues(dir)
	if err != nil {
		// A malformed cargo config is cargo's to complain about, and it will do
		// so far better than we can. Do not pre-empt it with a worse message.
		fmt.Fprintf(os.Stderr, "Warning: could not read cargo configuration: %v\n", err)
		return nil
	}
	if len(issues) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("\nThis project's cargo configuration requires tools that are not installed:\n\n")
	for _, issue := range issues {
		fmt.Fprintf(&b, "  ✗ %s\n", issue)
	}
	b.WriteString("\nThe build would fail almost immediately. Install them, or bypass this\n")
	b.WriteString("check with --skip-toolchain-check.\n")

	return fmt.Errorf("%s", b.String())
}

// cargoToolchainIssues returns the unmet cargo configuration requirements.
func cargoToolchainIssues(dir string) ([]cargocfg.Issue, error) {
	files, err := cargocfg.Discover(dir)
	if err != nil {
		return nil, err
	}
	return cargocfg.NewChecker().Check(files), nil
}

// reportCargoToolchain prints the cargo configuration status for doctor and
// reports whether everything it requires is present.
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

	issues := cargocfg.NewChecker().Check(files)
	if len(issues) == 0 {
		fmt.Printf("✓ all tools required by %d config file(s) are installed\n", len(files))
		return true
	}
	for _, issue := range issues {
		fmt.Printf("✗ %s\n", issue)
	}
	return false
}
