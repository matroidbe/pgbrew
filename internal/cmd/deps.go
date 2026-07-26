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

// maxResolveRounds bounds the interactive install/re-probe loop.
const maxResolveRounds = 3

// resolveSystemDeps checks the extension's declared native dependencies and
// returns the environment variables the build should be given so it can find
// them.
//
// An extension that declares nothing returns no environment and no error, which
// is the case for almost every extension — this must stay a no-op for them.
//
// When something is missing there are three paths: --install-deps installs
// without asking (for scripts and CI), a terminal session gets a menu of
// options, and anything else gets a report and a failure.
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

	// --install-deps: an explicit, unattended instruction. No prompt even on a
	// terminal, because the user has already answered the question.
	if installDeps {
		pm, err := selectPackageManager()
		if err != nil {
			return nil, err
		}
		if pm == nil {
			return nil, fmt.Errorf("\n%s", sysdeps.Report(results, nil))
		}

		results, err = attemptInstall(pm, manifest, prober, results)
		if err != nil {
			return nil, err
		}
		if !allSatisfied(results) {
			return nil, fmt.Errorf("system dependencies are still unsatisfied after installing:\n\n%s",
				sysdeps.Report(results, pm))
		}
		reportSatisfied(results)
		return sysdeps.BuildEnv(results, nil), nil
	}

	if interactive() {
		return resolveInteractively(manifest, prober, results)
	}

	// Nobody to ask: report what is missing and how to fix it, and fail before
	// the build rather than after.
	pm, _ := selectPackageManager()
	return nil, fmt.Errorf("\n%s", sysdeps.Report(results, pm))
}

// resolveInteractively offers a menu, acting on the choice and re-offering it
// if an install did not resolve everything — the usual case being a distro
// package that turned out to be too old, where the next move is Homebrew.
func resolveInteractively(manifest *sysdeps.Manifest, prober *sysdeps.Prober, results []sysdeps.Result) ([]string, error) {
	for round := 0; round < maxResolveRounds; round++ {
		fmt.Println()
		fmt.Print(sysdeps.MissingReport(results))
		fmt.Println()

		choices := buildDepChoices(results, sysdeps.CandidateManagers(results))
		choice, err := promptDepChoice(os.Stdout, os.Stdin, choices)
		if err != nil {
			return nil, err
		}

		switch choice.action {
		case actionInstall:
			fmt.Println()
			results, err = attemptInstall(choice.manager, manifest, prober, results)
			if err != nil {
				// Not fatal: another manager may still be able to help, so show
				// the problem and come back to the menu.
				fmt.Fprintf(os.Stderr, "\n%v\n", err)
				continue
			}
			if allSatisfied(results) {
				reportSatisfied(results)
				return sysdeps.BuildEnv(results, nil), nil
			}
			fmt.Println("\nThat did not satisfy everything.")

		case actionPrintCommand:
			if choice.manager != nil {
				packages, _ := sysdeps.PackagesFor(choice.manager, results)
				fmt.Println()
				fmt.Println(choice.manager.CommandString(packages))
			}
			return nil, fmt.Errorf("system dependencies not installed; re-run once they are")

		case actionSkip:
			fmt.Fprintln(os.Stderr, "\nWarning: continuing without the required system dependencies.")
			fmt.Fprintln(os.Stderr, "The build will probably fail at the compiler or linker.")
			return nil, nil

		case actionAbort:
			return nil, fmt.Errorf("aborted at user request")
		}
	}

	return nil, fmt.Errorf("system dependencies are still unsatisfied after %d attempts", maxResolveRounds)
}

// attemptInstall installs the packages for the unsatisfied dependencies and
// re-probes.
//
// Re-probing is the point: installing does not prove the dependency is now
// usable. The common failure is a distro package that installs happily but is
// older than the extension requires.
func attemptInstall(
	pm *sysdeps.PackageManager,
	manifest *sysdeps.Manifest,
	prober *sysdeps.Prober,
	results []sysdeps.Result,
) ([]sysdeps.Result, error) {
	packages, unmapped := sysdeps.PackagesFor(pm, results)
	if len(unmapped) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: no %s package declared for: %s\n",
			pm.Name, strings.Join(unmapped, ", "))
	}
	if len(packages) == 0 {
		return results, fmt.Errorf("no %s packages are declared for the missing dependencies", pm.Name)
	}

	fmt.Printf("Installing system dependencies with %s...\n", pm.Name)
	if err := pm.Install(packages); err != nil {
		return results, err
	}

	return prober.ProbeAll(manifest), nil
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
