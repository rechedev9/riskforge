# Technical Design: Merge Carrier Gateway

**Change**: merge-carrier-gateway
**Date**: 2026-03-22T20:00:00Z
**Status**: draft
**Depends On**: propose.md, spec.md

---

## Technical Approach

Merge carrier-gateway's hexagonal architecture into riskforge, preserving layer boundaries. The domain and port layers copy verbatim (import rename only). The adapter layer replaces Postgres with Spanner and adds appetite/carrier repositories. The orchestrator gains an appetite pre-filter stage backed by an in-memory cache. The handler extends its JSON schema. A new `cmd/api/main.go` wires everything for Cloud Run.

All code lives under `internal/` per Go convention. No public API packages are exported.

---

## Architecture Decisions

| # | Decision | Choice | Alternatives Considered | Rationale |
|---|----------|--------|------------------------|-----------|
| 1 | CarrierConfig storage | JSON blob column `Config JSON` on Carriers table | (a) New columns per field, (b) YAML config file, (c) env vars | JSON avoids DDL changes per new config field; Spanner JSON type supports partial indexing; config evolves independently of schema |
| 2 | Appetite caching | In-memory `[]AppetiteRule` with `sync.RWMutex`, 60s background refresh | (a) Per-request Spanner query, (b) Redis/Memorystore, (c) Spanner read-only transaction cache | Per-request adds ~5ms latency to hot path; Redis adds infrastructure; Spanner RO txn still hits network. In-memory is zero-latency, 60s staleness is acceptable for rules that change hourly at most |
| 3 | CoverageLine type | `type CoverageLine = string` (type alias, not defined type) | (a) Keep enum with mapping layer, (b) `type CoverageLine string` (defined type) | Type alias means no type conversion needed when comparing with `LineOfBusiness` strings from Spanner. Defined type would require explicit conversion at every comparison. Constants retained for convenience |
| 4 | Spanner adapter layout | `internal/adapter/spanner/` sub-package with separate files per repo | (a) Flat files in `internal/adapter/`, (b) `internal/repository/spanner/` | Sub-package isolates Spanner import from adapter registry; flat files would pollute adapter package with GCP deps |
| 5 | Orchestrator appetite dependency | Optional `AppetiteRepository` field; nil skips appetite filtering | (a) Required dependency, (b) Feature flag | Optional preserves backward compat for tests; nil-check is idiomatic Go |
| 6 | Handler health check | `HealthChecker` interface replacing `*sql.DB` | (a) `*spanner.Client` directly, (b) Generic `io.Closer` | Interface allows mocking in tests; decouples handler from Spanner import |
| 7 | Quotes table key | `QuoteId STRING(36)` PK, secondary index on `RequestID` | (a) Composite PK `(RequestID, CarrierId)`, (b) Interleaved under Carriers | Simple PK avoids hot-spotting on RequestID prefix; secondary index serves cache lookups; not interleaved because quotes are cross-carrier |

---

## Data Flow

### Quote Request Pipeline (after merge)

```
POST /quotes
  { request_id, coverage_lines, state?, class_code?, estimated_premium_cents?, timeout_ms? }
    |
    v
handler.handlePostQuotes()
    |-- validate request (relaxed coverage_lines, optional appetite fields)
    |-- build domain.QuoteRequest (with State, ClassCode, EstimatedPremium)
    |-- orch.GetQuotes(ctx, req)
          |
          |-- cache check (repo.FindByRequestID)
          |-- singleflight dedup
          |-- fanOut(ctx, req)
                |
                |-- filterEligibleCarriers(req)  <-- MODIFIED (see below)
                |-- launch carrier goroutines (errgroup)
                |-- hedgeMonitor (concurrent)
                |-- collect, dedup, sort by premium
                |-- repo.Save (async)
          |
    |-- encode response
```

### filterEligibleCarriers: BEFORE vs AFTER

