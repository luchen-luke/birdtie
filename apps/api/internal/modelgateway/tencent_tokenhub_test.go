package modelgateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type tencentRoundTripper func(*http.Request) (*http.Response, error)

func (f tencentRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func tencentRequest() ProviderRequest {
	return ProviderRequest{TaskKind: ActivityQuery, PromptVersion: "test.tencent.v1", OutputMode: Text,
		OutputSchemaVersion: "air.answer.v1", ToolAllowlist: []string{}, MaxOutputTokens: 128,
		Messages: []Message{{Role: "system", Content: "只回答当前公开问题。"}, {Role: "user", Content: "请解释城市地点检索。"}}, DeadlineAt: time.Now().Add(time.Minute)}
}

func tencentResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func tencentSuccess() string {
	return `{"id":"chatcmpl-local1","object":"chat.completion","model":"hy3","search_info":null,"choices":[{"index":0,"message":{"role":"assistant","content":"当前地点需要核验来源。","reasoning_content":"reasoning-secret-sentinel"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":12,"total_tokens":43,"cache_read_tokens":4,"cache_write_tokens":2,"completion_tokens_details":{"reasoning_tokens":2},"prompt_tokens_details":{"cached_tokens":4},"tool_usage":{"web_search_call":0}}}`
}

func tencentAdapter(t *testing.T, rt http.RoundTripper) *TencentTokenHubAdapter {
	t.Helper()
	a, err := NewTencentTokenHubAdapter(tencentTestConfig(t), rt)
	if err != nil {
		t.Fatal("synthetic transport construction failed")
	}
	return a
}

func TestTencentTransportSuccessHasExactNoSearchPayloadAndUsageFacts(t *testing.T) {
	calls := 0
	native := testRequest(Text)
	native.Messages = tencentRequest().Messages
	native.DeadlineAt = time.Now().Add(time.Minute)
	native.Budget.MaxOutputTokens = 128
	native.ToolAllowlist = []string{}
	r := providerRequest(native)
	a := tencentAdapter(t, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodPost || req.URL.String() != tencentChatEndpoint || req.URL.Scheme != "https" || req.URL.RawQuery != "" || req.URL.User != nil || req.URL.Fragment != "" {
			t.Fatal("transport endpoint or method changed")
		}
		if req.Header.Get("Authorization") != "Bearer "+tencentTestKey || req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Accept") != "application/json" {
			t.Fatal("expected protected header not applied")
		}
		if req.GetBody != nil || !req.Close || req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != "" {
			t.Fatal("request permits hidden replay or connection reuse")
		}
		deadline, ok := req.Context().Deadline()
		if !ok || deadline.After(r.DeadlineAt) {
			t.Fatal("actual HTTP call lost the original deadline")
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil || len(raw) > MaxRequestBytes {
			t.Fatal("bounded HTTP body unavailable")
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) != nil || len(body) != 7 {
			t.Fatal("provider request has unapproved fields")
		}
		for key, expected := range map[string]string{"model": `"hy3"`, "n": "1", "stream": "false", "max_completion_tokens": "128", "thinking": `{"type":"disabled"}`, "tool_choice": `"none"`} {
			if string(body[key]) != expected {
				t.Fatal("provider control is not pinned:", key)
			}
		}
		for _, field := range []string{"tools", "web_search_options", "max_tokens", "reasoning_effort", "store", "user", "run_id", "agent_ref", "budget_ref", "context_snapshot_ref"} {
			if body[field] != nil {
				t.Fatal("native selector or unapproved capability entered provider payload")
			}
		}
		headers, _ := json.Marshal(req.Header)
		for _, id := range []string{native.RunID, native.Agent.AgentID, native.Agent.Principal.ID, native.ContextSnapshotRef, native.DataPolicyRef, native.BudgetRef} {
			if strings.Contains(string(raw), id) || strings.Contains(string(headers), id) || strings.Contains(req.URL.String(), id) {
				t.Fatal("native ID crossed the transport boundary")
			}
		}
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	raw, err := a.Complete(context.Background(), r)
	if err != nil || calls != 1 {
		t.Fatal("single synthetic HTTP completion failed")
	}
	obj, err := strictObject(raw, []string{"status", "request_id", "finish_reason", "text", "usage"}, nil)
	if err != nil || string(obj["status"]) != `"COMPLETED"` || string(obj["text"]) != `"当前地点需要核验来源。"` {
		t.Fatal("existing normalized text contract not produced")
	}
	u, err := parseUsage(obj["usage"], r.MaxOutputTokens)
	if err != nil || u.Status != "KNOWN" || *u.InputTokens != 31 || *u.OutputTokens != 12 || u.CostStatus != "UNKNOWN" {
		t.Fatal("token facts were lost or fabricated into monetary accounting")
	}
	for _, secret := range []string{tencentTestKey, "reasoning-secret-sentinel", "reasoning_content", "reasoning_details"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private transport/reasoning material released")
		}
	}
}

func TestTencentDefaultTransportUsesOneHTTP1WireAttempt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		drop   bool
	}{{"success", 200, false}, {"rate_limit", 429, false}, {"temporary", 503, false}, {"redirect", 307, false}, {"response_lost", 0, true}} {
		t.Run(tc.name, func(t *testing.T) {
			a := tencentAdapter(t, nil)
			transport, ok := a.client.Transport.(*http.Transport)
			if !ok || transport.Protocols == nil || !transport.Protocols.HTTP1() || transport.Protocols.HTTP2() || transport.Protocols.UnencryptedHTTP2() || transport.ForceAttemptHTTP2 || !transport.DisableKeepAlives || transport.Proxy != nil {
				t.Fatal("default transport admits HTTP2, reuse or an environment proxy")
			}
			defer transport.CloseIdleConnections()
			var dials, wires atomic.Int64
			observed := make(chan error, 8)
			// A pipe exercises net/http's actual HTTP/1 transport/parser without
			// opening a socket, resolving DNS or contacting a model service.
			transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
				return nil, errors.New("unexpected non-TLS dial")
			}
			transport.DialTLSContext = func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				client, server := net.Pipe()
				go func() {
					defer server.Close()
					_ = server.SetDeadline(time.Now().Add(2 * time.Second))
					req, err := http.ReadRequest(bufio.NewReader(server))
					if err != nil {
						observed <- err
						return
					}
					wires.Add(1)
					body, err := io.ReadAll(req.Body)
					_ = req.Body.Close()
					var fields map[string]json.RawMessage
					if err != nil || req.Proto != "HTTP/1.1" || !req.Close || req.Host != "tokenhub.tencentmaas.com" || json.Unmarshal(body, &fields) != nil || string(fields["thinking"]) != `{"type":"disabled"}` || string(fields["max_completion_tokens"]) != "128" {
						observed <- errors.New("wire contract changed")
						return
					}
					if tc.drop {
						observed <- nil
						return // Remote response loss must not cause a second POST.
					}
					bodyText := tencentSuccess()
					header := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nLocation: https://example.org/replay\r\n\r\n", tc.status, http.StatusText(tc.status), len(bodyText))
					_, err = io.WriteString(server, header+bodyText)
					observed <- err
				}()
				return client, nil
			}
			// Check the actual request before handing it to the default transport.
			a.client.Transport = tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
				if req.GetBody != nil || !req.Close {
					return nil, errors.New("replayable body")
				}
				return transport.RoundTrip(req)
			})
			request := tencentRequest()
			request.DeadlineAt = time.Now().Add(2 * time.Second)
			raw, err := a.Complete(context.Background(), request)
			if (tc.status == 200 && (err != nil || len(raw) == 0)) || (tc.status != 200 && (err == nil || raw != nil)) || dials.Load() != 1 || wires.Load() != 1 {
				t.Fatal("default transport did not keep exactly one wire attempt")
			}
			select {
			case pipeErr := <-observed:
				// Error responses may close before their unused body finishes.
				if pipeErr != nil && !errors.Is(pipeErr, io.ErrClosedPipe) {
					t.Fatal("local wire fixture failed")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("local wire fixture did not finish")
			}
		})
	}
}

