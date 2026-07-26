package sysdeps

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Version is a dotted numeric version, e.g. 7.6.3 -> [7 6 3].
type Version []int

// ParseVersion parses a dotted numeric version such as "7", "7.6" or "7.6.3".
//
// Trailing non-numeric detail is tolerated and dropped, so distro strings like
// "7.6.3+dfsg1-7.1build1" compare as 7.6.3.
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty version")
	}

	var v Version
	for _, part := range strings.Split(s, ".") {
		// Cut at the first non-digit: "3+dfsg1" -> "3".
		end := 0
		for end < len(part) && part[end] >= '0' && part[end] <= '9' {
			end++
		}
		if end == 0 {
			break // No leading digits: stop, keeping what we have.
		}
		n, err := strconv.Atoi(part[:end])
		if err != nil {
			break
		}
		v = append(v, n)
		if end != len(part) {
			break // Consumed the numeric head of a suffixed component.
		}
	}

	if len(v) == 0 {
		return nil, fmt.Errorf("no numeric components in version %q", s)
	}
	return v, nil
}

// Compare returns -1 if v < other, 0 if equal, +1 if v > other.
//
// Missing trailing components count as zero, so 7.6 == 7.6.0 and 7.6 < 7.6.1.
func (v Version) Compare(other Version) int {
	n := len(v)
	if len(other) > n {
		n = len(other)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(v) {
			a = v[i]
		}
		if i < len(other) {
			b = other[i]
		}
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	return 0
}

// AtLeast reports whether v >= min.
func (v Version) AtLeast(min Version) bool {
	return v.Compare(min) >= 0
}

// String renders the version in dotted form.
func (v Version) String() string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// versionFromHeader reads a version out of a C/C++ header by looking up integer
// `#define` macros, the way libraries conventionally publish their version.
func versionFromHeader(path string, spec VersionSpec) (Version, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)

	major, ok := parseDefine(text, spec.Major)
	if !ok {
		return nil, fmt.Errorf("macro %s not found in %s", spec.Major, path)
	}
	v := Version{major}

	if spec.Minor != "" {
		if minor, ok := parseDefine(text, spec.Minor); ok {
			v = append(v, minor)
		}
	}
	if spec.Patch != "" {
		if patch, ok := parseDefine(text, spec.Patch); ok {
			v = append(v, patch)
		}
	}
	return v, nil
}

// parseDefine extracts the integer value of `#define <name> <int>`.
//
// Comment lines are skipped, which matters because headers routinely document
// their own version macros in comments directly above the real definition.
func parseDefine(text, name string) (int, bool) {
	for _, line := range strings.Split(text, "\n") {
		rest := strings.TrimSpace(line)
		if !strings.HasPrefix(rest, "#define") {
			continue
		}
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "#define"))
		if !strings.HasPrefix(rest, name) {
			continue
		}
		rest = rest[len(name):]
		// Require whitespace after the name so MAJOR does not match MAJOR_EXT.
		if rest == "" || !isSpace(rest[0]) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if n, err := strconv.Atoi(fields[0]); err == nil {
			return n, true
		}
	}
	return 0, false
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r'
}
