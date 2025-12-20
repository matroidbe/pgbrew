# pgbrew

Homebrew-inspired package manager for pgrx-based PostgreSQL extensions.

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

# Install extension from GitHub
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

- Go 1.21+ (for building pgx)
- Rust (for building pgrx extensions)
- cargo-pgrx (`cargo install cargo-pgrx`)
- PostgreSQL with development headers

## How It Works

1. `pgx install` clones the repository (or uses local path)
2. Detects pgrx extension (looks for `Cargo.toml` with pgrx dependency)
3. Runs `cargo pgrx install --release` to build and install
4. Tracks installation in `~/.pgbrew/installed.json`

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
