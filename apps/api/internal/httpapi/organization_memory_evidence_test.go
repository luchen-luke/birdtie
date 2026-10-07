package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationevidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type organizationEvidenceTransportCapability interface {
	agentorganizationevidence.Store
}
type organizationEvidenceTransport struct {
	foundation.PublicCatalog
	agentorganizationmemory.Store
	organizationEvidenceTransportCapability
	now   time.Time
	err   error
	calls int
}

func (f *organizationEvidenceTransport) ValidateOrganizationMemoryAccess(context.Context, agentorganizationmemory.Access) (time.Time, error) {
	f.calls++
	return f.now, f.err
}

type organizationEvidenceTransportAccess struct{ *privateProfileHTTPAccess }

func (a organizationEvidenceTransportAccess) AuthenticateOrganizationAgent(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	return a.Authenticate(ctx, digest)
}
func TestOrganizationEvidenceHTTPRejectsAuthorityBeforeStore(t *testing.T) {
	for name, body := range map[string]string{"confirmed": `{"expectedMemoryVersion":1,"sourceType":"ORGANIZATION_PROFILE","sourceId":"` + privateProfileHTTPOwner + `","confirmed":true}`, "duplicate": `{"expectedMemoryVersion":1,"expectedMemoryVersion":2,"sourceType":"ORGANIZATION_PROFILE","sourceId":"` + privateProfileHTTPOwner + `"}`, "wrong_kind": `{"expectedMemoryVersion":1,"sourceType":"MOMENT","sourceId":"` + privateProfileHTTPOwner + `"}`, "null": "null", "array": "[]", "fraction": `{"expectedMemoryVersion":1.2,"sourceType":"ORGANIZATION_PROFILE","sourceId":"` + privateProfileHTTPOwner + `"}`} {
		t.Run(name, func(t *testing.T) {
			s, a, _, token := privateProfileHTTPFixture(t)
			s.access = organizationEvidenceTransportAccess{a}
			spy := &organizationEvidenceTransport{now: time.Now()}
			s.catalog = spy
			r := privateProfileHTTPRequest("PUT", "/source", body, token, "application/json")
			r.SetPathValue("organizationID", privateProfileHTTPOwner)
			r.SetPathValue("memoryID", privateProfileHTTPAgent)
			r.SetPathValue("evidenceID", privateProfileHTTPForeign)
			w := httptest.NewRecorder()
			s.putOrganizationMemoryEvidence(w, r)
			if w.Code != 400 || spy.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String(), spy.calls)
			}
		})
	}
}
func TestOrganizationEvidenceHTTPCanonicalPath(t *testing.T) {
	for _, id := range []string{"", strings.ToUpper("ac000000-0000-4000-8000-000000000001"), "00000000-0000-0000-0000-000000000000", "relative/other"} {
		t.Run(id, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.SetPathValue("memoryID", id)
			if _, e := organizationEvidencePath(r, "memoryID"); e == nil {
				t.Fatal("invalid ID accepted")
			}
		})
	}
}
