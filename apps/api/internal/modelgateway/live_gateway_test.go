package modelgateway

import (
	"bufio"
	"bytes"
	"context"
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

// Trusted in-memory test port only. It is not NativeStore/accounting evidence.
type liveTestPort struct {
	checks, releases, settles atomic.Int32
	denied                    atomic.Bool
	onCheck                   func(context.Context, Request, PreparedTencentWire) error
	onRelease                 func(context.Context, Request, PreparedTencentWire, func(context.Context) ([]byte, error)) ([]byte, error)
	onSettle                  func(context.Context, Request, PreparedTencentWire) error
}

func (p *liveTestPort) CheckCurrent(ctx context.Context, r Request, w PreparedTencentWire) error {
	p.checks.Add(1)
	if p.denied.Load() {
		return errors.New("private native source sentinel")
	}
	if p.onCheck != nil {
		return p.onCheck(ctx, r, w)
	}
	return nil
}
func (p *liveTestPort) ReleaseOnce(ctx context.Context, r Request, w PreparedTencentWire, send func(context.Context) ([]byte, error)) ([]byte, error) {
	p.releases.Add(1)
	if p.onRelease != nil {
		return p.onRelease(ctx, r, w, send)
	}
	return send(ctx)
}
func (p *liveTestPort) SettleUnknown(ctx context.Context, r Request, w PreparedTencentWire) error {
	p.settles.Add(1)
	if p.onSettle != nil {
		return p.onSettle(ctx, r, w)
	}
	return nil
}
func liveTestRequest() Request {
	r := testRequest(Text)
	r.Messages = tencentRequest().Messages
	r.ToolAllowlist = []string{}
	r.DeadlineAt = time.Now().Add(time.Minute)
	return r
}
func liveTestGateway(t *testing.T, p *liveTestPort, rt http.RoundTripper) (*Gateway, *TencentTokenHubAdapter) {
	t.Helper()
	a := tencentAdapter(t, rt)
	g, err := NewNativeLiveGateway(&fakeGate{true}, a, p)
	if err != nil {
		t.Fatal("trusted test gateway rejected")
	}
	return g, a
}
func assertNoLiveText(t *testing.T, result Result, err error) {
	t.Helper()
	if err == nil || result.Text != "" || result.Answer != nil || result.Candidate != nil || len(result.ToolProposals) != 0 || result.Usage.CostStatus != "UNKNOWN" || strings.Contains(err.Error(), "sentinel") {
		t.Fatal("failed native boundary released text or sensitive error")
	}
}

func TestNativeLiveGatewayCannotBorrowHY3ProofForSelectedDeepSeek(t *testing.T) {
	c := tencentTestConfig(t)
	c.model = TencentTokenHubDeepSeekModel
	wires := 0
	a, err := NewTencentTokenHubAdapter(c, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		wires++
		return nil, ErrAdapter
	}))
	if err != nil {
		t.Fatal("selected transport unavailable")
	}
	p := &liveTestPort{}
	if g, err := NewNativeLiveGateway(&fakeGate{true}, a, p); err != nil || g == nil || wires != 0 || p.checks.Load() != 0 || p.releases.Load() != 0 || p.settles.Load() != 0 {
		t.Fatal("selected model construction consumed a dispatch or lost its private proof")
	}
	// Changing an adapter after preparation cannot send the older model's
	// approved wire under a different selected-model descriptor.
	g, hy3 := liveTestGateway(t, p, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		wires++
		return nil, ErrAdapter
	}))
	r := liveTestRequest()
	prepared, err := hy3.Prepare(providerRequest(r))
	if err != nil {
		t.Fatal("legacy preparation rejected")
	}
	g.live.adapter = a
	if err := g.currentLive(context.Background(), r, prepared); !errors.Is(err, ErrUnavailable) || wires != 0 || p.checks.Load() != 0 {
		t.Fatal("current native guard accepted mismatched prepared and adapter models")
	}
	p.onCheck = func(_ context.Context, _ Request, got PreparedTencentWire) error {
		if got.Descriptor() != prepared.Descriptor() || got.WireDigest() != prepared.WireDigest() {
			return ErrUnavailable // The old native approval remains tied to HY3.
		}
		return nil
	}
	if result, err := g.Complete(context.Background(), r); err == nil || result.Text != "" || wires != 0 || p.releases.Load() != 0 || p.settles.Load() != 0 {
		t.Fatal("changed model released native text or invoked provider")
	}
}

