package pgconf

import (
	"strings"
	"testing"
)

func TestParseOverrides(t *testing.T) {
	got, err := ParseOverrides([]string{
		"pg_kafka.database=app",
		"pg_kafka.hosts=a,b,c",       // commas belong to the value
		"pg_kafka.dsn=host=x port=5", // only the first '=' splits
		"  work_mem = 64MB ",         // whitespace around key and value is trimmed
		"pg_kafka.empty=",            // an empty value is a value
	})
	if err != nil {
		t.Fatalf("ParseOverrides: %v", err)
	}
	want := map[string]string{
		"pg_kafka.database": "app",
		"pg_kafka.hosts":    "a,b,c",
		"pg_kafka.dsn":      "host=x port=5",
		"work_mem":          "64MB",
		"pg_kafka.empty":    "",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseOverridesStripsOneLayerOfQuotes(t *testing.T) {
	// `--set "x='a b'"` is the natural way to write a quoted value; storing the
	// quotes would render as '''a b''' in the drop-in.
	got, err := ParseOverrides([]string{`a.x='a b'`, `a.y="c"`, `a.z='it''s'`})
	if err != nil {
		t.Fatal(err)
	}
	if got["a.x"] != "a b" || got["a.y"] != "c" {
		t.Errorf("quotes not stripped: %v", got)
	}
	if got["a.z"] != "it's" {
		t.Errorf("doubled quote inside a quoted value should unescape: %q", got["a.z"])
	}
}

func TestParseOverridesLastOneWins(t *testing.T) {
	got, err := ParseOverrides([]string{"a.x=1", "A.X=2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("GUC names are case-insensitive; want one entry, got %v", got)
	}
	for _, v := range got {
		if v != "2" {
			t.Errorf("later --set should win, got %q", v)
		}
	}
}

func TestParseOverridesRejectsMalformedInput(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want string
	}{
		{"pg_kafka.database", "expected key=value"},
		{"=app", "empty key"},
		{"  =app", "empty key"},
		{"bad key=1", "invalid setting name"},
		{"a#b=1", "invalid setting name"},
		{"a.x=line1\nline2", "newline"},
		{"shared_preload_libraries=pg_kafka", "shared_preload_libraries"},
		{"Shared_Preload_Libraries=pg_kafka", "shared_preload_libraries"},
		{"include_dir=/tmp", "include_dir"},
	} {
		_, err := ParseOverrides([]string{tc.arg})
		if err == nil {
			t.Errorf("%q: expected an error", tc.arg)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %q does not mention %q", tc.arg, err, tc.want)
		}
	}
}

func TestParseOverridesPreloadErrorExplainsWhy(t *testing.T) {
	_, err := ParseOverrides([]string{"shared_preload_libraries=x"})
	if err == nil || !strings.Contains(err.Error(), "merged") {
		t.Errorf("error should explain the list is merged: %v", err)
	}
}

func TestWithOverridesReplacesDeclaredValue(t *testing.T) {
	declared := map[string]string{"pg_kafka.database": "postgres", "pg_kafka.port": "9092"}
	plan := Plan{Extension: "pg_kafka", PreloadLibrary: "pg_kafka", Settings: declared}

	got, undeclared := plan.WithOverrides(map[string]string{"pg_kafka.database": "app"})

	if got.Settings["pg_kafka.database"] != "app" {
		t.Errorf("override not applied: %v", got.Settings)
	}
	if got.Settings["pg_kafka.port"] != "9092" {
		t.Errorf("undeclared keys must be kept: %v", got.Settings)
	}
	if len(undeclared) != 0 {
		t.Errorf("pg_kafka.database is declared, got undeclared %v", undeclared)
	}
	if declared["pg_kafka.database"] != "postgres" {
		t.Error("WithOverrides must not mutate the manifest's map")
	}
	if got.PreloadLibrary != "pg_kafka" {
		t.Error("preload library lost")
	}
}

func TestWithOverridesMatchesDeclaredKeyCaseInsensitively(t *testing.T) {
	plan := Plan{Extension: "e", Settings: map[string]string{"e.Database": "postgres"}}
	got, undeclared := plan.WithOverrides(map[string]string{"e.database": "app"})
	if len(got.Settings) != 1 || got.Settings["e.Database"] != "app" {
		t.Errorf("a differently-cased override must replace, not duplicate: %v", got.Settings)
	}
	if len(undeclared) != 0 {
		t.Errorf("undeclared = %v", undeclared)
	}
}

func TestWithOverridesAddsAndReportsUndeclaredKeys(t *testing.T) {
	plan := Plan{Extension: "pg_kafka", Settings: map[string]string{"pg_kafka.port": "9092"}}
	got, undeclared := plan.WithOverrides(map[string]string{
		"work_mem":       "64MB",
		"pg_kafka.extra": "1",
		"pg_kafka.port":  "9093",
	})
	if got.Settings["work_mem"] != "64MB" || got.Settings["pg_kafka.extra"] != "1" {
		t.Errorf("undeclared keys should be added: %v", got.Settings)
	}
	if strings.Join(undeclared, ",") != "pg_kafka.extra,work_mem" {
		t.Errorf("undeclared = %v, want sorted [pg_kafka.extra work_mem]", undeclared)
	}
}

func TestWithOverridesOnEmptyPlan(t *testing.T) {
	plan := Plan{Extension: "pg_hello"}
	got, _ := plan.WithOverrides(map[string]string{"pg_hello.greeting": "hi"})
	if got.IsEmpty() {
		t.Error("a --set on an extension that declares nothing still has something to write")
	}
	if got.NeedsRestart() {
		t.Error("settings alone need only a reload")
	}
}

func TestDescribeMarksOverriddenValues(t *testing.T) {
	plan := Plan{Extension: "pg_kafka", Settings: map[string]string{
		"pg_kafka.database": "postgres", "pg_kafka.port": "9092",
	}}
	got, _ := plan.WithOverrides(map[string]string{"pg_kafka.database": "app"})
	out := got.Describe()
	if !strings.Contains(out, "pg_kafka.database = 'app'  (--set)") {
		t.Errorf("effective value should be shown and marked:\n%s", out)
	}
	if strings.Contains(out, "postgres") {
		t.Errorf("the overridden declared value should not be shown:\n%s", out)
	}
	if strings.Contains(out, "9092  (--set)") {
		t.Errorf("only overridden keys are marked:\n%s", out)
	}
}
