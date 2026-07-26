package sysdeps

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// DefaultPrefixes are the installation prefixes searched when a dependency does
// not name its own. Homebrew's Linux prefix is included so a Linuxbrew-installed
// library is found without extra configuration.
var DefaultPrefixes = []string{
	"/usr",
	"/usr/local",
	"/opt/homebrew",
	"/opt/local",
	"/home/linuxbrew/.linuxbrew",
}

// Source records how a dependency was located.
type Source string

const (
	SourceEnv    Source = "environment override"
	SourceBrew   Source = "homebrew"
	SourcePrefix Source = "system prefix"
)

// Result is the outcome of probing for one dependency.
type Result struct {
	Dependency Dependency

	// Found reports whether the dependency was located at all.
	Found bool
	// Source records how it was located.
	Source Source

	Prefix     string
	IncludeDir string
	LibDir     string

	// Version is the detected version; VersionKnown says whether it is
	// meaningful. A version is not always discoverable (no [version] block, or
	// an explicit environment override that we take on trust).
	Version      Version
	VersionKnown bool

	// TooOld is set when a known version is below the declared min_version.
	TooOld bool
}

// OK reports whether the dependency is usable: present, and new enough.
func (r Result) OK() bool { return r.Found && !r.TooOld }

// Summary is a one-line human-readable status.
func (r Result) Summary() string {
	if !r.Found {
		return r.Dependency.Name + ": not found"
	}
	s := r.Dependency.Name
	if r.VersionKnown {
		s += " " + r.Version.String()
	}
	s += " at " + r.Prefix + " (" + string(r.Source) + ")"
	if r.TooOld {
		s += " — too old, need >= " + r.Dependency.MinVersion
	}
	return s
}

// Prober locates native dependencies. The injectable fields exist so the search
// can be tested without touching the real filesystem or Homebrew.
type Prober struct {
	// Prefixes is the search list. Defaults to DefaultPrefixes.
	Prefixes []string
	// Arch is the multiarch tag used for Debian-style library directories
	// (e.g. "x86_64" for /usr/lib/x86_64-linux-gnu).
	Arch string
	// BrewPrefix resolves a Homebrew formula to its prefix.
	BrewPrefix func(formula string) (string, bool)
	// LookupEnv reads an environment variable.
	LookupEnv func(key string) (string, bool)
}

// NewProber returns a Prober wired to the real system.
func NewProber() *Prober {
	return &Prober{
		Prefixes:   DefaultPrefixes,
		Arch:       multiarchTag(),
		BrewPrefix: brewPrefix,
		LookupEnv:  os.LookupEnv,
	}
}

// Probe locates a single dependency.
func (p *Prober) Probe(dep Dependency) Result {
	// 1. An explicit environment override wins outright. The user has told us
	//    where the library is; we take that at face value and do not enforce
	//    min_version against it, because we may not be able to read a version
	//    from an arbitrary layout and refusing to build would be obstructive.
	for _, key := range dep.EnvVars {
		value, ok := p.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		prefix := strings.TrimSpace(value)
		if !isDir(prefix) {
			continue
		}
		if r, ok := p.inspect(dep, prefix, SourceEnv); ok {
			return r
		}
		// The variable points somewhere real but not prefix-shaped (it may be
		// an include dir the extension's own build understands). Accept it.
		return Result{
			Dependency: dep,
			Found:      true,
			Source:     SourceEnv,
			Prefix:     prefix,
		}
	}

	// 2. Homebrew, if available. Checked before the system prefixes because a
	//    brew-installed copy is usually newer than the distro's.
	if p.BrewPrefix != nil {
		if prefix, ok := p.BrewPrefix(dep.Formula()); ok {
			if r, ok := p.inspect(dep, prefix, SourceBrew); ok {
				return r
			}
		}
	}

	// 3. Declared prefixes, then the defaults.
	prefixes := append(expandPrefixes(dep.Prefixes), p.searchPrefixes()...)
	for _, prefix := range prefixes {
		if r, ok := p.inspect(dep, prefix, SourcePrefix); ok {
			return r
		}
	}

	return Result{Dependency: dep}
}

// expandPrefixes resolves glob patterns in a dependency's declared prefixes.
//
// Some libraries only ever install under a version-stamped prefix — LLVM is
// /usr/lib/llvm-20 on one machine and /usr/lib/llvm-18 on the next — so a
// literal list cannot name them all. A pattern like "/usr/lib/llvm-*" can.
// Matches are searched newest-first (reverse lexical order), because a library
// that ships several versions side by side wants the newest one.
func expandPrefixes(prefixes []string) []string {
	var out []string
	for _, prefix := range prefixes {
		if !strings.ContainsAny(prefix, "*?[") {
			out = append(out, prefix)
			continue
		}
		matches, err := filepath.Glob(prefix)
		if err != nil {
			// A malformed pattern is the manifest author's problem, but it must
			// not abort the probe — the default prefixes may still find it.
			continue
		}
		dirs := make([]string, 0, len(matches))
		for _, match := range matches {
			if isDir(match) {
				dirs = append(dirs, match)
			}
		}
		sort.Sort(sort.Reverse(natural(dirs)))
		out = append(out, dirs...)
	}
	return out
}

// natural orders version-stamped paths so that llvm-20 sorts above llvm-9,
// which plain lexical order gets backwards.
type natural []string

