package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// stubTool puts a fake executable on PATH that appends its arguments, one call
// per line, to a log file, and returns that log's path.
func stubTool(t *testing.T, name string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, name+".log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func readLog(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

const timescaleCMakeLists = "cmake_minimum_required(VERSION 3.15)\nfind_program(PG_CONFIG pg_config)\n"

func TestValidateBuildVars(t *testing.T) {
	good := []string{"BISON=/opt/homebrew/opt/bison/bin/bison", "BISONFLAGS=--defines=a.h -Wno-error", "REGRESS_CHECKS=OFF", "X="}
	if err := ValidateBuildVars(good); err != nil {
		t.Errorf("ValidateBuildVars(%v) = %v", good, err)
	}
	for _, bad := range []string{"BISON", "=x", "1X=y", "A-B=c", "A B=c"} {
		if err := ValidateBuildVars([]string{bad}); err == nil {
			t.Errorf("ValidateBuildVars(%q) accepted it", bad)
		}
	}
}

func TestDetectPrefersPGXSOverCMake(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"Makefile":       "PGXS := $(shell $(PG_CONFIG) --pgxs)\ninclude $(PGXS)\n",
		"CMakeLists.txt": timescaleCMakeLists,
		"ext.control":    "default_version = '1.0'\n",
	})
	b, err := DetectBuilder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "pgxs" {
		t.Errorf("detected %s, want pgxs", b.Name())
	}
}

func TestDetectCMakeProjectWithControlTemplate(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"CMakeLists.txt":         timescaleCMakeLists,
		"timescaledb.control.in": "default_version = '@PROJECT_VERSION_MOD@'\n",
	})
	b, err := DetectBuilder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "cmake" {
		t.Fatalf("detected %s, want cmake", b.Name())
	}
	if name, _ := b.GetExtensionName(dir); name != "timescaledb" {
		t.Errorf("name = %q, want timescaledb", name)
	}
	if v, err := b.GetVersion(dir); err == nil {
		t.Errorf("GetVersion before configure = %q, want an error (only a template exists)", v)
	}

	// After configure, the generated control file has the real version.
	writeFiles(t, dir, map[string]string{
		filepath.Join(cmakeBuildDir, "timescaledb.control"): "default_version = '2.30.2'\n",
	})
	if v, err := b.GetVersion(dir); err != nil || v != "2.30.2" {
		t.Errorf("GetVersion after configure = %q, %v; want 2.30.2", v, err)
	}
}

func TestCMakeIgnoresProjectsThatAreNotPostgresExtensions(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"CMakeLists.txt": "project(app)\n",
		"x.control":      "default_version = '1.0'\n",
	})
	if (&CMakeBuilder{}).Detect(dir) {
		t.Error("a CMake project that never mentions pg_config was detected as an extension")
	}
}

func TestPGXSPassesBuildVarsToMake(t *testing.T) {
	log := stubTool(t, "make")
	dir := t.TempDir()
	vars := []string{"BISON=/opt/homebrew/opt/bison/bin/bison", "BISONFLAGS=--defines=src/include/parser/cypher_gram_def.h"}

	if _, err := (&PgxsBuilder{}).Package(dir, InstallOptions{PgConfig: "/pg/bin/pg_config", BuildVars: vars}); err != nil {
		t.Fatal(err)
	}
	calls := readLog(t, log)
	if len(calls) != 2 {
		t.Fatalf("make ran %d times, want 2 (build, staged install): %v", len(calls), calls)
	}
	for _, call := range calls {
		for _, v := range vars {
			if !strings.Contains(call, v) {
				t.Errorf("make %q is missing %q", call, v)
			}
		}
	}
}

func TestCMakePassesBuildVarsAsDefines(t *testing.T) {
	log := stubTool(t, "cmake")
	dir := t.TempDir()

	stage, err := (&CMakeBuilder{}).Package(dir, InstallOptions{
		PgConfig:  "/pg/bin/pg_config",
		BuildVars: []string{"REGRESS_CHECKS=OFF", "OPENSSL_ROOT_DIR=/opt/homebrew/opt/openssl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)

	calls := readLog(t, log)
	if len(calls) != 3 {
		t.Fatalf("cmake ran %d times, want 3 (configure, build, install): %v", len(calls), calls)
	}
	for _, want := range []string{"-DPG_CONFIG=/pg/bin/pg_config", "-DREGRESS_CHECKS=OFF", "-DOPENSSL_ROOT_DIR=/opt/homebrew/opt/openssl"} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("configure %q is missing %q", calls[0], want)
		}
	}
	if !strings.HasPrefix(calls[2], "--install ") {
		t.Errorf("last call = %q, want cmake --install", calls[2])
	}
}

func TestPgrxRejectsBuildVars(t *testing.T) {
	_, err := (&PgrxBuilder{}).Package(t.TempDir(), InstallOptions{BuildVars: []string{"A=b"}})
	if err == nil || !strings.Contains(err.Error(), "--build-var") {
		t.Errorf("Package with build vars = %v, want a --build-var error", err)
	}
}
