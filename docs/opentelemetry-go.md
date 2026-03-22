---
summary: "OpenTelemetry Go on GCP - tracing, metrics, Cloud Run, Spanner instrumentation"
read_when: [observability, tracing, metrics, monitoring, debugging latency]
---

# OpenTelemetry Go on GCP

## 1. Setup - TracerProvider and MeterProvider

### Direct Google Cloud Exporters

Use `opentelemetry-operations-go` to export directly to Cloud Trace and Cloud Monitoring
without an OTel Collector.

```go
package main

import (
	"context"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"

	gcpdetector "go.opentelemetry.io/contrib/detectors/gcp"
)

func setupOpenTelemetry(ctx context.Context) (func(context.Context) error, error) {
	// --- Resource ---
	res, err := resource.New(ctx,
		resource.WithDetectors(gcpdetector.NewDetector()),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName("my-service"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")

	// --- Trace exporter (Cloud Trace) ---
	traceExp, err := texporter.New(texporter.WithProjectID(projectID))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// --- Metric exporter (Cloud Monitoring) ---
	metricExp, err := mexporter.New(mexporter.WithProjectID(projectID))
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)
	otel.SetMeterProvider(mp)

	// --- Shutdown ---
	shutdown := func(ctx context.Context) error {
		tpErr := tp.Shutdown(ctx)
		mpErr := mp.Shutdown(ctx)
		if tpErr != nil {
			return tpErr
		}
		return mpErr
	}
	return shutdown, nil
}
```

In GCP environments (Cloud Run, GKE, GCE), credentials are detected automatically via
`google.FindDefaultCredentials`. For local development, set `GOOGLE_APPLICATION_CREDENTIALS`
or run `gcloud auth application-default login`, then set `GOOGLE_CLOUD_PROJECT`.

### Using autoexport (env-driven, OTel Collector compatible)

If you send telemetry to an OTel Collector sidecar rather than directly to GCP, use
`autoexport` to configure exporters via `OTEL_EXPORTER_*` environment variables:

```go
import (
	"go.opentelemetry.io/contrib/exporters/autoexport"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/contrib/propagators/autoprop"
)

func setupWithAutoExport(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())

	spanExp, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spanExp))
	otel.SetTracerProvider(tp)

	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}
```

Set `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317` to point at a local collector.

---

## 2. Tracing - HTTP, gRPC, Custom Spans

### HTTP Middleware (otelhttp)

```go
import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/users", usersHandler)
	mux.HandleFunc("/api/health", healthHandler)

	// Wrap entire mux - creates a span per request
	handler := otelhttp.NewHandler(mux, "server",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)
	http.ListenAndServe(":8080", handler)
}

// Per-route wrapping with route tag (better span names)
func registerRoutes(mux *http.ServeMux) {
	routes := map[string]http.HandlerFunc{
		"/api/users":  usersHandler,
		"/api/orders": ordersHandler,
	}
	for route, handler := range routes {
		instrumentedHandler := otelhttp.NewHandler(
			otelhttp.WithRouteTag(route, handler), route,
		)
		mux.Handle(route, instrumentedHandler)
	}
}

// Instrumented HTTP client (outbound requests)
func newTracedHTTPClient() *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
}
```

### gRPC Interceptors (otelgrpc)

The interceptor-based API is deprecated. Use **stats handlers** instead:

```go
import (
	"google.golang.org/grpc"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

// Server
func newGRPCServer() *grpc.Server {
	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
}

// Client
func newGRPCClient(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
}

// Server with dynamic metric attributes from metadata
func newGRPCServerWithMetadata() *grpc.Server {
	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithMetricAttributesFn(func(ctx context.Context) []attribute.KeyValue {
				md, ok := metadata.FromIncomingContext(ctx)
				if !ok {
					return nil
				}
				if origins := md.Get("origin"); len(origins) > 0 {
					return []attribute.KeyValue{
						attribute.String("origin", origins[0]),
					}
				}
				return nil
			}),
		)),
	)
}
```

### Custom Spans and Attributes

