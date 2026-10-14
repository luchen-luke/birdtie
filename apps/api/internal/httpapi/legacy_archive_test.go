package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
)

// These are closed wiring spies, not a native Session, imported owner, media
// rights or an assertion that real Civu login/restore has passed.
type legacyArchiveHTTPProof struct {
	err   error
	calls int
}

func (p *legacyArchiveHTTPProof) RevalidateHumanControl(context.Context) error {
	p.calls++
	return p.err
}

type legacyArchiveHTTPSpy struct {
	calls, finalized int
	digest           [32]byte
	actor            identity.Actor
	page             legacyarchive.Page
	detail           legacyarchive.Detail
	err              error
	finalErr         error
}

func (p *legacyArchiveHTTPSpy) ListHumanLegacyArchives(_ context.Context, d [32]byte, a identity.Actor, _ string) (legacyarchive.Page, error) {
	p.calls++
	p.digest = d
	p.actor = a
	return p.page, p.err
}
func (p *legacyArchiveHTTPSpy) ReadHumanLegacyArchive(_ context.Context, d [32]byte, a identity.Actor, _ string) (legacyarchive.Detail, error) {
	p.calls++
	p.digest = d
	p.actor = a
	return p.detail, p.err
}
func (p *legacyArchiveHTTPSpy) FinalizeHumanLegacyArchiveRead(context.Context, [32]byte, identity.Actor, legacyarchive.Detail) error {
	p.finalized++
	return p.finalErr
}
func (p *legacyArchiveHTTPSpy) FinalizeHumanLegacyArchivePage(context.Context, [32]byte, identity.Actor, legacyarchive.Page) error {
	p.finalized++
	return p.finalErr
}

func legacyArchiveHTTPFixture(t *testing.T) (http.Handler, *legacyArchiveHTTPSpy, *humanMomentAccessSpy, string) {
	t.Helper()
	token, _, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	a := &humanMomentAccessSpy{actor: identity.Actor{ID: "11111111-1111-4111-8111-111111111111", AccountType: "person"}}
	proof := &legacyArchiveHTTPProof{}
	summary := legacyarchive.Summary{ID: "22222222-2222-4222-8222-222222222222", Source: legacyarchive.SourceSelector{System: legacyarchive.SourceSystem, Kind: "note", ID: "33333333-3333-4333-8333-333333333333"}, SourceVersion: 7, SourceSHA256: strings.Repeat("a", 64), ImportedAt: time.Now().UTC(), SourceVisibility: "public", SourceModerationStatus: "approved"}
	spy := &legacyArchiveHTTPSpy{page: legacyarchive.Page{Items: []legacyarchive.Summary{summary}, ResponseProof: proof}, detail: legacyarchive.Detail{Summary: summary, Body: "LEGACY_SYNTHETIC_PRIVATE_BODY", Relationships: &legacyarchive.Relationships{Places: []legacyarchive.PlaceSelector{}, Media: []legacyarchive.MediaSelector{}, MediaLocations: []legacyarchive.MediaLocation{}, ThreadLegs: []legacyarchive.ThreadLeg{}}}}
	spy.detail.ResponseProof = proof
	h := New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, func(s *server) { s.legacyArchives = spy })
	return h, spy, a, token
}

func legacyArchiveHTTPCall(h http.Handler, token, path, body, org string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if org != "" {
		r.Header.Set("X-Birdtie-Organization-Workspace", org)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLegacyArchiveHTTPUsesOriginalPersonSessionAndNeverRawSource(t *testing.T) {
	for _, detail := range []bool{false, true} {
		h, p, a, token := legacyArchiveHTTPFixture(t)
		path := "/v1/me/migration-archive?kind=note"
		if detail {
			path = "/v1/me/migration-archive/" + p.detail.ID
		}
		w := legacyArchiveHTTPCall(h, token, path, "", "")
		if w.Code != 200 || p.calls != 1 || p.finalized != 1 || p.digest != a.seen || p.actor != a.actor || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("original person/session wiring", w.Code, w.Body.String())
		}
		for _, secret := range []string{"ResponseProof", "password_hash", "auth_identities", "public_url", "rawSource"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("raw importer data reached response")
			}
		}
		if detail && !strings.Contains(w.Body.String(), p.detail.Body) {
			t.Fatal("literal original body not preserved")
		}
	}
}

