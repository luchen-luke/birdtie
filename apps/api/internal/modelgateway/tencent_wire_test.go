package modelgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTencentPreparedWireSharesExactFormatterAndUniversalBound(t *testing.T) {
	var expected PreparedTencentWire
	calls := 0
	a := tencentAdapter(t, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		body, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(body, expected.ExactWire()) || tencentWireDigest(body) != expected.WireDigest() {
			t.Fatal("actual transport differed from prepared bytes")
		}
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	r := tencentRequest()
	r.Messages[1].Content = "汉字😀<>&\n\"\\ current query"
	var err error
	expected, err = a.Prepare(r)
	if err != nil || !expected.valid(time.Now()) || calls != 0 || expected.InputTokenBound() != 196608 || expected.InputBoundEvidence() != "PROVIDER_MAX_INPUT_BOUND" || expected.InputBoundSource() != TencentLiveInputBoundSource || expected.WireContract() != TencentLiveWireContract || expected.MaxOutputTokens() != 128 {
		t.Fatal("preparation invented a lower tokenizer bound or dispatched")
	}
	if expected.InputTokenBound()+4*768 != 199680 {
		t.Fatal("universal per API bound arithmetic changed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(expected.ExactWire(), &fields) != nil || len(fields) != 7 || string(fields["thinking"]) != `{"type":"disabled"}` || string(fields["model"]) != `"hy3"` || string(fields["max_completion_tokens"]) != "128" {
		t.Fatal("prepared no-tools/thinking-disabled shape changed")
	}
	if _, err := a.Complete(context.Background(), r); err != nil || calls != 1 {
		t.Fatal("shared formatter changed existing transport behavior")
	}
	// Caller edits to either original messages or an exported byte copy cannot
	// lower/alter the private exact wire or fixed universal proof.
	copyBytes := expected.ExactWire()
	copyBytes[0] = '['
	r.Messages[1].Content = "mutated caller input"
	if !expected.valid(time.Now()) || strings.Contains(string(expected.ExactWire()), "mutated caller input") {
		t.Fatal("prepared payload was mutable through exported copies")
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if formatted := fmt.Sprintf(verb, expected); !strings.Contains(formatted, "redacted") || strings.Contains(formatted, r.Messages[0].Content) {
			t.Fatal("preparation formatting exposed messages")
		}
	}
	if raw, err := json.Marshal(expected); !errors.Is(err, ErrLivePreparation) || raw != nil {
		t.Fatal("preparation serialized as an approval")
	}
	if err := json.Unmarshal([]byte(`{}`), &expected); !errors.Is(err, ErrLivePreparation) || expected.valid(time.Now()) || len(expected.ExactWire()) != 0 {
		t.Fatal("preparation decoder retained data")
	}
}

func TestTencentPreparedWireRejectsUnsupportedSelectorsAndTamper(t *testing.T) {
	a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("preparation contacted transport")
		return nil, nil
	}))
	for name, mutate := range map[string]func(*ProviderRequest){
		"too_much_output": func(r *ProviderRequest) { r.MaxOutputTokens = 769 },
		"zero_output":     func(r *ProviderRequest) { r.MaxOutputTokens = 0 },
		"tools":           func(r *ProviderRequest) { r.ToolAllowlist = []string{"activity.search"} },
		"structured":      func(r *ProviderRequest) { r.OutputMode = Structured },
		"memory":          func(r *ProviderRequest) { r.TaskKind = MemoryCandidateExtraction },
		"context_role":    func(r *ProviderRequest) { r.Messages[1].Role = "context" },
		"expired":         func(r *ProviderRequest) { r.DeadlineAt = time.Now().Add(-time.Second) },
		"unbounded":       func(r *ProviderRequest) { r.DeadlineAt = time.Now().Add(3 * time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			r := tencentRequest()
			mutate(&r)
			if p, err := a.Prepare(r); !errors.Is(err, ErrLivePreparation) || len(p.ExactWire()) != 0 || p.valid(time.Now()) {
				t.Fatal("unsupported preparation accepted")
			}
		})
	}
	p, err := a.Prepare(tencentRequest())
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*PreparedTencentWire){
		"wire":     func(p *PreparedTencentWire) { p.wire += " " },
		"digest":   func(p *PreparedTencentWire) { p.digest = strings.Repeat("0", 64) },
		"messages": func(p *PreparedTencentWire) { p.request.Messages = []Message{{Role: "user", Content: "tampered"}} },
		"output":   func(p *PreparedTencentWire) { p.request.MaxOutputTokens = 769 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := p
			mutate(&changed)
			if changed.valid(time.Now()) {
				t.Fatal("tampered preparation accepted")
			}
		})
	}
	var nilAdapter *TencentTokenHubAdapter
	if _, err := nilAdapter.Prepare(tencentRequest()); !errors.Is(err, ErrLivePreparation) {
		t.Fatal("nil adapter prepared")
	}
}