**BEFORE** (carrier-gateway):
```go
func (o *Orchestrator) filterEligibleCarriers(lines []CoverageLine) []Carrier {
    requested := map[CoverageLine]bool{...}
    for _, c := range o.carriers {
        // 1. Skip Open circuit breakers
        if breaker.State() == CBStateOpen { continue }
        // 2. Capability match: carrier.Capabilities intersects requested lines
        for _, cap := range c.Capabilities {
            if requested[cap] { eligible = append(eligible, c); break }
        }
    }
    return eligible
}
```

**AFTER** (riskforge):
```go
func (o *Orchestrator) filterEligibleCarriers(req QuoteRequest) []Carrier {
    // Stage 1: Appetite rule match (from cache)
    //   - If appetite fields present (State non-empty), query cache for rules
    //     matching (State, LineOfBusiness, ClassCode, premium range)
    //   - Build set of eligible CarrierIDs from matched rules
    //   - If no appetite fields or cache empty, skip (all carriers pass)
    appetiteEligible := o.matchAppetiteRules(req)

    // Stage 2: Capability match
    //   - If appetite filter produced results, use LineOfBusiness from matched
    //     rules as the capability set (replaces req.CoverageLines)
    //   - If no appetite filter, use req.CoverageLines directly (backward compat)
    //   - Filter: carrier.Capabilities intersects coverage set

    // Stage 3: Circuit breaker state
    //   - Skip carriers with Open breakers
    //   - HalfOpen carriers pass (probe call)

    for _, c := range o.carriers {
        // Stage 1: appetite gate
        if appetiteEligible != nil {
            if !appetiteEligible[c.ID] { continue }
        }

        // Stage 3: circuit breaker gate
        if breaker, ok := o.breakers[c.ID]; ok && breaker.State() == CBStateOpen {
            continue
        }

        // Stage 2: capability match
        for _, cap := range c.Capabilities {
            if requested[cap] {
                eligible = append(eligible, c)
                break
            }
        }
    }
    return eligible
}
```

### matchAppetiteRules (new method on Orchestrator)

```go
func (o *Orchestrator) matchAppetiteRules(req QuoteRequest) map[string]bool {
    if req.State == "" {
        return nil // no appetite filtering; all carriers pass
    }

    o.cacheMu.RLock()
    rules := o.cachedRules
    o.cacheMu.RUnlock()

    if len(rules) == 0 {
        return nil // cache empty; all carriers pass (graceful degradation)
    }

    eligible := make(map[string]bool)
    for _, rule := range rules {
        if !rule.IsActive { continue }
        if rule.State != req.State { continue }

        // Match LineOfBusiness against requested coverage lines
        lobMatch := false
        for _, line := range req.CoverageLines {
            if rule.LineOfBusiness == line {
                lobMatch = true
                break
            }
        }
        if !lobMatch { continue }

        // Optional ClassCode match
        if req.ClassCode != "" && rule.ClassCode != "" && rule.ClassCode != req.ClassCode {
            continue
        }

        // Optional premium range match
        if req.EstimatedPremium.Amount > 0 {
            if rule.MinPremium > 0 && req.EstimatedPremium.Amount < rule.MinPremium { continue }
            if rule.MaxPremium > 0 && req.EstimatedPremium.Amount > rule.MaxPremium { continue }
        }

        eligible[rule.CarrierID] = true
    }
    return eligible
}
```

### Appetite Cache Refresh

```
Orchestrator.StartCacheRefresh(ctx)
    |-- goroutine loop:
          every 60s (ticker):
            appetiteRepo.FindAll(ctx)
              -> Spanner SQL: SELECT * FROM AppetiteRules WHERE IsActive = true
            cacheMu.Lock()
            cachedRules = results
            cacheMu.Unlock()
          on ctx.Done():
            return
```

---

## Key Types

### internal/domain/appetite.go (new)

