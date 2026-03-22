---
summary: "Google Cloud Run reference for Go services"
read_when: [Cloud Run deployment, service configuration, scaling, IAM setup]
---

# Google Cloud Run -- Go Reference

## 1. Container Contract

### Port & Request Listening

The ingress container must listen on `0.0.0.0` on the port specified by the `PORT` env var (default `8080`). The container must begin listening within **4 minutes** of startup.

### Injected Environment Variables

| Variable | Description |
|---|---|
| `PORT` | Port to listen on (default 8080) |
| `K_SERVICE` | Service name |
| `K_REVISION` | Revision name |
| `K_CONFIGURATION` | Configuration name |

### Instance Lifecycle

1. **Startup** -- must listen within 4 minutes; pending requests queue for up to 3.5x average startup time or 10 seconds (whichever is greater).
2. **Processing** -- CPU allocated while handling requests (request-based billing) or always (instance-based billing).
3. **Idle** -- instances kept idle up to 15 minutes; CPU allocation depends on billing mode.
4. **Shutdown** -- `SIGTERM` sent with **10-second** grace period before `SIGKILL`.

### Request Timeout

- Default: **300 seconds** (5 minutes)
- Maximum: **3600 seconds** (60 minutes)
- On timeout: connection closed, HTTP 504 returned; container keeps running (may interfere with subsequent requests on the same instance).

```bash
gcloud run deploy SERVICE --image IMAGE_URL --timeout=300s
```

```yaml
spec:
  template:
    spec:
      containers:
      - image: IMAGE
      timeoutSeconds: 300
```

For timeouts >15 min, implement retries and idempotent handlers.

### CPU

| Setting | Values |
|---|---|
| Fractional | 0.08 - <1.0 vCPU (0.01 increments) |
| Standard | 1, 2, 4, 6, 8 vCPU |

CPU allocation modes:
- **Request-based** (default) -- CPU only during request processing. Fractional CPU requires this mode + concurrency=1 + gen1.
- **Instance-based** (always-allocated) -- CPU for entire instance lifetime. Required for background work.

#### Startup CPU Boost

Temporary CPU increase during startup + 10 seconds after:

| Base CPU | Boosted CPU |
|---|---|
| 0-1 | 2 |
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

### Memory

- Default: **512 MiB**
- Range: 128 MiB (gen1) / 512 MiB (gen2) to **32 GiB**
- Formula: `(Standing Memory) + (Memory per Request) x (Concurrency)`
- Filesystem is in-memory; written files consume the memory limit.

CPU-to-memory constraints:

| CPU | Max Memory |
|---|---|
| 1 vCPU | 4 GiB |
| 2 vCPU | 8 GiB |
| 4 vCPU | 16 GiB |
| 6 vCPU | 24 GiB |
| 8 vCPU | 32 GiB |

```bash
gcloud run deploy SERVICE --image IMAGE_URL --cpu=2 --memory=1Gi
```

```yaml
resources:
  limits:
    cpu: "2"
    memory: "1Gi"
```

### Concurrency

- Default: **80** concurrent requests per instance.
- Lower for CPU-heavy work (1 for serial processing); raise for I/O-bound work.
- Adjust memory proportionally when changing concurrency.

```bash
gcloud run services update SERVICE --concurrency 80
```

```yaml
spec:
  template:
    spec:
      containerConcurrency: 80
```

### Execution Environment

| | Gen 1 | Gen 2 |
|---|---|---|
| Sandbox | gVisor | Full Linux |
| Syscalls | Limited | Full compatibility |
| Network | Standard | Standard |
| Min memory | 128 MiB | 512 MiB |
| Jobs | Not available | Always gen2 |

### Probes

#### Startup Probe

Default TCP startup probe on new services:

```yaml
startupProbe:
  tcpSocket:
    port: 8080
  timeoutSeconds: 240
  periodSeconds: 240
  failureThreshold: 1
```

HTTP startup probe:

```yaml
startupProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 0
  timeoutSeconds: 1
  failureThreshold: 3
  periodSeconds: 10
```

```bash
gcloud run deploy SERVICE --image IMAGE_URL \
  --startup-probe httpGet.path=/healthz,httpGet.port=8080
```

#### Liveness Probe

On failure: container receives `SIGKILL`, requests get HTTP 503, new instance starts.

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 0
  timeoutSeconds: 1
  failureThreshold: 3
  periodSeconds: 10
