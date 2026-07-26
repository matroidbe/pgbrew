package sysdeps

import (
	"errors"
	"strings"
	"testing"
)

// stubLookPath makes only the named binaries appear to be on PATH.
func stubLookPath(t *testing.T, available ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, name := range available {
		set[name] = true
	}
	original := lookPath
	lookPath = func(file string) (string, error) {
		if set[file] {
			return "/usr/bin/" + file, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = original })
}

// stubEuid pins the effective uid so sudo behaviour is independent of whoever
// runs the tests (CI often runs as root, developers usually do not).
func stubEuid(t *testing.T, uid int) {
	t.Helper()
	original := geteuid
	geteuid = func() int { return uid }
	t.Cleanup(func() { geteuid = original })
}

func TestDetectPrefersNativeManagerOverBrew(t *testing.T) {
	// A Linux host with both apt and brew should use apt: installing into the
	// system prefix is what an operator expects by default.
	stubLookPath(t, "apt-get", "brew")
	if pm := detectLinux(); pm != Apt {
		t.Errorf("detected %v, want apt", pm)
	}
}

func TestDetectFallsBackToBrewOnLinux(t *testing.T) {
	// Linuxbrew on a host with no recognised native manager.
	stubLookPath(t, "brew")
	if pm := detectLinux(); pm != Brew {
		t.Errorf("detected %v, want brew", pm)
	}
}

func TestDetectReturnsNilWhenNothingAvailable(t *testing.T) {
	stubLookPath(t)
	if pm := detectLinux(); pm != nil {
		t.Errorf("detected %v, want nil", pm)
	}
}

func TestDetectOrder(t *testing.T) {
	stubLookPath(t, "dnf", "pacman")
	if pm := detectLinux(); pm != Dnf {
		t.Errorf("detected %v, want dnf to win over pacman", pm)
	}
}

// detectLinux exercises the Linux branch of Detect regardless of the host OS,
// so the ordering logic is testable on macOS too.
func detectLinux() *PackageManager {
	for _, pm := range linuxOrder {
		if _, err := lookPath(pm.Bin); err == nil {
			return pm
		}
	}
	return nil
}

func TestByName(t *testing.T) {
	pm, err := ByName("brew")
	if err != nil || pm != Brew {
		t.Errorf("ByName(brew) = %v, %v", pm, err)
	}
	if _, err := ByName("nope"); err == nil {
		t.Error("expected an error for an unknown manager")
	} else if !strings.Contains(err.Error(), "apt") {
		t.Errorf("error should list the known managers: %v", err)
	}
}

func TestPackagesPerManager(t *testing.T) {
	dep := Dependency{
		Name: "opencascade",
		Packages: Packages{
			Apt: []string{"libocct-foundation-dev", "libocct-data-exchange-dev"},
			Dnf: []string{"opencascade-devel"},
		},
	}

	if got := Apt.Packages(dep); len(got) != 2 || got[0] != "libocct-foundation-dev" {
		t.Errorf("apt packages = %v", got)
	}
	if got := Dnf.Packages(dep); len(got) != 1 || got[0] != "opencascade-devel" {
		t.Errorf("dnf packages = %v", got)
	}
	if got := Pacman.Packages(dep); len(got) != 0 {
		t.Errorf("pacman packages = %v, want none declared", got)
	}
}

func TestBrewPackagesDefaultToFormula(t *testing.T) {
	// Saves every manifest from restating the formula name.
	dep := Dependency{Name: "occt", BrewFormula: "opencascade"}
	if got := Brew.Packages(dep); len(got) != 1 || got[0] != "opencascade" {
		t.Errorf("brew packages = %v, want [opencascade]", got)
	}

	explicit := Dependency{Name: "occt", Packages: Packages{Brew: []string{"opencascade@7.8"}}}
	if got := Brew.Packages(explicit); got[0] != "opencascade@7.8" {
		t.Errorf("brew packages = %v, want the explicit list to win", got)
	}
}

func TestInstallCommandUsesSudoForRootManagers(t *testing.T) {
	stubEuid(t, 1000)

	bin, args := Apt.InstallCommand([]string{"libfoo-dev"})
	if bin != "sudo" || args[0] != "apt-get" {
		t.Errorf("got %s %v, want sudo apt-get ...", bin, args)
	}
	if got := Apt.CommandString([]string{"libfoo-dev"}); got != "sudo apt-get install -y libfoo-dev" {
		t.Errorf("CommandString = %q", got)
	}
}

func TestInstallCommandSkipsSudoWhenRoot(t *testing.T) {
	stubEuid(t, 0)
	if bin, _ := Apt.InstallCommand([]string{"libfoo-dev"}); bin != "apt-get" {
		t.Errorf("bin = %q, want no sudo when already root", bin)
	}
}

