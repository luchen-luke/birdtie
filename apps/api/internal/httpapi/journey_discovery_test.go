package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type journeyDiscoveryHTTPSpy struct {
	collectionPublicationHTTPSpy
	page          content.JourneyDiscoveryPage
	lists, finals int
	cursor        string
	deny          error
}

func (s *journeyDiscoveryHTTPSpy) ListJourneyDiscovery(_ context.Context, d [32]byte, a identity.Actor, cursor string) (content.JourneyDiscoveryPage, error) {
	s.lists++
	s.cursor = cursor
	s.access = content.PublicMomentAccess{Actor: a, SessionDigest: d}
	return s.page, nil
}
func (s *journeyDiscoveryHTTPSpy) RevalidateJourneyDiscovery(_ context.Context, d [32]byte, a identity.Actor, v content.JourneyDiscoveryPage) error {
	s.finals++
	if a != s.access.Actor || d != s.access.SessionDigest || v.ViewerAccountID != a.ID {
		return content.ErrConflict
	}
	return s.deny
}

func TestJourneyDiscoveryHTTPAuthenticatedClosedAndFinalFence(t *testing.T) {
	token, _, _ := identity.NewToken()
	a := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	for _, tc := range []struct {
		query string
		deny  error
		want  int
		lists int
	}{{"", nil, 200, 1}, {"?city=aberdeen-gb", nil, 400, 0}, {"?cursor=jd1.a&cursor=jd1.b", nil, 400, 0}, {"", content.ErrNotFound, 404, 1}} {
		g := &journeyDiscoveryHTTPSpy{deny: tc.deny, page: content.JourneyDiscoveryPage{SchemaVersion: content.JourneyDiscoverySchema, ViewerAccountID: a.actor.ID, Items: []content.PublicCollection{}, ObservedAt: time.Now().UTC(), ValidUntil: time.Now().UTC().Add(time.Minute), SourceVersion: strings.Repeat("a", 64)}}
		r := httptest.NewRequest("GET", "/v1/me/journeys/discovery"+tc.query, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
		if w.Code != tc.want || g.lists != tc.lists || g.lists == 1 && g.finals != 1 {
			t.Fatal("discovery final boundary", tc.query, w.Code, w.Body.String(), g.lists, g.finals)
		}
	}
	r := httptest.NewRequest("GET", "/v1/me/journeys/discovery", nil)
	w := httptest.NewRecorder()
	g := &journeyDiscoveryHTTPSpy{}
	collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
	if w.Code != 401 || g.lists != 0 {
		t.Fatal("anonymous discovery bypass", w.Code)
	}
}
