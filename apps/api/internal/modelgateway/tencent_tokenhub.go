package modelgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"
)

const tencentChatEndpoint = TencentTokenHubBaseURL + "/chat/completions"

// TencentTokenHubAdapter translates only the selected text/no-tools contract.
// It has no actor, source, permission, approval, budget or tool executor port.
// Construction does not register it in Gateway or make OfflineHarness live.
type TencentTokenHubAdapter struct {
	config TencentTokenHubConfig
	client *http.Client
	now    func() time.Time
}

var _ ProviderAdapter = (*TencentTokenHubAdapter)(nil)

func (TencentTokenHubAdapter) String() string { return "TencentTokenHubAdapter{redacted}" }
func (TencentTokenHubAdapter) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "TencentTokenHubAdapter{redacted}")
}

// transport is a trusted server dependency/test seam, never client input.
// Nil uses HTTP/1 only, no connection reuse, no environment proxy and no
// replayable request body. Transport retries must not bypass native accounting.
func NewTencentTokenHubAdapter(c TencentTokenHubConfig, transport http.RoundTripper) (*TencentTokenHubAdapter, error) {
	if !c.valid() {
		return nil, ErrTencentConfig
	}
	if transport == nil {
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		transport = &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			Protocols:             protocols,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			MaxConnsPerHost:       1,
		}
	} else {
		v := reflect.ValueOf(transport)
		if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface || v.Kind() == reflect.Func || v.Kind() == reflect.Map || v.Kind() == reflect.Slice || v.Kind() == reflect.Chan) && v.IsNil() {
			return nil, ErrUnavailable
		}
	}
	return &TencentTokenHubAdapter{config: c, now: time.Now, client: &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (*TencentTokenHubAdapter) Descriptor() ProviderDescriptor {
	// hy3 is the selected provider route alias, not a verified immutable weight
	// revision. Runtime approval must not infer a stronger version guarantee.
	return ProviderDescriptor{ProviderID: "tencent_tokenhub", ModelID: TencentTokenHubModel, ModelVersion: TencentTokenHubModel, Mode: Live}
}

type tencentTextRequest struct {
	Model               string    `json:"model"`
	Messages            []Message `json:"messages"`
	MaxCompletionTokens int       `json:"max_completion_tokens"`
	N                   int       `json:"n"`
	Stream              bool      `json:"stream"`
	Thinking            struct {
		Type string `json:"type"`
	} `json:"thinking"`
	ToolChoice string `json:"tool_choice"`
}

func validateTencentRequest(r ProviderRequest, now time.Time, maxOutput int) error {
	if now.IsZero() || r.TaskKind != ActivityQuery || !identifier.MatchString(r.PromptVersion) || r.OutputMode != Text || r.OutputSchemaVersion != "air.answer.v1" || len(r.ToolAllowlist) != 0 || r.MaxOutputTokens < 1 || r.MaxOutputTokens > maxOutput || r.MaxOutputTokens > 4096 || len(r.Messages) < 1 || len(r.Messages) > 16 || !r.DeadlineAt.After(now) || r.DeadlineAt.After(now.Add(MaxDeadline)) {
		return ErrInvalid
	}
	size, users := 0, 0
	for i, m := range r.Messages {
		if !validMessage(m) || (m.Role != "user" && m.Role != "system") || (m.Role == "system" && i != 0) {
			return ErrInvalid
		}
		if m.Role == "user" {
			users++
		}
		size += len(m.Content)
	}
	if users == 0 || size > 16*1024 {
		return ErrInvalid
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > MaxRequestBytes {
		return ErrInvalid
	}
	return nil
}

func (a *TencentTokenHubAdapter) Complete(ctx context.Context, r ProviderRequest) ([]byte, error) {
	if a == nil || a.client == nil || a.now == nil || !a.config.valid() {
		return nil, ErrUnavailable
	}
	if ctx == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.Messages = append([]Message(nil), r.Messages...)
	body, err := formatTencentTextWire(r, a.now(), a.config.maxOutputTokens)
	if err != nil {
		return nil, err
	}
	callctx, cancel := context.WithDeadline(ctx, r.DeadlineAt)
	defer cancel()
	req, err := http.NewRequestWithContext(callctx, http.MethodPost, tencentChatEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrInvalid
	}
	// bytes.Reader otherwise installs GetBody, enabling hidden transport replay.
	req.GetBody = nil
	req.Close = true
	req.Header.Set("Authorization", "Bearer "+a.config.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if err := callctx.Err(); err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, tencentTransportError(callctx, err)
	}
	if resp == nil || resp.Body == nil {
		return nil, ErrAdapter
	}
	defer resp.Body.Close()
	if err := callctx.Err(); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, tencentHTTPError(resp, a.now())
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, ErrAdapter
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResultBytes+1))
	defer clear(raw)
	if err != nil {
		return nil, tencentTransportError(callctx, err)
	}
	if err := callctx.Err(); err != nil {
		return nil, err
	}
	if !r.DeadlineAt.After(a.now()) {
		return nil, ErrDeadline
	}
	normalized, err := normalizeTencentText(raw, r.MaxOutputTokens)
	if current := callctx.Err(); current != nil {
		return nil, current
	}
	if !r.DeadlineAt.After(a.now()) {
		return nil, ErrDeadline
	}
	return normalized, err
}

