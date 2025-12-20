# pgbrew

Homebrew-inspired package manager for PostgreSQL extensions. Supports both **pgrx** (Rust) and **PGXS** (C) extensions.

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/matroidbe/pgbrew/main/install.sh | bash
```

Or with Go:
```bash
go install github.com/matroidbe/pgbrew/cmd/pgx@latest
```

## Usage

```bash
# Check prerequisites
pgx doctor

# Install C extension from GitHub
pgx install github.com/pgvector/pgvector

# Install Rust/pgrx extension from GitHub
pgx install github.com/supabase/pg_graphql

# Install specific version/tag/branch
pgx install github.com/supabase/pg_graphql@v1.5.0

# Install from monorepo subdirectory
pgx install github.com/user/repo/extensions/myext@main

# Install from local directory
pgx install ./my_extension

# List installed extensions
pgx list

# List all PostgreSQL extensions (including non-pgx)
pgx list --all

# Show extension info
pgx info pg_graphql

# Uninstall extension (dry run first)
pgx uninstall --dry-run pg_graphql
pgx uninstall pg_graphql

# Upgrade pgx itself
pgx upgrade
```

## Multiple PostgreSQL Versions

Use the `PG_CONFIG` environment variable to target a specific PostgreSQL installation:

```bash
PG_CONFIG=/usr/lib/postgresql/16/bin/pg_config pgx install github.com/user/repo
PG_CONFIG=/usr/lib/postgresql/16/bin/pg_config pgx doctor
```

## Requirements

**For all extensions:**
- Go 1.21+ (for building pgx)
- PostgreSQL with development headers
- Git

**For C extensions (PGXS):**
- GCC or compatible C compiler
- Make

**For Rust extensions (pgrx):**
- Rust toolchain
- cargo-pgrx (`cargo install cargo-pgrx`)

## How It Works

1. `pgx install` clones the repository (or uses local path)
2. Auto-detects extension type:
   - **pgrx (Rust)**: `Cargo.toml` with pgrx dependency
   - **PGXS (C)**: `Makefile` with PGXS + `.control` file
3. For pgrx: Automatically installs the correct `cargo-pgrx` version
4. Builds and installs the extension
5. Tracks installation in `~/.pgbrew/installed.json`

## Automatic cargo-pgrx Version Management

Different pgrx extensions require specific versions of `cargo-pgrx`. pgx automatically:

- Detects the required pgrx version from the extension's `Cargo.toml`
- Compares it to your installed `cargo-pgrx` version
- Installs the matching version if needed

This means you can install extensions built with different pgrx versions without manual intervention.

## Tested Extensions

### C Extensions (PGXS)

| Extension | Description | Install Command |
|-----------|-------------|-----------------|
| [pgvector](https://github.com/pgvector/pgvector) | Vector similarity search | `pgx install github.com/pgvector/pgvector` |
| [pg_cron](https://github.com/citusdata/pg_cron) | Cron-based job scheduler | `pgx install github.com/citusdata/pg_cron` |
| [pg_partman](https://github.com/pgpartman/pg_partman) | Partition management | `pgx install github.com/pgpartman/pg_partman` |

### Rust Extensions (pgrx)

| Extension | Description | Install Command |
|-----------|-------------|-----------------|
| [pg_uuidv7](https://github.com/craigpastro/pg_uuidv7) | UUIDv7 generation | `pgx install github.com/craigpastro/pg_uuidv7` |
| [pg_graphql](https://github.com/supabase/pg_graphql) | GraphQL for PostgreSQL | `pgx install github.com/supabase/pg_graphql` |
| [plprql](https://github.com/kaspermarstal/plprql) | PRQL language for PostgreSQL | `pgx install github.com/kaspermarstal/plprql/plprql` |
| [pg_search](https://github.com/paradedb/paradedb) | Full-text search with BM25 | `pgx install github.com/paradedb/paradedb/pg_search` |

Note: Some extensions are in monorepos and require a subdirectory path.

## Shell Completions

```bash
# Bash
pgx completion bash > ~/.local/share/bash-completion/completions/pgx

# Zsh
pgx completion zsh > ~/.zsh/completions/_pgx

# Fish
pgx completion fish > ~/.config/fish/completions/pgx.fish
```

## License

MIT
