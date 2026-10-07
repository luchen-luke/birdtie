package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBusinessAgentIdentityRegisteredRoute(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/v1/me/businesses/be000000-0000-4000-8000-000000000001/agent-identity", nil))
		if w.Code != 503 {
			t.Fatalf("actual route must exist and fail closed without native store: %s %d %s", method, w.Code, w.Body.String())
		}
	}
}

type bizIdentityHTTPAccess struct {
	identity.AccessStore
	digest [32]byte
	kind   string
}

func (a *bizIdentityHTTPAccess) AuthenticateOrganizationAgent(_ context.Context, d [32]byte) (identity.Actor, error) {
	if d != a.digest {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	return identity.Actor{ID: "be000000-0000-4000-8000-000000000002", AccountType: a.kind}, nil
}

type bizIdentityHTTPPort struct {
	foundation.PublicCatalog
	value         agentbusiness.BusinessIdentity
	reads, writes int
	late          bool
	cancel        context.CancelFunc
	fail          error
}

func (p *bizIdentityHTTPPort) ReadBusinessAgentIdentity(_ context.Context, a businessconsole.Access) (agentbusiness.BusinessIdentity, error) {
	p.reads++
	v := p.value
	if p.late && p.reads > 1 {
		v.SourceVersion = agentbusiness.Digest([]byte("changed"))
	}
	if p.cancel != nil && p.reads > 1 {
		p.cancel()
	}
	return v, p.fail
}
func (p *bizIdentityHTTPPort) EstablishBusinessAgentIdentity(_ context.Context, a businessconsole.Access, input agentbusiness.EstablishIdentity) (agentbusiness.BusinessIdentity, error) {
	p.writes++
	if input.ExpectedClaimVersion != p.value.ClaimVersion {
		return agentbusiness.BusinessIdentity{}, businessconsole.ErrConflict
	}
	return p.value, p.fail
}
func bizIdentityHTTPFixture(t *testing.T) (*bizIdentityHTTPPort, *bizIdentityHTTPAccess, http.Handler, string) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC()
	principal := actorref.PrincipalRef{Type: actorref.Business, ID: "be000000-0000-4000-8000-000000000003"}
	profile, _ := agentprofile.New("be000000-0000-4000-8000-000000000004", principal, at)
	p := &bizIdentityHTTPPort{value: agentbusiness.BusinessIdentity{Schema: agentbusiness.IdentitySchema, BusinessID: "be000000-0000-4000-8000-000000000001", BusinessName: "合成商家", Principal: principal, Role: "owner", ClaimStatus: "verified", ClaimState: "verified", ClaimVersion: 2, Agent: &agentbusiness.BusinessIdentityAgent{ID: profile.AgentID, Type: actorref.Business, Status: "suspended", Profile: profile}, ObservedAt: at, ValidUntil: at.Add(30 * time.Second), MetadataOnly: true, Tools: []string{}, SourceVersion: agentbusiness.Digest([]byte("current"))}}
	a := &bizIdentityHTTPAccess{digest: d, kind: "person"}
	return p, a, New(p, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil), token
}
func bizIdentityHTTPCall(h http.Handler, token, method, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/v1/me/businesses/be000000-0000-4000-8000-000000000001/agent-identity", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestBusinessAgentIdentityHTTPCurrentGetAndExplicitPost(t *testing.T) {
	p, _, h, token := bizIdentityHTTPFixture(t)
	w := bizIdentityHTTPCall(h, token, "GET", "", nil)
	if w.Code != 200 || p.reads != 2 || p.writes != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	p.reads = 0
	w = bizIdentityHTTPCall(h, token, "POST", `{"expectedClaimVersion":2}`, nil)
	if w.Code != 200 || p.writes != 1 || p.reads != 1 {
		t.Fatal(w.Code)
	}
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	data := m["data"].(map[string]any)
	if data["metadataOnly"] != true || data["runtimeAvailable"] != false {
		t.Fatal(data)
	}
}
func TestBusinessAgentIdentityHTTPDenialAndClosedBody(t *testing.T) {
	p, a, h, token := bizIdentityHTTPFixture(t)
	for _, tc := range []struct {
		method, body string
		edit         func(*http.Request)
		code         int
	}{{"GET", "", func(r *http.Request) { r.Header.Del("Authorization") }, 401}, {"GET", "", func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", "x") }, 400}, {"POST", `{"expectedClaimVersion":2,"enabled":true}`, nil, 400}, {"POST", `{"expectedClaimVersion":3}`, nil, 409}} {
		w := bizIdentityHTTPCall(h, token, tc.method, tc.body, tc.edit)
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	a.kind = "organization"
	w := bizIdentityHTTPCall(h, token, "GET", "", nil)
	if w.Code != 403 || p.reads != 0 {
		t.Fatal(w.Code)
	}
}
func TestBusinessAgentIdentityHTTPEncodedSourceChanged(t *testing.T) {
	p, _, h, token := bizIdentityHTTPFixture(t)
	p.late = true
	w := bizIdentityHTTPCall(h, token, "GET", "", nil)
	if w.Code != 409 || strings.Contains(w.Body.String(), "businessName") {
		t.Fatal(w.Code, w.Body.String())
	}
	p.late = false
	p.reads = 0
	p.fail = businessconsole.ErrForbidden
	w = bizIdentityHTTPCall(h, token, "GET", "", nil)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestBusinessAgentIdentityHTTPCancelAndTypedNil(t *testing.T) {
	p, _, h, token := bizIdentityHTTPFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	w := bizIdentityHTTPCall(h, token, "GET", "", func(r *http.Request) { *r = *r.WithContext(ctx) })
	if strings.Contains(w.Body.String(), "businessName") {
		t.Fatal("cancelled metadata emitted")
	}
	var port *bizIdentityHTTPPort
	h = New(port, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w = bizIdentityHTTPCall(h, token, "GET", "", nil)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
