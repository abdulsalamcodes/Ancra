# =============================================================================
# Ancra Makefile
# =============================================================================

# Load .env if it exists so DATABASE_URL etc. are available for migrate targets.
-include .env
export

BINARY      := ancra
BUILD_DIR   := ./bin
MAIN        := ./cmd/api/main.go
MIGRATE     := migrate           # expects golang-migrate CLI on PATH
MIGRATIONS  := db/migrations
DB_URL      ?= $(DATABASE_URL)

.PHONY: all build run test tidy migrate-up migrate-down lint clean help

## all: build the binary (default target)
all: build

## build: compile the API binary into ./bin/ancra
build:
	@echo "==> Building $(BINARY)…"
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) $(MAIN)
	@echo "==> Binary written to $(BUILD_DIR)/$(BINARY)"

## run: build and run the API server (loads .env automatically)
run:
	@echo "==> Running $(BINARY)…"
	go run $(MAIN)

## test: run all tests with race detector
test:
	@echo "==> Running tests…"
	go test -race -count=1 ./...

## tidy: tidy and vendor go modules
tidy:
	@echo "==> Tidying modules…"
	go mod tidy

## migrate-up: apply all pending migrations
migrate-up:
	@echo "==> Applying migrations (up)…"
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" up

## migrate-down: rollback the last migration
migrate-down:
	@echo "==> Rolling back last migration…"
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" down 1

## migrate-force: force migration version (usage: make migrate-force VERSION=1)
migrate-force:
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DB_URL)" force $(VERSION)

## lint: run staticcheck (install with: go install honnef.co/go/tools/cmd/staticcheck@latest)
lint:
	staticcheck ./...

## clean: remove compiled binaries
clean:
	@echo "==> Cleaning build artefacts…"
	rm -rf $(BUILD_DIR)

## help: print this help message
help:
	@grep -E '^##' Makefile | sed 's/## //' | column -t -s ':'
