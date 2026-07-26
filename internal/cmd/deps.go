package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// Flags shared by the commands that deal with system dependencies.
var (
	installDeps   bool
	depsVia       string
	skipDepChecks bool
)

// depsViaHelp describes the --deps-via flag.
var depsViaHelp = "Package manager to install system dependencies with (" +
	strings.Join(sysdeps.Names(), ", ") + "); defaults to the platform's"

// resolveSystemDeps checks the extension's declared native dependencies and
// returns the environment variables the build should be given so it can find
// them.
//
// An extension that declares nothing returns no environment and no error, which
// is the case for almost every extension — this must stay a no-op for them.
func resolveSystemDeps(dir string) ([]string, error) {
	if skipDepChecks {
		return nil, nil
	}

	manifest, err := sysdeps.Load(dir)
	if err != nil {
		return nil, err
	}
	if manifest.IsEmpty() {
		return nil, nil
	}

	prober := sysdeps.NewProber()
	results := prober.ProbeAll(manifest)

	if allSatisfied(results) {
		reportSatisfied(results)
		return sysdeps.BuildEnv(results, nil), nil
	}

	pm, err := selectPackageManager()
	if err != nil {
		return nil, err
	}

	if !installDeps {
		// Fail before the build rather than after, so the user gets an
		// actionable message instead of a compiler or linker error.
		return nil, fmt.Errorf("\n%s", sysdeps.Report(results, pm))
	}

	if pm == nil {
		return nil, fmt.Errorf("\n%s", sysdeps.Report(results, nil))
	}

	packages, unmapped := sysdeps.PackagesFor(pm, results)
	if len(unmapped) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: no %s package declared for: %s\n",
			pm.Name, strings.Join(unmapped, ", "))
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("\n%s", sysdeps.Report(results, pm))
	}

	fmt.Printf("Installing system dependencies with %s...\n", pm.Name)
	if err := pm.Install(packages); err != nil {
		return nil, err
	}

	// Re-probe: installing does not guarantee satisfaction. The common failure
	// is a distro package that is present but older than the extension needs.
	results = prober.ProbeAll(manifest)
	if !allSatisfied(results) {
		return nil, fmt.Errorf("system dependencies are still unsatisfied after installing:\n\n%s",
			sysdeps.Report(results, pm))
	}

	reportSatisfied(results)
	return sysdeps.BuildEnv(results, nil), nil
}

// selectPackageManager honours --deps-via, falling back to platform detection.
// A nil result means none was found, which callers report rather than treat as
// an error in its own right.
func selectPackageManager() (*sysdeps.PackageManager, error) {
	if depsVia == "" {
		return sysdeps.Detect(), nil
	}

	pm, err := sysdeps.ByName(depsVia)
	if err != nil {
		return nil, err
	}
	if !pm.Available() {
		return nil, fmt.Errorf("%s was requested via --deps-via but is not installed on this host", pm.Name)
	}
	return pm, nil
}

func allSatisfied(results []sysdeps.Result) bool {
	for _, r := range results {
		if !r.OK() {
			return false
		}
	}
	return true
}

func reportSatisfied(results []sysdeps.Result) {
	for _, r := range results {
		fmt.Printf("✓ %s\n", r.Summary())
	}
}