func TestBrewNeverUsesSudo(t *testing.T) {
	// Homebrew refuses to run under sudo, so this must hold at any uid.
	for _, uid := range []int{0, 1000} {
		stubEuid(t, uid)
		if bin, _ := Brew.InstallCommand([]string{"opencascade"}); bin != "brew" {
			t.Errorf("uid %d: bin = %q, want brew", uid, bin)
		}
	}
}

func TestPackagesForDeduplicatesAndReportsUnmapped(t *testing.T) {
	shared := Dependency{Name: "a", Packages: Packages{Apt: []string{"libcommon-dev", "liba-dev"}}}
	overlapping := Dependency{Name: "b", Packages: Packages{Apt: []string{"libcommon-dev", "libb-dev"}}}
	unmapped := Dependency{Name: "c"} // no apt package declared

	results := []Result{
		{Dependency: shared},
		{Dependency: overlapping},
		{Dependency: unmapped},
	}

	packages, missing := PackagesFor(Apt, results)
	want := []string{"libcommon-dev", "liba-dev", "libb-dev"}
	if len(packages) != len(want) {
		t.Fatalf("packages = %v, want %v", packages, want)
	}
	for i := range want {
		if packages[i] != want[i] {
			t.Errorf("packages = %v, want %v (declaration order, deduplicated)", packages, want)
		}
	}
	if len(missing) != 1 || missing[0] != "c" {
		t.Errorf("unmapped = %v, want [c]", missing)
	}
}

func TestPackagesForSkipsSatisfiedDependencies(t *testing.T) {
	results := []Result{
		{Dependency: Dependency{Name: "ok", Packages: Packages{Apt: []string{"lib-ok"}}}, Found: true},
		{Dependency: Dependency{Name: "missing", Packages: Packages{Apt: []string{"lib-missing"}}}},
	}
	packages, _ := PackagesFor(Apt, results)
	if len(packages) != 1 || packages[0] != "lib-missing" {
		t.Errorf("packages = %v, want only the unsatisfied one", packages)
	}
}

func TestReportIsActionable(t *testing.T) {
	stubEuid(t, 1000)
	results := []Result{{
		Dependency: Dependency{
			Name:     "opencascade",
			Packages: Packages{Apt: []string{"libocct-foundation-dev"}},
		},
	}}

	report := Report(results, Apt)
	for _, want := range []string{
		"opencascade: not found",
		"sudo apt-get install -y libocct-foundation-dev",
		"--install-deps",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
}

func TestReportSuggestsBrewWhenTheDistroPackageIsTooOld(t *testing.T) {
	// The Ubuntu-24.04-ships-OCCT-7.6 case: apt cannot fix it, brew can.
	stubEuid(t, 1000)
	results := []Result{{
		Dependency: Dependency{
			Name:       "opencascade",
			MinVersion: "7.8",
			Packages:   Packages{Apt: []string{"libocct-foundation-dev"}},
		},
		Found:        true,
		Version:      Version{7, 6, 3},
		VersionKnown: true,
		TooOld:       true,
		Prefix:       "/usr",
	}}

	report := Report(results, Apt)
	if !strings.Contains(report, "found 7.6.3") || !strings.Contains(report, ">= 7.8") {
		t.Errorf("report should explain the version gap:\n%s", report)
	}
	if !strings.Contains(report, "--deps-via brew") {
		t.Errorf("report should suggest brew for a too-old distro package:\n%s", report)
	}
}

func TestReportDoesNotSuggestBrewWhenAlreadyUsingBrew(t *testing.T) {
	results := []Result{{
		Dependency:   Dependency{Name: "opencascade", MinVersion: "9.0"},
		Found:        true,
		Version:      Version{7, 8, 1},
		VersionKnown: true,
		TooOld:       true,
	}}
	if report := Report(results, Brew); strings.Contains(report, "--deps-via brew") {
		t.Errorf("should not suggest brew when brew is already selected:\n%s", report)
	}
}

func TestReportWithoutPackageManager(t *testing.T) {
	results := []Result{{Dependency: Dependency{Name: "opencascade"}}}
	report := Report(results, nil)
	if !strings.Contains(report, "No supported package manager") {
		t.Errorf("report = %q", report)
	}
}

func TestAvailableUsesLookPath(t *testing.T) {
	stubLookPath(t, "brew")
	if !Brew.Available() {
		t.Error("brew should be available")
	}
	if Apt.Available() {
		t.Error("apt should not be available")
	}
}

// Guard against a typo in a manager definition making InstallCommand produce
// something that could not run.
func TestEveryManagerHasABinaryAndInstallArgs(t *testing.T) {
	for _, pm := range All {
		if pm.Name == "" || pm.Bin == "" || len(pm.InstallArgs) == 0 {
			t.Errorf("incomplete package manager definition: %+v", pm)
		}
	}
}