```

```bash
gcloud run deploy SERVICE --image IMAGE_URL \
  --liveness-probe httpGet.path=/healthz,httpGet.port=8080,periodSeconds=10
```

Probes always have CPU allocated and are billed for CPU/memory but not per-request.

### Scaling: Min & Max Instances

```bash
# Min instances (service-level, preferred)
gcloud run services update SERVICE --min 1

# Max instances (revision default: 100)
gcloud run services update SERVICE --max 50
```

```yaml
# Service-level annotations
metadata:
  annotations:
    run.googleapis.com/minScale: '1'
    run.googleapis.com/maxScale: '50'

# Revision-level annotations
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/minScale: '1'
        autoscaling.knative.dev/maxScale: '50'
```

Terraform:

```hcl
resource "google_cloud_run_v2_service" "default" {
  template {
    scaling {
      min_instance_count = 1
      max_instance_count = 50
    }
  }
}
```

With traffic splitting, min/max instances are distributed proportionally across revisions.

---

## 2. Go on Cloud Run

### Graceful Shutdown with signal.NotifyContext

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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Start server in goroutine.
	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Block until SIGTERM/SIGINT.
	<-ctx.Done()
	slog.Info("shutdown signal received")

	// 8-second deadline (Cloud Run gives 10s after SIGTERM).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}
	slog.Info("server stopped")
}
```

### Structured Logging with slog

Cloud Run picks up JSON written to `stderr`/`stdout` and parses special fields automatically.

```go
package main

import (
	"log/slog"
	"net/http"
	"os"
)

func init() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
}
```

For request-correlated logging, extract the trace from the `X-Cloud-Trace-Context` header:

```go
func withTrace(r *http.Request, projectID string) slog.Attr {
	traceHeader := r.Header.Get("X-Cloud-Trace-Context")
	if traceHeader == "" {
		return slog.Attr{}
	}
	parts := strings.SplitN(traceHeader, "/", 2)
	if len(parts) == 0 {
		return slog.Attr{}
	}
	trace := fmt.Sprintf("projects/%s/traces/%s", projectID, parts[0])
	return slog.String("logging.googleapis.com/trace", trace)
}

// Usage in handler:
func handler(w http.ResponseWriter, r *http.Request) {
	slog.Info("processing request",
		withTrace(r, "my-project-id"),
		"method", r.Method,
		"path", r.URL.Path,
	)
}
```

Cloud Logging special JSON fields:
- `severity` -- maps to log severity (slog levels map naturally: INFO, WARN, ERROR)
- `message` -- display text
- `logging.googleapis.com/trace` -- enables log correlation with request traces

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

### Health Check Endpoint

```go
mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// Or with dependency checks:
mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
	if err := db.PingContext(r.Context()); err != nil {
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
})
```

### Dockerfile -- Multi-stage with Distroless

```dockerfile
# Build stage
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /app/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

Alternative with scratch (smaller, no shell/debug tools):

```dockerfile
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

Key points:
- `CGO_ENABLED=0` for static binary (required for scratch/distroless).
- `-ldflags="-s -w"` strips debug info, reduces binary size.
- Copy CA certs when using scratch (needed for HTTPS calls).
- Use `nonroot` distroless tag to avoid running as root.
- Do not include `~/.config/gcloud/gce` in the image.

### .dockerignore

```
.git
*.md
vendor/
```

---

## 3. Cloud Run + Pub/Sub

### Push Subscription Setup

Pub/Sub delivers messages as HTTP POST requests to the Cloud Run service URL.

```bash
# Create topic
gcloud pubsub topics create my-topic

# Create service account for invocation
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
    "data": "<base64-encoded-payload>",
    "attributes": { "key": "value" },
    "messageId": "123456789",
    "publishTime": "2024-01-01T00:00:00Z"
  },
  "subscription": "projects/PROJECT_ID/subscriptions/my-sub"
}
```

### Go Handler

```go
type PubSubMessage struct {
	Message struct {
		Data       []byte            `json:"data,omitempty"`
		Attributes map[string]string `json:"attributes,omitempty"`
		ID         string            `json:"messageId"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

