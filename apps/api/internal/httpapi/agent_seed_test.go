package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Wiring spies establish no database authority. Native tests cover that boundary.
type seedHTTPSpy struct {
	foundation.PublicCatalog
	calls  int
	digest [32]byte
	actor  identity.Actor
}

func (s *seedHTTPSpy) ReadOwnAgentSeed(_ context.Context, d [32]byte, a identity.Actor) (agentseed.Record, error) {
	s.calls++
	s.digest = d
	s.actor = a
	return agentseed.Record{}, agentseed.ErrUnavailable
}
func (s *seedHTTPSpy) SaveOwnAgentSeed(_ context.Context, d [32]byte, a identity.Actor, _ agentseed.Input) (agentseed.Record, error) {
	s.calls++
	s.digest = d
	s.actor = a
	return agentseed.Record{}, agentseed.ErrUnavailable
}
func TestAgentSeedHTTPStrictWireAndTrustedDigest(t *testing.T) {
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	access := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: d}
	spy := &seedHTTPSpy{}
	handler := New(spy, access, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	call := func(method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if call("GET", "/v1/me/agent-seed", "") != 503 || spy.calls != 1 || spy.digest != d || spy.actor != access.actor {
		t.Fatal("not actual trusted binding")
	}
	spy.calls = 0
	valid := `{"expectedSnapshot":"` + strings.Repeat("a", 64) + `","action":"DEFER"}`
	for _, key := range []string{"ownerId", "agentId", "confirmed", "verified", "purpose", "ExpectedSnapshot"} {
		t.Run(key, func(t *testing.T) {
			bad := strings.TrimSuffix(valid, "}") + `,"` + key + `":true}`
			if call("PUT", "/v1/me/agent-seed", bad) != 400 || spy.calls != 0 {
				t.Fatal("accepted authority wire")
			}
		})
	}
	for _, body := range []string{`null`, `{}`, strings.TrimSuffix(valid, "}") + `,"action":"SAVE"}`, strings.Replace(valid, `"DEFER"`, `null`, 1)} {
		if call("PUT", "/v1/me/agent-seed", body) != 400 || spy.calls != 0 {
			t.Fatal("invalid wire")
		}
	}
	for _, path := range []string{"/v1/me/agent-seed?owner=%zz", "/v1/me/agent-seed?"} {
		if call("GET", path, "") != 400 || spy.calls != 0 {
			t.Fatal("query")
		}
	}
	r := httptest.NewRequest(http.MethodPut, "/v1/me/agent-seed", strings.NewReader(valid))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Birdtie-Organization-Workspace", "organization-actor-id")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 || spy.calls != 0 {
		t.Fatal("organization scope")
	}
	if call("PUT", "/v1/me/agent-seed", valid) != 503 || spy.calls != 1 {
		t.Fatal("direct callable method unavailable")
	}
	// An unavailable native capability cannot invoke a legacy owner-ID writer.
	handler = New(nil, access, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	if call("GET", "/v1/me/agent-seed", "") != 503 {
		t.Fatal("missing native fallback")
	}
}
