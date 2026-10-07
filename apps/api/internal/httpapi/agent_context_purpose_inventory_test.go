package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
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

func TestTaskContextInventoryRegisteredRouteExists(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/me/agent-context/grants", nil))
	if w.Code != 503 {
		t.Fatalf("actual registered route absent or not fail closed: %d %s", w.Code, w.Body.String())
	}
}

type taskInventoryHTTPPort struct {
	foundation.PublicCatalog
	acb.PurposeStore
	value         acb.PurposeInventory
	calls         int
	access        agentprofile.PrivateAccess
	resolveCalls  int
	resolveError  error
	resolvedAgent *agentcognitive.AgentReference
}

func (p *taskInventoryHTTPPort) ResolveOwnContextAgent(_ context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
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

func (p *taskInventoryHTTPPort) ListOwnContextPurposes(_ context.Context, a agentprofile.PrivateAccess) (acb.PurposeInventory, error) {
	p.calls++
	p.access = a
	return p.value, nil
}
func (p *taskInventoryHTTPPort) ReadOwnContextPurpose(_ context.Context, a agentprofile.PrivateAccess, id string) (acb.PurposeGrant, error) {
	g := p.value.Grants[0]
	p.access = a
	return acb.PurposeGrant{ID: g.ID, Revision: g.Revision, Purpose: g.Purpose, CreatedAt: g.CreatedAt, ExpiresAt: g.ExpiresAt, RevokedAt: g.RevokedAt, Selection: acb.PurposeSelection{AgentID: p.value.AgentID, TaskID: g.TaskID, CityID: g.CityID, TaskUpdatedAt: g.TaskUpdatedAt, ProfileFields: g.ProfileFields, MemoryIDs: g.MemoryIDs, PlaceIDs: g.PlaceIDs, ActivityIDs: g.ActivityIDs, RelationshipTieIDs: g.RelationshipTieIDs, PolicyFamilies: g.PolicyFamilies, QueryDigest: strings.Repeat("a", 64), DeadlineAt: g.ExpiresAt}, Sources: []acb.Source{}}, nil
}
func (p *taskInventoryHTTPPort) RevokeOwnContextPurpose(c context.Context, a agentprofile.PrivateAccess, id string, r int64) (acb.PurposeGrant, error) {
	v, e := p.ReadOwnContextPurpose(c, a, id)
	if e != nil {
		return v, e
	}
	if r != v.Revision {
		return v, acb.ErrDenied
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	v.Revision++
	v.RevokedAt = &at
	return v, nil
}
func taskInventoryHTTPFixture(t *testing.T) (*taskInventoryHTTPPort, *enrichmentInventoryHTTPAccess, http.Handler, string) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}
	p := &taskInventoryHTTPPort{value: acb.PurposeInventory{SchemaVersion: acb.PurposeInventorySchema, Owner: owner, AgentID: privateProfileHTTPAgent, ObservedAt: at, ValidUntil: at.Add(30 * time.Second), Limit: 50, Grants: []acb.PurposeInventoryGrant{{ID: privateProfileHTTPForeign, Revision: 1, Purpose: acb.TaskContextRead, TaskID: "81000000-0000-4000-8000-000000000004", CityID: "aberdeen", TaskUpdatedAt: at.Add(-time.Minute), ProfileFields: []string{"agentNotes"}, MemoryIDs: []string{}, PlaceIDs: []string{}, ActivityIDs: []string{}, RelationshipTieIDs: []string{}, PolicyFamilies: []agentpolicysettings.Family{}, CreatedAt: at.Add(-time.Second), ExpiresAt: at.Add(time.Minute)}}}}
	a := &enrichmentInventoryHTTPAccess{digest: d, actor: identity.Actor{ID: owner.ID, AccountType: "person"}}
	return p, a, New(p, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil), token
}
func taskInventoryHTTPCall(h http.Handler, token string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/v1/me/agent-context/grants", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestTaskContextInventoryRegisteredProjectionAndOriginalRevoke(t *testing.T) {
	p, a, h, token := taskInventoryHTTPFixture(t)
	w := taskInventoryHTTPCall(h, token, nil)
	if w.Code != 200 || p.access.SessionDigest != a.digest || a.finalCalls != 1 || p.resolveCalls != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, bad := range []string{"queryDigest", "rowToken", "authority", "review", "session"} {
		if strings.Contains(w.Body.String(), bad) {
			t.Fatal("control data exposed", bad)
		}
	}
	t.Logf("SYNTHETIC_REGISTERED_CURRENT_SESSION_INVENTORY %s", w.Body.Bytes())
	r := httptest.NewRequest("DELETE", "/v1/me/agent-context/grants/"+p.value.Grants[0].ID, strings.NewReader(`{"expectedRevision":1}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	reply := httptest.NewRecorder()
	h.ServeHTTP(reply, r)
	var body struct {
		Data acb.PurposeGrant `json:"data"`
	}
	if reply.Code != 200 || json.Unmarshal(reply.Body.Bytes(), &body) != nil || body.Data.Revision != 2 || body.Data.RevokedAt == nil {
		t.Fatal("original revoke not reused", reply.Code, reply.Body.String())
	}
}
func TestTaskContextInventoryRegisteredBoundaries(t *testing.T) {
	for _, name := range []string{"query", "body", "Org", "foreign owner", "invalid", "late Session", "late Agent", "foreign Agent", "cancel"} {
		t.Run(name, func(t *testing.T) {
			p, a, h, token := taskInventoryHTTPFixture(t)
			want := 503
			edit := func(r *http.Request) {}
			switch name {
			case "query":
				want = 400
				edit = func(r *http.Request) { r.URL.RawQuery = "x=1" }
			case "body":
				want = 400
				edit = func(r *http.Request) { r.Body = httptest.NewRequest("GET", "/", strings.NewReader("{} ")).Body }
			case "Org":
				want = 403
				edit = func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", "other") }
			case "foreign owner":
				p.value.Owner.ID = "81000000-0000-4000-8000-000000000008"
			case "invalid":
				p.value.Grants[0].Purpose = "MODEL_EGRESS"
			case "late Session":
				want = 401
				a.late = identity.ErrUnauthorized
			case "late Agent":
				want = 403
				p.resolveError = acb.ErrDenied
			case "foreign Agent":
				p.resolvedAgent = &agentcognitive.AgentReference{AgentID: privateProfileHTTPForeign, Principal: p.value.Owner, Role: agentruntime.PersonalAgent}
			case "cancel":
				c, cancel := context.WithCancel(context.Background())
				a.cancel = cancel
				edit = func(r *http.Request) { *r = *r.WithContext(c) }
			}
			w := taskInventoryHTTPCall(h, token, edit)
			if w.Code != want || strings.Contains(w.Body.String(), `"grants"`) {
				t.Fatal(name, w.Code, w.Body.String())
			}
		})
	}
}

type taskInventoryNoAgentResolver struct {
	foundation.PublicCatalog
	acb.PurposeInventoryStore
}

func TestTaskContextInventoryMissingCurrentAgentFailsClosed(t *testing.T) {
	p, a, _, token := taskInventoryHTTPFixture(t)
	catalog := &taskInventoryNoAgentResolver{PublicCatalog: p, PurposeInventoryStore: p}
	h := New(catalog, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := taskInventoryHTTPCall(h, token, nil)
	if w.Code != 503 || p.calls != 0 || p.resolveCalls != 0 || strings.Contains(w.Body.String(), `"grants"`) {
		t.Fatal("missing current native Agent resolver exposed stale inventory", w.Code, w.Body.String())
	}
}
