---
summary: "Cloud Run Go reference - container contract, scaling, Pub/Sub push, IAM, deployment"
read_when: [Cloud Run deployment, service configuration, scaling, Pub/Sub integration]
---

# Cloud Run with Go -- Comprehensive Reference

## 1. Container Contract

Cloud Run injects the `PORT` environment variable into the ingress container (default `8080`).
The container **must** listen on `0.0.0.0:$PORT` -- never `127.0.0.1`.

```go
port := os.Getenv("PORT")
if port == "" {
    port = "8080"
}
srv := &http.Server{Addr: ":" + port, Handler: mux}
```

### Injected Environment Variables

| Variable | Description |
|---|---|
| `PORT` | Port to listen on (default 8080) |
| `K_SERVICE` | Service name |
| `K_REVISION` | Revision name |
| `K_CONFIGURATION` | Configuration name |

### Execution Environment

| Property | Gen 1 | Gen 2 |
|---|---|---|
| Sandbox | gVisor (limited syscalls) | Full Linux compatibility |
| Min memory | 128 MiB | 512 MiB |
| Jobs | Not available | Always gen2 |
| DMI product name | -- | `Google Compute Engine` |

Filesystem is **in-memory** (tmpfs); writes consume the instance's memory limit. Data does not persist when instances stop.

### Instance Lifecycle

1. **Startup** -- must begin listening within **4 minutes**; startup CPU boost available.
2. **Processing** -- CPU allocated to all containers while handling requests.
3. **Idle** -- instances kept idle up to **15 minutes** (10 min for GPU); CPU allocation depends on billing mode.
4. **Shutdown** -- `SIGTERM` sent with **10-second** grace period before `SIGKILL`.

### Request Timeout

- Default: **300 seconds** (5 minutes). Maximum: **3600 seconds** (60 minutes).
- On timeout: HTTP 504 returned to client; container keeps running.
- Outbound idle timeouts: **10 min** for VPC connections, **20 min** for internet.

```bash
gcloud run deploy SERVICE --image IMAGE --timeout=300s
```

```yaml
spec:
  template:
    spec:
      containers:
      - image: IMAGE
      timeoutSeconds: 300
```

### Health Probes

Startup probes disable liveness/readiness checks until the container is marked started.

**Default TCP startup probe:**

```yaml
startupProbe:
  tcpSocket:
    port: 8080
  timeoutSeconds: 240
  periodSeconds: 240
  failureThreshold: 1
```

**HTTP startup probe:**

```yaml
startupProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 0
  timeoutSeconds: 1
  periodSeconds: 10
  failureThreshold: 3
```

**Liveness probe** (HTTP -- restarts container on failure):

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 0
  timeoutSeconds: 1
  periodSeconds: 10
  failureThreshold: 3
```

**Readiness probe** (preview -- stops sending traffic on failure):

```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  periodSeconds: 10
  successThreshold: 2
  failureThreshold: 3
  timeoutSeconds: 1
```

Configurable parameter ranges:

| Parameter | Startup | Liveness | Readiness |
|---|---|---|---|
| `initialDelaySeconds` | 0-240 | 0-240 | -- |
| `periodSeconds` | 1-240 | 1-3600 | 1-300 |
| `failureThreshold` | configurable | configurable | configurable |
| `timeoutSeconds` | 1-240 | 1-3600 | 1-300 |

Probe types: **HTTP** (2xx/3xx = success), **TCP** (connection established = success), **gRPC** (requires gRPC Health Checking protocol).

### Metadata Server

Available at `http://metadata.google.internal/` with header `Metadata-Flavor: Google`.

| Path | Returns |
|---|---|
| `/computeMetadata/v1/project/project-id` | Project ID |
| `/computeMetadata/v1/instance/region` | Instance region |
| `/computeMetadata/v1/instance/service-accounts/default/email` | SA email |
| `/computeMetadata/v1/instance/service-accounts/default/token` | OAuth2 access token |
| `/computeMetadata/v1/instance/service-accounts/default/identity?audience=URL` | OIDC ID token |

---

## 2. Go Best Practices

### Graceful Shutdown with signal.NotifyContext + http.Server.Shutdown

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Trap SIGTERM (Cloud Run shutdown) and SIGINT (local dev).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		// Check downstream dependencies here.
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background goroutine.
	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen failed", "error", err)
			os.Exit(1)
		}
	}()

	// Block until SIGTERM received.
	<-ctx.Done()
	slog.Info("shutdown signal received")

	// Give in-flight requests up to 8 seconds to complete
	// (leaving 2s buffer before SIGKILL at 10s).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("server stopped")
}
```

### Structured Logging with slog to stderr (JSON Handler for Cloud Logging)

Cloud Run captures JSON written to `stderr` and parses it into Cloud Logging's structured format. Map `slog` field names to Cloud Logging's expected keys:

| slog key | Cloud Logging key |
|---|---|
| `msg` | `message` |
| `level` | `severity` |
| `source` | `logging.googleapis.com/sourceLocation` |

```go
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"cloud.google.com/go/compute/metadata"
)

