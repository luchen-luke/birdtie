package agenttool

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

const TencentWSAEndpoint = "https://api.wsa.cloud.tencent.com/SearchPro"
const TencentWSAMaxDeadline = 45 * time.Second
const TencentWSAMaxConfigBytes = 8 * 1024
const TencentWSAMaxRequestBytes = 4 * 1024
const TencentWSAMaxResultBytes = 512 * 1024
const TencentWSAMaxSources = 10

// Conservative published maximum per WSA call for later original-ledger
// reservation input only. This is not actual billed cash or a budget grant.
const TencentWSAMaxTariffMicros = 80_000

var ErrTencentWSAConfig = errors.New("invalid WSA server configuration")
var ErrTencentWSARequest = errors.New("invalid WSA transport request")
var ErrTencentWSAResponse = errors.New("invalid WSA provider response")

// This configuration is transport-only. It grants neither MODEL_EGRESS nor a
// tool invocation, and has no budget, store, actor or registry registration.
type TencentWSAConfig struct {
	apiKey  string
	keyName string
}

func (TencentWSAConfig) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "TencentWSAConfig{redacted}")
}
func (TencentWSAConfig) MarshalJSON() ([]byte, error) { return nil, ErrTencentWSAConfig }
func (c *TencentWSAConfig) UnmarshalJSON([]byte) error {
	*c = TencentWSAConfig{}
	return ErrTencentWSAConfig
}
func (c TencentWSAConfig) valid() bool {
	if len(c.apiKey) < 1 || len(c.apiKey) > 4096 || !wsaText(c.keyName, 80, false) || strings.TrimSpace(c.keyName) != c.keyName {
		return false
	}
	for _, b := range []byte(c.apiKey) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

func ParseTencentWSAConfig(raw []byte) (TencentWSAConfig, error) {
	if len(raw) == 0 || len(raw) > TencentWSAMaxConfigBytes {
		return TencentWSAConfig{}, ErrTencentWSAConfig
	}
	obj, err := wsaObject(raw, []string{"apiKey", "keyName"}, nil)
	var c TencentWSAConfig
	if err != nil || json.Unmarshal(obj["apiKey"], &c.apiKey) != nil || json.Unmarshal(obj["keyName"], &c.keyName) != nil || !c.valid() {
		return TencentWSAConfig{}, ErrTencentWSAConfig
	}
	return c, nil
}

// Load only the narrowly named, ignored, absolute server file. Errors and
// formatting never expose the path or credentials. Ignore is an operator
// prerequisite, not an authorization performed by this transport layer.
func LoadTencentWSAConfig(path string) (TencentWSAConfig, error) {
	if !filepath.IsAbs(path) || filepath.Base(path) != ".env.wsa.local.json" {
		return TencentWSAConfig{}, ErrTencentWSAConfig
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > TencentWSAMaxConfigBytes {
		return TencentWSAConfig{}, ErrTencentWSAConfig
	}
	f, err := os.Open(path)
	if err != nil {
		return TencentWSAConfig{}, ErrTencentWSAConfig
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return TencentWSAConfig{}, ErrTencentWSAConfig
	}
	raw, err := io.ReadAll(io.LimitReader(f, TencentWSAMaxConfigBytes+1))
	defer clear(raw)
	if err != nil {
		return TencentWSAConfig{}, ErrTencentWSAConfig
	}
	return ParseTencentWSAConfig(raw)
}

// Returned snippets are untrusted provider data, never instructions, entity
// IDs, coordinates, native field evidence or grants. Version UNKNOWN cannot
// establish a tariff. Cash is UNKNOWN even when a provider tier is recognized.
type TencentWSASource struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Passage string `json:"passage"`
	Date    string `json:"date"`
	Site    string `json:"site"`
}
type TencentWSAResult struct {
	RequestID  string             `json:"requestId"`
	Version    string             `json:"version"`
	CashStatus string             `json:"cashStatus"`
	Sources    []TencentWSASource `json:"sources"`
}

// Provider errors retain only a fixed classification, never the provider's
// message or a network error containing the URL, bearer or query. No retries.
type TencentWSAProviderError struct{ Code string }

func (e TencentWSAProviderError) Error() string { return "WSA provider error: " + e.Code }

type TencentWSAAdapter struct {
	config TencentWSAConfig
	client *http.Client
	now    func() time.Time
}

func (*TencentWSAAdapter) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "TencentWSAAdapter{redacted}")
}
func (*TencentWSAAdapter) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (a *TencentWSAAdapter) UnmarshalJSON([]byte) error {
	*a = TencentWSAAdapter{}
	return ErrServerOnly
}

