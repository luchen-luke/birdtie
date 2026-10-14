package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	review "github.com/birdtie/birdtie/apps/api/internal/agentmemoryreview"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type memoryReviewHTTPSpy struct {
	*memoryHTTPStore
	page   review.MemoryPage
	cursor string
}

func (p *memoryReviewHTTPSpy) ReadOwnMemoryPage(_ context.Context, a agentprofile.PrivateAccess, cursor string) (review.MemoryPage, error) {
	p.calls++
	p.access = a
	p.cursor = cursor
	return p.page, p.err
}
func TestMemoryReviewPageHTTPClosedSelectorsAndPrivateOwner(t *testing.T) {
	s, _, old, token, _ := memoryHTTPFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	p := &memoryReviewHTTPSpy{memoryHTTPStore: old, page: review.MemoryPage{Schema: review.Schema, Collection: review.Memories, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}, AgentID: privateProfileHTTPAgent, Items: old.records, ObservedAt: now, ExpiresAt: now.Add(time.Minute)}}
	s.memories = p
	for _, query := range []string{"owner=x", "limit=1000", "cursor=", "cursor=a&cursor=b", "cursor=a&filter=active", "cursor=" + strings.Repeat("a", 1025)} {
		w := httptest.NewRecorder()
		r := privateProfileHTTPRequest("GET", "/v1/me/agent-memories/page?"+query, "", token, "application/json")
		s.listOwnAgentMemoryPage(w, r)
		if w.Code != 400 || p.calls != 0 {
			t.Fatal("unsafe page selector", query, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := privateProfileHTTPRequest("GET", "/v1/me/agent-memories/page?cursor=sealed_value", "", token, "application/json")
	s.listOwnAgentMemoryPage(w, r)
	if w.Code != 200 || p.cursor != "sealed_value" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	p.err = agentmemory.ErrConflict
	w = httptest.NewRecorder()
	s.listOwnAgentMemoryPage(w, r)
	if w.Code != 409 {
		t.Fatal("cursor version not authoritative", w.Code)
	}
	p.err = nil
	p.page.ModelAccess = true
	w = httptest.NewRecorder()
	s.listOwnAgentMemoryPage(w, r)
	if w.Code != 503 {
		t.Fatal("forged model grant accepted")
	}
}
