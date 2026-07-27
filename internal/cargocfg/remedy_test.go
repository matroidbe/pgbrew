package cargocfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// macChecker is a Mac looking at a Linux-authored config: the host triple is
// darwin, so the Linux [target] section does not apply, and neither sccache
// nor /usr/lib/llvm-20/lib exists.
func macChecker(t *testing.T, libclang string) *Checker {
	t.Helper()
	c := checker(t, nil, nil)
	c.HostTriple = "x86_64-apple-darwin"
	c.FindLibclang = func() (string, bool) {
		if libclang == "" {
			return "", false
		}
		return libclang, true
	}
	return c
}

// TestMacOSInstallIsNotBlockedByALinuxConfig is the bug: `pgx install` of a
// pg_extensions extension on macOS refused to build, naming a compiler cache
// and a Debian libclang directory, neither of which the user could act on.
func TestMacOSInstallIsNotBlockedByALinuxConfig(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, pgExtensionsConfig)

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	c := macChecker(t, "/Library/Developer/CommandLineTools/usr/lib")
	fixes, blocking := c.Remedies(c.Check(files))

	if len(blocking) != 0 {
		t.Fatalf("build should not be blocked, got %v", fields(blocking))
	}

	got := map[string]string{}
	for _, fix := range fixes {
		got[fix.Key] = fix.Value
	}
	if v, ok := got["RUSTC_WRAPPER"]; !ok || v != "" {
		t.Errorf("expected RUSTC_WRAPPER to be neutralised, got %q (present: %v)", v, ok)
	}
	if got["LIBCLANG_PATH"] != "/Library/Developer/CommandLineTools/usr/lib" {
		t.Errorf("expected LIBCLANG_PATH retargeted, got %q", got["LIBCLANG_PATH"])
	}
}

func TestEnvPairsRenderOverrides(t *testing.T) {
	pairs := EnvPairs([]Remedy{
		{Key: "RUSTC_WRAPPER", Value: ""},
		{Key: "LIBCLANG_PATH", Value: "/opt/llvm/lib"},
	})
	want := []string{"RUSTC_WRAPPER=", "LIBCLANG_PATH=/opt/llvm/lib"}
	if len(pairs) != len(want) {
		t.Fatalf("got %v, want %v", pairs, want)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pair %d: got %q, want %q", i, pairs[i], want[i])
		}
	}
}

// A libclang that cannot be found anywhere is not something pgbrew can invent,
// so it has to stay a blocking issue rather than silently letting bindgen fail.
func TestLibclangIsBlockingWhenNoneIsInstalled(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, pgExtensionsConfig)

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	c := macChecker(t, "")
	fixes, blocking := c.Remedies(c.Check(files))

	if len(fixes) != 1 || fixes[0].Key != "RUSTC_WRAPPER" {
		t.Fatalf("only the wrapper should be fixable, got %v", fixes)
	}
	if len(blocking) != 1 || blocking[0].Field != "env.LIBCLANG_PATH" {
		t.Fatalf("expected LIBCLANG_PATH to block, got %v", fields(blocking))
	}
	if !strings.Contains(blocking[0].Hint, "brew install llvm") {
		t.Errorf("hint should name how to get libclang, got %q", blocking[0].Hint)
	}
}

// An unrecognised wrapper may be doing something the build depends on. Only
// wrappers that are known compiler caches are transparent enough to drop.
func TestUnknownRustcWrapperStillBlocks(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, `
[build]
rustc-wrapper = "my-instrumenting-shim"
`)

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	c := macChecker(t, "/usr/lib")
	fixes, blocking := c.Remedies(c.Check(files))

	if len(fixes) != 0 {
		t.Fatalf("an unknown wrapper must not be silently dropped, got %v", fixes)
	}
	if len(blocking) != 1 {
		t.Fatalf("expected the wrapper to block, got %v", fields(blocking))
	}
	if !strings.Contains(blocking[0].Hint, "compiler caches") {
		t.Errorf("hint should explain why it was not bypassed, got %q", blocking[0].Hint)
	}
}

// A missing linker is a real missing tool, not a portability accident: the
// build cannot proceed without it and pgbrew has nothing to substitute.
func TestMissingLinkerStillBlocks(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, pgExtensionsConfig)

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	// On Linux the [target.x86_64-unknown-linux-gnu] section applies.
	c := checker(t, nil, nil)
	c.FindLibclang = func() (string, bool) { return "/usr/lib/llvm-21/lib", true }
	_, blocking := c.Remedies(c.Check(files))

	var sawLinker, sawMold bool
	for _, issue := range blocking {
		if strings.HasSuffix(issue.Field, ".linker") {
			sawLinker = true
		}
		if strings.Contains(issue.Value, "mold") {
			sawMold = true
		}
	}
	if !sawLinker || !sawMold {
		t.Fatalf("clang and mold must still be reported, got %v", fields(blocking))
	}
}

// Everything installed means nothing to fix and nothing to report — the
// remedy pass must not manufacture work on a machine that is already correct.
func TestNothingToRemedyWhenEverythingIsInstalled(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, pgExtensionsConfig)

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	c := checker(t, []string{"clang", "mold", "sccache"}, []string{"/usr/lib/llvm-20/lib"})
	fixes, blocking := c.Remedies(c.Check(files))
	if len(fixes) != 0 || len(blocking) != 0 {
		t.Fatalf("expected a clean bill of health, got fixes=%v blocking=%v", fixes, fields(blocking))
	}
}

func TestRemedyStringNamesTheSettingAndTheFix(t *testing.T) {
	r := Remedy{
		Issue:  Issue{Source: "/repo/.cargo/config.toml", Field: "env.LIBCLANG_PATH", Value: "/usr/lib/llvm-20/lib"},
		Key:    "LIBCLANG_PATH",
		Value:  "/opt/homebrew/opt/llvm/lib",
		Reason: "does not exist here; using /opt/homebrew/opt/llvm/lib",
	}
	s := r.String()
	for _, want := range []string{"env.LIBCLANG_PATH", "config.toml", "/opt/homebrew/opt/llvm/lib"} {
		if !strings.Contains(s, want) {
			t.Errorf("%q missing from %q", want, s)
		}
	}
}

func TestHasLibclangRecognisesEachNamingConvention(t *testing.T) {
	for _, name := range []string{"libclang.dylib", "libclang.so", "libclang.so.14.0.6"} {
		dir := t.TempDir()
		writeFile(t, dir, name)
		if !hasLibclang(dir) {
			t.Errorf("%s not recognised as libclang", name)
		}
	}

	empty := t.TempDir()
	if hasLibclang(empty) {
		t.Error("an empty directory should not pass as a libclang location")
	}

	decoy := t.TempDir()
	writeFile(t, decoy, "libclang-cpp.so.14")
	if hasLibclang(decoy) {
		t.Error("libclang-cpp is not libclang")
	}
}

// writeFile creates an empty file in dir.
func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
