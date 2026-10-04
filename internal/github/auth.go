package github

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
)

// TokenVars are the environment variables a GitHub token is read from, in
// order of preference. Both are the names the gh CLI and GitHub Actions use,
// so a token that already authenticates `gh` authenticates pgbrew too.
var TokenVars = []string{"GH_TOKEN", "GITHUB_TOKEN"}

// Token returns the GitHub token from the environment, or "" if none is set.
func Token() string {
	for _, v := range TokenVars {
		if t := strings.TrimSpace(os.Getenv(v)); t != "" {
			return t
		}
	}
	return ""
}

// WithAuth returns env extended so that every git process started with it
// authenticates to github.com with the token from the environment. With no
// token it returns env unchanged, and git falls back to whatever credential
// helper, SSH agent or keychain the user already has.
//
// This is what makes a private repository installable where no credential
// helper exists — an image build, a CI job — and it has to reach two places:
// pgbrew's own clone, and cargo's fetch of the workspace's git dependencies
// (pgbrew already points cargo at the git CLI, see pgrx.withGitCLIFetch). Both
// are child processes, so the environment is the one channel that covers both.
//
// The token is passed as an HTTP header through GIT_CONFIG_COUNT, never in a
// URL or on a command line: a URL would be written into the clone's
// .git/config and printed in error messages, and argv is visible to every
// user on the machine. The header is scoped to https://github.com/, so the
// token is never sent to any other host a build happens to fetch from.
func WithAuth(env []string) []string {
	token := Token()
	if token == "" {
		return env
	}

	// Keep any GIT_CONFIG_* entries already present, and number ours after
	// them; overwriting index 0 would silently drop the caller's setting.
	n := 0
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "GIT_CONFIG_COUNT="); ok {
			if c, err := strconv.Atoi(v); err == nil && c > 0 {
				n = c
			}
		}
	}

	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	idx := strconv.Itoa(n)
	env = setEnv(env, "GIT_CONFIG_KEY_"+idx, "http.https://github.com/.extraheader")
	env = setEnv(env, "GIT_CONFIG_VALUE_"+idx, "Authorization: Basic "+basic)
	return setEnv(env, "GIT_CONFIG_COUNT", strconv.Itoa(n+1))
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