```go
package domain

// AppetiteRule represents a carrier's willingness to write business for a
// specific state, line of business, class code, and premium range.
type AppetiteRule struct {
    RuleID              string
    CarrierID           string
    State               string  // 2-char US state code
    LineOfBusiness      string  // matches CoverageLine values
    ClassCode           string  // optional; empty = any
    MinPremium          int64   // cents; 0 = no minimum
    MaxPremium          int64   // cents; 0 = no maximum
    IsActive            bool
    EligibilityCriteria map[string]any // parsed from JSON column
}

// RiskClassification is the input for appetite matching.
type RiskClassification struct {
    State            string
    LineOfBusiness   string
    ClassCode        string
    EstimatedPremium Money
}

// MatchResult is the output of an appetite rule match.
type MatchResult struct {
    CarrierID string
    RuleID    string
    MatchScore float64 // 0.0-1.0; reserved for future ranking
}
```

### internal/domain/quote.go (modified)

```go
// CoverageLine is a free string identifying a line of business.
// Type alias (not defined type) for zero-friction comparison with Spanner strings.
type CoverageLine = string

// Convenience constants retained for backward compatibility.
const (
    CoverageLineAuto       CoverageLine = "auto"
    CoverageLineHomeowners CoverageLine = "homeowners"
    CoverageLineUmbrella   CoverageLine = "umbrella"
)

// QuoteRequest adds appetite-filtering fields.
type QuoteRequest struct {
    RequestID        string
    ClientID         string
    CoverageLines    []CoverageLine
    Timeout          time.Duration
    // Appetite fields (all optional; zero values skip appetite filtering)
    State            string  // 2-char US state code
    ClassCode        string  // insurance class code
    EstimatedPremium Money   // expected premium for pre-filtering
}
```

### internal/domain/carrier.go (modified)

```go
// CarrierConfig adds JSON tags for Spanner JSON column deserialization.
type CarrierConfig struct {
    TimeoutHint           time.Duration  `json:"timeout_hint"`
    OpenTimeout           time.Duration  `json:"open_timeout"`
    FailureThreshold      int            `json:"failure_threshold"`
    SuccessThreshold      int            `json:"success_threshold"`
    HedgeMultiplier       float64        `json:"hedge_multiplier"`
    EMAAlpha              float64        `json:"ema_alpha"`
    EMAWindowSize         int            `json:"ema_window_size"`
    EMAWarmupObservations int            `json:"ema_warmup_observations"`
    RateLimit             RateLimitConfig `json:"rate_limit"`
    Priority              int            `json:"priority"`
}

type RateLimitConfig struct {
    TokensPerSecond float64 `json:"tokens_per_second"`
    Burst           int     `json:"burst"`
}
```

### internal/ports/appetite_port.go (new)

```go
package ports

import (
    "context"
    "github.com/rechedev9/riskforge/internal/domain"
)

// AppetiteRepository is the outbound port for querying appetite rules.
type AppetiteRepository interface {
    // FindMatchingRules returns active rules matching the classification criteria.
    FindMatchingRules(ctx context.Context, rc domain.RiskClassification) ([]domain.AppetiteRule, error)

    // FindAll returns all active appetite rules (used for cache population).
    FindAll(ctx context.Context) ([]domain.AppetiteRule, error)
}
```

---

## Spanner Adapters

### internal/adapter/spanner/client.go

```go
package spanner

import (
    "context"
    "fmt"
    "cloud.google.com/go/spanner"
)

// Client wraps a Spanner client with connection metadata.
type Client struct {
    sc       *spanner.Client
    database string // "projects/P/instances/I/databases/D"
}

func NewClient(ctx context.Context, projectID, instanceID, databaseID string) (*Client, error) {
    db := fmt.Sprintf("projects/%s/instances/%s/databases/%s", projectID, instanceID, databaseID)
    sc, err := spanner.NewClient(ctx, db)
    if err != nil {
        return nil, fmt.Errorf("spanner.NewClient: %w", err)
    }
    return &Client{sc: sc, database: db}, nil
}

func (c *Client) Ping(ctx context.Context) error {
    iter := c.sc.Single().Query(ctx, spanner.NewStatement("SELECT 1"))
    defer iter.Stop()
    _, err := iter.Next()
    return err
}

func (c *Client) Close() { c.sc.Close() }
```

