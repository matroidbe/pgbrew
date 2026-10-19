package cmd

import (
	"strings"
	"testing"

	"github.com/matroidbe/pgbrew/internal/cellar"
)

func TestPlanUpdate(t *testing.T) {
	local := t.TempDir()
	cases := []struct {
		name       string
		source     string
		ref        string
		bottle     string
		wantSource string
		wantBottle string
		wantErr    string
	}{
		{name: "github, unpinned, rebuilds the default branch",
			source: "github.com/pgvector/pgvector", wantSource: "github.com/pgvector/pgvector"},
		{name: "github, a new ref",
			source: "github.com/pgvector/pgvector@v0.8.7", ref: "v0.8.8", wantSource: "github.com/pgvector/pgvector@v0.8.8"},
		{name: "github subpath keeps the subpath",
			source: "github.com/matroidbe/pg_extensions/extensions/pg_kafka@v0.7.1", ref: "v0.7.2",
			wantSource: "github.com/matroidbe/pg_extensions/extensions/pg_kafka@v0.7.2"},
		{name: "a ref with a slash",
			source: "github.com/apache/age@PG18/v1.8.0-rc0", ref: "PG18/v1.8.0",
			wantSource: "github.com/apache/age@PG18/v1.8.0"},
		{name: "github, pinned, no new ref",
			source: "github.com/pgvector/pgvector@v0.8.7", wantErr: "name the version to update to"},
		{name: "a bottle needs a newer bottle",
			source: "https://example.com/vector-0.8.7-pg18-darwin-arm64.tar.gz", wantErr: "--bottle"},
		{name: "a local bottle file is a bottle too",
			source: "/bottles/vector-0.8.7-pg18-darwin-arm64.tar.gz", wantErr: "--bottle"},
		{name: "--bottle replaces any source",
			source: "github.com/pgvector/pgvector", bottle: "v.tar.gz", wantBottle: "v.tar.gz"},
		{name: "--bottle and @ref together",
			source: "github.com/pgvector/pgvector", ref: "v1", bottle: "v.tar.gz", wantErr: "not both"},
		{name: "local directory rebuilds in place",
			source: local, wantSource: local},
		{name: "local directory takes no ref",
			source: local, ref: "v1", wantErr: "local directory"},
		{name: "local directory that is gone",
			source: local + "/gone", wantErr: "no longer exists"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := planUpdate(cellar.Entry{Name: "ext", Source: c.source}, c.ref, c.bottle)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.source != c.wantSource || got.bottle != c.wantBottle {
				t.Errorf("plan = %+v, want source %q bottle %q", got, c.wantSource, c.wantBottle)
			}
		})
	}
}
