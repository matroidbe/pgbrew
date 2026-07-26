package cargocfg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pgExtensionsConfig is the real .cargo/config.toml from the pg_extensions
// repo, which is what exposed this gap: `pgx doctor` reported everything green
// and the build then failed twice, first on mold and then on libclang.
const pgExtensionsConfig = `
[target.x86_64-unknown-linux-gnu]
linker = "clang"
rustflags = ["-C", "link-arg=-fuse-ld=mold"]

[build]
rustc-wrapper = "sccache"

[env]
CARGO_PROFILE_DEV_DEBUG = "line-tables-only"
LIBCLANG_PATH = "/usr/lib/llvm-20/lib"
`

func writeCargoConfig(t *testing.T, dir, content string) {
	t.Helper()
	cargoDir := filepath.Join(dir, ".cargo")
	if err := os.MkdirAll(cargoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cargoDir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// checker builds a Checker with a fixed idea of what exists.
func checker(t *testing.T, onPath []string, paths []string) *Checker {
	t.Helper()
	pathSet := map[string]bool{}
	for _, p := range onPath {
		pathSet[p] = true
	}
	fileSet := map[string]bool{}
	for _, p := range paths {
		fileSet[p] = true
	}
	return &Checker{
		HostTriple: "x86_64-unknown-linux-gnu",
		LookPath: func(name string) (string, error) {
			if pathSet[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Exists:    func(p string) bool { return fileSet[p] },
		LookupEnv: func(string) (string, bool) { return "", false },
	}
}

func fields(issues []Issue) []string {
	out := make([]string, len(issues))
	for i, issue := range issues {
		out[i] = issue.Field
	}
	return out
}

func TestParsesRealWorldConfig(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, pgExtensionsConfig)

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no config discovered")
	}

	cfg := files[0].Config
	if cfg.Build.RustcWrapper != "sccache" {
		t.Errorf("rustc-wrapper = %q", cfg.Build.RustcWrapper)
	}
	target := cfg.Target["x86_64-unknown-linux-gnu"]
	if target.Linker != "clang" {
		t.Errorf("linker = %q", target.Linker)
	}
	if len(target.Rustflags) != 2 || target.Rustflags[1] != "link-arg=-fuse-ld=mold" {
		t.Errorf("rustflags = %v", target.Rustflags)
	}
	if cfg.Env["LIBCLANG_PATH"].Value != "/usr/lib/llvm-20/lib" {
		t.Errorf("LIBCLANG_PATH = %+v", cfg.Env["LIBCLANG_PATH"])
	}
}

func TestCatchesExactlyTheThreeFailuresWeHit(t *testing.T) {
	// Nothing installed but clang: reproduces the sandbox where the pg_solid
	// build failed twice before succeeding.
	dir := t.TempDir()
	writeCargoConfig(t, dir, pgExtensionsConfig)
	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	issues := checker(t, []string{"clang"}, nil).Check(files)

	got := strings.Join(fields(issues), " ")
	for _, want := range []string{"build.rustc-wrapper", "rustflags", "env.LIBCLANG_PATH"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing an issue for %s; got %v", want, got)
		}
	}
	if len(issues) != 3 {
		t.Errorf("got %d issues, want exactly 3: %v", len(issues), got)
	}
}

func TestNoIssuesWhenEverythingIsInstalled(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, pgExtensionsConfig)
	files, _ := Discover(dir)

	c := checker(t, []string{"clang", "sccache", "ld.mold"}, []string{"/usr/lib/llvm-20/lib"})
	if issues := c.Check(files); len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLinkerResolvesUnderEitherName(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, "[build]\nrustflags = [\"-C\", \"link-arg=-fuse-ld=mold\"]\n")
	files, _ := Discover(dir)

	// `ld.mold` is the conventional name, but a bare `mold` counts too.
	for _, installed := range [][]string{{"ld.mold"}, {"mold"}} {
		if issues := checker(t, installed, nil).Check(files); len(issues) != 0 {
			t.Errorf("with %v installed, expected no issue, got %v", installed, issues)
		}
	}
	if issues := checker(t, nil, nil).Check(files); len(issues) != 1 {
		t.Errorf("with mold absent, expected 1 issue, got %v", issues)
	}
}

func TestSkipsOtherTargetTriples(t *testing.T) {
	// A cross-compilation config must not fail a native build.
	dir := t.TempDir()
	writeCargoConfig(t, dir, `
[target.aarch64-unknown-linux-musl]
linker = "aarch64-linux-musl-gcc"
rustflags = ["-C", "link-arg=-fuse-ld=some-exotic-linker"]
`)
	files, _ := Discover(dir)

	if issues := checker(t, nil, nil).Check(files); len(issues) != 0 {
		t.Errorf("settings for another triple should be ignored, got %v", issues)
	}
}

func TestSkipsCfgPredicateTargets(t *testing.T) {
	// Evaluating cargo's cfg() expressions is out of scope; guessing could be
	// wrong in either direction, so these are left alone.
	dir := t.TempDir()
	writeCargoConfig(t, dir, `
[target.'cfg(all(target_arch = "x86_64", target_os = "linux"))']
linker = "definitely-not-installed"
`)
	files, _ := Discover(dir)

	if issues := checker(t, nil, nil).Check(files); len(issues) != 0 {
		t.Errorf("cfg() targets should be skipped, got %v", issues)
	}
}

