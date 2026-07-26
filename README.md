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

# Install to system PostgreSQL (requires sudo)
pgx install --sudo github.com/pgvector/pgvector

# List installed extensions
pgx list

# List all PostgreSQL extensions (including non-pgx)
pgx list --all

# Show extension info
pgx info pg_graphql

# Uninstall extension (dry run first)
pgx uninstall --dry-run pg_graphql
pgx uninstall pg_graphql

# Install an extension's native library dependencies too
pgx install --install-deps github.com/user/repo

# Upgrade pgx itself
pgx upgrade
```

## Bottles (prebuilt artifacts)

Building an extension from source costs a Rust toolchain, cargo-pgrx, whatever
native libraries it links against, and several minutes of compiling. None of
that is inherent — the output is a handful of files, and the same files work on
any machine with a matching PostgreSQL major version, OS and architecture.

A **bottle** is those files plus a manifest. Installing one is a download, a
checksum check and a copy.

```bash
# Build a bottle (needs the full toolchain, once, on one machine)
pgx bottle ./pg_solid -o dist/
# -> dist/pg_solid-0.2.0-pg16-linux-amd64.tar.gz

# Install it anywhere (needs none of the toolchain)
pgx install --bottle dist/pg_solid-0.2.0-pg16-linux-amd64.tar.gz
pgx install --bottle https://example.com/bottles/pg_solid-0.2.0-pg16-linux-amd64.tar.gz
```

Publish the file wherever you like — a GitHub release, an object store, a file
share.

### Why the target is in the filename

A PostgreSQL extension is a shared library loaded into a running server
process, so it must agree with that process on ABI. There is no such thing as
one artifact that works everywhere; a bottle is inherently keyed by
`(version, pg_major, os, arch)`. `pgx install --bottle` refuses a bottle that
does not match the host rather than installing something that would fail at
load time.

### What is in a bottle

```
bottle.json                              manifest: name, version, target, checksums
lib/pg_solid.so                       -> pkglibdir
share/extension/pg_solid.control      -> sharedir
share/extension/pg_solid--0.2.0.sql   -> sharedir
```

Files are stored by role rather than by absolute path, which is what makes a
bottle relocatable across PostgreSQL installations.

Everything is verified before anything is written: the format version, every
path (a bottle cannot contain an absolute path, a `..` segment, a symlink, or
anything outside `lib/` and `share/`), and every file's SHA-256. A bottle
arrives over a network and is unpacked into privileged directories, so this is
checked rather than assumed.

## System Dependencies

Some extensions need a native library present *before* they can compile —
pg_solid needs OpenCASCADE, for instance. Without knowing about these, the
failure mode is a wall of linker errors.

An extension declares what it needs in a `pgbrew.toml` (or a
`[package.metadata.pgbrew]` table in `Cargo.toml`). pgbrew then probes for each
one before building:

```bash
# Report what an extension needs and whether it is satisfied
pgx doctor ./pg_solid

# Install missing dependencies with the platform package manager
pgx install --install-deps ./pg_solid

# Force a specific package manager
pgx install --install-deps --deps-via brew ./pg_solid

# Skip the check entirely
pgx install --skip-dep-check ./pg_solid
```

### In a terminal, pgbrew asks

When a dependency is missing and you are on a terminal, the build stops before
it starts and you get a menu:

```
Missing system dependencies:

  ✗ opencascade: found 7.6.3 at /usr, but >= 7.8 is required

How would you like to proceed?

  1) Install with apt   (may not have a new enough version)   sudo apt-get install -y libocct-foundation-dev
  2) Install with brew  (usually carries newer versions)      brew install opencascade
  3) Print the install command and exit
  4) Continue anyway (skip the dependency check)
  5) Abort

Choice [1]:
```

Pressing Enter takes the first option. Note the annotations: when a library is
present but too old, the distro's package usually cannot fix it and Homebrew
usually can, so the menu says which is which instead of leaving you to work it
out.

After installing, pgbrew **re-probes** rather than assuming success — the common
failure is a package that installs happily but is older than the extension
needs. If it is still unsatisfied you come back to the menu, with the
alternative right there.

### Everywhere else, it tells you

Outside a terminal — a pipeline, CI, a Dockerfile — prompting would hang the
build, so pgbrew reports and fails instead:

```
Missing system dependencies:

  ✗ opencascade: found 7.6.3 at /usr, but >= 7.8 is required

Install with:
  sudo apt-get install -y libocct-foundation-dev libocct-data-exchange-dev

Or let pgbrew do it:
  pgx install --install-deps <source>

The installed version is too old and your distro may not carry a newer one.
Homebrew usually does, and works on Linux:
  pgx install --install-deps --deps-via brew <source>
```

`--install-deps` never prompts, on a terminal or otherwise: you have already
answered the question. A closed stdin aborts rather than being read as
agreement.

Supported package managers: `apt`, `dnf`, `pacman`, `apk`, `zypper`, `brew`.
A native manager is preferred by default; Homebrew is used on macOS and is
available on Linux too, which matters when a distro's package is older than the
extension requires.

### Where a dependency is found

Probed in order — first hit wins:

1. Environment variables the extension declares (e.g. `OCCT_ROOT`). If you have
   set one, pgbrew defers to it and does not second-guess your choice.
2. `brew --prefix <formula>`, since a Homebrew copy is usually newer than the
   distro's.
3. Prefixes declared by the extension, then `/usr`, `/usr/local`,
   `/opt/homebrew`, `/opt/local`, `/home/linuxbrew/.linuxbrew`.

Whatever is found is exported into the build (as the variables the manifest
names), so an extension installed in a non-standard prefix is compiled and
linked against the right copy.

### Manifest format

```toml
[[system_dependencies]]
name = "opencascade"
header = "opencascade/Standard_Version.hxx"   # probed under <prefix>/include
library = "TKernel"                            # probed as libTKernel.{so,dylib,a}
min_version = "7.6"
brew_formula = "opencascade"
env_vars = ["OCCT_ROOT", "OCCT_INCLUDE_DIR"]   # user overrides to respect

# Read the installed version from integer #define macros in a header.
[system_dependencies.version]
major = "OCC_VERSION_MAJOR"
minor = "OCC_VERSION_MINOR"
patch = "OCC_VERSION_MAINTENANCE"

# Exported into the build; {prefix}, {include} and {lib} are substituted.
[system_dependencies.env]
OCCT_ROOT = "{prefix}"

[system_dependencies.packages]
apt = ["libocct-foundation-dev", "libocct-data-exchange-dev"]
dnf = ["opencascade-devel"]
brew = ["opencascade"]
```

Extensions that declare no manifest are unaffected — the check is a no-op for
them.

## Installing to System PostgreSQL

System-installed PostgreSQL typically has extension directories owned by root. Use the `--sudo` flag to install with elevated permissions:

```bash
pgx install --sudo github.com/pgvector/pgvector
```

This runs the installation step (`make install` or `cargo pgrx install`) with sudo while keeping the build step as your regular user.

## Multiple PostgreSQL Versions

Use the `PG_CONFIG` environment variable to target a specific PostgreSQL installation:

```bash
PG_CONFIG=/usr/lib/postgresql/16/bin/pg_config pgx install github.com/user/repo
PG_CONFIG=/usr/lib/postgresql/16/bin/pg_config pgx doctor

# Combine with --sudo for system PostgreSQL
PG_CONFIG=/usr/lib/postgresql/16/bin/pg_config pgx install --sudo github.com/pgvector/pgvector
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
