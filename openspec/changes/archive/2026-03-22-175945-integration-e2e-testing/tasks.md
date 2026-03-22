# Apply: integration-e2e-testing

**Date**: 2026-03-22
**Status**: Complete

## Files Created (6)

| File | Lines | Status |
|------|-------|--------|
| `internal/integration/http_stack_test.go` | ~400 | PASS — 11 tests, -race clean |
| `internal/adapter/spanner/testutil_test.go` | ~30 | Compiles with `-tags integration` |
| `internal/adapter/spanner/quote_repo_test.go` | ~150 | Compiles with `-tags integration` |
| `internal/adapter/spanner/carrier_repo_test.go` | ~130 | Compiles with `-tags integration` |
| `internal/adapter/spanner/appetite_repo_test.go` | ~200 | Compiles with `-tags integration` |
| `internal/cli/run_test.go` | ~80 | PASS — 2 tests, -race clean |

## Files Modified (2)

| File | Change |
|------|--------|
| `.github/workflows/go.yml` | Added `integration-tests` job |
| `Makefile` | Added `test-integration` target |

## Tasks

- [x] Task 1: Create HTTP stack integration tests
- [x] Task 2: Create Spanner emulator test helper
- [x] Task 3: Create QuoteRepo integration tests
- [x] Task 4: Create CarrierRepo integration tests
- [x] Task 5: Create AppetiteRepo integration tests
- [x] Task 6: Create CLI lifecycle tests
- [x] Task 7: Update CI workflow with integration job
- [x] Task 8: Add Makefile test-integration target

## Verification

- `go build ./...` — PASS
- `go vet ./...` — PASS
- `go test -race -count=1 ./...` — ALL PASS (13 new tests in integration + cli)
- `go build -tags integration ./internal/adapter/spanner/...` — PASS (Spanner tests compile)
- No production code modified
- All 8 tasks complete