func tencentTransportError(ctx context.Context, err error) error {
	if current := ctx.Err(); current != nil {
		return current
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return ProviderError{Code: "UNKNOWN", Retryable: false}
}

func tencentHTTPError(resp *http.Response, now time.Time) error {
	code, retry := "UNKNOWN", false
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		code = "AUTHENTICATION"
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge:
		code = "INVALID_REQUEST"
	case http.StatusTooManyRequests:
		code, retry = "RATE_LIMIT", true
	default:
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			code, retry = "TEMPORARY", true
		}
	}
	p := ProviderError{Code: code, Retryable: retry}
	if retry && len(resp.Header.Values("Retry-After")) != 0 {
		values := resp.Header.Values("Retry-After")
		if len(values) != 1 {
			return NewRetryAfterError(p, -1)
		}
		return NewRetryAfterHeaderError(p, values[0], now)
	}
	return p
}

func decodeTencentInt(raw json.RawMessage, dst *int) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, dst) != nil {
		return ErrAdapter
	}
	return nil
}

func tencentNullOrEmptyArray(raw json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var items []json.RawMessage
	return json.Unmarshal(raw, &items) == nil && items != nil && len(items) == 0
}

func validateTencentNoSearchInfo(raw json.RawMessage) error {
	if raw == nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	info, err := strictObject(raw, nil, []string{"search_results"})
	if err != nil {
		return ErrAdapter
	}
	if results := info["search_results"]; results != nil && !tencentNullOrEmptyArray(results) {
		return ErrAdapter
	}
	return nil
}

