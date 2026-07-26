package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

func missingResult(name string, packages sysdeps.Packages) sysdeps.Result {
	return sysdeps.Result{
		Dependency: sysdeps.Dependency{Name: name, Packages: packages},
	}
}

func tooOldResult(name string, packages sysdeps.Packages) sysdeps.Result {
	return sysdeps.Result{
		Dependency: sysdeps.Dependency{
			Name:       name,
			MinVersion: "7.8",
			Packages:   packages,
		},
		Found:        true,
		Version:      sysdeps.Version{7, 6, 3},
		VersionKnown: true,
		TooOld:       true,
		Prefix:       "/usr",
	}
}

func TestBuildDepChoicesOffersEachManagerThenTheFallbacks(t *testing.T) {
	results := []sysdeps.Result{missingResult("opencascade", sysdeps.Packages{
		Apt:  []string{"libocct-foundation-dev"},
		Brew: []string{"opencascade"},
	})}

	choices := buildDepChoices(results, []*sysdeps.PackageManager{sysdeps.Apt, sysdeps.Brew})

	if len(choices) != 5 {
		t.Fatalf("got %d choices, want 5 (2 managers + print + skip + abort)", len(choices))
	}
	if choices[0].action != actionInstall || choices[0].manager != sysdeps.Apt {
		t.Errorf("first choice should install with the platform manager, got %+v", choices[0])
	}
	if choices[1].manager != sysdeps.Brew {
		t.Errorf("second choice should be brew, got %+v", choices[1])
	}
	for i, want := range map[int]depAction{2: actionPrintCommand, 3: actionSkip, 4: actionAbort} {
		if choices[i].action != want {
			t.Errorf("choice %d action = %v, want %v", i, choices[i].action, want)
		}
	}
}

func TestBuildDepChoicesShowsTheInstallCommandAsDetail(t *testing.T) {
	results := []sysdeps.Result{missingResult("lz4", sysdeps.Packages{Apt: []string{"liblz4-dev"}})}
	choices := buildDepChoices(results, []*sysdeps.PackageManager{sysdeps.Apt})

	if !strings.Contains(choices[0].detail, "liblz4-dev") {
		t.Errorf("install choice should show the command, got %q", choices[0].detail)
	}
}

func TestBuildDepChoicesAnnotatesVersionMismatch(t *testing.T) {
	// The whole point of offering a second manager: when the distro package is
	// too old, the menu should say which option can actually help.
	results := []sysdeps.Result{tooOldResult("opencascade", sysdeps.Packages{
		Apt:  []string{"libocct-foundation-dev"},
		Brew: []string{"opencascade"},
	})}

	choices := buildDepChoices(results, []*sysdeps.PackageManager{sysdeps.Apt, sysdeps.Brew})

	if !strings.Contains(choices[0].label, "may not have a new enough version") {
		t.Errorf("distro manager should be flagged as possibly insufficient: %q", choices[0].label)
	}
	if !strings.Contains(choices[1].label, "newer versions") {
		t.Errorf("brew should be flagged as the likely fix: %q", choices[1].label)
	}
}

func TestBuildDepChoicesWithNoUsableManager(t *testing.T) {
	// Nothing can install it, but the user can still print, skip or abort.
	results := []sysdeps.Result{missingResult("mystery", sysdeps.Packages{})}
	choices := buildDepChoices(results, nil)

	if len(choices) != 3 {
		t.Fatalf("got %d choices, want 3", len(choices))
	}
	if choices[0].action != actionPrintCommand {
		t.Errorf("first choice = %v", choices[0].action)
	}
	if choices[0].manager != nil {
		t.Errorf("print choice should have no manager when none is available")
	}
}

func TestBuildDepChoicesSkipsManagersWithoutPackages(t *testing.T) {
	// dnf is available in this hypothetical, but the manifest declares no dnf
	// package, so offering it would be a dead end.
	results := []sysdeps.Result{missingResult("opencascade", sysdeps.Packages{Apt: []string{"libocct-dev"}})}
	choices := buildDepChoices(results, []*sysdeps.PackageManager{sysdeps.Apt, sysdeps.Dnf})

	for _, c := range choices {
		if c.action == actionInstall && c.manager == sysdeps.Dnf {
			t.Error("dnf should not be offered when no dnf package is declared")
		}
	}
}

