package agentcurrentcontext

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

const fixtureOwner = "36000000-0000-4000-8000-000000000001"
const fixtureAgent = "36000000-0000-4000-8000-000000000002"
const fixtureContext = "36000000-0000-4000-8000-000000000003"
const fixtureTask = "36000000-0000-4000-8000-000000000004"

func fixtureRequest(now time.Time) Request {
	return Request{Access: agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: fixtureOwner}}, Agent: agentcognitive.AgentReference{AgentID: fixtureAgent, Principal: actorref.PrincipalRef{Type: actorref.Person, ID: fixtureOwner}, Role: agentruntime.PersonalAgent}, Selection: CurrentDeclaration, Query: "今晚有什么活动", DeadlineAt: now.Add(10 * time.Minute)}
}
func fixtureState(now time.Time) nativeState {
	v, _ := contextVersion("CITY_CATALOG", agentevent.UpdatedAtDigestVersion, now.Add(-time.Hour), []byte(`{"fixture":"OFFLINE_SHAPE_ONLY"}`))
	return nativeState{now: now, sessionLimit: now.Add(time.Hour), authority: "OFFLINE_SHAPE_ONLY", city: nativeCity{ID: "aberdeen-gb", Label: "合成城市", TimeZone: "Europe/London", ContextID: fixtureContext, SourceUpdatedAt: now.Add(-time.Hour)}, sources: []Source{{"CITY_CATALOG", "aberdeen-gb", v, now.Add(-time.Hour)}}}
}
func TestRequestClosedShapeAndLease(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name   string
		change func(*Request)
	}{
		{"no_session", func(r *Request) { r.Access.SessionDigest = [32]byte{} }}, {"other_owner", func(r *Request) { r.Agent.Principal.ID = fixtureTask }},
		{"organization", func(r *Request) {
			r.Access.WorkspacePrincipal.Type = actorref.Organization
			r.Agent.Principal.Type = actorref.Organization
			r.Agent.Role = agentruntime.OrganizationAgent
		}},
		{"business", func(r *Request) {
			r.Access.WorkspacePrincipal.Type = actorref.Business
			r.Agent.Principal.Type = actorref.Business
			r.Agent.Role = agentruntime.BusinessAgent
		}},
		{"wrong_role", func(r *Request) { r.Agent.Role = agentruntime.OrganizationAgent }}, {"model_as_agent", func(r *Request) { r.Agent.AgentID = "fake-model" }},
		{"zero_agent", func(r *Request) { r.Agent.AgentID = "00000000-0000-0000-0000-000000000000" }},
		{"unknown_selection", func(r *Request) { r.Selection = "VERIFIED_CITY" }}, {"decl_city_override", func(r *Request) { r.CityID = "other-city" }},
		{"decl_task_override", func(r *Request) { r.TaskID = fixtureTask }}, {"task_missing", func(r *Request) { r.Selection = CurrentTask; r.Query = "" }},
		{"task_fresh_text_override", func(r *Request) { r.Selection = CurrentTask; r.TaskID = fixtureTask }},
		{"task_city_override", func(r *Request) {
			r.Selection = CurrentTask
			r.TaskID = fixtureTask
			r.Query = ""
			r.CityID = "aberdeen-gb"
		}},
		{"request_missing_city", func(r *Request) { r.Selection = SelectedCity }}, {"request_missing_query", func(r *Request) { r.Selection = SelectedCity; r.CityID = "aberdeen-gb"; r.Query = "" }},
		{"query_long", func(r *Request) { r.Query = strings.Repeat("a", MaxQueryBytes+1) }}, {"query_nul", func(r *Request) { r.Query = "今晚\x00活动" }},
		{"query_newline", func(r *Request) { r.Query = "今晚\n活动" }}, {"query_space", func(r *Request) { r.Query = " 今晚活动" }},
		{"query_utf8", func(r *Request) { r.Query = string([]byte{255}) }}, {"expiry_zero", func(r *Request) { r.DeadlineAt = time.Time{} }},
		{"expiry_equal", func(r *Request) { r.DeadlineAt = now }}, {"expiry_past", func(r *Request) { r.DeadlineAt = now.Add(-time.Nanosecond) }},
		{"expiry_long", func(r *Request) { r.DeadlineAt = now.Add(MaxRequestDeadline + time.Nanosecond) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := fixtureRequest(now)
			tc.change(&r)
			if e := validateRequest(r, now); e == nil {
				t.Fatal("invalid/offline authority shape accepted")
			}
		})
	}
	for _, kind := range []Selection{CurrentDeclaration, CurrentTask, SelectedCity} {
		t.Run("valid_shape_"+string(kind), func(t *testing.T) {
			r := fixtureRequest(now)
			r.Selection = kind
			if kind == CurrentTask {
				r.TaskID = fixtureTask
				r.Query = ""
			}
			if kind == SelectedCity {
				r.CityID = "aberdeen-gb"
			}
			if e := validateRequest(r, now); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestTemporaryWindowActualTimeZoneMidnightAndDST(t *testing.T) {
	cases := []struct {
		name, now, zone, pref, end string
		hours                      float64
	}{
		{"london_tonight", "2026-10-02T21:00:00Z", "Europe/London", "tonight", "2026-10-02T23:00:00Z", 6},
		{"shanghai_midnight", "2026-10-02T15:59:59Z", "Asia/Shanghai", "tonight", "2026-10-02T16:00:00Z", 6},
		{"spring_day", "2026-03-29T10:00:00Z", "Europe/London", "today", "2026-03-29T23:00:00Z", 23},
		{"autumn_day", "2026-10-25T10:00:00Z", "Europe/London", "today", "2026-10-26T00:00:00Z", 25},
		{"tomorrow", "2026-10-02T10:00:00Z", "Europe/London", "tomorrow", "2026-10-03T23:00:00Z", 24},
		{"weekend_saturday", "2026-10-03T10:00:00Z", "Europe/London", "weekend", "2026-10-04T23:00:00Z", 48},
		{"weekend_sunday", "2026-10-04T10:00:00Z", "Europe/London", "weekend", "2026-10-04T23:00:00Z", 48},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.now)
			a, b, e := temporaryWindow(now, tc.zone, tc.pref, nil)
			if e != nil || a == nil || b == nil || b.Format(time.RFC3339) != tc.end || b.Sub(*a).Hours() != tc.hours {
				t.Fatal("actual calendar window", a, b, e)
			}
		})
	}
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	future := now.Add(time.Nanosecond)
	for _, tc := range []struct {
		name, zone, pref string
		anchor           *time.Time
	}{{"stale_task_tonight", "Europe/London", "tonight", &old}, {"bad_zone", "guess-city-timezone", "tonight", nil}, {"missing_zone", "", "today", nil}, {"server_local_zone", "Local", "tonight", nil}, {"unknown_time", "Europe/London", "every_evening", nil}, {"future_task", "Europe/London", "tonight", &future}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, e := temporaryWindow(now, tc.zone, tc.pref, tc.anchor); e == nil {
				t.Fatal("unknown/expired relative window")
			}
		})
	}
	if a, b, e := temporaryWindow(now, "Europe/London", "anytime", nil); e != nil || a != nil || b != nil {
		t.Fatal("anytime invented availability")
	}
}
func TestSnapshotOfflineShapeDistinguishesRequestTaskAndMemory(t *testing.T) {
	now := time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC)
	r := fixtureRequest(now)
	state := fixtureState(now)
	snapshot, e := buildSnapshot(r, state)
	if e != nil || snapshot.Scope != "CURRENT_CONTEXT" || snapshot.Purpose != "HUMAN_SELF_REVIEW" || snapshot.MemoryPromotionAllowed || snapshot.ModelAccess != "UNAVAILABLE" || snapshot.TimePreference != "tonight" || snapshot.QueryOrigin != "CURRENT_REQUEST" || snapshot.City.Context != (contextgraph.Ref{Type: contextgraph.City, ID: fixtureContext}) {
		t.Fatal("offline shape only", e)
	}
	if !snapshot.ExpiresAt.Equal(now.Add(MaxLease)) {
		t.Fatal("short lease")
	}
	for _, tc := range []struct {
		name     string
		change   func(*Request, *nativeState)
		expected time.Time
	}{{"request_deadline", func(r *Request, _ *nativeState) { r.DeadlineAt = now.Add(time.Minute) }, now.Add(time.Minute)}, {"session_limit", func(_ *Request, s *nativeState) { s.sessionLimit = now.Add(2 * time.Minute) }, now.Add(2 * time.Minute)}, {"city_source_limit", func(_ *Request, s *nativeState) { v := now.Add(3 * time.Minute); s.city.SourceExpiresAt = &v }, now.Add(3 * time.Minute)}} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixtureRequest(now)
			s := fixtureState(now)
			tc.change(&r, &s)
			got, e := buildSnapshot(r, s)
			if e != nil || !got.ExpiresAt.Equal(tc.expected) {
				t.Fatal("bounded lease", e)
			}
		})
	}
	nearMidnight := time.Date(2026, 10, 2, 22, 59, 30, 0, time.UTC)
	out, e := buildSnapshot(fixtureRequest(nearMidnight), fixtureState(nearMidnight))
	if e != nil || out.ExpiresAt.Sub(nearMidnight) != 30*time.Second {
		t.Fatal("tonight bounded local midnight", e)
	}
}
func TestActualUnavailablePortsAndJSONClaims(t *testing.T) {
	r := fixtureRequest(time.Now().UTC())
	if _, e := json.Marshal(r); !errors.Is(e, ErrServerOnly) {
		t.Fatal("server credential serialized")
	}
	if e := json.Unmarshal([]byte(`{"Verified":true}`), &r); !errors.Is(e, ErrServerOnly) || r.Access.SessionDigest != ([32]byte{}) {
		t.Fatal("JSON control forged")
	}
	snapshot := Snapshot{Purpose: "HUMAN_SELF_REVIEW"}
	if e := json.Unmarshal([]byte(`{"Scope":"CURRENT_CONTEXT","confirmed":true}`), &snapshot); !errors.Is(e, ErrServerOnly) || snapshot.Purpose != "" {
		t.Fatal("JSON snapshot authority")
	}
	cfg := agentfeature.DefaultConfig()
	ctrl, e := agentfeature.NewController(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = NewService(nil, ctrl, false); !errors.Is(e, ErrUnavailable) {
		t.Fatal("nil native database")
	}
	svc, e := NewService(&pgxpool.Pool{}, ctrl, false)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := svc.ReadOwn(context.Background(), fixtureRequest(time.Now().UTC())); !errors.Is(e, ErrUnavailable) || out.Query != "" {
		t.Fatal("default flag reached native/fake source")
	}
	if _, e = svc.ReadForCognition(context.Background(), agentcognitive.ReadRequest{}); !errors.Is(e, agentcognitive.ErrUnavailable) {
		t.Fatal("ordinary read became cognition")
	}
	var absent *Service
	if _, e = absent.ReadOwn(context.Background(), Request{}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("nil service")
	}
	if _, e = svc.ReadOwn(nil, Request{}); !errors.Is(e, ErrInvalid) {
		t.Fatal("nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = svc.ReadOwn(ctx, Request{}); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled request")
	}
}
func TestNativeVersionUsesRealTimestampAndMutationShape(t *testing.T) {
	now := time.Now().UTC()
	raw := []byte(`{"rowToken":"1","createdAt":"native"}`)
	a, e := contextVersion("CITY_DECLARATION", agentevent.CreatedAtDigestVersion, now, raw)
	if e != nil || a.Revision != 0 || len(a.Token) != 64 {
		t.Fatal("native version shape", e)
	}
	b, _ := contextVersion("CITY_DECLARATION", agentevent.CreatedAtDigestVersion, now, []byte(`{"rowToken":"2","createdAt":"native"}`))
	c, _ := contextVersion("CITY_DECLARATION", agentevent.CreatedAtDigestVersion, now.Add(time.Microsecond), raw)
	if a == b || a == c {
		t.Fatal("same ID changed native generation not represented")
	}
	for _, kind := range []string{"PROFILE_SOURCE", "VERIFIED_LOCATION", "MEMORY"} {
		if _, e = contextVersion(kind, agentevent.CreatedAtDigestVersion, now, raw); e == nil {
			t.Fatal("unsupported context source")
		}
	}
	if _, e = contextVersion("CITY_DECLARATION", agentevent.RevisionVersion, now, raw); e == nil {
		t.Fatal("invented source revision")
	}
}