func (n natural) Len() int      { return len(n) }
func (n natural) Swap(i, j int) { n[i], n[j] = n[j], n[i] }
func (n natural) Less(i, j int) bool {
	a, b := n[i], n[j]
	for len(a) > 0 && len(b) > 0 {
		ad, bd := isDigit(a[0]), isDigit(b[0])
		if ad && bd {
			ai, an := scanNumber(a)
			bi, bn := scanNumber(b)
			if ai != bi {
				return ai < bi
			}
			a, b = a[an:], b[bn:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// scanNumber reads the leading run of digits, returning its value and length.
func scanNumber(s string) (int, int) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, i
	}
	return n, i
}

// ProbeAll probes every dependency in a manifest.
func (p *Prober) ProbeAll(m *Manifest) []Result {
	results := make([]Result, 0, len(m.SystemDependencies))
	for _, dep := range m.SystemDependencies {
		results = append(results, p.Probe(dep))
	}
	return results
}

// inspect checks one prefix for the dependency's header and library.
func (p *Prober) inspect(dep Dependency, prefix string, source Source) (Result, bool) {
	includeDir := ""
	if dep.Header != "" {
		includeDir = p.findIncludeDir(prefix, dep.Header)
		if includeDir == "" {
			return Result{}, false
		}
	}

	libDir := ""
	if dep.Library != "" {
		libDir = p.findLibDir(prefix, dep.Library)
		if libDir == "" {
			return Result{}, false
		}
	}

	r := Result{
		Dependency: dep,
		Found:      true,
		Source:     source,
		Prefix:     prefix,
		IncludeDir: includeDir,
		LibDir:     libDir,
	}

	if dep.Version != nil && includeDir != "" {
		spec := *dep.Version
		if spec.Header == "" {
			spec.Header = dep.Header
		}
		if v, err := versionFromHeader(filepath.Join(includeDir, spec.Header), spec); err == nil {
			r.Version = v
			r.VersionKnown = true
		}
	}

	if r.VersionKnown && dep.MinVersion != "" {
		if min, err := ParseVersion(dep.MinVersion); err == nil && !r.Version.AtLeast(min) {
			r.TooOld = true
		}
	}

	return r, true
}

// findIncludeDir returns the include directory under prefix that contains the
// dependency's header.
func (p *Prober) findIncludeDir(prefix, header string) string {
	for _, sub := range []string{"include", ""} {
		dir := prefix
		if sub != "" {
			dir = filepath.Join(prefix, sub)
		}
		if isFile(filepath.Join(dir, header)) {
			return dir
		}
	}
	return ""
}

// findLibDir returns the library directory under prefix that contains the
// dependency's library, in any of the platform's shared/static forms.
func (p *Prober) findLibDir(prefix, library string) string {
	candidates := []string{
		filepath.Join(prefix, "lib"),
		filepath.Join(prefix, "lib64"),
	}
	if p.Arch != "" {
		// Debian/Ubuntu multiarch, e.g. /usr/lib/x86_64-linux-gnu
		candidates = append(candidates, filepath.Join(prefix, "lib", p.Arch+"-linux-gnu"))
	}

	names := []string{
		"lib" + library + ".so",
		"lib" + library + ".dylib",
		"lib" + library + ".a",
	}
	for _, dir := range candidates {
		for _, name := range names {
			if isFile(filepath.Join(dir, name)) {
				return dir
			}
		}
	}
	return ""
}

func (p *Prober) searchPrefixes() []string {
	if len(p.Prefixes) > 0 {
		return p.Prefixes
	}
	return DefaultPrefixes
}

// BuildEnv renders the environment variables a set of probe results should
// export into the extension's build, expanding {prefix}, {include} and {lib}.
//
// Only satisfied dependencies contribute, and a variable the user already set
// is never overridden — an explicit override outranks anything we detected.
func BuildEnv(results []Result, lookupEnv func(string) (string, bool)) []string {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	var env []string
	for _, r := range results {
		if !r.OK() {
			continue
		}
		for key, tmpl := range r.Dependency.Env {
			if existing, ok := lookupEnv(key); ok && strings.TrimSpace(existing) != "" {
				continue
			}
			value := expandPlaceholders(tmpl, r)
			if value == "" {
				continue
			}
			env = append(env, key+"="+value)
		}
	}
	return env
}

// expandPlaceholders substitutes {prefix}, {include} and {lib}. A template
// referencing a path the probe did not resolve expands to empty, so the
// variable is skipped rather than exported with a dangling value.
func expandPlaceholders(tmpl string, r Result) string {
	replacements := map[string]string{
		"{prefix}":  r.Prefix,
		"{include}": r.IncludeDir,
		"{lib}":     r.LibDir,
	}
	out := tmpl
	for placeholder, value := range replacements {
		if strings.Contains(out, placeholder) {
			if value == "" {
				return ""
			}
			out = strings.ReplaceAll(out, placeholder, value)
		}
	}
	return out
}

// brewPrefix asks Homebrew where a formula is installed.
func brewPrefix(formula string) (string, bool) {
	cmd := exec.Command("brew", "--prefix", formula)
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	prefix := strings.TrimSpace(string(output))
	if prefix == "" || !isDir(prefix) {
		return "", false
	}
	return prefix, true
}

// multiarchTag maps the Go architecture to the Debian multiarch directory tag.
func multiarchTag() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i386"
	default:
		return runtime.GOARCH
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
