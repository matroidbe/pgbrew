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

func (b *PgrxBuilder) Install(dir string, pgConfig string) error {
	return pgrx.Install(dir)
}

func (b *PgrxBuilder) NeedsSharedPreload(dir string) bool {
	return pgrx.NeedsSharedPreload(dir)
}
