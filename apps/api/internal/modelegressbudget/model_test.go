package modelegressbudget

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"math"
	"testing"
	"time"
)

func testPrice() Price {
	return Price{Version: "local.v1", Destination: modelcapability.Key{Provider: "fake", Model: "test", Version: "v1", WireContract: "offline.v1"}, Region: modelcapability.UK, Retention: Retention, Currency: "GBP", InputMicrosPerToken: 2, OutputMicrosPerToken: 3, InputTokenCeiling: 100, OutputTokenCeiling: 128, Evidence: LocalPrice, ExpiresAt: time.Now().Add(time.Hour)}
}
func TestModelEgressPriceStrictBoundaries(t *testing.T) {
	now := time.Now()
	p := testPrice()
	if ValidatePrice(p, now) != nil {
		t.Fatal("valid local synthetic price")
	}
	changes := []struct {
		name   string
		change func(*Price)
	}{
		{"unknown_price", func(v *Price) { v.Evidence = "LIVE" }}, {"free_missing_input", func(v *Price) { v.InputMicrosPerToken = 0 }}, {"free_missing_output", func(v *Price) { v.OutputMicrosPerToken = 0 }},
		{"negative_rate", func(v *Price) { v.InputMicrosPerToken = -1 }}, {"overflow_rate", func(v *Price) { v.OutputMicrosPerToken = math.MaxInt64 }},
		{"missing_input_ceiling", func(v *Price) { v.InputTokenCeiling = 0 }}, {"oversize_input", func(v *Price) { v.InputTokenCeiling = 1_000_001 }}, {"output_ceiling", func(v *Price) { v.OutputTokenCeiling = 4097 }},
		{"unknown_region", func(v *Price) { v.Region = "UNKNOWN" }}, {"retain_source", func(v *Price) { v.Retention = "STORE" }}, {"currency", func(v *Price) { v.Currency = "gbp" }},
		{"expired", func(v *Price) { v.ExpiresAt = now }}, {"unbounded_price", func(v *Price) { v.ExpiresAt = now.Add(31 * 24 * time.Hour) }}, {"latest", func(v *Price) { v.Destination.Version = "latest" }},
		{"missing_provider", func(v *Price) { v.Destination.Provider = "" }}, {"agent_as_model", func(v *Price) { v.Destination.Model = "78000000-0000-4000-8000-000000000001" }},
	}
	for _, c := range changes {
		t.Run(c.name, func(t *testing.T) {
			v := p
			c.change(&v)
			if !errors.Is(ValidatePrice(v, now), ErrInvalid) {
				t.Fatal("invalid price accepted")
			}
		})
	}
}
func TestModelEgressIntegerBoundsAndLimitOverflow(t *testing.T) {
	p := testPrice()
	a, e := Bound(p, 128)
	if e != nil || a != (Amount{100, 128, 584}) {
		t.Fatal(a, e)
	}
	for _, output := range []int{0, -1, 129, math.MaxInt} {
		t.Run("output_"+time.Duration(output).String(), func(t *testing.T) {
			if _, e := Bound(p, output); e == nil {
				t.Fatal("bad output bound accepted")
			}
		})
	}
	p.InputMicrosPerToken = 1_000_000
	p.InputTokenCeiling = 1_000_000
	p.OutputMicrosPerToken = 1_000_000
	p.OutputTokenCeiling = 4096
	if _, e = Bound(p, 4096); e == nil {
		t.Fatal("sum beyond hard integer cost cap accepted")
	}
	l := Limits{2, 200, 256, 1168}
	if !Fits(l, Limits{}, a) || !Fits(l, Limits{1, 100, 128, 584}, a) || Fits(l, Limits{2, 100, 128, 584}, a) {
		t.Fatal("request counter boundary")
	}
	for _, used := range []Limits{{1, 101, 128, 584}, {1, 100, 129, 584}, {1, 100, 128, 585}, {-1, 0, 0, 0}, {1, math.MaxInt64, 0, 0}} {
		if Fits(l, used, a) {
			t.Fatal("over limit accepted", used)
		}
	}
	if Fits(l, Limits{}, Amount{math.MaxInt64, 1, 1}) || Fits(l, Limits{}, Amount{1, 1, -1}) {
		t.Fatal("overflow or negative amount accepted")
	}
}
func TestModelEgressLocalUsageUnknownNotPaid(t *testing.T) {
	in, out := int64(12), int64(7)
	valid := []modelgateway.Usage{{Status: "UNKNOWN", CostStatus: "UNKNOWN"}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &in, OutputTokens: &out}}
	for _, v := range valid {
		u, e := NewLocalUsage(v)
		if e != nil {
			t.Fatal(e)
		}
		known, i, o := u.Values()
		if known != (v.Status == "KNOWN") || (known && (i != in || o != out)) {
			t.Fatal("usage changed")
		}
	}
	invalid := []modelgateway.Usage{{Status: "KNOWN", CostStatus: "PAID", InputTokens: &in, OutputTokens: &out}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &in}, {Status: "UNKNOWN", CostStatus: "UNKNOWN", InputTokens: &in}, {Status: "OTHER", CostStatus: "UNKNOWN"}}
	for i, v := range invalid {
		t.Run(time.Duration(i).String(), func(t *testing.T) {
			if _, e := NewLocalUsage(v); e == nil {
				t.Fatal("unverified usage accepted")
			}
		})
	}
	negative, huge := int64(-1), int64(math.MaxInt64)
	for _, n := range []*int64{&negative, &huge} {
		if _, e := NewLocalUsage(modelgateway.Usage{Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: n, OutputTokens: &out}); e == nil {
			t.Fatal("usage range accepted")
		}
	}
}
func TestModelEgressServerAuthorityNotJSONTransferable(t *testing.T) {
	for _, v := range []any{Price{}, LocalUsage{}} {
		if _, e := json.Marshal(v); !errors.Is(e, ErrServerOnly) {
			t.Fatal("authority marshaled", e)
		}
	}
	for _, v := range []any{&Price{}, &LocalUsage{}, &Preview{}} {
		if e := json.Unmarshal([]byte(`{"status":"APPROVED"}`), v); !errors.Is(e, ErrServerOnly) {
			t.Fatal("client reconstructed authority", e)
		}
	}
}
func TestModelEgressDigestBindsExactDataDestinationAndBounds(t *testing.T) {
	r := modelgateway.Request{Messages: []modelgateway.Message{{Role: "user", Content: "合成个人任务"}}, DeadlineAt: time.Now().Add(time.Minute)}
	p := testPrice()
	base := Digest(r, p)
	changes := []func(*Price){func(v *Price) { v.Version = "local.v2" }, func(v *Price) { v.Destination.Provider = "other" }, func(v *Price) { v.Region = modelcapability.EU }, func(v *Price) { v.Retention = "OTHER" }, func(v *Price) { v.Currency = "USD" }, func(v *Price) { v.InputMicrosPerToken++ }, func(v *Price) { v.OutputMicrosPerToken++ }, func(v *Price) { v.InputTokenCeiling++ }}
	for i, c := range changes {
		t.Run(time.Duration(i).String(), func(t *testing.T) {
			v := p
			c(&v)
			if Digest(r, v) == base {
				t.Fatal("changed price/destination retained approval")
			}
		})
	}
	r.Messages[0].Content = "新任务"
	if Digest(r, p) == base {
		t.Fatal("changed source retained approval")
	}
}