func TestTencentPreparedWireMatchesExactCurrentProjectionOnly(t *testing.T) {
	a := tencentAdapter(t, nil)
	r := tencentRequest()
	p, err := a.Prepare(r)
	if err != nil || !p.Matches(r, time.Now()) {
		t.Fatal("current exact projection did not match")
	}
	for name, mutate := range map[string]func(*ProviderRequest){
		"other_query": func(r *ProviderRequest) {
			r.Messages = append([]Message(nil), r.Messages...)
			r.Messages[1].Content = "other current query"
		},
		"other_prompt":   func(r *ProviderRequest) { r.PromptVersion = "other.prompt.v1" },
		"other_deadline": func(r *ProviderRequest) { r.DeadlineAt = r.DeadlineAt.Add(-time.Second) },
		"other_output":   func(r *ProviderRequest) { r.MaxOutputTokens-- },
		"web_context": func(r *ProviderRequest) {
			r.Messages = append(append([]Message(nil), r.Messages...), Message{Role: "user", Content: "unapproved web passage"})
		},
		"tools": func(r *ProviderRequest) { r.ToolAllowlist = []string{"activity.search"} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := r
			mutate(&changed)
			if p.Matches(changed, time.Now()) {
				t.Fatal("different current native projection matched")
			}
		})
	}
	if p.Matches(r, r.DeadlineAt.Add(time.Second)) || (PreparedTencentWire{}).Matches(r, time.Now()) {
		t.Fatal("expired/zero proof matched")
	}
}

func TestTencentLiveWireGuardRejectsChangesAndReplayBeforeWire(t *testing.T) {
	a := tencentAdapter(t, nil)
	p, err := a.Prepare(tencentRequest())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"body", "endpoint", "method", "replay", "reuse", "revoked", "valid"} {
		t.Run(name, func(t *testing.T) {
			var attempts, wires atomic.Int32
			calls := 0
			guard := liveWireTransport{prepared: p, attempts: &attempts, wires: &wires, before: func() error {
				if name == "revoked" {
					return ErrUnavailable
				}
				return nil
			}, next: tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(req.Body)
				if !bytes.Equal(raw, p.ExactWire()) {
					t.Fatal("guard changed exact bytes")
				}
				return tencentResponse(tencentSuccess(), 200), nil
			})}
			makeRequest := func() *http.Request {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, tencentChatEndpoint, bytes.NewReader(p.ExactWire()))
				req.GetBody = nil
				req.Close = true
				return req
			}
			req := makeRequest()
			switch name {
			case "body":
				req.Body = io.NopCloser(strings.NewReader(`{}`))
			case "endpoint":
				req.URL.Host = "example.org"
			case "method":
				req.Method = http.MethodGet
			case "replay":
				req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(p.ExactWire())), nil }
			case "reuse":
				req.Close = false
			}
			resp, err := guard.RoundTrip(req)
			if name == "valid" {
				if err != nil || calls != 1 || wires.Load() != 1 {
					t.Fatal("valid exact wire rejected")
				}
				_ = resp.Body.Close()
			} else if err == nil || calls != 0 || wires.Load() != 0 {
				t.Fatal("invalid guard counted or sent a wire")
			}
			if _, err := guard.RoundTrip(makeRequest()); name == "valid" && (err == nil || calls != 1 || wires.Load() != 1) {
				t.Fatal("guard replayed wire")
			}
		})
	}
}

func TestTencentLiveWireGuardBodyRemainsExactAfterEarlyRoundTripResponse(t *testing.T) {
	adapter := tencentAdapter(t, nil)
	prepared, err := adapter.Prepare(tencentRequest())
	if err != nil {
		t.Fatal("local wire preparation failed")
	}
	var attempts, wires atomic.Int32
	var retainedBody io.ReadCloser
	guard := liveWireTransport{prepared: prepared, attempts: &attempts, wires: &wires, before: func() error { return nil }, next: tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		// A transport can return early response headers while its body writer
		// still owns the reader. Neither it nor an exported byte copy may share
		// a mutable buffer that the guard clears after RoundTrip returns.
		retainedBody = req.Body
		return tencentResponse(tencentSuccess(), 503), nil
	})}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, tencentChatEndpoint, bytes.NewReader(prepared.ExactWire()))
	req.GetBody = nil
	req.Close = true
	response, err := guard.RoundTrip(req)
	if err != nil || retainedBody == nil {
		t.Fatal("local early-response guard failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(retainedBody)
	_ = retainedBody.Close()
	if err != nil || !bytes.Equal(body, prepared.ExactWire()) || tencentWireDigest(body) != prepared.WireDigest() || attempts.Load() != 1 || wires.Load() != 1 {
		t.Fatal("early-response cleanup changed approved wire bytes")
	}
}
