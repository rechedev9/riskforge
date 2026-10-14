# riskforge 🔨: Parallel insurance quote gateway in Go, with Terraform for GCP

[![CI](https://img.shields.io/github/actions/workflow/status/rechedev9/riskforge/go.yml?branch=main&style=flat-square&label=ci)](https://github.com/rechedev9/riskforge/actions/workflows/go.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/rechedev9/riskforge?style=flat-square)](go.mod)

riskforge is a study project, not a production system. Luis Reche built it as a portfolio piece to learn the insurance domain: a quote gateway in Go that asks several carriers for a price in parallel, plus Terraform for Cloud Run, Spanner and Pub/Sub. The carriers are mock carriers with simulated latencies and random premiums, no real carrier is called, and it is not meant to take real traffic. The Terraform variable files hold placeholders, so the infrastructure is a template and not a running stack.

```console
$ curl -s -H "Authorization: Bearer dev-key-for-local-testing-only-32chars" \
    -d '{"request_id":"demo","coverage_lines":["auto","homeowners"],"timeout_ms":5000}' \
    localhost:8080/quotes | jq
{
  "request_id": "demo",
  "quotes": [
    { "carrier_id": "beta",  "premium_cents": 49470, "currency": "USD", "is_hedged": false, "latency_ms": 197 },
    { "carrier_id": "alpha", "premium_cents": 75760, "currency": "USD", "is_hedged": false, "latency_ms": 107 },
    { "carrier_id": "gamma", "premium_cents": 77366, "currency": "USD", "is_hedged": false, "latency_ms": 791 }
  ],
  "duration_ms": 791
}
```

Premiums are random, so your numbers will differ. Quotes come back sorted by premium, cheapest first.

## Quick start

You need Go 1.25 or newer. No database is required in this mode.

```bash
git clone https://github.com/rechedev9/riskforge && cd riskforge
API_KEYS="dev-key-for-local-testing-only-32chars" go run ./cmd/api
```

The server listens on port 8080 (`PORT` changes it). Send the request above from a second terminal.

To run with the Spanner emulator, use Docker:

```bash
make up    # docker compose up --build -d: emulator, schema init, API on :8080
make down
```

In Spanner mode quotes are cached by request id. With an empty `Carriers` table the API falls back to the mock carriers; `scripts/seed-emulator` data does not produce quotes yet, see [Status](#status).

## How a quote request fans out

1. The handler validates the body: at most 1 MiB, unknown fields rejected, `coverage_lines` from `auto`, `homeowners`, `umbrella`, `timeout_ms` from 100 to 30000 (default 5000).
2. With Spanner configured, the orchestrator first looks for a cached result for the same client and `request_id`. Concurrent identical requests share one fan-out (singleflight).
3. Carriers are selected by the coverage lines they declare. Carriers whose circuit breaker is open are skipped.
4. Each carrier is called in its own goroutine, through a per-carrier token bucket and then a circuit breaker: 5 consecutive failures open it for 30 seconds, then one probe decides whether it closes.
5. Hedging: every 5 ms a monitor compares each pending carrier with its moving average latency. Once a carrier has run longer than 1.5 times that average (and 10 observations exist), the same request goes to the other eligible carrier with the lowest average, if its rate limiter has a free token.
6. When the timeout hits, the answers collected so far are returned, de-duplicated by carrier, sorted by premium, and saved to Spanner if configured.

Details and code pointers are in [docs/architecture.md](docs/architecture.md).

## API

| Endpoint | Auth | Purpose |
|----------|------|---------|
| `POST /quotes` | Bearer API key | Body: `request_id`, `coverage_lines`, optional `timeout_ms` |
| `GET /healthz` | none | Returns `ok` |
| `GET /readyz` | none | Returns `ok`; the database check is not wired in yet |
| `GET /metrics` | none | Prometheus metrics: breaker state, latency, hedges, rate limit drops |

| Status | Meaning |
|--------|---------|
| 200 | Quotes returned |
| 400 | Invalid JSON or invalid fields |
| 401 | Missing or wrong API key |
| 422 | No eligible carrier, or none answered before the timeout |
| 429 | Too many failed authentication attempts from one IP |
| 500 | Unexpected error in the orchestrator |
| 503 | More than 100 requests in flight |

## Infrastructure

Eight modules under `terraform/modules`, wired by `terraform/environments/dev` and `prod`, plus a `terraform/bootstrap` root for the state bucket and GitHub Workload Identity.

| Module | What it is for |
|--------|----------------|
| `iam` | Service accounts for API, worker and Pub/Sub push, audit logging |
| `networking` | VPC, VPC connector, 5 firewall rules, Cloud Armor policy (not attached to a load balancer) |
| `spanner` | Instance and database with the `Carriers`, `AppetiteRules` and `Quotes` tables |
| `cloud-run` | Generic service module, used for the public API and the internal worker |
| `pubsub` | Events topic, dead-letter topic, push subscription to the worker |
| `storage` | Documents bucket and Artifact Registry repository |
| `monitoring` | Uptime check, alert policies for latency, errors, CPU and DLQ backlog |
| `kms` | Optional customer-managed key, off by default and unused by both environments |

Environment sizes and the gaps between the Terraform and the Go code are in [docs/infrastructure.md](docs/infrastructure.md).

## Tests and checks

```bash
make test              # go test -race -count=1 ./...
make check             # go vet, then the tests
make test-integration  # Spanner repositories against the emulator, needs Docker
```

The suite has unit tests per package, an HTTP stack test, orchestrator chaos and randomized tests, a goroutine leak check (goleak), a fuzz test for `POST /quotes`, and an adversarial black-box suite in `internal/antirez`. `.github/workflows/go.yml` builds, vets and runs the tests with the race detector, then the emulator tests, on pushes to `main` that touch Go files, nightly, and on manual dispatch. There is no formatter or linter step.

## Status

| Area | State |
|------|-------|
| Fan-out, hedging, circuit breaker, rate limiter | Implemented and tested with mock carriers |
| Appetite pre-filter | Tables, repository and seed data exist; the request path does not call them |
| Real carriers | Spanner carriers with `BaseURL` are registered as HTTP adapters; without Spanner, `cli.Run` still uses the mock carriers |
| Spanner mode | Quote cache works; carriers loaded from Spanner have no coverage lines, so requests return 422 |
| Worker and Pub/Sub | Terraform only; there is no worker code and the API does not publish |

## Documentation

- [docs/architecture.md](docs/architecture.md): packages, request flow, what is and is not wired
- [docs/development.md](docs/development.md): prerequisites, environment variables, Make targets, emulator
- [docs/infrastructure.md](docs/infrastructure.md): Terraform layout, environments, deploy workflow
- [docs/security.md](docs/security.md): API and Terraform controls, open items
- Reference notes: [Cloud Run](docs/cloud-run.md), [Cloud Spanner](docs/cloud-spanner.md), [Cloud Pub/Sub](docs/cloud-pubsub.md), [Terraform on GCP](docs/terraform-gcp.md), [OpenTelemetry for Go](docs/opentelemetry-go.md), [Anthropic Go SDK](docs/anthropic-go-sdk.md) (not used by the code)
- Agent session templates: [docs/handoff.md](docs/handoff.md), [docs/pickup.md](docs/pickup.md)
