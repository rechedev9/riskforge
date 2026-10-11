---
summary: "Package layout, request flow and what the orchestrator really does"
read_when: [understanding the Go code, changing the orchestrator, hedging, circuit breaker, wiring in cli]
---

# Architecture

riskforge is a study project. This page describes the code as it is, including the parts that are described by types and tables but not wired into the request path.

## Package layout

```
cmd/api/main.go            14 lines, delegates to internal/cli
internal/
  cli/                     server wiring, Spanner connection, signal handling
  domain/                  Carrier, Quote, AppetiteRule, Money, errors (stdlib only)
  ports/                   interfaces: OrchestratorPort, repositories, MetricsRecorder
  adapter/                 mock carriers, HTTP carrier, Delta example, generic adapter registry
    spanner/               QuoteRepo, CarrierRepo, AppetiteRepo (Spanner client)
  orchestrator/            fan-out, singleflight, adaptive hedging
  circuitbreaker/          3-state machine (closed, open, half-open) on atomics
  ratelimiter/             token bucket on golang.org/x/time/rate
  handler/                 HTTP handlers: POST /quotes, /healthz, /readyz, /metrics
  middleware/              API key auth, security headers, concurrency limit, audit log
  metrics/                 Prometheus recorder
  cleanup/                 background ticker that deletes expired quotes
  testutil/                test helpers (NoopRecorder)
```

## Request flow

```
HTTP request -> AuditLog -> SecurityHeaders -> RequireAPIKey -> LimitConcurrency
  -> handlePostQuotes: validate the body
    -> Orchestrator.GetQuotes
       - cache lookup in Spanner (only when Spanner is configured)
       - singleflight on client id + request_id
       - fanOut
           - filterEligibleCarriers (coverage line capability, circuit not open)
           - one goroutine per carrier (errgroup): rate limiter, circuit breaker, adapter
           - hedgeMonitor polls every 5 ms
           - collect, drop duplicate carrier ids, sort by premium ascending
           - save to Spanner (3 second timeout, failure only logged)
    -> JSON response
```

## Fan-out details

- The per-request timeout comes from `timeout_ms` (100 to 30000, default 5000). When it expires, the carriers that already answered are returned.
- The rate limiter blocks a primary call until a token is available or the request deadline passes. A hedge call only takes a token if one is free.
- The circuit breaker opens after 5 consecutive failures and stays open for 30 seconds. After that it lets one probe through (half-open) and closes after 2 consecutive successes. A failed probe opens it again. The `cli` package sets these values for every carrier that does not carry its own.
- Hedging uses an exponential moving average of the carrier's observed latency. The code and the metric call it p95, but the update is `alpha * latency + (1 - alpha) * average`, so it is a smoothed average and not a percentile. Alpha is `2 / (19 + 1)`, the average starts at twice the carrier's `TimeoutHint`, and no hedge fires until 10 observations exist. The hedge threshold is the average times 1.5.
- A hedge sends the same request to the other eligible carrier with the lowest average, skipping carriers whose circuit is open. Its result is marked `is_hedged`. Results are de-duplicated by carrier id, so the first answer for a carrier id wins.
- With the built-in mock carriers (no failures, jitter of 10 ms) a hedge rarely fires, because latencies stay below the 1.5x threshold. Tests cover the average, the warm-up and the threshold, the breaker transitions, and chaos cases (all carriers fail, carriers time out, rapid breaker transitions), but no test asserts that a hedge actually fires.

## Described but not wired

- Appetite pre-filtering. `AppetiteRepo`, the `AppetiteRules` table and the seed script exist, and `QuoteRequest` has `State`, `ClassCode` and `EstimatedPremium` fields, but nothing calls the repository from the request path. The handler does not accept those fields (unknown JSON fields are rejected with 400). Carriers are chosen only by the coverage lines they declare.
- Carriers from Spanner. `CarrierRepo.ListActive` fills id, name, code and config but never `Capabilities`, so the eligibility filter finds no carrier and every request returns 422 in Spanner mode. The seed script also stores config values such as `"TimeoutHint":"100ms"` as strings, which do not decode into `time.Duration`, so the defaults are used.
- Real carriers. `HTTPCarrier` (retries with exponential backoff, per-attempt timeout) and a Delta example adapter exist and have tests, but `cli.Run` registers a mock carrier for every carrier id.
- `/readyz`. The handler can ping a `*sql.DB`, but `cli.Run` passes `nil`, so the endpoint always answers `ok`.
- 504. The handler maps `ErrCarrierTimeout` to 504, but the orchestrator does not return that error. A request that times out before any carrier answers gets 422 (`NO_ELIGIBLE_CARRIERS`).
- Worker. `CLAUDE.md` lists `cmd/worker` as planned. There is no worker code, and the API never publishes to Pub/Sub.
