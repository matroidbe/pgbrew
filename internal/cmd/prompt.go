package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// depAction is what the user chose to do about missing system dependencies.
type depAction int

const (
	// actionInstall installs with the choice's package manager.
	actionInstall depAction = iota
	// actionPrintCommand prints the install command and stops.
	actionPrintCommand
	// actionSkip proceeds with the build regardless.
	actionSkip
	// actionAbort stops without doing anything.
	actionAbort
)

// depChoice is one entry in the interactive menu.
type depChoice struct {
	label   string
	detail  string
	action  depAction
	manager *sysdeps.PackageManager
}

// buildDepChoices assembles the menu for a set of unsatisfied dependencies.
//
// One install entry per package manager that could plausibly fix them, then the
// non-install options. The platform's own manager is first, so it is the
// default; when the problem is a too-old distro package, the alternative
// (usually Homebrew) is right there rather than something to go and read about.
func buildDepChoices(results []sysdeps.Result, managers []*sysdeps.PackageManager) []depChoice {
	var choices []depChoice

	tooOld := false
	for _, r := range results {
		if r.TooOld {
			tooOld = true
		}
	}

	for _, pm := range managers {
		packages, _ := sysdeps.PackagesFor(pm, results)
		if len(packages) == 0 {
			continue
		}
		label := "Install with " + pm.Name
		// When something is present but too old, the distro manager is usually
		// the one that cannot help and Homebrew is the one that can. Say so,
		// rather than leaving the user to guess which to pick.
		if tooOld {
			if pm.Name == sysdeps.Brew.Name {
				label += "  (usually carries newer versions)"
			} else if pm == managers[0] {
				label += "  (may not have a new enough version)"
			}
		}
		choices = append(choices, depChoice{
			label:   label,
			detail:  pm.CommandString(packages),
			action:  actionInstall,
			manager: pm,
		})
	}

	// The print-and-exit entry reports the default manager's command, so it
	// needs a manager attached even though it installs nothing.
	var defaultManager *sysdeps.PackageManager
	if len(managers) > 0 {
		defaultManager = managers[0]
	}

	choices = append(choices,
		depChoice{label: "Print the install command and exit", action: actionPrintCommand, manager: defaultManager},
		depChoice{label: "Continue anyway (skip the dependency check)", action: actionSkip},
		depChoice{label: "Abort", action: actionAbort},
	)
	return choices
}

// maxPromptAttempts bounds re-asking so a stream of unusable input cannot spin
// forever.
const maxPromptAttempts = 3

// promptDepChoice renders the menu and reads a selection.
//
// An empty line takes the first entry, which is always the most likely fix.
// EOF (a closed stdin, or Ctrl-D) aborts rather than proceeding, because the
// alternatives all change the machine or the build.
func promptDepChoice(out io.Writer, in io.Reader, choices []depChoice) (depChoice, error) {
	if len(choices) == 0 {
		return depChoice{}, fmt.Errorf("no choices to offer")
	}

	fmt.Fprintln(out, "How would you like to proceed?")
	fmt.Fprintln(out)
	width := 0
	for _, c := range choices {
		if len(c.label) > width {
			width = len(c.label)
		}
	}
	for i, c := range choices {
		if c.detail != "" {
			fmt.Fprintf(out, "  %d) %-*s   %s\n", i+1, width, c.label, c.detail)
		} else {
			fmt.Fprintf(out, "  %d) %s\n", i+1, c.label)
		}
	}
	fmt.Fprintln(out)

	reader := bufio.NewReader(in)
	for attempt := 0; attempt < maxPromptAttempts; attempt++ {
		fmt.Fprint(out, "Choice [1]: ")

		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			// EOF with nothing typed: no one is there to answer.
			fmt.Fprintln(out)
			return depChoice{label: "Abort", action: actionAbort}, nil
		}

		answer := strings.TrimSpace(line)
		if answer == "" {
			return choices[0], nil
		}
		if n, convErr := strconv.Atoi(answer); convErr == nil && n >= 1 && n <= len(choices) {
			return choices[n-1], nil
		}
		fmt.Fprintf(out, "Please enter a number between 1 and %d.\n", len(choices))
	}

	return depChoice{}, fmt.Errorf("no valid choice after %d attempts", maxPromptAttempts)
}

// interactive reports whether pgbrew can ask the user a question: both stdin
// and stdout must be a terminal. In a pipeline, a CI job or a Dockerfile they
// are not, and prompting would hang the build.
func interactive() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
