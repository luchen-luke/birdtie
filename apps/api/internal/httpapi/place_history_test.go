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

type placeHistoryHTTPSpy struct {
	foundation.PublicCatalog
	calls   int
	access  ph.Access
	summary ph.Summary
}

func (p *placeHistoryHTTPSpy) GetPublicPlaceSocialHistory(_ context.Context, a ph.Access, id string) (ph.Summary, error) {
	p.calls++
	p.access = a
	return p.summary, nil
}
func TestPlaceHistoryHTTPPublicBoundary(t *testing.T) {
	for _, mode := range []string{"anonymous", "signed", "bad_session", "workspace", "query", "unknown_reply", "missing_port", "missing_access"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, token := privateProfileHTTPFixture(t)
			now := time.Now().UTC()
			p := &placeHistoryHTTPSpy{summary: ph.Summary{SchemaVersion: ph.SchemaVersion, PlaceID: publicationHTTPPlace, CityID: "test", WindowDays: 30, WindowStart: now.Add(-30 * 24 * time.Hour), CheckedAt: now, RecentMoments: []ph.Moment{}, ActivityPatterns: []ph.ActivityPattern{}}}
			s.catalog = p
			path := "/v1/places/" + publicationHTTPPlace + "/social-history"
			want := 200
			switch mode {
			case "anonymous":
				token = ""
			case "bad_session":
				token = "bad"
				want = 401
			case "workspace":
				want = 400
			case "query":
				path += "?private=true"
				want = 400
			case "unknown_reply":
				p.summary.PlaceID = publicationHTTPMoment
				want = 503
			case "missing_port":
				s.catalog = struct{ foundation.PublicCatalog }{}
				want = 503
			case "missing_access":
				s.access = nil
				want = 503
			}
			r := privateProfileHTTPRequest("GET", path, "", token, "")
			r.SetPathValue("placeID", publicationHTTPPlace)
			if mode == "workspace" {
				r.Header.Set("X-Birdtie-Organization-Workspace", publicationHTTPPlace)
			}
			w := httptest.NewRecorder()
			s.getPublicPlaceSocialHistory(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private") {
				t.Fatal("public access transport", mode, w.Code, w.Body.String())
			}
			if mode == "anonymous" && p.access.Actor.ID != "" {
				t.Fatal("anonymous inherited Person")
			}
			if mode == "signed" && p.access.Actor.ID != privateProfileHTTPOwner {
				t.Fatal("real digest not forwarded")
			}
			if mode == "bad_session" && p.calls != 0 {
				t.Fatal("invalid session downgraded anonymous")
			}
		})
	}
}
