---
summary: "Security controls in the API and the Terraform, with their limits"
read_when: [auth, middleware, firewall, IAM, security review, hardening]
---

# Security

riskforge is a study project and has not had an external security review. The controls below are what the code and Terraform contain. An internal hardening change (`openspec/changes/owasp-security-hardening`) listed 15 findings against the Terraform. Most were addressed; two are only partly addressed and are called out below.

## API (Go)

- API keys as `Authorization: Bearer <key>`. The comparison is constant time and checks every configured key. The key length is not enforced.
- Failed authentication is limited per client IP: a burst of 10, refilling about one attempt every 6 seconds, then 429 with `Retry-After: 60`. Idle IP entries are evicted after 5 minutes.
- Response headers on every response: `X-Content-Type-Options: nosniff`, `Strict-Transport-Security`, `X-Frame-Options: DENY`, `Cache-Control: no-store`, `Content-Security-Policy: default-src 'none'`.
- At most 100 requests in flight; the next one gets 503 with `Retry-After: 1`.
- Request bodies are capped at 1 MiB, unknown JSON fields are rejected, `request_id` is limited to 256 printable ASCII characters.
- Cache and singleflight keys include the first 8 characters of the API key, so two clients cannot read each other's cached quotes by reusing a `request_id`.
- Every request is logged with method, path, status, duration and a truncated client id.

## Terraform

- Audit logging (`ADMIN_READ`, `DATA_READ`, `DATA_WRITE`) for all services.
- Firewall: deny all ingress and egress at priority 65534, with allow rules for health checks, the connector subnet and `199.36.153.8/30` on port 443.
- `secretmanager.secretAccessor` is not granted at project level.
- The Terraform service account has 11 explicit roles and no `roles/editor`. They are admin roles, so this is narrower than editor but not least privilege.
- State bucket and documents bucket enforce public access prevention. Artifact Registry has vulnerability scanning and reader access for the runtime service accounts.
- Google provider pinned to `~> 6.14` in the modules and bootstrap.
- The CI plan step no longer uses `continue-on-error`.

## Open items

- The API Cloud Run service sets `allow_unauthenticated = true`. Access control is the API key check inside the app.
- The Cloud Armor policy is not attached to a load balancer, so it has no effect.
- The CMEK module is not used by any environment.
