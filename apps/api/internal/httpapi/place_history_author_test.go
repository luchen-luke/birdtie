package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type placeHistoryAuthorHTTPSpy struct {
	foundation.PublicCatalog
	legacyCalls, authorCalls int
	out                      ph.SummaryWithAuthor
	access                   ph.Access
	onRead                   func(int) error
}

func (p *placeHistoryAuthorHTTPSpy) GetPublicPlaceSocialHistory(_ context.Context, a ph.Access, id string) (ph.Summary, error) {
	p.legacyCalls++
	p.access = a
	out := p.out.Summary
	out.SchemaVersion = ph.SchemaVersion
	return out, nil
}
func (p *placeHistoryAuthorHTTPSpy) GetPublicPlaceSocialHistoryWithAuthor(_ context.Context, a ph.Access, id string) (ph.SummaryWithAuthor, error) {
	p.authorCalls++
	p.access = a
	if p.onRead != nil {
		if e := p.onRead(p.authorCalls); e != nil {
			return ph.SummaryWithAuthor{}, e
		}
	}
	return p.out, nil
}
func placeHistoryAuthorHTTPFixture() ph.SummaryWithAuthor {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	id := privateProfileHTTPForeign
	m := ph.Moment{ID: publicationHTTPMoment, Title: "公开动态", Excerpt: "公开节选", Revision: 2, PublishedAt: now.Add(-time.Hour)}
	return ph.SummaryWithAuthor{Summary: ph.Summary{SchemaVersion: ph.AuthorSchemaVersion, PlaceID: publicationHTTPPlace, CityID: "test", WindowDays: 30, WindowStart: now.Add(-30 * 24 * time.Hour), CheckedAt: now, RecentMomentCount: 1, RecentMoments: []ph.Moment{m}, ActivityPatterns: []ph.ActivityPattern{}}, RecentMoments: []ph.MomentWithAuthor{{Moment: m, AuthorID: &id, AuthorPublicProfile: &content.PublicMomentAuthor{AccountID: id, DisplayName: "公开作者"}}}, SourceBinding: "native-history-source"}
}
func TestPlaceHistoryAuthorHTTPExplicitOptInAndFinalFence(t *testing.T) {
	for _, mode := range []string{"legacy", "anonymous", "signed", "nullable", "clock_only", "bad_session", "revoke", "changed_epoch", "changed_payload", "mutable_author", "withdrawn", "blocked", "retained_expired", "invalid_final", "missing_v2_port"} {
		t.Run(mode, func(t *testing.T) {
			s, auth, _, token := privateProfileHTTPFixture(t)
			p := &placeHistoryAuthorHTTPSpy{out: placeHistoryAuthorHTTPFixture()}
			s.catalog = p
			path := "/v1/places/" + publicationHTTPPlace + "/social-history?include=public-author"
			want := 200
			switch mode {
			case "legacy":
				path = "/v1/places/" + publicationHTTPPlace + "/social-history"
			case "anonymous":
				token = ""
			case "nullable":
				p.out.RecentMoments[0].AuthorID = nil
				p.out.RecentMoments[0].AuthorPublicProfile = nil
			case "clock_only":
				p.onRead = func(n int) error {
					if n == 2 {
						p.out.CheckedAt = p.out.CheckedAt.Add(time.Second)
						p.out.WindowStart = p.out.WindowStart.Add(time.Second)
					}
					return nil
				}
			case "bad_session":
				token = "invalid"
				want = 401
			case "revoke":
				p.onRead = func(n int) error {
					if n == 1 {
						auth.err = identity.ErrUnauthorized
					}
					return nil
				}
				want = 401
			case "changed_epoch":
				p.onRead = func(n int) error {
					if n == 2 {
						p.out.SourceBinding = "new-source"
					}
					return nil
				}
				want = 503
			case "changed_payload":
				p.onRead = func(n int) error {
					if n == 2 {
						p.out.RecentMoments[0].Excerpt = "变更节选"
						p.out.Summary.RecentMoments[0].Excerpt = "变更节选"
					}
					return nil
				}
				want = 503
			case "mutable_author":
				p.onRead = func(n int) error {
					if n == 2 {
						p.out.RecentMoments[0].AuthorPublicProfile.DisplayName = "变更公开作者"
					}
					return nil
				}
				want = 503
			case "withdrawn", "blocked", "retained_expired":
				p.onRead = func(n int) error {
					if n == 2 {
						return ph.ErrNotFound
					}
					return nil
				}
				want = 404
			case "invalid_final":
				p.onRead = func(n int) error {
					if n == 2 {
						p.out.RecentMoments[0].AuthorID = nil
					}
					return nil
				}
				want = 503
			case "missing_v2_port":
				s.catalog = struct{ foundation.PublicCatalog }{}
				want = 503
			}
			r := privateProfileHTTPRequest("GET", path, "", token, "")
			r.SetPathValue("placeID", publicationHTTPPlace)
			w := httptest.NewRecorder()
			s.getPublicPlaceSocialHistory(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(mode, w.Code, w.Body.String())
			}
			if want != 200 {
				if strings.Contains(w.Body.String(), "公开作者") || strings.Contains(w.Body.String(), "公开节选") {
					t.Fatal("failed source leaked")
				}
				return
			}
			var result struct{ Data map[string]json.RawMessage }
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Data) != 10 {
				t.Fatal("closed summary changed")
			}
			var rows []map[string]json.RawMessage
			if json.Unmarshal(result.Data["recentMoments"], &rows) != nil {
				t.Fatal("rows malformed")
			}
			if mode == "legacy" {
				if len(rows[0]) != 5 || p.authorCalls != 0 || p.legacyCalls != 1 {
					t.Fatal("legacy summary changed")
				}
			} else {
				if len(rows[0]) != 7 || p.authorCalls != 2 || p.legacyCalls != 0 {
					t.Fatal("new source read/fence missing")
				}
			}
			if mode == "signed" && (p.access.Actor.ID != privateProfileHTTPOwner || p.access.SessionDigest != auth.digest || auth.calls != 2) {
				t.Fatal("session fence missing")
			}
			if mode == "nullable" && (string(rows[0]["authorId"]) != "null" || string(rows[0]["authorPublicProfile"]) != "null") {
				t.Fatal("author null omitted")
			}
		})
	}
}
