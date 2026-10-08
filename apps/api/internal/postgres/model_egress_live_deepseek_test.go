package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func liveDeepSeekPrice(now time.Time) modelegressbudget.LivePrice {
	p := liveLedgerPrice(now, modelegressbudget.LiveToken)
	p.Base.Version = "unit.deepseek.0813.peak"
	p.Base.Destination = modelegressbudget.LiveDeepSeek0813Destination()
	p.Base.InputMicrosPerToken, p.Base.OutputMicrosPerToken = 9, 27
	p.Base.InputTokenCeiling = modelgateway.TencentDeepSeekMaxInputTokens
	return p
}

func liveDeepSeekAdapter(t *testing.T) *modelgateway.TencentTokenHubAdapter {
	t.Helper()
	c, err := modelgateway.ParseTencentTokenHubConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"deepseek-v4-pro-0813","maxOutputTokens":768,"apiKey":"unit-test-placeholder"}`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := modelgateway.NewTencentTokenHubAdapter(c, liveLedgerNoNetwork{t})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestLiveDeepSeekNativeWireBindsExactPriceAndTwoRoleInput(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := "Aberdeen public places 中文 😀"
	r, p, adapter := liveLedgerRequest(now, query), liveDeepSeekPrice(now), liveDeepSeekAdapter(t)
	w, q, digest, upper, err := prepareLiveWire(query, r, p, adapter, now)
	if err != nil || q == "" || digest != w.WireDigest() || upper.Amount != (modelegressbudget.Amount{InputTokens: 16384, OutputTokens: 768, CostMicros: 168192}) || !liveTokenWireMatches(p, liveProviderRequest(r), w, now) {
		t.Fatal("bounded exact DS preparation rejected", err)
	}
	if w.Descriptor().ModelID != modelgateway.TencentTokenHubDeepSeekModel || w.InputBoundEvidence() != modelgateway.TencentDeepSeekInputBoundEvidence {
		t.Fatal("prepared input proof describes another model")
	}
	for name, edit := range map[string]func(*modelegressbudget.LivePrice){
		"HY3_price":   func(p *modelegressbudget.LivePrice) { *p = liveLedgerPrice(now, modelegressbudget.LiveToken) },
		"version":     func(p *modelegressbudget.LivePrice) { p.Base.Destination.Version = "hy3" },
		"wire":        func(p *modelegressbudget.LivePrice) { p.Base.Destination.WireContract = "other.v1" },
		"lower_bound": func(p *modelegressbudget.LivePrice) { p.Base.InputTokenCeiling-- },
		"cache_price": func(p *modelegressbudget.LivePrice) { p.Base.InputMicrosPerToken = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			copy := p
			edit(&copy)
			if liveTokenWireMatches(copy, liveProviderRequest(r), w, now) {
				t.Fatal("private wire borrowed different price")
			}
		})
	}
	if _, _, _, _, err := prepareLiveWire(query, r, p, liveLedgerAdapter(t), now); !errors.Is(err, modelegressbudget.ErrDenied) {
		t.Fatal("HY3 projector supplied DS proof", err)
	}
	r.Messages = append(r.Messages, modelgateway.Message{Role: "assistant", Content: "unapproved history"})
	if _, _, _, _, err := prepareLiveWire(query, r, p, adapter, now); !errors.Is(err, modelegressbudget.ErrDenied) {
		t.Fatal("history widened query-only scope", err)
	}
}

func TestLiveDeepSeekNativeSourceWireAndOriginalFourBudgetCaps(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := "Aberdeen places"
	sources := []modelegressbudget.LivePublicSource{{Title: "UNIT data only", URL: "https://example.org/public", Passage: "Not a real verified source."}}
	payload, err := modelegressbudget.LivePublicSearchPayload(query, sources)
	if err != nil {
		t.Fatal(err)
	}
	r, p := liveLedgerRequest(now, query), liveDeepSeekPrice(now)
	r.Messages[1].Content = payload
	w, _, _, upper, err := prepareLiveSourceWire(query, sources, r, p, liveDeepSeekAdapter(t), now)
	if err != nil || !liveTokenWireMatches(p, liveProviderRequest(r), w, now) {
		t.Fatal("typed source wire rejected", err)
	}
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON", "ROOT", "TASK"} {
		t.Run(scope, func(t *testing.T) {
			row := egressBudgetRow{scope: scope, currency: "CNY", limit: modelegressbudget.Limits{Requests: 10, InputTokens: 1000000, OutputTokens: 4096, CostMicros: 10000000}, used: modelegressbudget.Limits{Requests: 1, CostMicros: 80000}}
			if !fitsLivePriceBudget(row, p, upper) {
				t.Fatal("original combined budget rejected DS peak hold")
			}
			row.used.CostMicros = 10000000
			if fitsLivePriceBudget(row, p, upper) {
				t.Fatal("owner old UNKNOWN holds ignored")
			}
			if scope == "ROOT" || scope == "TASK" {
				row.used = modelegressbudget.Limits{Requests: 2}
				if fitsLivePriceBudget(row, p, upper) {
					t.Fatal("third request permitted")
				}
			}
		})
	}
	r.Messages[1].Content = strings.Repeat("x", 4097)
	if _, _, _, _, err := prepareLiveSourceWire(query, sources, r, p, liveDeepSeekAdapter(t), now); err == nil {
		t.Fatal("source wire differs from scoped envelope")
	}
	if liveRootCashLimit != 279680 || liveRequestCashLimit != 200000 || liveOwnerCashLimit != 10000000 {
		t.Fatal("approved monetary caps changed")
	}
}

func TestLiveDeepSeekNativeResolvedWireRetainsCurrentQueryAndSourceScope(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := "  更近一点  "
	c := resolvedNativeTestContext()
	compiled, err := modelegressbudget.CompileLiveResolvedPublicSearchQuery(query, c)
	if err != nil {
		t.Fatal(err)
	}
	proof := nativeResolvedPublicSearch{context: c, compiledQuery: compiled, evidence: strings.Repeat("d", 64), valid: true}
	sources := liveSourceTestSources()
	payload, err := modelegressbudget.LiveResolvedPublicSearchPayload(query, c, sources)
	if err != nil {
		t.Fatal(err)
	}
	r, p, adapter := liveLedgerRequest(now, query), liveDeepSeekPrice(now), liveDeepSeekAdapter(t)
	r.Messages[1].Content = payload
	w, q, digest, upper, err := prepareResolvedLiveSourceWire(query, proof, sources, r, p, adapter, now)
	if err != nil || q != liveDigestBytes("birdtie.live-current-query.v1", []byte(query)) || digest != w.WireDigest() || upper.Amount.CostMicros != 168192 || !liveTokenWireMatches(p, liveProviderRequest(r), w, now) {
		t.Fatal("resolved DS wire lost raw query or exact price", err)
	}
	if _, _, _, _, err := prepareLiveWire(query, r, p, adapter, now); err == nil {
		t.Fatal("resolved envelope entered literal grant")
	}
	if _, _, _, _, err := prepareLiveSourceWire(query, sources, r, p, adapter, now); err == nil {
		t.Fatal("resolved envelope entered v1 source grant")
	}
	if _, _, _, _, err := prepareResolvedLiveSourceWire(query, proof, sources, r, p, liveLedgerAdapter(t), now); err == nil {
		t.Fatal("HY3 proof borrowed DS tariff")
	}
	proof.context.ResolvedSlots.Category = "basketball"
	if _, _, _, _, err := prepareResolvedLiveSourceWire(query, proof, sources, r, p, adapter, now); err == nil {
		t.Fatal("changed context retained compiled source proof")
	}
}
