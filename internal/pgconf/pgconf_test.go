package pgconf

import (
	"strings"
	"testing"
)

func TestParsePreloadList(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"'pg_cron'", []string{"pg_cron"}},
		{"'pg_cron,pg_kafka'", []string{"pg_cron", "pg_kafka"}},
		{"'pg_cron, pg_kafka, pg_mqtt'", []string{"pg_cron", "pg_kafka", "pg_mqtt"}},
		{"pg_cron", []string{"pg_cron"}},
		{`"pg_cron, pg_kafka"`, []string{"pg_cron", "pg_kafka"}},
		// Per-entry quoting is legal too.
		{"'pg_cron', 'pg_kafka'", []string{"pg_cron", "pg_kafka"}},
		{"''", nil},
		{"", nil},
		{"  ", nil},
		// A trailing comma must not produce an empty library name.
		{"'pg_cron,'", []string{"pg_cron"}},
	} {
		got := ParsePreloadList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ParsePreloadList(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParsePreloadList(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

// This is the behaviour that protects a working database: adding one extension
// must never remove another.
func TestMergePreloadPreservesExisting(t *testing.T) {
	existing := []string{"pg_cron", "pg_mqtt"}
	merged, changed := MergePreload(existing, "pg_kafka")

	if !changed {
		t.Error("adding a new library should report a change")
	}
	want := []string{"pg_cron", "pg_mqtt", "pg_kafka"}
	if strings.Join(merged, ",") != strings.Join(want, ",") {
		t.Errorf("merged = %v, want %v", merged, want)
	}
}

func TestMergePreloadIsIdempotent(t *testing.T) {
	// Reinstalling an extension must not duplicate its entry.
	existing := []string{"pg_cron", "pg_kafka"}
	merged, changed := MergePreload(existing, "pg_kafka")

	if changed {
		t.Error("adding an already-present library should report no change")
	}
	if len(merged) != 2 {
		t.Errorf("merged = %v, want no duplicate", merged)
	}
}

func TestMergePreloadKeepsOperatorOrdering(t *testing.T) {
	// Load order can matter between interacting extensions, so an existing
	// arrangement must not be reordered.
	existing := []string{"zzz_first_on_purpose", "aaa_second"}
	merged, _ := MergePreload(existing, "pg_kafka")

	if merged[0] != "zzz_first_on_purpose" || merged[1] != "aaa_second" {
		t.Errorf("merge reordered the existing list: %v", merged)
	}
	if merged[2] != "pg_kafka" {
		t.Errorf("new entry should be appended, got %v", merged)
	}
}

func TestMergePreloadDeduplicatesExisting(t *testing.T) {
	merged, changed := MergePreload([]string{"pg_cron", "pg_cron"})
	if len(merged) != 1 {
		t.Errorf("merged = %v, want duplicates collapsed", merged)
	}
	if !changed {
		t.Error("collapsing a duplicate is a change")
	}
}

func TestMergePreloadIntoEmpty(t *testing.T) {
	merged, changed := MergePreload(nil, "pg_kafka")
	if !changed || len(merged) != 1 || merged[0] != "pg_kafka" {
		t.Errorf("merged = %v, changed = %v", merged, changed)
	}
}

func TestMergePreloadIgnoresBlankAdditions(t *testing.T) {
	merged, changed := MergePreload([]string{"pg_cron"}, "", "   ")
	if changed || len(merged) != 1 {
		t.Errorf("merged = %v, changed = %v; blanks should be ignored", merged, changed)
	}
}

func TestRemoveFromPreload(t *testing.T) {
	kept, changed := RemoveFromPreload([]string{"pg_cron", "pg_kafka", "pg_mqtt"}, "pg_kafka")
	if !changed {
		t.Error("removing a present library is a change")
	}
	if strings.Join(kept, ",") != "pg_cron,pg_mqtt" {
		t.Errorf("kept = %v", kept)
	}

	if _, changed := RemoveFromPreload([]string{"pg_cron"}, "not_there"); changed {
		t.Error("removing an absent library is not a change")
	}
}

func TestRoundTripPreloadValue(t *testing.T) {
	original := "'pg_cron, pg_mqtt'"
	libs := ParsePreloadList(original)
	merged, _ := MergePreload(libs, "pg_kafka")
	got := FormatPreloadList(merged)

	if got != "'pg_cron, pg_mqtt, pg_kafka'" {
		t.Errorf("round trip produced %q", got)
	}
	// And the result must parse back to the same libraries.
	if len(ParsePreloadList(got)) != 3 {
		t.Errorf("re-parsing %q lost entries", got)
	}
}

func TestFindSetting(t *testing.T) {
	config := `
# shared_preload_libraries = 'commented_out'
max_connections = 100
shared_preload_libraries = 'pg_cron'   # trailing comment
port = 5432
`
	value, ok := FindSetting(config, PreloadSetting)
	if !ok {
		t.Fatal("setting not found")
	}
	if value != "'pg_cron'" {
		t.Errorf("value = %q", value)
	}
	if libs := ParsePreloadList(value); len(libs) != 1 || libs[0] != "pg_cron" {
		t.Errorf("libs = %v", libs)
	}
}

func TestFindSettingIgnoresComments(t *testing.T) {
	config := "#shared_preload_libraries = 'ghost'\n#   shared_preload_libraries = 'ghost2'\n"
	if _, ok := FindSetting(config, PreloadSetting); ok {
		t.Error("a commented setting must not be treated as set")
	}
}

func TestFindSettingLastAssignmentWins(t *testing.T) {
	// PostgreSQL applies the last assignment, so reading anything else would
	// merge into a value that is not actually in effect.
	config := "shared_preload_libraries = 'first'\nshared_preload_libraries = 'second'\n"
	value, _ := FindSetting(config, PreloadSetting)
	if value != "'second'" {
		t.Errorf("value = %q, want the last assignment", value)
	}
}

func TestFindSettingKeepsHashInsideQuotes(t *testing.T) {
	config := `password_thing = 'a#b'`
	value, ok := FindSetting(config, "password_thing")
	if !ok || value != "'a#b'" {
		t.Errorf("value = %q, ok = %v; a # inside quotes is part of the value", value, ok)
	}
}

func TestFindSettingMissing(t *testing.T) {
	if _, ok := FindSetting("max_connections = 100\n", PreloadSetting); ok {
		t.Error("expected not found")
	}
}

func TestHasIncludeDir(t *testing.T) {
	if !HasIncludeDir("include_dir = 'conf.d'\n", "conf.d") {
		t.Error("should detect the include_dir")
	}
	if !HasIncludeDir("include_dir='conf.d'  # comment\n", "conf.d") {
		t.Error("should tolerate no spaces and a trailing comment")
	}
	if HasIncludeDir("#include_dir = 'conf.d'\n", "conf.d") {
		t.Error("a commented include_dir does not count")
	}
	if HasIncludeDir("include_dir = 'other.d'\n", "conf.d") {
		t.Error("a different directory does not count")
	}
	if HasIncludeDir("", "conf.d") {
		t.Error("empty config has no include_dir")
	}
}

func TestRenderPreloadFile(t *testing.T) {
	out := RenderPreloadFile([]string{"pg_cron", "pg_kafka"})
	if !strings.Contains(out, "shared_preload_libraries = 'pg_cron, pg_kafka'") {
		t.Errorf("rendered file missing the setting:\n%s", out)
	}
	if !strings.Contains(out, "Managed by pgbrew") {
		t.Errorf("rendered file should be marked as managed:\n%s", out)
	}
	// It must parse back to what went in.
	value, ok := FindSetting(out, PreloadSetting)
	if !ok {
		t.Fatal("rendered file does not parse")
	}
	if len(ParsePreloadList(value)) != 2 {
		t.Errorf("rendered value does not round trip: %q", value)
	}
}

func TestRenderDropIn(t *testing.T) {
	out := RenderDropIn("pg_kafka", map[string]string{
		"pgkafka.port":            "9092",
		"pgkafka.advertised_host": "localhost",
		"pgkafka.enabled":         "on",
	})

	for _, want := range []string{
		"pgkafka.advertised_host = 'localhost'",
		"pgkafka.enabled = on",
		"pgkafka.port = 9092",
		"pg_kafka",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderDropInIsDeterministic(t *testing.T) {
	settings := map[string]string{"b": "2", "a": "1", "c": "3"}
	if RenderDropIn("x", settings) != RenderDropIn("x", settings) {
		t.Error("drop-in rendering is not deterministic")
	}
	// Sorted, so regenerating never produces a spurious diff.
	out := RenderDropIn("x", settings)
	if strings.Index(out, "a = 1") > strings.Index(out, "b = 2") {
		t.Errorf("settings should be sorted:\n%s", out)
	}
}

func TestQuoteValue(t *testing.T) {
	for in, want := range map[string]string{
		"9092":       "9092",
		"1.5":        "1.5",
		"-1":         "-1",
		"on":         "on",
		"off":        "off",
		"true":       "true",
		"localhost":  "'localhost'",
		"a b":        "'a b'",
		"":           "''",
		"it's":       "'it''s'",
		"/var/lib/x": "'/var/lib/x'",
		"100MB":      "'100MB'",
	} {
		if got := quoteValue(in); got != want {
			t.Errorf("quoteValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuoteValueEscapesQuotesSoConfigStaysParseable(t *testing.T) {
	// An unescaped quote would terminate the value early and change the meaning
	// of everything after it.
	rendered := RenderDropIn("x", map[string]string{"thing": "a'b"})
	value, ok := FindSetting(rendered, "thing")
	if !ok {
		t.Fatalf("rendered config does not parse:\n%s", rendered)
	}
	if value != "'a''b'" {
		t.Errorf("value = %q, want the quote doubled", value)
	}
}

func TestDropInNaming(t *testing.T) {
	if got := DropInName("pg_kafka"); got != "10-pgbrew-pg_kafka.conf" {
		t.Errorf("DropInName = %q", got)
	}
	// The shared preload file must sort before per-extension files, so the
	// merged list is established first.
	if !(PreloadFileName < DropInName("aaa")) {
		t.Errorf("%q should sort before %q", PreloadFileName, DropInName("aaa"))
	}
}

func TestIsManagedFile(t *testing.T) {
	for _, name := range []string{PreloadFileName, DropInName("pg_kafka")} {
		if !IsManagedFile(name) {
			t.Errorf("%q should be recognised as managed", name)
		}
	}
	// Anything the operator wrote must be left alone.
	for _, name := range []string{"99-local-tuning.conf", "postgresql.conf", "10-custom.conf"} {
		if IsManagedFile(name) {
			t.Errorf("%q must not be treated as pgbrew-managed", name)
		}
	}
}
