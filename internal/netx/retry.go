// Package netx contains the bounded-retry HTTP helper shared by the Google
// and optional provider clients.
package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// ProviderError is a safe, user-presentable error. It never contains
// response bodies, tokens or request payloads.
type ProviderError struct {
	Msg    string
	Status int
	// Uncertain is true when a write may have been applied remotely.
	Uncertain bool
}

func (e *ProviderError) Error() string { return e.Msg }

// Errorf builds a ProviderError.
func Errorf(format string, a ...any) error { return &ProviderError{Msg: fmt.Sprintf(format, a...)} }

// Policy bounds retries.
type Policy struct {
	Attempts int
	MaxSleep time.Duration
	Sleep    func(ctx context.Context, d time.Duration) error
}

// DefaultRead is used for idempotent reads.
var DefaultRead = Policy{Attempts: 4, MaxSleep: 20 * time.Second}

// WriteOnce is used for non-idempotent writes: never retried blindly.
var WriteOnce = Policy{Attempts: 1}

// SleepCtx sleeps unless the context ends first.
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RetryAfter parses a Retry-After header (seconds or HTTP date), capped.
func RetryAfter(h string, limit time.Duration) time.Duration {
	if h == "" {
		return 0
	}
	if s, err := strconv.Atoi(h); err == nil && s >= 0 {
		return min(time.Duration(s)*time.Second, limit)
	}
	if t, err := http.ParseTime(h); err == nil {
		return min(max(time.Until(t), 0), limit)
	}
	return 0
}

func retryable(code int) bool {
	return code == 429 || code == 500 || code == 502 || code == 503 || code == 504
}

// DoJSON sends a JSON request and decodes a JSON response with bounded
// retries for 429/5xx/network errors. Auth and other client errors are not
// retried. For WriteOnce, any failure after sending is marked Uncertain.
func DoJSON(ctx context.Context, c *http.Client, method, url string, headers map[string]string, in, out any, p Policy) error {
	if p.Attempts < 1 {
		p.Attempts = 1
	}
	if p.MaxSleep == 0 {
		p.MaxSleep = 60 * time.Second
	}
	sleep := p.Sleep
	if sleep == nil {
		sleep = SleepCtx
	}
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	write := method != http.MethodGet && method != http.MethodHead
	var last error
	for attempt := 0; attempt < p.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.Do(req)
		var wait time.Duration
		if err != nil {
			if ctx.Err() != nil {
				if write {
					return &ProviderError{Msg: "Cancelled while a write was in flight; reconcile before retrying", Uncertain: true}
				}
				return ctx.Err()
			}
			last = &ProviderError{Msg: "Network error contacting provider", Uncertain: write}
		} else {
			data, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
			resp.Body.Close()
			switch {
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				if rerr != nil {
					return &ProviderError{Msg: "Provider response was interrupted", Uncertain: write}
				}
				if out == nil || len(data) == 0 {
					return nil
				}
				if err := json.Unmarshal(data, out); err != nil {
					return &ProviderError{Msg: "Provider returned malformed JSON", Uncertain: write}
				}
				return nil
			case resp.StatusCode == 401 || resp.StatusCode == 403:
				return &ProviderError{Msg: fmt.Sprintf("Provider HTTP %d: authorization or permission problem; reconnect or check sharing", resp.StatusCode), Status: resp.StatusCode}
			case resp.StatusCode == 404:
				return &ProviderError{Msg: "Provider HTTP 404: destination not found; check the ID and tab name", Status: 404}
			case retryable(resp.StatusCode):
				last = &ProviderError{Msg: fmt.Sprintf("Provider HTTP %d after bounded retries", resp.StatusCode), Status: resp.StatusCode, Uncertain: write && resp.StatusCode >= 500}
				wait = RetryAfter(resp.Header.Get("Retry-After"), p.MaxSleep)
			default:
				return &ProviderError{Msg: fmt.Sprintf("Provider HTTP %d; check configuration", resp.StatusCode), Status: resp.StatusCode}
			}
		}
		if attempt < p.Attempts-1 {
			backoff := time.Duration(1<<attempt)*time.Second + time.Duration(rand.IntN(500))*time.Millisecond
			if wait < backoff {
				wait = min(backoff, p.MaxSleep)
			}
			if err := sleep(ctx, wait); err != nil {
				return err
			}
		}
	}
	return last
}

// IsUncertain reports whether err indicates a possibly-applied write.
func IsUncertain(err error) bool {
	var pe *ProviderError
	return errors.As(err, &pe) && pe.Uncertain
}

// SafeMessage returns a message suitable for persistence and display.
func SafeMessage(err error) string {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.Msg
	}
	if errors.Is(err, context.Canceled) {
		return "Cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Timed out"
	}
	return "Operation failed; check configuration or retry"
}
