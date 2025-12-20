# pgbrew

Homebrew-inspired package manager for PostgreSQL extensions.

## Installation

```bash
go install github.com/matroidbe/pgbrew/cmd/pgx@latest
```

## Usage

```bash
# Check prerequisites
pgx doctor

# Install extension from GitHub
pgx install github.com/matroidbe/pg_extensions/extensions/pg_kafka

# List installed extensions
pgx list

# Show extension info
pgx info pg_kafka

# Uninstall extension
pgx uninstall pg_kafka
```

## Requirements

- Go 1.21+
- Rust (for building pgrx extensions)
- cargo-pgrx (`cargo install cargo-pgrx`)
- PostgreSQL with development headers

## How It Works

1. `pgx install github.com/user/repo[/path]` clones the repository
2. Detects pgrx extension (looks for `Cargo.toml` with pgrx dependency)
3. Runs `cargo pgrx install` to build and install
4. Tracks installation in `~/.pgbrew/installed.json`

## Roadmap

- [ ] MVP: GitHub install for pgrx extensions
- [ ] Formula support (YAML definitions)
- [ ] Tap support (third-party formula repos)
- [ ] Bottle support (pre-built binaries)
- [ ] PGXS/make support (C extensions)

## License

MIT
