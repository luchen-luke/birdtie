package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
)

const savedNoteHTTPID = "11111111-1111-4111-8111-111111111111"
const savedNoteHTTPOp = "22222222-2222-4222-8222-222222222222"
const savedNoteHTTPBody = `{"note":"  私人备注  ","expectedRevision":0,"operationId":"22222222-2222-4222-8222-222222222222"}`

type savedNoteHTTPAccess struct {
	identity.AccessStore
	actor     identity.Actor
	seen      [32]byte
	final     error
	validated bool
}

func (a *savedNoteHTTPAccess) AuthenticateHumanSocial(_ context.Context, d [32]byte) (identity.Actor, error) {
	a.seen = d
	return a.actor, nil
}
func (a *savedNoteHTTPAccess) ValidateHumanSocialResponse(_ context.Context, d [32]byte, p identity.Actor) error {
	a.validated = true
	if d != a.seen || p != a.actor {
		return identity.ErrUnauthorized
	}
	return a.final
}

type savedNoteHTTPStore struct {
	saved.Store
	calls  int
	actor  identity.Actor
	digest [32]byte
	input  saved.NoteInput
	err    error
	after  func()
}

func (p *savedNoteHTTPStore) hit(d [32]byte, a identity.Actor) {
	p.calls++
	p.actor = a
	p.digest = d
	if p.after != nil {
		p.after()
	}
}
func (p *savedNoteHTTPStore) GetHumanSavedNote(_ context.Context, d [32]byte, a identity.Actor, id string) (saved.Note, error) {
	p.hit(d, a)
	return saved.Note{SavedID: id, Note: "本人私注", Revision: 3}, p.err
}
func (p *savedNoteHTTPStore) UpdateHumanSavedNote(_ context.Context, d [32]byte, a identity.Actor, id string, in saved.NoteInput) (saved.NoteOperation, error) {
	p.hit(d, a)
	p.input = in
	return saved.NoteOperation{SavedID: id, OperationID: in.OperationID, Revision: 1, Status: "SUCCEEDED"}, p.err
}
func (p *savedNoteHTTPStore) GetHumanSavedNoteOperation(_ context.Context, d [32]byte, a identity.Actor, id, op string) (saved.NoteOperation, error) {
	p.hit(d, a)
	return saved.NoteOperation{SavedID: id, OperationID: op, Revision: 1, Status: "SUCCEEDED"}, p.err
}

func savedNoteHTTPHandler(a identity.AccessStore, p saved.Store) http.Handler {
	s := &server{access: a, saved: p}
	mux := http.NewServeMux()
	s.registerSavedNoteRoutes(mux)
	return mux
}
func savedNoteHTTPRequest(t *testing.T, method, path, body string) (*http.Request, *savedNoteHTTPAccess) {
	t.Helper()
	token, _, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r, &savedNoteHTTPAccess{actor: identity.Actor{ID: savedNoteHTTPID, AccountType: "person"}}
}

func TestPrivateSavedNoteHTTPCurrentActorReceiptsAndNoFallback(t *testing.T) {
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/v1/me/saved/" + savedNoteHTTPID + "/note", ""},
		{"PUT", "/v1/me/saved/" + savedNoteHTTPID + "/note", savedNoteHTTPBody},
		{"GET", "/v1/me/saved/" + savedNoteHTTPID + "/note/operations/" + savedNoteHTTPOp, ""},
	} {
		r, a := savedNoteHTTPRequest(t, c.method, c.path, c.body)
		p := &savedNoteHTTPStore{}
		w := httptest.NewRecorder()
		savedNoteHTTPHandler(a, p).ServeHTTP(w, r)
		if w.Code != 200 || p.calls != 1 || p.actor != a.actor || p.digest != a.seen || !a.validated || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("current Session/actor/receipt contract", c.method, w.Code, w.Body.String())
		}
		if c.method == "PUT" && (p.input.Note != "私人备注" || !strings.Contains(w.Body.String(), `"status":"SUCCEEDED"`) || strings.Contains(w.Body.String(), "私人备注")) {
			t.Fatal("receipt copied private body or skipped normalization", w.Body.String())
		}
		if strings.Contains(c.path, "operations") && strings.Contains(w.Body.String(), "本人私注") {
			t.Fatal("recovery exposed current note")
		}
	}
	r, a := savedNoteHTTPRequest(t, "GET", "/v1/me/saved/"+savedNoteHTTPID+"/note", "")
	w := httptest.NewRecorder()
	savedNoteHTTPHandler(a, struct{ saved.Store }{}).ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("legacy note fallback", w.Code)
	}
}

