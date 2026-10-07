package agenttool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func wsaNativeGuardExpected(query string) string {
	body, _ := json.Marshal(struct {
		Query string `json:"Query"`
	}{query})
	return string(body)
}
func wsaNativeGuardRequest(ctx context.Context, expected string) *http.Request {
	r, _ := http.NewRequestWithContext(ctx, http.MethodPost, TencentWSAEndpoint, strings.NewReader(expected))
	r.GetBody = nil
	r.Close = true
	return r
}

func TestTencentWSANativeGuardNormalExactQueryAndOriginalSearchUnchanged(t *testing.T) {
	var guards atomic.Int32
	calls := 0
	a := wsaTestAdapter(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != TencentWSAEndpoint || r.Method != http.MethodPost || !r.Close || r.GetBody != nil || r.ProtoMajor != 1 {
			t.Fatal("fixed request boundary changed")
		}
		body, e := io.ReadAll(r.Body)
		if e != nil || string(body) != wsaNativeGuardExpected(wsaTestQuery) {
			t.Fatal("wire was not the exact canonical Query-only bytes")
		}
		return wsaTestHTTP(wsaTestWire(nil, "standard")), nil
	})
	before := func(ctx context.Context) error { guards.Add(1); return ctx.Err() }
	result, e := a.SearchWithNativeGuard(context.Background(), wsaTestQuery, time.Now().Add(time.Second), before)
	if e != nil || result.RequestID != wsaTestID || calls != 1 || guards.Load() < 3 {
		t.Fatalf("exact native guarded query failed: %v", e)
	}
	oldCount := guards.Load()
	if _, e = a.Search(context.Background(), wsaTestQuery, time.Now().Add(time.Second)); e != nil || calls != 2 || guards.Load() != oldCount {
		t.Fatal("guard copy changed original Search or shared client")
	}
}

func TestTencentWSANativeGuardConnectionWaitRevocationReleasesZeroQueryBytes(t *testing.T) {
	allowed := true
	wireBytes := 0
	calls := 0
	a := wsaTestAdapter(t, func(r *http.Request) (*http.Response, error) {
		calls++
		// Simulate native revocation during DNS/TLS connection establishment,
		// after RoundTrip begins but before the HTTP writer reads any query.
		allowed = false
		body, e := io.ReadAll(r.Body)
		wireBytes += len(body)
		if e == nil {
			t.Fatal("revoked native source was readable after connection wait")
		}
		return nil, e
	})
	before := func(context.Context) error {
		if !allowed {
			return errors.New("private native reason must never escape")
		}
		return nil
	}
	result, e := a.SearchWithNativeGuard(context.Background(), wsaTestQuery, time.Now().Add(time.Second), before)
	if e == nil || calls != 1 || wireBytes != 0 || result.RequestID != "" || strings.Contains(e.Error(), "private native") || strings.Contains(e.Error(), wsaTestQuery) {
		t.Fatal("late native revocation leaked query or error details")
	}
}

func TestTencentWSANativeGuardRevalidatesBetweenPartialBodyReads(t *testing.T) {
	allowed := true
	b := &tencentWSANativeGuardBody{ctx: context.Background(), now: time.Now, deadline: time.Now().Add(time.Second), payload: wsaNativeGuardExpected(wsaTestQuery), before: func(context.Context) error {
		if !allowed {
			return ErrTencentWSARequest
		}
		return nil
	}}
	first := make([]byte, 3)
	if n, e := b.Read(first); e != nil || n != 3 {
		t.Fatal("initial approved bounded read rejected")
	}
	allowed = false
	rest := make([]byte, 256)
	if n, e := b.Read(rest); n != 0 || e == nil {
		t.Fatal("next chunk bypassed fresh native check")
	}
	if !bytes.Equal(rest, make([]byte, len(rest))) {
		t.Fatal("denied chunk modified destination bytes")
	}
}

