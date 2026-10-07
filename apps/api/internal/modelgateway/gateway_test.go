package modelgateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAdapter struct {
	mu         sync.Mutex
	descriptor ProviderDescriptor
	response   []byte
	err        error
	calls      int
	received   ProviderRequest
	hook       func(context.Context)
}

func (f *fakeAdapter) Descriptor() ProviderDescriptor {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.descriptor
}
func (f *fakeAdapter) Complete(ctx context.Context, r ProviderRequest) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	f.received = r
	raw := append([]byte{}, f.response...)
	err := f.err
	hook := f.hook
	f.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	return raw, err
}

type fakeGate struct{ enabled bool }

func (g *fakeGate) InferenceEnabled(context.Context) bool { return g.enabled }

func harness(t *testing.T, raw string) (*OfflineHarness, *fakeAdapter) {
	t.Helper()
	f := &fakeAdapter{descriptor: testDescriptor(), response: []byte(raw)}
	h, err := NewOfflineHarness(f)
	if err != nil {
		t.Fatal(err)
	}
	h.now = testNow
	return h, f
}

func TestUnifiedGatewayOfflineAdapterContractSuite(t *testing.T) {
	cases := []struct {
		name    string
		request Request
		raw     string
		status  Status
	}{
		{"text", testRequest(Text), `{"status":"COMPLETED","request_id":"fake-req1","finish_reason":"stop","text":"合成回答：先看看附近活动","usage":{"status":"KNOWN","input_tokens":20,"output_tokens":10}}`, Completed},
		{"structured", testRequest(Structured), `{"status":"COMPLETED","request_id":"fake-req2","finish_reason":"stop","structured":{"answer":"合成活动建议，实体仍须原域核验","entity_refs":[{"type":"ACTIVITY","id":"` + fixtureEntity + `"}]}}`, Completed},
		{"tool_proposals", testRequest(ToolProposals), `{"status":"COMPLETED","request_id":"fake-req3","finish_reason":"stop","tool_proposals":[{"tool":"activity.search","arguments":{"query":"羽毛球","city_id":"aberdeen-gb"},"reason_summary":"合成只读搜索提案"},{"tool":"activity.detail","arguments":{"activity_id":"` + fixtureEntity + `"},"reason_summary":"合成详情提案"}]}`, Completed},
		{"refusal", testRequest(Text), `{"status":"REFUSED","request_id":"fake-req4","finish_reason":"refusal"}`, Refused},
		{"truncated", testRequest(Text), `{"status":"TRUNCATED","request_id":"fake-req5","finish_reason":"length","text":"未完整的合成输出"}`, Truncated},
		{"unavailable", testRequest(Text), `{"status":"UNAVAILABLE","request_id":"fake-req6","finish_reason":"unavailable"}`, Unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, f := harness(t, tc.raw)
			result, err := h.Complete(context.Background(), tc.request)
			if err != nil || result.Status != tc.status || result.Mode != OfflineContract || result.Agent != tc.request.Agent || result.RunID != tc.request.RunID || f.calls != 1 {
				t.Fatal("normalized fake contract failed", err)
			}
			if tc.name == "text" {
				if result.Usage.Status != "KNOWN" || *result.Usage.OutputTokens != 10 || result.Text == "" {
					t.Fatal("known usage/text not normalized")
				}
			} else if result.Usage.Status != "UNKNOWN" || result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil {
				t.Fatal("missing usage was fabricated zero")
			}
			if result.Usage.CostStatus != "UNKNOWN" {
				t.Fatal("fixture usage became real pricing")
			}
			if tc.name == "truncated" && (result.Text != "" || result.Answer != nil || result.Candidate != nil || len(result.ToolProposals) > 0) {
				t.Fatal("partial truncation released content/action")
			}
		})
	}
	r := testRequest(Structured)
	r.TaskKind = MemoryCandidateExtraction
	r.OutputSchemaVersion = "air.candidate_proposal.v1"
	r.ToolAllowlist = []string{}
	h, _ := harness(t, `{"status":"COMPLETED","request_id":"fake-memory","finish_reason":"stop","structured":{"status":"CANDIDATE","predicate":"ACTIVITY_CATEGORY","value":"badminton","source_ref_ids":["`+fixtureContext+`"],"subject_attribution":"UNVERIFIED"}}`)
	result, err := h.Complete(context.Background(), r)
	if err != nil || result.Candidate == nil || result.Candidate.Status != "CANDIDATE" {
		t.Fatal("candidate proposal contract failed", err)
	}
}

func TestDefaultGatewayAlwaysUnavailableWithoutTrustedLiveBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		gate LiveGate
	}{
		{"nil", nil}, {"off", &fakeGate{false}}, {"fake_allow", &fakeGate{true}}, {"typed_nil", (*fakeGate)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGateway(tc.gate)
			g.now = testNow
			r := testRequest(Text)
			result, err := g.Complete(context.Background(), r)
			if !errors.Is(err, ErrUnavailable) || result.Status != Unavailable || result.Mode != Disabled || result.Text != "" || result.Answer != nil || result.Candidate != nil || len(result.ToolProposals) != 0 || result.ProviderID != "" {
				t.Fatal("flag/fixture opened live inference", err)
			}
		})
	}
	var g *Gateway
	if result, err := g.Complete(context.Background(), testRequest(Text)); !errors.Is(err, ErrUnavailable) || result.Mode != Disabled {
		t.Fatal("nil gateway unsafe")
	}
}

func TestProviderErrorsAreNormalizedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		code  string
		retry bool
	}{
		{"unknown_private", errors.New("secret-api-key private-context"), "UNKNOWN", false},
		{"rate_limit", ProviderError{"RATE_LIMIT", false}, "RATE_LIMIT", true},
		{"temporary", ProviderError{"TEMPORARY", false}, "TEMPORARY", true},
		{"auth", ProviderError{"AUTHENTICATION", true}, "AUTHENTICATION", false},
		{"malicious_code", ProviderError{"secret-api-key", true}, "UNKNOWN", false},
		{"deadline", context.DeadlineExceeded, "DEADLINE", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f := harness(t, "")
			f.err = tc.err
			result, err := h.Complete(context.Background(), testRequest(Text))
			var provider ProviderError
			if !errors.As(err, &provider) || provider.Code != tc.code || provider.Retryable != tc.retry || result.Status != Unavailable || result.Text != "" {
				t.Fatal("provider error normalized incorrectly", err)
			}
			data, _ := json.Marshal(result)
			if strings.Contains(string(data), "secret") || strings.Contains(err.Error(), "secret") {
				t.Fatal("provider detail leaked")
			}
		})
	}
}

func TestGatewayModelSwitchKeepsNativeAgentAndProposalOnly(t *testing.T) {
	r := testRequest(ToolProposals)
	raw := `{"status":"COMPLETED","request_id":"fake-tools","finish_reason":"stop","tool_proposals":[{"tool":"activity.detail","arguments":{"activity_id":"` + fixtureEntity + `"},"reason_summary":"仅提案"}]}`
	h1, _ := harness(t, raw)
	h2, f2 := harness(t, raw)
	f2.descriptor.ModelID = "fake-second"
	h2.descriptor = f2.descriptor
	a, e1 := h1.Complete(context.Background(), r)
	b, e2 := h2.Complete(context.Background(), r)
	if e1 != nil || e2 != nil || a.Agent != b.Agent || a.Agent != r.Agent || a.ProviderModelVersion == b.ProviderModelVersion || len(a.ToolProposals) != 1 {
		t.Fatal("model changed Agent identity")
	}
	// There is no executor interface/call in ModelGateway or ProviderAdapter.
	if a.ReasonCode != "synthetic_adapter_contract_only" || a.Mode != OfflineContract {
		t.Fatal("proposal became execution receipt")
	}
	for _, change := range []func(*ProviderDescriptor){
		func(d *ProviderDescriptor) { d.ModelID = fixtureAgent }, func(d *ProviderDescriptor) { d.Mode = "LIVE" },
		func(d *ProviderDescriptor) { d.ProviderID = "https://provider.invalid" }, func(d *ProviderDescriptor) { d.ModelVersion = "" },
	} {
		f := &fakeAdapter{descriptor: testDescriptor()}
		change(&f.descriptor)
		if _, err := NewOfflineHarness(f); !errors.Is(err, ErrUnavailable) || f.calls != 0 {
			t.Fatal("unapproved/live descriptor accepted")
		}
	}
}

func TestGatewayRejectsCancelledAndLateResponseWithoutPayload(t *testing.T) {
	t.Run("cancel_before", func(t *testing.T) {
		h, f := harness(t, `{}`)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := h.Complete(ctx, testRequest(Text))
		if !errors.Is(err, context.Canceled) || result.Text != "" || f.calls != 0 {
			t.Fatal("cancelled request dispatched")
		}
	})
	t.Run("invalid_before", func(t *testing.T) {
		h, f := harness(t, `{}`)
		r := testRequest(Text)
		r.RunID = "provider-name"
		result, err := h.Complete(context.Background(), r)
		if !errors.Is(err, ErrInvalid) || result.RunID != "" || f.calls != 0 {
			t.Fatal("invalid request dispatched")
		}
	})
	t.Run("late", func(t *testing.T) {
		h, f := harness(t, `{"status":"COMPLETED","request_id":"late","finish_reason":"stop","text":"late private fixture"}`)
		f.hook = func(context.Context) { h.now = func() time.Time { return testNow().Add(2 * time.Minute) } }
		result, err := h.Complete(context.Background(), testRequest(Text))
		if !errors.Is(err, ErrDeadline) || result.Text != "" {
			t.Fatal("late response leaked")
		}
	})
	t.Run("cancel_after", func(t *testing.T) {
		h, f := harness(t, `{"status":"COMPLETED","request_id":"late","finish_reason":"stop","text":"late private fixture"}`)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.hook = func(context.Context) { cancel() }
		result, err := h.Complete(ctx, testRequest(Text))
		if !errors.Is(err, context.Canceled) || result.Text != "" {
			t.Fatal("cancelled late response leaked")
		}
	})
	t.Run("descriptor_changed", func(t *testing.T) {
		h, f := harness(t, `{}`)
		f.hook = func(context.Context) { f.descriptor.ModelID = "changed" }
		result, err := h.Complete(context.Background(), testRequest(Text))
		if !errors.Is(err, ErrUnavailable) || result.Text != "" {
			t.Fatal("changed adapter scope accepted")
		}
	})
}
