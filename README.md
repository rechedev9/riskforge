<p align="center">
  <img src="assets/banner.png" alt="RISKFORGE — Carrier Quote Gateway" width="720">
</p>

<h1 align="center">riskforge</h1>

<p align="center">
  <strong>Multi-carrier insurance quote aggregation engine with appetite-based pre-filtering.<br>Go backend + GCP infrastructure (Terraform) + OWASP-hardened security.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go" alt="Go 1.25">
  <img src="https://img.shields.io/badge/Terraform-1.5+-7B42BC?style=for-the-badge&logo=terraform" alt="Terraform 1.5+">
  <img src="https://img.shields.io/badge/GCP-Cloud_Run_|_Spanner_|_Pub/Sub-4285F4?style=for-the-badge&logo=googlecloud" alt="GCP">
  <img src="https://img.shields.io/badge/Security-OWASP_Hardened-EF3B2D?style=for-the-badge" alt="OWASP">
</p>

<p align="center">
  <a href="#how-it-works">How It Works</a> ·
  <a href="#quick-start">Quick Start</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#api">API</a> ·
  <a href="#infrastructure">Infrastructure</a> ·
  <a href="#security">Security</a>
</p>

---

A carrier quote gateway that fans out quote requests to multiple insurance carriers in parallel, hedges slow responders adaptively, and returns sorted results through a single HTTP endpoint. Carriers are pre-filtered by appetite rules (state, line of business, premium range) stored in Spanner before any quote request is sent.

```
POST /quotes
  → appetite pre-filter (Spanner)
    → fan-out to eligible carriers (parallel)
      → hedge slow carriers (adaptive EMA p95)
        → deduplicate + sort by premium
          → JSON response
```

---

## How It Works

1. **Request arrives** at `POST /quotes` with coverage lines, state, and optional premium estimate
2. **Appetite filter** checks Spanner for which carriers accept that risk profile
3. **Fan-out** sends quote requests to all eligible carriers in parallel
4. **Hedging** monitors carrier latency — if a carrier exceeds its p95 threshold, a hedge request fires to a faster carrier
5. **Circuit breakers** trip after consecutive failures, excluding unreliable carriers
6. **Rate limiters** enforce per-carrier token buckets to prevent overload
7. **Results** are deduplicated, sorted by premium ascending, and cached in Spanner

---

## Quick Start

### Run locally (no dependencies)

```bash
API_KEYS="dev-key-for-local-testing-only-32chars" go run ./cmd/api/
```

```bash
curl -s -H "Authorization: Bearer dev-key-for-local-testing-only-32chars" \
  -d '{"request_id":"demo","coverage_lines":["auto","homeowners"],"timeout_ms":5000}' \
  localhost:8080/quotes | jq
```

```json
{
  "request_id": "demo",
  "quotes": [
    {"carrier_id": "alpha", "premium_cents": 89790, "currency": "USD", "latency_ms": 100},
    {"carrier_id": "beta",  "premium_cents": 61813, "currency": "USD", "latency_ms": 210},
    {"carrier_id": "gamma", "premium_cents": 72677, "currency": "USD", "latency_ms": 800}
  ],
  "duration_ms": 809
}
```

### Run with Spanner emulator (Docker)

```bash
docker compose up --build
```

This starts:
- **Spanner emulator** on ports 9010/9020
- **Schema init** (creates Carriers, AppetiteRules, Quotes tables)
- **API server** on port 8080

Seed demo data:
```bash
./scripts/seed-emulator  # 3 carriers, 10 appetite rules
```

### Build

```bash
make build    # binary at ./bin/api
make test     # tests with race detector
make check    # fmt + lint + vet + test
```

---

## Architecture

```
cmd/api/main.go            → 14 lines, delegates to internal/cli
internal/
├── cli/                   → Server wiring, Spanner connection, signal handling
├── domain/                → Carrier, Quote, AppetiteRule, Money, errors (zero deps)
├── ports/                 → Interfaces: CarrierPort, OrchestratorPort, repositories
├── adapter/               → Mock carriers, HTTP carrier, generic adapter registry
│   └── spanner/           → QuoteRepo, CarrierRepo, AppetiteRepo (Spanner client)
├── orchestrator/          → Fan-out, singleflight dedup, adaptive hedging (EMA p95)
├── circuitbreaker/        → 3-state machine (Closed→Open→HalfOpen), atomic ops
├── ratelimiter/           → Token bucket via x/time/rate
├── handler/               → HTTP handler (POST /quotes, /healthz, /readyz, /metrics)
├── middleware/             → API key auth, security headers, concurrency limiter, audit
├── metrics/               → Prometheus recorder (gauges, histograms, counters)
├── cleanup/               → Background expired quote cleanup ticker
└── testutil/              → Test helpers (NoopRecorder)
```

### Request flow