func choicesForPromptTest() []depChoice {
	return []depChoice{
		{label: "Install with apt", detail: "apt-get install -y libfoo", action: actionInstall, manager: sysdeps.Apt},
		{label: "Install with brew", detail: "brew install foo", action: actionInstall, manager: sysdeps.Brew},
		{label: "Print the install command and exit", action: actionPrintCommand},
		{label: "Continue anyway", action: actionSkip},
		{label: "Abort", action: actionAbort},
	}
}

func TestPromptDepChoiceSelectsByNumber(t *testing.T) {
	for input, wantIdx := range map[string]int{
		"1\n": 0,
		"2\n": 1,
		"3\n": 2,
		"4\n": 3,
		"5\n": 4,
		// Surrounding whitespace is a typo, not a refusal to answer.
		"  2  \n": 1,
	} {
		var out bytes.Buffer
		choices := choicesForPromptTest()
		got, err := promptDepChoice(&out, strings.NewReader(input), choices)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if got.label != choices[wantIdx].label {
			t.Errorf("input %q selected %q, want %q", input, got.label, choices[wantIdx].label)
		}
	}
}

func TestPromptDepChoiceEmptyLineTakesTheFirstOption(t *testing.T) {
	var out bytes.Buffer
	got, err := promptDepChoice(&out, strings.NewReader("\n"), choicesForPromptTest())
	if err != nil {
		t.Fatal(err)
	}
	if got.action != actionInstall || got.manager != sysdeps.Apt {
		t.Errorf("empty input should take the default, got %+v", got)
	}
	if !strings.Contains(out.String(), "[1]") {
		t.Errorf("prompt should show the default: %q", out.String())
	}
}

func TestPromptDepChoiceEOFAborts(t *testing.T) {
	// A closed stdin must not be read as consent to install or to skip.
	var out bytes.Buffer
	got, err := promptDepChoice(&out, strings.NewReader(""), choicesForPromptTest())
	if err != nil {
		t.Fatal(err)
	}
	if got.action != actionAbort {
		t.Errorf("EOF should abort, got %v", got.action)
	}
}

func TestPromptDepChoiceRejectsOutOfRangeAndRetries(t *testing.T) {
	var out bytes.Buffer
	got, err := promptDepChoice(&out, strings.NewReader("9\nbanana\n2\n"), choicesForPromptTest())
	if err != nil {
		t.Fatal(err)
	}
	if got.manager != sysdeps.Brew {
		t.Errorf("expected the third attempt to win, got %+v", got)
	}
	if !strings.Contains(out.String(), "between 1 and 5") {
		t.Errorf("should explain the valid range: %q", out.String())
	}
}

func TestPromptDepChoiceGivesUpAfterRepeatedGarbage(t *testing.T) {
	var out bytes.Buffer
	_, err := promptDepChoice(&out, strings.NewReader("x\ny\nz\nq\n"), choicesForPromptTest())
	if err == nil {
		t.Error("expected an error after repeated invalid input, not an infinite loop")
	}
}

func TestPromptDepChoiceRendersEveryOption(t *testing.T) {
	var out bytes.Buffer
	choices := choicesForPromptTest()
	if _, err := promptDepChoice(&out, strings.NewReader("1\n"), choices); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	for _, c := range choices {
		if !strings.Contains(rendered, c.label) {
			t.Errorf("menu is missing %q:\n%s", c.label, rendered)
		}
	}
	if !strings.Contains(rendered, "apt-get install -y libfoo") {
		t.Errorf("menu should show install commands:\n%s", rendered)
	}
}

func TestPromptDepChoiceWithNoChoicesErrors(t *testing.T) {
	var out bytes.Buffer
	if _, err := promptDepChoice(&out, strings.NewReader("1\n"), nil); err == nil {
		t.Error("expected an error when there is nothing to choose from")
	}
}
