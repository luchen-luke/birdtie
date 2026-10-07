package modelcapability

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"strings"
	"testing"
	"time"
)

const uuidBase = "74000000-0000-4000-8000-00000000000"

func fixtureRequest(now time.Time, mode modelgateway.OutputMode) modelgateway.Request {
	caps := []string{"text"}
	if mode != modelgateway.Text {
		caps = append(caps, "structured_output_validatable")
	}
	if mode == modelgateway.ToolProposals {
		caps = append(caps, "tools")
	}
	return modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: uuidBase + "1",
		Agent:    agentcognitive.AgentReference{AgentID: uuidBase + "3", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: uuidBase + "2"}, Role: agentruntime.PersonalAgent},
		TaskKind: modelgateway.ActivityQuery, PromptVersion: "activity_query.v1", InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1",
		ContextSnapshotRef: uuidBase + "4", DataPolicyRef: uuidBase + "5", BudgetRef: uuidBase + "6", Budget: modelgateway.Budget{MaxOutputTokens: 128},
		Messages: []modelgateway.Message{{Role: "system", Content: "合成固定提示"}, {Role: "user", Content: "合成活动问句"}}, OutputMode: mode,
		ToolAllowlist: []string{"activity.search", "activity.detail"}, CapabilitiesRequired: caps, DeadlineAt: now.Add(time.Minute)}
}
func grant(reg *Registry, r modelgateway.Request, n Requirements, now time.Time) OfflineGrant {
	version := agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: strings.Repeat("a", 64)}
	g := OfflineGrant{Agent: r.Agent, RequestDigest: RequestDigest(r, n), RegistryDigest: reg.Digest(), SourceVersion: version, CurrentSourceVersion: version,
		Purpose: "MODEL_CONTEXT_EGRESS", ConsentRevision: 1, CurrentConsentRevision: 1, PolicyRevision: 2, CurrentPolicyRevision: 2, State: "ALLOWED", CheckedAt: now, ExpiresAt: now.Add(30 * time.Second)}
	for _, record := range reg.Records() {
		g.Destinations = append(g.Destinations, Destination{record.Key, EU, true, true})
	}
	return g
}
func TestCurrentAuthorizationClosedBeforeRanking(t *testing.T) {
	now := time.Now().UTC()
	reg := registry(t, fixtureRecord(now))
	r := fixtureRequest(now, modelgateway.Text)
	n := Requirements{}
	cases := []struct {
		name   string
		change func(*OfflineGrant)
	}{
		{"unknown", func(g *OfflineGrant) { g.State = "UNKNOWN" }}, {"unavailable", func(g *OfflineGrant) { g.State = "UNAVAILABLE" }},
		{"verified", func(g *OfflineGrant) { g.State = "VERIFIED" }}, {"revoked", func(g *OfflineGrant) { g.Revoked = true }}, {"deleted", func(g *OfflineGrant) { g.Deleted = true }},
		{"agent", func(g *OfflineGrant) { g.Agent.AgentID = uuidBase + "9" }}, {"principal", func(g *OfflineGrant) { g.Agent.Principal.ID = uuidBase + "9" }},
		{"organization_namespace", func(g *OfflineGrant) { g.Agent.Principal.Type = actorref.Organization }}, {"role", func(g *OfflineGrant) { g.Agent.Role = agentruntime.OrganizationAgent }},
		{"request_digest", func(g *OfflineGrant) { g.RequestDigest = strings.Repeat("b", 64) }}, {"registry_digest", func(g *OfflineGrant) { g.RegistryDigest = strings.Repeat("b", 64) }},
		{"purpose", func(g *OfflineGrant) { g.Purpose = "PROFILE_VIEW" }}, {"zero_consent", func(g *OfflineGrant) { g.ConsentRevision = 0 }},
		{"consent_changed", func(g *OfflineGrant) { g.CurrentConsentRevision++ }}, {"zero_policy", func(g *OfflineGrant) { g.PolicyRevision = 0 }},
		{"policy_changed", func(g *OfflineGrant) { g.CurrentPolicyRevision++ }}, {"source_changed", func(g *OfflineGrant) { g.CurrentSourceVersion.Token = strings.Repeat("b", 64) }},
		{"source_fake_revision", func(g *OfflineGrant) { g.SourceVersion.Revision = 1; g.CurrentSourceVersion = g.SourceVersion }},
		{"source_kind", func(g *OfflineGrant) {
			g.SourceVersion.Kind = "CLIENT_VERIFIED"
			g.CurrentSourceVersion = g.SourceVersion
		}},
		{"source_uppercase", func(g *OfflineGrant) {
			g.SourceVersion.Token = strings.Repeat("A", 64)
			g.CurrentSourceVersion = g.SourceVersion
		}},
		{"source_missing", func(g *OfflineGrant) { g.SourceVersion.Token = ""; g.CurrentSourceVersion = g.SourceVersion }},
		{"empty_destination", func(g *OfflineGrant) { g.Destinations = nil }}, {"duplicate_destination", func(g *OfflineGrant) { g.Destinations = append(g.Destinations, g.Destinations[0]) }},
		{"unknown_destination_region", func(g *OfflineGrant) { g.Destinations[0].Region = "GLOBAL" }},
		{"stale_checked", func(g *OfflineGrant) { g.CheckedAt = now.Add(-time.Nanosecond) }}, {"future_checked", func(g *OfflineGrant) { g.CheckedAt = now.Add(time.Nanosecond) }},
		{"expiry_equal", func(g *OfflineGrant) { g.ExpiresAt = now }}, {"expiry_past", func(g *OfflineGrant) { g.ExpiresAt = now.Add(-time.Nanosecond) }},
		{"expiry_after_deadline", func(g *OfflineGrant) { g.ExpiresAt = r.DeadlineAt.Add(time.Nanosecond) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := grant(reg, r, n, now)
			tc.change(&g)
			if p, e := SelectOffline(reg, r, n, g, now); e == nil || p.Key.Provider != "" {
				t.Fatal("stale authority selected", e)
			}
		})
	}
	for _, change := range []func(*modelgateway.Request){func(r *modelgateway.Request) {
		r.Agent.Principal.Type = actorref.Business
		r.Agent.Role = agentruntime.BusinessAgent
	}, func(r *modelgateway.Request) { r.Agent.Principal.Type = actorref.Community }, func(r *modelgateway.Request) { r.DeadlineAt = now }, func(r *modelgateway.Request) { r.CapabilitiesRequired = []string{"vision"} }} {
		bad := r
		change(&bad)
		if _, e := SelectOffline(reg, bad, n, grant(reg, bad, n, now), now); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid actual 007 shape accepted", e)
		}
	}
	org := r
	org.Agent.Principal.Type = actorref.Organization
	org.Agent.Role = agentruntime.OrganizationAgent
	if _, e := SelectOffline(reg, org, n, grant(reg, org, n, now), now); e != nil {
		t.Fatal("typed Organization offline shape", e)
	}
}
func TestSixCapabilityMetadataMatrix(t *testing.T) {
	now := time.Now().UTC()
	for _, field := range []string{"vision", "tools", "schema", "streaming", "state", "storage"} {
		for _, support := range []Support{Supported, Unsupported, Unknown} {
			t.Run(field+"_"+string(support), func(t *testing.T) {
				record := fixtureRecord(now)
				r := fixtureRequest(now, modelgateway.Text)
				n := Requirements{}
				switch field {
				case "vision":
					record.Capabilities.Vision = support
					n.Vision = true
				case "tools":
					record.Capabilities.Tools = support
					r = fixtureRequest(now, modelgateway.ToolProposals)
				case "schema":
					record.Capabilities.Schema = support
					r = fixtureRequest(now, modelgateway.Structured)
				case "streaming":
					record.Capabilities.Streaming = support
					n.Streaming = true
				case "state":
					record.Capabilities.State = support
					n.State = true
				case "storage":
					record.Capabilities.Storage = support
					n.Storage = true
				}
				reg := registry(t, record)
				g := grant(reg, r, n, now)
				p, e := SelectOffline(reg, r, n, g, now)
				allowed := support == Supported || (field == "schema" && support == Unsupported)
				if allowed {
					if e != nil || p.Mode != modelgateway.OfflineContract || p.LocalSchemaRequired != (r.OutputMode != modelgateway.Text) {
						t.Fatal("offline matrix", e)
					}
				} else if !errors.Is(e, ErrUnsupported) {
					t.Fatal("unknown/unsupported admitted", e)
				}
			})
		}
	}
}
func TestEligibilityExactVersionRegionDateAndPurpose(t *testing.T) {
	now := time.Now().UTC()
	r := fixtureRequest(now, modelgateway.Text)
	for _, name := range []string{"old_version", "wrong_provider", "wrong_model", "wrong_wire", "wrong_region", "future_checked", "record_expired", "documentation_only", "text_unknown", "text_unsupported", "state_not_approved", "storage_not_approved"} {
		t.Run(name, func(t *testing.T) {
			rec := fixtureRecord(now)
			n := Requirements{}
			if name == "future_checked" {
				rec.ValidatedAt = now.Add(time.Nanosecond)
			}
			if name == "record_expired" {
				rec.ValidatedAt = now.Add(-time.Hour)
				rec.ExpiresAt = now
			}
			if name == "documentation_only" {
				rec.Evidence = DocumentationChecked
				rec.References = []string{"https://developers.openai.com/api/docs/guides/structured-outputs"}
			}
			if name == "text_unknown" {
				rec.Text = Unknown
			}
			if name == "text_unsupported" {
				rec.Text = Unsupported
			}
			if name == "state_not_approved" {
				n.State = true
			}
			if name == "storage_not_approved" {
				n.Storage = true
			}
			reg := registry(t, rec)
			g := grant(reg, r, n, now)
			switch name {
			case "old_version":
				g.Destinations[0].Key.Version = "fixture-v0"
			case "wrong_provider":
				g.Destinations[0].Key.Provider = "other-provider"
			case "wrong_model":
				g.Destinations[0].Key.Model = "other-model"
			case "wrong_wire":
				g.Destinations[0].Key.WireContract = "other-wire.v1"
			case "wrong_region":
				g.Destinations[0].Region = US
			case "state_not_approved":
				g.Destinations[0].AllowState = false
			case "storage_not_approved":
				g.Destinations[0].AllowStorage = false
			}
			if _, e := SelectOffline(reg, r, n, g, now); !errors.Is(e, ErrUnsupported) {
				t.Fatal("ineligible destination selected", e)
			}
		})
	}
	reg := registry(t, fixtureRecord(now))
	g := grant(reg, r, Requirements{}, now)
	changed := fixtureRecord(now)
	changed.QualityRank++
	if _, e := SelectOffline(registry(t, changed), r, Requirements{}, g, now); !errors.Is(e, ErrDenied) {
		t.Fatal("old registry grant reused", e)
	}
}
func TestDeterministicQualityCostAfterEligibility(t *testing.T) {
	now := time.Now().UTC()
	r := fixtureRequest(now, modelgateway.Text)
	a := fixtureRecord(now)
	b := a
	b.Key.Model = "fake-second"
	c := a
	c.Key.Model = "fake-cheapest"
	c.QualityRank = 1000
	v := int64(0)
	c.CostEstimateMicros = &v
	reg := registry(t, c, b, a)
	g := grant(reg, r, Requirements{}, now)
	g.Destinations = g.Destinations[1:] // sorted cheapest removed before ranking
	p, e := SelectOffline(reg, r, Requirements{}, g, now)
	if e != nil || p.Key != b.Key || p.CostStatus != "UNKNOWN" {
		t.Fatal("unauthorized expensive quality won", e)
	}
	v2 := int64(8)
	b.CostEstimateMicros = &v2
	reg = registry(t, b, a)
	p, e = SelectOffline(reg, r, Requirements{}, grant(reg, r, Requirements{}, now), now)
	if e != nil || p.Key != b.Key || p.CostStatus != "SYNTHETIC_ESTIMATE" {
		t.Fatal("known synthetic cost ordering", e)
	}
	v3 := int64(9)
	a.CostEstimateMicros = &v3
	reg = registry(t, a, b)
	p, _ = SelectOffline(reg, r, Requirements{}, grant(reg, r, Requirements{}, now), now)
	if p.Key != b.Key {
		t.Fatal("lower cost not selected")
	}
	a.QualityRank = 11
	reg = registry(t, b, a)
	p, _ = SelectOffline(reg, r, Requirements{}, grant(reg, r, Requirements{}, now), now)
	if p.Key != a.Key {
		t.Fatal("quality priority changed")
	}
	a.Text = Unsupported
	reg = registry(t, a, b)
	p, _ = SelectOffline(reg, r, Requirements{}, grant(reg, r, Requirements{}, now), now)
	if p.Key != b.Key {
		t.Fatal("unsupported high quality selected")
	}
}