```
HTTP Request → AuditLog → SecurityHeaders → RequireAPIKey → LimitConcurrency
  → Handler.handlePostQuotes
    → validate request
    → Orchestrator.GetQuotes
      ├── cache lookup (Spanner, optional)
      ├── singleflight dedup (same request_id)
      └── fanOut
          ├── filterEligibleCarriers (appetite + capabilities + circuit breaker)
          ├── launch primary goroutines (errgroup)
          │   └── RateLimiter → CircuitBreaker → AdapterFunc
          ├── hedgeMonitor (5ms poll, fires when elapsed > p95 × multiplier)
          ├── collect + deduplicate results
          ├── sort by premium ascending
          └── save to Spanner cache
    → JSON response
```

---

## API

### POST /quotes

```bash
curl -H "Authorization: Bearer $API_KEY" \
  -d '{
    "request_id": "q-123",
    "coverage_lines": ["auto", "homeowners"],
    "timeout_ms": 5000
  }' \
  http://localhost:8080/quotes
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `request_id` | string | yes | Unique ID (≤256 chars, ASCII printable) |
| `coverage_lines` | string[] | yes | Lines of business to price |
| `timeout_ms` | int | no | Max wait time (100-30000ms, default 5000) |

**Response** (200):
```json
{
  "request_id": "q-123",
  "quotes": [
    {
      "carrier_id": "beta",
      "premium_cents": 61813,
      "currency": "USD",
      "is_hedged": false,
      "latency_ms": 210
    }
  ],
  "duration_ms": 219
}
```

| Status | Meaning |
|--------|---------|
| 200 | Quotes returned (sorted by premium) |
| 400 | Invalid request |
| 401 | Missing/invalid API key |
| 422 | No eligible carriers |
| 429 | Auth failure rate limit |
| 504 | Timeout before any carrier responded |

### Other endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/healthz` | GET | No | Always returns `ok` |
| `/readyz` | GET | No | DB health check |
| `/metrics` | GET | No | Prometheus metrics |

---

## Infrastructure

8 Terraform modules provision the full GCP stack:

| Module | Resources |
|--------|-----------|
| **iam** | 3 SAs, project IAM, audit logging |
| **networking** | VPC, subnet, VPC connector, 5 firewall rules, Cloud Armor |
| **spanner** | Instance, database (Carriers + AppetiteRules + Quotes), IAM |
| **cloud-run** | Generic module (API public + Worker internal) |
| **pubsub** | Topic, DLQ, push subscription with OIDC |
| **storage** | GCS bucket, Artifact Registry, IAM |
| **monitoring** | API + Worker alerts (latency, errors, CPU), DLQ backlog, uptime |
| **kms** | CMEK opt-in scaffold (disabled by default) |

### Environments

| | Dev | Prod |
|---|---|---|
| Instances | 0-5 (scale-to-zero) | 2-20 |
| Spanner | 100 PU | 300 PU |
| Alerts | Disabled | Enabled |
| Deletion protection | No | Yes |

---

## Security

OWASP-hardened infrastructure (15 findings fixed):

- **Audit logging** — DATA_READ, DATA_WRITE, ADMIN_READ for all GCP services
- **Firewall rules** — deny-all default + explicit allowlists (health checks, internal, GCP APIs)
- **IAM scoping** — secretmanager.secretAccessor removed from project scope; per-secret bindings
- **Terraform SA** — 11 explicit roles, no roles/editor
- **CI/CD** — plan output masked in PR comments, continue-on-error removed
- **State bucket** — public_access_prevention enforced
- **Artifact Registry** — reader IAM scoped to runtime SAs, vulnerability scanning enabled
- **Cloud Armor** — rate limiting policy (1000 req/min per IP)
- **Provider pinning** — all modules pinned to ~> 6.14
- **API auth** — constant-time key comparison, per-IP failure rate limiting with TTL eviction
- **Security headers** — HSTS, X-Frame-Options, CSP, no-store cache

---

## Development

### Prerequisites

- Go 1.25+
- Docker (for Spanner emulator)
- Terraform 1.5+ (for infrastructure)

### Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `API_KEYS` | yes | — | Comma-separated API keys (≥32 chars each) |
| `PORT` | no | 8080 | HTTP server port |
| `SPANNER_PROJECT` | no | — | GCP project (enables Spanner mode) |
| `SPANNER_INSTANCE` | no | — | Spanner instance name |
| `SPANNER_DATABASE` | no | — | Spanner database name |
| `SPANNER_EMULATOR_HOST` | no | — | Emulator address (e.g., localhost:9010) |

### Make targets

```bash
make build     # Build binary
make test      # Run tests with race detector
make vet       # Run go vet
make check     # fmt + vet + test
make docker    # Build Docker image
make up        # docker compose up --build
make down      # docker compose down
```

---

## Built With

- **[Go](https://go.dev/)** — API server, hexagonal architecture, zero web frameworks
- **[Cloud Spanner](https://cloud.google.com/spanner)** — Carrier data, appetite rules, quote cache
- **[Terraform](https://www.terraform.io/)** — 8 GCP infrastructure modules
- **[Prometheus](https://prometheus.io/)** — Metrics (latency, errors, circuit breaker state)
- **[GitHub Actions](https://github.com/features/actions)** — CI/CD with Workload Identity Federation

## License

MIT
