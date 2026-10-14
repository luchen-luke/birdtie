package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

type openCandidateStore struct {
	cityseed.Store
	listed, written int
	deny            bool
	failAt          int
	failure         error
	duplicate       bool
	actor, city     string
	session         cityseed.HumanSession
}

func (s *openCandidateStore) List(ctx context.Context, actor, city string) ([]cityseed.Candidate, error) {
	s.listed++
	if s.deny {
		return nil, cityseed.ErrForbidden
	}
	return nil, nil
}
func (s *openCandidateStore) Submit(ctx context.Context, actor, city string, in cityseed.SubmitInput) (cityseed.Candidate, error) {
	s.written++
	s.actor, s.city = actor, city
	s.session, _ = cityseed.CurrentHumanSession(ctx)
	if s.duplicate {
		return cityseed.Candidate{}, cityseed.ErrDuplicate
	}
	if s.failAt == s.written {
		if s.failure != nil {
			return cityseed.Candidate{}, s.failure
		}
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	return cityseed.Candidate{ID: cityActivityHTTPCandidate, Status: "pending"}, nil
}

type openCatalog struct {
	foundation.PublicCatalog
	read       int
	withoutMap bool
}

func (c *openCatalog) GetCity(ctx context.Context, id string) (foundation.City, error) {
	c.read++
	if c.withoutMap {
		return foundation.City{ID: id}, nil
	}
	return foundation.City{ID: id, Map: &foundation.CityMap{Latitude: 57.1497, Longitude: -2.0943}}, nil
}

type openSource struct {
	calls int
	query agenttool.OpenPlaceQuery
	facts []cityseed.SubmitInput
	err   error
}

func (s *openSource) Search(ctx context.Context, q agenttool.OpenPlaceQuery) ([]cityseed.SubmitInput, error) {
	s.calls++
	s.query = q
	return s.facts, s.err
}
func openHTTPFixture(t *testing.T) (*server, *openCandidateStore, *openCatalog, *openSource, string) {
	t.Helper()
	s, _, token := placePermissionHTTPFixture(t)
	store := &openCandidateStore{}
	catalog := &openCatalog{}
	source := &openSource{facts: []cityseed.SubmitInput{placePermissionHTTPInput()}}
	s.seed, s.catalog, s.openPlaces = store, catalog, source
	return s, store, catalog, source, token
}
func acquire(t *testing.T, s *server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := privateProfileHTTPRequest(http.MethodPost, "/", body, token, "application/json")
	r.SetPathValue("cityID", "native-city")
	w := httptest.NewRecorder()
	s.acquireOpenPlaceCandidates(w, r)
	return w
}
func TestOpenPlaceAcquisitionUnknownCommitIsNotNotImported(t *testing.T) {
	s, store, _, source, token := openHTTPFixture(t)
	fact := placePermissionHTTPInput()
	fact.ProviderCode = "openstreetmap"
	fact.ProviderPlaceID = "node/123"
	fact.Attribution = "Community fixture"
	source.facts = []cityseed.SubmitInput{fact, fact, fact}
	store.failAt = 2
	store.failure = errors.New("synthetic commit response lost")
	w := acquire(t, s, token, `{"category":"museum"}`)
	if w.Code != 200 || store.written != 2 || !containsBody(w.Body.String(), `"notAttempted":1`, `"outcome":"UNKNOWN"`, `"providerPlaceId":"node/123"`) || strings.Contains(w.Body.String(), "notImported") || strings.Contains(w.Body.String(), "response lost") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestOpenPlaceAcquisitionNativeCandidateFlow(t *testing.T) {
	s, store, catalog, source, token := openHTTPFixture(t)
	w := acquire(t, s, token, `{"category":"museum"}`)
	if w.Code != 200 || store.written != 1 || catalog.read != 1 || source.calls != 1 || store.session.ActorID != store.actor || store.session.Digest == ([32]byte{}) || store.city != "native-city" || source.query.Latitude != 57.1497 || source.query.Longitude != -2.0943 || source.query.RadiusMeters != 6000 || source.query.Category != "museum" {
		t.Fatal("native city/session/candidate path mismatch", w.Code, w.Body.String())
	}
}
func TestOpenPlaceAcquisitionBeforeNetworkBoundaries(t *testing.T) {
	for _, which := range []string{"editor", "source", "category", "coordinate-injection", "map", "authorization"} {
		t.Run(which, func(t *testing.T) {
			s, store, _, source, token := openHTTPFixture(t)
			body := `{"category":"museum"}`
			want := 400
			switch which {
			case "editor":
				store.deny = true
				want = 403
			case "source":
				s.openPlaces = nil
				want = 503
			case "category":
				body = `{"category":"museum\"];out;"}`
			case "coordinate-injection":
				body = `{"category":"museum","latitude":1}`
			case "map":
				s.catalog.(*openCatalog).withoutMap = true
				want = 422
			case "authorization":
				token = ""
				want = 401
			}
			w := acquire(t, s, token, body)
			if w.Code != want || source.calls != 0 || store.written != 0 {
				t.Fatal(which, w.Code, w.Body.String())
			}
		})
	}
}
func TestOpenPlaceAcquisitionValidatesWholeBatch(t *testing.T) {
	s, store, _, source, token := openHTTPFixture(t)
	bad := placePermissionHTTPInput()
	bad.Rights = nil
	source.facts = append(source.facts, bad)
	w := acquire(t, s, token, `{"category":"museum"}`)
	if w.Code != 502 || store.written != 0 {
		t.Fatal("invalid later field allowed earlier writes", w.Code)
	}
}
func containsBody(body string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(body, fragment) {
			return false
		}
	}
	return true
}
func TestOpenPlaceAcquisitionDuplicateAndPartialAreHonest(t *testing.T) {
	for _, partial := range []bool{false, true} {
		s, store, _, source, token := openHTTPFixture(t)
		if partial {
			source.facts = append(source.facts, placePermissionHTTPInput())
			store.failAt = 2
		} else {
			store.duplicate = true
		}
		w := acquire(t, s, token, `{"category":"museum"}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if partial && !containsBody(w.Body.String(), `"notAttempted":0`, `"outcome":"NOT_WRITTEN"`, `"status":"PARTIAL_PENDING_REVIEW"`, `"status":"pending"`) {
			t.Fatal(w.Body.String())
		}
		if !partial && !containsBody(w.Body.String(), `"duplicates":1`, `"data":[]`) {
			t.Fatal(w.Body.String())
		}
	}
}
func TestOpenPlaceAcquisitionSourceFailureHasNoWrite(t *testing.T) {
	s, store, _, source, token := openHTTPFixture(t)
	source.err = errors.New("untrusted wire body")
	w := acquire(t, s, token, `{"category":"museum"}`)
	if w.Code != 502 || store.written != 0 || containsBody(w.Body.String(), "untrusted") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestOpenPlaceAcquisitionCurrentReuseIsDistinctFromPendingDuplicate(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "published-current"}[current], func(t *testing.T) {
			s, store, _, _, token := openHTTPFixture(t)
			store.failAt = 1
			store.failure = cityseed.ErrDuplicate
			want := `"reusedCurrent":0`
			wantStatus := `"status":"PENDING_REVIEW"`
			if current {
				store.failure = errors.Join(cityseed.ErrDuplicate, cityseed.ErrSourceAlreadyCurrent)
				want = `"reusedCurrent":1`
				wantStatus = `"status":"REUSED_CURRENT"`
			}
			w := acquire(t, s, token, `{"category":"museum"}`)
			if w.Code != 200 || !containsBody(w.Body.String(), `"duplicates":1`, `"data":[]`, want, wantStatus) {
				t.Fatal("pending conflict must not claim current published reuse", w.Code, w.Body.String())
			}
		})
	}
}