func TestNativeLiveNormalizedResultRequiresOriginalNativeModelProfile(t *testing.T) {
	a := tencentAdapter(t, nil)
	r := liveTestRequest()
	p, err := a.Prepare(providerRequest(r))
	if err != nil {
		t.Fatal("legacy preparation rejected")
	}
	normalized, err := normalizeTencentText([]byte(tencentSuccess()), r.Budget.MaxOutputTokens)
	if err != nil {
		t.Fatal("synthetic normalization failed")
	}
	result, err := normalizeLiveTencentText(r, normalized, p)
	if err != nil || result.ProviderID != p.Descriptor().ProviderID || result.ProviderModelVersion != p.Descriptor().ModelID+"@"+p.Descriptor().ModelVersion {
		t.Fatal("live result model was not bound to its private preparation")
	}
	p.profile, _ = tencentProfileForModel(TencentTokenHubDeepSeekModel)
	if result, err := normalizeLiveTencentText(r, normalized, p); !errors.Is(err, ErrAdapter) || result.Text != "" {
		t.Fatal("changed model profile normalized the other route's wire")
	}
}

func TestNativeLiveGatewayDeepSeekUsesPrivateByteProofAndOriginalReleaseBoundary(t *testing.T) {
	c := tencentTestConfig(t)
	c.model = TencentTokenHubDeepSeekModel
	p := &liveTestPort{}
	wires := 0
	var expected PreparedTencentWire
	a, err := NewTencentTokenHubAdapter(c, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		wires++
		if p.releases.Load() != 1 || p.checks.Load() < 3 || p.settles.Load() != 0 {
			t.Fatal("selected model wire preceded native release")
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(raw, expected.ExactWire()) || tencentWireDigest(raw) != expected.WireDigest() {
			t.Fatal("selected model wire changed after native approval")
		}
		return tencentResponse(strings.Replace(tencentSuccess(), `"model":"hy3"`, `"model":"deepseek-v4-pro-0813"`, 1), 200), nil
	}))
	if err != nil {
		t.Fatal("selected adapter rejected")
	}
	r := liveTestRequest()
	expected, err = a.Prepare(providerRequest(r))
	if err != nil {
		t.Fatal("private byte preparation rejected")
	}
	p.onCheck = func(ctx context.Context, got Request, w PreparedTencentWire) error {
		if ctx.Err() != nil || got.RunID != r.RunID || got.Agent != r.Agent || w.Descriptor() != a.Descriptor() || w.InputTokenBound() != 16384 || w.InputBoundEvidence() != "TOKENIZER_BYTE_BPE_UPPER_BOUND" || w.InputBoundSource() != TencentDeepSeekInputBoundSource || w.WireDigest() != expected.WireDigest() || !w.Matches(providerRequest(r), time.Now()) {
			t.Fatal("selected native price/wire projection lost exact model proof")
		}
		return nil
	}
	g, err := NewNativeLiveGateway(&fakeGate{true}, a, p)
	if err != nil {
		t.Fatal("selected private native route rejected")
	}
	result, err := g.Complete(context.Background(), r)
	if err != nil || result.Text == "" || result.ProviderID != "tencent_tokenhub" || result.ProviderModelVersion != "deepseek-v4-pro-0813@deepseek-v4-pro-0813" || result.Usage.CostStatus != "UNKNOWN" || wires != 1 || p.releases.Load() != 1 || p.settles.Load() != 1 {
		t.Fatal("selected native text changed model binding, once-only dispatch, or unknown accounting")
	}
	if _, err := NewOfflineHarness(a); !errors.Is(err, ErrUnavailable) {
		t.Fatal("selected live route entered offline harness")
	}
	if result, err := NewGateway(&fakeGate{true}).Complete(context.Background(), r); !errors.Is(err, ErrUnavailable) || result.Mode != Disabled {
		t.Fatal("default Gateway activated selected provider")
	}
}

