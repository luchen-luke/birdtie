package systempush

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// The URL remains the production fixed host. Only a test-owned TLS dial is
// replaced, exercising the actual standard HTTP/1 or HTTP/2 transport locally.
func localPushTLSClient(t *testing.T, h2 bool, handler http.Handler) *http.Client {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = h2
	server.StartTLS()
	t.Cleanup(server.Close)
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ForceAttemptHTTP2 = h2
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} // synthetic loopback certificate only
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func localFCM(client *http.Client) *FCMProvider {
	return &FCMProvider{project: "birdtie-test", client: client, cached: &oauth2.Token{AccessToken: "synthetic-access", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}}
}
func TestSystemPushRealHTTP1TransportFinalBodyFenceAndNoReplay(t *testing.T) {
	t.Run("revocation-on-first-body-read", func(t *testing.T) {
		var complete atomic.Int32
		client := localPushTLSClient(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, e := io.ReadAll(r.Body)
			if e == nil && len(raw) > 0 {
				complete.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"projects/birdtie-test/messages/synthetic"}`))
		}))
		p := localFCM(client)
		var checks atomic.Int32
		request := testSend(FCMToken)
		request.Check = func(context.Context) error {
			if checks.Add(1) > 1 {
				return ErrNotFound
			}
			return nil
		}
		out := p.Send(context.Background(), request)
		if out.Outcome != Unknown || complete.Load() != 0 || checks.Load() < 2 {
			t.Fatal("body consumed after revocation", out.Outcome, complete.Load(), checks.Load())
		}
	})
	t.Run("connection-dropped-after-whole-body", func(t *testing.T) {
		var complete atomic.Int32
		client := localPushTLSClient(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, e := io.ReadAll(r.Body)
			if e != nil || len(raw) == 0 || !r.Close {
				t.Error("non-replayable request body/Close contract absent")
			}
			complete.Add(1)
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			_ = conn.Close()
		}))
		p := localFCM(client)
		out := p.Send(context.Background(), testSend(FCMToken))
		if out.Outcome != Unknown || complete.Load() != 1 {
			t.Fatal("unknown transport invisibly re-sent", out.Outcome, complete.Load())
		}
	})
}
func TestSystemPushRealHTTP2TransportOpaqueAPNSAndTimeoutNoReplay(t *testing.T) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted-http2", true: "response-timeout"}[timeout], func(t *testing.T) {
			var complete atomic.Int32
			client := localPushTLSClient(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, e := io.ReadAll(r.Body)
				if e != nil || len(raw) == 0 || r.ProtoMajor != 2 {
					t.Error("actual HTTP2 request was not consumed")
				}
				complete.Add(1)
				if timeout {
					<-r.Context().Done()
					return
				}
				w.Header().Set("apns-id", "11111111-1111-4111-8111-111111111111")
				w.WriteHeader(200)
			}))
			p := &APNSProvider{key: key, keyID: "KEY0000001", team: "TEAM000001", topic: "app.birdtie.test", host: "api.sandbox.push.apple.com", client: client}
			if p.Prepare(context.Background()) != nil {
				t.Fatal("ES256 prepare failed")
			}
			request := testSend(APNSToken)
			request.Address = Address(strings.Repeat("a", 64))
			ctx := context.Background()
			var cancel context.CancelFunc
			if timeout {
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			out := p.Send(ctx, request)
			want := Accepted
			if timeout {
				want = Unknown
			}
			if out.Outcome != want || complete.Load() != 1 {
				t.Fatal("HTTP2 response/replay contract", out.Outcome, complete.Load())
			}
		})
	}
}
func TestSystemPushGuardedRequestCannotCloneAndRefreshesEveryRead(t *testing.T) {
	var checks int
	req, e := guardedRequest(context.Background(), "https://fcm.googleapis.com/v1/projects/birdtie-test/messages:send", []byte("synthetic body"), func(context.Context) error {
		checks++
		if checks > 2 {
			return ErrNotFound
		}
		return nil
	})
	if e != nil || req.GetBody != nil || !req.Close {
		t.Fatal("replayable request constructed")
	}
	defer req.Body.Close()
	buf := make([]byte, 2)
	if n, e := req.Body.Read(buf); n != 2 || e != nil {
		t.Fatal("first read failed")
	}
	if n, e := req.Body.Read(buf); n != 2 || e != nil {
		t.Fatal("second read failed")
	}
	if n, e := req.Body.Read(buf); n != 0 || e == nil || checks != 3 {
		t.Fatal("late native check did not stop body")
	}
}
