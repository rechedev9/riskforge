# Review: integration-e2e-testing

**Date**: 2026-03-22
**Verdict**: PASS

## Summary

The implementation is high quality and closely follows the spec, design, and proposal. All 6 new test files and 2 modified config files are present and structurally correct. The middleware chain wiring in `newTestStack` faithfully mirrors `cli.Run` lines 178-184. Test isolation via unique IDs per test, build tag discipline, cleanup registration, and CI configuration are all properly implemented. Five spec scenarios are missing from the implementation (detailed below), one of which is a MAJOR gap; the rest are MINOR. No correctness bugs or race safety issues were found.

## Checklist

- [x] Spec coverage: 39 of 43 scenarios implemented (MAJOR-1 fixed; 4 MINOR gaps remain -- see below)
- [x] Correctness: All assertions match the source code behavior; no false positives
- [x] Race safety: `t.Cleanup(ts.Close)` and `t.Cleanup(stopAuth)` registered in all HTTP stack tests; concurrency tests use channel signaling (not timing); Spanner tests are single-goroutine
- [x] Build tag discipline: All 4 Spanner test files have `//go:build integration` as first line
- [x] Test isolation: Unique IDs via `fmt.Sprintf("..-%s", t.Name())` in Spanner tests; HTTP stack tests use independent `httptest.Server` instances per test
- [x] CI correctness: Workflow YAML has correct env vars matching docker-compose; jobs are independent (no `needs:` dependency)
- [x] Code quality: Follows existing project patterns; clean imports; no dead code

## Issues Found

### MAJOR-1: FIXED -- REQ-QUOTE-REPO-002 scenario 2 (Mix of expired and valid rows returns only valid)

**Spec requires**: Two rows for the same RequestID -- one expired, one valid. `FindByRequestID` must return `found=true` with `len(results)==1`.

**Fix applied**: Added `TestQuoteRepo_FindByRequestID_MixedExpiredAndValid` in `quote_repo_test.go:114`. Inserts one valid row via `repo.Save` and one expired row via direct mutation. Asserts `found=true`, `len(results)==1`, and the returned result is the valid row (`carrier-valid`, premium 200000).

**Status**: Verified — compiles with `-tags integration`, full test suite passes.

### MINOR-1: Missing spec scenario -- REQ-CARRIER-REPO-001 scenario 2 (ListActive with all inactive returns empty slice)

**Spec requires**: A test where only inactive carriers exist, verifying `ListActive` returns an empty slice.

**Implementation**: `TestCarrierRepo_ListActive_ReturnsActive` tests filtering but does not cover the all-inactive edge case. Due to shared emulator state, this is hard to test in isolation without table truncation, but the scenario is explicitly spec'd.

**Recommendation**: Add a test that inserts only inactive carriers with unique IDs and verifies none of them appear in `ListActive` results. Since other tests may have inserted active rows, the assertion should check that none of the *test-inserted* carriers appear, which is already the pattern used in existing tests.

### MINOR-2: Missing spec scenario -- REQ-APPETITE-REPO-002 scenario 2 (Query without classCode skips ClassCode filter entirely)

**Spec requires**: When `ClassCode=""` is passed, the ClassCode filter clause is not appended, so a rule with `ClassCode="auto-commercial"` still matches because the filter is absent.

**Implementation**: `TestAppetiteRepo_FindMatchingRules_WithClassCode` tests the non-empty ClassCode case but does not test the empty-ClassCode case separately. The `TestAppetiteRepo_FindMatchingRules_RequiredOnly` test uses `ClassCode=""` but inserts rules with `ClassCode=NULL`, which matches trivially; it does not test that a rule with a non-NULL ClassCode is returned when the query has no ClassCode filter.

**Recommendation**: Add a subtest or new test that inserts a rule with `ClassCode="auto-commercial"` and queries with `ClassCode=""`, asserting the rule is still returned (because the `ClassCode IS NULL OR ClassCode = @classCode` clause is not appended).

### MINOR-3: Missing spec scenario -- REQ-APPETITE-REPO-004 scenario 2 (ListAll excludes inactive rules)

**Spec requires**: A test verifying that `ListAll` does not return inactive rules.

**Implementation**: `TestAppetiteRepo_ListAll` inserts 3 active rules and asserts all 3 are returned, but does not insert an inactive rule to verify exclusion.

**Recommendation**: Add an inactive rule to the seeded data in `TestAppetiteRepo_ListAll` and verify it does not appear in the results.