func TestTencentTransportAcceptsOnlySemanticallyEmptyUnrequestedToolsAndSearch(t *testing.T) {
	for _, searchInfo := range []string{"null", "{ }", `{"search_results":null}`, `{"search_results":[ ]}`} {
		t.Run(searchInfo, func(t *testing.T) {
			body := strings.Replace(tencentSuccess(), `"search_info":null`, `"search_info":`+searchInfo, 1)
			body = strings.Replace(body, `"role":"assistant"`, `"role":"assistant","tool_calls":[ ],"function_call":null,"search_results":[\n\t ],"annotations":[]`, 1)
			body = strings.ReplaceAll(body, `\n\t `, "\n\t ")
			a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) { return tencentResponse(body, 200), nil }))
			raw, err := a.Complete(context.Background(), tencentRequest())
			if err != nil || strings.Contains(string(raw), "search_info") || strings.Contains(string(raw), "tool_calls") || strings.Contains(string(raw), "cache_read_tokens") {
				t.Fatal("empty provider metadata was rejected or released as an execution fact")
			}
		})
	}
}

func TestTencentTransportRefusalTruncationAndUnknownUsage(t *testing.T) {
	for _, tc := range []struct{ name, body, status, finish string }{
		{"refusal", `{"id":"local-refused","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":null,"refusal":"private refusal sentinel"},"finish_reason":"stop"}]}`, "REFUSED", "refusal"},
		{"filter", `{"id":"local-filter","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"partial private sentinel"},"finish_reason":"content_filter"}]}`, "REFUSED", "refusal"},
		{"truncated", `{"id":"local-length","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"partial private sentinel"},"finish_reason":"length"}]}`, "TRUNCATED", "length"},
		{"missing_usage", `{"id":"local-unknown","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"本地文本"},"finish_reason":"stop"}]}`, "COMPLETED", "stop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) { return tencentResponse(tc.body, 200), nil }))
			raw, err := a.Complete(context.Background(), tencentRequest())
			var obj map[string]json.RawMessage
			if err != nil || json.Unmarshal(raw, &obj) != nil || string(obj["status"]) != `"`+tc.status+`"` || string(obj["finish_reason"]) != `"`+tc.finish+`"` {
				t.Fatal("status not normalized")
			}
			if tc.status != "COMPLETED" && obj["text"] != nil {
				t.Fatal("refused or truncated content released")
			}
			u, err := parseUsage(obj["usage"], 128)
			if err != nil || u.Status != "UNKNOWN" || u.InputTokens != nil || u.OutputTokens != nil || u.CostStatus != "UNKNOWN" {
				t.Fatal("missing usage became a zero or paid receipt")
			}
		})
	}
}