func TestNativeLiveGatewayDeepSeekBodyReadRechecksRevocationAndSettlesUnknown(t *testing.T) {
	c := tencentTestConfig(t)
	c.model = TencentTokenHubDeepSeekModel
	p := &liveTestPort{}
	wires, bytesReleased := 0, 0
	a, err := NewTencentTokenHubAdapter(c, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		wires++
		p.denied.Store(true) // Revocation after handoff, before actual body write.
		raw, e := io.ReadAll(req.Body)
		bytesReleased += len(raw)
		if e == nil || len(raw) != 0 {
			t.Fatal("revoked selected-model body released bytes")
		}
		return nil, e
	}))
	if err != nil {
		t.Fatal("selected adapter rejected")
	}
	g, err := NewNativeLiveGateway(&fakeGate{true}, a, p)
	if err != nil {
		t.Fatal("selected private native route rejected")
	}
	result, err := g.Complete(context.Background(), liveTestRequest())
	assertNoLiveText(t, result, err)
	if wires != 1 || bytesReleased != 0 || p.releases.Load() != 1 || p.settles.Load() != 1 || result.Usage.CostStatus != "UNKNOWN" {
		t.Fatal("selected-model revocation retried, skipped UNKNOWN settlement, or released bytes")
	}
}

func TestNativeLiveGatewayUsesOriginalGatewayTypeWithTrustedPortOnly(t *testing.T) {
	p := &liveTestPort{}
	var expected PreparedTencentWire
	wires := 0
	g, a := liveTestGateway(t, p, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		wires++
		if p.releases.Load() != 1 || p.checks.Load() < 3 || p.settles.Load() != 0 {
			t.Fatal("wire preceded native release/current checks")
		}
		raw, _ := io.ReadAll(req.Body)
		if !bytes.Equal(raw, expected.ExactWire()) || tencentWireDigest(raw) != expected.WireDigest() {
			t.Fatal("unapproved bytes sent")
		}
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	r := liveTestRequest()
	expected, _ = a.Prepare(providerRequest(r))
	p.onCheck = func(_ context.Context, got Request, w PreparedTencentWire) error {
		if got.RunID != r.RunID || got.Agent != r.Agent || w.WireDigest() != expected.WireDigest() || w.InputTokenBound() != 196608 || w.InputBoundEvidence() != TencentLiveInputBoundEvidence {
			t.Fatal("native port lost selector/payload binding")
		}
		return nil
	}
	p.onSettle = func(ctx context.Context, _ Request, _ PreparedTencentWire) error {
		if ctx.Err() != nil || wires != 1 {
			t.Fatal("UNKNOWN settlement skipped or counted no-send as send")
		}
		return nil
	}
	result, err := g.Complete(context.Background(), r)
	if err != nil || result.Mode != Live || result.Status != Completed || result.Text != "当前地点需要核验来源。" || result.ProviderID != "tencent_tokenhub" || result.ProviderModelVersion != "hy3@hy3" || result.RunID != r.RunID || result.Agent != r.Agent || result.Usage.CostStatus != "UNKNOWN" || p.releases.Load() != 1 || p.settles.Load() != 1 || wires != 1 {
		t.Fatal("controlled test LIVE proposal failed")
	}
	if _, err := NewOfflineHarness(a); !errors.Is(err, ErrUnavailable) {
		t.Fatal("LIVE adapter entered offline harness")
	}
	if result, err := NewGateway(&fakeGate{true}).Complete(context.Background(), r); !errors.Is(err, ErrUnavailable) || result.Mode != Disabled {
		t.Fatal("default gateway became live")
	}
}

func TestNativeLiveGatewayConstructorReferencesAreNotAuthority(t *testing.T) {
	a := tencentAdapter(t, tencentRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("constructor dispatched"); return nil, nil }))
	var nilPort *liveTestPort
	for _, port := range []NativeLiveDispatchPort{nil, nilPort} {
		if _, err := NewNativeLiveGateway(&fakeGate{true}, a, port); !errors.Is(err, ErrUnavailable) {
			t.Fatal("missing native port accepted")
		}
	}
	for _, gate := range []LiveGate{nil, (*fakeGate)(nil)} {
		if _, err := NewNativeLiveGateway(gate, a, &liveTestPort{}); !errors.Is(err, ErrUnavailable) {
			t.Fatal("missing gate accepted")
		}
	}
	if _, err := NewNativeLiveGateway(&fakeGate{true}, nil, &liveTestPort{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil adapter accepted")
	}
	p := &liveTestPort{}
	p.denied.Store(true)
	g, _ := NewNativeLiveGateway(&fakeGate{true}, a, p)
	result, err := g.Complete(context.Background(), liveTestRequest())
	assertNoLiveText(t, result, err)
	if p.releases.Load() != 0 || p.settles.Load() != 0 {
		t.Fatal("constructed refs manufactured release")
	}
}

