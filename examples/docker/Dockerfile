# PostgreSQL with pgx extension manager
# Example: Build and install pg_kafka extension

FROM postgres:16

# Install build dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    git \
    curl \
    pkg-config \
    libssl-dev \
    librdkafka-dev \
    postgresql-server-dev-16 \
    && rm -rf /var/lib/apt/lists/*

# Install Go
RUN curl -fsSL https://go.dev/dl/go1.23.4.linux-amd64.tar.gz | tar -C /usr/local -xzf -
ENV PATH="/usr/local/go/bin:${PATH}"

# Install Rust (needed for pgrx extensions)
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Build and install pgx from source (includes auto pgrx-init feature)
WORKDIR /tmp
RUN git clone https://github.com/matroidbe/pgbrew.git && \
    cd pgbrew && \
    go build -o /usr/local/bin/pgx ./cmd/pgx && \
    rm -rf /tmp/pgbrew

# Install pg_kafka using pgx
RUN pgx install github.com/matroidbe/pg_extensions/extensions/pg_kafka

# Copy initialization script with proper permissions
COPY --chmod=644 init.sql /docker-entrypoint-initdb.d/

EXPOSE 5432

# Configure PostgreSQL for pg_kafka background worker
CMD ["postgres", "-c", "shared_preload_libraries=pg_kafka", "-c", "log_statement=all"]
