package httpapi

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const publicationHTTPMoment = "81000000-0000-4000-8000-000000000001"
const publicationHTTPPlace = "81000000-0000-4000-8000-000000000002"

type publicationHTTPSpy struct {
	content.MomentStore
	preview content.MomentPublicationPreview
	receipt content.MomentPublicationReceipt
	calls   int
	err     error
	digest  [32]byte
	actor   identity.Actor
}

func (p *publicationHTTPSpy) PreviewHumanMomentPublication(_ context.Context, d [32]byte, a identity.Actor, id string) (content.MomentPublicationPreview, error) {
	p.calls++
	p.digest = d
	p.actor = a
	return p.preview, p.err
}
func (p *publicationHTTPSpy) PublishHumanMoment(_ context.Context, d [32]byte, a identity.Actor, id string, in content.MomentPublicationInput) (content.MomentPublicationReceipt, error) {
	p.calls++
	p.digest = d
	p.actor = a
	return p.receipt, p.err
}
func publicationHTTPBoundary(t *testing.T) (*server, *publicationHTTPSpy, string) {
	t.Helper()
	s, _, _, token := privateProfileHTTPFixture(t)
	p := &publicationHTTPSpy{preview: content.MomentPublicationPreview{MomentID: publicationHTTPMoment, PlaceID: publicationHTTPPlace, CityID: "test", PlaceName: "合成地点", Title: "合成明确公开", Body: "仅合成", Revision: 1, Snapshot: "mp1.1.2." + strings.Repeat("a", 64), ExpiresAt: time.Now().UTC().Add(time.Minute)}, receipt: content.MomentPublicationReceipt{MomentID: publicationHTTPMoment, PlaceID: publicationHTTPPlace, Revision: 2, Status: "published", PublishedAt: time.Now().UTC()}}
	s.content = p
	return s, p, token
}
func publicationHTTPServe(s *server, method, body, token string) *httptest.ResponseRecorder {
	r := privateProfileHTTPRequest(method, "/v1/me/moments/"+publicationHTTPMoment+"/publication", body, token, "application/json")
	r.SetPathValue("momentID", publicationHTTPMoment)
	w := httptest.NewRecorder()
	if method == "GET" {
		s.previewMomentPublication(w, r)
	} else {
		s.publishMomentPublication(w, r)
	}
	return w
}
func TestMomentPublicationHTTPStrictWire(t *testing.T) {
	valid := `{"revision":1,"snapshot":"mp1.1.2.` + strings.Repeat("a", 64) + `","confirmPublic":true}`
	for _, raw := range []string{`{}`, `null`, strings.Replace(valid, `"revision":1`, `"revision":null`, 1), strings.Replace(valid, `"revision":1`, `"revision":1,"revision":1`, 1), strings.Replace(valid, `"revision":1`, `"Revision":1`, 1), strings.Replace(valid, `true`, `false`, 1), strings.Replace(valid, `true`, `true,"ownerId":"PRIVATE_BODY_CANARY"`, 1), valid + ` {}`, string([]byte{0xff}), valid + strings.Repeat(" ", 4096)} {
		t.Run(raw[:min(len(raw), 20)], func(t *testing.T) {
			s, p, token := publicationHTTPBoundary(t)
			w := publicationHTTPServe(s, "POST", raw, token)
			if w.Code != 400 || p.calls != 0 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "PRIVATE_BODY_CANARY") {
				t.Fatal("invalid write/unsafe error", w.Code, p.calls)
			}
		})
	}
	s, p, token := publicationHTTPBoundary(t)
	w := publicationHTTPServe(s, "POST", valid, token)
	if w.Code != 200 || p.calls != 1 || p.digest == ([32]byte{}) || p.actor.ID != privateProfileHTTPOwner {
		t.Fatal("exact native access transport", w.Code)
	}
}
func TestMomentPublicationHTTPFailClosedPortsAndResponses(t *testing.T) {
	for _, mode := range []string{"missing", "wrong_target", "private_error", "wrong_receipt", "anonymous", "org"} {
		t.Run(mode, func(t *testing.T) {
			s, p, token := publicationHTTPBoundary(t)
			method := "GET"
			body := ""
			want := 503
			switch mode {
			case "missing":
				s.content = struct{ content.MomentStore }{}
			case "wrong_target":
				p.preview.MomentID = publicationHTTPPlace
			case "private_error":
				p.err = errors.New("SECRET_SQL_TOKEN_BODY")
			case "wrong_receipt":
				method = "POST"
				body = `{"revision":1,"snapshot":"mp1.1.2.` + strings.Repeat("a", 64) + `","confirmPublic":true}`
				p.receipt.Revision = 1
			case "anonymous":
				token = ""
				want = http.StatusUnauthorized
			case "org":
				s.access.(*privateProfileHTTPAccess).actor.AccountType = "organization"
				want = 403
			}
			w := publicationHTTPServe(s, method, body, token)
			if w.Code != want || strings.Contains(w.Body.String(), "SECRET_SQL_TOKEN_BODY") {
				t.Fatal(w.Code, want)
			}
		})
	}
}