### internal/adapter/spanner/quote_repo.go

Key patterns:

```go
// Save uses InsertOrUpdate mutations (idempotent upsert).
func (r *QuoteRepo) Save(ctx context.Context, requestID string, results []domain.QuoteResult) error {
    var ms []*spanner.Mutation
    for _, res := range results {
        ms = append(ms, spanner.InsertOrUpdate("Quotes",
            []string{"QuoteId", "RequestID", "CarrierId", "PremiumCents", "Currency",
                      "CarrierRef", "ExpiresAt", "IsHedged", "LatencyMs", "CreatedAt"},
            []interface{}{uuid(), requestID, res.CarrierID, res.Premium.Amount,
                          res.Premium.Currency, res.CarrierRef, res.ExpiresAt,
                          res.IsHedged, res.Latency.Milliseconds(),
                          spanner.CommitTimestamp},
        ))
    }
    _, err := r.client.sc.Apply(ctx, ms)
    return err
}

// FindByRequestID reads non-expired quotes.
func (r *QuoteRepo) FindByRequestID(ctx context.Context, requestID string) ([]domain.QuoteResult, bool, error) {
    stmt := spanner.Statement{
        SQL: `SELECT CarrierId, PremiumCents, Currency, CarrierRef, ExpiresAt, IsHedged, LatencyMs
              FROM Quotes
              WHERE RequestID = @requestID AND ExpiresAt > CURRENT_TIMESTAMP()`,
        Params: map[string]interface{}{"requestID": requestID},
    }
    // ... iterate rows, build []QuoteResult
}

// DeleteExpired removes stale quotes.
func (r *QuoteRepo) DeleteExpired(ctx context.Context) (int64, error) {
    // Spanner doesn't support DELETE ... RETURNING count natively.
    // Use a read-then-delete pattern or DML.
    stmt := spanner.Statement{
        SQL: "DELETE FROM Quotes WHERE ExpiresAt <= CURRENT_TIMESTAMP()",
    }
    count, err := r.client.sc.PartitionedUpdate(ctx, stmt)
    return count, err
}
```

### internal/adapter/spanner/appetite_repo.go

```go
func (r *AppetiteRepo) FindAll(ctx context.Context) ([]domain.AppetiteRule, error) {
    stmt := spanner.Statement{
        SQL: `SELECT RuleId, CarrierId, State, LineOfBusiness, ClassCode,
                     MinPremium, MaxPremium, IsActive, EligibilityCriteria
              FROM AppetiteRules
              WHERE IsActive = true`,
    }
    // ... iterate, deserialize EligibilityCriteria JSON
}

func (r *AppetiteRepo) FindMatchingRules(ctx context.Context, rc domain.RiskClassification) ([]domain.AppetiteRule, error) {
    stmt := spanner.Statement{
        SQL: `SELECT RuleId, CarrierId, State, LineOfBusiness, ClassCode,
                     MinPremium, MaxPremium, IsActive, EligibilityCriteria
              FROM AppetiteRules
              WHERE IsActive = true
                AND State = @state
                AND LineOfBusiness = @lob
                AND (ClassCode IS NULL OR ClassCode = @classCode)
                AND (MinPremium IS NULL OR MinPremium <= @premium)
                AND (MaxPremium IS NULL OR MaxPremium >= @premium)`,
        Params: map[string]interface{}{
            "state":     rc.State,
            "lob":       rc.LineOfBusiness,
            "classCode": rc.ClassCode,
            "premium":   float64(rc.EstimatedPremium.Amount) / 100.0,
        },
    }
    // ...
}
```