func TestTencentTransportSanitizesHTTPFailureAndNeverRetriesOrRedirects(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		retry  bool
	}{
		{401, "AUTHENTICATION", false}, {403, "AUTHENTICATION", false}, {400, "INVALID_REQUEST", false},
		{429, "RATE_LIMIT", true}, {500, "TEMPORARY", true}, {302, "UNKNOWN", false},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			calls := 0
			a := tencentAdapter(t, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Host != "tokenhub.tencentmaas.com" {
					t.Fatal("redirect reached an alternate host")
				}
				resp := tencentResponse("upstream-secret-body "+tencentTestKey, tc.status)
				resp.Header.Set("Location", "https://example.org/secret-redirect")
				resp.Header.Set("Retry-After", "7")
				return resp, nil
			}))
			raw, err := a.Complete(context.Background(), tencentRequest())
			var provider ProviderError
			if err == nil || raw != nil || !errors.As(err, &provider) || provider.Code != tc.code || provider.Retryable != tc.retry || calls != 1 {
				t.Fatal("HTTP status mapping/retry boundary failed")
			}
			for _, text := range []string{tencentTestKey, "upstream-secret-body", "example.org", "secret-redirect", "tokenhub.tencentmaas.com"} {
				if strings.Contains(err.Error(), text) {
					t.Fatal("upstream error or endpoint leaked")
				}
			}
			if tc.retry {
				delay, present, valid := RetryAfter(err)
				if !present || !valid || delay != 7*time.Second {
					t.Fatal("safe retry metadata lost")
				}
			}
		})
	}
}

