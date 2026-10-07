package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Actual registered HTTP and original Service calls, synthetic Store contract.
// These fixture revisions/xmin/authority are NOT native PostgreSQL evidence.
type humanSelfReviewHTTPSpy struct {
	foundation.PublicCatalog
	access          agentprofile.PrivateAccess
	calls, resolves int
	requests        []acb.Request
	mutate          func(int, *acb.BuiltContext)
	lateErr         error
	sourceTime      time.Time
	contractErr     error
	contractDetail  string
}

func (s *humanSelfReviewHTTPSpy) ResolveOwnContextAgent(_ context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	s.resolves++
	s.access = a
	return agentcognitive.AgentReference{AgentID: httpActionSafetyOrgID, Principal: a.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, nil
}
func (s *humanSelfReviewHTTPSpy) BuildOwnAgentContext(ctx context.Context, r acb.Request) (acb.BuiltContext, error) {
	s.calls++
	s.requests = append(s.requests, r)
	if s.calls > 1 && s.lateErr != nil {
		return acb.BuiltContext{}, s.lateErr
	}
	// The actual native clock and persisted settings contract use microseconds.
	// Untruncated fixture nanoseconds are not a valid policy observation clock.
	now := time.Now().UTC().Truncate(time.Microsecond)
	sourceTime := s.sourceTime
	b := acb.Bundle{SchemaVersion: acb.SchemaVersion, Agent: r.Agent, Mode: r.Mode, RequestID: r.RequestID, ObservedAt: now, ExpiresAt: r.DeadlineAt, ModelAccess: "UNAVAILABLE",
		Sections: acb.Sections{Profile: "NOT_REQUESTED", Memories: "NOT_REQUESTED", Policies: "NOT_REQUESTED", Activities: "NOT_REQUESTED", Places: "NOT_REQUESTED", Relationships: "UNAVAILABLE"}}
	if len(r.ProfileFields) > 0 {
		b.Sections.Profile = "AVAILABLE"
		b.Profile = map[string]json.RawMessage{}
		for _, key := range r.ProfileFields {
			b.Profile[key] = json.RawMessage(`"本人可见声明"`)
			if key == "preferredActivityTypes" {
				b.Profile[key] = json.RawMessage(`["badminton"]`)
			}
		}
		b.Sources = append(b.Sources, acb.Source{Kind: "HUMAN_PRIVATE_PROFILE", ID: r.Agent.AgentID, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 4}, NativeTime: sourceTime, RowToken: "INTERNAL_XMIN_CANARY"})
	}
	for _, id := range r.MemoryIDs {
		b.Sections.Memories = "AVAILABLE"
		from, created := sourceTime.Add(-time.Hour), sourceTime.Add(-24*time.Hour)
		b.Memories = append(b.Memories, acb.ReviewMemory{ID: id, Version: 2, MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:badminton", Summary: "我不偏好羽毛球活动", StructuredValue: json.RawMessage(`{"activityCategory":"badminton","nature":"human-correction","HIDDEN_STRUCTURED_CANARY":true}`), ValidUntil: now.Add(time.Hour), ValidFrom: &from, CreatedAt: &created})
		b.Sources = append(b.Sources, acb.Source{Kind: "HUMAN_EXPLICIT_MEMORY", ID: id, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 2}, NativeTime: sourceTime, RowToken: "INTERNAL_MEMORY_XMIN_CANARY"})
	}
	for _, f := range r.PolicyFamilies {
		b.Sections.Policies = "UNCONFIGURED"
		b.Policies = append(b.Policies, agentpolicysettings.DefaultRecord(f))
	}
	fields, e := acb.BuildFieldEvidenceSet(b)
	if e != nil {
		return acb.BuiltContext{}, e
	}
	b.FieldEvidenceSet = fields
	out := acb.BuiltContext{Bundle: b, Authority: strings.Repeat("a", 64)}
	if s.mutate != nil {
		s.mutate(s.calls, &out)
	}
	s.contractErr = acb.ValidateBuilt(r, out)
	if s.contractErr != nil {
		policyErr := error(nil)
		if len(out.Bundle.Policies) > 0 {
			policyErr = agentpolicysettings.ValidateRecord(out.Bundle.Policies[0], now)
		}
		s.contractDetail = fmt.Sprintf("fields=%v policy=%v sections=%+v memories=%d sources=%d selectedMemories=%d selectedPolicy=%v", acb.ValidateExactFieldEvidence(out.Bundle), policyErr, out.Bundle.Sections, len(out.Bundle.Memories), len(out.Bundle.Sources), len(r.MemoryIDs), r.PolicyFamilies)
	}
	if ctx.Err() != nil {
		return acb.BuiltContext{}, acb.ErrUnavailable
	}
	return out, nil
}
func humanSelfReviewHTTPFixture(t *testing.T) (*humanSelfReviewHTTPSpy, http.Handler, string) {
	t.Helper()
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	spy := &humanSelfReviewHTTPSpy{sourceTime: time.Now().UTC().Add(-time.Hour).Truncate(time.Second)}
	return spy, New(spy, httpActionSafetyAccess{digest: digest}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil), token
}

const humanSelfReviewHTTPPath = "/v1/me/agent-context/self-review"
const humanSelfReviewHTTPBody = `{"profileFields":["preferredActivityTypes"],"memoryIds":["40000000-0000-4000-8000-000000000007"],"policyFamilies":[]}`

func humanSelfReviewHTTPCall(h http.Handler, token, body string, edit func(*http.Request)) (int, []byte) {
	r := httptest.NewRequest(http.MethodPost, humanSelfReviewHTTPPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func TestHumanSelfReviewHTTPRegisteredRouteProvidesBothSources(t *testing.T) {
	spy, h, token := humanSelfReviewHTTPFixture(t)
	code, raw := humanSelfReviewHTTPCall(h, token, humanSelfReviewHTTPBody, nil)
	t.Logf("SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_WIRE %s", raw)
	if code != 200 {
		t.Fatalf("actual registered human self-review absent: status=%d wire=%s", code, raw)
	}
	if spy.calls != 2 || spy.resolves != 1 {
		t.Fatalf("same Service snapshot/final revalidation missing: build=%d resolve=%d", spy.calls, spy.resolves)
	}
	if !strings.Contains(string(raw), "AWAITING_CONFIRMATION") || !strings.Contains(string(raw), "我不偏好羽毛球活动") {
		t.Fatal("both actual selected declarations/conflict missing")
	}
	for _, v := range []string{"INTERNAL_XMIN_CANARY", "INTERNAL_MEMORY_XMIN_CANARY", "HIDDEN_STRUCTURED_CANARY", "structuredValue", "memoryKey", "authority", "seal", "SessionDigest"} {
		if strings.Contains(string(raw), v) {
			t.Fatal("internal/private hidden metadata leaked", v)
		}
	}
	for _, r := range spy.requests {
		if r.Mode != acb.HumanSelfReview || r.Selection != acb.ExactSelfReview || r.TaskID != "" || r.PurposeGrantID != "" || r.Access != spy.access {
			t.Fatal("human review inherited another purpose or identity")
		}
	}
}
func TestHumanSelfReviewHTTPClosedThreeSelectors(t *testing.T) {
	bad := map[string]string{
		"duplicate":       `{"profileFields":[],"profileFields":[],"memoryIds":[],"policyFamilies":[]}`,
		"owner":           `{"profileFields":[],"memoryIds":[],"policyFamilies":[],"ownerId":"someone"}`,
		"taskGrant":       `{"profileFields":[],"memoryIds":[],"policyFamilies":[],"grantId":"approved"}`,
		"confirmed":       `{"profileFields":[],"memoryIds":[],"policyFamilies":[],"confirmed":true}`,
		"deadline":        `{"profileFields":[],"memoryIds":[],"policyFamilies":[],"deadlineAt":"2099-01-01T00:00:00Z"}`,
		"null":            `{"profileFields":null,"memoryIds":[],"policyFamilies":[]}`,
		"notArray":        `{"profileFields":"preferredActivityTypes","memoryIds":[],"policyFamilies":[]}`,
		"missing":         `{"profileFields":[],"memoryIds":[]}`,
		"case":            `{"ProfileFields":[],"memoryIds":[],"policyFamilies":[]}`,
		"emptySelection":  `{"profileFields":[],"memoryIds":[],"policyFamilies":[]}`,
		"fourFields":      `{"profileFields":["availability","agentNotes","personalPreferences","travelPreferences"],"memoryIds":[],"policyFamilies":[]}`,
		"duplicateField":  `{"profileFields":["availability","availability"],"memoryIds":[],"policyFamilies":[]}`,
		"wrongField":      `{"profileFields":["auth.role"],"memoryIds":[],"policyFamilies":[]}`,
		"fourMemories":    `{"profileFields":[],"memoryIds":["40000000-0000-4000-8000-000000000007","40000000-0000-4000-8000-000000000008","40000000-0000-4000-8000-000000000009","40000000-0000-4000-8000-000000000010"],"policyFamilies":[]}`,
		"duplicateMemory": `{"profileFields":[],"memoryIds":["40000000-0000-4000-8000-000000000007","40000000-0000-4000-8000-000000000007"],"policyFamilies":[]}`,
		"badMemory":       `{"profileFields":[],"memoryIds":["anyone"],"policyFamilies":[]}`,
		"twoPolicies":     `{"profileFields":[],"memoryIds":[],"policyFamilies":["AUTONOMY","SOCIAL"]}`,
		"unknownPolicy":   `{"profileFields":[],"memoryIds":[],"policyFamilies":["MODEL_EGRESS"]}`,
		"oversize":        strings.Repeat("x", 8193), "trailing": humanSelfReviewHTTPBody + `{}`,
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			spy, h, token := humanSelfReviewHTTPFixture(t)
			code, raw := humanSelfReviewHTTPCall(h, token, body, nil)
			if code != 400 || spy.calls != 0 || strings.Contains(string(raw), `"data"`) {
				t.Fatalf("invalid selectors read private content %d %s calls=%d", code, raw, spy.calls)
			}
		})
	}
}
func TestHumanSelfReviewHTTPCurrentIdentityAndRequestFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		edit   func(*http.Request)
	}{
		{"anonymous", 401, func(r *http.Request) { r.Header.Del("Authorization") }},
		{"badBearer", 401, func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }},
		{"duplicateAuthorization", 401, func(r *http.Request) { r.Header.Add("Authorization", "Bearer wrong") }},
		{"organization", 403, func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", httpActionSafetyOrgID) }},
		{"emptyOrganization", 403, func(r *http.Request) { r.Header["X-Birdtie-Organization-Workspace"] = []string{""} }},
		{"query", 400, func(r *http.Request) { r.URL.RawQuery = "owner=other" }},
		{"forceQuery", 400, func(r *http.Request) { r.URL.ForceQuery = true }},
		{"mime", 415, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"cancelled", 503, func(r *http.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			*r = *r.WithContext(ctx)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy, h, token := humanSelfReviewHTTPFixture(t)
			code, raw := humanSelfReviewHTTPCall(h, token, humanSelfReviewHTTPBody, tc.edit)
			if code != tc.status || spy.calls != 0 || spy.resolves != 0 || strings.Contains(string(raw), `"data"`) {
				t.Fatalf("request bypassed closed identity %d %s", code, raw)
			}
		})
	}
}
func TestHumanSelfReviewHTTPFinalRefusalNeverExposesOldSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"denied", acb.ErrDenied, 403}, {"expired", acb.ErrExpired, 409}, {"unauthorized", identity.ErrUnauthorized, 401}, {"unavailable", acb.ErrUnavailable, 503}, {"rawError", errors.New("RAW_PRIVATE_ERROR_CANARY"), 503}} {
		t.Run(tc.name, func(t *testing.T) {
			spy, h, token := humanSelfReviewHTTPFixture(t)
			spy.lateErr = tc.err
			code, raw := humanSelfReviewHTTPCall(h, token, humanSelfReviewHTTPBody, nil)
			t.Logf("SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_FINAL_%s_WIRE %s", tc.name, raw)
			if code != tc.status || spy.calls != 2 || strings.Contains(string(raw), `"data"`) || strings.Contains(string(raw), "我不偏好") || strings.Contains(string(raw), "RAW_PRIVATE_ERROR_CANARY") {
				t.Fatalf("late refusal leaked old private content %d %s", code, raw)
			}
		})
	}
	for _, name := range []string{"sourceRevision", "sourceXminABA", "authority", "owner", "expiredFinalClock", "cancelAfterFinalRead", "claimsPolluted"} {
		t.Run(name, func(t *testing.T) {
			spy, h, token := humanSelfReviewHTTPFixture(t)
			spy.mutate = func(n int, b *acb.BuiltContext) {
				if n != 2 {
					return
				}
				switch name {
				case "sourceRevision":
					b.Bundle.Sources[0].Version.Revision++
				case "sourceXminABA":
					b.Bundle.Sources[0].RowToken = "RESTORED_ROW_NEW_XMIN"
				case "authority":
					b.Authority = strings.Repeat("b", 64)
				case "owner":
					b.Bundle.Agent.Principal.ID = httpActionSafetyOtherAcct
				case "expiredFinalClock":
					b.Bundle.ObservedAt = b.Bundle.ExpiresAt
				case "claimsPolluted":
					b.Bundle.FieldEvidenceSet.GrantsAuthority = true
				}
			}
			var cancel context.CancelFunc
			if name == "cancelAfterFinalRead" {
				spy.mutate = func(n int, b *acb.BuiltContext) {
					if n == 2 {
						cancel()
					}
				}
			}
			code, raw := humanSelfReviewHTTPCall(h, token, humanSelfReviewHTTPBody, func(r *http.Request) {
				var ctx context.Context
				ctx, cancel = context.WithCancel(r.Context())
				*r = *r.WithContext(ctx)
			})
			defer cancel()
			if code == 200 || spy.calls != 2 || strings.Contains(string(raw), `"data"`) || strings.Contains(string(raw), "我不偏好") {
				t.Fatalf("changed current source accepted: %d %s", code, raw)
			}
		})
	}
}

