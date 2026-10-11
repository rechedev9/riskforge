---
summary: "Prerequisites, environment variables, Make targets, Spanner emulator and seed data"
read_when: [setting up locally, running the emulator, environment variables, Make targets]
---

# Development

## Prerequisites

- Go 1.25 or newer (`go.mod` says `go 1.25.0`).
- Docker, for the Spanner emulator and the integration tests.
- `gcloud`, only for `scripts/seed-emulator`.
- Terraform, only if you work on `terraform/`. The bootstrap root requires 1.5 or newer; the other roots set no constraint.

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `API_KEYS` | yes | none | Comma-separated API keys |
| `PORT` | no | 8080 | HTTP port |
| `SPANNER_PROJECT` | no | none | GCP project; Spanner mode needs all three Spanner variables |
| `SPANNER_INSTANCE` | no | none | Spanner instance name |
| `SPANNER_DATABASE` | no | none | Spanner database name |
| `SPANNER_EMULATOR_HOST` | no | none | Emulator address such as `localhost:9010`, read by the Spanner client library |

Without the three Spanner variables the server uses the built-in mock carriers and no cache.

## Make targets

```bash
make build             # CGO_ENABLED=0 go build -o bin/api ./cmd/api
make test              # go test -race -count=1 ./...
make vet               # go vet ./...
make check             # vet, then test
make docker            # docker build -t riskforge-api .
make up                # docker compose up --build -d
make down              # docker compose down
make test-integration  # start the emulator, run the Spanner repository tests (build tag: integration)
make clean             # remove bin/
```

`make check` does not run a formatter or a linter. `.golangci.yml` exists, but neither the Makefile nor CI calls it.

## Spanner emulator

`docker compose up --build` starts the emulator on ports 9010 and 9020, a one-shot `spanner-init` container that creates the `Carriers`, `AppetiteRules` and `Quotes` tables in `riskforge-dev/test-instance/test-db`, and the API on port 8080.

`./scripts/seed-emulator` inserts 3 carriers and 10 appetite rules with `gcloud spanner databases execute-sql`. See [architecture.md](architecture.md) for why the seeded carriers do not produce quotes today.