func TestTencentTransportInvalidRequestNeverCallsHTTP(t *testing.T) {
	for name, change := range map[string]func(*ProviderRequest){
		"structured":         func(r *ProviderRequest) { r.OutputMode = Structured },
		"memory":             func(r *ProviderRequest) { r.TaskKind = MemoryCandidateExtraction },
		"tools":              func(r *ProviderRequest) { r.ToolAllowlist = []string{"activity.search"} },
		"context":            func(r *ProviderRequest) { r.Messages[1].Role = "context" },
		"prompt":             func(r *ProviderRequest) { r.PromptVersion = "" },
		"schema":             func(r *ProviderRequest) { r.OutputSchemaVersion = "other" },
		"system_order":       func(r *ProviderRequest) { r.Messages[1].Role = "system" },
		"empty":              func(r *ProviderRequest) { r.Messages = nil },
		"zero_tokens":        func(r *ProviderRequest) { r.MaxOutputTokens = 0 },
		"over_config_tokens": func(r *ProviderRequest) { r.MaxOutputTokens = 769 },
		"expired":            func(r *ProviderRequest) { r.DeadlineAt = time.Now().Add(-time.Second) },
		"far_deadline":       func(r *ProviderRequest) { r.DeadlineAt = time.Now().Add(3 * time.Minute) },
		"nul":                func(r *ProviderRequest) { r.Messages[1].Content = "invalid\x00message" },
		"large_message":      func(r *ProviderRequest) { r.Messages[1].Content = strings.Repeat("x", 4097) },
		"large_encoded": func(r *ProviderRequest) {
			r.Messages = []Message{{Role: "system", Content: strings.Repeat("\x01", 4096)}, {Role: "user", Content: strings.Repeat("\x01", 4096)}}
		},
		"large_total": func(r *ProviderRequest) {
			r.Messages = nil
			for range 5 {
				r.Messages = append(r.Messages, Message{Role: "user", Content: strings.Repeat("x", 4096)})
			}
		},
		"many_messages": func(r *ProviderRequest) {
			r.Messages = nil
			for range 17 {
				r.Messages = append(r.Messages, Message{Role: "user", Content: "text"})
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				return tencentResponse(tencentSuccess(), 200), nil
			}))
			r := tencentRequest()
			change(&r)
			if raw, err := a.Complete(context.Background(), r); !errors.Is(err, ErrInvalid) || raw != nil || calls != 0 {
				t.Fatal("invalid contract reached transport")
			}
		})
	}
}

