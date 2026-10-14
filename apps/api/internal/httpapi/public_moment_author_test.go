package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type publicMomentAuthorHTTPSpy struct {
	foundation.PublicCatalog
	legacyCalls, authorCalls int
	out                      content.PublicMomentWithAuthor
	access                   content.PublicMomentAccess
	onRead                   func(int) error
}

func (p *publicMomentAuthorHTTPSpy) GetPublicMoment(_ context.Context, a content.PublicMomentAccess, id string) (content.PublicMoment, error) {
	p.legacyCalls++
	p.access = a
	out := p.out.PublicMoment
	out.SchemaVersion = "public-moment-v1"
	return out, nil
}
func (p *publicMomentAuthorHTTPSpy) GetPublicMomentWithAuthor(_ context.Context, a content.PublicMomentAccess, id string) (content.PublicMomentWithAuthor, error) {
	p.authorCalls++
	p.access = a
	if p.onRead != nil {
		if e := p.onRead(p.authorCalls); e != nil {
			return content.PublicMomentWithAuthor{}, e
		}
	}
	return p.out, nil
}
func publicMomentAuthorHTTPFixture() content.PublicMomentWithAuthor {
	id := privateProfileHTTPForeign
	return content.PublicMomentWithAuthor{PublicMoment: content.PublicMoment{SchemaVersion: content.PublicMomentAuthorSchemaVersion, ID: publicationHTTPMoment, Title: "本人公开分享", Body: "正文", PlaceID: publicationHTTPPlace, PlaceName: "公开地点", CityID: "test", Revision: 2, PublishedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), SourceBinding: "native-public-source"}, AuthorID: &id, AuthorPublicProfile: &content.PublicMomentAuthor{AccountID: id, DisplayName: "公开作者"}}
}

func TestPublicMomentAuthorHTTPExplicitOptInAndFinalFence(t *testing.T) {
	for _, mode := range []string{"legacy", "anonymous", "signed", "nullable", "bad_session", "revoke_while_auth_wait", "changed_epoch", "changed_payload", "mutable_author", "withdrawn", "blocked", "retained_expired", "invalid_final", "wrong_target", "missing_v2_port"} {
		t.Run(mode, func(t *testing.T) {
			s, auth, _, token := privateProfileHTTPFixture(t)
			p := &publicMomentAuthorHTTPSpy{out: publicMomentAuthorHTTPFixture()}
			s.catalog = p
			path := "/v1/moments/" + publicationHTTPMoment + "?include=public-author"
			want := 200
			switch mode {
			case "legacy":
				path = "/v1/moments/" + publicationHTTPMoment
			case "anonymous":
				token = ""
			case "nullable":
				p.out.AuthorID = nil
				p.out.AuthorPublicProfile = nil
			case "bad_session":
				token = "invalid"
				want = 401
			case "revoke_while_auth_wait":
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
						p.out.Body = "改变后正文"
					}
					return nil
				}
				want = 503
			case "mutable_author":
				p.onRead = func(n int) error {
					if n == 2 {
						p.out.AuthorPublicProfile.DisplayName = "变更公开作者"
					}
					return nil
				}
				want = 503
			case "withdrawn", "blocked", "retained_expired":
				p.onRead = func(n int) error {
					if n == 2 {
						return content.ErrNotFound
					}
					return nil
				}
				want = 404
			case "invalid_final":
				p.onRead = func(n int) error {
					if n == 2 {
						p.out.AuthorID = nil
					}
					return nil
				}
				want = 503
			case "wrong_target":
				p.out.ID = publicationHTTPPlace
				want = 503
			case "missing_v2_port":
				s.catalog = struct{ foundation.PublicCatalog }{}
				want = 503
			}
			r := privateProfileHTTPRequest("GET", path, "", token, "")
			r.SetPathValue("momentID", publicationHTTPMoment)
			w := httptest.NewRecorder()
			s.getPublicMoment(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(mode, w.Code, w.Body.String())
			}
			if want != 200 {
				if strings.Contains(w.Body.String(), "公开作者") || strings.Contains(w.Body.String(), "正文") {
					t.Fatal("failed read leaked payload")
				}
				return
			}
			var result struct{ Data map[string]json.RawMessage }
			if json.Unmarshal(w.Body.Bytes(), &result) != nil {
				t.Fatal("invalid envelope")
			}
			if mode == "legacy" {
				if len(result.Data) != 9 || p.authorCalls != 0 || p.legacyCalls != 2 {
					t.Fatal("bare endpoint changed")
				}
			} else {
				if len(result.Data) != 11 || p.authorCalls != 2 || p.legacyCalls != 0 {
					t.Fatal("explicit v2 missing strict final read")
				}
			}
			if mode == "anonymous" && p.access.Actor.ID != "" {
				t.Fatal("anonymous inherited identity")
			}
			if mode == "signed" && (p.access.Actor.ID != privateProfileHTTPOwner || p.access.SessionDigest != auth.digest || auth.calls != 2) {
				t.Fatal("session/identity fence missing")
			}
			if mode == "nullable" && (string(result.Data["authorId"]) != "null" || string(result.Data["authorPublicProfile"]) != "null") {
				t.Fatal("nullable author fields omitted")
			}
		})
	}
}

func TestPublicMomentAuthorOptInRejectsAmbiguousSelectors(t *testing.T) {
	for _, query := range []string{"?", "?include=", "?include=author", "?Include=public-author", "?include=public-author&include=public-author", "?include=public-author&ownerId=" + privateProfileHTTPOwner, "?include=%zz"} {
		t.Run(query, func(t *testing.T) {
			s, _, _, _ := privateProfileHTTPFixture(t)
			p := &publicMomentAuthorHTTPSpy{out: publicMomentAuthorHTTPFixture()}
			s.catalog = p
			r := privateProfileHTTPRequest("GET", "/v1/moments/"+publicationHTTPMoment+query, "", "", "")
			r.SetPathValue("momentID", publicationHTTPMoment)
			w := httptest.NewRecorder()
			s.getPublicMoment(w, r)
			if w.Code != 400 || p.authorCalls != 0 || p.legacyCalls != 0 {
				t.Fatal("selector crossed read boundary", w.Code)
			}
		})
	}
}
