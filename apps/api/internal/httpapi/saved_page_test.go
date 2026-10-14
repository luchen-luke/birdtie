package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
)

type savedPageHTTPSpy struct {
	saved.Store
	page   saved.Page
	calls  int
	actor  identity.Actor
	cursor string
	digest [32]byte
	err    error
	target *saved.Item
}

func (p *savedPageHTTPSpy) GetHumanSavedTarget(_ context.Context, d [32]byte, a identity.Actor, kind, id string) (*saved.Item, error) {
	p.calls++
	p.actor = a
	p.digest = d
	return p.target, p.err
}

func (p *savedPageHTTPSpy) ListHumanSavedPage(_ context.Context, d [32]byte, a identity.Actor, cursor string) (saved.Page, error) {
	p.calls++
	p.actor = a
	p.digest = d
	p.cursor = cursor
	return p.page, p.err
}

type savedPageHTTPAccess struct {
	identity.AccessStore
	actor    identity.Actor
	finalErr error
	final    int
}

func (p *savedPageHTTPAccess) AuthenticateHumanSocial(_ context.Context, _ [32]byte) (identity.Actor, error) {
	return p.actor, nil
}
func (p *savedPageHTTPAccess) ValidateHumanSocialResponse(_ context.Context, _ [32]byte, _ identity.Actor) error {
	p.final++
	return p.finalErr
}
func savedPageHTTP(t *testing.T) (http.Handler, *savedPageHTTPSpy, *savedPageHTTPAccess, string) {
	t.Helper()
	token, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	access := &savedPageHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}}
	store := &savedPageHTTPSpy{page: saved.Page{Items: []saved.Item{{ID: humanHTTPOp, TargetID: privateProfileHTTPAgent, Kind: "place", Title: "当前可见合成收藏", Note: "只本人可见的备注", Available: true}}}}
	return New(nil, access, nil, nil, nil, nil, nil, nil, store, nil, nil, nil, false, nil, nil, nil), store, access, token
}
func TestSavedPageRegisteredClosedQueryAndCurrentResponse(t *testing.T) {
	h, p, a, token := savedPageHTTP(t)
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page", "", token, nil)
	if w.Code != 200 || p.calls != 1 || a.final != 1 || p.actor != a.actor || p.digest == ([32]byte{}) || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "只本人可见的备注") {
		t.Fatal("current human projection", w.Code)
	}
	for _, query := range []string{"owner=other", "cursor=x", "cursor=x&cursor=x", "cursor=00000000-0000-0000-0000-000000000000", "cursor=;bad", "limit=500"} {
		h, p, _, token = savedPageHTTP(t)
		w = messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page?"+query, "", token, nil)
		if w.Code != 400 || p.calls != 0 {
			t.Fatal("unknown query entered source", query, w.Code)
		}
	}
}
func TestSavedPageFinalSessionDenialMalformedAndNoLegacyFallback(t *testing.T) {
	h, p, a, token := savedPageHTTP(t)
	a.finalErr = identity.ErrUnauthorized
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page", "", token, nil)
	if w.Code != 401 || a.final != 1 || strings.Contains(w.Body.String(), "备注") {
		t.Fatal("retired session leaked", w.Code)
	}
	h, p, a, token = savedPageHTTP(t)
	p.page.Items = append(p.page.Items, p.page.Items[0])
	w = messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page", "", token, nil)
	if w.Code != 503 || a.final != 0 {
		t.Fatal("duplicate source page", w.Code)
	}
	h, p, _, token = savedPageHTTP(t)
	p.err = saved.ErrPageChanged
	w = messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page", "", token, nil)
	if w.Code != 409 || p.calls != 1 {
		t.Fatal("changed cursor", w.Code)
	}
	h, _, _, token = savedPageHTTP(t)
	w = messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page", "", token, func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPAgent) })
	if w.Code != 403 {
		t.Fatal("organization entered private bookmarks")
	}
	w = messagePolicyUnitRequest(h, "GET", "/v1/me/saved/page", "", "", nil)
	if w.Code != 401 {
		t.Fatal("anonymous private read")
	}
}

func TestSavedTargetOriginalBookmarkLookupIsHumanOnly(t *testing.T) {
	h, p, a, token := savedPageHTTP(t)
	p.target = &p.page.Items[0]
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/saved/target/place/"+privateProfileHTTPAgent, "", token, nil)
	if w.Code != 200 || p.calls != 1 || a.final != 1 || !strings.Contains(w.Body.String(), humanHTTPOp) {
		t.Fatal("target lookup failed", w.Code)
	}
	h, p, _, token = savedPageHTTP(t)
	w = messagePolicyUnitRequest(h, "GET", "/v1/me/saved/target/place/"+privateProfileHTTPAgent, "", token, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":null`) {
		t.Fatal("unsaved target is not empty", w.Code)
	}
	for _, path := range []string{"/v1/me/saved/target/business/" + privateProfileHTTPAgent, "/v1/me/saved/target/place/x", "/v1/me/saved/target/place/" + privateProfileHTTPAgent + "?owner=x"} {
		h, p, _, token = savedPageHTTP(t)
		w = messagePolicyUnitRequest(h, "GET", path, "", token, nil)
		if w.Code != 400 || p.calls != 0 {
			t.Fatal("invalid target lookup entered domain", w.Code)
		}
	}
}
