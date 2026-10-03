package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/matroidbe/pgbrew/internal/bottle"
	"github.com/matroidbe/pgbrew/internal/pgconf"
)

// captureStdout runs fn and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	return <-done
}

// withSetFlags sets the parsed --set overrides for one test.
func withSetFlags(t *testing.T, overrides map[string]string) {
	t.Helper()
	orig := setOverrides
	setOverrides = overrides
	t.Cleanup(func() { setOverrides = orig })
}

func TestInstallSetFlagIsRepeatableAndKeepsCommas(t *testing.T) {
	flag := installCmd.Flags().Lookup("set")
	if flag == nil {
		t.Fatal("pgx install has no --set flag")
	}
	if flag.Value.Type() != "stringArray" {
		t.Fatalf("--set is %s; a stringSlice would split values on commas", flag.Value.Type())
	}

	orig := append([]string(nil), setArgs...)
	t.Cleanup(func() {
		setArgs = orig
		_ = flag.Value.(interface{ Replace([]string) error }).Replace(orig)
		flag.Changed = false
	})
	if err := installCmd.Flags().Parse([]string{"--set", "a.x=1,2", "--set", "a.y=3"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(setArgs, "|") != "a.x=1,2|a.y=3" {
		t.Errorf("setArgs = %q", setArgs)
	}
}

func TestRunInstallRejectsMalformedSetBeforeInstalling(t *testing.T) {
	origArgs, origBottle := setArgs, bottleSource
	t.Cleanup(func() { setArgs, bottleSource = origArgs, origBottle; setOverrides = nil })

	// The bottle does not exist: if --set were checked late, this would fail
	// on the bottle instead.
	bottleSource = "/nonexistent/pg_kafka-0.1.0-pg16-linux-amd64.tar.gz"
	for _, bad := range []string{"pg_kafka.database", "=app", "shared_preload_libraries=pg_kafka"} {
		setArgs = []string{bad}
		err := runInstall(installCmd, nil)
		if err == nil {
			t.Errorf("%q: expected an error", bad)
			continue
		}
		if !strings.Contains(err.Error(), "--set") {
			t.Errorf("%q: error should name --set, got %v", bad, err)
		}
		if strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("%q: validation ran after the bottle was opened: %v", bad, err)
		}
	}
}

func TestHandlePostgresConfigReportShowsSetValues(t *testing.T) {
	withSetFlags(t, map[string]string{"pg_kafka.database": "app", "work_mem": "64MB"})
	configureServer = false

	plan := planFromBottle(bottle.Manifest{
		Name: "pg_kafka",
		Postgres: &bottle.PostgresConfig{
			SharedPreloadLibraries: true,
			Settings:               map[string]string{"pg_kafka.database": "postgres"},
		},
	})

	var err error
	out := captureStdout(t, func() { err = handlePostgresConfig(plan) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"shared_preload_libraries += pg_kafka",
		"pg_kafka.database = 'app'  (--set)",
		"work_mem = '64MB'  (--set)",
		"note: work_mem is not a setting pg_kafka declares",
		"Not applied",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "'postgres'") {
		t.Errorf("the overridden default should not be reported:\n%s", out)
	}
	if strings.Contains(out, "pg_kafka.database is not a setting") {
		t.Errorf("a declared key must not get the undeclared note:\n%s", out)
	}
}

func TestHandlePostgresConfigSetOnExtensionThatDeclaresNothing(t *testing.T) {
	withSetFlags(t, map[string]string{"pg_hello.greeting": "hi"})
	configureServer = false

	out := captureStdout(t, func() {
		if err := handlePostgresConfig(pgconf.Plan{Extension: "pg_hello"}); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "pg_hello.greeting = 'hi'  (--set)") {
		t.Errorf("--set must be reported even with no declaration:\n%s", out)
	}
}

func TestHandlePostgresConfigWritesSetValues(t *testing.T) {
	withSetFlags(t, map[string]string{"pg_kafka.database": "app"})
	dir := t.TempDir()
	conf := dir + "/postgresql.conf"
	if err := os.WriteFile(conf, []byte("max_connections = 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGBREW_POSTGRESQL_CONF", conf)
	configureServer = true
	t.Cleanup(func() { configureServer = false })

	plan := pgconf.Plan{
		Extension: "pg_kafka",
		Settings:  map[string]string{"pg_kafka.database": "postgres", "pg_kafka.port": "9092"},
	}
	captureStdout(t, func() {
		if err := handlePostgresConfig(plan); err != nil {
			t.Error(err)
		}
	})

	data, err := os.ReadFile(dir + "/conf.d/" + pgconf.DropInName("pg_kafka"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "pg_kafka.database = 'app'\n") || !strings.Contains(got, "pg_kafka.port = 9092\n") {
		t.Errorf("drop-in:\n%s", got)
	}
	if strings.Contains(got, "postgres'") {
		t.Errorf("declared default should have been overridden:\n%s", got)
	}
}