// The optional RoundTripper is a trusted server/test seam, never request input.
// Nil creates a dedicated HTTP/1 transport: no proxy, redirects, replay,
// connection reuse, automatic decompression, media reads or secondary fetches.
func NewTencentWSAAdapter(c TencentWSAConfig, transport http.RoundTripper) (*TencentWSAAdapter, error) {
	if !c.valid() {
		return nil, ErrTencentWSAConfig
	}
	if transport == nil {
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		protocols.SetHTTP2(false)
		protocols.SetUnencryptedHTTP2(false)
		transport = &http.Transport{
			Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			Protocols:       protocols,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}},
			TLSNextProto:    map[string]func(string, *tls.Conn) http.RoundTripper{}, ForceAttemptHTTP2: false,
			DisableKeepAlives: true, DisableCompression: true, MaxConnsPerHost: 1,
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		}
	} else {
		v := reflect.ValueOf(transport)
		if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface || v.Kind() == reflect.Func || v.Kind() == reflect.Map || v.Kind() == reflect.Slice || v.Kind() == reflect.Chan) && v.IsNil() {
			return nil, ErrTencentWSARequest
		}
	}
	return &TencentWSAAdapter{config: c, now: time.Now, client: &http.Client{Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Search sends exactly one query-only request. The native caller must reserve
// the original request budget and check current egress BEFORE this method is
// used. Native dispatch additionally uses SearchWithNativeGuard to revalidate
// after connection waits, before each actual body read. Neither method
// registers this adapter in Catalogue/main or confers egress permission.
func (a *TencentWSAAdapter) Search(ctx context.Context, query string, deadline time.Time) (TencentWSAResult, error) {
	var empty TencentWSAResult
	if a == nil || a.client == nil || a.now == nil || !a.config.valid() || ctx == nil {
		return empty, ErrTencentWSARequest
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !wsaText(query, 240, false) || strings.TrimSpace(query) != query || !deadline.After(a.now()) || deadline.After(a.now().Add(TencentWSAMaxDeadline)) {
		return empty, ErrTencentWSARequest
	}
	body, err := json.Marshal(struct {
		Query string `json:"Query"`
	}{query})
	if err != nil || len(body) > TencentWSAMaxRequestBytes {
		return empty, ErrTencentWSARequest
	}
	callctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	req, err := http.NewRequestWithContext(callctx, http.MethodPost, TencentWSAEndpoint, bytes.NewReader(body))
	if err != nil {
		return empty, ErrTencentWSARequest
	}
	req.GetBody = nil
	req.Close = true
	req.Proto, req.ProtoMajor, req.ProtoMinor = "HTTP/1.1", 1, 1
	req.Header.Set("Authorization", "Bearer "+a.config.apiKey)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		if current := callctx.Err(); current != nil {
			return empty, current
		}
		return empty, TencentWSAProviderError{Code: "TRANSPORT_UNKNOWN"}
	}
	if resp == nil || resp.Body == nil {
		return empty, ErrTencentWSAResponse
	}
	defer resp.Body.Close()
	if err := callctx.Err(); err != nil {
		return empty, err
	}
	if resp.ProtoMajor != 1 {
		return empty, ErrTencentWSAResponse
	}
	if resp.StatusCode != http.StatusOK {
		return empty, wsaHTTPError(resp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || resp.Uncompressed || resp.Header.Get("Content-Encoding") != "" {
		return empty, ErrTencentWSAResponse
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, TencentWSAMaxResultBytes+1))
	defer clear(raw)
	if current := callctx.Err(); current != nil {
		return empty, current
	}
	if err != nil {
		return empty, TencentWSAProviderError{Code: "TRANSPORT_UNKNOWN"}
	}
	if !deadline.After(a.now()) {
		return empty, context.DeadlineExceeded
	}
	result, err := parseTencentWSAResult(raw, query)
	if current := callctx.Err(); current != nil {
		return empty, current
	}
	if !deadline.After(a.now()) {
		return empty, context.DeadlineExceeded
	}
	return result, err
}

func wsaHTTPError(status int) error {
	code := "HTTP_UNKNOWN"
	switch {
	case status == 401 || status == 403:
		code = "AUTHENTICATION"
	case status == 429:
		code = "RATE_LIMIT"
	case status >= 500 && status <= 599:
		code = "TEMPORARY"
	case status >= 400 && status <= 499:
		code = "INVALID_REQUEST"
	}
	return TencentWSAProviderError{Code: code}
}

func parseTencentWSAResult(raw []byte, query string) (TencentWSAResult, error) {
	var empty TencentWSAResult
	if len(raw) == 0 || len(raw) > TencentWSAMaxResultBytes {
		return empty, ErrTencentWSAResponse
	}
	root, err := wsaObject(raw, []string{"Response"}, nil)
	if err != nil {
		return empty, ErrTencentWSAResponse
	}
	obj, err := wsaObject(root["Response"], []string{"RequestId"}, []string{"Query", "Pages", "Version", "Msg", "Error"})
	if err != nil {
		return empty, ErrTencentWSAResponse
	}
	id, err := wsaString(obj["RequestId"], 80, false)
	if err != nil || !wsaRequestID(id) {
		return empty, ErrTencentWSAResponse
	}
	if rawError, exists := obj["Error"]; exists {
		if _, exists := obj["Pages"]; exists {
			return empty, ErrTencentWSAResponse
		}
		failure, e := wsaObject(rawError, []string{"Code", "Message"}, nil)
		if e != nil {
			return empty, ErrTencentWSAResponse
		}
		code, e := wsaString(failure["Code"], 100, false)
		if e != nil {
			return empty, ErrTencentWSAResponse
		}
		if _, e = wsaString(failure["Message"], 2048, true); e != nil {
			return empty, ErrTencentWSAResponse
		}
		classified := "PROVIDER_UNKNOWN"
		switch code {
		case "InternalError":
			classified = "TEMPORARY"
		case "InvalidParameter":
			classified = "INVALID_REQUEST"
		case "RequestLimitExceeded":
			classified = "RATE_LIMIT"
		case "ResourceNotFound", "ResourceUnavailable":
			classified = "RESOURCE_UNAVAILABLE"
		case "UnauthorizedOperation":
			classified = "AUTHENTICATION"
		}
		return empty, TencentWSAProviderError{Code: classified}
	}
	returnedQuery, err := wsaString(obj["Query"], 240, false)
	if err != nil || returnedQuery != query {
		return empty, ErrTencentWSAResponse
	}
	version := "UNKNOWN"
	if rawVersion, exists := obj["Version"]; exists {
		v, e := wsaString(rawVersion, 32, true)
		if e != nil {
			return empty, ErrTencentWSAResponse
		}
		if v != "" {
			if v != "standard" && v != "premium" && v != "lite" {
				return empty, ErrTencentWSAResponse
			}
			version = v
		}
	}
	if rawMsg, exists := obj["Msg"]; exists {
		msg, e := wsaString(rawMsg, 512, true)
		if e != nil {
			return empty, ErrTencentWSAResponse
		}
		if msg != "" {
			return empty, TencentWSAProviderError{Code: "QUERY_REJECTED"}
		}
	}
	var pages []string
	if len(obj["Pages"]) == 0 || bytes.Equal(bytes.TrimSpace(obj["Pages"]), []byte("null")) || json.Unmarshal(obj["Pages"], &pages) != nil || len(pages) > 50 {
		return empty, ErrTencentWSAResponse
	}
	result := TencentWSAResult{RequestID: id, Version: version, CashStatus: "UNKNOWN", Sources: []TencentWSASource{}}
	seen := make(map[string]bool)
	for _, page := range pages {
		source, e := parseTencentWSAPage([]byte(page))
		if e != nil {
			return empty, ErrTencentWSAResponse
		}
		if !seen[source.URL] && len(result.Sources) < TencentWSAMaxSources {
			result.Sources = append(result.Sources, source)
		}
		seen[source.URL] = true
	}
	return result, nil
}

func parseTencentWSAPage(raw []byte) (TencentWSASource, error) {
	var source TencentWSASource
	if len(raw) == 0 || len(raw) > 32*1024 {
		return source, ErrTencentWSAResponse
	}
	obj, err := wsaObject(raw, []string{"title", "url", "passage"}, []string{"date", "site", "content", "score", "images", "favicon"})
	if err != nil {
		return source, err
	}
	if source.Title, err = wsaString(obj["title"], 512, false); err != nil {
		return source, err
	}
	if source.URL, err = wsaString(obj["url"], 2048, false); err != nil {
		return source, err
	}
	if source.URL, err = wsaPublicURL(source.URL); err != nil {
		return source, err
	}
	if source.Passage, err = wsaDataString(obj["passage"], 4096); err != nil {
		return source, err
	}
	for _, field := range []struct {
		name  string
		limit int
		value *string
	}{{"date", 64, &source.Date}, {"site", 256, &source.Site}} {
		if v, exists := obj[field.name]; exists {
			if *field.value, err = wsaString(v, field.limit, true); err != nil {
				return source, err
			}
		}
	}
	if v, exists := obj["content"]; exists {
		if _, err = wsaDataString(v, 8192); err != nil {
			return source, err
		}
	}
	if v, exists := obj["favicon"]; exists {
		if _, err = wsaString(v, 2048, true); err != nil {
			return source, err
		}
	}
	if v, exists := obj["score"]; exists {
		var score float64
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) || json.Unmarshal(v, &score) != nil || score < 0 || score > 1 {
			return source, ErrTencentWSAResponse
		}
	}
	if v, exists := obj["images"]; exists {
		var images []json.RawMessage
		if len(v) > 16*1024 || bytes.Equal(bytes.TrimSpace(v), []byte("null")) || json.Unmarshal(v, &images) != nil || len(images) > 20 {
			return source, ErrTencentWSAResponse
		}
	}
	return source, nil
}

// No DNS lookup or page/media fetch is performed. Reject literal non-public IPs
// and local/internal names; DNS-backed links remain untrusted source metadata,
// not a network fetch authorization (DNS rebinding cannot be inferred here).
func wsaPublicURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\\\r\n\t") {
		return "", ErrTencentWSAResponse
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Contains(host, "%") || strings.HasSuffix(host, ".") {
		return "", ErrTencentWSAResponse
	}
	if port := u.Port(); port != "" && !((u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443")) {
		return "", ErrTencentWSAResponse
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return "", ErrTencentWSAResponse
		}
		for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4"} {
			if netip.MustParsePrefix(prefix).Contains(ip) {
				return "", ErrTencentWSAResponse
			}
		}
	} else {
		labels := strings.Split(host, ".")
		if len(labels) < 2 || len(host) > 253 {
			return "", ErrTencentWSAResponse
		}
		for _, suffix := range []string{"localhost", "localdomain", "local", "internal", "lan", "home", "home.arpa", "test", "invalid", "onion"} {
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return "", ErrTencentWSAResponse
			}
		}
		for _, label := range labels {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", ErrTencentWSAResponse
			}
			for _, ch := range label {
				if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
					return "", ErrTencentWSAResponse
				}
			}
		}
		// Reject alternative numeric IP spellings such as 127.1/0x7f.1.
		top := labels[len(labels)-1]
		alpha := false
		for _, ch := range top {
			alpha = alpha || ch >= 'a' && ch <= 'z'
		}
		if !alpha {
			return "", ErrTencentWSAResponse
		}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	u.Fragment = ""
	return u.String(), nil
}

