package sysdeps

import "strings"

// ApplyEnv merges KEY=VALUE pairs into an environment slice, replacing any
// existing entry for the same key and appending the rest.
//
// Builders use this to inject the system-dependency locations pgbrew
// discovered into the environment of the compiler they invoke.
func ApplyEnv(env []string, pairs []string) []string {
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			continue
		}
		env = setEnv(env, key, value)
	}
	return env
}

// EnvKeys returns the keys of a set of KEY=VALUE pairs, in order.
//
// Needed because sudo drops the environment by default: the keys have to be
// named explicitly in --preserve-env for them to survive into the build.
func EnvKeys(pairs []string) []string {
	keys := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		key, _, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// setEnv replaces or appends a KEY=VALUE pair.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
