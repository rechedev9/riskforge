---
summary: "Cloud Pub/Sub Go client reference - publish, subscribe, exactly-once, DLQ, testing"
read_when: [event-driven architecture, Pub/Sub setup, message processing, async workflows]
---

# Google Cloud Pub/Sub -- Go Reference

Import path: `cloud.google.com/go/pubsub` (v1) or `cloud.google.com/go/pubsub/v2` (v2).

v2 renames `Topic` -> `Publisher`, `Subscription` -> `Subscriber`, and exposes
`TopicAdminClient` / `SubscriptionAdminClient` for CRUD. This document uses **v1**
names for broader compatibility and notes v2 equivalents where relevant.

---

## 1. Client Setup

### Creating the client

```go
import (
    "context"
    "cloud.google.com/go/pubsub"
)

ctx := context.Background()
client, err := pubsub.NewClient(ctx, "my-project-id")
if err != nil {
    return fmt.Errorf("pubsub.NewClient: %w", err)
}
defer client.Close()
```

With OpenTelemetry tracing (v2):

```go
import "cloud.google.com/go/pubsub/v2"

cfg := &pubsub.ClientConfig{EnableOpenTelemetryTracing: true}
client, err := pubsub.NewClientWithConfig(ctx, "my-project-id", cfg)
```

### Creating a topic

```go
topic, err := client.CreateTopic(ctx, "orders")
if err != nil {
    return fmt.Errorf("CreateTopic: %w", err)
}
```

With config (KMS, storage policy, retention):

```go
topic, err := client.CreateTopicWithConfig(ctx, "orders", &pubsub.TopicConfig{
    KMSKeyName: "projects/p/locations/global/keyRings/kr/cryptoKeys/ck",
    MessageStoragePolicy: pubsub.MessageStoragePolicy{
        AllowedPersistenceRegions: []string{"us-central1"},
    },
    RetentionDuration: 24 * time.Hour,
})
```

### Creating a subscription

```go
sub, err := client.CreateSubscription(ctx, "orders-worker", pubsub.SubscriptionConfig{
    Topic:       topic,
    AckDeadline: 30 * time.Second,
})
```

**v2 equivalents:** `client.Publisher("orders")`, `client.Subscriber("orders-worker")`.
Admin ops use `client.TopicAdminClient.CreateTopic(ctx, &pubsubpb.Topic{...})` and
`client.SubscriptionAdminClient.CreateSubscription(ctx, &pubsubpb.Subscription{...})`.

---

## 2. Publishing

### Basic publish with result

```go
topic := client.Topic("orders")
defer topic.Stop() // flush pending messages and release resources

result := topic.Publish(ctx, &pubsub.Message{
    Data: []byte(`{"order_id":"123"}`),
})

// Block until the server confirms (or context expires).
msgID, err := result.Get(ctx)
if err != nil {
    return fmt.Errorf("publish: %w", err)
}
fmt.Printf("published msg ID: %s\n", msgID)
```

### Message attributes

```go
result := topic.Publish(ctx, &pubsub.Message{
    Data: []byte(`{"order_id":"123"}`),
    Attributes: map[string]string{
        "event_type": "order.created",
        "version":    "1",
    },
})
```

### Ordering keys

Ordering keys guarantee FIFO within the same key. The publisher must
opt in, and you should use a regional endpoint for the ordering guarantee.

```go
import "google.golang.org/api/option"

client, err := pubsub.NewClient(ctx, projectID,
    option.WithEndpoint("us-central1-pubsub.googleapis.com:443"))

topic := client.Topic("orders")
topic.EnableMessageOrdering = true

result := topic.Publish(ctx, &pubsub.Message{
    Data:        []byte(`{"order_id":"123"}`),
    OrderingKey: "customer-456",
})
```

If a publish with an ordering key fails, subsequent publishes for that key
are paused. Resume with:

```go
topic.ResumePublish("customer-456")
```

### Batch settings

