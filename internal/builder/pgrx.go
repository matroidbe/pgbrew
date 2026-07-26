package builder

import (
	"github.com/matroidbe/pgbrew/internal/pgrx"
)

// PgrxBuilder implements the Builder interface for pgrx-based Rust extensions.
type PgrxBuilder struct{}

func init() {
	Register(&PgrxBuilder{})
}

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
	return pgrx.Install(dir, pgrx.InstallOptions{
		PgConfig: opts.PgConfig,
		UseSudo:  opts.UseSudo,
		Features: opts.Features,
		Env:      opts.Env,
	})
}

func (b *PgrxBuilder) Package(dir string, opts InstallOptions) (string, error) {
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
