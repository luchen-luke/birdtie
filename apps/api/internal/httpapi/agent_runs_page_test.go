package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"testing"
	"time"
)

type runPageHTTPSpy struct {
	ar.Gateway
	calls, checks int
	cursor        string
	page          ar.Page
	err           error
}

func (s *runPageHTTPSpy) AuthenticateRunPage(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, nil
}
func (s *runPageHTTPSpy) ReadOwnRunPage(_ context.Context, _ agentprofile.PrivateAccess, cursor string) (ar.Page, error) {
	s.calls++
	s.cursor = cursor
	return s.page, s.err
}
func (s *runPageHTTPSpy) RevalidateOwnRunPage(_ context.Context, _ agentprofile.PrivateAccess, p ar.Page) error {
	s.checks++
	if p.Proof != "native-proof" {
		return ar.ErrConflict
	}
	return s.err
}
func TestAgentRunPageHTTPClosedQueryBareWireAndFinalFence(t *testing.T) {
	s, _, _, token, _ := memoryHTTPFixture(t)
	now := time.Now().UTC()
	g := &runPageHTTPSpy{page: ar.Page{Schema: ar.PageSchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}, AgentID: privateProfileHTTPAgent, Items: []ar.Record{}, ObservedAt: now, ExpiresAt: now.Add(time.Minute), Proof: "native-proof"}}
	s.agentRuns = g
	for _, q := range []string{"owner=x", "cursor=", "cursor=a&cursor=b", "cursor=x&limit=20"} {
		w := httptest.NewRecorder()
		s.listOwnAgentRunPage(w, privateProfileHTTPRequest("GET", "/v1/me/agent-runs/page?"+q, "", token, "application/json"))
		if w.Code != 400 || g.calls != 0 {
			t.Fatal(q, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.listOwnAgentRunPage(w, privateProfileHTTPRequest("GET", "/v1/me/agent-runs/page?cursor=opaque", "", token, "application/json"))
	if w.Code != 200 || g.cursor != "opaque" || g.checks != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	g.err = ar.ErrConflict
	w = httptest.NewRecorder()
	s.listOwnAgentRunPage(w, privateProfileHTTPRequest("GET", "/v1/me/agent-runs/page", "", token, "application/json"))
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}