func TestLegacyArchiveHTTPRejectsForeignSelectorsOrgAndWrites(t *testing.T) {
	for _, in := range []struct{ path, body, org string }{
		{"/v1/me/migration-archive?ownerId=other", "", ""},
		{"/v1/me/migration-archive?cursor=a&cursor=b", "", ""},
		{"/v1/me/migration-archive?cursor=%zz", "", ""},
		{"/v1/me/migration-archive?kind=person", "", ""},
		{"/v1/me/migration-archive?", "", ""},
		{"/v1/me/migration-archive", "{}", ""},
		{"/v1/me/migration-archive", "", "organization"},
		{"/v1/me/migration-archive/bad-id", "", ""},
	} {
		h, p, _, token := legacyArchiveHTTPFixture(t)
		w := legacyArchiveHTTPCall(h, token, in.path, in.body, in.org)
		if w.Code != 400 && w.Code != 403 {
			t.Fatalf("selector not rejected: %d", w.Code)
		}
		if p.calls != 0 {
			t.Fatal("client authority reached reader")
		}
	}
	h, p, _, token := legacyArchiveHTTPFixture(t)
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		r := httptest.NewRequest(method, "/v1/me/migration-archive", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 405 || p.calls != 0 {
			t.Fatal("archive mutation endpoint was created")
		}
	}
}

func TestLegacyArchiveHTTPLateProofAndMalformedResponseLeakZeroBody(t *testing.T) {
	for _, mode := range []string{"proof", "final", "nil-proof", "wrong-id", "deleted-body"} {
		h, p, _, token := legacyArchiveHTTPFixture(t)
		switch mode {
		case "proof":
			p.detail.ResponseProof = &legacyArchiveHTTPProof{err: identity.ErrControlChanged}
		case "final":
			p.finalErr = identity.ErrControlChanged
		case "nil-proof":
			p.detail.ResponseProof = nil
		case "wrong-id":
			p.detail.Source.System = "untrusted"
		case "deleted-body":
			p.detail.Deleted = true
		}
		w := legacyArchiveHTTPCall(h, token, "/v1/me/migration-archive/"+p.detail.ID, "", "")
		if w.Code != 409 && w.Code != 503 {
			t.Fatalf("late/malformed result not denied: %s %d", mode, w.Code)
		}
		if strings.Contains(w.Body.String(), "LEGACY_SYNTHETIC_PRIVATE_BODY") {
			t.Fatal("late private body leaked")
		}
	}
	h, p, _, token := legacyArchiveHTTPFixture(t)
	p.page.Items = append(p.page.Items, p.page.Items[0])
	w := legacyArchiveHTTPCall(h, token, "/v1/me/migration-archive", "", "")
	if w.Code != 503 {
		t.Fatal("duplicate stable ID accepted")
	}
}

type legacyArchiveTailAccess struct {
	*humanMomentAccessSpy
	proof *legacyArchiveHTTPProof
}

func (a *legacyArchiveTailAccess) ValidateHumanControlSession(ctx context.Context, d [32]byte, actor identity.Actor, frame identity.HumanControlFrame) error {
	if e := a.humanMomentAccessSpy.ValidateHumanControlSession(ctx, d, actor, frame); e != nil {
		return e
	}
	// Simulate an archive source retiring during the final generic Session
	// check. The actual encoded source proof must run after this check.
	a.proof.err = identity.ErrControlChanged
	return nil
}

func TestLegacyArchiveHTTPSourceProofRunsAfterWaitingSessionTail(t *testing.T) {
	_, p, access, token := legacyArchiveHTTPFixture(t)
	proof := p.detail.ResponseProof.(*legacyArchiveHTTPProof)
	tail := &legacyArchiveTailAccess{humanMomentAccessSpy: access, proof: proof}
	h := New(nil, tail, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, func(s *server) { s.legacyArchives = p })
	w := legacyArchiveHTTPCall(h, token, "/v1/me/migration-archive/"+p.detail.ID, "", "")
	if w.Code != 409 || proof.calls != 1 || strings.Contains(w.Body.String(), p.detail.Body) {
		t.Fatal("retired source survived final waiting Session check", w.Code)
	}
}
