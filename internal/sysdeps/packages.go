package sysdeps

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

// PackageManager is a platform package manager pgbrew can install through.
type PackageManager struct {
	// Name is the manifest key, e.g. "apt".
	Name string
	// Bin is the executable, e.g. "apt-get".
	Bin string
	// InstallArgs precede the package names, e.g. ["install", "-y"].
	InstallArgs []string
	// NeedsRoot says whether installing requires elevated privileges.
	NeedsRoot bool
}

// The package managers pgbrew knows about, in Linux detection order. Homebrew
// is last so a native manager wins by default, but it is available on Linux too
// (Linuxbrew) and can be selected explicitly — useful when the distro's package
// is older than the extension requires.
var (
	Apt    = &PackageManager{Name: "apt", Bin: "apt-get", InstallArgs: []string{"install", "-y"}, NeedsRoot: true}
	Dnf    = &PackageManager{Name: "dnf", Bin: "dnf", InstallArgs: []string{"install", "-y"}, NeedsRoot: true}
	Pacman = &PackageManager{Name: "pacman", Bin: "pacman", InstallArgs: []string{"-S", "--needed", "--noconfirm"}, NeedsRoot: true}
	Apk    = &PackageManager{Name: "apk", Bin: "apk", InstallArgs: []string{"add"}, NeedsRoot: true}
	Zypper = &PackageManager{Name: "zypper", Bin: "zypper", InstallArgs: []string{"install", "-y"}, NeedsRoot: true}
	Brew   = &PackageManager{Name: "brew", Bin: "brew", InstallArgs: []string{"install"}, NeedsRoot: false}
)

// linuxOrder is the detection order on Linux: native managers first, Homebrew
// as a fallback for hosts that have it.
var linuxOrder = []*PackageManager{Apt, Dnf, Pacman, Apk, Zypper, Brew}

// All is every known package manager, for name lookup and help text.
var All = []*PackageManager{Apt, Dnf, Pacman, Apk, Zypper, Brew}

// lookPath and geteuid are indirected so detection and privilege handling can
// be tested independently of the host and of the test runner's own uid.
var (
	lookPath = exec.LookPath
	geteuid  = os.Geteuid
)

// Detect returns the package manager to use on this host, or nil if none is
// available.
func Detect() *PackageManager {
	if runtime.GOOS == "darwin" {
		if _, err := lookPath(Brew.Bin); err == nil {
			return Brew
		}
		return nil
	}
	for _, pm := range linuxOrder {
		if _, err := lookPath(pm.Bin); err == nil {
			return pm
		}
	}
	return nil
}

// ByName looks up a package manager by its manifest key.
func ByName(name string) (*PackageManager, error) {
	for _, pm := range All {
		if pm.Name == name {
			return pm, nil
		}
	}
	return nil, fmt.Errorf("unknown package manager %q (known: %s)", name, strings.Join(Names(), ", "))
}

// Names lists the known package manager keys.
func Names() []string {
	names := make([]string, len(All))
	for i, pm := range All {
		names[i] = pm.Name
	}
	sort.Strings(names)
	return names
}

// Available reports whether this package manager's binary is on PATH.
func (pm *PackageManager) Available() bool {
	_, err := lookPath(pm.Bin)
	return err == nil
}

// Packages returns the package names this manager should install for dep.
func (pm *PackageManager) Packages(dep Dependency) []string {
	switch pm.Name {
	case Apt.Name:
		return dep.Packages.Apt
	case Dnf.Name:
		return dep.Packages.Dnf
	case Pacman.Name:
		return dep.Packages.Pacman
	case Apk.Name:
		return dep.Packages.Apk
	case Zypper.Name:
		return dep.Packages.Zypper
	case Brew.Name:
		// Fall back to the formula name, which is usually right and saves every
		// manifest from restating it.
		if len(dep.Packages.Brew) > 0 {
			return dep.Packages.Brew
		}
		return []string{dep.Formula()}
	}
	return nil
}

