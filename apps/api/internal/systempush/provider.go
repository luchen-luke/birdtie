package systempush

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

type Outcome string

const (
	Accepted     Outcome = "ACCEPTED"
	Retry        Outcome = "RETRY"
	Rejected     Outcome = "REJECTED"
	InvalidToken Outcome = "INVALID_TOKEN"
	Unknown      Outcome = "UNKNOWN"
)

type ProviderResult struct {
	Outcome       Outcome
	Code          string
	MessageID     string
	RetryAfter    time.Duration
	InvalidatedAt *time.Time
}
type SendRequest struct {
	EventID        string
	BindingID      string
	BindingVersion int64
	Kind           Kind
	Address        SecretAddress
	Check          func(context.Context) error
}

func (r SendRequest) Valid() bool {
	return ValidID(r.EventID) && ValidID(r.BindingID) && r.BindingVersion > 0 && r.BindingVersion <= MaxVersion && r.Address.Valid(r.Kind)
}
func (r SendRequest) Data() map[string]string {
	return map[string]string{"schema": "birdtie-push-v1", "eventId": r.EventID, "bindingId": r.BindingID, "bindingVersion": strconv.FormatInt(r.BindingVersion, 10)}
}

type Provider interface {
	Prepare(context.Context) error
	Send(context.Context, SendRequest) ProviderResult
}
type Registry map[Kind]Provider

func NewProviders(c Config) (Registry, error) {
	p := Registry{}
	if e := c.Validate(); e != nil {
		return nil, e
	}
	if !c.Enabled {
		return p, nil
	}
	if c.FCM.Enabled {
		v, e := NewFCMProvider(c.FCM)
		if e != nil {
			return nil, e
		}
		p[FCMToken] = v
	}
	if c.APNS.Enabled {
		v, e := NewAPNSProvider(c.APNS)
		if e != nil {
			return nil, e
		}
		p[APNSToken] = v
	}
	return p, nil
}
func transportClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 90 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type FCMProvider struct {
	project string
	client  *http.Client
	token   *jwt.Config
	tokenMu sync.Mutex
	cached  *oauth2.Token
}