func TestNativeLiveGatewayRevocationCancellationAndLateResultsRemainHeld(t *testing.T) {
	for _, name := range []string{"before_release", "before_wire", "after_wire", "during_settlement", "late_wire", "cancelled_wire", "cancelled_before", "gate_revoked"} {
		t.Run(name, func(t *testing.T) {
			p := &liveTestPort{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wires := 0
			var g *Gateway
			var gate *fakeGate
			g, _ = liveTestGateway(t, p, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				wires++
				switch name {
				case "after_wire":
					p.denied.Store(true)
				case "late_wire":
					g.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
				case "cancelled_wire":
					cancel()
				case "gate_revoked":
					gate.enabled = false
				}
				return tencentResponse(tencentSuccess(), 200), nil
			}))
			gate = g.gate.(*fakeGate)
			if name == "before_release" {
				p.denied.Store(true)
			}
			if name == "cancelled_before" {
				cancel()
			}
			p.onRelease = func(c context.Context, _ Request, _ PreparedTencentWire, send func(context.Context) ([]byte, error)) ([]byte, error) {
				if name == "before_wire" {
					p.denied.Store(true)
				}
				return send(c)
			}
			p.onSettle = func(c context.Context, _ Request, _ PreparedTencentWire) error {
				if c.Err() != nil {
					t.Fatal("cancelled caller skipped bounded UNKNOWN cleanup")
				}
				if name == "during_settlement" {
					p.denied.Store(true)
				}
				return nil
			}
			result, err := g.Complete(ctx, liveTestRequest())
			assertNoLiveText(t, result, err)
			if name == "before_release" || name == "cancelled_before" {
				if wires != 0 || p.releases.Load() != 0 || p.settles.Load() != 0 {
					t.Fatal("pre-cancel or denied authority dispatched")
				}
			} else if p.releases.Load() != 1 || p.settles.Load() != 1 || (name == "before_wire" && wires != 0) || (name != "before_wire" && wires != 1) {
				t.Fatal("released attempt was replayed/refunded or lost UNKNOWN cleanup")
			}
		})
	}
}