type fakeAdapter struct {
	descriptor modelgateway.ProviderDescriptor
	wire       string
	raw        string
	calls      int
	received   modelgateway.ProviderRequest
	hook       func()
}

func (f *fakeAdapter) Descriptor() modelgateway.ProviderDescriptor { return f.descriptor }
func (f *fakeAdapter) WireContractVersion() string                 { return f.wire }
func (f *fakeAdapter) Complete(_ context.Context, r modelgateway.ProviderRequest) ([]byte, error) {
	f.calls++
	f.received = r
	if f.hook != nil {
		f.hook()
	}
	return []byte(f.raw), nil
}
func fake(rec Record) *fakeAdapter {
	return &fakeAdapter{descriptor: modelgateway.ProviderDescriptor{ProviderID: rec.Key.Provider, ModelID: rec.Key.Model, ModelVersion: rec.Key.Version, Mode: modelgateway.OfflineContract}, wire: rec.Key.WireContract, raw: `{"status":"COMPLETED","request_id":"fake-request","finish_reason":"stop","text":"合成回答"}`}
}
func runner(t *testing.T, reg *Registry, f *fakeAdapter, now time.Time) *OfflineRunner {
	t.Helper()
	run, e := NewOfflineRunner(reg, []OfflineAdapter{f})
	if e != nil {
		t.Fatal(e)
	}
	run.now = func() time.Time { return now }
	return run
}