```go
import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("my-service/orders")

func processOrder(ctx context.Context, orderID string) error {
	ctx, span := tracer.Start(ctx, "processOrder",
		trace.WithAttributes(
			attribute.String("order.id", orderID),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	// Add attributes after creation
	span.SetAttributes(attribute.Int("order.items", 5))

	// Add an event (annotation visible in Cloud Trace)
	span.AddEvent("payment.started", trace.WithAttributes(
		attribute.String("payment.method", "card"),
	))

	if err := chargePayment(ctx, orderID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func chargePayment(ctx context.Context, orderID string) error {
	// Child span - automatically linked via ctx
	ctx, span := tracer.Start(ctx, "chargePayment")
	defer span.End()
	// ...
	return nil
}
```

---

## 3. Metrics - Histograms, Counters, Gauges

### Custom Metrics with Cloud Monitoring Export

```go
import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("my-service/metrics")

func setupMetrics() error {
	// Counter - monotonically increasing (e.g., requests served)
	requestCounter, err := meter.Int64Counter("http.requests.total",
		metric.WithDescription("Total HTTP requests processed"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return err
	}

	// Histogram - distribution (e.g., latency)
	latencyHistogram, err := meter.Float64Histogram("http.request.duration",
		metric.WithDescription("HTTP request latency"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(
			1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000,
		),
	)
	if err != nil {
		return err
	}

	// UpDownCounter - value that can increase and decrease (e.g., active connections)
	activeConns, err := meter.Int64UpDownCounter("connections.active",
		metric.WithDescription("Number of active connections"),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return err
	}

	// Gauge - point-in-time value (e.g., queue depth)
	queueGauge, err := meter.Int64Gauge("queue.depth",
		metric.WithDescription("Current queue depth"),
		metric.WithUnit("{item}"),
	)
	if err != nil {
		return err
	}

	// --- Recording ---
	ctx := context.Background()
	attrs := metric.WithAttributes(
		attribute.String("method", "GET"),
		attribute.String("route", "/api/users"),
	)

	requestCounter.Add(ctx, 1, attrs)
	latencyHistogram.Record(ctx, 42.5, attrs)
	activeConns.Add(ctx, 1, attrs)   // increment
	activeConns.Add(ctx, -1, attrs)  // decrement
	queueGauge.Record(ctx, 17, attrs)

	return nil
}

// Observable (async) gauge - registered callback invoked at export time
func registerAsyncGauge() error {
	_, err := meter.Int64ObservableGauge("process.goroutines",
		metric.WithDescription("Number of goroutines"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(runtime.NumGoroutine()))
			return nil
		}),
	)
	return err
}
```

Cloud Monitoring notes:
- Metrics appear under `workload.googleapis.com/<metric-name>` by default (direct exporter)
  or `custom.googleapis.com/opentelemetry/<metric-name>` (via OTel Collector with
  `googlemanagedprometheus` exporter).
- Cloud Monitoring enforces a **minimum 10-second write interval** per time series.
  Set `PeriodicReader` interval to 30-60s in production.
- Use `mexporter.WithCreateServiceTimeSeries()` to avoid descriptor conflicts in shared
  projects.

---

## 4. Spanner Instrumentation

### Enable OpenTelemetry for Spanner Client

Set the environment variable **before** importing/loading the Spanner client library:

```bash
export GOOGLE_API_GO_EXPERIMENTAL_TELEMETRY_PLATFORM_TRACING=opentelemetry
```

### Spanner with OpenTelemetry Tracing

```go
import (
	"context"
	"os"

	"cloud.google.com/go/spanner"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
)

func main() {
	ctx := context.Background()

	// Ensure env var is set
	os.Setenv("GOOGLE_API_GO_EXPERIMENTAL_TELEMETRY_PLATFORM_TRACING", "opentelemetry")

	// Setup TracerProvider
	res, _ := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName("spanner-app"),
			semconv.ServiceVersion("0.1.0"),
		))

	traceExp, _ := texporter.New()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1)),
	)
	defer tp.Shutdown(ctx)
	otel.SetTracerProvider(tp)

	// Register W3C TraceContext propagator for end-to-end tracing
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create Spanner client with end-to-end tracing
	client, err := spanner.NewClientWithConfig(ctx,
		"projects/my-project/instances/my-instance/databases/my-db",
		spanner.ClientConfig{
			SessionPoolConfig:    spanner.DefaultSessionPoolConfig,
			EnableEndToEndTracing: true,
		},
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Queries now produce spans automatically
	iter := client.Single().Query(ctx, spanner.Statement{
		SQL: "SELECT SingerId, AlbumTitle FROM Albums",
	})
	defer iter.Stop()
	// ...
}
```

### Custom Spanner Query Metrics

