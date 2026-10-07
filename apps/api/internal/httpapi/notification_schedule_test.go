package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotificationScheduleUnitRegisteredRoute(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, "/v1/me/notification-schedule", nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("actual registered route status=%d want unavailable503, body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// Registered HTTP and current-response spies, not native session/DB evidence.
type scheduleHTTPUnitStore struct {
	policy ns.Policy
	calls  int
	access agentprofile.PrivateAccess
	input  ns.PutInput
	err    error
}

func (s *scheduleHTTPUnitStore) GetOwnNotificationSchedule(_ context.Context, a agentprofile.PrivateAccess) (ns.Policy, error) {
	s.calls++
	s.access = a
	return s.policy, s.err
}
func (s *scheduleHTTPUnitStore) PutOwnNotificationSchedule(_ context.Context, a agentprofile.PrivateAccess, in ns.PutInput) (ns.Policy, error) {
	s.calls++
	s.access = a
	s.input = in
	return s.policy, s.err
}

type scheduleHTTPUnitCatalog struct {
	foundation.PublicCatalog
	*scheduleHTTPUnitStore
}
type scheduleHTTPUnitAccess struct {
	*privateProfileHTTPAccess
	responseErr error
}

// Explicit current-Agent response spy; this is not native identity evidence.
func (a *scheduleHTTPUnitAccess) ResolveOwnContextAgent(_ context.Context, access agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	return agentcognitive.AgentReference{AgentID: privateProfileHTTPAgent, Principal: access.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, nil
}

func (a *scheduleHTTPUnitAccess) ValidateHumanSocialResponse(ctx context.Context, d [32]byte, actor identity.Actor) error {
	if a.responseErr != nil {
		return a.responseErr
	}
	return a.privateProfileHTTPAccess.ValidateHumanSocialResponse(ctx, d, actor)
}

func scheduleHTTPUnitFixture(t *testing.T) (http.Handler, *scheduleHTTPUnitAccess, *scheduleHTTPUnitStore, string, string) {
	t.Helper()
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	expires := now.Add(time.Hour)
	settings := ns.Settings{Enabled: true, TimeZone: "Asia/Shanghai", LocalMinute: 1080, GapPolicy: ns.GapSkip, FoldPolicy: ns.FoldEarlierOnce, MaxContactsPerDay: 3, Categories: []agentnotification.Category{agentnotification.CategoryActivity, agentnotification.CategorySocial}}
	policy := ns.Policy{SchemaVersion: ns.SchemaVersion, Version: 1, AgentID: privateProfileHTTPAgent, Configured: true, Status: "ACTIVE", BudgetWindowHours: 24, Settings: settings, ValidFrom: &now, UpdatedAt: &now, ExpiresAt: &expires}
	if ns.ValidatePolicy(policy) != nil {
		t.Fatal("invalid fixture")
	}
	store := &scheduleHTTPUnitStore{policy: policy}
	a := &scheduleHTTPUnitAccess{privateProfileHTTPAccess: &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: digest}}
	body, _ := json.Marshal(ns.PutInput{ExpectedVersion: 0, Settings: settings, ExpiresAt: expires})
	h := New(scheduleHTTPUnitCatalog{scheduleHTTPUnitStore: store}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	return h, a, store, token, string(body)
}
func scheduleHTTPUnitRequest(h http.Handler, method, token, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/v1/me/notification-schedule", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	if method == "PUT" {
		r.Header.Set("Content-Type", "application/json")
	}
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestNotificationScheduleUnitHTTPBoundary(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		t.Run(method, func(t *testing.T) {
			h, _, s, token, body := scheduleHTTPUnitFixture(t)
			if method == "GET" {
				body = ""
			}
			w := scheduleHTTPUnitRequest(h, method, token, body, nil)
			if w.Code != 200 || s.calls != 1 || s.access.WorkspacePrincipal.ID != privateProfileHTTPOwner || s.access.SessionDigest == ([32]byte{}) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String(), s.calls)
			}
			if method == "PUT" && s.input.ExpectedVersion != 0 {
				t.Fatal("exact CAS")
			}
			if strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), "session_id") {
				t.Fatal("secret leakage")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		edit   func(*http.Request)
		status int
	}{{"org", func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", "org") }, 403}, {"query", func(r *http.Request) { r.URL.RawQuery = "ownerId=peer" }, 400}, {"forcequery", func(r *http.Request) { r.URL.ForceQuery = true }, 400}, {"anonymous", func(r *http.Request) { r.Header.Del("Authorization") }, 401}, {"bad_json_type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 400}, {"double_type", func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }, 400}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, s, token, body := scheduleHTTPUnitFixture(t)
			w := scheduleHTTPUnitRequest(h, "PUT", token, body, tc.edit)
			if w.Code != tc.status || s.calls != 0 {
				t.Fatal(w.Code, w.Body.String(), s.calls)
			}
		})
	}
	for _, e := range []error{ns.ErrInvalid, ns.ErrDenied, ns.ErrChanged, ns.ErrUnavailable, errors.New("PRIVATE_CANARY")} {
		h, _, s, token, body := scheduleHTTPUnitFixture(t)
		s.err = e
		w := scheduleHTTPUnitRequest(h, "PUT", token, body, nil)
		want := 503
		switch e {
		case ns.ErrInvalid:
			want = 400
		case ns.ErrDenied:
			want = 403
		case ns.ErrChanged:
			want = 409
		}
		if w.Code != want || strings.Contains(w.Body.String(), "CANARY") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	h, a, s, token, _ := scheduleHTTPUnitFixture(t)
	a.err = identity.ErrUnauthorized
	w := scheduleHTTPUnitRequest(h, "GET", token, "", nil)
	if w.Code != 401 || s.calls != 0 {
		t.Fatal("expired session must not read store")
	}
}
func TestNotificationScheduleUnitHTTPMalformedAndLate(t *testing.T) {
	for _, tc := range []struct {
		name string
		body func(string) string
	}{{"confirmed", func(s string) string {
		return strings.Replace(s, `"enabled":true`, `"confirmed":true,"enabled":true`, 1)
	}}, {"peer", func(s string) string {
		return strings.Replace(s, `"enabled":true`, `"ownerId":"peer","enabled":true`, 1)
	}}, {"duplicate", func(s string) string {
		return strings.Replace(s, `"enabled":true`, `"enabled":false,"enabled":true`, 1)
	}}, {"null", func(string) string { return "null" }}, {"large", func(string) string { return strings.Repeat("x", ns.MaxBodyBytes+1) }}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, s, token, b := scheduleHTTPUnitFixture(t)
			w := scheduleHTTPUnitRequest(h, "PUT", token, tc.body(b), nil)
			if w.Code != 400 || s.calls != 0 {
				t.Fatal(w.Code, w.Body.String(), s.calls)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*scheduleHTTPUnitStore)
	}{{"wrong_version", func(s *scheduleHTTPUnitStore) { s.policy.Version = 2 }}, {"wrong_settings", func(s *scheduleHTTPUnitStore) { s.policy.LocalMinute++ }}, {"unknown_model_window", func(s *scheduleHTTPUnitStore) { s.policy.BudgetWindowHours = 0 }}, {"wrong_expiry", func(s *scheduleHTTPUnitStore) { n := s.policy.ExpiresAt.Add(time.Microsecond); s.policy.ExpiresAt = &n }}} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, s, token, b := scheduleHTTPUnitFixture(t)
			tc.edit(s)
			w := scheduleHTTPUnitRequest(h, "PUT", token, b, nil)
			if w.Code != 503 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	// Existing current-session spy can deny at response serialization boundary.
	h, a, _, token, _ := scheduleHTTPUnitFixture(t)
	a.responseErr = identity.ErrUnauthorized
	w := scheduleHTTPUnitRequest(h, "GET", token, "", nil)
	if w.Code != 401 || bytes.Contains(w.Body.Bytes(), []byte(`"data"`)) {
		t.Fatal("late revoked response", w.Code, w.Body.String())
	}
}
