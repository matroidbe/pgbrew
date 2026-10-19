package cmd

import (
	"strings"
	"testing"

	"github.com/matroidbe/pgbrew/internal/bottle"
)

func TestCheckRuntimeRequirementsNamesTheFormulae(t *testing.T) {
	m := bottle.Manifest{Name: "postgis", Requires: []bottle.RuntimeLibrary{
		{Path: "/nonexistent/opt/geos/lib/libgeos_c.1.dylib", Formula: "geos"},
		{Path: "/nonexistent/opt/proj/lib/libproj.25.dylib", Formula: "proj"},
	}}
	err := checkRuntimeRequirements(m)
	if err == nil {
		t.Fatal("a bottle with missing libraries installed")
	}
	for _, want := range []string{"libgeos_c.1.dylib", "brew install geos proj", "--install-deps"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}

	orig := skipDepChecks
	skipDepChecks = true
	defer func() { skipDepChecks = orig }()
	if err := checkRuntimeRequirements(m); err != nil {
		t.Errorf("--skip-dep-check still refused: %v", err)
	}
}