```go
import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"cloud.google.com/go/spanner"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/api/iterator"
)

func captureQueryStatsMetric(
	ctx context.Context,
	mp *sdkmetric.MeterProvider,
	client *spanner.Client,
) error {
	meter := mp.Meter("my-service/spanner")
	queryStats, err := meter.Float64Histogram(
		"spanner/query_stats_elapsed",
		metric.WithDescription("Spanner query execution time"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(
			0, 0.01, 0.05, 0.1, 0.3, 0.6, 0.8, 1, 2, 3, 4, 5,
			6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100,
			130, 160, 200, 250, 300, 400, 500, 650, 800, 1000,
			2000, 5000, 10000, 20000, 50000, 100000,
		),
	)
	if err != nil {
		return err
	}

	stmt := spanner.Statement{SQL: "SELECT SingerId, AlbumId, AlbumTitle FROM Albums"}
	iter := client.Single().QueryWithStats(ctx, stmt)
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			// Extract elapsed time from query stats
			elapsed := iter.QueryStats["elapsed_time"].(string)
			ms, err := strconv.ParseFloat(strings.TrimSuffix(elapsed, " msecs"), 64)
			if err != nil {
				return err
			}
			queryStats.Record(ctx, ms)
			return nil
		}
		if err != nil {
			return err
		}
		var singerID, albumID int64
		var albumTitle string
		_ = row.Columns(&singerID, &albumID, &albumTitle)
	}
}
```

---

## 5. Cloud Run Integration

### Trace Context Propagation

Cloud Run injects the `X-Cloud-Trace-Context` header on incoming requests. OpenTelemetry's
W3C `traceparent` header is the standard. To support both, register a composite propagator:

```go
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
)

func init() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		// W3C Trace Context (traceparent / tracestate)
		propagation.TraceContext{},
		// Google Cloud Trace header (X-Cloud-Trace-Context)
		gcppropagator.CloudTraceFormatPropagator{},
		// W3C Baggage
		propagation.Baggage{},
	))
}
```

Cloud Trace header format:

```
X-Cloud-Trace-Context: TRACE_ID/SPAN_ID;o=TRACE_TRUE

  TRACE_ID:   32-char hex (128-bit)
  SPAN_ID:    decimal uint64
  TRACE_TRUE: 1 = sampled, 0 = not sampled
```

### GCP Resource Detection on Cloud Run

The GCP resource detector automatically populates `cloud.provider`, `cloud.platform`,
`faas.name`, `faas.version`, `faas.id`, and `cloud.region` attributes when running on
Cloud Run:

```go
import (
	"go.opentelemetry.io/otel/sdk/resource"
	gcpdetector "go.opentelemetry.io/contrib/detectors/gcp"
)

res, err := resource.New(ctx,
	resource.WithDetectors(gcpdetector.NewDetector()),
	resource.WithTelemetrySDK(),
	resource.WithFromEnv(),
)
```

### Full Cloud Run Server Example

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	gcpdetector "go.opentelemetry.io/contrib/detectors/gcp"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
)

func main() {
	ctx := context.Background()
	shutdown, err := setupOTel(ctx)
	if err != nil {
		slog.Error("otel setup failed", "error", err)
		os.Exit(1)
	}
	defer shutdown(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)

	handler := otelhttp.NewHandler(mux, "cloud-run-server")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	slog.Info("starting server", "port", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("server error", "error", err)
	}
}

func setupOTel(ctx context.Context) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithDetectors(gcpdetector.NewDetector()),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName("my-cloud-run-service")),
	)
	if err != nil {
		return nil, err
	}

	// Propagator: W3C + GCP Cloud Trace header
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		gcppropagator.CloudTraceFormatPropagator{},
		propagation.Baggage{},
	))

	// Traces
	texp, err := texporter.New()
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(texp),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
	)
	otel.SetTracerProvider(tp)

	// Metrics
	mexp, err := mexporter.New()
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp)),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}
```

---

## 6. OTel Collector Sidecar on Cloud Run

### Collector Configuration

Store as a Secret Manager secret, or bake into the collector container image.

```yaml
# collector-config.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: localhost:4317
      http:
        endpoint: localhost:4318

processors:
  batch:
    send_batch_max_size: 200
    send_batch_size: 200
    timeout: 5s

  memory_limiter:
    check_interval: 1s
    limit_percentage: 65
    spike_limit_percentage: 20

  resourcedetection:
    detectors: [gcp]
    timeout: 10s

  resource:
    attributes:
      - key: service.instance.id
        from_attribute: faas.id
        action: upsert
      - key: service.name
        value: ${env:K_SERVICE}
        action: insert