### internal/adapter/spanner/carrier_repo.go

```go
func (r *CarrierRepo) LoadActive(ctx context.Context) ([]domain.Carrier, error) {
    stmt := spanner.Statement{
        SQL: `SELECT CarrierId, Name, Code, Config
              FROM Carriers
              WHERE IsActive = true`,
    }
    // For each row:
    //   1. Read CarrierId, Name, Code
    //   2. Read Config as spanner.NullJSON
    //   3. json.Unmarshal Config into domain.CarrierConfig
    //   4. Query AppetiteRules for this carrier to build Capabilities
    //      (distinct LineOfBusiness values = carrier's capability set)
}
```

---

## Handler Changes

### Request schema extension

```go
type quoteRequest struct {
    RequestID            string   `json:"request_id"`
    CoverageLines        []string `json:"coverage_lines"`
    TimeoutMs            int      `json:"timeout_ms,omitempty"`
    State                string   `json:"state,omitempty"`
    ClassCode            string   `json:"class_code,omitempty"`
    EstimatedPremiumCents int64   `json:"estimated_premium_cents,omitempty"`
}
```

### Validation changes

```go
func validateQuoteRequest(req *quoteRequest) error {
    // ... existing request_id validation unchanged ...

    if len(req.CoverageLines) == 0 {
        return fmt.Errorf("%w: coverage_lines must contain at least one entry", domain.ErrInvalidRequest)
    }
    for _, line := range req.CoverageLines {
        if line == "" || len(line) > 100 {
            return fmt.Errorf("%w: coverage_line must be 1-100 characters", domain.ErrInvalidRequest)
        }
    }

    // New: appetite field validation
    if req.State != "" && len(req.State) != 2 {
        return fmt.Errorf("%w: state must be a 2-character code", domain.ErrInvalidRequest)
    }
    if req.EstimatedPremiumCents < 0 {
        return fmt.Errorf("%w: estimated_premium_cents must be non-negative", domain.ErrInvalidRequest)
    }

    // ... timeout validation unchanged ...
}
```

### HealthChecker interface

```go
// HealthChecker abstracts the readiness probe dependency.
type HealthChecker interface {
    Ping(ctx context.Context) error
}

type Handler struct {
    orch     ports.OrchestratorPort
    metrics  ports.MetricsRecorder
    gatherer prometheus.Gatherer
    log      *slog.Logger
    health   HealthChecker // replaces *sql.DB
}
```

---

## Spanner DDL Changes

### terraform/modules/spanner/main.tf

Add `Config JSON` column to Carriers table:

```hcl
CREATE TABLE Carriers (
    CarrierId STRING(36) NOT NULL DEFAULT (GENERATE_UUID()),
    Name STRING(255) NOT NULL,
    Code STRING(50) NOT NULL,
    IsActive BOOL NOT NULL DEFAULT (true),
    Config JSON,
    CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (CarrierId)
```

Add Quotes table:

```hcl
CREATE TABLE Quotes (
    QuoteId STRING(36) NOT NULL DEFAULT (GENERATE_UUID()),
    RequestID STRING(256) NOT NULL,
    CarrierId STRING(36) NOT NULL,
    PremiumCents INT64 NOT NULL,
    Currency STRING(3) NOT NULL,
    CarrierRef STRING(256),
    ExpiresAt TIMESTAMP NOT NULL,
    IsHedged BOOL NOT NULL DEFAULT (false),
    LatencyMs INT64,
    CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (QuoteId)
```

Add secondary index:

```hcl
CREATE INDEX QuotesByRequestID ON Quotes(RequestID)
```

---

## cmd/api/main.go Wiring