type humanSelfReviewTypedNilAccess struct{ identity.AccessStore }

func (*humanSelfReviewTypedNilAccess) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	panic("typed nil auth invoked")
}
func TestHumanSelfReviewHTTPMissingCapabilitiesFailClosed(t *testing.T) {
	for _, name := range []string{"missingStore", "typedNilStore", "missingAuth", "typedNilAuth"} {
		t.Run(name, func(t *testing.T) {
			spy, _, token := humanSelfReviewHTTPFixture(t)
			var catalog foundation.PublicCatalog = spy
			var auth identity.AccessStore = httpActionSafetyAccess{}
			switch name {
			case "missingStore":
				catalog = nil
			case "typedNilStore":
				catalog = (*humanSelfReviewHTTPSpy)(nil)
			case "missingAuth":
				auth = nil
			case "typedNilAuth":
				auth = (*humanSelfReviewTypedNilAccess)(nil)
			}
			h := New(catalog, auth, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			code, raw := humanSelfReviewHTTPCall(h, token, humanSelfReviewHTTPBody, nil)
			if code != 503 || strings.Contains(string(raw), `"data"`) {
				t.Fatalf("unavailable capability borrowed authority %d %s", code, raw)
			}
		})
	}
}
func TestHumanSelfReviewHTTPUnconfiguredAndLegacyMetadata(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "originalFields", true: "legacyFieldsNil"}[legacy], func(t *testing.T) {
			spy, h, token := humanSelfReviewHTTPFixture(t)
			if legacy {
				spy.mutate = func(_ int, b *acb.BuiltContext) { b.Bundle.FieldEvidenceSet = nil }
			}
			body := `{"profileFields":[],"memoryIds":["40000000-0000-4000-8000-000000000007"],"policyFamilies":["AUTONOMY"]}`
			code, raw := humanSelfReviewHTTPCall(h, token, body, nil)
			t.Logf("SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_UNCONFIGURED_%t_WIRE %s", legacy, raw)
			if code != 200 || spy.calls != 2 || !strings.Contains(string(raw), `"policies":"UNCONFIGURED"`) || !strings.Contains(string(raw), `"fieldEvidenceSet"`) {
				t.Fatalf("unconfigured became unavailable/default grant %d %s calls=%d native-contract=%v %s", code, raw, spy.calls, spy.contractErr, spy.contractDetail)
			}
		})
	}
}