func TestPrivateSavedNoteHTTPStrictInputAndPrincipal(t *testing.T) {
	for _, body := range []string{
		`{"note":"x","operationId":"22222222-2222-4222-8222-222222222222"}`,
		`{"expectedRevision":0,"operationId":"22222222-2222-4222-8222-222222222222"}`,
		`{"note":"x","expectedRevision":0}`,
		`{"note":"x","expectedRevision":-1,"operationId":"22222222-2222-4222-8222-222222222222"}`,
		`{"note":"x","expectedRevision":9007199254740991,"operationId":"22222222-2222-4222-8222-222222222222"}`,
		`{"note":"x","expectedRevision":0,"operationId":"22222222-2222-4222-8222-222222222222","owner":"other"}`,
		`{"note":"x","expectedRevision":0,"operationId":"22222222-2222-4222-8222-222222222222"} {}`,
	} {
		r, a := savedNoteHTTPRequest(t, "PUT", "/v1/me/saved/"+savedNoteHTTPID+"/note", body)
		p := &savedNoteHTTPStore{}
		w := httptest.NewRecorder()
		savedNoteHTTPHandler(a, p).ServeHTTP(w, r)
		if w.Code != 400 || p.calls != 0 {
			t.Fatal("bad request reached writer", w.Code)
		}
	}
	for _, mode := range []string{"organization", "business", "query", "bad_id", "duplicate_authorization"} {
		r, a := savedNoteHTTPRequest(t, "GET", "/v1/me/saved/"+savedNoteHTTPID+"/note", "")
		want := 400
		switch mode {
		case "organization":
			r.Header.Set("X-Birdtie-Organization-Workspace", savedNoteHTTPID)
			want = 403
		case "business":
			a.actor.AccountType = "business"
			want = 403
		case "query":
			r.URL.RawQuery = "owner=other"
		case "bad_id":
			r.URL.Path = "/v1/me/saved/invalid/note"
		case "duplicate_authorization":
			r.Header.Add("Authorization", "Bearer duplicate")
			want = 401
		}
		p := &savedNoteHTTPStore{}
		w := httptest.NewRecorder()
		savedNoteHTTPHandler(a, p).ServeHTTP(w, r)
		if w.Code != want || p.calls != 0 {
			t.Fatal(mode, w.Code, p.calls)
		}
	}
}

func TestPrivateSavedNoteHTTPFinalRevocationAndBoundedFailures(t *testing.T) {
	for _, c := range []struct {
		e      error
		status int
	}{
		{saved.ErrNotFound, 404}, {saved.ErrNoteChanged, 409}, {saved.ErrInvalidNote, 400}, {saved.ErrNoteUnavailable, 503}, {identity.ErrUnauthorized, 401}, {errors.New("internal unknown"), 503},
	} {
		r, a := savedNoteHTTPRequest(t, "PUT", "/v1/me/saved/"+savedNoteHTTPID+"/note", savedNoteHTTPBody)
		p := &savedNoteHTTPStore{err: c.e}
		w := httptest.NewRecorder()
		savedNoteHTTPHandler(a, p).ServeHTTP(w, r)
		if w.Code != c.status || strings.Contains(w.Body.String(), "SUCCEEDED") {
			t.Fatal("error became success", w.Code)
		}
	}
	for _, method := range []string{"GET", "PUT"} {
		body := ""
		if method == "PUT" {
			body = savedNoteHTTPBody
		}
		r, a := savedNoteHTTPRequest(t, method, "/v1/me/saved/"+savedNoteHTTPID+"/note", body)
		p := &savedNoteHTTPStore{after: func() { a.final = identity.ErrUnauthorized }}
		w := httptest.NewRecorder()
		savedNoteHTTPHandler(a, p).ServeHTTP(w, r)
		if w.Code != 401 || strings.Contains(w.Body.String(), "本人私注") || strings.Contains(w.Body.String(), "SUCCEEDED") {
			t.Fatal("late revoked response disclosed note or claimed write", w.Code)
		}
	}
}