func TestOfflineRunnerStrictLocalSchemaWithoutNativeSupport(t *testing.T) {
	now := time.Now().UTC()
	rec := fixtureRecord(now)
	rec.Capabilities.Schema = Unsupported
	reg := registry(t, rec)
	r := fixtureRequest(now, modelgateway.Structured)
	cases := []struct {
		name, raw string
		good      bool
	}{
		{"valid", `{"status":"COMPLETED","request_id":"fake-schema","finish_reason":"stop","structured":{"answer":"合成建议","entity_refs":[]}}`, true},
		{"unknown_field", `{"status":"COMPLETED","request_id":"fake-schema","finish_reason":"stop","structured":{"answer":"合成建议","entity_refs":[],"approved":true}}`, false},
		{"missing_field", `{"status":"COMPLETED","request_id":"fake-schema","finish_reason":"stop","structured":{"answer":"合成建议"}}`, false},
		{"duplicate_key", `{"status":"COMPLETED","request_id":"fake-schema","finish_reason":"stop","structured":{"answer":"合成建议","answer":"绕过","entity_refs":[]}}`, false},
		{"wrong_mode", `{"status":"COMPLETED","request_id":"fake-schema","finish_reason":"stop","text":"未校验文本"}`, false},
		{"truncated", `{"status":"TRUNCATED","request_id":"fake-schema","finish_reason":"length","structured":{"answer":"部分建议","entity_refs":[]}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fake(rec)
			f.raw = tc.raw
			run := runner(t, reg, f, now)
			result, e := run.Complete(context.Background(), r, Requirements{}, grant(reg, r, Requirements{}, now))
			if tc.good {
				if e != nil || result.Answer == nil || result.Mode != modelgateway.OfflineContract || result.Usage.CostStatus != "UNKNOWN" {
					t.Fatal("local schema positive", e)
				}
			} else if result.Answer != nil || result.Text != "" || len(result.ToolProposals) > 0 {
				t.Fatal("unvalidated output released", e)
			}
			if f.calls != 1 {
				t.Fatal("fake not exercised")
			}
		})
	}
}
func TestRunnerNoUnsupportedModalityOrStaleAuthorizationDispatch(t *testing.T) {
	now := time.Now().UTC()
	rec := fixtureRecord(now)
	reg := registry(t, rec)
	r := fixtureRequest(now, modelgateway.Text)
	for _, n := range []Requirements{{Vision: true}, {Streaming: true}, {State: true}, {Storage: true}} {
		f := fake(rec)
		run := runner(t, reg, f, now)
		if _, e := run.Complete(context.Background(), r, n, grant(reg, r, n, now)); !errors.Is(e, ErrUnsupported) || f.calls != 0 {
			t.Fatal("007 unsupported modality dispatched", e)
		}
	}
	f := fake(rec)
	run := runner(t, reg, f, now)
	g := grant(reg, r, Requirements{}, now)
	g.Revoked = true
	if _, e := run.Complete(context.Background(), r, Requirements{}, g); !errors.Is(e, ErrDenied) || f.calls != 0 {
		t.Fatal("revoked dispatched", e)
	}
	raw, _ := json.Marshal(r)
	raw = append(raw[:len(raw)-1], []byte(`,"image_bytes":"private-image","Verified":true}`)...)
	if _, e := modelgateway.DecodeRequest(raw, now); e == nil || f.calls != 0 {
		t.Fatal("image/client fields admitted")
	}
}
func TestRunnerPostResponseExpiryAndWireChangeDiscard(t *testing.T) {
	for _, name := range []string{"expired", "capability_expired", "wire_changed", "mode_changed", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			rec := fixtureRecord(now)
			if name == "capability_expired" {
				rec.ExpiresAt = now.Add(time.Second)
			}
			reg := registry(t, rec)
			r := fixtureRequest(now, modelgateway.Text)
			f := fake(rec)
			run := runner(t, reg, f, now)
			g := grant(reg, r, Requirements{}, now)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.hook = func() {
				switch name {
				case "expired":
					run.now = func() time.Time { return g.ExpiresAt }
				case "capability_expired":
					run.now = func() time.Time { return rec.ExpiresAt }
				case "wire_changed":
					f.wire = "changed.v2"
				case "mode_changed":
					f.descriptor.Mode = "LIVE"
				case "cancelled":
					cancel()
				}
			}
			result, e := run.Complete(ctx, r, Requirements{}, g)
			if e == nil || result.Text != "" || result.Answer != nil || f.calls != 1 {
				t.Fatal("late invalid content released", e)
			}
		})
	}
}

func TestToolSchemaPermissionClaimsAndSelectorsStayLocal(t *testing.T) {
	now := time.Now().UTC()
	rec := fixtureRecord(now)
	rec.Capabilities.Schema = Unsupported
	reg := registry(t, rec)
	r := fixtureRequest(now, modelgateway.ToolProposals)
	for _, tc := range []struct {
		name, raw string
		good      bool
	}{
		{"known_proposal", `{"status":"COMPLETED","request_id":"fake-tools","finish_reason":"stop","tool_proposals":[{"tool":"activity.search","arguments":{"query":"合成问句","city_id":"aberdeen-gb"},"reason_summary":"只读提案"}]}`, true},
		{"unknown_tool", `{"status":"COMPLETED","request_id":"fake-tools","finish_reason":"stop","tool_proposals":[{"tool":"grant.approve","arguments":{},"reason_summary":"自授权"}]}`, false},
		{"tool_permission", `{"status":"COMPLETED","request_id":"fake-tools","finish_reason":"stop","tool_proposals":[{"tool":"activity.search","arguments":{"query":"合成问句","city_id":"aberdeen-gb","approved":true},"reason_summary":"伪权限"}]}`, false},
		{"top_permission", `{"status":"COMPLETED","request_id":"fake-tools","finish_reason":"stop","tool_proposals":[],"Verified":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fake(rec)
			f.raw = tc.raw
			run := runner(t, reg, f, now)
			out, e := run.Complete(context.Background(), r, Requirements{}, grant(reg, r, Requirements{}, now))
			if tc.good {
				if e != nil || len(out.ToolProposals) != 1 {
					t.Fatal("proposal contract", e)
				}
			} else if e == nil || len(out.ToolProposals) != 0 {
				t.Fatal("unknown tool/permission escaped schema", e)
			}
			wire, _ := json.Marshal(f.received)
			for _, ref := range []string{r.RunID, r.Agent.AgentID, r.Agent.Principal.ID, r.ContextSnapshotRef, r.DataPolicyRef, r.BudgetRef} {
				if strings.Contains(string(wire), ref) {
					t.Fatal("native selector leaked to fake payload")
				}
			}
		})
	}
}

