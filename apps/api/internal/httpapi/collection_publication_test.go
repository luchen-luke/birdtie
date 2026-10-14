package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type collectionPublicationHTTPSpy struct {
	humanMomentLegacySpy
	content.CollectionPublicationStore
	calls, reads, checks int
	action               string
	actor                identity.Actor
	digest               [32]byte
	access               content.PublicMomentAccess
	approval             content.CollectionPublicationInput
	closeInput           content.CollectionPublicationCloseInput
	closeError           error
	terminal             string
	public               content.PublicCollection
	currentError         error
	after                func()
	check                func(content.PublicCollection) error
}

func (s *collectionPublicationHTTPSpy) hit(a identity.Actor, d [32]byte, action string) {
	s.calls++
	s.actor = a
	s.digest = d
	s.action = action
	if s.after != nil {
		s.after()
	}
}
func (s *collectionPublicationHTTPSpy) PublishHumanCollection(_ context.Context, d [32]byte, a identity.Actor, k, id string, in content.CollectionPublicationInput) (content.PersonalCollection, error) {
	s.hit(a, d, "PUBLISH")
	s.approval = in
	return content.PersonalCollection{ID: id, Revision: in.Revision + 1, Status: "published", Visibility: "public", PublicationOperation: &content.CollectionPublicationOperation{OperationID: in.OperationID, Action: "PUBLISH", Outcome: "COMMITTED", ExpectedRevision: in.Revision, ResultRevision: in.Revision + 1}}, nil
}
func (s *collectionPublicationHTTPSpy) UnpublishHumanCollection(_ context.Context, d [32]byte, a identity.Actor, k, id string, in content.CollectionUnpublishInput) (content.PersonalCollection, error) {
	s.hit(a, d, "UNPUBLISH")
	return content.PersonalCollection{ID: id, Revision: in.Revision + 1, Status: "draft", Visibility: "private"}, nil
}
func (s *collectionPublicationHTTPSpy) GetHumanCollectionPublicationOperation(_ context.Context, d [32]byte, a identity.Actor, op string) (content.PersonalCollection, error) {
	s.hit(a, d, "RECOVER")
	meta := &content.CollectionPublicationOperation{OperationID: op, Action: "PUBLISH", Outcome: "COMMITTED", ExpectedRevision: 1, ResultRevision: 2}
	if s.terminal == "NO_EFFECT" {
		meta.Outcome, meta.ResultRevision = "NO_EFFECT", 0
	}
	return content.PersonalCollection{ID: imageMomentID, Status: "draft", Visibility: "private", Revision: 4, PublicationOperation: meta}, s.closeError
}
func (s *collectionPublicationHTTPSpy) CloseHumanCollectionPublicationOperation(_ context.Context, d [32]byte, a identity.Actor, op string, in content.CollectionPublicationCloseInput) (content.PersonalCollection, error) {
	s.hit(a, d, "CLOSE")
	s.closeInput = in
	meta := &content.CollectionPublicationOperation{OperationID: op, Action: in.Action, Outcome: "NO_EFFECT", ExpectedRevision: in.Revision}
	if s.terminal == "COMMITTED" {
		meta.Outcome, meta.ResultRevision = "COMMITTED", in.Revision+1
	}
	return content.PersonalCollection{ID: in.ID, CollectionInput: content.CollectionInput{Kind: in.Kind}, Status: "draft", Visibility: "private", Revision: 4, PublicationOperation: meta}, s.closeError
}
func (s *collectionPublicationHTTPSpy) GetPublicCollection(_ context.Context, a content.PublicMomentAccess, k, id string) (content.PublicCollection, error) {
	s.reads++
	s.access = a
	return s.public, nil
}
func (s *collectionPublicationHTTPSpy) RevalidatePublicCollection(_ context.Context, a content.PublicMomentAccess, v content.PublicCollection) error {
	s.checks++
	if a != s.access {
		return content.ErrUnavailable
	}
	if s.check != nil {
		return s.check(v)
	}
	return s.currentError
}
func collectionPublicationHTTPHandler(a identity.AccessStore, g content.MomentStore) http.Handler {
	s := &server{access: a, content: g}
	mux := http.NewServeMux()
	s.registerCollectionPublicationRoutes(mux)
	return mux
}

const collectionPublicationBody = `{"operationId":"22222222-2222-4222-8222-222222222222","revision":1,"entries":[{"placeId":"11111111-1111-4111-8111-111111111111","locationVisibility":"hidden"}]}`