func TestTencentTransportDeadlineCancellationAndRedactedNetworkErrors(t *testing.T) {
	t.Run("actual_deadline", func(t *testing.T) {
		calls := 0
		a := tencentAdapter(t, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
			calls++
			<-req.Context().Done()
			return nil, req.Context().Err()
		}))
		r := tencentRequest()
		r.DeadlineAt = time.Now().Add(30 * time.Millisecond)
		if raw, err := a.Complete(context.Background(), r); !errors.Is(err, context.DeadlineExceeded) || raw != nil || calls != 1 {
			t.Fatal("actual call was not deadline bounded")
		}
	})
	t.Run("cancel_before", func(t *testing.T) {
		calls := 0
		a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if raw, err := a.Complete(ctx, tencentRequest()); !errors.Is(err, context.Canceled) || raw != nil || calls != 0 {
			t.Fatal("cancelled query dispatched")
		}
	})
	t.Run("cancel_after", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
			cancel()
			return tencentResponse(tencentSuccess(), 200), nil
		}))
		if raw, err := a.Complete(ctx, tencentRequest()); !errors.Is(err, context.Canceled) || raw != nil {
			t.Fatal("cancelled response released")
		}
	})
	t.Run("late_clock", func(t *testing.T) {
		r := tencentRequest()
		var a *TencentTokenHubAdapter
		a = tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
			a.now = func() time.Time { return r.DeadlineAt }
			return tencentResponse(tencentSuccess(), 200), nil
		}))
		if raw, err := a.Complete(context.Background(), r); !errors.Is(err, ErrDeadline) || raw != nil {
			t.Fatal("late provider text released")
		}
	})
	t.Run("network_error", func(t *testing.T) {
		a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("https://private-url " + tencentTestKey)
		}))
		raw, err := a.Complete(context.Background(), tencentRequest())
		var provider ProviderError
		if raw != nil || !errors.As(err, &provider) || provider.Code != "UNKNOWN" || strings.Contains(err.Error(), tencentTestKey) || strings.Contains(err.Error(), "private-url") {
			t.Fatal("network error retained private details")
		}
	})
}

func TestTencentTransportRejectsUnexpectedProviderShapes(t *testing.T) {
	base := tencentSuccess()
	for name, raw := range map[string]string{
		"duplicate":                 strings.Replace(base, `"id":`, `"id":"other","id":`, 1),
		"model":                     strings.Replace(base, `"hy3"`, `"other-model"`, 1),
		"object":                    strings.Replace(base, `"chat.completion"`, `"chat.completion.chunk"`, 1),
		"request_id":                strings.Replace(base, "chatcmpl-local1", "https://secret-id", 1),
		"choice_index":              strings.Replace(base, `"index":0`, `"index":1`, 1),
		"no_choices":                strings.Replace(base, `"choices":[`, `"choices":[],"other":[`, 1),
		"empty_content":             strings.Replace(base, "当前地点需要核验来源。", "", 1),
		"huge_content":              strings.Replace(base, "当前地点需要核验来源。", strings.Repeat("x", 8193), 1),
		"tool_calls":                strings.Replace(base, `"role":"assistant"`, `"role":"assistant","tool_calls":[{"id":"tool"}]`, 1),
		"search_results":            strings.Replace(base, `"role":"assistant"`, `"role":"assistant","search_results":[{"url":"https://example.org"}]`, 1),
		"search_info":               strings.Replace(base, `"search_info":null`, `"search_info":{"search_results":[{"url":"https://example.org"}]}`, 1),
		"search_info_unknown":       strings.Replace(base, `"search_info":null`, `"search_info":{"query":""}`, 1),
		"search_info_duplicate":     strings.Replace(base, `"search_info":null`, `"search_info":{"search_results":[],"search_results":[]}`, 1),
		"search_info_array":         strings.Replace(base, `"search_info":null`, `"search_info":[]`, 1),
		"search_info_wrong_results": strings.Replace(base, `"search_info":null`, `"search_info":{"search_results":{}}`, 1),
		"tool_calls_object":         strings.Replace(base, `"role":"assistant"`, `"role":"assistant","tool_calls":{}`, 1),
		"cache_read_negative":       strings.Replace(base, `"cache_read_tokens":4`, `"cache_read_tokens":-1`, 1),
		"cache_read_large":          strings.Replace(base, `"cache_read_tokens":4`, `"cache_read_tokens":32`, 1),
		"cache_read_fractional":     strings.Replace(base, `"cache_read_tokens":4`, `"cache_read_tokens":0.5`, 1),
		"cache_read_null":           strings.Replace(base, `"cache_read_tokens":4`, `"cache_read_tokens":null`, 1),
		"cache_write_negative":      strings.Replace(base, `"cache_write_tokens":2`, `"cache_write_tokens":-1`, 1),
		"cache_write_large":         strings.Replace(base, `"cache_write_tokens":2`, `"cache_write_tokens":32`, 1),
		"cache_write_string":        strings.Replace(base, `"cache_write_tokens":2`, `"cache_write_tokens":"2"`, 1),
		"search_usage":              strings.Replace(base, `"web_search_call":0`, `"web_search_call":1`, 1),
		"usage_partial":             strings.Replace(base, `"prompt_tokens":31,`, "", 1),
		"usage_negative":            strings.Replace(base, `"prompt_tokens":31`, `"prompt_tokens":-31`, 1),
		"usage_total":               strings.Replace(base, `"total_tokens":43`, `"total_tokens":44`, 1),
		"usage_output":              strings.Replace(base, `"completion_tokens":12`, `"completion_tokens":129`, 1),
		"usage_reasoning":           strings.Replace(base, `"reasoning_tokens":2`, `"reasoning_tokens":13`, 1),
		"finish":                    strings.Replace(base, `"stop"`, `"tool_calls"`, 1),
		"trailing":                  base + "{}",
		"oversized":                 strings.Repeat(" ", MaxResultBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) { return tencentResponse(raw, 200), nil }))
			if result, err := a.Complete(context.Background(), tencentRequest()); !errors.Is(err, ErrAdapter) || result != nil {
				t.Fatal("unexpected provider shape released an answer")
			}
		})
	}
	for _, contentType := range []string{"text/html", "", "application/json;invalid"} {
		t.Run("content_type_"+contentType, func(t *testing.T) {
			a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				resp := tencentResponse(base, 200)
				resp.Header.Set("Content-Type", contentType)
				return resp, nil
			}))
			if result, err := a.Complete(context.Background(), tencentRequest()); !errors.Is(err, ErrAdapter) || result != nil {
				t.Fatal("non-JSON provider body accepted")
			}
		})
	}
}