// LevelCritical maps to Cloud Logging CRITICAL severity.
const LevelCritical = slog.Level(12)

// SetupLogging configures slog with a JSON handler that maps to Cloud Logging fields.
func SetupLogging() {
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if groups != nil {
				return a
			}
			switch a.Key {
			case slog.MessageKey:
				a.Key = "message"
			case slog.SourceKey:
				a.Key = "logging.googleapis.com/sourceLocation"
			case slog.LevelKey:
				a.Key = "severity"
				level := a.Value.Any().(slog.Level)
				if level == LevelCritical {
					a.Value = slog.StringValue("CRITICAL")
				}
			}
			return a
		},
	})
	slog.SetDefault(slog.New(&traceHandler{handler: h}))
}

// traceHandler injects the Cloud Trace ID from context into every log record.
type traceHandler struct {
	handler slog.Handler
}

type traceKey struct{}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, rec slog.Record) error {
	if trace, ok := ctx.Value(traceKey{}).(string); ok && trace != "" {
		rec = rec.Clone()
		rec.Add("logging.googleapis.com/trace", slog.StringValue(trace))
	}
	return h.handler.Handle(ctx, rec)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{handler: h.handler.WithGroup(name)}
}

// WithCloudTraceContext is HTTP middleware that extracts the trace ID
// from the X-Cloud-Trace-Context header and stores it in the request context.
func WithCloudTraceContext(next http.Handler) http.Handler {
	projectID, _ := metadata.ProjectIDWithContext(context.Background())

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var trace string
		traceHeader := r.Header.Get("X-Cloud-Trace-Context")
		parts := strings.Split(traceHeader, "/")
		if len(parts) > 0 && parts[0] != "" {
			trace = fmt.Sprintf("projects/%s/traces/%s", projectID, parts[0])
		}
		ctx := context.WithValue(r.Context(), traceKey{}, trace)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

Usage:

```go
func main() {
    logging.SetupLogging()

    mux := http.NewServeMux()
    mux.HandleFunc("GET /", handleRoot)

    // Wrap with trace middleware.
    handler := logging.WithCloudTraceContext(mux)

    srv := &http.Server{Addr: ":8080", Handler: handler}
    // ...
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
    slog.InfoContext(r.Context(), "request received",
        "method", r.Method,
        "path",   r.URL.Path,
    )
    w.Write([]byte("ok"))
}
```

Cloud Logging special JSON fields reference:

```json
{
  "severity": "ERROR",
  "message": "something failed",
  "logging.googleapis.com/trace": "projects/my-project/traces/abc123",
  "logging.googleapis.com/spanId": "000000000000004a",
  "logging.googleapis.com/sourceLocation": {"file": "main.go", "line": "42", "function": "main.handler"},
  "logging.googleapis.com/labels": {"request_id": "xyz"},
  "logging.googleapis.com/insertId": "unique-id"
}
```

### Health Check Endpoints

```go
// /healthz -- liveness: is the process alive and not deadlocked?
mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
    w.WriteHeader(http.StatusOK)
})

// /readyz -- readiness: can the instance accept traffic? (DB, caches, etc.)
mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
    if err := db.PingContext(r.Context()); err != nil {
        http.Error(w, "not ready", http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
})
```

### Global State & Lazy Initialization

Leverage instance reuse -- expensive init once at cold start:

```go
var (
    client     *storage.Client
    clientOnce sync.Once
)

func getClient(ctx context.Context) *storage.Client {
    clientOnce.Do(func() {
        var err error
        client, err = storage.NewClient(ctx)
        if err != nil {
            slog.Error("storage client init failed", "err", err)
        }
    })
    return client
}
```

---

## 3. Dockerfile

Multi-stage build producing a minimal image:

```dockerfile
# ---- Build stage ----
FROM golang:1.23-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static binary, no CGO. Strip debug symbols for smaller binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# ---- Runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /server /server

# Cloud Run injects PORT; 8080 is the documented default.
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/server"]
```

Alternative with scratch (even smaller, but no CA certs or tzdata bundled):

```dockerfile
FROM golang:1.23-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

Key points:

- `CGO_ENABLED=0` produces a fully static binary (no libc dependency) -- **required** for scratch/distroless.
- `gcr.io/distroless/static-debian12:nonroot` includes CA certs and tzdata but no shell -- minimal attack surface.
- `-trimpath` removes local filesystem paths from binary. `-ldflags="-s -w"` strips symbol/DWARF tables.
- For scratch: manually copy CA certs and timezone data.
- Use `nonroot` distroless tag to avoid running as root.

For an even simpler workflow, use **ko** (`ko build ./cmd/server`) which produces distroless images automatically without a Dockerfile.

### .dockerignore

```
.git
*.md
vendor/
```

---

## 4. Concurrency & Scaling

### Max Concurrency

Maximum concurrent requests a single instance handles simultaneously.

| Deployed via | Default concurrency |
|---|---|
| gcloud / Terraform | 80 * number of vCPUs |
| Console | 80 |

Maximum configurable value: **1000**. The autoscaler targets **60% of max concurrency** over a 1-minute window.

```bash
# Set concurrency
gcloud run services update SERVICE --concurrency 100

# Reset to default
gcloud run services update SERVICE --concurrency default
```

```yaml
spec:
  template:
    spec:
      containerConcurrency: 100
```

Guidelines:
- **Lower to 1** for CPU-heavy serial processing or when each request consumes most available resources.
- **Raise for I/O-bound** or multi-threaded applications.
- Higher concurrency = fewer instances = lower cost, but requires the app to handle parallel requests efficiently.
- For single-threaded apps, keep at 1 vCPU -- multi-vCPU with single-threaded code prevents CPU-based autoscaling.

### Min Instances

Keep instances warm to reduce cold starts. Default: **0** (scale to zero).

```bash
gcloud run deploy SERVICE --image IMAGE --min 2
gcloud run services update SERVICE --min 2

# Clear (back to 0)
gcloud run services update SERVICE --min default
```

```yaml
metadata:
  annotations:
    run.googleapis.com/minScale: '2'
```

Revision-level:

```yaml
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/minScale: '2'
```

Billing: idle min-instances are billed at a **reduced rate** under request-based billing, or at the full rate under instance-based billing. Consider Committed Use Discounts for predictable charges.

### Max Instances

Default: **100** per revision. All services have a max-instances limit even if unset.

```bash
gcloud run deploy SERVICE --image IMAGE --max 50
gcloud run services update SERVICE --max 50
```

```yaml
metadata:
  annotations:
    run.googleapis.com/maxScale: '50'
```

When max is reached, excess requests queue for up to **3.5x average startup time** or **10 seconds** (whichever is greater), then return `429`. Cloud Run may briefly exceed the limit during traffic spikes.

### CPU Configuration

Supported vCPU values:
- Fractional: 0.08 -- 1.0 (increments of 0.001)
- Integer: 2, 4, 6, 8

CPU-to-memory constraints:

| CPU | Max Memory |
|---|---|
| 0.08-0.5 vCPU | 512 MiB |
| 1 vCPU | 4 GiB |
| 2 vCPU | 8 GiB |
| 4 vCPU | 16 GiB |
| 6 vCPU | 24 GiB |
| 8 vCPU | 32 GiB |

Sub-1 vCPU restrictions: max 512 MiB memory, concurrency must be 1, request-based billing only, gen1 required.

```bash
gcloud run deploy SERVICE --image IMAGE --cpu=2 --memory=1Gi
```

```yaml
resources:
  limits:
    cpu: "2"
    memory: "1Gi"
```

### CPU Allocation Modes

| Mode | Behavior | Use case |
|---|---|---|
| Request-based (default) | CPU allocated only during request processing | Intermittent traffic |
| Instance-based (always-on) | CPU allocated for entire instance lifetime | Background work, websockets, streaming |

```bash
# Always-on CPU
gcloud run services update SERVICE --no-cpu-throttling

# Request-based (default)
gcloud run services update SERVICE --cpu-throttling
```

### Startup CPU Boost

Temporarily increases CPU during container startup + 10 seconds after:

| Configured CPU | Boosted CPU |
|---|---|
| <= 1 | 2 |
| 2 | 4 |
| 4 | 8 |
| 6-8 | 8 |

```bash
gcloud run services update SERVICE --cpu-boost
gcloud run services update SERVICE --no-cpu-boost
```

```yaml
metadata:
  annotations:
    run.googleapis.com/startup-cpu-boost: 'true'
```

You are billed for boosted CPU during the boost period.

### Scaling Behavior

Autoscaler signals:
1. **Request concurrency** -- targets 60% of max concurrency over 1-minute window.
2. **CPU utilization** -- targets 60% CPU utilization over 1-minute window.
3. **Incoming request rate** -- scales based on rate of incoming requests.

Cold start mitigation:
- Set `--min 1` to keep warm instances.
- Enable `--cpu-boost` for faster startup.
- Use small container images (distroless/scratch).
- Initialize expensive resources lazily with `sync.Once`.
- Idle instances kept up to **15 minutes** before shutdown.

---

## 5. Pub/Sub Push

Cloud Run receives Pub/Sub messages as HTTP POST requests. The push subscription sends a JSON envelope with base64-encoded data.

### Push Subscription Setup

```bash
# Create topic
gcloud pubsub topics create my-topic

# Create service account for push invocation
gcloud iam service-accounts create pubsub-invoker \
  --display-name "Pub/Sub Invoker"

# Grant invoker role on the Cloud Run service
gcloud run services add-iam-policy-binding my-service \
  --member=serviceAccount:pubsub-invoker@PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/run.invoker

# Grant token creator to Pub/Sub service agent (for OIDC tokens)
PROJECT_NUMBER=$(gcloud projects describe PROJECT_ID --format='value(projectNumber)')
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member=serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-pubsub.iam.gserviceaccount.com \
  --role=roles/iam.serviceAccountTokenCreator

# Create push subscription with OIDC auth
gcloud pubsub subscriptions create my-sub \
  --topic=my-topic \
  --ack-deadline=600 \
  --push-endpoint=https://my-service-xxxxx.a.run.app/ \
  --push-auth-service-account=pubsub-invoker@PROJECT_ID.iam.gserviceaccount.com
```

### Message Format

```json
{
  "message": {
    "data": "SGVsbG8gV29ybGQ=",
    "attributes": { "key": "value" },
    "messageId": "123456789",
    "publishTime": "2024-01-01T00:00:00Z"
  },
  "subscription": "projects/PROJECT_ID/subscriptions/my-sub"
}
```

The `data` field is base64-encoded. When unmarshalled into a `[]byte` field with `encoding/json`, Go automatically decodes the base64.

### Go Handler

```go
// PubSubEnvelope is the push delivery wrapper.
type PubSubEnvelope struct {
	Message struct {
		Data        []byte            `json:"data,omitempty"` // auto base64-decoded by encoding/json
		Attributes  map[string]string `json:"attributes,omitempty"`
		ID          string            `json:"messageId"`
		PublishTime string            `json:"publishTime"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

func handlePubSub(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(r.Context(), "read body failed", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var env PubSubEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		slog.ErrorContext(r.Context(), "unmarshal failed", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	slog.InfoContext(r.Context(), "pubsub message received",
		"messageId", env.Message.ID,
		"data", string(env.Message.Data),
	)

	// Process the message...

	// Return 2xx to ACK. Any non-2xx triggers redelivery.
	w.WriteHeader(http.StatusOK)
}
```

### Ack/Nack Behavior

| HTTP Response | Pub/Sub Action |
|---|---|
| 200, 201, 202, 204 | Acknowledge (message consumed) |
| 102, 4xx, 5xx | Nack (message retried with backoff) |

Messages not acknowledged within the ack deadline are redelivered. Implement **idempotent handlers** to handle redeliveries safely.

### OIDC Token Validation

When the push subscription is configured with OIDC authentication, Pub/Sub attaches a JWT in the `Authorization: Bearer <token>` header.

**For Cloud Run services: validation is automatic.** Cloud Run verifies the token and checks that the push subscription's service account has `roles/run.invoker`. No application-level validation needed.

For manual validation (e.g., behind a load balancer without Cloud Run's built-in check):

```go
import "google.golang.org/api/idtoken"

func validatePubSubToken(r *http.Request, audience string) error {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return fmt.Errorf("missing bearer token")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	payload, err := idtoken.Validate(r.Context(), token, audience)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	slog.Info("token validated", "email", payload.Claims["email"])
	return nil
}
```

### Terraform

```hcl
resource "google_pubsub_subscription" "push" {
  name  = "my-sub"
  topic = google_pubsub_topic.topic.id

  ack_deadline_seconds = 600

  push_config {
    push_endpoint = google_cloud_run_v2_service.default.uri

    oidc_token {
      service_account_email = google_service_account.invoker.email
    }
  }
}
```

---

## 6. Eventarc Triggers

Eventarc delivers events in **CloudEvents v1.0** format via HTTP POST (binary content mode).

### Cloud Storage Events

Four direct event types:

| Event Type | Trigger |
|---|---|
| `google.cloud.storage.object.v1.finalized` | Object created or overwritten |
| `google.cloud.storage.object.v1.deleted` | Object deleted |
| `google.cloud.storage.object.v1.archived` | Live version becomes noncurrent |
| `google.cloud.storage.object.v1.metadataUpdated` | Object metadata changed |

```bash
# Enable APIs
gcloud services enable eventarc.googleapis.com eventarcpublishing.googleapis.com

# Grant Pub/Sub publisher to Cloud Storage service agent
GCS_SA="$(gcloud storage service-agent --project=PROJECT_ID)"
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:${GCS_SA}" \
  --role="roles/pubsub.publisher"

# Create trigger
gcloud eventarc triggers create storage-trigger \
  --location=us-central1 \
  --destination-run-service=my-service \
  --destination-run-region=us-central1 \
  --event-filters="type=google.cloud.storage.object.v1.finalized" \
  --event-filters="bucket=my-bucket" \
  --service-account=PROJECT_NUMBER-compute@developer.gserviceaccount.com
```

Constraints:
- Bucket must be in same project and region/multi-region as the trigger.
- Max 10 notifications per bucket.
- Trigger event filters cannot be changed after creation.
- Propagation takes up to 2 minutes.

### Spanner Change Stream Events

Spanner change streams emit events through Pub/Sub. Route via Eventarc:

```bash
gcloud eventarc triggers create spanner-trigger \
  --location=us-central1 \
  --destination-run-service=my-service \
  --destination-run-region=us-central1 \
  --event-filters="type=google.cloud.pubsub.topic.v1.messagePublished" \
  --transport-topic=projects/PROJECT_ID/topics/SPANNER_CHANGE_TOPIC \
  --service-account=PROJECT_NUMBER-compute@developer.gserviceaccount.com
```

The Spanner change record arrives as base64-encoded data in the Pub/Sub push format (see Section 5).

### Audit Log Events

Trigger on any audited GCP API call:

```bash
gcloud eventarc triggers create audit-trigger \
  --location=us-central1 \
  --destination-run-service=my-service \
  --destination-run-region=us-central1 \
  --event-filters="type=google.cloud.audit.log.v1.written" \
  --event-filters="serviceName=bigquery.googleapis.com" \
  --event-filters="methodName=google.cloud.bigquery.v2.JobService.InsertJob" \
  --service-account=PROJECT_NUMBER-compute@developer.gserviceaccount.com
```

All three filters (`type`, `serviceName`, `methodName`) are mandatory.

### CloudEvents Format

The CloudEvent HTTP headers contain metadata in binary content mode:

| Header | Maps to |
|---|---|
| `ce-type` | Event type (e.g., `google.cloud.storage.object.v1.finalized`) |
| `ce-source` | Source (e.g., `//storage.googleapis.com/projects/_/buckets/BUCKET`) |
| `ce-subject` | Object path (e.g., `objects/my-file.txt`) |
| `ce-id` | Unique event ID |
| `ce-time` | Event timestamp (RFC 3339) |
| `Content-Type` | `application/json` |

### Go CloudEvents Handler

Use the CloudEvents SDK (`github.com/cloudevents/sdk-go/v2`):

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// StorageObjectData represents the Cloud Storage object payload.
type StorageObjectData struct {
	Bucket         string    `json:"bucket"`
	Name           string    `json:"name"`
	ContentType    string    `json:"contentType"`
	Size           string    `json:"size"`
	TimeCreated    time.Time `json:"timeCreated"`
	Updated        time.Time `json:"updated"`
	Metageneration string    `json:"metageneration"`
}

func handleCloudEvent(w http.ResponseWriter, r *http.Request) {
	event, err := cloudevents.NewEventFromHTTPRequest(r)
	if err != nil {
		slog.Error("failed to parse CloudEvent", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	slog.Info("event received",
		"type", event.Type(),
		"source", event.Source(),
		"subject", event.Subject(),
		"id", event.ID(),
	)

	var data StorageObjectData
	if err := event.DataAs(&data); err != nil {
		slog.Error("failed to parse event data", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	slog.Info("storage object",
		"bucket", data.Bucket,
		"name", data.Name,
		"contentType", data.ContentType,
		"size", data.Size,
	)

	w.WriteHeader(http.StatusOK)
}
```

Alternative: receiver pattern (starts its own HTTP server):

```go
func receive(event cloudevents.Event) {
	slog.Info("received", "type", event.Type(), "id", event.ID())
}

func main() {
	c, err := cloudevents.NewClientHTTP()
	if err != nil {
		slog.Error("failed to create client", "error", err)
		os.Exit(1)
	}
	if err = c.StartReceiver(context.Background(), receive); err != nil {
		slog.Error("failed to start receiver", "error", err)
		os.Exit(1)
	}
}
```

### Required IAM for Eventarc

```bash
# Grant the compute service account permission to invoke Cloud Run
gcloud run services add-iam-policy-binding my-service \
  --member="serviceAccount:PROJECT_NUMBER-compute@developer.gserviceaccount.com" \
  --role="roles/run.invoker"

# Grant Eventarc event receiver role
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:PROJECT_NUMBER-compute@developer.gserviceaccount.com" \
  --role="roles/eventarc.eventReceiver"
```

---

## 7. IAM & Security

### Service Accounts

Two identities in play:
- **Deployer** -- user or SA that calls Cloud Run Admin API.
- **Runtime service identity** -- SA used when the container calls GCP APIs.

Always use a dedicated **user-managed service account** with least privilege (not the Compute Engine default SA).

```bash
# Create dedicated SA
gcloud iam service-accounts create my-service-sa \
  --display-name="My Service Runtime SA"

# Deploy with custom SA
gcloud run deploy my-service \
  --image IMAGE \
  --service-account=my-service-sa@PROJECT_ID.iam.gserviceaccount.com
```

### Key IAM Roles

| Role | Description | Permissions (key) |
|---|---|---|
| `roles/run.admin` | Full control over all Cloud Run resources | CRUD services/jobs, set IAM policies |
| `roles/run.developer` | Deploy and manage services/jobs | CRUD services/jobs, get (not set) IAM policies |
| `roles/run.invoker` | Invoke services, run/cancel jobs | `run.routes.invoke`, `run.jobs.run` |
| `roles/run.viewer` | Read-only access | View services, jobs, get IAM policies |

### Making Services Public or Private

```bash
# Public -- disable invoker IAM check (recommended for public APIs)
gcloud run deploy my-service --no-invoker-iam-check

# Public -- alternative: grant allUsers the invoker role
gcloud run services add-iam-policy-binding my-service \
  --member="allUsers" \
  --role="roles/run.invoker"

# Private -- require authentication (default)
gcloud run deploy my-service --invoker-iam-check

# Grant specific SA access
gcloud run services add-iam-policy-binding my-service \
  --member="serviceAccount:caller@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/run.invoker"
```

### Service-to-Service Authentication

The calling service fetches an ID token and sends it as a Bearer token:

```go
import "google.golang.org/api/idtoken"

func callService(ctx context.Context, targetURL string) error {
	client, err := idtoken.NewClient(ctx, targetURL)
	if err != nil {
		return err
	}
	resp, err := client.Get(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
```

The token's `aud` claim must match the receiving service's URL or a configured custom audience. Tokens expire after ~1 hour; the `idtoken` client handles refresh automatically.

### Ingress Settings

Control which network paths can reach the service.

| Setting | Allows |
|---|---|
| `all` (default) | All traffic including direct internet to `*.run.app` URL |
| `internal` | VPC traffic, Shared VPC, and GCP managed services (Pub/Sub, Scheduler, Tasks, Eventarc, Workflows) |
| `internal-and-cloud-load-balancing` | Internal + external via Application Load Balancer only (blocks direct `run.app` access) |

```bash
gcloud run deploy my-service --image IMAGE --ingress internal
gcloud run services update my-service --ingress internal-and-cloud-load-balancing
```

```yaml
metadata:
  annotations:
    run.googleapis.com/ingress: internal-and-cloud-load-balancing
```

Use `internal-and-cloud-load-balancing` to enable Cloud Armor, IAP, and CDN while blocking direct `run.app` URL access. Administrators can restrict allowed ingress settings via the `run.allowedIngress` organization policy.

### VPC Connectors (Egress)

Enable private network egress from Cloud Run to VPC resources. Each connector needs a `/28` subnet.

```bash
# Create connector
gcloud compute networks vpc-access connectors create my-connector \
  --region us-central1 \
  --subnet my-subnet \
  --min-instances 2 \
  --max-instances 10 \
  --machine-type e2-micro

# Deploy with connector
gcloud run deploy my-service \
  --image IMAGE \
  --vpc-connector my-connector \
  --vpc-egress private-ranges-only   # or all-traffic

# Disconnect
gcloud run services update my-service --clear-vpc-connector
```

```yaml
spec:
  template:
    metadata:
      annotations:
        run.googleapis.com/vpc-access-connector: my-connector
        run.googleapis.com/vpc-access-egress: private-ranges-only
```

Egress modes:
- `private-ranges-only` (default) -- only RFC 1918/6598 traffic goes through VPC.
- `all-traffic` -- all outbound traffic routes through the connector (requires NAT gateway for internet access).

Notes:
- Connector must be in the same region as the Cloud Run service.
- Machine types: `f1-micro`, `e2-micro`, `e2-standard-4`. Use `e2-standard-4` for high-concurrency workloads.
- Connector instances (min 2, max 10) incur charges even when idle.
- **Alternative**: Direct VPC egress (no connector needed, newer approach).

---

## 8. Secrets

Two methods to expose Secret Manager secrets to Cloud Run containers.

### As Environment Variables

Resolved at instance **startup**. Pin to a specific version for deterministic deployments.

```bash
gcloud run deploy my-service \
  --image IMAGE \
  --update-secrets=DB_PASSWORD=db-password-secret:3
```

```yaml
spec:
  template:
    spec:
      containers:
      - image: IMAGE
        env:
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: db-password-secret
              key: "3"                 # pinned version number
```

Go usage:

```go
dbPassword := os.Getenv("DB_PASSWORD")
```

### As Volume Mounts

Mounted as files. Can auto-refresh when using `latest`.

```bash
gcloud run deploy my-service \
  --image IMAGE \
  --update-secrets=/secrets/db-password=db-password-secret:latest
```

```yaml
spec:
  template:
    spec:
      containers:
      - image: IMAGE
        volumeMounts:
        - name: db-password
          mountPath: /secrets/db-password
          readOnly: true
      volumes:
      - name: db-password
        secret:
          secretName: db-password-secret
          items:
          - key: latest
            path: value
```

Go usage:

```go
secret, err := os.ReadFile("/secrets/db-password/value")
if err != nil {
    return fmt.Errorf("read secret: %w", err)
}
```

### Latest vs Pinned Versions

| Method | Recommended version | Why |
|---|---|---|
| Env var | Pinned numeric (`3`, `5`) | Resolved once at startup; `latest` may change between deploys |
| Volume mount | `latest` | Re-read on each access; supports rotation without redeploy |

### Multiple Secrets

```bash
gcloud run deploy my-service \
  --image IMAGE \
  --update-secrets=DB_PASSWORD=db-password:3,API_KEY=api-key:latest
```

### Cross-Project Secrets

```bash
gcloud run deploy my-service \
  --image IMAGE \
  --update-secrets=/secrets/key=projects/123456/secrets/shared-secret:latest
```

### Required IAM

The runtime SA needs `roles/secretmanager.secretAccessor` on each secret:

```bash
gcloud secrets add-iam-policy-binding db-password-secret \
  --member=serviceAccount:my-service-sa@PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/secretmanager.secretAccessor
```

### Limitations

- Cannot mount at `/dev`, `/proc`, `/sys` or their subdirectories.
- Multiple secrets cannot share the same mount path.
- Mounting at a path hides existing directory contents.
- Regional secrets not supported.

---

## 9. Deployment

### gcloud run deploy

```bash
# From container image
gcloud run deploy my-service \
  --image us-docker.pkg.dev/PROJECT/REPO/IMAGE:TAG \
  --region us-central1 \
  --allow-unauthenticated

# With full configuration
gcloud run deploy my-service \
  --image IMAGE \
  --region us-central1 \
  --cpu 2 --memory 1Gi \
  --min 1 --max 50 \
  --concurrency 100 \
  --timeout 300s \
  --service-account my-sa@PROJECT.iam.gserviceaccount.com \
  --ingress internal \
  --set-env-vars APP_ENV=production,LOG_LEVEL=info \
  --update-secrets DB_PASS=db-pass:3 \
  --cpu-boost \
  --no-cpu-throttling
```

### Source Deploy

Cloud Run uses Cloud Build + Buildpacks (or your Dockerfile if present) to build and push automatically. Creates an Artifact Registry repo named `cloud-run-source-deploy` if none exists.

```bash
# Auto-detect: uses Dockerfile if present, otherwise Buildpacks
gcloud run deploy my-service --source . --region us-central1

# Deploy without build (preview -- for pre-built artifacts)
gcloud run deploy my-service --source . --no-build --base-image BASE_IMAGE
```

Required permissions for source deploy:
- `roles/run.sourceDeveloper` (Cloud Run Source Developer)
- `roles/serviceusage.serviceUsageConsumer`
- `roles/iam.serviceAccountUser` on the Cloud Run service identity
- Cloud Build SA needs `roles/run.builder`

### Cloud Build Integration

```yaml
# cloudbuild.yaml
steps:
  - name: 'gcr.io/cloud-builders/docker'
    args: ['build', '-t', 'us-docker.pkg.dev/$PROJECT_ID/my-repo/my-service:$COMMIT_SHA', '.']
  - name: 'gcr.io/cloud-builders/docker'
    args: ['push', 'us-docker.pkg.dev/$PROJECT_ID/my-repo/my-service:$COMMIT_SHA']
  - name: 'gcr.io/google.com/cloudsdktool/cloud-sdk'
    entrypoint: gcloud
    args:
      - 'run'
      - 'deploy'
      - 'my-service'
      - '--image=us-docker.pkg.dev/$PROJECT_ID/my-repo/my-service:$COMMIT_SHA'
      - '--region=us-central1'
images:
  - 'us-docker.pkg.dev/$PROJECT_ID/my-repo/my-service:$COMMIT_SHA'
```

Cloud Build SA requires: `roles/run.admin`, `roles/iam.serviceAccountUser`, `roles/artifactregistry.writer`.

### Traffic Splitting

```bash
# Deploy without serving traffic (canary prep)
gcloud run deploy my-service --image IMAGE --no-traffic

# Send 10% to new revision
gcloud run services update-traffic my-service --to-revisions LATEST=10

# Split between specific revisions
gcloud run services update-traffic my-service \
  --to-revisions my-service-00005-abc=90,my-service-00006-def=10

# Route all traffic to latest
gcloud run services update-traffic my-service --to-latest
```

### Tagged Revisions (Preview URLs)

```bash
# Deploy with a tag (gets URL: https://canary---my-service-xxxxx-uc.a.run.app)
gcloud run deploy my-service --image IMAGE --no-traffic --tag canary

# Send traffic to tagged revision
gcloud run services update-traffic my-service --to-tags canary=5

# Remove tag
gcloud run services update-traffic my-service --remove-tags canary
```

### Rollback

```bash
# List revisions
gcloud run revisions list --service my-service

# Roll back to a known-good revision
gcloud run services update-traffic my-service \
  --to-revisions my-service-00003-xyz=100
```

Cloud Run checks deployment health automatically: a new revision must pass its startup probe before receiving traffic. If the probe fails, the revision is marked unhealthy and traffic stays on the previous revision.

### Disable Deployment Health Check

```bash
gcloud run deploy my-service --image IMAGE --no-deploy-health-check
```

---

## 10. Environment Variables

### Via gcloud

```bash
# Set on deploy (destructive -- replaces all existing vars)
gcloud run deploy my-service --image IMAGE \
  --set-env-vars KEY1=value1,KEY2=value2

# Update without replacing all (non-destructive)
gcloud run services update my-service \
  --update-env-vars KEY3=value3

# Remove specific vars
gcloud run services update my-service \
  --remove-env-vars KEY1,KEY2

# Clear all
gcloud run services update my-service --clear-env-vars

# From file (.env or YAML format)
gcloud run deploy my-service --image IMAGE \
  --env-vars-file=.env.yaml
```

Escape commas in values with a custom delimiter:

```bash
gcloud run deploy my-service --image IMAGE \
  --set-env-vars "^@^HOSTS=host1,host2,host3@DB=mydb"
```

### Via YAML (Revision Template)

```yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: my-service
spec:
  template:
    metadata:
      name: my-service-v2
    spec:
      containers:
      - image: IMAGE
        env:
        - name: APP_ENV
          value: production
        - name: LOG_LEVEL
          value: info
```

```bash
gcloud run services replace service.yaml
```

Supported env file formats:

```yaml
# .env.yaml
APP_ENV: production
LOG_LEVEL: info
```

```
# .env
APP_ENV=production
LOG_LEVEL=info
```

### Via Terraform

```hcl
resource "google_cloud_run_v2_service" "default" {
  name                = "my-service"
  location            = "us-central1"
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"

  template {
    service_account = "my-sa@my-project.iam.gserviceaccount.com"

    scaling {
      min_instance_count = 1
      max_instance_count = 10
    }

    containers {
      image = "us-docker.pkg.dev/my-project/my-repo/my-service:latest"

      resources {
        limits = {
          cpu    = "2"
          memory = "1Gi"
        }
        cpu_idle          = false    # always-on CPU
        startup_cpu_boost = true
      }

      env {
        name  = "APP_ENV"
        value = "production"
      }

      env {
        name  = "LOG_LEVEL"
        value = "info"
      }

      # Secret from Secret Manager
      env {
        name = "DB_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = "db-password-secret"
            version = "3"
          }
        }
      }

      ports {
        container_port = 8080
      }

      startup_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
        initial_delay_seconds = 0
        timeout_seconds       = 1
        period_seconds        = 10
        failure_threshold     = 3
      }

      liveness_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
        initial_delay_seconds = 0
        timeout_seconds       = 1
        period_seconds        = 10
        failure_threshold     = 3
      }
    }
  }

  traffic {
    percent = 100
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
  }
}
```

### Limits

- Max **1,000** environment variables per container.
- Max **32 KB** per variable value.
- Reserved (cannot be set): `PORT`, `K_SERVICE`, `K_REVISION`, `K_CONFIGURATION`.
- Invalid names: empty string, contains `=`, starts with `X_GOOGLE_`.

---

## Sources

- [Container Runtime Contract](https://docs.cloud.google.com/run/docs/container-contract)
- [Configure Health Checks](https://docs.cloud.google.com/run/docs/configuring/healthchecks)
- [About Concurrency](https://docs.cloud.google.com/run/docs/about-concurrency)
- [Set Max Concurrent Requests](https://docs.cloud.google.com/run/docs/configuring/concurrency)
- [About Instance Autoscaling](https://docs.cloud.google.com/run/docs/about-instance-autoscaling)
- [Set Min Instances](https://docs.cloud.google.com/run/docs/configuring/min-instances)
- [Set Max Instances](https://docs.cloud.google.com/run/docs/configuring/max-instances)
- [Configure CPU](https://docs.cloud.google.com/run/docs/configuring/services/cpu)
- [Pub/Sub Tutorial](https://docs.cloud.google.com/run/docs/tutorials/pubsub)
- [Authenticate Push Subscriptions](https://docs.cloud.google.com/pubsub/docs/authenticate-push-subscriptions)
- [Service-to-Service Auth](https://docs.cloud.google.com/run/docs/authenticating/service-to-service)
- [Eventarc Cloud Storage](https://docs.cloud.google.com/run/docs/tutorials/eventarc)
- [CloudEvents SDK for Go](https://github.com/cloudevents/sdk-go)
- [Cloud Run IAM Roles](https://docs.cloud.google.com/run/docs/reference/iam/roles)
- [Managing Access](https://docs.cloud.google.com/run/docs/securing/managing-access)
- [Ingress Settings](https://docs.cloud.google.com/run/docs/securing/ingress)
- [VPC Connectors](https://docs.cloud.google.com/run/docs/configuring/vpc-connectors)
- [Configure Secrets](https://docs.cloud.google.com/run/docs/configuring/services/secrets)
- [Environment Variables](https://docs.cloud.google.com/run/docs/configuring/services/environment-variables)
- [Source Deploy](https://docs.cloud.google.com/run/docs/deploying-source-code)
- [Rollouts and Rollbacks](https://docs.cloud.google.com/run/docs/rollouts-rollbacks-traffic-migration)
- [Structured Logging](https://docs.cloud.google.com/logging/docs/structured-logging)
- [Cloud Logging with slog](https://github.com/remko/cloudrun-slog)
- [Terraform google_cloud_run_v2_service](https://registry.terraform.io/providers/hashicorp/google/latest/docs/resources/cloud_run_v2_service)