exporters:
  # Traces -> Cloud Trace
  googlecloud:
    log:
      default_log_name: opentelemetry-collector

  # Metrics -> Cloud Monitoring (via Managed Prometheus)
  googlemanagedprometheus:

extensions:
  health_check:
    endpoint: 0.0.0.0:13133

service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [resourcedetection, memory_limiter, batch]
      exporters: [googlecloud]
    metrics:
      receivers: [otlp]
      processors: [resourcedetection, memory_limiter, batch]
      exporters: [googlemanagedprometheus]
    logs:
      receivers: [otlp]
      processors: [resourcedetection, memory_limiter, batch]
      exporters: [googlecloud]
```

### Collector Dockerfile

```dockerfile
FROM us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google:0.144.0
COPY collector-config.yaml /etc/otelcol-google/config.yaml
```

### Cloud Run Multi-Container Service YAML

```yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: my-service
  annotations:
    run.googleapis.com/launch-stage: BETA
spec:
  template:
    metadata:
      annotations:
        # Collector must start before the app
        run.googleapis.com/container-dependencies: "{app:[collector]}"
    spec:
      containers:
        # --- Application container ---
        - image: REGION-docker.pkg.dev/PROJECT_ID/REPO/my-app:latest
          name: app
          ports:
            - containerPort: 8080
          env:
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://localhost:4317"

        # --- OTel Collector sidecar ---
        - image: REGION-docker.pkg.dev/PROJECT_ID/REPO/otel-collector:latest
          name: collector
          startupProbe:
            httpGet:
              path: /
              port: 13133
            timeoutSeconds: 30
            periodSeconds: 30
          livenessProbe:
            httpGet:
              path: /
              port: 13133
            timeoutSeconds: 30
            periodSeconds: 30
```

When using the collector sidecar, the app sends OTLP to `localhost:4317`. The Go app
setup simplifies to standard OTLP exporters:

```go
import (
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
)

func setupWithCollector(ctx context.Context) (*sdktrace.TracerProvider, *sdkmetric.MeterProvider, error) {
	// Trace exporter -> collector
	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(), // localhost, no TLS
	)
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
	)

	// Metric exporter -> collector
	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	return tp, mp, nil
}
```

IAM requirements for the Cloud Run service account:
- `roles/monitoring.metricWriter` (Cloud Monitoring)
- `roles/cloudtrace.agent` (Cloud Trace)
- `roles/logging.logWriter` (Cloud Logging, if exporting logs)

---

## 7. Structured Logging with slog - Trace Correlation

### Custom slog Handler for GCP Cloud Logging

Correlate logs with traces in Cloud Logging by injecting `logging.googleapis.com/trace`,
`logging.googleapis.com/spanId`, and `logging.googleapis.com/trace_sampled` fields.

```go
package logging

import (
	"context"
	"log/slog"
	"os"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// spanContextLogHandler wraps a slog.Handler and injects trace context
// attributes from the OpenTelemetry span in ctx.
type spanContextLogHandler struct {
	slog.Handler
	projectID string
}

func NewCloudLoggingHandler(projectID string) slog.Handler {
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if groups != nil {
				return a
			}
			switch a.Key {
			case slog.MessageKey:
				a.Key = "message"
			case slog.LevelKey:
				a.Key = "severity"
			case slog.SourceKey:
				a.Key = "logging.googleapis.com/sourceLocation"
			}
			return a
		},
	})
	return &spanContextLogHandler{
		Handler:   jsonHandler,
		projectID: projectID,
	}
}