type tencentCountingBody struct {
	remaining, read int
	closed          bool
}

func (b *tencentCountingBody) Read(dst []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(dst), b.remaining)
	for i := range n {
		dst[i] = ' '
	}
	b.read += n
	b.remaining -= n
	return n, nil
}
func (b *tencentCountingBody) Close() error { b.closed = true; return nil }

func TestTencentTransportResponseReadLimitAndNoGatewayActivation(t *testing.T) {
	body := &tencentCountingBody{remaining: MaxResultBytes * 2}
	calls := 0
	a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		resp := tencentResponse("", 200)
		resp.Body = body
		return resp, nil
	}))
	if raw, err := a.Complete(context.Background(), tencentRequest()); !errors.Is(err, ErrAdapter) || raw != nil || body.read != MaxResultBytes+1 || !body.closed {
		t.Fatal("provider response exceeded bounded read or was not closed")
	}
	if a.Descriptor().Mode != Live || a.Descriptor().ModelID != TencentTokenHubModel {
		t.Fatal("real transport was labelled as an offline fixture")
	}
	if _, err := NewOfflineHarness(a); !errors.Is(err, ErrUnavailable) || calls != 1 {
		t.Fatal("OfflineHarness accepted LIVE transport")
	}
	request := testRequest(Text)
	request.DeadlineAt = time.Now().Add(time.Minute)
	for _, gate := range []LiveGate{nil, &fakeGate{false}, &fakeGate{true}} {
		result, err := NewGateway(gate).Complete(context.Background(), request)
		if !errors.Is(err, ErrUnavailable) || result.Mode != Disabled || result.Text != "" || calls != 1 {
			t.Fatal("default gateway became a LIVE route")
		}
	}
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(verb, a), tencentTestKey) {
			t.Fatal("adapter formatting leaked key")
		}
	}
	var typedNil tencentRoundTripper
	if _, err := NewTencentTokenHubAdapter(tencentTestConfig(t), typedNil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("typed nil transport accepted")
	}
	if raw, err := a.Complete(nil, tencentRequest()); !errors.Is(err, ErrInvalid) || raw != nil {
		t.Fatal("nil context accepted")
	}
}
