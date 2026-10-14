package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

type collectionAppendHTTPSpy struct {
	humanMomentLegacySpy
	r                    content.CollectionAppendReceipt
	reads, writes, stops int
	wrong                bool
	after                func()
	err                  error
}

func (s *collectionAppendHTTPSpy) AppendHumanCollectionPlaces(_ context.Context, _ [32]byte, _ identity.Actor, kind, id string, in content.CollectionAppendInput) (content.CollectionAppendReceipt, error) {
	s.writes++
	s.r = content.CollectionAppendReceipt{Kind: kind, CollectionID: id, OperationID: in.OperationID, AppliedRevision: in.ExpectedRevision + 1, Status: "SUCCEEDED"}
	if s.wrong {
		s.r.OperationID = "44444444-4444-4444-8444-444444444444"
	}
	if s.after != nil {
		s.after()
	}
	return s.r, s.err
}
func (s *collectionAppendHTTPSpy) GetHumanCollectionAppendOperation(_ context.Context, _ [32]byte, _ identity.Actor, _, _, _ string) (content.CollectionAppendReceipt, error) {
	s.reads++
	return s.r, s.err
}
func (s *collectionAppendHTTPSpy) StopHumanCollectionAppendOperation(_ context.Context, _ [32]byte, _ identity.Actor, k, id, op string, _ content.CollectionAppendStopInput) (content.CollectionAppendReceipt, error) {
	s.stops++
	s.r = content.CollectionAppendReceipt{Kind: k, CollectionID: id, OperationID: op, Status: "STOPPED"}
	return s.r, s.err
}

type collectionAppendHTTPAccess struct {
	humanMomentAccessSpy
	finalErr error
	final    int
}

func (a *collectionAppendHTTPAccess) ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error {
	a.final++
	return a.finalErr
}

const collectionAppendHTTPBody = `{"cityId":"aberdeen-gb","expectedRevision":1,"operationId":"22222222-2222-4222-8222-222222222222","entries":[{"placeId":"11111111-1111-4111-8111-111111111111","note":"","locationVisibility":"hidden"}]}`

func TestCollectionAppendRegisteredOriginalOpAndFinalSession(t *testing.T) {
	for _, change := range []string{"current", "wrong-op", "late-Session", "STOPPED"} {
		t.Run(change, func(t *testing.T) {
			token, _, _ := identity.NewToken()
			a := &collectionAppendHTTPAccess{humanMomentAccessSpy: humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}}
			spy := &collectionAppendHTTPSpy{}
			want := 200
			if change == "wrong-op" {
				spy.wrong = true
				want = 503
			}
			if change == "late-Session" {
				spy.after = func() { a.finalErr = identity.ErrUnauthorized }
				want = 401
			}
			path := "/v1/me/personal-collections/journey/" + imageMomentID + "/append-places"
			body := collectionAppendHTTPBody
			if change == "STOPPED" {
				path = "/v1/me/personal-collections/journey/" + imageMomentID + "/append-operations/22222222-2222-4222-8222-222222222222/stop"
				body = `{"cityId":"aberdeen-gb","expectedRevision":1}`
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			collectionTestHandler(a, spy).ServeHTTP(w, r)
			if w.Code != want {
				t.Fatal("registered append response", w.Code)
			}
			if want != 200 && strings.Contains(w.Body.String(), "appliedRevision") {
				t.Fatal("wrong/retired receipt emitted")
			}
			if change == "STOPPED" && (spy.writes != 0 || spy.stops != 1 || !strings.Contains(w.Body.String(), "STOPPED")) {
				t.Fatal("stop became append")
			}
		})
	}
}
func TestCollectionAppendRegisteredClosedBodyAndOwner(t *testing.T) {
	for _, change := range []string{"note", "duplicate", "organization", "query"} {
		token, _, _ := identity.NewToken()
		a := &collectionAppendHTTPAccess{humanMomentAccessSpy: humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}}
		s := &collectionAppendHTTPSpy{}
		body := collectionAppendHTTPBody
		path := "/v1/me/personal-collections/journey/" + imageMomentID + "/append-places"
		want := 400
		if change == "note" {
			body = strings.Replace(body, `"note":""`, `"note":"PRIVATE_SAVED_NOTE"`, 1)
		}
		if change == "duplicate" {
			body = strings.Replace(body, `"cityId":"aberdeen-gb"`, `"cityId":"aberdeen-gb","cityId":"other"`, 1)
		}
		if change == "query" {
			path += "?owner=other"
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		if change == "organization" {
			r.Header.Set("X-Birdtie-Organization-Workspace", imageMomentID)
			want = 403
		}
		w := httptest.NewRecorder()
		collectionTestHandler(a, s).ServeHTTP(w, r)
		if w.Code != want || s.writes != 0 {
			t.Fatal("closed append violated", change, w.Code)
		}
	}
}
