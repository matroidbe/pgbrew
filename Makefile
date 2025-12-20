.PHONY: build install clean test

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/matroidbe/pgbrew/internal/cmd.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/pgx ./cmd/pgx

install:
	go install $(LDFLAGS) ./cmd/pgx

clean:
	rm -rf bin/

test:
	go test ./...

# Development helpers
run-doctor: build
	./bin/pgx doctor

run-version: build
	./bin/pgx version