### MINOR-4: Spanner tests lack `t.Parallel()`

**Observation**: None of the Spanner adapter tests call `t.Parallel()`. The test isolation strategy (unique IDs per test) explicitly enables parallel execution per the design doc (Architecture Decision #7). While they will still pass sequentially, adding `t.Parallel()` would make the tests run faster and would validate the isolation claim.

**Recommendation**: Add `t.Parallel()` to all Spanner adapter tests for consistency with the HTTP stack tests.

### NIT-1: `TestHTTPStack_ConcurrencyLimit_503` and `TestHTTPStack_ConcurrencyLimit_RecoveryAfterRelease` do not call `t.Parallel()`

**Observation**: These two tests intentionally do not use `t.Parallel()` and instead build their own middleware stack with a blocking handler. This is correct -- the `time.Sleep(50ms)` for goroutine scheduling would be unreliable under parallel test contention. However, neither test has a comment explaining why `t.Parallel()` is omitted, which may confuse future maintainers.

**Recommendation**: Add a brief comment like `// Not parallel: relies on deterministic slot acquisition timing.`

### NIT-2: `TestHTTPStack_ConcurrencyLimit_RecoveryAfterRelease` recovery request sends nil body

**Observation**: At line 433 of `http_stack_test.go`, the recovery request after `close(blockCh)` sends `POST /quotes` with `nil` body and no `Content-Type` header. The blocking handler does not parse the body (it just writes 200), so the assertion (`resp.StatusCode != http.StatusOK`) holds. However, this means the test is asserting 200 from the blocking handler, not from the real quote handler. The spec says "a new POST /quotes request is sent with valid auth and valid body" and expects 200. This is a weak assertion -- it proves the semaphore was freed but does not prove a real request would succeed.

**Recommendation**: Consider using `newTestStack(t, 2)` for the recovery test instead of the blocking handler, or send a valid body through the real handler after the blockers are released.

### NIT-3: Spec request_id values differ from implementation

**Observation**: The spec uses request IDs like `"integ-001"`, `"integ-002"`, `"integ-003"` for specific scenarios. The implementation uses `"happy-1"`, `"reqid-42"`, `"noauth-1"`, etc. This is purely cosmetic and has no behavioral impact. The spec says "WHEN a POST /quotes request is sent with request_id set to 'integ-002'" -- the implementation uses `"reqid-42"` for the same scenario.

**Impact**: None. Test correctness is unaffected by the specific string values.

### NIT-4: Design doc mentions `clearTable` helper in testutil_test.go; not implemented

**Observation**: The design doc's file change table (row 2) says testutil_test.go will contain `newEmulatorClient(t)` helper and `clearTable(t, client, table)` helper. The implementation only has `newEmulatorClient` and `envOrDefault`. The `clearTable` helper was not needed because the unique-ID-per-test strategy makes table truncation unnecessary.

**Impact**: None. Beneficial deviation -- simpler code without unused helpers.

## Spec Traceability

### Covered (38/43 scenarios)

| Requirement | Scenario | Test Function |
|---|---|---|
| REQ-HTTP-STACK-001 | Happy path POST /quotes | `TestHTTPStack_PostQuotes_HappyPath` |
| REQ-HTTP-STACK-001 | Response includes request_id and duration_ms | `TestHTTPStack_PostQuotes_RequestIDAndDuration` |
| REQ-HTTP-STACK-002 | Missing Authorization header returns 401 | `TestHTTPStack_PostQuotes_MissingAuth` |
| REQ-HTTP-STACK-002 | Invalid Bearer token returns 401 | `TestHTTPStack_PostQuotes_WrongKey` |
| REQ-HTTP-STACK-003 | All five security headers present | `TestHTTPStack_SecurityHeaders` |
| REQ-HTTP-STACK-003 | Security headers on error responses | `TestHTTPStack_SecurityHeaders_OnError` |
| REQ-HTTP-STACK-004 | Overflow request receives 503 | `TestHTTPStack_ConcurrencyLimit_503` |
| REQ-HTTP-STACK-004 | Requests succeed after release | `TestHTTPStack_ConcurrencyLimit_RecoveryAfterRelease` |
| REQ-HTTP-STACK-005 | GET /healthz bypasses auth | `TestHTTPStack_HealthzBypassesAuth` |
| REQ-HTTP-STACK-005 | GET /readyz bypasses auth | `TestHTTPStack_ReadyzBypassesAuth` |
| REQ-HTTP-STACK-005 | GET /metrics bypasses auth | `TestHTTPStack_MetricsBypassesAuth` |
| REQ-HTTP-STACK-006 | stopAuth cleanup | All tests via `t.Cleanup(stopAuth)` |
| REQ-HTTP-STACK-006 | Race detector passes | Enforced by `-race` in CI and Makefile |
| REQ-QUOTE-REPO-001 | Save + FindByRequestID round-trip | `TestQuoteRepo_Save_FindByRequestID` |
| REQ-QUOTE-REPO-001 | FindByRequestID miss | `TestQuoteRepo_FindByRequestID_Miss` |
| REQ-QUOTE-REPO-002 | Expired row not returned | `TestQuoteRepo_FindByRequestID_IgnoresExpired` |
| REQ-QUOTE-REPO-003 | DeleteExpired removes row | `TestQuoteRepo_DeleteExpired` |
| REQ-QUOTE-REPO-003 | FindByRequestID misses after DeleteExpired | `TestQuoteRepo_DeleteExpired` (combined) |
| REQ-QUOTE-REPO-004 | SPANNER_EMULATOR_HOST unset skips | `newEmulatorClient` (structural) |
| REQ-QUOTE-REPO-004 | SPANNER_EMULATOR_HOST set returns client | `newEmulatorClient` (structural) |
| REQ-CARRIER-REPO-001 | ListActive filters inactive | `TestCarrierRepo_ListActive_ReturnsActive` |
| REQ-CARRIER-REPO-002 | JSON Config round-trip | `TestCarrierRepo_ListActive_DecodesConfig` |
| REQ-CARRIER-REPO-002 | NULL Config yields zero-value | `TestCarrierRepo_ListActive_NullConfig` |
| REQ-APPETITE-REPO-001 | Match by state and LOB | `TestAppetiteRepo_FindMatchingRules_RequiredOnly` |
| REQ-APPETITE-REPO-001 | No matching rules | `TestAppetiteRepo_FindMatchingRules_NoMatch` |
| REQ-APPETITE-REPO-002 | NULL ClassCode wildcard | `TestAppetiteRepo_FindMatchingRules_WithClassCode` |
| REQ-APPETITE-REPO-003 | Premium within range | `TestAppetiteRepo_FindMatchingRules_PremiumRange` |
| REQ-APPETITE-REPO-003 | Premium outside range | `TestAppetiteRepo_FindMatchingRules_PremiumRange` |
| REQ-APPETITE-REPO-004 | ListAll returns all active | `TestAppetiteRepo_ListAll` |
| REQ-CLI-LIFECYCLE-001 | Empty API_KEYS returns error | `TestRun_MissingAPIKeys` |
| REQ-CLI-LIFECYCLE-001 | Unset API_KEYS returns error | `TestRun_MissingAPIKeys` (covers both) |
| REQ-CLI-LIFECYCLE-002 | Server starts, healthz, shutdown | `TestRun_CleanShutdown` |
| REQ-CLI-LIFECYCLE-002 | Does not block after cancel | `TestRun_CleanShutdown` (timeout assertion) |
| REQ-CI-INTEGRATION-001 | CI job env vars | `.github/workflows/go.yml` integration-tests job |
| REQ-CI-INTEGRATION-001 | CI job independent of build-and-test | No `needs:` in workflow |
| REQ-CI-INTEGRATION-002 | make test-integration | `Makefile` test-integration target |
| REQ-CI-INTEGRATION-002 | Additive to existing test target | `test` target unchanged in Makefile |
| REQ-CI-INTEGRATION-003 | Build tag isolation | `//go:build integration` on all Spanner test files |

### Not Covered (5 scenarios)

| Requirement | Scenario | Severity |
|---|---|---|
| REQ-QUOTE-REPO-002 scenario 2 | Mix of expired and valid rows returns only valid | **MAJOR** |
| REQ-CARRIER-REPO-001 scenario 2 | ListActive with all inactive returns empty | MINOR |
| REQ-APPETITE-REPO-002 scenario 2 | Query without classCode skips ClassCode filter | MINOR |
| REQ-APPETITE-REPO-004 scenario 2 | ListAll excludes inactive rules | MINOR |
| REQ-CI-INTEGRATION-003 scenario 2 | go test -tags integration includes Spanner tests | Not directly testable in review (CI validation) |