```go
func main() {
    // 1. Logger (JSON, configurable level)
    // 2. Spanner client from env vars (SPANNER_PROJECT, SPANNER_INSTANCE, SPANNER_DATABASE)
    // 3. Load carriers from Spanner: carrierRepo.LoadActive(ctx)
    // 4. Build per-carrier infrastructure from Carrier.Config:
    //    - breakers[id] = circuitbreaker.New(id, Config{...}, rec)
    //    - limiters[id] = ratelimiter.New(id, Config.RateLimit, rec)
    //    - trackers[id] = orchestrator.NewEMATracker(id, Config.TimeoutHint, Config, rec)
    // 5. Adapter registry: register mock + HTTP carriers
    //    - Mock carriers for dev (ENABLE_MOCK_CARRIERS env var)
    //    - HTTP carriers from Carrier.Config (future)
    // 6. Repositories: quoteRepo, appetiteRepo
    // 7. Orchestrator: New(carriers, registry, breakers, limiters, trackers, rec, cfg, log, quoteRepo, appetiteRepo)
    // 8. Start appetite cache refresh
    // 9. Cleanup ticker for expired quotes
    // 10. Handler: New(orch, rec, reg, log, spannerClient) -- spannerClient implements HealthChecker
    // 11. Middleware stack: AuditLog -> SecurityHeaders -> RequireAPIKey -> LimitConcurrency
    // 12. HTTP server with Cloud Run timeouts
    // 13. Signal handler (SIGTERM) -> graceful shutdown
    // 14. Close Spanner client
}
```

---

## Dockerfile

```dockerfile
# Build stage
FROM golang:1.25 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /api ./cmd/api/

# Runtime stage
FROM gcr.io/distroless/static-debian12
COPY --from=builder /api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
```

---

## CI Workflow

`.github/workflows/go.yml`:

```yaml
name: Go
on:
  push:
    branches: [main]
    paths: ['**.go', 'go.mod', 'go.sum']
  pull_request:
    branches: [main]
    paths: ['**.go', 'go.mod', 'go.sum']

jobs:
  build-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.0'
          cache: true
      - run: go build ./...
      - run: go test ./...
      - run: go vet ./...
```

---

## File Inventory