func (h *spanContextLogHandler) Handle(ctx context.Context, rec slog.Record) error {
	if sc := oteltrace.SpanContextFromContext(ctx); sc.IsValid() {
		rec.AddAttrs(
			slog.String("logging.googleapis.com/trace",
				"projects/"+h.projectID+"/traces/"+sc.TraceID().String()),
			slog.String("logging.googleapis.com/spanId",
				sc.SpanID().String()),
			slog.Bool("logging.googleapis.com/trace_sampled",
				sc.TraceFlags().IsSampled()),
		)
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *spanContextLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &spanContextLogHandler{Handler: h.Handler.WithAttrs(attrs), projectID: h.projectID}
}

func (h *spanContextLogHandler) WithGroup(name string) slog.Handler {
	return &spanContextLogHandler{Handler: h.Handler.WithGroup(name), projectID: h.projectID}
}
```

### Wiring It Up

```go
func main() {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	slog.SetDefault(slog.New(logging.NewCloudLoggingHandler(projectID)))

	// ... setup OTel ...

	// Always use slog.*Context variants so trace context propagates
	slog.InfoContext(ctx, "processing order", "orderID", "abc-123")
}
```

### Usage in HTTP Handlers

```go
func ordersHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // otelhttp middleware already stores the span here

	slog.InfoContext(ctx, "handling request",
		"method", r.Method,
		"path", r.URL.Path,
	)

	// Any child spans created with this ctx will share the same trace
	result, err := processOrder(ctx, r.URL.Query().Get("id"))
	if err != nil {
		slog.ErrorContext(ctx, "order processing failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.InfoContext(ctx, "order processed", "result", result)
	w.WriteHeader(http.StatusOK)
}
```

The resulting JSON log line in Cloud Logging:

```json
{
  "severity": "INFO",
  "message": "handling request",
  "method": "GET",
  "path": "/api/orders",
  "logging.googleapis.com/trace": "projects/my-project/traces/abc123def456...",
  "logging.googleapis.com/spanId": "1234567890abcdef",
  "logging.googleapis.com/trace_sampled": true,
  "logging.googleapis.com/sourceLocation": { ... }
}
```

Cloud Logging automatically links these log entries to the corresponding trace in
Cloud Trace, visible via the "Show logs for this trace" button.

---

## Import Paths Reference

| Package | Import Path |
|---------|-------------|
| OTel API | `go.opentelemetry.io/otel` |
| Trace SDK | `go.opentelemetry.io/otel/sdk/trace` |
| Metric SDK | `go.opentelemetry.io/otel/sdk/metric` |
| Attributes | `go.opentelemetry.io/otel/attribute` |
| Propagation | `go.opentelemetry.io/otel/propagation` |
| Semconv | `go.opentelemetry.io/otel/semconv/v1.24.0` |
| Resource | `go.opentelemetry.io/otel/sdk/resource` |
| GCP Trace Exporter | `github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace` |
| GCP Metric Exporter | `github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric` |
| GCP Propagator | `github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator` |
| GCP Resource Detector | `go.opentelemetry.io/contrib/detectors/gcp` |
| otelhttp | `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` |
| otelgrpc | `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` |
| OTLP Trace (gRPC) | `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` |
| OTLP Metric (gRPC) | `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` |
| Auto Export | `go.opentelemetry.io/contrib/exporters/autoexport` |
| Auto Propagator | `go.opentelemetry.io/contrib/propagators/autoprop` |
| Spanner | `cloud.google.com/go/spanner` |

## Version Compatibility (as of early 2026)

- `opentelemetry-operations-go`: v1.31.0 / v0.55.0
- `go.opentelemetry.io/otel`: v1.28+
- `go.opentelemetry.io/contrib`: v0.67+
- Google-Built OTel Collector image: `otelcol-google:0.144.0`

## Sources

- [opentelemetry-operations-go GitHub](https://github.com/GoogleCloudPlatform/opentelemetry-operations-go)
- [Cloud Trace Go Setup](https://docs.cloud.google.com/trace/docs/setup/go-ot)
- [Spanner Tracing Setup](https://docs.cloud.google.com/spanner/docs/set-up-tracing)
- [Spanner Custom Metrics](https://docs.cloud.google.com/spanner/docs/capture-custom-metrics-opentelemetry)
- [OTel Collector Sidecar on Cloud Run](https://docs.cloud.google.com/run/docs/tutorials/custom-metrics-opentelemetry-sidecar)
- [Google-Built Collector on Cloud Run](https://docs.cloud.google.com/stackdriver/docs/instrumentation/opentelemetry-collector-cloud-run)
- [otelhttp pkg.go.dev](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp)
- [otelgrpc pkg.go.dev](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc)
- [GCP Metric Exporter pkg.go.dev](https://pkg.go.dev/github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric)
- [opentelemetry-cloud-run GitHub](https://github.com/GoogleCloudPlatform/opentelemetry-cloud-run)
- [GCP Resource Detector](https://pkg.go.dev/go.opentelemetry.io/contrib/detectors/gcp)
