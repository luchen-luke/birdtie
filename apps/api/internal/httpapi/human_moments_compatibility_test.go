package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// These are wiring spies, not native authorization/source proof. The real
// registered PostgreSQL tests exercise revocation, clock and rollback.
type humanMomentLegacySpy struct {
	content.MomentStore
	calls int
}

func (s *humanMomentLegacySpy) CreateMomentDraft(context.Context, string, content.MomentInput) (content.Moment, error) {
	s.calls++
	return content.Moment{}, nil
}
func (s *humanMomentLegacySpy) ListOwnMoments(context.Context, string) ([]content.Moment, error) {
	s.calls++
	return nil, nil
}
func (s *humanMomentLegacySpy) GetOwnMoment(context.Context, string, string) (content.Moment, error) {
	s.calls++
	return content.Moment{}, nil
}
func (s *humanMomentLegacySpy) UpdateMomentDraft(context.Context, string, string, int64, content.MomentInput) (content.Moment, error) {
	s.calls++
	return content.Moment{}, nil
}
func (s *humanMomentLegacySpy) WithdrawMoment(context.Context, string, string, int64) error {
	s.calls++
	return nil
}

type humanMomentAccessSpy struct {
	identity.AccessStore
	actor identity.Actor
	seen  [32]byte
}

func (s *humanMomentAccessSpy) Authenticate(_ context.Context, digest [32]byte) (identity.Actor, error) {
	s.seen = digest
	return s.actor, nil
}

type humanMomentNativeSpy struct {
	humanMomentLegacySpy
	seen  [32]byte
	actor identity.Actor
	calls int
}

func (s *humanMomentNativeSpy) capture(digest [32]byte, actor identity.Actor) {
	s.seen = digest
	s.actor = actor
	s.calls++
}
func (s *humanMomentNativeSpy) CreateHumanMomentDraft(_ context.Context, digest [32]byte, actor identity.Actor, _ content.MomentInput) (content.Moment, error) {
	s.capture(digest, actor)
	return content.Moment{}, content.ErrUnavailable
}
func (s *humanMomentNativeSpy) ListHumanMoments(_ context.Context, digest [32]byte, actor identity.Actor) ([]content.Moment, error) {
	s.capture(digest, actor)
	return nil, content.ErrUnavailable
}
func (s *humanMomentNativeSpy) GetHumanMoment(_ context.Context, digest [32]byte, actor identity.Actor, _ string) (content.Moment, error) {
	s.capture(digest, actor)
	return content.Moment{}, content.ErrUnavailable
}
func (s *humanMomentNativeSpy) UpdateHumanMomentDraft(_ context.Context, digest [32]byte, actor identity.Actor, _ string, _ int64, _ content.MomentInput) (content.Moment, error) {
	s.capture(digest, actor)
	return content.Moment{}, content.ErrUnavailable
}
func (s *humanMomentNativeSpy) WithdrawHumanMoment(_ context.Context, digest [32]byte, actor identity.Actor, _ string, _ int64) error {
	s.capture(digest, actor)
	return content.ErrUnavailable
}

func TestHumanMomentHTTPNoLegacyFallback(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	token, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.Actor{ID: id, AccountType: "person"}
	routes := []struct{ method, path, body string }{
		{"POST", "/v1/me/moments", historicalMomentBody("year", "2025-01-01T00:00:00Z", 0)},
		{"GET", "/v1/me/moments", ""}, {"GET", "/v1/me/moments/" + id, ""},
		{"PUT", "/v1/me/moments/" + id, historicalMomentBody("unknown", "", 1)},
		{"DELETE", "/v1/me/moments/" + id + "?revision=1", ""},
	}
	for _, route := range routes {
		t.Run(route.method+route.path, func(t *testing.T) {
			access := &humanMomentAccessSpy{actor: actor}
			legacy := &humanMomentLegacySpy{}
			call := func(store content.MomentStore) *httptest.ResponseRecorder {
				h := New(nil, access, nil, store, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				rw := httptest.NewRecorder()
				h.ServeHTTP(rw, req)
				return rw
			}
			if rw := call(legacy); rw.Code != http.StatusServiceUnavailable || legacy.calls != 0 {
				t.Fatal("owner-ID Store was an authorization fallback")
			}
			native := &humanMomentNativeSpy{}
			if rw := call(native); rw.Code != http.StatusServiceUnavailable || native.calls != 1 || native.humanMomentLegacySpy.calls != 0 || native.seen != access.seen || native.actor != actor {
				t.Fatal("registered route did not pass trusted Authenticate digest/Actor to current human gateway")
			}
		})
	}
}
func TestHumanMomentHTTPRejectsClientAuthoritySelectors(t *testing.T) {
	token, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: "11111111-1111-4111-8111-111111111111", AccountType: "person"}}
	native := &humanMomentNativeSpy{}
	h := New(nil, access, nil, native, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	for _, field := range []string{"authorAccountId", "ownerId", "verified", "sessionDigest", "initialActor"} {
		input := map[string]any{}
		_ = json.Unmarshal([]byte(historicalMomentBody("unknown", "", 0)), &input)
		input[field] = "client-assertion"
		body, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", "/v1/me/moments", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != 400 || native.calls != 0 {
			t.Fatalf("client authority field %s reached domain", field)
		}
	}
	for _, path := range []string{"/v1/me/moments?ownerId=other", "/v1/me/moments?", "/v1/me/moments?purpose=agent_analysis", "/v1/me/moments?revision=1&owner=%zz"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != 400 || native.calls != 0 {
			t.Fatal("query selector reached gateway")
		}
	}
	for _, query := range []string{"revision=1&owner=%zz", "revision=1;owner=other", "revision=1&revision=2", "revision=1&owner=other", "revision=%zz"} {
		req := httptest.NewRequest("DELETE", "/v1/me/moments/11111111-1111-4111-8111-111111111111?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != 400 || native.calls != 0 {
			t.Fatalf("invalid withdrawal query reached gateway: %s", query)
		}
	}
	req := httptest.NewRequest("POST", "/v1/me/moments", strings.NewReader(historicalMomentBody("unknown", "", 0)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Birdtie-Organization-Workspace", "11111111-1111-4111-8111-111111111111")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 403 || native.calls != 0 {
		t.Fatal("organization workspace reinterpreted personal draft")
	}
}
