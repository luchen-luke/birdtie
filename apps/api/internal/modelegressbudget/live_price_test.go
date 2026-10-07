package modelegressbudget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func livePriceTestNow() time.Time { return time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC) }
func livePriceTestHash(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}
func livePriceFixture(kind LiveChargeKind) LivePrice {
	now := livePriceTestNow()
	s := LiveSnapshot{SourceURL: LiveModelPriceURL, ArtifactSHA256: livePriceTestHash("public tariff fixture only"), DocumentUpdatedAt: now.Add(-24 * time.Hour), ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	p := LivePrice{Base: Price{Version: "hy3.cny.20261008.v1", Destination: LiveHY3Destination(), Region: modelcapability.APAC, Retention: LiveRetentionUnknown, Currency: "CNY", InputMicrosPerToken: 1, OutputMicrosPerToken: 4, InputTokenCeiling: 16_384, OutputTokenCeiling: 768, Evidence: LiveTariffEvidence, ExpiresAt: s.ExpiresAt}, Kind: kind, RequestCeiling: 1, Snapshot: s}
	if kind == LiveCall {
		p.Base.Version = "wsa.cny.20261008.v1"
		p.Base.Destination = LiveWSADestination()
		p.Base.InputMicrosPerToken, p.Base.OutputMicrosPerToken = 0, 0
		p.Base.InputTokenCeiling, p.Base.OutputTokenCeiling = 0, 0
		p.Snapshot.SourceURL = LiveSearchPriceURL
		p.CallMicros = LiveSearchCallMicros
	}
	return p
}
func liveDigestFixture(kind LiveChargeKind) LiveDigestInput {
	in := LiveDigestInput{Scope: Scope, Purpose: Purpose, TaskID: "78000000-0000-4000-8000-000000000001", RootTraceID: "78000000-0000-4000-8000-000000000002", BindingID: "78000000-0000-4000-8000-000000000003", SourceToken: livePriceTestHash("current source fixture"), AuthorityToken: livePriceTestHash("current authority fixture"), CurrentQueryEvidenceDigest: livePriceTestHash("current query fixture"), EgressPayloadDigest: livePriceTestHash("exact wire fixture without key"), MaxOutputTokens: 768, DeadlineAt: livePriceTestNow().Add(time.Minute)}
	if kind == LiveCall {
		in.Purpose, in.MaxOutputTokens = LiveSearchPurpose, 0
	}
	return in
}

func TestLivePriceClosedTariffs(t *testing.T) {
	now := livePriceTestNow()
	for _, kind := range []LiveChargeKind{LiveToken, LiveCall} {
		t.Run(string(kind), func(t *testing.T) {
			p := livePriceFixture(kind)
			if err := ValidateLivePrice(p, now); err != nil {
				t.Fatal(err)
			}
			if err := ValidatePrice(p.Base, now); !errors.Is(err, ErrInvalid) {
				t.Fatal("LIVE entered old LOCAL validation", err)
			}
			output := 768
			want := LiveAmount{Kind: LiveToken, Requests: 1, Amount: Amount{InputTokens: 16_384, OutputTokens: 768, CostMicros: 19_456}}
			if kind == LiveCall {
				output = 0
				want = LiveAmount{Kind: LiveCall, Requests: 1, Amount: Amount{CostMicros: 80_000}}
			}
			got, err := BoundLive(p, output, now)
			if err != nil || got != want {
				t.Fatal(got, err)
			}
			if kind == LiveCall && Fits(Limits{2, 1, 1, 200_000}, Limits{}, got.Amount) {
				t.Fatal("old Fits accepted zero tokens")
			}
		})
	}
	first := LiveHY3Destination()
	first.Provider = "mutated"
	if LiveHY3Destination().Provider != "tencent_tokenhub" {
		t.Fatal("mutable supplier key")
	}
}

func TestLivePriceRejectsMissingOrChangedFacts(t *testing.T) {
	changes := []struct {
		name string
		edit func(*LivePrice)
	}{
		{"kind", func(p *LivePrice) { p.Kind = "MODEL" }},
		{"version_missing", func(p *LivePrice) { p.Base.Version = "" }},
		{"version_latest", func(p *LivePrice) { p.Base.Version = "latest" }},
		{"version_default", func(p *LivePrice) { p.Base.Version = "default" }},
		{"currency", func(p *LivePrice) { p.Base.Currency = "GBP" }},
		{"retention_claim", func(p *LivePrice) { p.Base.Retention = Retention }},
		{"synthetic", func(p *LivePrice) { p.Base.Evidence = LocalPrice }},
		{"paid", func(p *LivePrice) { p.Base.Evidence = "PAID" }},
		{"region", func(p *LivePrice) { p.Base.Region = modelcapability.US }},
		{"provider", func(p *LivePrice) { p.Base.Destination.Provider = "other" }},
		{"model", func(p *LivePrice) { p.Base.Destination.Model = "other" }},
		{"route_version", func(p *LivePrice) { p.Base.Destination.Version = "immutable-weights" }},
		{"wire", func(p *LivePrice) { p.Base.Destination.WireContract = "offline.v1" }},
		{"request_zero", func(p *LivePrice) { p.RequestCeiling = 0 }},
		{"request_two", func(p *LivePrice) { p.RequestCeiling = 2 }},
		{"wrong_doc", func(p *LivePrice) { p.Snapshot.SourceURL = "https://example.invalid/tariff" }},
		{"doc_query", func(p *LivePrice) { p.Snapshot.SourceURL += "?apiKey=ignored" }},
		{"doc_http", func(p *LivePrice) { p.Snapshot.SourceURL = strings.Replace(p.Snapshot.SourceURL, "https:", "http:", 1) }},
		{"hash_missing", func(p *LivePrice) { p.Snapshot.ArtifactSHA256 = "" }},
		{"hash_zero", func(p *LivePrice) { p.Snapshot.ArtifactSHA256 = strings.Repeat("0", 64) }},
		{"hash_upper", func(p *LivePrice) { p.Snapshot.ArtifactSHA256 = strings.ToUpper(p.Snapshot.ArtifactSHA256) }},
		{"hash_bad", func(p *LivePrice) { p.Snapshot.ArtifactSHA256 = strings.Repeat("g", 64) }},
		{"document_missing", func(p *LivePrice) { p.Snapshot.DocumentUpdatedAt = time.Time{} }},
		{"document_future", func(p *LivePrice) { p.Snapshot.DocumentUpdatedAt = livePriceTestNow().Add(time.Hour) }},
		{"observed_missing", func(p *LivePrice) { p.Snapshot.ObservedAt = time.Time{} }},
		{"observed_future", func(p *LivePrice) { p.Snapshot.ObservedAt = livePriceTestNow().Add(time.Second) }},
		{"expires_missing", func(p *LivePrice) { p.Snapshot.ExpiresAt = time.Time{} }},
		{"expiry_mismatch", func(p *LivePrice) { p.Base.ExpiresAt = p.Base.ExpiresAt.Add(time.Minute) }},
		{"expired", func(p *LivePrice) { p.Snapshot.ExpiresAt = livePriceTestNow(); p.Base.ExpiresAt = p.Snapshot.ExpiresAt }},
		{"unbounded", func(p *LivePrice) {
			p.Snapshot.ExpiresAt = p.Snapshot.ObservedAt.Add(24*time.Hour + time.Microsecond)
			p.Base.ExpiresAt = p.Snapshot.ExpiresAt
		}},
		{"nano", func(p *LivePrice) { p.Snapshot.ObservedAt = p.Snapshot.ObservedAt.Add(time.Nanosecond) }},
		{"year", func(p *LivePrice) {
			p.Snapshot.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			p.Base.ExpiresAt = p.Snapshot.ExpiresAt
		}},
	}
	for _, kind := range []LiveChargeKind{LiveToken, LiveCall} {
		for _, c := range changes {
			t.Run(string(kind)+"/"+c.name, func(t *testing.T) {
				p := livePriceFixture(kind)
				c.edit(&p)
				if !errors.Is(ValidateLivePrice(p, livePriceTestNow()), ErrInvalid) {
					t.Fatal("invalid tariff accepted")
				}
			})
		}
	}
	for name, edit := range map[string]func(*LivePrice){
		"input_zero":      func(p *LivePrice) { p.Base.InputTokenCeiling = 0 },
		"input_oversize":  func(p *LivePrice) { p.Base.InputTokenCeiling = LiveMaxInputTokens + 1 },
		"output_zero":     func(p *LivePrice) { p.Base.OutputTokenCeiling = 0 },
		"output_oversize": func(p *LivePrice) { p.Base.OutputTokenCeiling = LiveMaxOutputTokens + 1 },
		"cached_discount": func(p *LivePrice) { p.Base.InputMicrosPerToken = 0 },
		"input_price":     func(p *LivePrice) { p.Base.InputMicrosPerToken = 2 },
		"output_price":    func(p *LivePrice) { p.Base.OutputMicrosPerToken = 3 },
		"hybrid_call":     func(p *LivePrice) { p.CallMicros = LiveSearchCallMicros },
	} {
		t.Run("TOKEN/"+name, func(t *testing.T) {
			p := livePriceFixture(LiveToken)
			edit(&p)
			if ValidateLivePrice(p, livePriceTestNow()) == nil {
				t.Fatal("bad token tariff")
			}
		})
	}
	for name, edit := range map[string]func(*LivePrice){
		"input_fake_token":  func(p *LivePrice) { p.Base.InputTokenCeiling = 1 },
		"output_fake_token": func(p *LivePrice) { p.Base.OutputTokenCeiling = 1 },
		"input_rate":        func(p *LivePrice) { p.Base.InputMicrosPerToken = 1 },
		"output_rate":       func(p *LivePrice) { p.Base.OutputMicrosPerToken = 1 },
		"free":              func(p *LivePrice) { p.CallMicros = 0 },
		"unproven_lite":     func(p *LivePrice) { p.CallMicros = 18_000 },
		"oversize":          func(p *LivePrice) { p.CallMicros = 80_001 },
		"overflow":          func(p *LivePrice) { p.CallMicros = math.MaxInt64 },
	} {
		t.Run("CALL/"+name, func(t *testing.T) {
			p := livePriceFixture(LiveCall)
			edit(&p)
			if ValidateLivePrice(p, livePriceTestNow()) == nil {
				t.Fatal("bad call tariff")
			}
		})
	}
	if ValidateLivePrice(livePriceFixture(LiveToken), time.Time{}) == nil {
		t.Fatal("missing current time")
	}
}

func TestLiveAmountFitsSameCombinedBudget(t *testing.T) {
	now := livePriceTestNow()
	call, _ := BoundLive(livePriceFixture(LiveCall), 0, now)
	token, _ := BoundLive(livePriceFixture(LiveToken), 768, now)
	limit := Limits{Requests: 2, InputTokens: 16_384, OutputTokens: 768, CostMicros: 200_000}
	if !FitsLive(limit, Limits{}, call) || !FitsLive(limit, Limits{Requests: 1, CostMicros: 80_000}, token) || !FitsLive(limit, Limits{Requests: 1, InputTokens: 16_384, OutputTokens: 768, CostMicros: 19_456}, call) {
		t.Fatal("same combined root budget rejected")
	}
	for _, used := range []Limits{{Requests: 2}, {Requests: -1}, {InputTokens: -1}, {OutputTokens: -1}, {CostMicros: -1}, {InputTokens: 16_385}, {OutputTokens: 769}, {CostMicros: 120_001}, {CostMicros: math.MaxInt64}} {
		if FitsLive(limit, used, call) {
			t.Fatal("CALL over budget accepted", used)
		}
	}
	if !FitsLive(limit, Limits{CostMicros: 120_000}, call) || FitsLive(Limits{1, 1, 1, 79_999}, Limits{}, call) {
		t.Fatal("exact CALL fee boundary")
	}
	for _, a := range []LiveAmount{{Kind: LiveCall, Requests: 0, Amount: call.Amount}, {Kind: LiveCall, Requests: 2, Amount: call.Amount}, {Kind: LiveCall, Requests: 1, Amount: Amount{CostMicros: 79_999}}, {Kind: LiveCall, Requests: 1, Amount: Amount{InputTokens: 1, CostMicros: 80_000}}, {Kind: LiveToken, Requests: 1, Amount: Amount{InputTokens: math.MaxInt64, OutputTokens: 1, CostMicros: 1}}, {Kind: LiveToken, Requests: 1, Amount: Amount{InputTokens: 1, OutputTokens: 1, CostMicros: 4}}, {Kind: "OTHER", Requests: 1, Amount: call.Amount}} {
		if FitsLive(limit, Limits{}, a) {
			t.Fatal("invalid typed amount accepted", a)
		}
	}
	for _, kind := range []LiveChargeKind{LiveToken, LiveCall} {
		for _, output := range []int{-1, 769, math.MaxInt} {
			if _, e := BoundLive(livePriceFixture(kind), output, now); e == nil {
				t.Fatal("bad output accepted")
			}
		}
	}
	if _, e := BoundLive(livePriceFixture(LiveToken), 0, now); e == nil {
		t.Fatal("TOKEN zero output")
	}
	if _, e := BoundLive(livePriceFixture(LiveCall), 1, now); e == nil {
		t.Fatal("CALL fake output")
	}
	large := livePriceFixture(LiveToken)
	large.Base.InputTokenCeiling = 120_000
	largeUpper, _ := BoundLive(large, 768, now)
	if FitsLive(Limits{2, 120_000, 768, 200_000}, Limits{Requests: 1, CostMicros: 80_000}, largeUpper) {
		t.Fatal("WSA plus model exceeded shared .20")
	}
}

func TestLiveDigestBindsExplicitCurrentFacts(t *testing.T) {
	now := livePriceTestNow()
	for _, kind := range []LiveChargeKind{LiveToken, LiveCall} {
		t.Run(string(kind), func(t *testing.T) {
			p, in := livePriceFixture(kind), liveDigestFixture(kind)
			base, err := DigestLive(in, p, now)
			if err != nil || !liveHash(base) {
				t.Fatal(base, err)
			}
			if again, e := DigestLive(in, p, now); e != nil || again != base {
				t.Fatal("unstable explicit digest", e)
			}
			for name, edit := range map[string]func(*LiveDigestInput){
				"task":          func(i *LiveDigestInput) { i.TaskID = "78000000-0000-4000-8000-000000000004" },
				"root":          func(i *LiveDigestInput) { i.RootTraceID = "78000000-0000-4000-8000-000000000005" },
				"binding":       func(i *LiveDigestInput) { i.BindingID = "78000000-0000-4000-8000-000000000006" },
				"source":        func(i *LiveDigestInput) { i.SourceToken = livePriceTestHash("new source") },
				"authority":     func(i *LiveDigestInput) { i.AuthorityToken = livePriceTestHash("new authority") },
				"current_query": func(i *LiveDigestInput) { i.CurrentQueryEvidenceDigest = livePriceTestHash("new current query") },
				"exact_wire":    func(i *LiveDigestInput) { i.EgressPayloadDigest = livePriceTestHash("new wire") },
				"deadline":      func(i *LiveDigestInput) { i.DeadlineAt = i.DeadlineAt.Add(time.Second) },
			} {
				t.Run(name, func(t *testing.T) {
					changed := in
					edit(&changed)
					d, e := DigestLive(changed, p, now)
					if e != nil || d == base {
						t.Fatal("changed facts kept digest", e)
					}
				})
			}
			for name, edit := range map[string]func(*LivePrice){
				"price_version": func(p *LivePrice) { p.Base.Version += ".new" },
				"artifact":      func(p *LivePrice) { p.Snapshot.ArtifactSHA256 = livePriceTestHash("new artifact") },
				"document_date": func(p *LivePrice) { p.Snapshot.DocumentUpdatedAt = p.Snapshot.DocumentUpdatedAt.Add(time.Second) },
				"observed":      func(p *LivePrice) { p.Snapshot.ObservedAt = p.Snapshot.ObservedAt.Add(time.Second) },
				"expiry": func(p *LivePrice) {
					p.Snapshot.ExpiresAt = p.Snapshot.ExpiresAt.Add(time.Second)
					p.Base.ExpiresAt = p.Snapshot.ExpiresAt
				},
			} {
				t.Run(name, func(t *testing.T) {
					changed := p
					edit(&changed)
					d, e := DigestLive(in, changed, now)
					if e != nil || d == base {
						t.Fatal("changed snapshot kept digest", e)
					}
				})
			}
			if kind == LiveToken {
				changed := p
				changed.Base.InputTokenCeiling++
				d, e := DigestLive(in, changed, now)
				if e != nil || d == base {
					t.Fatal("input ceiling not bound")
				}
				changed = p
				changed.Base.OutputTokenCeiling--
				i := in
				i.MaxOutputTokens--
				d, e = DigestLive(i, changed, now)
				if e != nil || d == base {
					t.Fatal("output ceiling not bound")
				}
			}
			for name, edit := range map[string]func(*LiveDigestInput){
				"scope":            func(i *LiveDigestInput) { i.Scope = "FULL_HISTORY" },
				"purpose":          func(i *LiveDigestInput) { i.Purpose = "OTHER" },
				"task_missing":     func(i *LiveDigestInput) { i.TaskID = "" },
				"root_zero":        func(i *LiveDigestInput) { i.RootTraceID = "00000000-0000-0000-0000-000000000000" },
				"binding_bad":      func(i *LiveDigestInput) { i.BindingID = "fixture" },
				"source_missing":   func(i *LiveDigestInput) { i.SourceToken = "" },
				"authority_zero":   func(i *LiveDigestInput) { i.AuthorityToken = strings.Repeat("0", 64) },
				"query_missing":    func(i *LiveDigestInput) { i.CurrentQueryEvidenceDigest = "" },
				"wire_missing":     func(i *LiveDigestInput) { i.EgressPayloadDigest = "" },
				"deadline_missing": func(i *LiveDigestInput) { i.DeadlineAt = time.Time{} },
				"deadline_expired": func(i *LiveDigestInput) { i.DeadlineAt = now },
				"deadline_long":    func(i *LiveDigestInput) { i.DeadlineAt = now.Add(modelgateway.MaxDeadline + time.Microsecond) },
				"deadline_nano":    func(i *LiveDigestInput) { i.DeadlineAt = i.DeadlineAt.Add(time.Nanosecond) },
			} {
				t.Run("reject_"+name, func(t *testing.T) {
					changed := in
					edit(&changed)
					if d, e := DigestLive(changed, p, now); e == nil || d != "" {
						t.Fatal("bad digest facts accepted")
					}
				})
			}
			zone := time.FixedZone("same instant", 8*60*60)
			p.Snapshot.DocumentUpdatedAt = p.Snapshot.DocumentUpdatedAt.In(zone)
			p.Snapshot.ObservedAt = p.Snapshot.ObservedAt.In(zone)
			p.Snapshot.ExpiresAt = p.Snapshot.ExpiresAt.In(zone)
			p.Base.ExpiresAt = p.Base.ExpiresAt.In(zone)
			in.DeadlineAt = in.DeadlineAt.In(zone)
			if d, e := DigestLive(in, p, now); e != nil || d != base {
				t.Fatal("timestamp zone changed same digest", e)
			}
		})
	}
}

func TestLiveHoldUsageNeverSettlesCash(t *testing.T) {
	now := livePriceTestNow()
	p := livePriceFixture(LiveToken)
	i, o := int64(31), int64(12)
	for _, usage := range []modelgateway.Usage{{Status: "UNKNOWN", CostStatus: "UNKNOWN"}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &i, OutputTokens: &o}} {
		got, err := HoldLive(p, 768, usage, now)
		if err != nil || got.Upper != got.Held || got.Held.Amount.CostMicros != 19_456 || got.CashStatus != "UNKNOWN" {
			t.Fatal("usage released cost hold", got, err)
		}
		if usage.Status == "KNOWN" {
			if got.ReportedInput == nil || got.ReportedOutput == nil || *got.ReportedInput != i || *got.ReportedOutput != o {
				t.Fatal("known usage lost")
			}
			*got.ReportedInput++
			if i != 31 {
				t.Fatal("shared usage pointer")
			}
		}
	}
	negative, huge, zero := int64(-1), int64(math.MaxInt64), int64(0)
	for _, usage := range []modelgateway.Usage{{Status: "KNOWN", CostStatus: "PAID", InputTokens: &i, OutputTokens: &o}, {Status: "KNOWN", CostStatus: "FREE", InputTokens: &i, OutputTokens: &o}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &i}, {Status: "UNKNOWN", CostStatus: "UNKNOWN", InputTokens: &i}, {Status: "OTHER", CostStatus: "UNKNOWN"}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &negative, OutputTokens: &o}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &huge, OutputTokens: &o}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &i, OutputTokens: &huge}, {Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &i, OutputTokens: &negative}} {
		if _, e := HoldLive(p, 768, usage, now); e == nil {
			t.Fatal("invalid/paid usage accepted", usage.Status, usage.CostStatus)
		}
	}
	call := livePriceFixture(LiveCall)
	got, e := HoldLive(call, 0, modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}, now)
	if e != nil || got.Held.Amount != (Amount{CostMicros: 80_000}) || got.Held.Requests != 1 || got.ReportedInput != nil {
		t.Fatal("WSA hold changed", got, e)
	}
	if _, e = HoldLive(call, 0, modelgateway.Usage{Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &zero, OutputTokens: &zero}, now); e == nil {
		t.Fatal("WSA fake model token usage accepted")
	}
}