func TestNativeLiveGatewayNativeUnknownNoCallbackReplayOrTamperedReturn(t *testing.T) {
	for _, name := range []string{"unknown_commit", "no_callback", "duplicate_callback", "tampered_return", "settle_unknown", "late_callback", "nil_callback_context"} {
		t.Run(name, func(t *testing.T) {
			p := &liveTestPort{}
			wires := 0
			var late func(context.Context) ([]byte, error)
			g, _ := liveTestGateway(t, p, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				wires++
				return tencentResponse(tencentSuccess(), 200), nil
			}))
			p.onRelease = func(c context.Context, _ Request, _ PreparedTencentWire, send func(context.Context) ([]byte, error)) ([]byte, error) {
				switch name {
				case "unknown_commit":
					return nil, errors.New("unknown native commit sentinel")
				case "no_callback":
					return nil, nil
				case "late_callback":
					late = send
					return nil, nil
				case "nil_callback_context":
					return send(nil)
				}
				raw, err := send(c)
				if name == "duplicate_callback" {
					_, _ = send(c)
				}
				if name == "tampered_return" {
					raw = []byte(`{"private":"sentinel"}`)
				}
				return raw, err
			}
			p.onSettle = func(context.Context, Request, PreparedTencentWire) error {
				if name == "settle_unknown" {
					return errors.New("unknown settlement sentinel")
				}
				return nil
			}
			result, err := g.Complete(context.Background(), liveTestRequest())
			assertNoLiveText(t, result, err)
			if late != nil {
				if raw, err := late(context.Background()); err == nil || raw != nil {
					t.Fatal("port retained usable callback after release returned")
				}
			}
			wantWire := 0
			if name == "duplicate_callback" || name == "tampered_return" || name == "settle_unknown" {
				wantWire = 1
			}
			if wires != wantWire || p.releases.Load() != 1 || p.settles.Load() != 1 {
				t.Fatal("UNKNOWN/native failure caused retry/refund or wrong wire count")
			}
		})
	}
}

func TestNativeLiveGatewayProviderErrorsNoRetryAndUsageOverBoundDiscarded(t *testing.T) {
	for _, name := range []string{"rate_limit", "temporary", "lost_response", "over_input", "malformed", "refused", "truncated", "unknown_usage"} {
		t.Run(name, func(t *testing.T) {
			p := &liveTestPort{}
			wires := 0
			g, _ := liveTestGateway(t, p, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
				wires++
				raw := tencentSuccess()
				status := 200
				switch name {
				case "rate_limit":
					status = 429
				case "temporary":
					status = 503
				case "lost_response":
					return nil, errors.New("private transport sentinel")
				case "over_input":
					raw = strings.ReplaceAll(strings.ReplaceAll(raw, `"prompt_tokens":31`, `"prompt_tokens":196609`), `"total_tokens":43`, `"total_tokens":196621`)
				case "malformed":
					raw = `{"broken":true}`
				case "refused":
					raw = `{"id":"local-refused","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":null,"refusal":"private sentinel"},"finish_reason":"stop"}]}`
				case "truncated":
					raw = `{"id":"local-cut","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"partial sentinel"},"finish_reason":"length"}]}`
				case "unknown_usage":
					raw = `{"id":"local-no-usage","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"可核验文本"},"finish_reason":"stop"}]}`
				}
				return tencentResponse(raw, status), nil
			}))
			result, err := g.Complete(context.Background(), liveTestRequest())
			switch name {
			case "refused", "truncated":
				if err != nil || result.Text != "" || result.Mode != Live || (name == "refused" && result.Status != Refused) || (name == "truncated" && result.Status != Truncated) {
					t.Fatal("refusal/truncation released partial LIVE output")
				}
			case "unknown_usage":
				if err != nil || result.Mode != Live || result.Usage.Status != "UNKNOWN" || result.Usage.CostStatus != "UNKNOWN" {
					t.Fatal("missing usage fabricated tokens/cash")
				}
			default:
				assertNoLiveText(t, result, err)
			}
			if wires != 1 || p.releases.Load() != 1 || p.settles.Load() != 1 {
				t.Fatal("provider error retried or failed to retain UNKNOWN hold")
			}
		})
	}
}