A batch is flushed when **any** threshold is hit first.

```go
topic := client.Topic("orders")
topic.PublishSettings = pubsub.PublishSettings{
    DelayThreshold: 50 * time.Millisecond, // max wait before flush  (default 10ms)
    CountThreshold: 200,                   // max messages per batch (default 100)
    ByteThreshold:  5_000_000,             // max bytes per batch    (default 1MB)
    Timeout:        60 * time.Second,      // publish RPC timeout    (default 60s)
    // Flow control for the publisher buffer:
    FlowControlSettings: pubsub.FlowControlSettings{
        MaxOutstandingMessages: 2000,
        MaxOutstandingBytes:    50_000_000,
        LimitExceededBehavior:  pubsub.FlowControlBlock, // block until space available
    },
}
```

Hard limits: max 1000 messages or 10 MB per publish RPC.

### Concurrent publish pattern

```go
var wg sync.WaitGroup
var failures uint64

for i, payload := range payloads {
    res := topic.Publish(ctx, &pubsub.Message{Data: payload})
    wg.Add(1)
    go func(i int, r *pubsub.PublishResult) {
        defer wg.Done()
        if _, err := r.Get(ctx); err != nil {
            log.Printf("publish %d failed: %v", i, err)
            atomic.AddUint64(&failures, 1)
        }
    }(i, res)
}
wg.Wait()
```

---

## 3. Subscribing

### Receive with callback (StreamingPull)

This is the recommended approach. `Receive` blocks until the context is
cancelled or an unrecoverable error occurs.

```go
sub := client.Subscription("orders-worker")

err := sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
    if err := processOrder(msg.Data); err != nil {
        log.Printf("processing failed: %v", err)
        msg.Nack() // redelivered after ack deadline
        return
    }
    msg.Ack()
})
if err != nil {
    return fmt.Errorf("sub.Receive: %w", err)
}
```

### Pull subscription config

```go
sub, err := client.CreateSubscription(ctx, "orders-worker", pubsub.SubscriptionConfig{
    Topic:                 topic,
    AckDeadline:           30 * time.Second,
    EnableMessageOrdering: true,
    RetainAckedMessages:   false,
    RetentionDuration:     7 * 24 * time.Hour,
    ExpirationPolicy:      25 * time.Hour, // 0 = never expires
    Filter:                `attributes.event_type = "order.created"`,
    Labels: map[string]string{
        "team": "payments",
    },
})
```

### Push subscription (Cloud Run integration)

Push subscriptions deliver messages as HTTP POST to an endpoint. Typically
configured via Terraform or gcloud; here is the Go admin API:

```go
sub, err := client.CreateSubscription(ctx, "orders-push", pubsub.SubscriptionConfig{
    Topic:       topic,
    AckDeadline: 60 * time.Second,
    PushConfig: pubsub.PushConfig{
        Endpoint: "https://orders-service-abc123.run.app/pubsub",
        AuthenticationMethod: &pubsub.OIDCToken{
            ServiceAccountEmail: "invoker@my-project.iam.gserviceaccount.com",
            Audience:            "https://orders-service-abc123.run.app",
        },
    },
})
```

Push response codes that count as Ack: `102, 200, 201, 202, 204`.
Anything else triggers redelivery with exponential backoff (100ms - 60s).

Cloud Run handler skeleton:

```go
func handlePubSubPush(w http.ResponseWriter, r *http.Request) {
    var envelope struct {
        Message struct {
            Data        []byte            `json:"data"`
            ID          string            `json:"messageId"`
            Attributes  map[string]string `json:"attributes"`
            PublishTime time.Time         `json:"publishTime"`
        } `json:"message"`
        Subscription string `json:"subscription"`
    }

    if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
        http.Error(w, "bad request", http.StatusBadRequest)
        return // 400 -> nack -> redelivery
    }

    if err := processOrder(envelope.Message.Data); err != nil {
        http.Error(w, "processing failed", http.StatusInternalServerError)
        return // 500 -> nack -> redelivery
    }

    w.WriteHeader(http.StatusOK) // 200 -> ack
}
```

