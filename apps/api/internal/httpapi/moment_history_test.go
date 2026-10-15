package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type historyHTTPSpy struct {
	content.MomentStore
	foundation.PublicCatalog
	reads, finalizes int
	changed          bool
}

func (s *historyHTTPSpy) ReadOwnMomentHistory(_ context.Context, _ [32]byte, _ identity.Actor, q content.MomentHistoryQuery) (content.OwnMomentHistoryPage, error) {
	s.reads++
	now := time.Now().UTC()
	return content.OwnMomentHistoryPage{SchemaVersion: "owner-moment-history-v1", Status: q.Status, Items: []content.Moment{}, ObservedAt: now, SnapshotAt: now, ValidUntil: now.Add(time.Minute), RefreshCursor: "synthetic-protected-cursor"}, nil
}
func (s *historyHTTPSpy) RevalidateOwnMomentHistory(context.Context, [32]byte, identity.Actor, content.MomentHistoryQuery, content.OwnMomentHistoryPage) error {
	s.finalizes++
	if s.changed {
		return content.ErrMomentHistoryChanged
	}
	return nil
}
func (s *historyHTTPSpy) ReadPublicMomentHistory(_ context.Context, _ content.PublicMomentAccess, q content.MomentHistoryQuery) (content.PublicMomentHistoryPage, error) {
	s.reads++
	now := time.Now().UTC()
	return content.PublicMomentHistoryPage{SchemaVersion: "public-moment-history-v1", Scope: q.Scope, CityID: q.CityID, AuthorID: q.AuthorID, From: q.From, To: q.To, Items: []content.PublicMomentWithContext{}, ObservedAt: now, SnapshotAt: now, ValidUntil: now.Add(time.Minute), RefreshCursor: "synthetic-protected-cursor"}, nil
}
func (s *historyHTTPSpy) RevalidatePublicMomentHistory(context.Context, content.PublicMomentAccess, content.MomentHistoryQuery, content.PublicMomentHistoryPage) error {
	s.finalizes++
	if s.changed {
		return content.ErrMomentHistoryChanged
	}
	return nil
}

func TestMomentHistoryHTTPRegisteredClosedReadsAndFinalFence(t *testing.T) {
	token, _, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		path, token, body string
		changed           bool
		want              int
	}{
		{"/v1/me/moments/history", token, "", false, 200},
		{"/v1/moments/history?scope=city&cityId=aberdeen-gb", "", "", false, 200},
		{"/v1/moments/history?scope=friends&cityId=aberdeen-gb", "", "", false, 401},
		{"/v1/moments/history?scope=city&cityId=aberdeen-gb&scope=city", "", "", false, 400},
		{"/v1/me/moments/history?ownerId=someone", token, "", false, 400},
		{"/v1/me/moments/history", token, "{}", false, 400},
		{"/v1/moments/history?scope=city&cityId=aberdeen-gb", "", "", true, 409},
	} {
		spy := &historyHTTPSpy{changed: tc.changed}
		access := &humanMomentAccessSpy{actor: identity.Actor{ID: hcTestID, AccountType: "person"}}
		h := New(spy, access, nil, spy, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
		r := httptest.NewRequest("GET", tc.path, strings.NewReader(tc.body))
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal("registered history route", tc.path, w.Code, w.Body.String())
		}
		if tc.want == 200 && (spy.reads != 1 || spy.finalizes != 1 || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("history did not finalize actual registered source")
		}
		if tc.want == 409 && strings.Contains(w.Body.String(), "\"items\"") {
			t.Fatal("changed source emitted old payload")
		}
	}
}