func TestPermissionPrecedesUnsupportedModalityAndMissingAdapter(t *testing.T) {
	now := time.Now().UTC()
	rec := fixtureRecord(now)
	reg := registry(t, rec)
	r := fixtureRequest(now, modelgateway.Text)
	f := fake(rec)
	run := runner(t, reg, f, now)
	n := Requirements{Vision: true}
	g := grant(reg, r, n, now)
	g.Revoked = true
	if _, e := run.Complete(context.Background(), r, n, g); !errors.Is(e, ErrDenied) || f.calls != 0 {
		t.Fatal("authorization gate did not precede capability gate", e)
	}
	noAdapter, e := NewOfflineRunner(reg, nil)
	if e != nil {
		t.Fatal(e)
	}
	noAdapter.now = func() time.Time { return now }
	if _, e = noAdapter.Complete(context.Background(), r, Requirements{}, grant(reg, r, Requirements{}, now)); !errors.Is(e, ErrUnsupported) {
		t.Fatal("missing approved fake silently fell back", e)
	}
	if _, e = run.Complete(nil, r, Requirements{}, grant(reg, r, Requirements{}, now)); !errors.Is(e, ErrInvalid) {
		t.Fatal("nil context")
	}
}
func TestOfflineAdapterConstructorExactAndDormant(t *testing.T) {
	now := time.Now().UTC()
	rec := fixtureRecord(now)
	reg := registry(t, rec)
	for _, name := range []string{"nil", "typed_nil", "live", "wrong_wire", "wrong_version", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			f := fake(rec)
			adapters := []OfflineAdapter{f}
			switch name {
			case "nil":
				adapters[0] = nil
			case "typed_nil":
				adapters[0] = (*fakeAdapter)(nil)
			case "live":
				f.descriptor.Mode = "LIVE"
			case "wrong_wire":
				f.wire = "wrong.v1"
			case "wrong_version":
				f.descriptor.ModelVersion = "fixture-v0"
			case "duplicate":
				adapters = append(adapters, f)
			}
			if _, e := NewOfflineRunner(reg, adapters); e == nil || f.calls != 0 {
				t.Fatal("unapproved descriptor admitted", e)
			}
		})
	}
	if _, e := NewOfflineRunner(nil, nil); e == nil {
		t.Fatal("nil registry")
	}
	var empty *OfflineRunner
	if _, e := empty.Complete(context.Background(), fixtureRequest(now, modelgateway.Text), Requirements{}, OfflineGrant{}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("nil runner")
	}
}

