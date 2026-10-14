package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type peopleSearchSpy struct {
	identity.AccessStore
	actor    identity.Actor
	digest   [32]byte
	calls    int
	finalErr error
}

func (p *peopleSearchSpy) Authenticate(_ context.Context, d [32]byte) (identity.Actor, error) {
	if d != p.digest {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	return p.actor, nil
}
func (p *peopleSearchSpy) SearchPublicPersonIDs(_ context.Context, a identity.ProfileAccess, q string) ([]string, error) {
	p.calls++
	return []string{"22222222-2222-4222-8222-222222222222"}, nil
}
func (p *peopleSearchSpy) ReadProfileCurrent(_ context.Context, a identity.ProfileAccess, id string) (identity.Profile, error) {
	return identity.Profile{AccountID: id, DisplayName: "公开姓名", Visibility: "public", Bio: "PRIVATE_BIO_NOT_DIRECTORY"}, nil
}
func (p *peopleSearchSpy) ValidateProfileCurrent(context.Context, identity.ProfileAccess, string, identity.Profile) error {
	return p.finalErr
}

func TestPeopleSearchAuthenticationAndProjection(t *testing.T) {
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		name, token, query, workspace string
		finalErr                      error
		want, calls                   int
	}{
		{name: "public names only", token: token, query: "?q=公开", want: 200, calls: 1},
		{name: "guest", query: "?q=公开", want: 401},
		{name: "short query", token: token, query: "?q=x", want: 400},
		{name: "duplicate query", token: token, query: "?q=公开&q=公开", want: 400},
		{name: "unknown selector", token: token, query: "?q=公开&owner=x", want: 400},
		{name: "organization", token: token, query: "?q=公开", workspace: "work", want: 403},
		{name: "permission withdrawn", token: token, query: "?q=公开", finalErr: identity.ErrNotFound, want: 404, calls: 1},
		{name: "session expired", token: token, query: "?q=公开", finalErr: identity.ErrUnauthorized, want: 401, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := &peopleSearchSpy{actor: identity.Actor{ID: "11111111-1111-4111-8111-111111111111", AccountType: "person"}, digest: d, finalErr: test.finalErr}
			s := &server{access: port}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/people/search", s.searchPeople)
			r := httptest.NewRequest("GET", "/v1/people/search"+test.query, nil)
			if test.token != "" {
				r.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.workspace != "" {
				r.Header.Set("X-Birdtie-Organization-Workspace", test.workspace)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != test.want || port.calls != test.calls || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d calls %d body %s", w.Code, port.calls, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "PRIVATE_BIO_NOT_DIRECTORY") {
				t.Fatal("directory leaked profile body")
			}
			if test.want == 200 && !strings.Contains(w.Body.String(), "公开姓名") {
				t.Fatal("missing public name")
			}
			if test.want != 200 && strings.Contains(w.Body.String(), "公开姓名") {
				t.Fatal("late withdrawal leaked directory")
			}
		})
	}
}