func TestTencentWSANativeGuardOneRoundTripAndNoRetry(t *testing.T) {
	for _, status := range []int{200, 307, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			expected := wsaNativeGuardExpected(wsaTestQuery)
			calls := 0
			guard := &tencentWSANativeGuardTransport{expected: expected, before: func(context.Context) error { return nil }, now: time.Now, deadline: time.Now().Add(time.Second), next: wsaRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if raw, e := io.ReadAll(r.Body); e != nil || string(raw) != expected {
					t.Fatal("canonical wire changed")
				}
				resp := wsaTestHTTP(wsaTestWire(nil, "standard"))
				resp.StatusCode = status
				return resp, nil
			})}
			r := wsaNativeGuardRequest(context.Background(), expected)
			resp, e := guard.RoundTrip(r)
			if e != nil || resp == nil || calls != 1 {
				t.Fatal("first round trip failed")
			}
			resp.Body.Close()
			if _, e = guard.RoundTrip(wsaNativeGuardRequest(context.Background(), expected)); e == nil || calls != 1 {
				t.Fatal("repeated RoundTrip sent a second request")
			}
		})
	}
}

func TestTencentWSANativeGuardRejectsWrongWireAndRequestSelectors(t *testing.T) {
	for name, change := range map[string]func(*http.Request){
		"endpoint": func(r *http.Request) { r.URL.Host = "example.org" },
		"method":   func(r *http.Request) { r.Method = http.MethodGet },
		"replay": func(r *http.Request) {
			r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("query")), nil }
		},
		"connection_reuse": func(r *http.Request) { r.Close = false },
		"http2":            func(r *http.Request) { r.ProtoMajor = 2 },
		"length":           func(r *http.Request) { r.ContentLength-- },
		"body_tamper": func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", int(r.ContentLength))))
		},
		"missing_body": func(r *http.Request) { r.Body = nil },
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			expected := wsaNativeGuardExpected(wsaTestQuery)
			guard := &tencentWSANativeGuardTransport{expected: expected, now: time.Now, deadline: time.Now().Add(time.Second), before: func(context.Context) error { return nil }, next: wsaRoundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, nil })}
			r := wsaNativeGuardRequest(context.Background(), expected)
			change(r)
			if _, e := guard.RoundTrip(r); e == nil || calls != 0 {
				t.Fatal("invalid native wire reached transport")
			}
		})
	}
}

func TestTencentWSANativeGuardNilCancelExpiredAndCallbackCancellation(t *testing.T) {
	calls := 0
	a := wsaTestAdapter(t, func(*http.Request) (*http.Response, error) { calls++; return nil, nil })
	if _, e := a.SearchWithNativeGuard(context.Background(), wsaTestQuery, time.Now().Add(time.Second), nil); e == nil || calls != 0 {
		t.Fatal("nil native dependency dispatched")
	}
	for _, name := range []string{"cancel_before", "cancel_during_guard", "expired_before", "expired_during_guard", "native_denied"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Now()
			deadline := now.Add(time.Second)
			if name == "cancel_before" {
				cancel()
			}
			if name == "expired_before" {
				deadline = now.Add(-time.Second)
			}
			before := func(context.Context) error {
				switch name {
				case "cancel_during_guard":
					cancel()
				case "expired_during_guard":
					now = deadline
				case "native_denied":
					return errors.New("private native state")
				}
				return nil
			}
			wireCalls := 0
			guard := &tencentWSANativeGuardTransport{expected: wsaNativeGuardExpected(wsaTestQuery), now: func() time.Time { return now }, deadline: deadline, before: before, next: wsaRoundTrip(func(*http.Request) (*http.Response, error) { wireCalls++; return nil, nil })}
			if _, e := guard.RoundTrip(wsaNativeGuardRequest(ctx, guard.expected)); e == nil || wireCalls != 0 || strings.Contains(e.Error(), "private native state") {
				t.Fatal("cancelled/expired/unapproved native attempt advanced")
			}
		})
	}
}