---

## 4. Exactly-Once Delivery

Guarantees: after a successful ack, the message will **not** be redelivered.
Available only on **pull** subscriptions. Requires connecting to a
**regional endpoint** in the same region as the subscription.

### Enable on subscription creation

```go
sub, err := client.CreateSubscription(ctx, "orders-exactly-once", pubsub.SubscriptionConfig{
    Topic:                     topic,
    AckDeadline:               60 * time.Second,
    EnableExactlyOnceDelivery: true,
    EnableMessageOrdering:     true, // often combined
})
```

### AckWithResult / NackWithResult

```go
import "google.golang.org/api/option"

// Connect to regional endpoint (required for exactly-once guarantee).
client, err := pubsub.NewClient(ctx, projectID,
    option.WithEndpoint("us-central1-pubsub.googleapis.com:443"))

sub := client.Subscription("orders-exactly-once")

// Increase lease extension to prevent ack ID expiry from network hiccups.
sub.ReceiveSettings.MinExtensionPeriod = 600 * time.Second

err = sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
    if err := processOrder(msg.Data); err != nil {
        r := msg.NackWithResult()
        status, err := r.Get(ctx)
        if err != nil {
            log.Printf("nack failed for %s: %v", msg.ID, err)
        }
        return
    }

    r := msg.AckWithResult()
    status, err := r.Get(ctx)
    if err != nil {
        log.Printf("ack RPC error for %s: %v", msg.ID, err)
        return
    }

    switch status {
    case pubsub.AcknowledgeStatusSuccess:
        // Safe: message will NOT be redelivered.
    case pubsub.AcknowledgeStatusInvalidAckID:
        // Ack ID expired; message may be redelivered.
        log.Printf("invalid ack ID for %s -- will be redelivered", msg.ID)
    case pubsub.AcknowledgeStatusPermissionDenied:
        log.Printf("permission denied acking %s", msg.ID)
    case pubsub.AcknowledgeStatusFailedPrecondition:
        log.Printf("failed precondition acking %s", msg.ID)
    case pubsub.AcknowledgeStatusOther:
        log.Printf("unknown ack error for %s", msg.ID)
    }
})
```

### Limitations

| Constraint | Detail |
|---|---|
| Pull only | Push and export subscriptions are not supported. |
| Regional endpoint required | Client must connect to the same region as the subscription. |
| Throughput with ordering | ~1,000 msg/s per ordering key when combined with exactly-once. |
| Higher latency | Publish-to-subscribe latency is significantly higher than standard. |
| Publish-side duplicates | The publisher may still produce duplicates; dedup is on the subscribe side only. |
| Client library versions | Go >= v1.25.1 (v1) required. |

---

## 5. Dead Letter Queues

When a message exceeds `MaxDeliveryAttempts`, Pub/Sub forwards it to the
dead letter topic (DLT). Useful for isolating poison messages.

### Setup

```go
// 1. Create DLT and its subscription.
dlt, err := client.CreateTopic(ctx, "orders-dlq")
_, err = client.CreateSubscription(ctx, "orders-dlq-monitor", pubsub.SubscriptionConfig{
    Topic: dlt,
})

// 2. Create primary subscription with dead letter policy.
sub, err := client.CreateSubscription(ctx, "orders-worker", pubsub.SubscriptionConfig{
    Topic:       topic,
    AckDeadline: 30 * time.Second,
    DeadLetterPolicy: &pubsub.DeadLetterPolicy{
        DeadLetterTopic:     "projects/my-project/topics/orders-dlq",
        MaxDeliveryAttempts: 10, // range: 5-100, default 5
    },
    RetryPolicy: &pubsub.RetryPolicy{
        MinimumBackoff: 10 * time.Second,
        MaximumBackoff: 600 * time.Second,
    },
})
```

### IAM permissions

