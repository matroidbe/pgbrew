package sysdeps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// occtDep is the pg_solid dependency, used across the probe tests.
func occtDep() Dependency {
	return Dependency{
		Name:       "opencascade",
		Header:     "opencascade/Standard_Version.hxx",
		Library:    "TKernel",
		MinVersion: "7.6",
		Version: &VersionSpec{
			Major: "OCC_VERSION_MAJOR",
			Minor: "OCC_VERSION_MINOR",
			Patch: "OCC_VERSION_MAINTENANCE",
		},
		EnvVars: []string{"OCCT_ROOT"},
		Env:     map[string]string{"OCCT_ROOT": "{prefix}"},
	}
}

func versionHeader(major, minor, patch string) string {
	return "#define OCC_VERSION_MAJOR " + major + "\n" +
		"#define OCC_VERSION_MINOR " + minor + "\n" +
		"#define OCC_VERSION_MAINTENANCE " + patch + "\n"
}

// fakePrefix builds an installation prefix: <root>/include/<header> and
// <root>/<libSubdir>/libTKernel.so.
func fakePrefix(t *testing.T, libSubdir, header string) string {
	t.Helper()
	root := t.TempDir()

	incPath := filepath.Join(root, "include", "opencascade", "Standard_Version.hxx")
	if err := os.MkdirAll(filepath.Dir(incPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(incPath, []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}

	libDir := filepath.Join(root, libSubdir)
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "libTKernel.so"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// testProber returns a Prober isolated from the host: no brew, no environment.
func testProber(prefixes ...string) *Prober {
	return &Prober{
		Prefixes:   prefixes,
		Arch:       "x86_64",
		BrewPrefix: func(string) (string, bool) { return "", false },
		LookupEnv:  func(string) (string, bool) { return "", false },
	}
}

func TestProbeFindsDependencyInPrefix(t *testing.T) {
	root := fakePrefix(t, "lib", versionHeader("7", "6", "3"))

	r := testProber(root).Probe(occtDep())
	if !r.Found {
		t.Fatal("expected the dependency to be found")
	}
	if !r.OK() {
		t.Errorf("expected OK, got %s", r.Summary())
	}
	if r.Source != SourcePrefix {
		t.Errorf("source = %q", r.Source)
	}
	if r.Prefix != root {
		t.Errorf("prefix = %q, want %q", r.Prefix, root)
	}
	if !r.VersionKnown || r.Version.String() != "7.6.3" {
		t.Errorf("version = %v (known=%v), want 7.6.3", r.Version, r.VersionKnown)
	}
	if r.IncludeDir != filepath.Join(root, "include") {
		t.Errorf("include dir = %q", r.IncludeDir)
	}
	if r.LibDir != filepath.Join(root, "lib") {
		t.Errorf("lib dir = %q", r.LibDir)
	}
}

func TestProbeDetectsTooOld(t *testing.T) {
	// The motivating case inverted: a manifest wanting 7.8 on a 7.6 host.
	root := fakePrefix(t, "lib", versionHeader("7", "6", "3"))
	dep := occtDep()
	dep.MinVersion = "7.8"

	r := testProber(root).Probe(dep)
	if !r.Found {
		t.Fatal("expected the dependency to be found")
	}
	if !r.TooOld {
		t.Error("expected TooOld for 7.6.3 against min 7.8")
	}
	if r.OK() {
		t.Error("a too-old dependency must not be OK")
	}
	if !strings.Contains(r.Summary(), "too old") {
		t.Errorf("summary should say why: %q", r.Summary())
	}
}

func TestProbeAcceptsExactMinVersion(t *testing.T) {
	root := fakePrefix(t, "lib", versionHeader("7", "6", "0"))
	dep := occtDep() // min 7.6
	if r := testProber(root).Probe(dep); !r.OK() {
		t.Errorf("7.6.0 should satisfy >= 7.6: %s", r.Summary())
	}
}

func TestProbeNotFound(t *testing.T) {
	r := testProber(t.TempDir()).Probe(occtDep())
	if r.Found {
		t.Error("expected not found")
	}
	if r.OK() {
		t.Error("a missing dependency must not be OK")
	}
	if !strings.Contains(r.Summary(), "not found") {
		t.Errorf("summary = %q", r.Summary())
	}
}

func TestProbeRequiresBothHeaderAndLibrary(t *testing.T) {
	// A prefix with headers but no library is not a usable installation.
	root := t.TempDir()
	incPath := filepath.Join(root, "include", "opencascade", "Standard_Version.hxx")
	if err := os.MkdirAll(filepath.Dir(incPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(incPath, []byte(versionHeader("7", "8", "0")), 0o644); err != nil {
		t.Fatal(err)
	}

	if r := testProber(root).Probe(occtDep()); r.Found {
		t.Error("a prefix with headers but no library must not count as found")
	}
}

func TestProbeFindsMultiarchAndLib64(t *testing.T) {
	for _, subdir := range []string{"lib64", filepath.Join("lib", "x86_64-linux-gnu")} {
		t.Run(subdir, func(t *testing.T) {
			root := fakePrefix(t, subdir, versionHeader("7", "6", "3"))
			r := testProber(root).Probe(occtDep())
			if !r.Found {
				t.Fatalf("expected to find the library in %s", subdir)
			}
			if r.LibDir != filepath.Join(root, subdir) {
				t.Errorf("lib dir = %q, want %q", r.LibDir, filepath.Join(root, subdir))
			}
		})
	}
}

func TestProbeEnvOverrideWins(t *testing.T) {
	systemRoot := fakePrefix(t, "lib", versionHeader("7", "6", "0"))
	overrideRoot := fakePrefix(t, "lib", versionHeader("7", "8", "1"))

	p := testProber(systemRoot)
	p.LookupEnv = func(key string) (string, bool) {
		if key == "OCCT_ROOT" {
			return overrideRoot, true
		}
		return "", false
	}

	r := p.Probe(occtDep())
	if r.Source != SourceEnv {
		t.Errorf("source = %q, want the environment override to win", r.Source)
	}
	if r.Prefix != overrideRoot {
		t.Errorf("prefix = %q, want %q", r.Prefix, overrideRoot)
	}
	if r.Version.String() != "7.8.1" {
		t.Errorf("version = %v", r.Version)
	}
}

func TestProbeEnvOverrideIsTrustedEvenWhenUnrecognisable(t *testing.T) {
	// The variable may name something the extension's own build understands but
	// pgbrew cannot parse (e.g. an include dir, not a prefix). Refusing to build
	// would be obstructive, so we accept it and skip version enforcement.
	dir := t.TempDir()
	dep := occtDep()
	dep.MinVersion = "99.0"

	p := testProber()
	p.LookupEnv = func(key string) (string, bool) {
		if key == "OCCT_ROOT" {
			return dir, true
		}
		return "", false
	}

	r := p.Probe(dep)
	if !r.Found || !r.OK() {
		t.Errorf("an explicit override should be accepted: %s", r.Summary())
	}
	if r.VersionKnown {
		t.Error("version should be unknown for an unrecognisable override")
	}
}

func TestProbeIgnoresEmptyAndMissingEnvOverride(t *testing.T) {
	root := fakePrefix(t, "lib", versionHeader("7", "6", "3"))
	p := testProber(root)
	p.LookupEnv = func(key string) (string, bool) {
		if key == "OCCT_ROOT" {
			return "   ", true // set but blank
		}
		return "", false
	}

	if r := p.Probe(occtDep()); r.Source != SourcePrefix {
		t.Errorf("a blank override should be ignored, source = %q", r.Source)
	}
}

func TestProbePrefersBrewOverSystemPrefix(t *testing.T) {
	// A brew-installed copy is usually newer than the distro's, so it is checked
	// first. This is what makes `brew install opencascade` sufficient on a host
	// whose distro package is too old.
	systemRoot := fakePrefix(t, "lib", versionHeader("7", "6", "0"))
	brewRoot := fakePrefix(t, "lib", versionHeader("7", "8", "1"))

	p := testProber(systemRoot)
	p.BrewPrefix = func(formula string) (string, bool) {
		if formula != "opencascade" {
			t.Errorf("brew asked for formula %q", formula)
		}
		return brewRoot, true
	}

	r := p.Probe(occtDep())
	if r.Source != SourceBrew {
		t.Errorf("source = %q, want homebrew", r.Source)
	}
	if r.Version.String() != "7.8.1" {
		t.Errorf("version = %v", r.Version)
	}
}

func TestProbeFallsBackWhenBrewFormulaAbsent(t *testing.T) {
	root := fakePrefix(t, "lib", versionHeader("7", "6", "3"))
	p := testProber(root)
	// brew is installed but does not have this formula.
	p.BrewPrefix = func(string) (string, bool) { return "", false }

	if r := p.Probe(occtDep()); r.Source != SourcePrefix {
		t.Errorf("source = %q, want the system prefix", r.Source)
	}
}

func TestProbeDependencyPrefixesComeFirst(t *testing.T) {
	declared := fakePrefix(t, "lib", versionHeader("7", "9", "0"))
	fallback := fakePrefix(t, "lib", versionHeader("7", "6", "0"))

	dep := occtDep()
	dep.Prefixes = []string{declared}

	r := testProber(fallback).Probe(dep)
	if r.Prefix != declared {
		t.Errorf("prefix = %q, want the manifest-declared prefix %q", r.Prefix, declared)
	}
}

func TestProbeAll(t *testing.T) {
	root := fakePrefix(t, "lib", versionHeader("7", "6", "3"))
	m := &Manifest{SystemDependencies: []Dependency{
		occtDep(),
		{Name: "absent", Library: "NoSuchLib"},
	}}

	results := testProber(root).ProbeAll(m)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if !results[0].OK() {
		t.Errorf("opencascade should be OK: %s", results[0].Summary())
	}
	if results[1].OK() {
		t.Errorf("absent dependency should not be OK: %s", results[1].Summary())
	}
}

func TestBuildEnv(t *testing.T) {
	root := fakePrefix(t, "lib", versionHeader("7", "6", "3"))
	dep := occtDep()
	dep.Env = map[string]string{
		"OCCT_ROOT":        "{prefix}",
		"OCCT_INCLUDE_DIR": "{include}",
		"OCCT_LIB_DIR":     "{lib}",
	}

	r := testProber(root).Probe(dep)
	env := BuildEnv([]Result{r}, func(string) (string, bool) { return "", false })

	want := map[string]string{
		"OCCT_ROOT":        root,
		"OCCT_INCLUDE_DIR": filepath.Join(root, "include"),
		"OCCT_LIB_DIR":     filepath.Join(root, "lib"),
	}
	got := map[string]string{}
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		got[parts[0]] = parts[1]
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestBuildEnvNeverOverridesUserSetVariables(t *testing.T) {
	// If the user already exported OCCT_ROOT, our detected value must not
	// silently replace it.
	root := fakePrefix(t, "lib", versionHeader("7", "6", "3"))
	r := testProber(root).Probe(occtDep())

	env := BuildEnv([]Result{r}, func(key string) (string, bool) {
		if key == "OCCT_ROOT" {
			return "/user/choice", true
		}
		return "", false
	})
	for _, kv := range env {
		if strings.HasPrefix(kv, "OCCT_ROOT=") {
			t.Errorf("BuildEnv overrode a user-set variable: %q", kv)
		}
	}
}

func TestBuildEnvSkipsUnsatisfiedDependencies(t *testing.T) {
	r := testProber(t.TempDir()).Probe(occtDep()) // not found
	if env := BuildEnv([]Result{r}, func(string) (string, bool) { return "", false }); len(env) != 0 {
		t.Errorf("expected no env from an unsatisfied dependency, got %v", env)
	}
}

func TestExpandPlaceholdersSkipsUnresolvedPaths(t *testing.T) {
	// A dependency probed by library only has no include dir; a template
	// referencing {include} must be skipped rather than exported empty.
	r := Result{Prefix: "/opt/x", LibDir: "/opt/x/lib"}
	if got := expandPlaceholders("{include}", r); got != "" {
		t.Errorf("got %q, want empty for an unresolved placeholder", got)
	}
	if got := expandPlaceholders("{prefix}/share", r); got != "/opt/x/share" {
		t.Errorf("got %q", got)
	}
}

func TestMultiarchTagIsPlausible(t *testing.T) {
	if tag := multiarchTag(); tag == "" {
		t.Error("multiarchTag returned empty")
	}
}