type openLocalGate struct{}

func (openLocalGate) InferenceEnabled(context.Context) bool { return true }
func TestModelEgressDefaultAndOnGateRemainUnavailable(t *testing.T) {
	for _, gate := range []modelgateway.LiveGate{nil, openLocalGate{}} {
		s := NewService(gate)
		v, e := s.Complete(context.Background(), modelgateway.Request{})
		if e == nil || v.ProviderID != "" || v.Status == modelgateway.Completed {
			t.Fatal("local ledger activated real provider", v, e)
		}
	}
}

func TestModelEgressHumanPreviewIsExactAndReadOnly(t *testing.T) {
	p := testPrice()
	r := modelgateway.Request{Messages: []modelgateway.Message{{Role: "user", Content: "本人合成task"}}, Budget: modelgateway.Budget{MaxOutputTokens: 64}}
	v := Preview{ID: "local-preview", Price: p, Request: r, ExpiresAt: time.Now().Add(time.Minute), RequestDigest: Digest(r, p)}
	display, e := v.Display()
	if e != nil || display.Scope != Scope || display.Purpose != Purpose || display.Evidence != LocalPrice || display.Upper.CostMicros != 392 || display.InputMicrosPerToken != 2 {
		t.Fatal("incorrect exact human preview", e)
	}
	if _, e = json.Marshal(display); e != nil {
		t.Fatal("human view unavailable", e)
	}
	display.Request.Messages[0].Content = "篡改只读view不能改approved server body"
	if v.Request.Messages[0].Content != "本人合成task" {
		t.Fatal("shared backing slice")
	}
	if e = json.Unmarshal([]byte(`{"Status":"APPROVED"}`), &v); !errors.Is(e, ErrServerOnly) {
		t.Fatal("view became authority", e)
	}
}
