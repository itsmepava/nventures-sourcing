package netx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryAfterThenSuccessAndNoAuthRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch r.URL.Path {
		case "/limit":
			if n == 1 {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(429)
				return
			}
			w.Write([]byte(`{"ok":true}`))
		case "/auth":
			w.WriteHeader(401)
		case "/down":
			w.WriteHeader(503)
		case "/write":
			w.WriteHeader(502)
		}
	}))
	defer srv.Close()
	var slept []time.Duration
	p := Policy{Attempts: 4, MaxSleep: 20 * time.Second, Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }}
	var out struct{ OK bool }
	if err := DoJSON(context.Background(), srv.Client(), "GET", srv.URL+"/limit", nil, nil, &out, p); err != nil || !out.OK {
		t.Fatal(err)
	}
	if calls.Load() != 2 || slept[0] < 2*time.Second {
		t.Fatalf("calls=%d slept=%v", calls.Load(), slept)
	}
	calls.Store(0)
	if err := DoJSON(context.Background(), srv.Client(), "GET", srv.URL+"/auth", nil, nil, nil, p); err == nil || calls.Load() != 1 {
		t.Fatalf("auth retried: %d", calls.Load())
	}
	calls.Store(0)
	slept = nil
	if err := DoJSON(context.Background(), srv.Client(), "GET", srv.URL+"/down", nil, nil, nil, p); err == nil || calls.Load() != 4 || len(slept) != 3 {
		t.Fatalf("bounded: calls=%d sleeps=%d", calls.Load(), len(slept))
	}
	for _, d := range slept {
		if d > 20*time.Second {
			t.Fatal("sleep exceeded cap")
		}
	}
	calls.Store(0)
	err := DoJSON(context.Background(), srv.Client(), "POST", srv.URL+"/write", nil, map[string]int{"a": 1}, nil, WriteOnce)
	if calls.Load() != 1 || !IsUncertain(err) {
		t.Fatalf("write retried or not uncertain: %d %v", calls.Load(), err)
	}
}

func TestCancelDuringSleepAndNetwork(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-block:
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := DoJSON(ctx, srv.Client(), "GET", srv.URL+"/limit", nil, nil, nil, Policy{Attempts: 3, MaxSleep: time.Minute})
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("sleep not cancelled: %v %v", err, time.Since(start))
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel2()
	start = time.Now()
	err = DoJSON(ctx2, srv.Client(), "POST", srv.URL+"/slow", nil, map[string]int{}, nil, WriteOnce)
	if !IsUncertain(err) || time.Since(start) > 2*time.Second {
		t.Fatalf("network not cancelled or not uncertain: %v", err)
	}
}
