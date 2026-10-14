package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// maxResponseBytes caps carrier response body reads to prevent OOM.
const maxResponseBytes = 10 << 20 // 10 MiB

// HTTPCarrierConfig configures the HTTP carrier client.
type HTTPCarrierConfig struct {
	// BaseURL is the carrier API root (e.g. "https://api.delta-insurance.com").
	// A trailing slash is removed when the carrier is constructed.
	BaseURL string
	// APIKey is sent as the X-API-Key request header.
	APIKey string
	// MaxRetries is the number of retry attempts after the initial failure.
	// Nil uses 3. A pointer to 0 disables retries.
	MaxRetries *int
	// RetryDelay is the base delay before the first retry. Each subsequent
	// retry doubles the delay (exponential backoff). Default: 100ms.
	RetryDelay time.Duration
	// Timeout is the per-attempt HTTP timeout. Default: 5s.
	Timeout time.Duration
}

func (c *HTTPCarrierConfig) withDefaults() HTTPCarrierConfig {
	out := *c
	out.BaseURL = strings.TrimRight(out.BaseURL, "/")
	retries := 3
	if out.MaxRetries != nil {
		retries = *out.MaxRetries
		if retries < 0 {
			retries = 0
		}
	}
	out.MaxRetries = &retries
	if out.RetryDelay <= 0 {
		out.RetryDelay = 100 * time.Millisecond
	}
	if out.Timeout <= 0 {
		out.Timeout = 5 * time.Second
	}
	return out
}

// HTTPCarrier is a generic carrier client that calls a JSON REST endpoint.
// It handles auth, per-attempt timeouts, and exponential-backoff retries.
// All methods are safe for concurrent use.
type HTTPCarrier struct {
	id     string
	cfg    HTTPCarrierConfig
	client *http.Client
	log    *slog.Logger
}

// NewHTTPCarrier constructs an HTTPCarrier with the given config and logger.
func NewHTTPCarrier(id string, cfg HTTPCarrierConfig, log *slog.Logger) *HTTPCarrier {
	resolved := cfg.withDefaults()
	return &HTTPCarrier{
		id:  id,
		cfg: resolved,
		// Each attempt uses its own context with a per-attempt timeout, so
		// the shared client has no global timeout — that is intentional.
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		log: log,
	}
}

// Post sends a JSON-encoded body to path (relative to BaseURL) and decodes
// the JSON response into out. It retries on network errors and HTTP 5xx
// responses using exponential backoff. 4xx responses are not retried.
//
// ctx cancellation is honoured across all attempts.
func (h *HTTPCarrier) Post(ctx context.Context, path string, body, out any) error {
	url := h.cfg.BaseURL + "/" + strings.TrimLeft(path, "/")

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("http_carrier %s: marshal request: %w", h.id, err)
	}

	delay := h.cfg.RetryDelay
	lastErr := error(nil)

	maxRetries := *h.cfg.MaxRetries
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("http_carrier %s: context cancelled before retry %d: %w", h.id, attempt, ctx.Err())
			case <-timer.C:
				delay *= 2
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, h.cfg.Timeout)
		err := h.doOnce(attemptCtx, url, payload, out)
		cancel()

		if err == nil {
			return nil
		}

		// Do not retry on 4xx — these indicate a client-side problem that
		// won't be resolved by retrying (bad request, auth failure, etc.).
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) && statusErr.status >= 400 && statusErr.status < 500 {
			return fmt.Errorf("http_carrier %s: non-retryable status %d: %w", h.id, statusErr.status, err)
		}

		lastErr = err
		h.log.Warn("http carrier attempt failed, will retry",
			slog.String("carrier_id", h.id),
			slog.Int("attempt", attempt+1),
			slog.Int("max_retries", maxRetries),
			slog.String("error", err.Error()),
		)
	}

	return fmt.Errorf("http_carrier %s: all %d attempts failed: %w", h.id, maxRetries+1, lastErr)
}

type httpStatusError struct {
	status int
	detail string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("status %d: %s", e.status, e.detail)
}

func (h *HTTPCarrier) doOnce(ctx context.Context, url string, payload []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", h.cfg.APIKey)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &httpStatusError{status: resp.StatusCode, detail: strings.TrimSpace(string(body))}
	}

	// Cap response body to prevent OOM from oversized carrier responses (SEC-5).
	limited := io.LimitReader(resp.Body, maxResponseBytes)
	if err := json.NewDecoder(limited).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
