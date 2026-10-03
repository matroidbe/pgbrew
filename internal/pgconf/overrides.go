package pgconf

import (
	"fmt"
	"sort"
	"strings"
)

// directives are config-file keywords that look like settings but are not.
// Written into a drop-in they would pull in other files, which is not what
// overriding a setting means.
var directives = map[string]bool{
	"include":           true,
	"include_dir":       true,
	"include_if_exists": true,
}

// ParseOverrides parses `key=value` settings given on the command line.
//
// Only the first '=' separates key from value, so values may contain '=' and
// commas. Surrounding whitespace is trimmed, and one layer of matching quotes
// around the value is removed (quoting happens again when the drop-in is
// rendered). GUC names are case-insensitive, so a later assignment of the same
// name wins regardless of case.
//
// shared_preload_libraries is rejected: it is a cumulative list pgbrew merges,
// and setting it would drop every other preloaded library.
func ParseOverrides(args []string) (map[string]string, error) {
	overrides := map[string]string{}
	spelling := map[string]string{} // lower-cased name -> key in overrides

	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, fmt.Errorf("%q: expected key=value", arg)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" {
			return nil, fmt.Errorf("%q: empty key", arg)
		}
		if !validSettingName(key) {
			return nil, fmt.Errorf("%q: invalid setting name %q", arg, key)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%q: value may not contain a newline", key)
		}

		lower := strings.ToLower(key)
		if lower == PreloadSetting {
			return nil, fmt.Errorf(
				"%s cannot be set: it is one list shared by every preloaded "+
					"extension, so it is merged, never set (pgbrew adds the "+
					"extension's library when its manifest declares one)", PreloadSetting)
		}
		if directives[lower] {
			return nil, fmt.Errorf("%s is a config file directive, not a setting", lower)
		}

		if prev, seen := spelling[lower]; seen {
			delete(overrides, prev)
		}
		spelling[lower] = key
		overrides[key] = unquote(value)
	}
	return overrides, nil
}

// validSettingName accepts the characters PostgreSQL allows in a GUC name:
// letters, digits, '_', '$', and '.' for an extension's namespaced settings.
func validSettingName(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '.' || r == '$':
		default:
			return false
		}
	}
	return true
}

// unquote removes one layer of matching quotes. Inside single quotes a doubled
// quote is PostgreSQL's escape for a literal one.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if first != last || (first != '\'' && first != '"') {
		return value
	}
	inner := value[1 : len(value)-1]
	if first == '\'' {
		inner = strings.ReplaceAll(inner, "''", "'")
	}
	return inner
}

// WithOverrides returns a copy of the plan with settings given on the command
// line applied over the declared ones, and the overridden keys the extension
// did not declare (sorted), so the caller can point them out.
//
// A key matching a declared one case-insensitively replaces it under the
// declared spelling, so the drop-in never assigns the same setting twice.
// The plan's own Settings map is not modified: it is usually the manifest's.
func (p Plan) WithOverrides(overrides map[string]string) (Plan, []string) {
	if len(overrides) == 0 {
		return p, nil
	}

	settings := make(map[string]string, len(p.Settings)+len(overrides))
	declared := make(map[string]string, len(p.Settings)) // lower -> spelling
	for k, v := range p.Settings {
		settings[k] = v
		declared[strings.ToLower(k)] = k
	}

	set := make(map[string]bool, len(p.Set)+len(overrides))
	for k := range p.Set {
		set[k] = true
	}

	var undeclared []string
	for key, value := range overrides {
		if spelled, ok := declared[strings.ToLower(key)]; ok {
			key = spelled
		} else {
			undeclared = append(undeclared, key)
		}
		settings[key] = value
		set[key] = true
	}
	sort.Strings(undeclared)

	p.Settings = settings
	p.Set = set
	return p, undeclared
}