func TestLivePricePreservesOriginalV1DigestAndValidation(t *testing.T) {
	// Fixed original v1 byte fixture, independent of Price JSON authority.
	requestWire := "{\"schema_version\":\"\",\"run_id\":\"\",\"agent_ref\":{\"AgentID\":\"\",\"Principal\":{\"type\":\"\",\"id\":\"\"},\"Role\":\"\"},\"task_kind\":\"\",\"prompt_version\":\"\",\"input_schema_version\":\"\",\"output_schema_version\":\"\",\"context_snapshot_ref\":\"\",\"data_policy_ref\":\"\",\"budget_ref\":\"\",\"budget\":{\"max_output_tokens\":0},\"messages\":null,\"output_mode\":\"\",\"tool_allowlist\":null,\"capabilities_required\":null,\"deadline_at\":\"0001-01-01T00:00:00Z\"}"
	priceWire := ",\"Version\":\"\",\"Destination\":{\"Provider\":\"\",\"Model\":\"\",\"Version\":\"\",\"WireContract\":\"\"},\"Region\":\"\",\"Retention\":\"\",\"Currency\":\"\",\"InputRate\":0,\"OutputRate\":0,\"InputCeiling\":0,\"OutputCeiling\":0}"
	h := sha256.Sum256([]byte("birdtie.model-egress.self-query.v1\x00{\"Request\":" + requestWire + priceWire))
	if got := Digest(modelgateway.Request{}, Price{}); got != hex.EncodeToString(h[:]) {
		t.Fatal("original v1 bytes changed", got)
	}
	p := testPrice()
	if ValidatePrice(p, time.Now()) != nil {
		t.Fatal("LOCAL validator changed")
	}
	upper, e := Bound(p, 128)
	if e != nil || upper != (Amount{100, 128, 584}) || !Fits(Limits{2, 200, 256, 1168}, Limits{}, upper) {
		t.Fatal("LOCAL arithmetic changed")
	}
	r := modelgateway.Request{}
	old := Digest(r, p)
	p.ExpiresAt = p.ExpiresAt.Add(time.Minute)
	if Digest(r, p) != old {
		t.Fatal("old v1 unexpectedly added expiry")
	}
	live, e := DigestLive(liveDigestFixture(LiveToken), livePriceFixture(LiveToken), livePriceTestNow())
	if e != nil || live == Digest(r, livePriceFixture(LiveToken).Base) {
		t.Fatal("v2 reused v1 domain")
	}
	if _, e := json.Marshal(livePriceFixture(LiveToken)); !errors.Is(e, ErrServerOnly) {
		t.Fatal("live price reconstructed through JSON", e)
	}
	v := livePriceFixture(LiveToken)
	if e := json.Unmarshal([]byte("{\"approved\":true}"), &v); !errors.Is(e, ErrServerOnly) || v != (LivePrice{}) {
		t.Fatal("JSON became live authorization", e)
	}
	var nilPrice *LivePrice
	if e := nilPrice.UnmarshalJSON([]byte("{}")); !errors.Is(e, ErrServerOnly) {
		t.Fatal("nil receiver became authority", e)
	}
}
