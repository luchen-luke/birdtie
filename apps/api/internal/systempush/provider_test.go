package systempush

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pushRoundTripper func(*http.Request) (*http.Response, error)

func (f pushRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testSend(kind Kind) SendRequest {
	return SendRequest{EventID: "11111111-1111-4111-8111-111111111111", BindingID: "22222222-2222-4222-8222-222222222222", BindingVersion: 7, Kind: kind, Address: Address("synthetic-fcm-token-1234567890123456789"), Check: func(context.Context) error { return nil }}
}
func providerResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, ProtoMajor: 2, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestSystemPushFCMActualOAuthHTTPV1OpaquePayloadAndOutcomes(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	credentials, _ := json.Marshal(map[string]string{"type": "service_account", "project_id": "birdtie-test", "client_email": "synthetic@birdtie-test.iam.gserviceaccount.com", "private_key_id": "synthetic-kid", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))})
	path := filepath.Join(t.TempDir(), "credential.json")
	if e = os.WriteFile(path, credentials, 0600); e != nil {
		t.Fatal(e)
	}
	p, e := NewFCMProvider(FCMConfig{Enabled: true, ProjectID: "birdtie-test", ApplicationID: "app.birdtie.test", CredentialsFile: path})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	status := 200
	response := `{"name":"projects/birdtie-test/messages/synthetic"}`
	p.client.Transport = pushRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Host == "oauth2.googleapis.com" {
			q, e := url.ParseQuery(string(raw))
			if e != nil || q.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Fatal("missing OAuth JWT grant")
			}
			parts := strings.Split(q.Get("assertion"), ".")
			if len(parts) != 3 {
				t.Fatal("invalid signed JWT")
			}
			claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
			if !bytes.Contains(claims, []byte("https://www.googleapis.com/auth/firebase.messaging")) {
				t.Fatal("wrong OAuth scope")
			}
			return providerResponse(200, `{"access_token":"synthetic-access","token_type":"Bearer","expires_in":3600}`), nil
		}
		if r.URL.Host != "fcm.googleapis.com" || r.URL.Path != "/v1/projects/birdtie-test/messages:send" || r.Header.Get("Authorization") != "Bearer synthetic-access" {
			t.Fatal("HTTP v1 endpoint/auth incorrect")
		}
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil {
			t.Fatal("bad message JSON")
		}
		message := v["message"].(map[string]any)
		data := message["data"].(map[string]any)
		if len(data) != 4 || data["schema"] != "birdtie-push-v1" || data["bindingVersion"] != "7" || message["token"] == nil || message["fid"] != nil {
			t.Fatal("closed FCM_TOKEN payload violated")
		}
		notification := message["notification"].(map[string]any)
		if notification["body"] != "有新的通知，打开查看" {
			t.Fatal("private notification body")
		}
		return providerResponse(status, response), nil
	})
	if e = p.Prepare(context.Background()); e != nil {
		t.Fatal("OAuth preparation failed")
	}
	if out := p.Send(context.Background(), testSend(FCMToken)); out.Outcome != Accepted || calls != 2 {
		t.Fatal("actual OAuth then v1 send failed", out.Outcome, calls)
	}
	for _, tc := range []struct {
		status int
		body   string
		want   Outcome
	}{{404, `{"error":{"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`, InvalidToken}, {400, `{"error":{"status":"INVALID_ARGUMENT"}}`, Rejected}, {429, `{}`, Retry}, {503, `{}`, Retry}, {200, `{"secret":"not-a-receipt"}`, Unknown}} {
		status, response = tc.status, tc.body
		if out := p.Send(context.Background(), testSend(FCMToken)); out.Outcome != tc.want {
			t.Fatal("wrong normalized outcome", tc.status, out.Outcome)
		}
	}
	p.client.Transport = pushRoundTripper(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	if out := p.Send(context.Background(), testSend(FCMToken)); out.Outcome != Unknown {
		t.Fatal("timeout retried")
	}
}
func TestSystemPushAPNSActualES256HTTP2AndTimestamp(t *testing.T) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	path := filepath.Join(t.TempDir(), "apns.p8")
	if e = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := NewAPNSProvider(APNSConfig{Enabled: true, KeyFile: path, KeyID: "KEY0000001", TeamID: "TEAM000001", Topic: "app.birdtie.test", Environment: "sandbox"})
	if e != nil {
		t.Fatal(e)
	}
	request := testSend(APNSToken)
	request.Address = Address(strings.Repeat("a", 64))
	status := 200
	body := ``
	count := 0
	p.client.Transport = pushRoundTripper(func(r *http.Request) (*http.Response, error) {
		count++
		if r.URL.Host != "api.sandbox.push.apple.com" || r.Header.Get("apns-topic") != "app.birdtie.test" || r.Header.Get("apns-push-type") != "alert" || r.Header.Get("apns-expiration") != "0" {
			t.Fatal("APNS trusted headers incorrect")
		}
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("authorization"), "bearer "), ".")
		if len(parts) != 3 {
			t.Fatal("APNS JWT absent")
		}
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if len(sig) != 64 || !ecdsa.Verify(&key.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			t.Fatal("not real ES256")
		}
		raw, _ := io.ReadAll(r.Body)
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		if len(v) != 5 || v["bindingVersion"] != "7" || v["schema"] != "birdtie-push-v1" {
			t.Fatal("opaque APNS payload violated")
		}
		res := providerResponse(status, body)
		res.Header.Set("apns-id", request.EventID)
		return res, nil
	})
	if e = p.Prepare(context.Background()); e != nil {
		t.Fatal("ES256 preparation failed")
	}
	out := p.Send(context.Background(), request)
	if out.Outcome != Accepted || count != 1 {
		t.Fatal("APNS send failed", out.Outcome)
	}
	issued := p.bearer
	out = p.Send(context.Background(), request)
	if out.Outcome != Accepted || p.bearer != issued {
		t.Fatal("JWT unnecessarily rotated")
	}
	status = 410
	body = `{"reason":"Unregistered","timestamp":1700000000123}`
	out = p.Send(context.Background(), request)
	if out.Outcome != InvalidToken || out.InvalidatedAt == nil || out.InvalidatedAt.UnixMilli() != 1700000000123 {
		t.Fatal("410 timestamp lost")
	}
	status = 503
	body = `{}`
	out = p.Send(context.Background(), request)
	if out.Outcome != Retry || out.RetryAfter < 15*time.Minute {
		t.Fatal("APNS failure backoff absent")
	}
	p.client.Transport = pushRoundTripper(func(*http.Request) (*http.Response, error) {
		res := providerResponse(200, `{}`)
		res.ProtoMajor = 1
		return res, nil
	})
	if p.Send(context.Background(), request).Outcome != Unknown {
		t.Fatal("non-HTTP2 response accepted")
	}
}
