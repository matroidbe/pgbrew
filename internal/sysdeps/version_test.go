package sysdeps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    Version
		wantErr bool
	}{
		{in: "7", want: Version{7}},
		{in: "7.6", want: Version{7, 6}},
		{in: "7.6.3", want: Version{7, 6, 3}},
		{in: "  7.6.3  ", want: Version{7, 6, 3}},
		// Distro version strings carry packaging suffixes.
		{in: "7.6.3+dfsg1-7.1build1", want: Version{7, 6, 3}},
		{in: "1.2.3-rc1", want: Version{1, 2, 3}},
		{in: "", wantErr: true},
		{in: "abc", wantErr: true},
	}

	for _, tt := range tests {
		got, err := ParseVersion(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q) = %v, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got.String() != tt.want.String() {
			t.Errorf("ParseVersion(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"7.6", "7.8", -1},
		{"7.8", "7.6", 1},
		{"7.6", "7.6", 0},
		// Missing trailing components count as zero.
		{"7.6", "7.6.0", 0},
		{"7.6", "7.6.1", -1},
		{"7.6.1", "7.6", 1},
		{"8.0", "7.9.9", 1},
	}

	for _, tt := range tests {
		a, err := ParseVersion(tt.a)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tt.a, err)
		}
		b, err := ParseVersion(tt.b)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tt.b, err)
		}
		if got := a.Compare(b); got != tt.want {
			t.Errorf("%s.Compare(%s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	v := Version{7, 6, 3}
	if !v.AtLeast(Version{7, 6}) {
		t.Error("7.6.3 should satisfy >= 7.6")
	}
	if v.AtLeast(Version{7, 8}) {
		t.Error("7.6.3 must not satisfy >= 7.8")
	}
	if !v.AtLeast(Version{7, 6, 3}) {
		t.Error("a version should satisfy >= itself")
	}
}

// occtHeader mirrors the real OCCT Standard_Version.hxx: the macros are
// documented in comments above the definitions, and padded with several spaces.
const occtHeader = `
//            OCC_VERSION_MAJOR       : (integer) number identifying major version
//            OCC_VERSION_MINOR       : (integer) number identifying minor version
#define OCC_VERSION_MAJOR         7
#define OCC_VERSION_MINOR         6
#define OCC_VERSION_MAINTENANCE   3
#define OCC_VERSION_HEX    (OCC_VERSION_MAJOR << 16 | OCC_VERSION_MINOR << 8)
`

func TestParseDefine(t *testing.T) {
	for _, tt := range []struct {
		macro string
		want  int
		ok    bool
	}{
		{"OCC_VERSION_MAJOR", 7, true},
		{"OCC_VERSION_MINOR", 6, true},
		{"OCC_VERSION_MAINTENANCE", 3, true},
		{"OCC_VERSION_NOPE", 0, false},
		// Non-integer values are not versions.
		{"OCC_VERSION_HEX", 0, false},
	} {
		got, ok := parseDefine(occtHeader, tt.macro)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseDefine(%q) = (%d, %v), want (%d, %v)", tt.macro, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseDefineDoesNotMatchLongerMacroName(t *testing.T) {
	header := "#define OCC_VERSION_MAJOR_EXT 9\n#define OCC_VERSION_MAJOR 7\n"
	if got, ok := parseDefine(header, "OCC_VERSION_MAJOR"); !ok || got != 7 {
		t.Errorf("parseDefine = (%d, %v), want (7, true) — must not match MAJOR_EXT", got, ok)
	}
}

func TestVersionFromHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Standard_Version.hxx")
	if err := os.WriteFile(path, []byte(occtHeader), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := versionFromHeader(path, VersionSpec{
		Major: "OCC_VERSION_MAJOR",
		Minor: "OCC_VERSION_MINOR",
		Patch: "OCC_VERSION_MAINTENANCE",
	})
	if err != nil {
		t.Fatalf("versionFromHeader: %v", err)
	}
	if v.String() != "7.6.3" {
		t.Errorf("got %s, want 7.6.3", v)
	}

	// Major alone is enough.
	v, err = versionFromHeader(path, VersionSpec{Major: "OCC_VERSION_MAJOR"})
	if err != nil || v.String() != "7" {
		t.Errorf("major-only: got %v (%v), want 7", v, err)
	}

	// A missing major macro is an error, not a zero version.
	if _, err := versionFromHeader(path, VersionSpec{Major: "NOT_THERE"}); err == nil {
		t.Error("expected an error for a missing major macro")
	}

	// A missing file is an error.
	if _, err := versionFromHeader(filepath.Join(dir, "nope.hxx"), VersionSpec{Major: "X"}); err == nil {
		t.Error("expected an error for a missing header")
	}
}
