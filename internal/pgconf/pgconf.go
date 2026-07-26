// Package pgconf applies the PostgreSQL configuration an extension needs.
//
// Installing the files is only half of making an extension work. Many need to
// be in shared_preload_libraries before their background workers start, and
// most carry GUC settings. Without those, the extension loads and quietly does
// nothing.
//
// Two rules shape everything here:
//
//   - shared_preload_libraries is a single cumulative list shared by every
//     preloaded extension. Writing it rather than merging it silently disables
//     whatever was already there, which is the easiest way to break a working
//     database.
//   - Changes go into a conf.d drop-in file, never into postgresql.conf. That
//     keeps them attributable per extension and removable on uninstall; editing
//     the main config in place is how a change becomes impossible to back out.
package pgconf

import (
	"fmt"
	"sort"
	"strings"
)

// PreloadSetting is the GUC that lists libraries loaded at server start.
const PreloadSetting = "shared_preload_libraries"

// DropInPrefix marks the files pgbrew manages, so they can be recognised and
// removed without touching anything the operator wrote.
const DropInPrefix = "pgbrew-"

// PreloadFileName holds the merged shared_preload_libraries value.
//
// It is one shared file rather than one per extension, because the setting is
// a single list: two per-extension files each assigning it would mean the last
// one read wins and the others are silently dropped. The name sorts first so
// the merged list is established before any per-extension file is read.
const PreloadFileName = "00-" + DropInPrefix + "shared-preload.conf"

// DropInName returns the per-extension drop-in filename.
func DropInName(extension string) string {
	return fmt.Sprintf("10-%s%s.conf", DropInPrefix, extension)
}

// IsManagedFile reports whether a conf.d filename is one of pgbrew's.
func IsManagedFile(name string) bool {
	return strings.HasPrefix(name, "00-"+DropInPrefix) || strings.HasPrefix(name, "10-"+DropInPrefix)
}

// ParsePreloadList splits a shared_preload_libraries value into library names.
//
// The value is a comma-separated list which may be quoted as a whole, and may
// carry spaces around entries.
func ParsePreloadList(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `'"`)

	var libs []string
	for _, part := range strings.Split(value, ",") {
		lib := strings.TrimSpace(part)
		lib = strings.Trim(lib, `'"`)
		lib = strings.TrimSpace(lib)
		if lib != "" {
			libs = append(libs, lib)
		}
	}
	return libs
}

// FormatPreloadList renders library names as a shared_preload_libraries value.
func FormatPreloadList(libs []string) string {
	return "'" + strings.Join(libs, ", ") + "'"
}

// MergePreload adds libraries to an existing list, preserving order and
// removing duplicates. It reports whether anything changed.
//
// Existing entries keep their position and new ones are appended, so the merge
// never reorders a list an operator arranged deliberately — load order can
// matter between interacting extensions.
func MergePreload(existing []string, add ...string) ([]string, bool) {
	merged := make([]string, 0, len(existing)+len(add))
	seen := map[string]bool{}

	for _, lib := range existing {
		if lib == "" || seen[lib] {
			continue
		}
		seen[lib] = true
		merged = append(merged, lib)
	}

	changed := len(merged) != len(existing)
	for _, lib := range add {
		lib = strings.TrimSpace(lib)
		if lib == "" || seen[lib] {
			continue
		}
		seen[lib] = true
		merged = append(merged, lib)
		changed = true
	}

	return merged, changed
}

// RemoveFromPreload drops libraries from a list, reporting whether it changed.
func RemoveFromPreload(existing []string, remove ...string) ([]string, bool) {
	drop := map[string]bool{}
	for _, lib := range remove {
		drop[strings.TrimSpace(lib)] = true
	}

	kept := make([]string, 0, len(existing))
	for _, lib := range existing {
		if !drop[lib] {
			kept = append(kept, lib)
		}
	}
	return kept, len(kept) != len(existing)
}

// FindSetting returns the value of a setting in a PostgreSQL config file.
//
// The last uncommented assignment wins, matching how PostgreSQL itself reads
// the file: later assignments override earlier ones.
func FindSetting(config, name string) (string, bool) {
	value := ""
	found := false

	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, rest, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}

		// Strip a trailing comment. A '#' inside quotes is part of the value.
		rest = stripInlineComment(rest)
		value = strings.TrimSpace(rest)
		found = true
	}

	return value, found
}

// stripInlineComment removes a trailing `# ...` that is not inside quotes.
func stripInlineComment(s string) string {
	inSingle, inDouble := false, false
	for i, r := range s {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return s[:i]
			}
		}
	}
	return s
}

// HasIncludeDir reports whether the config already includes the given
// directory, so pgbrew's drop-ins would actually be read.
func HasIncludeDir(config, dir string) bool {
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, rest, ok := strings.Cut(trimmed, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "include_dir") {
			continue
		}
		value := strings.TrimSpace(stripInlineComment(rest))
		value = strings.Trim(value, `'"`)
		if value == dir {
			return true
		}
	}
	return false
}

// IncludeDirLine is the directive appended to postgresql.conf so the drop-in
// directory is read. It is appended at the end so the drop-ins are read last
// and therefore take effect.
func IncludeDirLine(dir string) string {
	return fmt.Sprintf("\n# Added by pgbrew: read extension configuration drop-ins.\ninclude_dir = '%s'\n", dir)
}

// RenderPreloadFile renders the shared file holding the merged preload list.
func RenderPreloadFile(libs []string) string {
	var b strings.Builder
	b.WriteString("# Managed by pgbrew. Do not edit.\n")
	b.WriteString("#\n")
	b.WriteString("# shared_preload_libraries is a single cumulative list, so pgbrew keeps the\n")
	b.WriteString("# merged value in this one file rather than one file per extension.\n")
	b.WriteString("# Entries found in postgresql.conf are preserved.\n")
	fmt.Fprintf(&b, "%s = %s\n", PreloadSetting, FormatPreloadList(libs))
	return b.String()
}

// RenderDropIn renders one extension's settings file.
func RenderDropIn(extension string, settings map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by pgbrew for %s. Do not edit.\n", extension)
	fmt.Fprintf(&b, "# Remove with: pgx uninstall %s\n", extension)

	// Sorted, so regenerating produces an identical file.
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(&b, "%s = %s\n", key, quoteValue(settings[key]))
	}
	return b.String()
}

// quoteValue quotes a setting value unless it is already a safe bare literal.
//
// PostgreSQL accepts numbers and booleans unquoted; everything else is quoted,
// and embedded single quotes are doubled.
func quoteValue(value string) string {
	if value == "" {
		return "''"
	}
	if isBareLiteral(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func isBareLiteral(value string) bool {
	switch strings.ToLower(value) {
	case "on", "off", "true", "false", "yes", "no":
		return true
	}
	// A plain number, optionally with a unit PostgreSQL understands.
	hasDigit := false
	for i, r := range value {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.' || (r == '-' && i == 0):
			// part of a number
		default:
			return false
		}
	}
	return hasDigit
}