func NewFCMProvider(c FCMConfig) (*FCMProvider, error) {
	if !c.Enabled || !projectPattern.MatchString(c.ProjectID) || !appPattern.MatchString(c.ApplicationID) {
		return nil, ErrConfig
	}
	b, e := readProtectedFile(c.CredentialsFile, 64<<10)
	if e != nil {
		return nil, ErrConfig
	}
	defer clear(b)
	var key struct {
		Type       string `json:"type"`
		Project    string `json:"project_id"`
		Email      string `json:"client_email"`
		PrivateKey string `json:"private_key"`
		KeyID      string `json:"private_key_id"`
	}
	if uniqueJSON(b) != nil || json.Unmarshal(b, &key) != nil || key.Type != "service_account" || key.Project != c.ProjectID || !strings.HasSuffix(key.Email, ".iam.gserviceaccount.com") || key.PrivateKey == "" || key.KeyID == "" {
		return nil, ErrConfig
	}
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		return nil, ErrConfig
	}
	parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	if e != nil {
		return nil, ErrConfig
	}
	if rsaKey, ok := parsed.(*rsa.PrivateKey); !ok || rsaKey.N.BitLen() < 2048 {
		return nil, ErrConfig
	} // OAuth service-account signing is RSA.
	// No ADC, metadata server, provider URL or OAuth URL can come from a caller.
	return &FCMProvider{project: c.ProjectID, client: transportClient(), token: &jwt.Config{Email: key.Email, PrivateKey: []byte(key.PrivateKey), PrivateKeyID: key.KeyID, Scopes: []string{"https://www.googleapis.com/auth/firebase.messaging"}, TokenURL: "https://oauth2.googleapis.com/token"}}, nil
}
func (p *FCMProvider) Prepare(ctx context.Context) error {
	if p == nil || ctx == nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	_, e := p.accessToken(ctx)
	return e
}
func (p *FCMProvider) accessToken(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.cached != nil && p.cached.Valid() {
		return p.cached.AccessToken, nil
	}
	t, e := p.token.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, p.client)).Token()
	if e != nil {
		return "", ErrUnavailable
	}
	p.cached = t
	return t.AccessToken, nil
}
func (p *FCMProvider) Send(ctx context.Context, r SendRequest) ProviderResult {
	if p == nil || ctx == nil || ctx.Err() != nil || !r.Valid() || r.Kind != FCMToken {
		return ProviderResult{Outcome: Rejected, Code: "INVALID_REQUEST"}
	}
	p.tokenMu.Lock()
	bearer := ""
	if p.cached != nil && p.cached.Valid() {
		bearer = p.cached.AccessToken
	}
	p.tokenMu.Unlock()
	if bearer == "" {
		return ProviderResult{Outcome: Rejected, Code: "AUTH_NOT_PREPARED"}
	}
	payload := map[string]any{"message": map[string]any{"token": r.Address.value, "data": r.Data(), "notification": map[string]string{"title": "Birdtie", "body": "有新的通知，打开查看"}, "android": map[string]any{"ttl": "60s", "priority": "NORMAL", "collapse_key": r.BindingID}}}
	b, e := json.Marshal(payload)
	if e != nil {
		return ProviderResult{Outcome: Rejected, Code: "INVALID_REQUEST"}
	}
	req, e := guardedRequest(ctx, "https://fcm.googleapis.com/v1/projects/"+p.project+"/messages:send", b, r.Check)
	if e != nil {
		return ProviderResult{Outcome: Rejected, Code: "INVALID_REQUEST"}
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	if r.Check == nil || r.Check(ctx) != nil {
		_ = req.Body.Close()
		return ProviderResult{Outcome: Rejected, Code: "SOURCE_UNAVAILABLE"}
	}
	defer req.Body.Close()
	resp, e := p.client.Do(req)
	if e != nil {
		return ProviderResult{Outcome: Unknown, Code: "TRANSPORT_UNKNOWN"}
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (16<<10)+1))
	defer clear(raw)
	media, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil || len(raw) > 16<<10 || mediaErr != nil || media != "application/json" {
		return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
	}
	if resp.StatusCode == http.StatusOK {
		var v struct {
			Name string `json:"name"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if uniqueJSON(raw) != nil || decoder.Decode(&v) != nil || !strings.HasPrefix(v.Name, "projects/"+p.project+"/messages/") || !ValidResultID(v.Name) || v.Name == "projects/"+p.project+"/messages/" {
			return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
		}
		return ProviderResult{Outcome: Accepted, Code: "ACCEPTED", MessageID: v.Name}
	}
	var body struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				Type string `json:"@type"`
				Code string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if uniqueJSON(raw) != nil || json.Unmarshal(raw, &body) != nil {
		return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
	}
	code := ""
	for _, d := range body.Error.Details {
		if d.Type == "type.googleapis.com/google.firebase.fcm.v1.FcmError" {
			code = d.Code
		}
	}
	if resp.StatusCode == 404 && code == "UNREGISTERED" {
		return ProviderResult{Outcome: InvalidToken, Code: "UNREGISTERED"}
	}
	if resp.StatusCode == 429 {
		return ProviderResult{Outcome: Retry, Code: "QUOTA_EXCEEDED", RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Minute)}
	}
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		return ProviderResult{Outcome: Retry, Code: "PROVIDER_UNAVAILABLE", RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Minute)}
	}
	// INVALID_ARGUMENT can refer to a malformed payload: do not invalidate a token.
	return ProviderResult{Outcome: Rejected, Code: statusCode(resp.StatusCode)}
}

// GetBody is absent and this reader cannot be cloned or rewound. The standard
// transport may reconnect before a body is consumed, but every body read then
// crosses the native current-source gate again. Partially consumed requests
// cannot be automatically replayed, and HTTP/1 requests disable connection reuse.
type fencedBody struct {
	mu     sync.Mutex
	data   []byte
	offset int
	closed bool
	ctx    context.Context
	check  func(context.Context) error
}

func (b *fencedBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	if b.check == nil {
		return 0, ErrUnavailable
	}
	if e := b.check(b.ctx); e != nil {
		return 0, ErrUnavailable
	}
	if b.offset == len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}
func (b *fencedBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	clear(b.data)
	return nil
}
func guardedRequest(ctx context.Context, url string, data []byte, check func(context.Context) error) (*http.Request, error) {
	body := &fencedBody{data: data, ctx: ctx, check: check}
	req, e := http.NewRequestWithContext(ctx, "POST", url, body)
	if e != nil {
		_ = body.Close()
		return nil, e
	}
	req.GetBody = nil
	req.ContentLength = int64(len(data))
	req.Close = true
	return req, nil
}
func retryAfter(v string, fallback time.Duration) time.Duration {
	d := fallback
	if n, e := strconv.ParseInt(v, 10, 32); e == nil && n >= 0 {
		d = time.Duration(n) * time.Second
	} else if at, e := http.ParseTime(v); e == nil {
		d = time.Until(at)
	}
	if d < fallback {
		d = fallback
	}
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	return d
}
func statusCode(code int) string {
	switch code {
	case 400:
		return "INVALID_ARGUMENT"
	case 401:
		return "UNAUTHENTICATED"
	case 403:
		return "PERMISSION_DENIED"
	case 404:
		return "NOT_FOUND"
	}
	return "PROVIDER_REJECTED"
}

type APNSProvider struct {
	key                      *ecdsa.PrivateKey
	keyID, team, topic, host string
	client                   *http.Client
	mu                       sync.Mutex
	bearer                   string
	issued                   time.Time
}

func NewAPNSProvider(c APNSConfig) (*APNSProvider, error) {
	if !c.Enabled || !appleIDPattern.MatchString(c.KeyID) || !appleIDPattern.MatchString(c.TeamID) || !appPattern.MatchString(c.Topic) || (c.Environment != "production" && c.Environment != "sandbox") {
		return nil, ErrConfig
	}
	b, e := readProtectedFile(c.KeyFile, 16<<10)
	if e != nil {
		return nil, ErrConfig
	}
	defer clear(b)
	block, rest := pem.Decode(b)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || block.Type != "PRIVATE KEY" {
		return nil, ErrConfig
	}
	v, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	if e != nil {
		return nil, ErrConfig
	}
	key, ok := v.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, ErrConfig
	}
	host := "api.push.apple.com"
	if c.Environment == "sandbox" {
		host = "api.sandbox.push.apple.com"
	}
	return &APNSProvider{key: key, keyID: c.KeyID, team: c.TeamID, topic: c.Topic, host: host, client: transportClient()}, nil
}
func (p *APNSProvider) authToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := time.Now().UTC()
	if p.bearer != "" && n.Sub(p.issued) >= 0 && n.Sub(p.issued) < 50*time.Minute {
		return p.bearer, nil
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": p.keyID})
	claims, _ := json.Marshal(map[string]any{"iss": p.team, "iat": n.Unix()})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, e := ecdsa.Sign(rand.Reader, p.key, digest[:])
	if e != nil {
		return "", ErrUnavailable
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	p.bearer = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	p.issued = n
	return p.bearer, nil
}
func (p *APNSProvider) Prepare(ctx context.Context) error {
	if p == nil || ctx == nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	_, e := p.authToken()
	return e
}
func (p *APNSProvider) Send(ctx context.Context, r SendRequest) ProviderResult {
	if p == nil || ctx == nil || ctx.Err() != nil || !r.Valid() || r.Kind != APNSToken {
		return ProviderResult{Outcome: Rejected, Code: "INVALID_REQUEST"}
	}
	p.mu.Lock()
	bearer := ""
	if p.bearer != "" && time.Since(p.issued) >= 0 && time.Since(p.issued) < time.Hour {
		bearer = p.bearer
	}
	p.mu.Unlock()
	if bearer == "" {
		return ProviderResult{Outcome: Rejected, Code: "AUTH_NOT_PREPARED"}
	}
	payload := map[string]any{"aps": map[string]any{"alert": map[string]string{"title": "Birdtie", "body": "有新的通知，打开查看"}}}
	for k, v := range r.Data() {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	req, e := guardedRequest(ctx, "https://"+p.host+"/3/device/"+r.Address.value, b, r.Check)
	if e != nil {
		return ProviderResult{Outcome: Rejected, Code: "INVALID_REQUEST"}
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", p.topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", "0")
	req.Header.Set("apns-collapse-id", r.BindingID)
	req.Header.Set("apns-id", r.EventID)
	req.Header.Set("content-type", "application/json")
	if r.Check == nil || r.Check(ctx) != nil {
		_ = req.Body.Close()
		return ProviderResult{Outcome: Rejected, Code: "SOURCE_UNAVAILABLE"}
	}
	defer req.Body.Close()
	resp, e := p.client.Do(req)
	if e != nil {
		return ProviderResult{Outcome: Unknown, Code: "TRANSPORT_UNKNOWN"}
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
	defer clear(raw)
	if e != nil || len(raw) > 4096 || resp.ProtoMajor != 2 {
		return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
	}
	if resp.StatusCode == 200 {
		if len(raw) != 0 {
			return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
		}
		id := resp.Header.Get("apns-id")
		if len(resp.Header.Values("apns-id")) != 1 || !ValidID(id) {
			return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
		}
		return ProviderResult{Outcome: Accepted, Code: "ACCEPTED", MessageID: id}
	}
	media, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaErr != nil || media != "application/json" {
		return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
	}
	var body struct {
		Reason    string `json:"reason"`
		Timestamp int64  `json:"timestamp"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if uniqueJSON(raw) != nil || decoder.Decode(&body) != nil {
		return ProviderResult{Outcome: Unknown, Code: "RESPONSE_UNKNOWN"}
	}
	if resp.StatusCode == 410 && body.Reason == "Unregistered" && body.Timestamp > 0 && body.Timestamp <= time.Now().Add(time.Minute).UnixMilli() {
		at := time.UnixMilli(body.Timestamp).UTC()
		return ProviderResult{Outcome: InvalidToken, Code: "UNREGISTERED", InvalidatedAt: &at}
	}
	if resp.StatusCode == 400 && (body.Reason == "BadDeviceToken" || body.Reason == "DeviceTokenNotForTopic") {
		return ProviderResult{Outcome: InvalidToken, Code: "INVALID_DEVICE_TOKEN"}
	}
	if resp.StatusCode == 429 {
		return ProviderResult{Outcome: Retry, Code: "TOO_MANY_REQUESTS", RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Minute)}
	}
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		return ProviderResult{Outcome: Retry, Code: "PROVIDER_UNAVAILABLE", RetryAfter: retryAfter(resp.Header.Get("Retry-After"), 15*time.Minute)}
	}
	return ProviderResult{Outcome: Rejected, Code: statusCode(resp.StatusCode)}
}
