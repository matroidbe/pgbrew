# PostgreSQL with pgx Docker Example

This example shows how to use pgx to install PostgreSQL extensions in a Docker container.

## Quick Start

```bash
# Build and run
docker compose up -d

# Connect to PostgreSQL
psql -h localhost -U postgres -d testdb
# Password: postgres

# Verify extension is installed
\dx
```

## What's Included

- PostgreSQL 16
- pgx extension manager
- pg_kafka extension (pre-installed)
- Rust toolchain (for pgrx extensions)
- Go (for building pgx)

## Customization

To install different extensions, modify the Dockerfile:

```dockerfile
# Install a C extension
RUN pgx install github.com/pgvector/pgvector

# Install a Rust/pgrx extension
RUN pgx install github.com/supabase/pg_graphql
```

## Building

```bash
docker compose build --no-cache
```

## Connecting

```bash
# Using psql
psql -h localhost -U postgres -d testdb

# Using docker exec
docker exec -it pgx-postgres psql -U postgres -d testdb
```
