package pgrx

import (
	"strings"
	"testing"
)

func lookup(env []string, key string) (string, bool) {
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// A private transitive git dependency is the case this exists for: pgbrew
// clones with the git CLI and inherits the user's credentials, so the build
// must fetch the same way or it 401s halfway through one `pgx install`.
func TestGitFetchUsesTheGitCLIByDefault(t *testing.T) {
	env := withGitCLIFetch([]string{"PATH=/usr/bin"})
	if v, ok := lookup(env, "CARGO_NET_GIT_FETCH_WITH_CLI"); !ok || v != "true" {
		t.Fatalf("expected the git CLI fetcher, got %q (present: %v)", v, ok)
	}
}

// An explicit choice is not ours to overrule — including "false", which is the
// only way a user can ask for libgit2 back.
func TestExplicitGitFetchChoiceIsPreserved(t *testing.T) {
	for _, want := range []string{"false", "true"} {
		env := withGitCLIFetch([]string{"CARGO_NET_GIT_FETCH_WITH_CLI=" + want})
		if got, _ := lookup(env, "CARGO_NET_GIT_FETCH_WITH_CLI"); got != want {
			t.Errorf("overwrote an explicit %q with %q", want, got)
		}
		if len(env) != 1 {
			t.Errorf("expected no duplicate entry, got %v", env)
		}
	}
}

// A variable whose name merely starts the same must not be mistaken for it.
func TestGitFetchDoesNotMatchOnPrefix(t *testing.T) {
	env := withGitCLIFetch([]string{"CARGO_NET_GIT_FETCH_WITH_CLI_EXTRA=false"})
	if v, ok := lookup(env, "CARGO_NET_GIT_FETCH_WITH_CLI"); !ok || v != "true" {
		t.Fatalf("expected the setting to still be applied, got %q (present: %v)", v, ok)
	}
}

// The loader variable differs by platform, and setting the wrong one fails
// silently: the loader simply never looks where we pointed it.
func TestLibraryPathVarMatchesTheLoader(t *testing.T) {
	key := libraryPathVar()
	if key != "LD_LIBRARY_PATH" && key != "DYLD_LIBRARY_PATH" {
		t.Fatalf("unexpected loader variable %q", key)
	}

	env := withLDPath([]string{"PATH=/usr/bin"}, "/build/target/release/deps")
	v, ok := lookup(env, key)
	if !ok || !strings.HasPrefix(v, "/build/target/release/deps") {
		t.Fatalf("deps dir should lead %s, got %q (present: %v)", key, v, ok)
	}
}
