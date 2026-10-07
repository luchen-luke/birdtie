package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// This exercises the actual registration. No DB or authenticated capability
// is supplied: an available route must fail closed, not silently be absent.
func TestEnrichmentInventoryRegisteredRouteExists(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/me/agent-enrichment-purpose/grants", nil))
	if w.Code != 503 {
		t.Fatalf("registered privacy inventory missing or not fail-closed: status=%d body=%s", w.Code, w.Body.String())
	}
}

type enrichmentInventoryHTTPPort struct {
	foundation.PublicCatalog
	aep.Store
	value          aep.Inventory
	err            error
	calls, deletes int
	access         agentprofile.PrivateAccess
	revision       int64
	resolveCalls   int
	resolveError   error
	resolvedAgent  *agentcognitive.AgentReference
}

func (p *enrichmentInventoryHTTPPort) ResolveOwnContextAgent(_ context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	p.resolveCalls++
	p.access = a
	if p.resolveError != nil {
		return agentcognitive.AgentReference{}, p.resolveError
	}
	if p.resolvedAgent != nil {
		return *p.resolvedAgent, nil
	}
	return agentcognitive.AgentReference{AgentID: p.value.AgentID, Principal: p.value.Owner, Role: agentruntime.PersonalAgent}, nil
}

func (p *enrichmentInventoryHTTPPort) ListOwnEnrichmentPurposes(_ context.Context, a agentprofile.PrivateAccess) (aep.Inventory, error) {
	p.calls++
	p.access = a
	return p.value, p.err
}
func (p *enrichmentInventoryHTTPPort) ReadOwnEnrichmentPurpose(_ context.Context, a agentprofile.PrivateAccess, id string) (aep.Grant, error) {
	p.access = a
	return p.value.Grants[0], nil
}
func (p *enrichmentInventoryHTTPPort) RevokeOwnEnrichmentPurpose(_ context.Context, a agentprofile.PrivateAccess, id string, r int64) (aep.Grant, error) {
	p.access = a
	p.revision = r
	p.deletes++
	g := p.value.Grants[0]
	g.Revision++
	at := g.ObservedAt
	g.RevokedAt = &at
	return g, nil
}

type enrichmentInventoryHTTPAccess struct {
	identity.AccessStore
	digest     [32]byte
	actor      identity.Actor
	err, late  error
	finalCalls int
	cancel     context.CancelFunc
}