| Target Path | Source | Action |
|---|---|---|
| `internal/domain/carrier.go` | `/tmp/carrier-gateway/internal/domain/carrier.go` | Copy + rename imports + add JSON tags |
| `internal/domain/quote.go` | `/tmp/carrier-gateway/internal/domain/quote.go` | Copy + modify CoverageLine + extend QuoteRequest |
| `internal/domain/errors.go` | `/tmp/carrier-gateway/internal/domain/errors.go` | Copy + rename imports |
| `internal/domain/appetite.go` | N/A | New |
| `internal/ports/quote_port.go` | `/tmp/carrier-gateway/internal/ports/quote_port.go` | Copy + rename imports |
| `internal/ports/repository_port.go` | `/tmp/carrier-gateway/internal/ports/repository_port.go` | Copy + rename imports |
| `internal/ports/metrics_port.go` | `/tmp/carrier-gateway/internal/ports/metrics_port.go` | Copy + rename imports |
| `internal/ports/appetite_port.go` | N/A | New |
| `internal/adapter/adapter.go` | `/tmp/carrier-gateway/internal/adapter/adapter.go` | Copy + rename imports |
| `internal/adapter/adapter_test.go` | `/tmp/carrier-gateway/internal/adapter/adapter_test.go` | Copy + rename imports |
| `internal/adapter/mock_carrier.go` | `/tmp/carrier-gateway/internal/adapter/mock_carrier.go` | Copy + rename imports |
| `internal/adapter/mock_carrier_test.go` | `/tmp/carrier-gateway/internal/adapter/mock_carrier_test.go` | Copy + rename imports |
| `internal/adapter/http_carrier.go` | `/tmp/carrier-gateway/internal/adapter/http_carrier.go` | Copy + rename imports |
| `internal/adapter/http_carrier_test.go` | `/tmp/carrier-gateway/internal/adapter/http_carrier_test.go` | Copy + rename imports |
| `internal/adapter/delta_carrier.go` | `/tmp/carrier-gateway/internal/adapter/delta_carrier.go` | Copy + rename imports |
| `internal/adapter/spanner/client.go` | N/A | New |
| `internal/adapter/spanner/quote_repo.go` | N/A | New |
| `internal/adapter/spanner/appetite_repo.go` | N/A | New |
| `internal/adapter/spanner/carrier_repo.go` | N/A | New |
| `internal/circuitbreaker/breaker.go` | `/tmp/carrier-gateway/internal/circuitbreaker/breaker.go` | Copy + rename imports |
| `internal/circuitbreaker/breaker_test.go` | `/tmp/carrier-gateway/internal/circuitbreaker/breaker_test.go` | Copy + rename imports |
| `internal/ratelimiter/limiter.go` | `/tmp/carrier-gateway/internal/ratelimiter/limiter.go` | Copy + rename imports |
| `internal/ratelimiter/limiter_test.go` | `/tmp/carrier-gateway/internal/ratelimiter/limiter_test.go` | Copy + rename imports |
| `internal/metrics/prometheus.go` | `/tmp/carrier-gateway/internal/metrics/prometheus.go` | Copy + rename imports |
| `internal/metrics/prometheus_test.go` | `/tmp/carrier-gateway/internal/metrics/prometheus_test.go` | Copy + rename imports |
| `internal/middleware/auth.go` | `/tmp/carrier-gateway/internal/middleware/auth.go` | Copy (no internal imports) |
| `internal/middleware/audit.go` | `/tmp/carrier-gateway/internal/middleware/audit.go` | Copy (no internal imports) |
| `internal/middleware/concurrency.go` | `/tmp/carrier-gateway/internal/middleware/concurrency.go` | Copy (no internal imports) |
| `internal/middleware/security.go` | `/tmp/carrier-gateway/internal/middleware/security.go` | Copy (no internal imports) |
| `internal/middleware/middleware_test.go` | `/tmp/carrier-gateway/internal/middleware/middleware_test.go` | Copy (no internal imports) |
| `internal/orchestrator/orchestrator.go` | `/tmp/carrier-gateway/internal/orchestrator/orchestrator.go` | Copy + modify |
| `internal/orchestrator/orchestrator_test.go` | `/tmp/carrier-gateway/internal/orchestrator/orchestrator_test.go` | Copy + modify |
| `internal/orchestrator/hedging.go` | `/tmp/carrier-gateway/internal/orchestrator/hedging.go` | Copy + rename imports |
| `internal/orchestrator/hedging_test.go` | `/tmp/carrier-gateway/internal/orchestrator/hedging_test.go` | Copy + rename imports |
| `internal/handler/http.go` | `/tmp/carrier-gateway/internal/handler/http.go` | Copy + modify |
| `internal/handler/http_test.go` | `/tmp/carrier-gateway/internal/handler/http_test.go` | Copy + modify |
| `internal/cleanup/cleanup.go` | `/tmp/carrier-gateway/internal/cleanup/cleanup.go` | Copy + rename imports |
| `internal/cleanup/ticker_test.go` | `/tmp/carrier-gateway/internal/cleanup/ticker_test.go` | Copy + rename imports |
| `internal/testutil/recorder.go` | `/tmp/carrier-gateway/internal/testutil/recorder.go` | Copy + rename imports |
| `cmd/api/main.go` | N/A (rewrite of `/tmp/carrier-gateway/cmd/carrier-gateway/main.go`) | New |
| `Dockerfile` | N/A | New |
| `go.mod` | Existing | Modify (Go 1.25, add deps) |
| `terraform/modules/spanner/main.tf` | Existing | Modify (Config column, Quotes table) |
| `.github/workflows/go.yml` | N/A | New |
