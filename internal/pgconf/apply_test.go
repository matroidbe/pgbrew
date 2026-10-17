package pgconf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLayout builds a fake PostgreSQL config directory.
func testLayout(t *testing.T, postgresqlConf string) Layout {
	t.Helper()
	dir := t.TempDir()
	confFile := filepath.Join(dir, "postgresql.conf")
	if err := os.WriteFile(confFile, []byte(postgresqlConf), 0o644); err != nil {
		t.Fatal(err)
	}
	return layoutFor(confFile)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestApplyWritesDropInAndPreload(t *testing.T) {
	layout := testLayout(t, "max_connections = 100\n")

	plan := Plan{
		Extension:      "pg_kafka",
		PreloadLibrary: "pg_kafka",
		Settings:       map[string]string{"pgkafka.port": "9092"},
	}

	result, err := Apply(plan, layout, false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Written) != 2 {
		t.Errorf("wrote %v, want the preload file and the drop-in", result.Written)
	}
	if !result.RestartRequired {
		t.Error("a preload change requires a restart")
	}

	preload := readFile(t, filepath.Join(layout.ConfDir, PreloadFileName))
	if !strings.Contains(preload, "'pg_kafka'") {
		t.Errorf("preload file:\n%s", preload)
	}

	dropIn := readFile(t, filepath.Join(layout.ConfDir, DropInName("pg_kafka")))
	if !strings.Contains(dropIn, "pgkafka.port = 9092") {
		t.Errorf("drop-in:\n%s", dropIn)
	}
}

func TestApplyNeverEditsPostgresqlConfExceptIncludeDir(t *testing.T) {
	original := "max_connections = 100\nport = 5432\n"
	layout := testLayout(t, original)

	_, err := Apply(Plan{Extension: "x", Settings: map[string]string{"a.b": "1"}}, layout, false)
	if err != nil {
		t.Fatal(err)
	}

	after := readFile(t, layout.ConfigFile)
	if !strings.HasPrefix(after, original) {
		t.Errorf("existing configuration was modified:\n%s", after)
	}
	if !strings.Contains(after, "include_dir = 'conf.d'") {
		t.Errorf("include_dir should have been appended:\n%s", after)
	}
}

func TestApplyDoesNotDuplicateIncludeDir(t *testing.T) {
	layout := testLayout(t, "include_dir = 'conf.d'\n")

	result, err := Apply(Plan{Extension: "x", Settings: map[string]string{"a.b": "1"}}, layout, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.IncludeDirAdded {
		t.Error("include_dir was already present and must not be added again")
	}
	if strings.Count(readFile(t, layout.ConfigFile), "include_dir") != 1 {
		t.Error("include_dir duplicated")
	}
}

// The critical behaviour: installing an extension must not disable ones that
// are already preloaded.
func TestApplyPreservesPreloadFromPostgresqlConf(t *testing.T) {
	layout := testLayout(t, "shared_preload_libraries = 'pg_cron, pg_stat_statements'\n")

	result, err := Apply(Plan{Extension: "pg_kafka", PreloadLibrary: "pg_kafka"}, layout, false)
	if err != nil {
		t.Fatal(err)
	}

	got := strings.Join(result.PreloadLibraries, ",")
	if got != "pg_cron,pg_stat_statements,pg_kafka" {
		t.Fatalf("merged list = %q; existing libraries must be preserved", got)
	}

	// And the file on disk must say the same thing.
	value, ok := FindSetting(readFile(t, filepath.Join(layout.ConfDir, PreloadFileName)), PreloadSetting)
	if !ok {
		t.Fatal("preload file does not parse")
	}
	if len(ParsePreloadList(value)) != 3 {
		t.Errorf("preload file lost entries: %q", value)
	}
}

// Installing several preloading extensions in sequence must accumulate, not
// overwrite — this is the multi-extension case that a naive implementation
// silently breaks.
func TestApplyAccumulatesAcrossInstalls(t *testing.T) {
	layout := testLayout(t, "shared_preload_libraries = 'pg_cron'\n")

	for _, ext := range []string{"pg_kafka", "pg_mqtt", "pg_ml"} {
		if _, err := Apply(Plan{Extension: ext, PreloadLibrary: ext}, layout, false); err != nil {
			t.Fatalf("installing %s: %v", ext, err)
		}
	}

	value, _ := FindSetting(readFile(t, filepath.Join(layout.ConfDir, PreloadFileName)), PreloadSetting)
	libs := ParsePreloadList(value)

	want := []string{"pg_cron", "pg_kafka", "pg_mqtt", "pg_ml"}
	if strings.Join(libs, ",") != strings.Join(want, ",") {
		t.Errorf("after three installs: %v, want %v", libs, want)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	layout := testLayout(t, "shared_preload_libraries = 'pg_cron'\n")
	plan := Plan{Extension: "pg_kafka", PreloadLibrary: "pg_kafka",
		Settings: map[string]string{"pgkafka.port": "9092"}}

	if _, err := Apply(plan, layout, false); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, filepath.Join(layout.ConfDir, PreloadFileName))
	firstConf := readFile(t, layout.ConfigFile)

	if _, err := Apply(plan, layout, false); err != nil {
		t.Fatal(err)
	}
	if second := readFile(t, filepath.Join(layout.ConfDir, PreloadFileName)); second != first {
		t.Errorf("reinstalling changed the preload file:\n%s\nvs\n%s", first, second)
	}
	if secondConf := readFile(t, layout.ConfigFile); secondConf != firstConf {
		t.Error("reinstalling modified postgresql.conf again")
	}
}

func TestApplyEmptyPlanDoesNothing(t *testing.T) {
	layout := testLayout(t, "max_connections = 100\n")
	before := readFile(t, layout.ConfigFile)

	result, err := Apply(Plan{Extension: "plain"}, layout, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 0 {
		t.Errorf("wrote %v for an empty plan", result.Written)
	}
	if readFile(t, layout.ConfigFile) != before {
		t.Error("postgresql.conf touched for an empty plan")
	}
	if _, err := os.Stat(layout.ConfDir); err == nil {
		t.Error("conf.d created for an empty plan")
	}
}

func TestApplySettingsOnlyNeedsNoRestart(t *testing.T) {
	layout := testLayout(t, "")
	result, err := Apply(Plan{Extension: "pg_uom", Settings: map[string]string{"pguom.x": "1"}}, layout, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.RestartRequired {
		t.Error("plain GUC settings need only a reload")
	}
}

func TestRemoveTakesLibraryBackOut(t *testing.T) {
	// A removed extension must not leave a preload entry behind: PostgreSQL
	// fails to start if it cannot load a listed library.
	layout := testLayout(t, "shared_preload_libraries = 'pg_cron'\n")
	for _, ext := range []string{"pg_kafka", "pg_mqtt"} {
		if _, err := Apply(Plan{Extension: ext, PreloadLibrary: ext,
			Settings: map[string]string{ext + ".x": "1"}}, layout, false); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Remove("pg_kafka", "pg_kafka", layout, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RestartRequired {
		t.Error("removing a preloaded library requires a restart")
	}

	value, _ := FindSetting(readFile(t, filepath.Join(layout.ConfDir, PreloadFileName)), PreloadSetting)
	libs := ParsePreloadList(value)
	if strings.Join(libs, ",") != "pg_cron,pg_mqtt" {
		t.Errorf("after removing pg_kafka: %v", libs)
	}

	if _, err := os.Stat(filepath.Join(layout.ConfDir, DropInName("pg_kafka"))); err == nil {
		t.Error("the extension's drop-in should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(layout.ConfDir, DropInName("pg_mqtt"))); err != nil {
		t.Error("another extension's drop-in must be left alone")
	}
}

func TestRemoveLastLibraryDeletesThePreloadFile(t *testing.T) {
	layout := testLayout(t, "")
	if _, err := Apply(Plan{Extension: "pg_kafka", PreloadLibrary: "pg_kafka"}, layout, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove("pg_kafka", "pg_kafka", layout, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(layout.ConfDir, PreloadFileName)); err == nil {
		t.Error("an empty preload file should be removed rather than left setting an empty list")
	}
}

func TestRemoveUnknownExtensionIsHarmless(t *testing.T) {
	layout := testLayout(t, "")
	if _, err := Remove("never_installed", "", layout, false); err != nil {
		t.Errorf("removing something that was never applied should be a no-op: %v", err)
	}
}

func TestPlanDescribe(t *testing.T) {
	plan := Plan{
		Extension:      "pg_kafka",
		PreloadLibrary: "pg_kafka",
		Settings:       map[string]string{"pgkafka.port": "9092", "pgkafka.host": "localhost"},
	}
	out := plan.Describe()
	for _, want := range []string{
		"shared_preload_libraries += pg_kafka",
		"pgkafka.host = 'localhost'",
		"pgkafka.port = 9092",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Describe() missing %q:\n%s", want, out)
		}
	}
}

func TestLocateHonoursExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "postgresql.conf")
	if err := os.WriteFile(conf, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGBREW_POSTGRESQL_CONF", conf)

	layout, err := Locate("16")
	if err != nil {
		t.Fatal(err)
	}
	if layout.ConfigFile != conf {
		t.Errorf("ConfigFile = %q", layout.ConfigFile)
	}
	if layout.ConfDir != filepath.Join(dir, ConfDirName) {
		t.Errorf("ConfDir = %q", layout.ConfDir)
	}
}

// maxWorkers reads the max_worker_processes pgbrew wrote, or 0 if none.
func maxWorkers(t *testing.T, layout Layout) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(layout.ConfDir, PreloadFileName))
	if err != nil {
		return 0
	}
	value, ok := FindSetting(string(data), MaxWorkersSetting)
	if !ok {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
		t.Fatalf("max_worker_processes = %q", value)
	}
	return n
}

// PostgreSQL's default of 8 worker slots is used up by three or four
// extensions; the rest then fail to register their workers ("too many
// background workers") and silently do nothing.
func TestApplyRaisesMaxWorkerProcessesForDeclaredWorkers(t *testing.T) {
	layout := testLayout(t, "")
	plan := Plan{Extension: "pg_kafka", PreloadLibrary: "pg_kafka", BackgroundWorkers: 4}
	if _, err := Apply(plan, layout, false); err != nil {
		t.Fatal(err)
	}
	if got, want := maxWorkers(t, layout), DefaultMaxWorkerProcesses+4; got != want {
		t.Errorf("max_worker_processes = %d, want %d", got, want)
	}
}

func TestApplyWorkerBudgetAccumulatesAcrossInstalls(t *testing.T) {
	layout := testLayout(t, "")
	for _, p := range []Plan{
		{Extension: "pg_kafka", PreloadLibrary: "pg_kafka", BackgroundWorkers: 4},
		{Extension: "pg_mqtt", PreloadLibrary: "pg_mqtt", BackgroundWorkers: 4},
		{Extension: "pg_s3", PreloadLibrary: "pg_s3", BackgroundWorkers: 1},
	} {
		if _, err := Apply(p, layout, false); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := maxWorkers(t, layout), DefaultMaxWorkerProcesses+9; got != want {
		t.Errorf("max_worker_processes = %d, want %d", got, want)
	}
}

// Bottles built before background_workers existed declare nothing; budget
// for them generously rather than leave their workers unregistered.
func TestApplyUndeclaredWorkersUseTheDefault(t *testing.T) {
	layout := testLayout(t, "")
	if _, err := Apply(Plan{Extension: "pg_swarm", PreloadLibrary: "pg_swarm"}, layout, false); err != nil {
		t.Fatal(err)
	}
	if got, want := maxWorkers(t, layout), DefaultMaxWorkerProcesses+UndeclaredWorkers; got != want {
		t.Errorf("max_worker_processes = %d, want %d", got, want)
	}
}

// Libraries the operator preloads in postgresql.conf are theirs; the baseline
// covers them, and pgbrew never lowers a value they chose.
func TestApplyWorkerBudgetRespectsTheOperator(t *testing.T) {
	layout := testLayout(t, "shared_preload_libraries = 'pg_cron'\n")
	if _, err := Apply(Plan{Extension: "pg_kafka", PreloadLibrary: "pg_kafka", BackgroundWorkers: 4}, layout, false); err != nil {
		t.Fatal(err)
	}
	if got, want := maxWorkers(t, layout), DefaultMaxWorkerProcesses+4; got != want {
		t.Errorf("pg_cron from postgresql.conf was counted: max_worker_processes = %d, want %d", got, want)
	}

	layout = testLayout(t, "max_worker_processes = 64\n")
	if _, err := Apply(Plan{Extension: "pg_kafka", PreloadLibrary: "pg_kafka", BackgroundWorkers: 4}, layout, false); err != nil {
		t.Fatal(err)
	}
	if got := maxWorkers(t, layout); got != 64 {
		t.Errorf("lowered the operator's max_worker_processes = 64 to %d", got)
	}
}

func TestRemoveShrinksTheWorkerBudget(t *testing.T) {
	layout := testLayout(t, "")
	for _, p := range []Plan{
		{Extension: "pg_kafka", PreloadLibrary: "pg_kafka", BackgroundWorkers: 4},
		{Extension: "pg_mqtt", PreloadLibrary: "pg_mqtt", BackgroundWorkers: 4},
	} {
		if _, err := Apply(p, layout, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Remove("pg_mqtt", "pg_mqtt", layout, false); err != nil {
		t.Fatal(err)
	}
	if got, want := maxWorkers(t, layout), DefaultMaxWorkerProcesses+4; got != want {
		t.Errorf("after removing pg_mqtt: max_worker_processes = %d, want %d", got, want)
	}
}
