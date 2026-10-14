package httpapi

import (
	"context"
	cc "github.com/birdtie/birdtie/apps/api/internal/contactcode"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type contactCodeHTTPSpy struct {
	*peopleSearchSpy
	calls, final int
	fail         error
	view         cc.View
}

func (p *contactCodeHTTPSpy) ReadOwnContactCode(context.Context, identity.ProfileAccess) (cc.View, error) {
	p.calls++
	return p.view, nil
}
func (p *contactCodeHTTPSpy) ChangeOwnContactCode(_ context.Context, a identity.ProfileAccess, in cc.Input, revoke bool) (cc.View, error) {
	p.calls++
	if a.Actor.ID != p.actor.ID || a.SessionDigest != p.digest {
		return cc.View{}, identity.ErrUnauthorized
	}
	return p.view, nil
}
func (p *contactCodeHTTPSpy) ValidateOwnContactCode(context.Context, identity.ProfileAccess, cc.View) error {
	p.final++
	return p.fail
}
func (p *contactCodeHTTPSpy) ResolveContactCode(context.Context, identity.ProfileAccess, string) (cc.Resolution, error) {
	p.calls++
	return cc.Resolution{AccountID: "22222222-2222-4222-8222-222222222222", Proof: "SERVER_ONLY"}, nil
}
func (p *contactCodeHTTPSpy) ValidateContactResolution(context.Context, identity.ProfileAccess, string, cc.Resolution) error {
	p.final++
	return p.fail
}
func TestContactCodeRegisteredPersonalClosedWireAndFinalDenial(t *testing.T) {
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	id := "33333333-3333-4333-8333-333333333333"
	expiry := time.Now().UTC().Add(cc.Lifetime)
	for _, tc := range []struct {
		method, path, body, workspace string
		anonymous                     bool
		fail                          error
		want, calls                   int
	}{
		{method: "GET", path: "/v1/contact-code", want: 200, calls: 1},
		{method: "POST", path: "/v1/contact-code", body: `{"operationId":"33333333-3333-4333-8333-333333333333","expectedVersion":0}`, want: 200, calls: 1},
		{method: "DELETE", path: "/v1/contact-code", body: `{"operationId":"33333333-3333-4333-8333-333333333333","expectedVersion":0}`, want: 200, calls: 1},
		{method: "POST", path: "/v1/contact-code/resolve", body: `{"codeId":"33333333-3333-4333-8333-333333333333"}`, want: 200, calls: 1},
		{method: "POST", path: "/v1/contact-code/resolve", body: `{"codeId":"33333333-3333-4333-8333-333333333333"}`, fail: cc.ErrNotFound, want: 404, calls: 1},
		{method: "GET", path: "/v1/contact-code", fail: identity.ErrUnauthorized, want: 401, calls: 1},
		{method: "GET", path: "/v1/contact-code?owner=x", want: 400},
		{method: "GET", path: "/v1/contact-code", workspace: "", anonymous: true, want: 401},
		{method: "GET", path: "/v1/contact-code", workspace: "org", want: 403},
		{method: "POST", path: "/v1/contact-code", body: `{"operationId":"33333333-3333-4333-8333-333333333333","expectedVersion":null}`, want: 400},
		{method: "POST", path: "/v1/contact-code/resolve", body: `{"codeId":"33333333-3333-4333-8333-333333333333","codeId":"33333333-3333-4333-8333-333333333333"}`, want: 400},
		{method: "POST", path: "/v1/contact-code/resolve", body: `{"codeId":"33333333-3333-4333-8333-333333333333","owner":"x"}`, want: 400},
	} {
		p := &contactCodeHTTPSpy{peopleSearchSpy: &peopleSearchSpy{actor: identity.Actor{ID: "11111111-1111-4111-8111-111111111111", AccountType: "person"}, digest: d}, fail: tc.fail, view: cc.View{Version: 1, CodeID: &id, ExpiresAt: &expiry}}
		h := New(nil, p, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if !tc.anonymous {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if tc.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if tc.workspace != "" {
			r.Header.Set("X-Birdtie-Organization-Workspace", tc.workspace)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want || p.calls != tc.calls || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s status%d calls%d body%s", tc.method, tc.path, w.Code, p.calls, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "SERVER_ONLY") || tc.want != 200 && strings.Contains(w.Body.String(), id) {
			t.Fatal("proof or denied payload leaked")
		}
	}
}

var _ http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