func TestCollectionPublicationHTTPTrustedActorClosedApprovalAndRecovery(t *testing.T) {
	token, _, _ := identity.NewToken()
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	base := "/v1/me/personal-collections/journey/" + imageMomentID
	for _, c := range []struct{ method, path, body, action string }{{"POST", base + "/publication", collectionPublicationBody, "PUBLISH"}, {"POST", base + "/unpublish", `{"operationId":"22222222-2222-4222-8222-222222222222","revision":2}`, "UNPUBLISH"}, {"GET", "/v1/me/personal-collections/publication-operations/22222222-2222-4222-8222-222222222222/receipt", "", "RECOVER"}} {
		g := &collectionPublicationHTTPSpy{}
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		collectionPublicationHTTPHandler(access, g).ServeHTTP(w, r)
		if w.Code != 200 || g.calls != 1 || g.action != c.action || g.actor != access.actor || g.digest != access.seen {
			t.Fatal(c.action, w.Code, w.Body.String(), g.calls)
		}
		if c.action == "RECOVER" && !strings.Contains(w.Body.String(), `"resultRevision":2`) {
			t.Fatal("causality missing", w.Body.String())
		}
	}
	for _, c := range []struct{ suffix, body string }{{"", strings.Replace(collectionPublicationBody, `"revision":1`, `"revision":0`, 1)}, {"", strings.Replace(collectionPublicationBody, `"revision":1`, `"revision":1,"ownerId":"spoof"`, 1)}, {"", strings.Replace(collectionPublicationBody, `"locationVisibility":"hidden"`, `"locationVisibility":"public"`, 1)}, {"?source=old", collectionPublicationBody}} {
		g := &collectionPublicationHTTPSpy{}
		r := httptest.NewRequest("POST", base+"/publication"+c.suffix, strings.NewReader(c.body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		collectionPublicationHTTPHandler(access, g).ServeHTTP(w, r)
		if w.Code != 400 || g.calls != 0 {
			t.Fatal("unapproved field/query reached store", w.Code, w.Body.String())
		}
	}
	for _, g := range []content.MomentStore{&humanMomentLegacySpy{}, &collectionPublicationHTTPSpy{}} {
		r := httptest.NewRequest("POST", base+"/publication", strings.NewReader(collectionPublicationBody))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, ok := g.(*collectionPublicationHTTPSpy); ok {
			r.Header.Set("X-Birdtie-Organization-Workspace", imageMomentID)
		}
		collectionPublicationHTTPHandler(access, g).ServeHTTP(w, r)
		if w.Code != 503 && w.Code != 403 {
			t.Fatal("legacy or organization fallback", w.Code)
		}
	}
}

type collectionPublicationAccessSpy struct {
	identity.AccessStore
	actor identity.Actor
	calls int
	after func()
	err   error
}

func (s *collectionPublicationAccessSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	s.calls++
	if s.calls == 2 && s.after != nil {
		s.after()
	}
	return s.actor, s.err
}
func publicationHTTPPublicFixture() content.PublicCollection {
	return content.PublicCollection{ID: imageMomentID, Kind: "journey", CityID: "aberdeen-gb", Title: "公开测试", Summary: "本人明示文本", Revision: 2, SourceExpiresAt: time.Now().Add(time.Minute).UTC(), SourceBinding: "internal-source", ResponseProof: "native-only", Entries: []content.PublicCollectionEntry{{Note: "原备注", LocationVisibility: "hidden"}, {LocationVisibility: "city"}}}
}
func TestCollectionPublicationHTTPPublicFreezeAndFinalGate(t *testing.T) {
	token, _, _ := identity.NewToken()
	path := "/v1/public-collections/journey/" + imageMomentID
	for _, test := range []string{"anonymous", "current", "source-retired", "session-retired", "mutable-alias", "hidden-leak", "cancelled"} {
		t.Run(test, func(t *testing.T) {
			g := &collectionPublicationHTTPSpy{public: publicationHTTPPublicFixture()}
			a := &collectionPublicationAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test == "source-retired" {
				g.currentError = content.ErrNotFound
			}
			if test == "session-retired" {
				a.after = func() { a.err = identity.ErrUnauthorized }
			}
			if test == "hidden-leak" {
				id := imageMomentID
				g.public.Entries[0].PlaceID = &id
			}
			if test == "mutable-alias" {
				a.after = func() { g.public.Entries[0].Note = "late-mutated" }
				g.check = func(v content.PublicCollection) error {
					if v.Entries[0].Note != "原备注" || v.SourceExpiresAt != g.public.SourceExpiresAt || v.ResponseProof != "native-only" || v.SourceBinding != "internal-source" {
						t.Fatal("encoded snapshot aliased or TTL extended", v)
					}
					return content.ErrUnavailable
				}
			}
			if test == "cancelled" {
				g.check = func(content.PublicCollection) error { cancel(); return nil }
			}
			r := httptest.NewRequest("GET", path, nil).WithContext(ctx)
			if test != "anonymous" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
			if test == "anonymous" || test == "current" {
				if w.Code != 200 || g.reads != 1 || g.checks != 1 {
					t.Fatal(w.Code, w.Body.String(), g.reads, g.checks)
				}
				if test == "anonymous" && a.calls != 0 || test == "current" && a.calls != 2 {
					t.Fatal("auth/final read order", a.calls)
				}
				var dto struct{ Data map[string]any }
				if json.Unmarshal(w.Body.Bytes(), &dto) != nil {
					t.Fatal("wire")
				}
				for _, entry := range dto.Data["entries"].([]any) {
					m := entry.(map[string]any)
					for _, key := range []string{"placeId", "placeName", "latitude", "longitude"} {
						if _, ok := m[key]; ok {
							t.Fatal("redacted stop leaked", key)
						}
					}
				}
				if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "internal-source") || strings.Contains(w.Body.String(), "native-only") {
					t.Fatal("private proof/cache leak")
				}
			} else if test == "cancelled" {
				if w.Body.Len() != 0 {
					t.Fatal("cancelled emitted")
				}
			} else if w.Code < 400 || strings.Contains(w.Body.String(), "原备注") {
				t.Fatal("stale public disclosure", w.Code, w.Body.String())
			}
		})
	}
}
func TestCollectionPublicationHTTPInvalidAuthorizationNeverAnonymous(t *testing.T) {
	path := "/v1/public-collections/journey/" + imageMomentID
	token, _, _ := identity.NewToken()
	for _, test := range []string{"broken-bearer", "expired", "organization", "query", "body", "typed-nil"} {
		t.Run(test, func(t *testing.T) {
			g := &collectionPublicationHTTPSpy{public: publicationHTTPPublicFixture()}
			a := &collectionPublicationAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
			r := httptest.NewRequest("GET", path, nil)
			switch test {
			case "broken-bearer":
				r.Header.Set("Authorization", "broken")
			case "expired":
				a.err = identity.ErrUnauthorized
				r.Header.Set("Authorization", "Bearer "+token)
			case "organization":
				r.Header.Set("X-Birdtie-Organization-Workspace", imageMomentID)
			case "query":
				r = httptest.NewRequest("GET", path+"?include=private", nil)
			case "body":
				r = httptest.NewRequest("GET", path, strings.NewReader("x"))
			}
			w := httptest.NewRecorder()
			var store content.MomentStore = g
			if test == "typed-nil" {
				var missing *collectionPublicationHTTPSpy
				store = missing
			}
			collectionPublicationHTTPHandler(a, store).ServeHTTP(w, r)
			if w.Code < 400 || g.reads != 0 || g.checks != 0 {
				t.Fatal("invalid access read public fallback", w.Code)
			}
		})
	}
}
func TestCollectionPublicationHTTPCancelledWriteHasNoReceipt(t *testing.T) {
	token, _, _ := identity.NewToken()
	a := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &collectionPublicationHTTPSpy{after: cancel}
	r := httptest.NewRequest("POST", "/v1/me/personal-collections/journey/"+imageMomentID+"/publication", strings.NewReader(collectionPublicationBody)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
	if w.Code < 400 || w.Body.Len() != 0 || g.calls != 1 {
		t.Fatal("cancelled write exposed receipt", w.Code)
	}
}

const collectionPublicationCloseBody = `{"kind":"journey","id":"11111111-1111-4111-8111-111111111111","action":"PUBLISH","revision":1}`
const collectionPublicationClosePath = "/v1/me/personal-collections/publication-operations/22222222-2222-4222-8222-222222222222/close"

func TestCollectionPublicationHTTPCloseExplicitSelectorsAndTerminalReceipt(t *testing.T) {
	token, _, _ := identity.NewToken()
	a := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	for _, outcome := range []string{"NO_EFFECT", "COMMITTED"} {
		t.Run(outcome, func(t *testing.T) {
			g := &collectionPublicationHTTPSpy{terminal: outcome}
			r := httptest.NewRequest("POST", collectionPublicationClosePath, strings.NewReader(collectionPublicationCloseBody))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
			if w.Code != 200 || g.calls != 1 || g.action != "CLOSE" || g.actor != a.actor || g.digest != a.seen || g.closeInput.ID != "11111111-1111-4111-8111-111111111111" || g.closeInput.Revision != 1 {
				t.Fatal("close changed original selectors or trusted actor", w.Code, w.Body.String(), g.closeInput)
			}
			var dto struct {
				Data struct {
					PublicationOperation map[string]any `json:"publicationOperation"`
				} `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &dto) != nil || dto.Data.PublicationOperation["outcome"] != outcome {
				t.Fatal("terminal missing", w.Body.String())
			}
			if _, exists := dto.Data.PublicationOperation["resultRevision"]; exists != (outcome == "COMMITTED") {
				t.Fatal("close invented committed result", w.Body.String())
			}
			g.calls = 0
			r = httptest.NewRequest("GET", strings.TrimSuffix(collectionPublicationClosePath, "close")+"receipt", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w = httptest.NewRecorder()
			collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
			if w.Code != 200 || g.calls != 1 || g.action != "RECOVER" || !strings.Contains(w.Body.String(), `"outcome":"`+outcome+`"`) {
				t.Fatal("GET did not recover terminal outcome", w.Code, w.Body.String())
			}
		})
	}
}

func TestCollectionPublicationHTTPCloseRejectsAdditionalApprovalAndInvalidScope(t *testing.T) {
	token, _, _ := identity.NewToken()
	for _, test := range []string{"duplicate", "unknown", "null", "missing", "oversized", "query", "workspace", "invalid-op", "legacy", "typed-nil", "broken-auth", "organization", "content-type"} {
		t.Run(test, func(t *testing.T) {
			g := &collectionPublicationHTTPSpy{}
			a := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
			body, path := collectionPublicationCloseBody, collectionPublicationClosePath
			switch test {
			case "duplicate":
				body = strings.Replace(body, `"revision":1`, `"revision":1,"revision":2`, 1)
			case "unknown":
				body = strings.Replace(body, `"revision":1`, `"revision":1,"entries":[]`, 1)
			case "null":
				body = strings.Replace(body, `"action":"PUBLISH"`, `"action":null`, 1)
			case "missing":
				body = `{}`
			case "oversized":
				body = strings.Repeat(" ", 4097) + body
			case "query":
				path += "?ownerId=old"
			case "invalid-op":
				path = strings.Replace(path, "22222222-2222-4222-8222-222222222222", "broken", 1)
			case "organization":
				a.actor.AccountType = "organization"
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			if test == "broken-auth" {
				r.Header.Set("Authorization", "broken")
			}
			if test == "workspace" {
				r.Header.Set("X-Birdtie-Organization-Workspace", imageMomentID)
			}
			if test == "content-type" {
				r.Header.Set("Content-Type", "text/plain")
			}
			var store content.MomentStore = g
			if test == "legacy" {
				store = &humanMomentLegacySpy{}
			}
			if test == "typed-nil" {
				var missing *collectionPublicationHTTPSpy
				store = missing
			}
			w := httptest.NewRecorder()
			collectionPublicationHTTPHandler(a, store).ServeHTTP(w, r)
			if w.Code < 400 || g.calls != 0 {
				t.Fatal("invalid close reached native selector action", w.Code, w.Body.String())
			}
		})
	}
}

func TestCollectionPublicationHTTPCloseFailureDoesNotClearUnknown(t *testing.T) {
	token, _, _ := identity.NewToken()
	a := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	for _, c := range []struct {
		err    error
		status int
	}{{content.ErrConflict, 409}, {content.ErrNotFound, 404}, {identity.ErrUnauthorized, 401}, {content.ErrUnavailable, 503}} {
		g := &collectionPublicationHTTPSpy{closeError: c.err}
		r := httptest.NewRequest("POST", collectionPublicationClosePath, strings.NewReader(collectionPublicationCloseBody))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
		if w.Code != c.status || strings.Contains(w.Body.String(), "publicationOperation") {
			t.Fatal("failure emitted terminal authority", w.Code, w.Body.String())
		}
	}
	// A GET 404 remains a missing receipt; it does not invoke close.
	g := &collectionPublicationHTTPSpy{closeError: content.ErrNotFound}
	r := httptest.NewRequest("GET", strings.TrimSuffix(collectionPublicationClosePath, "close")+"receipt", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
	if w.Code != 404 || g.calls != 1 || g.action != "RECOVER" || strings.Contains(w.Body.String(), "NO_EFFECT") {
		t.Fatal("GET404 implied no effect", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g = &collectionPublicationHTTPSpy{after: cancel}
	r = httptest.NewRequest("POST", collectionPublicationClosePath, strings.NewReader(collectionPublicationCloseBody)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	collectionPublicationHTTPHandler(a, g).ServeHTTP(w, r)
	if w.Body.Len() != 0 || g.calls != 1 {
		t.Fatal("retired response emitted a terminal receipt", w.Body.String())
	}
}