The Pub/Sub service account
`service-PROJECT_NUMBER@gcp-sa-pubsub.iam.gserviceaccount.com` needs:

- **Publisher** role on the dead letter topic.
- **Subscriber** role on the source subscription.

### Forwarded message attributes

Messages sent to the DLT get these attributes added automatically:

| Attribute | Description |
|---|---|
| `CloudPubSubDeadLetterSourceDeliveryCount` | Number of delivery attempts |
| `CloudPubSubDeadLetterSourceSubscription` | Source subscription name |
| `CloudPubSubDeadLetterSourceSubscriptionProject` | Source project |
| `CloudPubSubDeadLetterSourceTopicPublishTime` | Original publish timestamp |

### Reading delivery attempts

```go
err = sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
    if msg.DeliveryAttempt != nil {
        log.Printf("delivery attempt #%d for msg %s", *msg.DeliveryAttempt, msg.ID)
    }
    // ...
})
```

---

## 6. Retry Policy

The retry policy controls exponential backoff between redeliveries after a
Nack or ack deadline expiry.

```go
sub, err := client.CreateSubscription(ctx, "orders-worker", pubsub.SubscriptionConfig{
    Topic:       topic,
    AckDeadline: 30 * time.Second,
    RetryPolicy: &pubsub.RetryPolicy{
        MinimumBackoff: 10 * time.Second,  // initial delay after nack (default 10s)
        MaximumBackoff: 600 * time.Second, // max delay cap (default 600s, i.e. 10min)
    },
})
```

### How it works

1. Subscriber calls `msg.Nack()` or the ack deadline expires.
2. Pub/Sub waits `MinimumBackoff` before the first retry.
3. Each subsequent retry doubles the wait (exponential backoff).
4. Delay is capped at `MaximumBackoff`.
5. If `DeadLetterPolicy` is also set, the message goes to DLT after
   `MaxDeliveryAttempts`.

### Updating retry policy on existing subscription

```go
sub := client.Subscription("orders-worker")
cfg, err := sub.Update(ctx, pubsub.SubscriptionConfigToUpdate{
    RetryPolicy: &pubsub.RetryPolicy{
        MinimumBackoff: 20 * time.Second,
        MaximumBackoff: 300 * time.Second,
    },
})
```

---

## 7. Error Handling

### Transient vs permanent errors

The core decision: should the message be retried or routed elsewhere?

```go
err = sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
    err := processOrder(msg.Data)
    if err == nil {
        msg.Ack()
        return
    }

    if isTransient(err) {
        // Nack -> Pub/Sub redelivers with backoff per RetryPolicy.
        log.Printf("transient error, nacking msg %s: %v", msg.ID, err)
        msg.Nack()
        return
    }

    // Permanent failure: ack to stop redelivery, publish to error topic.
    log.Printf("permanent error for msg %s: %v", msg.ID, err)
    errMsg := &pubsub.Message{
        Data: msg.Data,
        Attributes: map[string]string{
            "original_msg_id": msg.ID,
            "error":           err.Error(),
            "event_type":      msg.Attributes["event_type"],
        },
    }
    if _, pubErr := errorTopic.Publish(ctx, errMsg).Get(ctx); pubErr != nil {
        // Cannot route to error topic -- nack to retry the whole flow.
        log.Printf("failed to publish to error topic: %v", pubErr)
        msg.Nack()
        return
    }
    msg.Ack() // remove from source subscription
})
```

### Classifying errors

```go
func isTransient(err error) bool {
    // Network timeouts, temporary unavailability, rate limiting.
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
        return true
    }
    // gRPC status codes that are retryable.
    code := status.Code(err)
    switch code {
    case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.ResourceExhausted:
        return true
    }
    return false
}
```

### Strategy comparison