func TestHumanSelfReviewHTTPConfiguredPolicyAndNativeClippedLease(t *testing.T) {
	spy, h, token := humanSelfReviewHTTPFixture(t)
	expires := time.Now().UTC().Add(15 * time.Second).Truncate(time.Microsecond)
	spy.mutate = func(_ int, out *acb.BuiltContext) {
		b := &out.Bundle
		b.ExpiresAt = expires
		b.Sections.Policies = "AVAILABLE"
		from, until := spy.sourceTime, spy.sourceTime.Add(2*time.Hour)
		p := agentpolicysettings.DefaultRecord(agentpolicysettings.Autonomy)
		p.Configured, p.NativeRevision, p.Status = true, 7, "ACTIVE"
		p.ValidFrom, p.UpdatedAt, p.ExpiresAt = &from, &from, &until
		b.Policies = []agentpolicysettings.Record{p}
		b.Sources = append(b.Sources, acb.Source{Kind: "HUMAN_POLICY_SETTINGS", ID: b.Agent.AgentID + ":AUTONOMY", Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 7}, NativeTime: from, RowToken: "POLICY_XMIN_CANARY"})
		var e error
		b.FieldEvidenceSet, e = acb.BuildFieldEvidenceSet(*b)
		if e != nil {
			t.Fatal(e)
		}
	}
	body := `{"profileFields":[],"memoryIds":["40000000-0000-4000-8000-000000000007"],"policyFamilies":["AUTONOMY"]}`
	code, raw := humanSelfReviewHTTPCall(h, token, body, nil)
	t.Logf("SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_CONFIGURED_WIRE %s", raw)
	if code != 200 || spy.calls != 2 || len(spy.requests) != 2 || !spy.requests[1].DeadlineAt.Equal(expires) || !spy.requests[0].DeadlineAt.After(expires) {
		t.Fatalf("original clipped lease renewed or configured snapshot lost: %d calls=%d %s", code, spy.calls, raw)
	}
	var wire struct {
		Data acb.HumanSelfReviewProjection `json:"data"`
	}
	if json.Unmarshal(raw, &wire) != nil || !wire.Data.ExpiresAt.Equal(expires) || !wire.Data.FieldEvidenceSet.ExpiresAt.Equal(expires) || len(wire.Data.Policies) != 1 || wire.Data.Policies[0].NativeRevision != 7 || wire.Data.GrantsAuthority || strings.Contains(string(raw), "POLICY_XMIN_CANARY") {
		t.Fatal("policy revision/interval or read lease/authority changed")
	}
	found := false
	for _, c := range wire.Data.FieldEvidenceSet.Claims {
		if c.ItemKind == "policies" {
			found = c.Use == "DECLARED_SETTINGS_NOT_AN_ACTION_GRANT" && c.Source.Version.Revision == 7 && c.CapturedAt == nil
		}
	}
	if !found {
		t.Fatal("same selected configured policy source absent")
	}
}

func TestHumanSelfReviewHTTPOversizeSnapshotDoesNotStreamPrivateBytes(t *testing.T) {
	spy, h, token := humanSelfReviewHTTPFixture(t)
	spy.mutate = func(_ int, b *acb.BuiltContext) {
		b.Bundle.Profile["preferredActivityTypes"] = json.RawMessage(`"` + strings.Repeat("PRIVATE_OVERSIZE_CANARY", 4000) + `"`)
		var e error
		b.Bundle.FieldEvidenceSet, e = acb.BuildFieldEvidenceSet(b.Bundle)
		if e != nil {
			t.Fatal(e)
		}
	}
	code, raw := humanSelfReviewHTTPCall(h, token, humanSelfReviewHTTPBody, nil)
	if code != 503 || spy.calls != 1 || strings.Contains(string(raw), `"data"`) || strings.Contains(string(raw), "PRIVATE_OVERSIZE_CANARY") {
		t.Fatalf("oversized private response streamed %d %s", code, raw)
	}
}
