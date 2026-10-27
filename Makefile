.PHONY: help setup db-up db-down db-reset db-psql migrate migrate-down migrate-version dev dev-api dev-web build test lint check ci

# Local Postgres from docker-compose.yml (pgvector/pgvector:pg17).
# Override per call: make migrate VELNEST_DB_URL=postgres://...
# The name is Velnest-specific on purpose: a generic DB_URL exported by another
# project in your shell must never redirect migrations.
VELNEST_DB_URL ?= postgres://velnest:velnest@localhost:54322/velnest?sslmode=disable
# Exported and read as "$$VELNEST_DB_URL" in recipes, so make never echoes the
# URL (it may carry credentials when overridden) into terminal or CI logs.
export VELNEST_DB_URL

# Number of migrations rolled back by `make migrate-down`.
N ?= 1

# Set FORCE=1 to run destructive targets against a non-local database.
FORCE ?=

# Shell snippet: sets $$host (never the credentials) and $$local (1 or 0) from
# $$VELNEST_DB_URL. Done in the recipe because make 3.81 (macOS) does not pass
# exported variables to $(shell). The URL counts as local only if (1) it parses
# as scheme://[user[:password]@]host[:port][/path][?query] with exactly one "@"
# in the authority (parsers split at the LAST "@", so a second one could hide the
# real host), (2) the host is local, and (3) every query parameter is on a short
# harmless list: drivers let ?host=, ?hostaddr= or ?service= override the host in
# the URL, so any other (or percent-encoded) parameter fails closed. A URL that
# does not parse yields host "(unparseable)", never any part of the URL.
DB_HOST_SH = host=$$(printf '%s' "$$VELNEST_DB_URL" | sed -nE 's,^[a-zA-Z0-9+]+://([^@/?]*@)?(\[[^]]+\]|[^:/?@]+)(:[0-9]*)?([/?].*)?$$,\2,p'); case "$$host" in localhost|127.0.0.1|'[::1]') local=1;; *) local=0;; esac; [ -n "$$host" ] || host='(unparseable)'; q=$$(printf '%s' "$$VELNEST_DB_URL" | sed -n 's,^[^?]*?,,p'); if printf '%s' "$$q" | tr '&' '\n' | grep -qvE '^(sslmode|connect_timeout|application_name|search_path)(=.*)?$$'; then local=0; fi

# Default target
.DEFAULT_GOAL := help

# Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*?# "} /^[a-zA-Z_-]+:.*?# / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "Variables: VELNEST_DB_URL (default: local docker Postgres), N (migrations to roll back), FORCE=1"

# Fail early with a hint when the pinned toolchains are not installed.
require-moon:
	@command -v moon >/dev/null 2>&1 || { echo "moon not found: install proto (https://moonrepo.dev/docs/proto/install), then run 'make setup'"; exit 1; }

# Refuse targets that drop data or create/drop databases unless the database
# host is local (or FORCE=1). Prints the host only, never the credentials.
require-local-db:
	@$(DB_HOST_SH); if [ "$$local" != 1 ] && [ "$(FORCE)" != 1 ]; then echo "refusing: database target is not provably local (host '$$host'; a non-local host or any URL query parameter other than sslmode, connect_timeout, application_name or search_path counts as non-local); set FORCE=1 to override"; exit 1; fi

# Start the local Postgres only when the target database is local.
ensure-db:
	@$(DB_HOST_SH); if [ "$$local" = 1 ]; then docker compose up -d --wait db; else echo "database target is not provably local (host '$$host'); not starting local Postgres"; fi

setup: # Install the pinned toolchains (proto) and JS dependencies (pnpm)
	proto use
	pnpm install

db-up: # Start local Postgres and wait until it is healthy
	docker compose up -d --wait db

db-down: # Stop local Postgres (data is kept)
	docker compose stop db

db-reset: require-local-db # DESTROY the local database volume, restart Postgres and re-apply migrations
	docker compose down -v
	$(MAKE) db-up
	$(MAKE) migrate

db-psql: db-up # Open psql in the local Postgres container
	docker compose exec db psql -U velnest -d velnest

migrate: ensure-db # Apply pending migrations to VELNEST_DB_URL
	cd apps/api && DATABASE_MIGRATE_URL="$$VELNEST_DB_URL" go run ./cmd/migrate up

migrate-down: require-local-db ensure-db # Roll back N migrations (default N=1); local databases only
	cd apps/api && DATABASE_MIGRATE_URL="$$VELNEST_DB_URL" go run ./cmd/migrate down $(N)

migrate-version: ensure-db # Show the current migration version of VELNEST_DB_URL
	cd apps/api && DATABASE_MIGRATE_URL="$$VELNEST_DB_URL" go run ./cmd/migrate version

dev: require-moon db-up # Run the API and the web app together (Ctrl+C stops both)
	moon run api:dev web:dev

dev-api: require-moon db-up # Run only the API on http://localhost:7979
	moon run api:dev

dev-web: require-moon # Run only the Astro dev server
	moon run web:dev

build: require-moon # Build the Go binaries and the web app
	moon run api:build web:build

test: require-moon require-local-db ensure-db # Run the Go tests against the local database
	VELNEST_TEST_DATABASE_URL="$$VELNEST_DB_URL" moon run api:test

lint: require-moon # Lint the Go code and the OpenAPI spec
	moon run api:lint openapi:lint

check: require-moon # Lint, type-check and test everything (fast pre-push check)
	$(MAKE) lint
	moon run web:check
	$(MAKE) test

ci: require-moon require-local-db ensure-db # Run exactly what GitHub Actions runs (moon ci, CI=true)
	CI=true VELNEST_TEST_DATABASE_URL="$$VELNEST_DB_URL" moon ci