func TestEnvironmentOverrideSuppressesWrapperIssue(t *testing.T) {
	// RUSTC_WRAPPER="" is the documented way to disable a configured wrapper.
	// Reporting it as missing then would be actively wrong.
	dir := t.TempDir()
	writeCargoConfig(t, dir, "[build]\nrustc-wrapper = \"sccache\"\n")
	files, _ := Discover(dir)

	c := checker(t, nil, nil)
	c.LookupEnv = func(key string) (string, bool) {
		if key == "RUSTC_WRAPPER" {
			return "", true // set, and set to empty
		}
		return "", false
	}
	if issues := c.Check(files); len(issues) != 0 {
		t.Errorf("an overridden wrapper should not be reported, got %v", issues)
	}
}

func TestEnvPathChecks(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, `
[env]
ABS_MISSING = "/nope/does/not/exist"
ABS_PRESENT = "/exists"
RELATIVE = "some/relative/path"
NOT_A_PATH = "line-tables-only"
`)
	files, _ := Discover(dir)

	issues := checker(t, nil, []string{"/exists"}).Check(files)
	if len(issues) != 1 || issues[0].Field != "env.ABS_MISSING" {
		t.Errorf("only the missing absolute path should be reported, got %v", fields(issues))
	}
}

func TestEnvRelativeFlagIsRespected(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, `
[env]
SOME_DIR = { value = "/resolved/later", relative = true }
`)
	files, _ := Discover(dir)

	if issues := checker(t, nil, nil).Check(files); len(issues) != 0 {
		t.Errorf("a relative entry is resolved by cargo, not us: %v", issues)
	}
}

func TestEnvTableForm(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, `
[env]
FORCED = { value = "/missing", force = true }
`)
	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := files[0].Config.Env["FORCED"]; got.Value != "/missing" || !got.Force {
		t.Fatalf("table-form env not parsed: %+v", got)
	}

	// force = true means the config wins over the environment, so an existing
	// environment variable does not excuse a missing path.
	c := checker(t, nil, nil)
	c.LookupEnv = func(string) (string, bool) { return "/something/else", true }
	if issues := c.Check(files); len(issues) != 1 {
		t.Errorf("a forced missing path should be reported, got %v", issues)
	}
}

func TestRustflagsAcceptsStringForm(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, "[build]\nrustflags = \"-C link-arg=-fuse-ld=mold\"\n")
	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := files[0].Config.Build.Rustflags; len(got) != 2 {
		t.Fatalf("string-form rustflags not split: %v", got)
	}
	if issues := checker(t, nil, nil).Check(files); len(issues) != 1 {
		t.Errorf("expected the mold issue from string-form rustflags, got %v", issues)
	}
}

func TestDiscoverWalksUpToAncestors(t *testing.T) {
	// A workspace member inherits the workspace root's cargo config.
	root := t.TempDir()
	writeCargoConfig(t, root, "[build]\nrustc-wrapper = \"sccache\"\n")
	member := filepath.Join(root, "extensions", "pg_solid")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := Discover(member)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Config.Build.RustcWrapper == "sccache" {
			found = true
		}
	}
	if !found {
		t.Error("a config in an ancestor directory should be discovered")
	}
}

func TestNearestConfigWins(t *testing.T) {
	root := t.TempDir()
	writeCargoConfig(t, root, "[build]\nrustc-wrapper = \"from-root\"\n")
	member := filepath.Join(root, "member")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCargoConfig(t, member, "[build]\nrustc-wrapper = \"from-member\"\n")

	files, _ := Discover(member)
	issues := checker(t, nil, nil).Check(files)

	if len(issues) != 1 {
		t.Fatalf("only the effective setting should be reported, got %v", issues)
	}
	if issues[0].Value != "from-member" {
		t.Errorf("value = %q, want the nearer config to win", issues[0].Value)
	}
}

func TestNoConfigIsNotAnError(t *testing.T) {
	files, err := Discover(t.TempDir())
	if err != nil {
		t.Fatalf("a project without cargo config must not error: %v", err)
	}
	if issues := checker(t, nil, nil).Check(files); len(issues) != 0 {
		t.Errorf("got %v", issues)
	}
}

func TestMalformedConfigIsReported(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, "[build\nrustc-wrapper =")
	if _, err := Discover(dir); err == nil {
		t.Error("expected an error for malformed TOML")
	}
}

func TestToolGivenAsPathIsCheckedAsPath(t *testing.T) {
	dir := t.TempDir()
	writeCargoConfig(t, dir, "[build]\nrustc-wrapper = \"/opt/bin/sccache\"\n")
	files, _ := Discover(dir)

	if issues := checker(t, nil, []string{"/opt/bin/sccache"}).Check(files); len(issues) != 0 {
		t.Errorf("an existing absolute path should pass, got %v", issues)
	}
	if issues := checker(t, nil, nil).Check(files); len(issues) != 1 {
		t.Error("a missing absolute path should be reported")
	}
}

func TestIssueStringIsReadable(t *testing.T) {
	issue := Issue{
		Source:  "/repo/.cargo/config.toml",
		Field:   "build.rustc-wrapper",
		Value:   "sccache",
		Problem: "not found on PATH",
		Hint:    "cargo install sccache",
	}
	s := issue.String()
	for _, want := range []string{"build.rustc-wrapper", "sccache", "config.toml", "not found", "cargo install"} {
		if !strings.Contains(s, want) {
			t.Errorf("Issue.String() missing %q: %s", want, s)
		}
	}
}