func (a *enrichmentInventoryHTTPAccess) Authenticate(_ context.Context, d [32]byte) (identity.Actor, error) {
	if d != a.digest {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	return a.actor, a.err
}
func (a *enrichmentInventoryHTTPAccess) AuthenticateHumanSocial(ctx context.Context, d [32]byte) (identity.Actor, error) {
	return a.Authenticate(ctx, d)
}
func (a *enrichmentInventoryHTTPAccess) ValidateHumanSocialResponse(_ context.Context, d [32]byte, p identity.Actor) error {
	a.finalCalls++
	if d != a.digest || p != a.actor {
		return identity.ErrUnauthorized
	}
	if a.cancel != nil {
		a.cancel()
	}
	return a.late
}
func inventoryHTTPFixture(t *testing.T) (*enrichmentInventoryHTTPPort, *enrichmentInventoryHTTPAccess, http.Handler, string) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}
	g := aep.Grant{SchemaVersion: aep.Schema, ID: privateProfileHTTPForeign, PreviewID: "49000000-0000-4000-8000-000000000003", Purpose: aep.Purpose, Owner: owner, AgentID: privateProfileHTTPAgent, Revision: 1, CreatedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Minute), ObservedAt: at, Selection: aep.Selection{TaskID: "49000000-0000-4000-8000-000000000005", MomentID: "49000000-0000-4000-8000-000000000006", MomentRevision: 2, Fields: []string{"title"}, DeadlineAt: at.Add(time.Minute)}}
	p := &enrichmentInventoryHTTPPort{value: aep.Inventory{SchemaVersion: aep.InventorySchema, Owner: owner, AgentID: g.AgentID, ObservedAt: at, ValidUntil: at.Add(30 * time.Second), Limit: 50, Grants: []aep.Grant{g}}}
	a := &enrichmentInventoryHTTPAccess{digest: d, actor: identity.Actor{ID: owner.ID, AccountType: "person"}}
	return p, a, New(p, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil), token
}
func inventoryHTTPCall(h http.Handler, token string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/v1/me/agent-enrichment-purpose/grants", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestEnrichmentInventoryRegisteredCurrentMetadataAndOriginalRevoke(t *testing.T) {
	p, a, h, token := inventoryHTTPFixture(t)
	w := inventoryHTTPCall(h, token, nil)
	if w.Code != 200 || p.access.SessionDigest != a.digest || p.access.WorkspacePrincipal.ID != a.actor.ID || a.finalCalls != 1 || p.resolveCalls != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	t.Logf("SYNTHETIC_REGISTERED_INVENTORY_WIRE %s", w.Body.Bytes())
	var v struct {
		Data aep.Inventory `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || aep.ValidateInventory(v.Data) != nil {
		t.Fatal("wire not real Inventory contract")
	}
	g := v.Data.Grants[0]
	r := httptest.NewRequest("DELETE", "/v1/me/agent-enrichment-purpose/grants/"+g.ID, strings.NewReader(`{"expectedRevision":1}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || p.deletes != 1 || p.revision != g.Revision || p.access.SessionDigest != a.digest {
		t.Fatal("original by-ID writer not reused", w.Code, w.Body.String())
	}
	t.Logf("SYNTHETIC_ORIGINAL_REVOKE_WIRE %s", w.Body.Bytes())
}
func TestEnrichmentInventoryRegisteredDenialAndLateSession(t *testing.T) {
	for _, name := range []string{"missing token", "expired session", "Org workspace", "caller owner query", "GET body", "foreign DTO owner", "unavailable native", "late revoked session", "late retired Agent", "foreign Agent", "cancel before output", "wrong purpose", "nil capability", "typed nil capability"} {
		t.Run(name, func(t *testing.T) {
			p, a, h, token := inventoryHTTPFixture(t)
			want := 503
			var edit func(*http.Request)
			switch name {
			case "missing token":
				token = ""
				want = 401
			case "expired session":
				a.err = identity.ErrUnauthorized
				want = 401
			case "Org workspace":
				edit = func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", "") }
				want = 403
			case "caller owner query":
				edit = func(r *http.Request) { r.URL.RawQuery = "ownerId=" + privateProfileHTTPForeign }
				want = 400
			case "GET body":
				edit = func(r *http.Request) { r.Body = httptest.NewRequest("POST", "/", strings.NewReader("{}")).Body }
				want = 400
			case "foreign DTO owner":
				p.value.Owner.ID = privateProfileHTTPForeign
			case "unavailable native":
				p.err = errors.New("PRIVATE_DB_CANARY")
			case "late revoked session":
				a.late = identity.ErrUnauthorized
				want = 401
			case "late retired Agent":
				p.resolveError = acb.ErrDenied
				want = 403
			case "foreign Agent":
				p.resolvedAgent = &agentcognitive.AgentReference{AgentID: privateProfileHTTPForeign, Principal: p.value.Owner, Role: agentruntime.PersonalAgent}
			case "cancel before output":
				edit = func(r *http.Request) {
					ctx, c := context.WithCancel(r.Context())
					a.cancel = c
					*r = *r.WithContext(ctx)
				}
			case "wrong purpose":
				p.value.Grants[0].Purpose = "TASK_CONTEXT_READ"
			case "nil capability":
				h = New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			case "typed nil capability":
				var n *enrichmentInventoryHTTPPort
				h = New(n, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			}
			w := inventoryHTTPCall(h, token, edit)
			if w.Code != want || strings.Contains(w.Body.String(), "PRIVATE_DB_CANARY") || strings.Contains(w.Body.String(), "grants\"") {
				t.Fatal(name, w.Code, w.Body.String())
			}
		})
	}
}

type enrichmentInventoryNoAgentResolver struct {
	foundation.PublicCatalog
	aep.InventoryStore
}

func TestEnrichmentInventoryMissingCurrentAgentFailsClosed(t *testing.T) {
	p, a, _, token := inventoryHTTPFixture(t)
	catalog := &enrichmentInventoryNoAgentResolver{PublicCatalog: p, InventoryStore: p}
	h := New(catalog, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := inventoryHTTPCall(h, token, nil)
	if w.Code != 503 || p.calls != 0 || p.resolveCalls != 0 || strings.Contains(w.Body.String(), `"grants"`) {
		t.Fatal("missing current native Agent exposed stale history", w.Code, w.Body.String())
	}
}
