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

type mapThreadHTTPSpy struct {
	*mapDisplayHTTPSpy
	reads      int
	hiddenText bool
}

func (p *mapThreadHTTPSpy) ReadHumanMapContentThread(_ context.Context, _ [32]byte, a identity.Actor, id string) (content.MapContentThread, error) {
	p.reads++
	m := p.moment(id, true)
	v := content.MapContentThread{SchemaVersion: content.MapContentThreadSchema, ViewerAccountID: a.ID, SeedMomentID: id, SeedRevision: m.Revision, SeedVisibilitySource: m.VisibilitySource, Status: "AVAILABLE", ThreadID: mapDisplayHTTPPlace, Entries: []content.MapContentThreadEntry{{Position: 0, Status: "AVAILABLE", Detail: &m}, {Position: 1, Status: "UNAVAILABLE"}}, ObservedAt: m.ObservedAt, ValidUntil: m.ValidUntil, SourceVersion: strings.Repeat("b", 64), ResponseProof: p.currentProof()}
	if p.hiddenText {
		v.Entries[1].Detail = &m
	}
	return v, p.readErr
}
func TestMapContentThreadHTTPAuthorityAndHiddenPositions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		mutate func(*mapThreadHTTPSpy, *http.Request)
	}{
		{"available", 200, func(*mapThreadHTTPSpy, *http.Request) {}},
		{"unknown query", 400, func(_ *mapThreadHTTPSpy, r *http.Request) { r.URL.RawQuery = "other=1" }},
		{"organization", 400, func(_ *mapThreadHTTPSpy, r *http.Request) {
			r.Header.Set("X-Birdtie-Organization-Workspace", "present")
		}},
		{"lost final proof", 409, func(p *mapThreadHTTPSpy, _ *http.Request) { p.proofErr = identity.ErrControlChanged }},
		{"hidden payload", 503, func(p *mapThreadHTTPSpy, _ *http.Request) { p.hiddenText = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &mapThreadHTTPSpy{mapDisplayHTTPSpy: newMapDisplayHTTPSpy(t)}
			s := &server{access: p, content: p}
			mux := http.NewServeMux()
			s.registerMapContentThreadRoutes(mux)
			r := hcRequest("GET", "/v1/me/map-content/moments/"+mapDisplayHTTPMoment+"/thread", "")
			tc.mutate(p, r)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("status=%d expected=%d body=%s", w.Code, tc.code, w.Body.String())
			}
			if tc.code == 200 && (!strings.Contains(w.Body.String(), `"detail":null`) || !strings.Contains(w.Header().Get("Cache-Control"), "no-store")) {
				t.Fatal("hidden position or no-store missing")
			}
			if tc.code != 200 && strings.Contains(w.Body.String(), "原正文") {
				t.Fatal("failed response leaked body")
			}
		})
	}
}
