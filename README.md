# Velnest

**Your Home, All in Order.** / **Rumahmu, Semua Teratur.**

Velnest is a household management platform: one place for a home's items, warranties, service schedules, documents, reminders and bills. This repository is the core monorepo (API, web app and shared API spec).

> Status: Foundation (W1). The repo skeleton, CI and deploy plumbing come first; product features land in later weeks. See `docs/` for the plan.

## Stack

- **API:** Go + Gin (`apps/api`), clean architecture, JSON logs on stdout, config embedded in the binary
- **Web:** Astro (`apps/web`)
- **Database:** Postgres 17 with pgvector (Supabase for staging, preview and production; `pgvector/pgvector:pg17` locally)
- **Monorepo:** [moon](https://moonrepo.dev) v2, toolchains pinned by proto (`.prototools`), pnpm workspaces for JavaScript
- **Hosting:** Vercel (two projects, region `sin1`) and Supabase (`ap-southeast-1`), both in Singapore
- **CI:** GitHub Actions running `moon ci`

## Repository layout

```text
.
├── .github/workflows/ci.yml   # moon ci + staging migrations
├── .moon/                     # moon workspace and toolchain config
├── .prototools                # pinned go, node, pnpm, moon
├── apps/
│   ├── api/                   # Go module
│   │   ├── cmd/
│   │   │   ├── server/        # Gin API (Vercel entrypoint, listens on $PORT)
│   │   │   ├── worker/        # skeleton in W1; River worker from W3
│   │   │   └── migrate/       # migration runner: up | down [n] | version
│   │   ├── configs/           # per-environment YAML, embedded in the binary
│   │   ├── migrations/        # SQL migrations
│   │   └── internal/          # handler, usecase, repository, model, pkg
│   └── web/                   # Astro app
├── packages/
│   └── openapi/               # OpenAPI spec (linted with Redocly)
├── docs/                      # PRD and build plan
├── docker-compose.yml         # local Postgres (pgvector)
└── .env.example               # documented environment variables
```

## Prerequisites

- [proto](https://moonrepo.dev/proto), which installs the pinned Go, Node, pnpm and moon:
  ```sh
  curl -fsSL https://moonrepo.dev/install/proto.sh | bash
  ```
- Docker (for the local database)

## Quick start

```sh
proto use                      # install the toolchains pinned in .prototools
pnpm install
cp .env.example .env           # optional reference; commands below set what they need
docker compose up -d db        # Postgres on localhost:54322

export DATABASE_URL='postgres://velnest:velnest@localhost:54322/velnest?sslmode=disable'
export DATABASE_MIGRATE_URL="$DATABASE_URL"
(cd apps/api && go run ./cmd/migrate up)

moon run api:dev               # API on http://localhost:7979
moon run web:dev               # Astro dev server
```

Run `api:dev` and `web:dev` in separate terminals. The API does not read `.env` by itself; export the variables in your shell (or use your own loader).

## Common commands

`make help` lists shortcuts that wrap the commands below (`make setup`, `make dev`, `make test`, `make check`, `make db-reset`, `make migrate`, ...). They only delegate to moon, docker compose and the migrate CLI; moon stays the source of truth. The target database is `VELNEST_DB_URL` (default: the local Docker Postgres). `migrate-down`, `db-reset`, `test` and `ci` refuse a non-local database unless you pass `FORCE=1`, because they drop data or create and drop databases.

| Command | What it does |
|---|---|
| `moon ci` | What CI runs: all tasks for projects affected by your changes |
| `moon run :test` | Run every project's `test` task |
| `moon run api:test` | Go tests (integration tests need `VELNEST_TEST_DATABASE_URL`) |
| `moon run api:lint` | golangci-lint (v2) |
| `moon run api:guard` | Table test for the Makefile's local-database guard (`scripts/test-make-guard.sh`) |
| `moon run api:build` | Build the Go binaries |
| `moon run api:migrate` | Run migrations through moon |
| `moon run web:build` | Production build of the web app |
| `moon run web:check` | `astro check` type checks |
| `moon run openapi:lint` | Lint the OpenAPI spec |
| `cd apps/api && go run ./cmd/migrate version` | Show the current migration version |

Integration tests read `VELNEST_TEST_DATABASE_URL`. Locally they skip when it is unset; when `CI` is set they fail instead. Point it at the local database:

```sh
export VELNEST_TEST_DATABASE_URL='postgres://velnest:velnest@localhost:54322/velnest?sslmode=disable'
```

## Environment variables

Documented with local values in [`.env.example`](.env.example).

| Variable | Used by | Notes |
|---|---|---|
| `ENV` | api | `development`, `staging` or `production`; selects the embedded config. Defaults to `development` locally; required on Vercel (startup fails without it) |
| `PORT` | api | HTTP port, default `7979` |
| `DATABASE_URL` | api runtime | Supabase transaction pooler (port 6543); the server uses the pgx simple protocol |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | api | Optional OTLP endpoint for traces and metrics |
| `DATABASE_MIGRATE_URL` | `cmd/migrate` | Supabase session pooler or direct connection; never port 6543 |
| `VELNEST_TEST_DATABASE_URL` | api tests | Test database for integration tests |

Staging, preview and production values live in Vercel environment variables (runtime) and GitHub environment secrets (migrations). Nothing secret belongs in the repo.

## Environments and deploy

- **Hosting:** two Vercel Pro projects by root directory (`apps/api` with the Go preset, `apps/web` with the Astro preset), region `sin1`. Each has an `ignoreCommand` in its `vercel.json` so it only builds when its own paths (or `packages/openapi`) change.
- **Database:** one Supabase project per environment (staging, preview, production) in `ap-southeast-1`. Preview deployments run code from any branch or pull request with whatever variables they are given, so Preview gets its own project and never a staging or production database URL.
- **Staging:** every merge to `main` deploys staging (a Vercel custom environment tracking `main`).
- **Migrations:** CI runs `go run ./cmd/migrate up` against staging after `moon ci` passes on `main`. Vercel can deploy before or after that job finishes, so migrations follow **expand-then-contract only**: add columns and tables first, remove old ones in a later release once no deployed code uses them.
- **Row level security:** tables have RLS enabled with no policies. The Go server connects as the table owner, so keep the Supabase Data API turned off.

## Provision checklist

One-time setup done by a human before the pipeline works end to end:

- [ ] Supabase staging, preview and production projects in `ap-southeast-1`, with the **Data API turned off**
- [ ] Two Vercel Pro projects: `apps/api` (Go preset) and `apps/web` (Astro preset), both region `sin1`
- [ ] Vercel custom environment `staging` tracking `main`
- [ ] Vercel env vars on the API project: `ENV` and `DATABASE_URL` (transaction pooler, port 6543). Set both for **every** Vercel environment you deploy to, each with its own database:
  - Production: `ENV=production`, `DATABASE_URL` of the production project
  - `staging` custom environment: `ENV=staging`, `DATABASE_URL` of the staging project
  - Preview: `ENV=staging` (there is no separate preview config; it reuses the staging one), `DATABASE_URL` of the **preview** project

  The server and worker exit at startup on Vercel when `ENV` is missing, on purpose, so a forgotten variable cannot fall back to development settings.
- [ ] GitHub environment `staging` with secret `STAGING_DATABASE_MIGRATE_URL`. GitHub runners are IPv4 only, so use Supabase's **session pooler** URL (port 5432 on the pooler host), not the IPv6-only direct host

## Known temporary items

- CI applies migrations to staging only. The preview project's schema is not migrated automatically yet: apply it by hand after each release that adds migrations, until a preview migration job is added. Enter the preview session-pooler URL at a prompt rather than on the command line, so the password stays out of your shell history and the process list (bash or zsh):
  ```sh
  printf 'preview session-pooler URL: '; read -rs VELNEST_DB_URL; echo
  export VELNEST_DB_URL && make migrate
  unset VELNEST_DB_URL
  ```
- `apps/api/internal/pkg/database` is a local DB adapter on purpose. The matching change (URL DSN plus a pgx simple-protocol option) is still owed upstream to `davidsugianto/go-pkgs` (`db`).
- The Vercel Go runtime and Vercel Daemons are beta. There is no Dockerfile yet: the container fallback (API and worker images) is planned for W3, together with the real worker. `cmd/worker` is only a skeleton until then.

## Docs

- [`docs/velnest-core-prd.md`](docs/velnest-core-prd.md): product requirements and architecture summary (sections 14.1, 14.2 and 14.4 cover deployment, backend structure and moon)
- [`docs/velnest-core-build-plan.html`](docs/velnest-core-build-plan.html): week-by-week build plan