| Scenario | Action |
|---|---|
| Transient error (timeout, 503) | `msg.Nack()` -- Pub/Sub retries with backoff |
| Permanent error (validation, bad data) | `msg.Ack()` + publish to error/DLQ topic |
| Unknown error | `msg.Nack()` -- let DeadLetterPolicy catch it after N attempts |
| Processing takes too long | Increase `AckDeadline` or `MaxExtension` |

---

## 8. Flow Control

Flow control prevents the subscriber from being overwhelmed. Configure via
`ReceiveSettings` on the subscription object.

```go
sub := client.Subscription("orders-worker")

sub.ReceiveSettings = pubsub.ReceiveSettings{
    // Max unprocessed messages buffered in memory.
    MaxOutstandingMessages: 100,   // default 1000

    // Max unprocessed bytes buffered in memory.
    MaxOutstandingBytes: 100e6,    // 100 MB (default 1 GB)

    // Number of goroutines for StreamingPull RPCs.
    NumGoroutines: 4,              // default 10

    // Max time the client auto-extends the ack deadline.
    MaxExtension: 30 * time.Minute, // default 60 min

    // Min time for each ack deadline extension (exactly-once tuning).
    MinExtensionPeriod: 0,          // default 0
}

err := sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
    fmt.Printf("got: %s\n", msg.Data)
    msg.Ack()
})
```

### Ack deadline tuning

The `AckDeadline` (set at subscription creation, 10-600s) is the initial
window. The client library **automatically extends** the deadline up to
`MaxExtension`. If processing genuinely takes longer, increase
`MaxExtension` or redesign the handler (offload heavy work).

### Publisher flow control

```go
topic := client.Topic("orders")
topic.PublishSettings.FlowControlSettings = pubsub.FlowControlSettings{
    MaxOutstandingMessages: 500,
    MaxOutstandingBytes:    25_000_000,
    LimitExceededBehavior:  pubsub.FlowControlBlock,
    // Other options: FlowControlIgnore (default), FlowControlSignalError
}
```

### Defaults summary

| Setting | Publisher default | Subscriber default |
|---|---|---|
| MaxOutstandingMessages | 1000 | 1000 |
| MaxOutstandingBytes | disabled (-1) | 1 GB |
| LimitExceededBehavior | FlowControlIgnore | -- |
| NumGoroutines | -- | 10 |
| MaxExtension | -- | 60 min |

---

## 9. Testing

### Pub/Sub emulator with testcontainers-go

The `testcontainers-go` gcloud module provides a ready-to-use Pub/Sub
emulator container.

```go
import (
    "context"
    "testing"

    "cloud.google.com/go/pubsub"
    tcpubsub "github.com/testcontainers/testcontainers-go/modules/gcloud/pubsub"
    "github.com/testcontainers/testcontainers-go"
    "google.golang.org/api/option"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func TestOrderProcessing(t *testing.T) {
    ctx := context.Background()

    // Start emulator container.
    container, err := tcpubsub.Run(ctx,
        "gcr.io/google.com/cloudsdktool/cloud-sdk:367.0.0-emulators",
        tcpubsub.WithProjectID("test-project"),
    )
    if err != nil {
        t.Fatal(err)
    }
    defer testcontainers.TerminateContainer(container)

    // Connect via gRPC (no TLS for emulator).
    conn, err := grpc.NewClient(
        container.URI(),
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        t.Fatal(err)
    }
    defer conn.Close()

    client, err := pubsub.NewClient(ctx, container.ProjectID(),
        option.WithGRPCConn(conn),
    )
    if err != nil {
        t.Fatal(err)
    }
    defer client.Close()

    // Create topic + subscription.
    topic, err := client.CreateTopic(ctx, "orders")
    if err != nil {
        t.Fatal(err)
    }
    sub, err := client.CreateSubscription(ctx, "orders-test", pubsub.SubscriptionConfig{
        Topic: topic,
    })
    if err != nil {
        t.Fatal(err)
    }

    // Publish.
    res := topic.Publish(ctx, &pubsub.Message{
        Data: []byte(`{"order_id":"abc"}`),
    })
    if _, err := res.Get(ctx); err != nil {
        t.Fatalf("publish: %v", err)
    }

    // Receive.
    cctx, cancel := context.WithCancel(ctx)
    var got []byte
    err = sub.Receive(cctx, func(_ context.Context, m *pubsub.Message) {
        got = m.Data
        m.Ack()
        cancel()
    })
    if err != nil {
        t.Fatal(err)
    }
    if string(got) != `{"order_id":"abc"}` {
        t.Fatalf("unexpected message: %s", got)
    }
}
```