// useSudo reports whether the install command must be prefixed with sudo.
func (pm *PackageManager) useSudo() bool {
	return pm.NeedsRoot && geteuid() != 0
}

// InstallCommand builds the argv for installing the given packages.
func (pm *PackageManager) InstallCommand(packages []string) (string, []string) {
	args := append(append([]string{}, pm.InstallArgs...), packages...)
	if pm.useSudo() {
		return "sudo", append([]string{pm.Bin}, args...)
	}
	return pm.Bin, args
}

// CommandString renders the install command for display.
func (pm *PackageManager) CommandString(packages []string) string {
	bin, args := pm.InstallCommand(packages)
	return strings.TrimSpace(bin + " " + strings.Join(args, " "))
}

// Install runs the package manager to install the given packages.
func (pm *PackageManager) Install(packages []string) error {
	if len(packages) == 0 {
		return nil
	}
	bin, args := pm.InstallCommand(packages)
	fmt.Printf("==> %s\n", pm.CommandString(packages))

	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin // apt/dnf may prompt
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s install failed: %w", pm.Name, err)
	}
	return nil
}

// PackagesFor collects the packages needed for every unsatisfied result,
// deduplicated and in declaration order. Dependencies the manager has no
// package name for are returned separately so they can be reported rather than
// silently skipped.
func PackagesFor(pm *PackageManager, results []Result) (packages []string, unmapped []string) {
	seen := map[string]bool{}
	for _, r := range results {
		if r.OK() {
			continue
		}
		pkgs := pm.Packages(r.Dependency)
		if len(pkgs) == 0 {
			unmapped = append(unmapped, r.Dependency.Name)
			continue
		}
		for _, p := range pkgs {
			if !seen[p] {
				seen[p] = true
				packages = append(packages, p)
			}
		}
	}
	return packages, unmapped
}

// Report renders an actionable message for unsatisfied dependencies: what is
// missing, why, and the exact command that would fix it.
func Report(results []Result, pm *PackageManager) string {
	var b strings.Builder

	b.WriteString("Missing system dependencies:\n\n")
	for _, r := range results {
		if r.OK() {
			continue
		}
		switch {
		case !r.Found:
			fmt.Fprintf(&b, "  ✗ %s: not found\n", r.Dependency.Name)
		case r.TooOld:
			fmt.Fprintf(&b, "  ✗ %s: found %s at %s, but >= %s is required\n",
				r.Dependency.Name, r.Version, r.Prefix, r.Dependency.MinVersion)
		}
	}

	if pm == nil {
		b.WriteString("\nNo supported package manager was detected on this host.\n")
		b.WriteString("Install the libraries above manually, then re-run.\n")
		return b.String()
	}

	packages, unmapped := PackagesFor(pm, results)
	if len(packages) > 0 {
		b.WriteString("\nInstall with:\n")
		fmt.Fprintf(&b, "  %s\n", pm.CommandString(packages))
		b.WriteString("\nOr let pgbrew do it:\n")
		b.WriteString("  pgx install --install-deps <source>\n")
	}
	if len(unmapped) > 0 {
		fmt.Fprintf(&b, "\nNo %s package is declared for: %s\n", pm.Name, strings.Join(unmapped, ", "))
		b.WriteString("Install these manually, or set the extension's override variables.\n")
	}

	// A dependency that is present but too old is the case a native package
	// manager usually cannot fix — point at Homebrew, which tends to carry
	// newer libraries and works on Linux too.
	if pm.Name != Brew.Name && anyTooOld(results) {
		b.WriteString("\nThe installed version is too old and your distro may not carry a newer one.\n")
		b.WriteString("Homebrew usually does, and works on Linux:\n")
		b.WriteString("  pgx install --install-deps --deps-via brew <source>\n")
	}

	return b.String()
}

func anyTooOld(results []Result) bool {
	for _, r := range results {
		if r.TooOld {
			return true
		}
	}
	return false
}
