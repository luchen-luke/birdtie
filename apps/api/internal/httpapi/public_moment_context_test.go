package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

type publicMomentContextHTTPSpy struct {
	publicMomentAuthorHTTPSpy
	initial, final      content.PublicMomentWithContext
	captures, finalizes int
}

func (s *publicMomentContextHTTPSpy) CapturePublicMomentWithContext(_ context.Context, _ content.PublicMomentAccess, _ string, author, photo bool) (content.PublicMomentContextReceipt, error) {
	s.captures++
	return content.PublicMomentContextReceipt{View: s.initial, IncludeAuthor: author, IncludePhoto: photo}, nil
}

func (s *publicMomentContextHTTPSpy) FinalizePublicMomentWithContext(_ context.Context, _ content.PublicMomentAccess, _ content.PublicMomentContextReceipt) (content.PublicMomentWithContext, error) {
	s.finalizes++
	return s.final, nil
}

func TestPublicMomentContextHTTPClosedCityWireAndCurrentSnapshot(t *testing.T) {
	for _, mode := range []string{"city", "unknown", "duplicate", "changed-city", "guessed-place", "organization", "unexpected-author"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, _ := privateProfileHTTPFixture(t)
			base := publicMomentPhotoHTTPView()
			base.SchemaVersion = content.PublicMomentContextSchemaVersion
			base.PlaceID, base.PlaceName, base.Photo = "", "", nil
			v := content.PublicMomentWithContext{PublicMomentWithPhoto: base, CityName: "阿伯丁", LocationPrecision: "city"}
			spy := &publicMomentContextHTTPSpy{initial: v, final: v}
			s.catalog = spy
			req := httptest.NewRequest("GET", "/v1/moments/"+v.ID, nil)
			req.SetPathValue("momentID", v.ID)
			req.Header.Set("X-Birdtie-Public-Moment-Media", "photo-v1")
			req.Header.Set("X-Birdtie-Public-Moment-Context", "city-v1")
			want := 200
			switch mode {
			case "unknown":
				req.Header.Set("X-Birdtie-Public-Moment-Context", "future")
				want = 400
			case "duplicate":
				req.Header.Add("X-Birdtie-Public-Moment-Context", "city-v1")
				want = 400
			case "changed-city":
				spy.final.CityName = "替换城市"
				want = 503
			case "guessed-place":
				spy.initial.PlaceID = publicationHTTPPlace
				want = 503
			case "organization":
				req.Header.Set("X-Birdtie-Organization-Workspace", publicationHTTPPlace)
				want = 400
			case "unexpected-author":
				id := "22222222-2222-4222-8222-222222222222"
				spy.initial.AuthorID = &id
				spy.initial.AuthorPublicProfile = &content.PublicMomentAuthor{AccountID: id, DisplayName: "未经请求的作者"}
				want = 503
			}
			w := httptest.NewRecorder()
			s.getPublicMoment(w, req)
			if w.Code != want {
				t.Fatalf("status=%d want=%d %s", w.Code, want, w.Body.String())
			}
			if want == 200 && (spy.captures != 1 || spy.finalizes != 1 || spy.legacyCalls != 0 || spy.authorCalls != 0 || !strings.Contains(w.Body.String(), `"schemaVersion":"public-moment-v4"`) || !strings.Contains(w.Body.String(), `"photo":null`) || strings.Contains(w.Body.String(), "市中心")) {
				t.Fatal("city did not use exact native snapshot", w.Body.String())
			}
		})
	}
}
