package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

type liveLedgerNoNetwork struct{ t *testing.T }

func (p liveLedgerNoNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	p.t.Fatal("ledger preparation contacted a provider")
	return nil, errors.New("unexpected dispatch")
}

type liveLedgerProjector func(modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error)

func (p liveLedgerProjector) Prepare(r modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
	return p(r)
}
func liveLedgerAdapter(t *testing.T) *modelgateway.TencentTokenHubAdapter {
	t.Helper()
	c, e := modelgateway.ParseTencentTokenHubConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy3","maxOutputTokens":768,"apiKey":"unit-test-placeholder"}`))
	if e != nil {
		t.Fatal(e)
	}
	a, e := modelgateway.NewTencentTokenHubAdapter(c, liveLedgerNoNetwork{t})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func liveLedgerPrice(now time.Time, kind modelegressbudget.LiveChargeKind) modelegressbudget.LivePrice {
	source := modelegressbudget.LiveModelPriceURL
	p := modelegressbudget.LivePrice{Kind: kind, RequestCeiling: 1, Base: modelegressbudget.Price{Version: "test.live.v1", Destination: modelegressbudget.LiveHY3Destination(), Region: modelcapability.APAC, Retention: modelegressbudget.LiveRetentionUnknown, Currency: "CNY", InputMicrosPerToken: 1, OutputMicrosPerToken: 4, InputTokenCeiling: modelgateway.TencentLiveMaxInputTokens, OutputTokenCeiling: 768, Evidence: modelegressbudget.LiveTariffEvidence, ExpiresAt: now.Add(time.Hour)}}
	if kind == modelegressbudget.LiveCall {
		source = modelegressbudget.LiveSearchPriceURL
		p.Base.Destination = modelegressbudget.LiveWSADestination()
		p.Base.InputMicrosPerToken = 0
		p.Base.OutputMicrosPerToken = 0
		p.Base.InputTokenCeiling = 0
		p.Base.OutputTokenCeiling = 0
		p.CallMicros = modelegressbudget.LiveSearchCallMicros
	}
	p.Snapshot = modelegressbudget.LiveSnapshot{SourceURL: source, ArtifactSHA256: strings.Repeat("a", 64), DocumentUpdatedAt: now.Add(-time.Hour), ObservedAt: now.Add(-time.Minute), ExpiresAt: p.Base.ExpiresAt}
	return p
}
func liveLedgerRequest(now time.Time, query string) modelgateway.Request {
	return modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: "11111111-1111-4111-8111-111111111111", Agent: agentcognitive.AgentReference{AgentID: "22222222-2222-4222-8222-222222222222", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: "33333333-3333-4333-8333-333333333333"}, Role: agentruntime.ForType(actorref.Person).Role}, TaskKind: modelgateway.ActivityQuery, PromptVersion: "test.prompt.v1", InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1", ContextSnapshotRef: "44444444-4444-4444-8444-444444444444", DataPolicyRef: "55555555-5555-4555-8555-555555555555", BudgetRef: "55555555-5555-4555-8555-555555555555", Budget: modelgateway.Budget{MaxOutputTokens: 768}, Messages: []modelgateway.Message{{Role: "system", Content: "registered prompt"}, {Role: "user", Content: query}}, OutputMode: modelgateway.Text, CapabilitiesRequired: []string{"text"}, DeadlineAt: now.Add(time.Minute)}
}
func TestLiveLedgerTokenBoundUsesExactImmutableProviderWire(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	query := "Find Aberdeen places 😀 <>&"
	r := liveLedgerRequest(now, query)
	p := liveLedgerPrice(now, modelegressbudget.LiveToken)
	a := liveLedgerAdapter(t)
	w, q, h, upper, e := prepareLiveWire(query, r, p, a, now)
	if e != nil || upper.Amount.InputTokens != 196608 || upper.Amount.OutputTokens != 768 || upper.Amount.CostMicros != 199680 || upper.Requests != 1 || q == "" || h != w.WireDigest() || !w.Matches(liveProviderRequest(r), now) {
		t.Fatalf("wire preparation failed: %v", e)
	}
	if liveRootCashLimit != 279680 || liveRequestCashLimit != 200000 || liveOwnerCashLimit != 10000000 {
		t.Fatal("human approved cost limits changed")
	}
	copyBytes := w.ExactWire()
	copyBytes[0] = '['
	if !w.Matches(liveProviderRequest(r), now) {
		t.Fatal("exported bytes changed immutable wire")
	}
}
func TestLiveLedgerTokenPreparationRejectsLowerOrChangedProof(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	a := liveLedgerAdapter(t)
	for name, mutate := range map[string]func(*modelgateway.Request, *modelegressbudget.LivePrice, *LiveWireProjector){
		"lower_ceiling": func(_ *modelgateway.Request, p *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			p.Base.InputTokenCeiling = 100
		},
		"higher_ceiling": func(_ *modelgateway.Request, p *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			p.Base.InputTokenCeiling = 196609
		},
		"nil_projector": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) { *p = nil },
		"typed_nil": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) {
			var adapter *modelgateway.TencentTokenHubAdapter
			*p = adapter
		},
		"zero_wire": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) {
			*p = liveLedgerProjector(func(modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
				return modelgateway.PreparedTencentWire{}, nil
			})
		},
		"different_query_proof": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) {
			*p = liveLedgerProjector(func(r modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
				r.Messages[1].Content = "other private query"
				return a.Prepare(r)
			})
		},
		"different_prompt_proof": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) {
			*p = liveLedgerProjector(func(r modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
				r.Messages[0].Content = "injected system prompt"
				return a.Prepare(r)
			})
		},
		"different_deadline_proof": func(_ *modelgateway.Request, _ *modelegressbudget.LivePrice, p *LiveWireProjector) {
			*p = liveLedgerProjector(func(r modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error) {
				r.DeadlineAt = r.DeadlineAt.Add(-time.Second)
				return a.Prepare(r)
			})
		},
		"different_user": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.Messages[1].Content = "other"
		},
		"unapproved_context": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.Messages = append(r.Messages, modelgateway.Message{Role: "context", Content: "web source"})
		},
		"tool_mode": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.OutputMode = modelgateway.ToolProposals
		},
		"too_much_output": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.Budget.MaxOutputTokens = 769
		},
		"expired": func(r *modelgateway.Request, _ *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			r.DeadlineAt = now.Add(-time.Second)
		},
		"unknown_tariff": func(_ *modelgateway.Request, p *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			p.Base.Evidence = "UNKNOWN"
		},
		"claimed_retention": func(_ *modelgateway.Request, p *modelegressbudget.LivePrice, _ *LiveWireProjector) {
			p.Base.Retention = modelegressbudget.Retention
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := liveLedgerRequest(now, "current query")
			p := liveLedgerPrice(now, modelegressbudget.LiveToken)
			var projector LiveWireProjector = a
			mutate(&r, &p, &projector)
			w, _, _, upper, e := prepareLiveWire("current query", r, p, projector, now)
			if e == nil || len(w.ExactWire()) != 0 || upper.Amount.CostMicros != 0 {
				t.Fatal("invalid proof retained a usable preparation")
			}
		})
	}
}
func TestLiveLedgerCallCanonicalQueryOnlyNoTokenizer(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	price := liveLedgerPrice(now, modelegressbudget.LiveCall)
	for _, query := range []string{"Aberdeen distilleries", "  Chinese 地点 😀 <>&  ", strings.Repeat("x", 240)} {
		w, q, h, upper, e := prepareLiveWire(query, modelgateway.Request{}, price, nil, now)
		if e != nil || len(w.ExactWire()) != 0 || upper.Amount != (modelegressbudget.Amount{CostMicros: 80000}) || upper.Kind != modelegressbudget.LiveCall || upper.Requests != 1 {
			t.Fatalf("valid query denied: %v", e)
		}
		p := PreparedLiveEgress{price: price, query: strings.TrimSpace(query), queryDigest: q, payloadDigest: h}
		var obj map[string]any
		if json.Unmarshal(p.SearchWire(), &obj) != nil || len(obj) != 1 || obj["Query"] != strings.TrimSpace(query) || liveWireHash(p.SearchWire()) != h || q != liveDigestBytes("birdtie.live-current-query.v1", []byte(query)) {
			t.Fatal("Query-only body lost current scalar provenance")
		}
		if p.Request().SchemaVersion != "" || len(p.ModelWire().ExactWire()) != 0 {
			t.Fatal("search produced a model request")
		}
	}
	for _, query := range []string{"", " ", strings.Repeat("x", 241), strings.Repeat("地", 81), "find\x00place", string([]byte{0xff})} {
		_, _, _, upper, e := prepareLiveWire(query, modelgateway.Request{}, price, nil, now)
		if e == nil || upper.Amount.CostMicros != 0 {
			t.Fatal("invalid Query received a call bound")
		}
	}
}
func TestLiveLedgerFourScopeCapsPreserveOriginalCounters(t *testing.T) {
	token := modelegressbudget.LiveAmount{Kind: modelegressbudget.LiveToken, Requests: 1, Amount: modelegressbudget.Amount{InputTokens: 196608, OutputTokens: 768, CostMicros: 199680}}
	call := modelegressbudget.LiveAmount{Kind: modelegressbudget.LiveCall, Requests: 1, Amount: modelegressbudget.Amount{CostMicros: 80000}}
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON", "ROOT", "TASK"} {
		t.Run(scope, func(t *testing.T) {
			row := egressBudgetRow{scope: scope, currency: "CNY", limit: modelegressbudget.Limits{Requests: 1000, InputTokens: 1000000000, OutputTokens: 1000000000, CostMicros: 1000000000000}}
			if !fitsLiveBudget(row, token) || !fitsLiveBudget(row, call) {
				t.Fatal("original scopes rejected bounded operation")
			}
			cap := liveOwnerCashLimit
			if scope == "ROOT" || scope == "TASK" {
				cap = liveRootCashLimit
				row.used.Requests = 1
			}
			row.used.CostMicros = cap - token.Amount.CostMicros
			if !fitsLiveBudget(row, token) {
				t.Fatal("exact approved cap rejected")
			}
			row.used.CostMicros++
			if fitsLiveBudget(row, token) {
				t.Fatal("over approved cap accepted")
			}
			if scope == "ROOT" || scope == "TASK" {
				row.used.CostMicros = 0
				row.used.Requests = 2
				if fitsLiveBudget(row, call) {
					t.Fatal("third same root attempt accepted")
				}
			}
			row.used = modelegressbudget.Limits{}
			row.currency = "GBP"
			if fitsLiveBudget(row, call) {
				t.Fatal("existing account currency was silently changed")
			}
		})
	}
	row := egressBudgetRow{scope: "ROOT", currency: "CNY", limit: modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680}, used: modelegressbudget.Limits{Requests: 1, CostMicros: 80000}}
	if !fitsLiveBudget(row, token) {
		t.Fatal("two separately authorized API requests were incorrectly constrained to one .20 combined request")
	}
}
func TestLiveLedgerKnownUsageNeverSettlesCashOrExceedsHeldBound(t *testing.T) {
	upper := modelegressbudget.Amount{InputTokens: 196608, OutputTokens: 768, CostMicros: 199680}
	input, output := int64(31), int64(12)
	u := modelgateway.Usage{Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &input, OutputTokens: &output}
	i, o, e := liveUsageObservation(modelegressbudget.LiveToken, upper, u)
	if e != nil || i == nil || o == nil || *i != 31 || *o != 12 {
		t.Fatal("bounded usage observation rejected")
	}
	input = 99
	output = 98
	if *i != 31 || *o != 12 {
		t.Fatal("usage stored caller-owned pointers")
	}
	for _, kind := range []modelegressbudget.LiveChargeKind{modelegressbudget.LiveToken, modelegressbudget.LiveCall} {
		i, o, e = liveUsageObservation(kind, upper, modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"})
		if e != nil || i != nil || o != nil {
			t.Fatal("unknown accounting invented usage")
		}
	}
	for name, usage := range map[string]modelgateway.Usage{"cash_known": {Status: "KNOWN", CostStatus: "KNOWN", InputTokens: &input, OutputTokens: &output}, "half_usage": {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &input}, "mixed_unknown": {Status: "UNKNOWN", CostStatus: "UNKNOWN", InputTokens: &input}, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			if _, _, e := liveUsageObservation(modelegressbudget.LiveToken, upper, usage); !errors.Is(e, modelegressbudget.ErrInvalid) {
				t.Fatal("invalid cash/usage observation accepted")
			}
		})
	}
	input = 196609
	if _, _, e = liveUsageObservation(modelegressbudget.LiveToken, upper, u); !errors.Is(e, modelegressbudget.ErrInvalid) {
		t.Fatal("input above fixed universal hold accepted")
	}
	input = 1
	output = 769
	if _, _, e = liveUsageObservation(modelegressbudget.LiveToken, upper, u); e == nil {
		t.Fatal("output above hold accepted")
	}
	input = 0
	output = 0
	if _, _, e = liveUsageObservation(modelegressbudget.LiveCall, modelegressbudget.Amount{CostMicros: 80000}, u); e == nil {
		t.Fatal("CALL fabricated known token usage")
	}
}
func TestLiveLedgerPhaseAndServerOnlyObjects(t *testing.T) {
	for _, r := range []modelegressbudget.Reservation{{State: "RESERVED", ExecutionStatus: "LIVE_RESERVED"}, {State: "IN_FLIGHT", ExecutionStatus: "LIVE_IN_FLIGHT"}, {State: "UNKNOWN", ExecutionStatus: "LIVE_ATTEMPTED"}} {
		if !liveCheckPhase(r) {
			t.Fatal("exact current phase rejected")
		}
	}
	for _, r := range []modelegressbudget.Reservation{{State: "SETTLED", ExecutionStatus: "LIVE_ATTEMPTED"}, {State: "RESERVED", ExecutionStatus: "LIVE_IN_FLIGHT"}, {State: "UNKNOWN", ExecutionStatus: "UNAVAILABLE"}, {State: "IN_FLIGHT", ExecutionStatus: "LIVE_ATTEMPTED"}} {
		if liveCheckPhase(r) {
			t.Fatal("mismatched or local phase accepted")
		}
	}
	p := PreparedLiveEgress{query: "private question", digest: strings.Repeat("a", 64), price: modelegressbudget.LivePrice{Kind: modelegressbudget.LiveToken}, request: modelgateway.Request{Messages: []modelgateway.Message{{Role: "user", Content: "private question"}}}}
	for _, obj := range []any{p, livePreview(p, "private ID", "DRAFT")} {
		if raw, e := json.Marshal(obj); raw != nil || !errors.Is(e, modelegressbudget.ErrServerOnly) {
			t.Fatal("server object serialized")
		}
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if formatted := fmt.Sprintf(verb, obj); !strings.Contains(formatted, "redacted") || strings.Contains(formatted, "private") {
				t.Fatal("formatter leaked server metadata")
			}
		}
	}
	copyRequest := p.Request()
	copyRequest.Messages[0].Content = "mutated"
	if p.request.Messages[0].Content != "private question" {
		t.Fatal("getter exposed mutable message slice")
	}
	if e := json.Unmarshal([]byte(`{}`), &p); !errors.Is(e, modelegressbudget.ErrServerOnly) || p.query != "" || p.digest != "" {
		t.Fatal("decoder retained preparation")
	}
}
func TestLiveLedgerPriceInstantComparisonAndNilStore(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	a := liveLedgerPrice(now, modelegressbudget.LiveToken)
	b := a
	b.Base.ExpiresAt = b.Base.ExpiresAt.In(time.FixedZone("offset", 3600))
	b.Snapshot.ObservedAt = b.Snapshot.ObservedAt.In(time.FixedZone("offset", 3600))
	if !equalLivePrice(a, b) {
		t.Fatal("same instant changed immutable price")
	}
	b.CallMicros = 80000
	if equalLivePrice(a, b) {
		t.Fatal("price identity change was ignored")
	}
	var s *Store
	ctx := context.Background()
	if e := s.RegisterLivePrice(ctx, a); !errors.Is(e, modelegressbudget.ErrUnavailable) {
		t.Fatal("nil store registered price")
	}
	if _, e := s.ReadLivePrice(ctx, a.Base.Version); !errors.Is(e, modelegressbudget.ErrUnavailable) {
		t.Fatal("nil store read price")
	}
	if _, e := s.PrepareOwnLiveEgress(ctx, agentevent.Access{}, modelegressbudget.PreviewInput{}, nil); !errors.Is(e, modelegressbudget.ErrUnavailable) {
		t.Fatal("nil store prepared request")
	}
}
