package builder

import (
	"fmt"

	"github.com/matroidbe/pgbrew/internal/pgrx"
)

// PgrxBuilder implements the Builder interface for pgrx-based Rust extensions.
type PgrxBuilder struct{}

func (b *PgrxBuilder) Name() string {
	return "pgrx"
}

func (b *PgrxBuilder) Detect(dir string) bool {
	return pgrx.IsProject(dir)
}

func (b *PgrxBuilder) GetExtensionName(dir string) (string, error) {
	return pgrx.GetExtensionName(dir)
}

func (b *PgrxBuilder) GetVersion(dir string) (string, error) {
	return pgrx.GetVersion(dir)
}

func (b *PgrxBuilder) Install(dir string, opts InstallOptions) error {
	if err := rejectBuildVars(opts); err != nil {
		return err
	}
	return pgrx.Install(dir, pgrx.InstallOptions{
		PgConfig: opts.PgConfig,
		UseSudo:  opts.UseSudo,
		Features: opts.Features,
		Env:      opts.Env,
	})
}

func (b *PgrxBuilder) Package(dir string, opts InstallOptions) (string, error) {
	if err := rejectBuildVars(opts); err != nil {
		return "", err
	}
	return pgrx.Package(dir, pgrx.InstallOptions{
		PgConfig: opts.PgConfig,
		UseSudo:  opts.UseSudo,
		Features: opts.Features,
		Env:      opts.Env,
	})
}

func (b *PgrxBuilder) NeedsSharedPreload(dir string) bool {
	return pgrx.NeedsSharedPreload(dir)
}

// rejectBuildVars refuses --build-var for pgrx, whose build is cargo: there is
// no make or CMake variable to set, and silently ignoring one would build
// something other than what was asked for. --features is the cargo knob.
func rejectBuildVars(opts InstallOptions) error {
	if len(opts.BuildVars) > 0 {
		return fmt.Errorf("--build-var applies to PGXS and CMake builds; this is a pgrx project (use --features)")
	}
	return nil
}