func wsaRequestID(id string) bool {
	if len(id) != 36 || id == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, ch := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if ch != '-' {
				return false
			}
			continue
		}
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}
func wsaText(value string, max int, empty bool) bool {
	if len(value) > max || !utf8.ValidString(value) || strings.ContainsRune(value, utf8.RuneError) || (!empty && strings.TrimSpace(value) == "") {
		return false
	}
	for _, ch := range value {
		if ch < 32 || ch == 127 {
			return false
		}
	}
	return true
}
func wsaString(raw []byte, max int, empty bool) (string, error) {
	var value string
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || !wsaText(value, max, empty) {
		return "", ErrTencentWSAResponse
	}
	return value, nil
}
func wsaDataString(raw []byte, max int) (string, error) {
	var value string
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || len(value) > max || !utf8.ValidString(value) || strings.ContainsRune(value, utf8.RuneError) {
		return "", ErrTencentWSAResponse
	}
	for _, ch := range value {
		if (ch < 32 && ch != '\n' && ch != '\r' && ch != '\t') || ch == 127 {
			return "", ErrTencentWSAResponse
		}
	}
	return value, nil
}

// Strict at every JSON object boundary, including nested discarded media.
// Page JSON strings are parsed separately using the same duplicate/depth guard.
func wsaObject(raw []byte, required, optional []string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || wsaStrictJSON(raw) != nil {
		return nil, ErrTencentWSAResponse
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, ErrTencentWSAResponse
	}
	allowed := make(map[string]bool)
	for _, key := range required {
		allowed[key] = true
		if _, ok := obj[key]; !ok {
			return nil, ErrTencentWSAResponse
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range obj {
		if !allowed[key] {
			return nil, ErrTencentWSAResponse
		}
	}
	return obj, nil
}
func wsaStrictJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 16 {
			return ErrTencentWSAResponse
		}
		token, err := d.Token()
		if err != nil {
			return ErrTencentWSAResponse
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return ErrTencentWSAResponse
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return ErrTencentWSAResponse
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return ErrTencentWSAResponse
			}
		default:
			return ErrTencentWSAResponse
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrTencentWSAResponse
	}
	return nil
}
