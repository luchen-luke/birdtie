package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type collectionHTTPSpy struct {
	humanMomentLegacySpy
	calls  int
	actor  identity.Actor
	digest [32]byte
	after  func()
}

func (s *collectionHTTPSpy) hit(d [32]byte, a identity.Actor) {
	s.calls++
	s.actor = a
	s.digest = d
	if s.after != nil {
		s.after()
	}
}
func (s *collectionHTTPSpy) CreateHumanCollection(_ context.Context, d [32]byte, a identity.Actor, p content.CollectionCreate) (content.PersonalCollection, error) {
	s.hit(d, a)
	return content.PersonalCollection{ID: p.OperationID, CollectionInput: p.CollectionInput}, nil
}
func (s *collectionHTTPSpy) ListHumanCollections(_ context.Context, d [32]byte, a identity.Actor, k, c string) (content.CollectionPage, error) {
	s.hit(d, a)
	return content.CollectionPage{Items: []content.PersonalCollection{}}, nil
}
func (s *collectionHTTPSpy) GetHumanCollection(_ context.Context, d [32]byte, a identity.Actor, k, id string) (content.PersonalCollection, error) {
	s.hit(d, a)
	return content.PersonalCollection{ID: id}, nil
}
func (s *collectionHTTPSpy) GetHumanCollectionOperation(_ context.Context, d [32]byte, a identity.Actor, id string) (content.PersonalCollection, error) {
	s.hit(d, a)
	return content.PersonalCollection{ID: id}, nil
}
func (s *collectionHTTPSpy) UpdateHumanCollection(_ context.Context, d [32]byte, a identity.Actor, id string, rev int64, p content.CollectionInput) (content.PersonalCollection, error) {
	s.hit(d, a)
	return content.PersonalCollection{ID: id}, nil
}
func (s *collectionHTTPSpy) WithdrawHumanCollection(_ context.Context, d [32]byte, a identity.Actor, k, id string, rev int64) error {
	s.hit(d, a)
	return nil
}
func collectionTestHandler(access identity.AccessStore, store content.MomentStore) http.Handler {
	s := &server{access: access, content: store}
	mux := http.NewServeMux()
	s.registerPersonalCollectionRoutes(mux)
	return mux
}

const collectionBody = `{"operationId":"22222222-2222-4222-8222-222222222222","kind":"journey","cityId":"aberdeen-gb","title":"周末","summary":"","entries":[{"placeId":"11111111-1111-4111-8111-111111111111","note":"","locationVisibility":"hidden"}]}`

func TestPersonalCollectionHTTPCurrentActorAndNoFallback(t *testing.T) {
	token, _, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	for _, native := range []bool{false, true} {
		var store content.MomentStore = &humanMomentLegacySpy{}
		spy := &collectionHTTPSpy{}
		if native {
			store = spy
		}
		r := httptest.NewRequest("POST", "/v1/me/personal-collections", strings.NewReader(collectionBody))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		collectionTestHandler(access, store).ServeHTTP(w, r)
		if native {
			if w.Code != 201 || spy.calls != 1 || spy.actor != access.actor || spy.digest != access.seen {
				t.Fatal("trusted actor not forwarded", w.Code, spy)
			}
		} else if w.Code != 503 {
			t.Fatal("legacy fallback", w.Code)
		}
	}
}
func TestPersonalCollectionHTTPBadQueriesAndOrganization(t *testing.T) {
	token, _, _ := identity.NewToken()
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	for _, path := range []string{"?kind=journey&kind=theme_map", "?kind=journey&cursor=broken", "?kind=journey&unknown=x", "?kind=public"} {
		spy := &collectionHTTPSpy{}
		r := httptest.NewRequest("GET", "/v1/me/personal-collections"+path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		collectionTestHandler(access, spy).ServeHTTP(w, r)
		if w.Code != 400 || spy.calls != 0 {
			t.Fatal(path, w.Code)
		}
	}
	spy := &collectionHTTPSpy{}
	r := httptest.NewRequest("POST", "/v1/me/personal-collections", strings.NewReader(collectionBody))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Birdtie-Organization-Workspace", imageMomentID)
	w := httptest.NewRecorder()
	collectionTestHandler(access, spy).ServeHTTP(w, r)
	if w.Code != 403 || spy.calls != 0 {
		t.Fatal("organization bypass", w.Code)
	}
}
func TestPersonalCollectionHTTPCancelledNoReceipt(t *testing.T) {
	token, _, _ := identity.NewToken()
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spy := &collectionHTTPSpy{after: cancel}
	r := httptest.NewRequest("POST", "/v1/me/personal-collections", strings.NewReader(collectionBody)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	collectionTestHandler(access, spy).ServeHTTP(w, r)
	if w.Code < 400 || w.Body.Len() != 0 {
		t.Fatal("cancelled request disclosed receipt", w.Code)
	}
}