### Using PUBSUB_EMULATOR_HOST directly

The Go client library auto-detects `PUBSUB_EMULATOR_HOST`. No code changes
needed -- just set the env var before running tests.

```bash
# Start emulator (default port 8085).
gcloud beta emulators pubsub start --project=test-project --host-port=127.0.0.1:8085

# In another terminal:
export PUBSUB_EMULATOR_HOST=127.0.0.1:8085
go test ./...
```

With the env var set, `pubsub.NewClient` connects to the emulator instead of
the real service. No credentials are needed.

### Emulator limitations

- No IAM enforcement.
- `UpdateTopic` and `UpdateSnapshot` RPCs not supported.
- Messages retained indefinitely (no configurable retention).
- Subscriptions never expire.
- No BigQuery/Cloud Storage export.
- Protocol buffer schemas not supported.

---

## 10. Terraform

### Topic

```hcl
resource "google_pubsub_topic" "orders" {
  name    = "orders"
  project = var.project_id

  labels = {
    team        = "payments"
    environment = "production"
  }

  message_retention_duration = "86400s" # 24h

  message_storage_policy {
    allowed_persistence_regions = ["us-central1"]
  }
}

# Dead letter topic
resource "google_pubsub_topic" "orders_dlq" {
  name    = "orders-dlq"
  project = var.project_id
}
```

### Pull subscription with dead letter + retry

```hcl
resource "google_pubsub_subscription" "orders_worker" {
  name    = "orders-worker"
  project = var.project_id
  topic   = google_pubsub_topic.orders.id

  ack_deadline_seconds       = 30
  enable_message_ordering    = true
  enable_exactly_once_delivery = false
  filter                     = "attributes.event_type = \"order.created\""

  expiration_policy {
    ttl = "" # never expires
  }

  retry_policy {
    minimum_backoff = "10s"
    maximum_backoff = "600s"
  }

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.orders_dlq.id
    max_delivery_attempts = 10
  }
}
```

### Push subscription with OIDC (Cloud Run)

```hcl
resource "google_pubsub_subscription" "orders_push" {
  name    = "orders-push"
  project = var.project_id
  topic   = google_pubsub_topic.orders.id

  ack_deadline_seconds = 60

  push_config {
    push_endpoint = google_cloud_run_v2_service.orders.uri

    oidc_token {
      service_account_email = google_service_account.invoker.email
      audience              = google_cloud_run_v2_service.orders.uri
    }
  }

  retry_policy {
    minimum_backoff = "10s"
    maximum_backoff = "600s"
  }

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.orders_dlq.id
    max_delivery_attempts = 10
  }
}
```

### DLQ subscription

```hcl
resource "google_pubsub_subscription" "orders_dlq_monitor" {
  name    = "orders-dlq-monitor"
  project = var.project_id
  topic   = google_pubsub_topic.orders_dlq.id

  ack_deadline_seconds = 60

  expiration_policy {
    ttl = "" # never expires
  }
}
```

### IAM for dead letter forwarding

```hcl
# Pub/Sub service account needs publisher on DLT and subscriber on source sub.
resource "google_pubsub_topic_iam_member" "dlq_publisher" {
  topic   = google_pubsub_topic.orders_dlq.id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:service-${data.google_project.current.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

resource "google_pubsub_subscription_iam_member" "source_subscriber" {
  subscription = google_pubsub_subscription.orders_worker.id
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:service-${data.google_project.current.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}
```