type fakeGate bool

func (g fakeGate) InferenceEnabled(context.Context) bool { return bool(g) }
func TestActualServiceAlwaysDisabledUnavailable(t *testing.T) {
	now := time.Now().UTC()
	r := fixtureRequest(now, modelgateway.Text)
	for _, gate := range []modelgateway.LiveGate{nil, fakeGate(false), fakeGate(true)} {
		result, e := NewService(gate).Complete(context.Background(), r)
		if !errors.Is(e, modelgateway.ErrUnavailable) || result.Mode != modelgateway.Disabled || result.Status != modelgateway.Unavailable || result.Text != "" || result.ProviderID != "" {
			t.Fatal("flag or fixture opened actual inference", e)
		}
	}
	var s *Service
	if result, e := s.Complete(context.Background(), r); !errors.Is(e, modelgateway.ErrUnavailable) || result.Mode != modelgateway.Disabled {
		t.Fatal("nil actual service")
	}
}

func TestPublicOfflineClockConstruction(t *testing.T) {
	now := time.Now().UTC()
	rec := fixtureRecord(now)
	reg := registry(t, rec)
	r := fixtureRequest(now, modelgateway.Text)
	f := fake(rec)
	run, e := NewOfflineRunnerWithClock(reg, []OfflineAdapter{f}, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	out, e := run.Complete(context.Background(), r, Requirements{}, grant(reg, r, Requirements{}, now))
	if e != nil || out.Mode != modelgateway.OfflineContract || out.Text == "" || f.calls != 1 {
		t.Fatal("public exact-time synthetic path unavailable", e)
	}
	if _, e = NewOfflineRunnerWithClock(reg, []OfflineAdapter{f}, nil); !errors.Is(e, ErrInvalid) {
		t.Fatal("nil clock accepted")
	}
	run, e = NewOfflineRunnerWithClock(reg, []OfflineAdapter{f}, func() time.Time { return time.Time{} })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = run.Complete(context.Background(), r, Requirements{}, grant(reg, r, Requirements{}, now)); !errors.Is(e, ErrInvalid) || f.calls != 1 {
		t.Fatal("invalid clock dispatched", e)
	}
}