func TestTencentWSANativeGuardBodyCloseAndCloseDuringGuard(t *testing.T) {
	b := &tencentWSANativeGuardBody{ctx: context.Background(), now: time.Now, deadline: time.Now().Add(time.Second), payload: wsaNativeGuardExpected(wsaTestQuery), before: func(context.Context) error { return nil }}
	if e := b.Close(); e != nil {
		t.Fatal(e)
	}
	if n, e := b.Read(make([]byte, 128)); n != 0 || !errors.Is(e, io.ErrClosedPipe) {
		t.Fatal("closed native reader remained readable")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	b = &tencentWSANativeGuardBody{ctx: context.Background(), now: time.Now, deadline: time.Now().Add(time.Second), payload: wsaNativeGuardExpected(wsaTestQuery), before: func(context.Context) error { close(entered); <-release; return nil }}
	done := make(chan error, 1)
	go func() {
		n, e := b.Read(make([]byte, 128))
		if n != 0 {
			done <- errors.New("closed reader released query")
			return
		}
		done <- e
	}()
	<-entered
	if e := b.Close(); e != nil {
		t.Fatal(e)
	}
	close(release)
	select {
	case e := <-done:
		if !errors.Is(e, io.ErrClosedPipe) {
			t.Fatal("Close during callback did not retire payload")
		}
	case <-time.After(time.Second):
		t.Fatal("native callback and Close deadlocked")
	}
}

func TestTencentWSANativeGuardBodyCancelAndDeadlineReleaseZeroBytes(t *testing.T) {
	for _, name := range []string{"cancel_before", "cancel_during_guard", "expired_before", "expired_during_guard"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Now()
			deadline := now.Add(time.Second)
			if name == "cancel_before" {
				cancel()
			}
			if name == "expired_before" {
				now = deadline
			}
			guardCalls := 0
			b := &tencentWSANativeGuardBody{ctx: ctx, now: func() time.Time { return now }, deadline: deadline, payload: wsaNativeGuardExpected(wsaTestQuery), before: func(received context.Context) error {
				guardCalls++
				if received != ctx {
					t.Fatal("body callback lost the request context")
				}
				if name == "cancel_during_guard" {
					cancel()
				}
				if name == "expired_during_guard" {
					now = deadline
				}
				return nil
			}}
			dst := bytes.Repeat([]byte{0x7f}, 128)
			expectedErr := context.DeadlineExceeded
			if strings.HasPrefix(name, "cancel") {
				expectedErr = context.Canceled
			}
			if n, e := b.Read(dst); n != 0 || !errors.Is(e, expectedErr) || !bytes.Equal(dst, bytes.Repeat([]byte{0x7f}, len(dst))) {
				t.Fatal("cancelled or expired body released query bytes")
			}
			if strings.HasSuffix(name, "before") && guardCalls != 0 || strings.HasSuffix(name, "guard") && guardCalls != 1 {
				t.Fatal("body callback did not follow the current context boundary")
			}
		})
	}
}

func TestTencentWSANativeGuardEarlyResponseLateReadCancelledAndConcurrentClose(t *testing.T) {
	var held io.ReadCloser
	a := wsaTestAdapter(t, func(r *http.Request) (*http.Response, error) {
		held = r.Body
		return wsaTestHTTP(wsaTestWire(nil, "standard")), nil
	})
	if _, e := a.SearchWithNativeGuard(context.Background(), wsaTestQuery, time.Now().Add(time.Second), func(context.Context) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if raw, e := io.ReadAll(held); len(raw) != 0 || !errors.Is(e, context.Canceled) {
		t.Fatal("late reader observed cleared or unapproved query after early response")
	}
	if e := held.Close(); e != nil {
		t.Fatal(e)
	}
	// Only fake readers/transports are exercised here, with no network or DB.
	for round := 0; round < 20; round++ {
		b := &tencentWSANativeGuardBody{ctx: context.Background(), now: time.Now, deadline: time.Now().Add(time.Second), payload: wsaNativeGuardExpected(wsaTestQuery), before: func(context.Context) error { return nil }}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = io.Copy(io.Discard, b) }()
		go func() { defer wg.Done(); _ = b.Close() }()
		wg.Wait()
	}
}

func TestTencentWSANativeGuardObjectsRedacted(t *testing.T) {
	payload := wsaNativeGuardExpected(wsaTestQuery)
	for _, obj := range []any{&tencentWSANativeGuardTransport{expected: payload}, &tencentWSANativeGuardBody{payload: payload}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%q", "%s"} {
			s := fmt.Sprintf(verb, obj)
			if !strings.Contains(s, "redacted") || strings.Contains(s, wsaTestQuery) {
				t.Fatal("native wire wrapper formatting exposed query")
			}
		}
	}
}