func pubsubHandler(w http.ResponseWriter, r *http.Request) {
	var msg PubSubMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		slog.Error("decode failed", "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	data := string(msg.Message.Data) // already decoded from base64 by json.Unmarshal
	slog.Info("received message", "id", msg.Message.ID, "data", data)

	// Process message...

	w.WriteHeader(http.StatusOK) // 200/204 = ack
}
```

### Ack/Nack Behavior

| HTTP Response | Pub/Sub Action |
|---|---|
| 200, 201, 202, 204 | Acknowledge (message consumed) |
| 102, 4xx, 5xx | Nack (message retried) |

- Ack deadline default in example: 600 seconds.
- Messages not acknowledged within the deadline are redelivered.
- Unhandled errors or container crashes cause redelivery.
- Implement idempotent handlers to handle redeliveries safely.

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

## 4. Cloud Run + Eventarc

Eventarc delivers events in **CloudEvents** format via HTTP POST. Two editions exist: Standard (simple trigger-based) and Advanced (many-to-many with transformation).

### Direct Cloud Storage Events

Four event types:

| Event Type | Trigger |
|---|---|
| `google.cloud.storage.object.v1.finalized` | Object created or overwritten |
| `google.cloud.storage.object.v1.deleted` | Object soft-deleted |
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
gcloud eventarc triggers create gcs-trigger \
  --location=us-central1 \
  --destination-run-service=my-service \
  --destination-run-region=us-central1 \
  --event-filters="type=google.cloud.storage.object.v1.finalized" \
  --event-filters="bucket=my-bucket" \
  --service-account=eventarc-sa@PROJECT_ID.iam.gserviceaccount.com
```

Constraints:
- Bucket must be in same project and region as the trigger.
- Max 10 notifications per bucket.
- Trigger event filters cannot be changed after creation.
- Propagation takes up to 2 minutes.

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
  --service-account=eventarc-sa@PROJECT_ID.iam.gserviceaccount.com
```

All three filters (`type`, `serviceName`, `methodName`) are mandatory. Optional resource filtering with path patterns:

```bash
--event-filters-path-pattern="resourceName=projects/_/buckets/**/r*.txt"
```

### Pub/Sub Custom Events

Any Pub/Sub topic can trigger a Cloud Run service via Eventarc:

```bash
gcloud eventarc triggers create pubsub-trigger \
  --location=us-central1 \
  --destination-run-service=my-service \
  --destination-run-region=us-central1 \
  --event-filters="type=google.cloud.pubsub.topic.v1.messagePublished" \
  --transport-topic=projects/PROJECT_ID/topics/my-topic \
  --service-account=eventarc-sa@PROJECT_ID.iam.gserviceaccount.com
```

### Go CloudEvents Handler

```go
import (
	cloudevents "github.com/cloudevents/sdk-go/v2"
)

type StorageObjectData struct {
	Bucket string `json:"bucket"`
	Name   string `json:"name"`
	Size   string `json:"size"`
}

func cloudEventHandler(w http.ResponseWriter, r *http.Request) {
	event, err := cloudevents.NewEventFromHTTPRequest(r)
	if err != nil {
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}

	slog.Info("received event",
		"type", event.Type(),
		"source", event.Source(),
		"id", event.ID(),
	)

	var data StorageObjectData
	if err := event.DataAs(&data); err != nil {
		http.Error(w, "data parse error", http.StatusBadRequest)
		return
	}

	slog.Info("storage event", "bucket", data.Bucket, "object", data.Name)
	w.WriteHeader(http.StatusOK)
}
```

---

## 5. IAM & Security

### Service Identity

Two identities in play:
- **Deployer** -- user or SA that calls Cloud Run Admin API.
- **Runtime service identity** -- SA used when the container calls GCP APIs.

Always use a dedicated **user-managed service account** (not the Compute Engine default SA).

```bash
# Create dedicated SA
gcloud iam service-accounts create my-service-sa \
  --display-name "My Service Runtime SA"

# Deploy with custom SA
gcloud run deploy my-service --image IMAGE_URL \
  --service-account=my-service-sa@PROJECT_ID.iam.gserviceaccount.com
```

### Invoker IAM Binding

Control who/what can call the service:

```bash
# Allow a specific SA
gcloud run services add-iam-policy-binding my-service \
  --member=serviceAccount:caller@PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/run.invoker

# Allow all authenticated users
gcloud run services add-iam-policy-binding my-service \
  --member=allUsers \
  --role=roles/run.invoker

# Allow all authenticated Google accounts
gcloud run services add-iam-policy-binding my-service \
  --member=allAuthenticatedUsers \
  --role=roles/run.invoker
```

### Service-to-Service Authentication

The calling service fetches an ID token from the metadata server and sends it as a Bearer token:

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

### Key IAM Roles

| Role | Purpose |
|---|---|
| `roles/run.invoker` | Invoke a Cloud Run service |
| `roles/run.developer` | Deploy and manage services |
| `roles/run.admin` | Full admin (deploy, IAM, config) |
| `roles/iam.serviceAccountUser` | Act as a service account |
| `roles/secretmanager.secretAccessor` | Access secrets from Secret Manager |

### Ingress Settings

| Setting | Allows |
|---|---|
| `all` | All traffic including direct internet |
| `internal` | VPC, Shared VPC, GCP services (Scheduler, Tasks, Pub/Sub, Eventarc) only |
| `internal-and-cloud-load-balancing` | Internal + external Application Load Balancer |

```bash
gcloud run deploy SERVICE --image IMAGE_URL --ingress internal

gcloud run services update SERVICE --ingress internal-and-cloud-load-balancing
```

```yaml
metadata:
  annotations:
    run.googleapis.com/ingress: internal
```

Use `internal-and-cloud-load-balancing` to enable Cloud Armor, IAP, and CDN while blocking direct `run.app` URL access.

### VPC Connectors

Enable private network egress from Cloud Run to VPC resources:

```bash
# Create connector (requires /28 subnet)
gcloud compute networks vpc-access connectors create my-connector \
  --region us-central1 \
  --subnet my-subnet \
  --min-instances 2 \
  --max-instances 10 \
  --machine-type e2-micro

# Attach to service
gcloud run deploy SERVICE --image IMAGE_URL \
  --vpc-connector my-connector \
  --vpc-egress private-ranges-only  # or all-traffic
```

```yaml
spec:
  template:
    metadata:
      annotations:
        run.googleapis.com/vpc-access-connector: my-connector
        run.googleapis.com/vpc-access-egress: private-ranges-only
```

Egress options:
- `private-ranges-only` (default) -- only RFC 1918/6598 traffic goes through VPC.
- `all-traffic` -- all egress through VPC (requires NAT gateway for internet).

---

## 6. Scaling Behavior

### Cold Starts

A cold start occurs when a new instance must be created to handle a request. Sequence:
1. Container image pulled (mitigated by image streaming).
2. Container started.
3. Startup probe checked (if configured).
4. Request delivered.

Mitigation strategies:
- Set `--min 1` to keep warm instances.
- Enable `--cpu-boost` for faster startup.
- Use small container images (distroless/scratch).
- Initialize expensive resources lazily with `sync.Once`.
- Avoid crashing -- a crash queues traffic while replacement starts.

### Request Buffering

When all instances are at capacity:
- New instances are started.
- Requests queue for up to **3.5x average startup time** or **10 seconds** (whichever is greater).
- If no instance becomes available, HTTP 429 is returned.

### Concurrent Request Handling

- Default concurrency: 80 requests per instance.
- Cloud Run distributes requests evenly across instances.
- Min instances receive traffic preferentially before autoscaled instances.
- Max instances can be briefly exceeded during traffic spikes.

### Instance Idle Timeout

- Idle instances are kept for up to **15 minutes** before being shut down.
- Min instances are never shut down due to idle timeout.

### Connection Timeouts

- VPC connections: 10 minutes idle timeout.
- Internet connections: 20 minutes idle timeout.

---

## 7. Deployment

### gcloud Deploy

```bash
# From container image
gcloud run deploy my-service \
  --image us-docker.pkg.dev/PROJECT/REPO/IMAGE:TAG \
  --region us-central1 \
  --allow-unauthenticated

# From source (auto-builds with Cloud Build or Buildpacks)
gcloud run deploy my-service --source . --region us-central1

# Deploy without sending traffic (for gradual rollout)
gcloud run deploy my-service --image IMAGE_URL --no-traffic --tag canary
```

### Traffic Splitting

```bash
# Gradual rollout
gcloud run services update-traffic my-service --to-revisions LATEST=10

# Split between revisions
gcloud run services update-traffic my-service \
  --to-revisions my-service-v1=70,my-service-v2=30

# Rollback to previous revision
gcloud run services update-traffic my-service --to-revisions my-service-v1=100

# Send all traffic to latest
gcloud run services update-traffic my-service --to-latest
```

Tagged revisions get dedicated URLs: `https://canary---my-service-xxxxx.a.run.app`.

### Cloud Build Integration

```yaml
# cloudbuild.yaml
steps:
  - name: 'gcr.io/cloud-builders/docker'
    args: ['build', '-t', 'LOCATION-docker.pkg.dev/PROJECT_ID/REPO/IMAGE:$COMMIT_SHA', '.']
  - name: 'gcr.io/cloud-builders/docker'
    args: ['push', 'LOCATION-docker.pkg.dev/PROJECT_ID/REPO/IMAGE:$COMMIT_SHA']
  - name: 'gcr.io/google.com/cloudsdktool/cloud-sdk'
    entrypoint: gcloud
    args:
      - 'run'
      - 'deploy'
      - 'my-service'
      - '--image'
      - 'LOCATION-docker.pkg.dev/PROJECT_ID/REPO/IMAGE:$COMMIT_SHA'
      - '--region'
      - 'us-central1'
images:
  - 'LOCATION-docker.pkg.dev/PROJECT_ID/REPO/IMAGE:$COMMIT_SHA'
```

Cloud Build SA requires: `roles/run.admin`, `roles/iam.serviceAccountUser`, `roles/artifactregistry.writer`.

### Multi-Container (Sidecars)

Up to 10 containers per instance. Only the ingress container exposes a port; sidecars communicate via localhost.

```bash
gcloud run deploy my-service \
  --container ingress --image INGRESS_IMAGE --port 8080 \
  --container sidecar --image SIDECAR_IMAGE
```

### Disable Deployment Health Check

```bash
gcloud run deploy my-service --image IMAGE_URL --no-deploy-health-check
```

---

## 8. Environment Variables & Secrets

### Environment Variables

```bash
# Set
gcloud run deploy SERVICE --image IMAGE_URL \
  --set-env-vars KEY1=VALUE1,KEY2=VALUE2

# Update (non-destructive)
gcloud run services update SERVICE --update-env-vars KEY1=NEW_VALUE

# Remove
gcloud run services update SERVICE --remove-env-vars KEY1,KEY2

# Clear all
gcloud run services update SERVICE --clear-env-vars

# From file
gcloud run deploy SERVICE --image IMAGE_URL --env-vars-file=.env.yaml
```

```yaml
containers:
- image: IMAGE
  env:
  - name: APP_ENV
    value: production
  - name: LOG_LEVEL
    value: info
```

Reserved variables (cannot be set): `PORT`, `K_SERVICE`, `K_REVISION`, `K_CONFIGURATION`.

Invalid names: empty string, contains `=`, starts with `X_GOOGLE_`.

Limits: max 32 KB per variable, up to 1000 variables.

### Secret Manager Integration

Two methods:

#### As Environment Variable

Resolved at instance startup. Pin to a specific version (not `latest`).

```bash
gcloud run deploy SERVICE --image IMAGE_URL \
  --update-secrets=DB_PASSWORD=my-db-secret:3
```

```yaml
env:
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: my-db-secret
      key: "3"  # version number, or "latest"
```

#### As Volume Mount

Fetched at access time (always gets latest value). Supports automatic rotation.

```bash
gcloud run deploy SERVICE --image IMAGE_URL \
  --update-secrets=/secrets/db/password=my-db-secret:latest
```

```yaml
spec:
  template:
    spec:
      containers:
      - image: IMAGE
        volumeMounts:
        - mountPath: /secrets/db
          name: db-secret
      volumes:
      - name: db-secret
        secret:
          secretName: my-db-secret
          items:
          - key: latest
            path: password
```

#### Cross-Project Secrets

```bash
gcloud run deploy SERVICE --image IMAGE_URL \
  --update-secrets=/secrets/key=projects/123456/secrets/shared-secret:latest
```

#### Required IAM

The runtime SA needs `roles/secretmanager.secretAccessor` on each secret:

```bash
gcloud secrets add-iam-policy-binding my-db-secret \
  --member=serviceAccount:my-service-sa@PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/secretmanager.secretAccessor
```

#### Terraform

```hcl
resource "google_cloud_run_v2_service" "default" {
  template {
    containers {
      image = "IMAGE_URL"

      # As env var
      env {
        name = "DB_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.db.secret_id
            version = "latest"
          }
        }
      }

      # As volume mount
      volume_mounts {
        name       = "db-secret"
        mount_path = "/secrets/db"
      }
    }

    volumes {
      name = "db-secret"
      secret {
        secret = google_secret_manager_secret.db.secret_id
      }
    }
  }
}
```

Limitations:
- Cannot mount at `/dev`, `/proc`, `/sys`.
- Mounting at a path hides existing directory contents.
- Regional secrets not supported.
- Multiple secrets cannot mount to the same path.