func TestNativeLiveGatewayClosedTextNormalizerAndRequestCopy(t *testing.T) {
	a := tencentAdapter(t, nil)
	r := liveTestRequest()
	p, _ := a.Prepare(providerRequest(r))
	for _, raw := range []string{
		`{"status":"COMPLETED","request_id":"local1","finish_reason":"stop","text":"ok","text":"other"}`,
		`{"status":"COMPLETED","request_id":"local1","finish_reason":"stop","text":"ok","structured":{}}`,
		`{"status":"COMPLETED","request_id":"local1","finish_reason":"stop","text":"ok","tool_proposals":[]}`,
		`{"status":"TRUNCATED","request_id":"local1","finish_reason":"length","text":"partial"}`,
		`{"status":"REFUSED","request_id":"local1","finish_reason":"refusal","text":"private"}`,
	} {
		if result, err := normalizeLiveTencentText(r, []byte(raw), p); err == nil || result.Text != "" || result.Mode != Live {
			t.Fatal("LIVE normalizer admitted offline/partial/ambiguous output")
		}
	}
	port := &liveTestPort{}
	port.onCheck = func(_ context.Context, copyRequest Request, _ PreparedTencentWire) error {
		copyRequest.Messages[0].Content = "native port mutation"
		return nil
	}
	g, _ := liveTestGateway(t, port, tencentRoundTripper(func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		if strings.Contains(string(raw), "native port mutation") {
			t.Fatal("native getter mutated prepared wire")
		}
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	if result, err := g.Complete(context.Background(), r); err != nil || result.Mode != Live || r.Messages[0].Content == "native port mutation" {
		t.Fatal("request slice copy lost")
	}
}

func TestNativeLiveGatewayActualHTTP1PipeHasOneCommittedWireAndUnknownHold(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		drop   bool
	}{{"success", 200, false}, {"rate_limit", 429, false}, {"temporary", 503, false}, {"redirect", 307, false}, {"lost_response", 0, true}} {
		t.Run(tc.name, func(t *testing.T) {
			port := &liveTestPort{}
			adapter := tencentAdapter(t, nil)
			transport := adapter.client.Transport.(*http.Transport)
			defer transport.CloseIdleConnections()
			var committed atomic.Bool
			var dials, wires atomic.Int32
			observed := make(chan error, 1)
			request := liveTestRequest()
			request.DeadlineAt = time.Now().Add(2 * time.Second)
			prepared, err := adapter.Prepare(providerRequest(request))
			if err != nil {
				t.Fatal("local exact wire unavailable")
			}
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
					if err != nil || !committed.Load() || req.Proto != "HTTP/1.1" || !req.Close || req.Host != "tokenhub.tencentmaas.com" || req.Method != http.MethodPost || req.URL.Path != "/v1/chat/completions" || !bytes.Equal(body, prepared.ExactWire()) || tencentWireDigest(body) != prepared.WireDigest() {
						observed <- errors.New("wire did not match committed approved payload")
						return
					}
					if tc.drop {
						observed <- nil
						return
					}
					bodyText := tencentSuccess()
					header := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nLocation: https://example.org/replay\r\n\r\n", tc.status, http.StatusText(tc.status), len(bodyText))
					_, err = io.WriteString(server, header+bodyText)
					observed <- err
				}()
				return client, nil
			}
			port.onRelease = func(ctx context.Context, _ Request, p PreparedTencentWire, send func(context.Context) ([]byte, error)) ([]byte, error) {
				if p.WireDigest() != prepared.WireDigest() || p.InputTokenBound() != 196608 {
					t.Fatal("native release received another payload")
				}
				committed.Store(true) // Synthetic native commit; no database proof.
				return send(ctx)
			}
			port.onSettle = func(ctx context.Context, _ Request, p PreparedTencentWire) error {
				if ctx.Err() != nil || wires.Load() != 1 || !committed.Load() || p.WireDigest() != prepared.WireDigest() {
					t.Fatal("wire lost its UNKNOWN hold cleanup")
				}
				return nil
			}
			gateway, err := NewNativeLiveGateway(&fakeGate{true}, adapter, port)
			if err != nil {
				t.Fatal("local trusted boundary rejected")
			}
			result, err := gateway.Complete(context.Background(), request)
			if tc.status == 200 {
				if err != nil || result.Mode != Live || result.Text == "" || result.Usage.Status != "KNOWN" || result.Usage.CostStatus != "UNKNOWN" {
					t.Fatal("pipe LIVE text/usage proposal unavailable")
				}
			} else {
				assertNoLiveText(t, result, err)
			}
			if dials.Load() != 1 || wires.Load() != 1 || port.releases.Load() != 1 || port.settles.Load() != 1 {
				t.Fatal("committed attempt caused hidden retries or lost cleanup")
			}
			select {
			case err := <-observed:
				if err != nil && !errors.Is(err, io.ErrClosedPipe) {
					t.Fatal("local HTTP/1 pipe fixture failed")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("local HTTP/1 pipe fixture did not finish")
			}
		})
	}
}