func normalizeTencentText(raw []byte, maxOutput int) ([]byte, error) {
	obj, err := strictObject(raw, []string{"id", "model", "choices"}, []string{"object", "created", "usage", "search_info", "system_fingerprint", "service_tier"})
	if err != nil {
		return nil, ErrAdapter
	}
	if validateTencentNoSearchInfo(obj["search_info"]) != nil {
		return nil, ErrAdapter
	}
	id, e1 := stringValue(obj["id"], 80)
	model, e2 := stringValue(obj["model"], 80)
	if e1 != nil || e2 != nil || !safeRequestID.MatchString(id) || model != TencentTokenHubModel {
		return nil, ErrAdapter
	}
	if v := obj["object"]; v != nil {
		var object string
		if json.Unmarshal(v, &object) != nil || object != "chat.completion" {
			return nil, ErrAdapter
		}
	}
	var choices []json.RawMessage
	if json.Unmarshal(obj["choices"], &choices) != nil || len(choices) != 1 {
		return nil, ErrAdapter
	}
	choice, err := strictObject(choices[0], []string{"index", "message", "finish_reason"}, []string{"logprobs"})
	var index int
	if err != nil || decodeTencentInt(choice["index"], &index) != nil || index != 0 {
		return nil, ErrAdapter
	}
	message, err := strictObject(choice["message"], []string{"role"}, []string{"content", "refusal", "reasoning_content", "reasoning_details", "tool_calls", "function_call", "search_results", "annotations"})
	var role string
	if err != nil || json.Unmarshal(message["role"], &role) != nil || role != "assistant" {
		return nil, ErrAdapter
	}
	for _, field := range []string{"tool_calls", "function_call", "search_results", "annotations"} {
		if v := message[field]; v != nil && !tencentNullOrEmptyArray(v) {
			return nil, ErrAdapter
		}
	}
	finish, err := stringValue(choice["finish_reason"], 40)
	if err != nil {
		return nil, ErrAdapter
	}
	refusal := false
	if v := message["refusal"]; v != nil && !bytes.Equal(v, []byte("null")) {
		var text string
		if json.Unmarshal(v, &text) != nil {
			return nil, ErrAdapter
		}
		refusal = strings.TrimSpace(text) != ""
	}
	status, normalizedFinish, text := Completed, "stop", ""
	switch finish {
	case "stop":
		if refusal {
			status, normalizedFinish = Refused, "refusal"
		} else {
			text, err = stringValue(message["content"], 8192)
		}
	case "content_filter", "refusal":
		status, normalizedFinish = Refused, "refusal"
	case "length":
		status, normalizedFinish = Truncated, "length"
	default:
		return nil, ErrAdapter
	}
	if err != nil {
		return nil, ErrAdapter
	}
	usage, err := normalizeTencentUsage(obj["usage"], maxOutput)
	if err != nil {
		return nil, ErrAdapter
	}
	// This is the existing adapter wire, not a native LIVE execution receipt.
	// Usage is the provider's token report; absent monetary accounting is UNKNOWN.
	result := struct {
		Status       Status          `json:"status"`
		RequestID    string          `json:"request_id"`
		FinishReason string          `json:"finish_reason"`
		Text         string          `json:"text,omitempty"`
		Usage        json.RawMessage `json:"usage"`
	}{status, id, normalizedFinish, text, usage}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > MaxResultBytes {
		return nil, ErrAdapter
	}
	return encoded, nil
}

func normalizeTencentUsage(raw json.RawMessage, maxOutput int) (json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return json.RawMessage(`{"status":"UNKNOWN"}`), nil
	}
	obj, err := strictObject(raw, []string{"prompt_tokens", "completion_tokens", "total_tokens"}, []string{"cache_read_tokens", "cache_write_tokens", "prompt_tokens_details", "completion_tokens_details", "tool_usage"})
	var input, output, total int
	if err != nil || decodeTencentInt(obj["prompt_tokens"], &input) != nil || decodeTencentInt(obj["completion_tokens"], &output) != nil || decodeTencentInt(obj["total_tokens"], &total) != nil || input < 0 || input > 1_000_000 || output < 0 || output > maxOutput || total != input+output {
		return nil, ErrAdapter
	}
	for _, field := range []string{"cache_read_tokens", "cache_write_tokens"} {
		if raw := obj[field]; raw != nil {
			var cached int
			if decodeTencentInt(raw, &cached) != nil || cached < 0 || cached > input {
				return nil, ErrAdapter
			}
		}
	}
	if v := obj["tool_usage"]; v != nil && !bytes.Equal(v, []byte("null")) {
		tools, e := strictObject(v, nil, []string{"web_search_call"})
		var calls int
		if e != nil {
			return nil, ErrAdapter
		}
		if callsRaw := tools["web_search_call"]; callsRaw != nil && (decodeTencentInt(callsRaw, &calls) != nil || calls != 0) {
			return nil, ErrAdapter
		}
	}
	for _, pair := range []struct {
		field string
		bound int
		keys  []string
	}{{"prompt_tokens_details", input, []string{"cached_tokens", "audio_tokens"}}, {"completion_tokens_details", output, []string{"reasoning_tokens", "audio_tokens", "accepted_prediction_tokens", "rejected_prediction_tokens"}}} {
		if v := obj[pair.field]; v != nil && !bytes.Equal(v, []byte("null")) {
			details, e := strictObject(v, nil, pair.keys)
			if e != nil {
				return nil, ErrAdapter
			}
			for _, value := range details {
				var tokens int
				if decodeTencentInt(value, &tokens) != nil || tokens < 0 || tokens > pair.bound {
					return nil, ErrAdapter
				}
			}
		}
	}
	encoded, err := json.Marshal(struct {
		Status string `json:"status"`
		Input  int    `json:"input_tokens"`
		Output int    `json:"output_tokens"`
	}{"KNOWN", input, output})
	return encoded, err
}
