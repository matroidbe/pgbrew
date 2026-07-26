package sysdeps

import (
	"strings"
	"testing"
)

func envValue(env []string, key string) (string, bool) {
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func TestApplyEnvAppendsNewKeys(t *testing.T) {
	env := ApplyEnv([]string{"PATH=/bin"}, []string{"OCCT_ROOT=/opt/occt"})
	if v, ok := envValue(env, "OCCT_ROOT"); !ok || v != "/opt/occt" {
		t.Errorf("OCCT_ROOT = %q (%v)", v, ok)
	}
	if v, _ := envValue(env, "PATH"); v != "/bin" {
		t.Errorf("PATH was clobbered: %q", v)
	}
}

func TestApplyEnvReplacesExistingKey(t *testing.T) {
	env := ApplyEnv([]string{"OCCT_ROOT=/old", "PATH=/bin"}, []string{"OCCT_ROOT=/new"})
	if v, _ := envValue(env, "OCCT_ROOT"); v != "/new" {
		t.Errorf("OCCT_ROOT = %q, want /new", v)
	}
	// Replacement, not duplication — a duplicate key is resolved unpredictably
	// by the child process.
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "OCCT_ROOT=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("found %d OCCT_ROOT entries, want exactly 1", count)
	}
}

func TestApplyEnvIgnoresMalformedPairs(t *testing.T) {
	env := ApplyEnv([]string{"PATH=/bin"}, []string{"NOEQUALS", "=novalue", ""})
	if len(env) != 1 {
		t.Errorf("malformed pairs should be skipped, got %v", env)
	}
}

func TestApplyEnvPreservesValuesContainingEquals(t *testing.T) {
	env := ApplyEnv(nil, []string{"CXXFLAGS=-DA=1 -DB=2"})
	if v, _ := envValue(env, "CXXFLAGS"); v != "-DA=1 -DB=2" {
		t.Errorf("CXXFLAGS = %q, want the full value after the first =", v)
	}
}

func TestEnvKeys(t *testing.T) {
	keys := EnvKeys([]string{"A=1", "B=2", "malformed", "=x"})
	if len(keys) != 2 || keys[0] != "A" || keys[1] != "B" {
		t.Errorf("EnvKeys = %v, want [A B]", keys)
	}
}