func TestNativeLiveGatewayAsyncCallbackAfterReleaseClosedCannotDispatch(t *testing.T) {
	port := &liveTestPort{}
	var wires atomic.Int32
	gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		wires.Add(1)
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	proceed := make(chan struct{})
	done := make(chan error, 1)
	port.onRelease = func(ctx context.Context, _ Request, _ PreparedTencentWire, send func(context.Context) ([]byte, error)) ([]byte, error) {
		go func() {
			<-proceed
			raw, err := send(ctx)
			if raw != nil || err == nil {
				done <- errors.New("closed release dispatched asynchronously")
				return
			}
			done <- nil
		}()
		return nil, nil
	}
	result, err := gateway.Complete(context.Background(), liveTestRequest())
	assertNoLiveText(t, result, err)
	close(proceed)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("late local callback did not finish")
	}
	if wires.Load() != 0 || port.releases.Load() != 1 || port.settles.Load() != 1 {
		t.Fatal("closed native release replayed or skipped UNKNOWN cleanup")
	}
}

func TestNativeLiveGatewayDeniesRevocationAtActualWireCheck(t *testing.T) {
	port := &liveTestPort{}
	wires := 0
	gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		wires++
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	port.onCheck = func(context.Context, Request, PreparedTencentWire) error {
		// 1: pre-release; 2: inside native callback; 3: exact-wire guard.
		if port.checks.Load() == 3 {
			port.denied.Store(true)
			return errors.New("revoked source sentinel")
		}
		return nil
	}
	result, err := gateway.Complete(context.Background(), liveTestRequest())
	assertNoLiveText(t, result, err)
	if wires != 0 || port.releases.Load() != 1 || port.settles.Load() != 1 {
		t.Fatal("last native check failed to fence the provider wire")
	}
}

func TestNativeLiveGatewayConcurrentDuplicateCallbackCannotReleaseTextOrSecondWire(t *testing.T) {
	port := &liveTestPort{}
	var wires atomic.Int32
	started := make(chan struct{})
	allowResponse := make(chan struct{}, 1)
	defer func() { allowResponse <- struct{}{} }()
	gateway, _ := liveTestGateway(t, port, tencentRoundTripper(func(*http.Request) (*http.Response, error) {
		wires.Add(1)
		close(started)
		<-allowResponse
		return tencentResponse(tencentSuccess(), 200), nil
	}))
	port.onRelease = func(ctx context.Context, _ Request, _ PreparedTencentWire, send func(context.Context) ([]byte, error)) ([]byte, error) {
		type outcome struct {
			raw []byte
			err error
		}
		first := make(chan outcome, 1)
		go func() { raw, err := send(ctx); first <- outcome{raw, err} }()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("first local callback did not reach wire")
		}
		if raw, err := send(ctx); raw != nil || err == nil {
			t.Fatal("concurrent callback reused native release")
		}
		allowResponse <- struct{}{}
		select {
		case result := <-first:
			return result.raw, result.err
		case <-time.After(time.Second):
			t.Fatal("first local callback did not finish")
		}
		return nil, ErrUnavailable
	}
	result, err := gateway.Complete(context.Background(), liveTestRequest())
	assertNoLiveText(t, result, err)
	if wires.Load() != 1 || port.releases.Load() != 1 || port.settles.Load() != 1 {
		t.Fatal("concurrent callback caused replay or lost UNKNOWN hold cleanup")
	}
}
