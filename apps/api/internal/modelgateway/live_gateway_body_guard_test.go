package modelgateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeLiveBodyGuardChecksRevocationAfterTransportHandoff(t *testing.T) {
	port := &liveTestPort{}
	var calls, bodyBytes atomic.Int32
	gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		// Models withdrawal while DNS/TLS waits, before the first payload byte.
		port.denied.Store(true)
		raw, e := io.ReadAll(req.Body)
		bodyBytes.Add(int32(len(raw)))
		if e == nil || len(raw) != 0 {
			t.Fatal("revoked source crossed actual Body.Read")
		}
		return nil, errors.New("controlled body read rejection")
	}))
	result, e := gateway.Complete(context.Background(), liveTestRequest())
	assertNoLiveText(t, result, e)
	if calls.Load() != 1 || bodyBytes.Load() != 0 || port.settles.Load() != 1 {
		t.Fatal("body guard lost one-attempt/UNKNOWN cleanup boundary")
	}
}

func TestNativeLiveBodyGuardExactChunksAndRevocationBeforeLaterRead(t *testing.T) {
	port := &liveTestPort{}
	request := liveTestRequest()
	reads := 0
	var expected PreparedTencentWire
	gateway, adapter := liveTestGateway(t, port, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		var wire bytes.Buffer
		chunk := make([]byte, 7)
		for {
			n, e := req.Body.Read(chunk)
			reads++
			wire.Write(chunk[:n])
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal("normal exact body rejected", e)
			}
		}
		if !bytes.Equal(wire.Bytes(), expected.ExactWire()) {
			t.Fatal("exact approved body changed")
		}
		if e := req.Body.Close(); e != nil {
			t.Fatal(e)
		}
		n, e := req.Body.Read(chunk)
		if n != 0 || e == nil {
			t.Fatal("closed body emitted bytes")
		}
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	var e error
	expected, e = adapter.Prepare(providerRequest(request))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = gateway.Complete(context.Background(), request); e != nil || reads < 2 || port.checks.Load() < int32(reads) {
		t.Fatal("native guard did not run for each body chunk", e)
	}
	// A partial allowed read cannot authorize the remaining request after revoke.
	allowed := true
	checks := 0
	body := &nativeLiveRequestBody{reader: strings.NewReader("immutable private bytes"), ctx: context.Background(), before: func() error {
		checks++
		if !allowed {
			return ErrUnavailable
		}
		return nil
	}}
	chunk := make([]byte, 4)
	if n, e := body.Read(chunk); n != 4 || e != nil {
		t.Fatal("first current read rejected")
	}
	allowed = false
	if n, e := body.Read(chunk); n != 0 || e == nil || checks != 2 {
		t.Fatal("later read reused old authority")
	}
}

func TestNativeLiveBodyGuardCancellationExpiryAndInvalidDependency(t *testing.T) {
	for _, kind := range []string{"cancelled", "deadline", "close", "during_native_check", "missing_context", "missing_guard", "missing_reader"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &nativeLiveRequestBody{reader: strings.NewReader("must not leave"), ctx: ctx, before: func() error { return nil }}
			switch kind {
			case "cancelled":
				cancel()
			case "deadline":
				expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer stop()
				body.ctx = expired
			case "close":
				_ = body.Close()
			case "during_native_check":
				body.before = func() error { cancel(); return nil }
			case "missing_context":
				body.ctx = nil
			case "missing_guard":
				body.before = nil
			case "missing_reader":
				body.reader = nil
			}
			raw, e := io.ReadAll(body)
			if e == nil || len(raw) != 0 {
				t.Fatal("invalid/late body released payload")
			}
		})
	}
}

func TestNativeLiveBodyGuardEarlyResponseRemainsImmutableAndCannotReplay(t *testing.T) {
	adapter := tencentAdapter(t, nil)
	request := liveTestRequest()
	prepared, e := adapter.Prepare(providerRequest(request))
	if e != nil {
		t.Fatal(e)
	}
	var delayed io.ReadCloser
	var attempts, wires atomic.Int32
	var calls atomic.Int32
	guard := liveWireTransport{prepared: prepared, attempts: &attempts, wires: &wires, before: func() error { return nil }, next: tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		delayed = req.Body
		return tencentResponse(tencentSuccess(), 200), nil
	})}
	makeRequest := func() *http.Request {
		req, e := http.NewRequestWithContext(context.Background(), http.MethodPost, tencentChatEndpoint, bytes.NewReader(prepared.ExactWire()))
		if e != nil {
			t.Fatal(e)
		}
		req.GetBody = nil
		req.Close = true
		return req
	}
	response, e := guard.RoundTrip(makeRequest())
	if e != nil {
		t.Fatal(e)
	}
	_ = response.Body.Close()
	raw, e := io.ReadAll(delayed)
	if e != nil || !bytes.Equal(raw, prepared.ExactWire()) {
		t.Fatal("early response mutated approved bytes", e)
	}
	if _, e = guard.RoundTrip(makeRequest()); e == nil || calls.Load() != 1 || attempts.Load() != 1 || wires.Load() != 1 {
		t.Fatal("body guard installed retry")
	}
	_ = delayed.Close()
}

func TestNativeLiveBodyGuardGatewayRetiresDelayedReadAfterResponse(t *testing.T) {
	port := &liveTestPort{}
	var delayed io.ReadCloser
	gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		delayed = req.Body
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	if _, e := gateway.Complete(context.Background(), liveTestRequest()); e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(delayed)
	if e == nil || len(raw) != 0 {
		t.Fatal("retired release emitted delayed private bytes")
	}
	_ = delayed.Close()
}
