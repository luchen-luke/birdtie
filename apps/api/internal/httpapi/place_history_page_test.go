package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pageHTTPSpy struct {
	foundation.PublicCatalog
	calls, validations int
	fail               error
	out                ph.Page
}

func (p *pageHTTPSpy) ReadPublicPlaceHistoryPage(_ context.Context, _ ph.Access, q ph.PageQuery) (ph.Page, error) {
	p.calls++
	p.out.PlaceID = q.PlaceID
	p.out.AuthorMode = "NONE"
	if q.WithAuthor {
		p.out.AuthorMode = "PUBLIC_PROFILE"
	}
	return p.out, nil
}
func (p *pageHTTPSpy) RevalidatePublicPlaceHistoryPage(_ context.Context, _ ph.Access, _ ph.PageQuery, _ ph.Page) error {
	p.validations++
	return p.fail
}
func TestPlaceHistoryPageHTTPStrictQueryAndFinalFence(t *testing.T) {
	for _, mode := range []string{"valid", "author", "changed", "missing_port", "unknown", "duplicate", "empty_cursor", "body", "bad_session"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, token := privateProfileHTTPFixture(t)
			now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			spy := &pageHTTPSpy{out: ph.Page{SchemaVersion: ph.PageSchemaVersion, CityID: "test", WindowDays: 30, WindowEnd: now, WindowStart: now.Add(-30 * 24 * time.Hour), ObservedAt: now, ValidUntil: now.Add(time.Minute), SourceVersion: strings.Repeat("a", 64), Items: []ph.MomentWithAuthor{}}}
			s.catalog = spy
			path := "/v1/places/" + publicationHTTPPlace + "/social-history/moments"
			body := ""
			want := 200
			switch mode {
			case "author":
				path += "?include=public-author"
			case "changed":
				spy.fail = ph.ErrChanged
				want = 409
			case "missing_port":
				s.catalog = struct{ foundation.PublicCatalog }{}
				want = 503
			case "unknown":
				path += "?owner=hidden"
				want = 400
			case "duplicate":
				path += "?include=public-author&include=public-author"
				want = 400
			case "empty_cursor":
				path += "?cursor="
				want = 400
			case "body":
				body = "x"
				want = 400
			case "bad_session":
				token = "invalid"
				want = 401
			}
			r := privateProfileHTTPRequest("GET", path, body, token, "")
			r.SetPathValue("placeID", publicationHTTPPlace)
			w := httptest.NewRecorder()
			s.getPublicPlaceHistoryMomentsPage(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(mode, w.Code, w.Body.String())
			}
			if want == 200 && (spy.calls != 1 || spy.validations != 1) {
				t.Fatal("typed proof omitted")
			}
			if want != 200 && strings.Contains(w.Body.String(), "sourceVersion") {
				t.Fatal("failed response exposed page")
			}
		})
	}
}
