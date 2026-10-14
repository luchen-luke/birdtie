package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	mphoto "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type publicMomentPhotoHTTPSpy struct {
	publicMomentAuthorHTTPSpy
	initial             content.PublicMomentWithPhoto
	final               content.PublicMomentWithPhoto
	captures, finalizes int
	finalError          error
}

func (s *publicMomentPhotoHTTPSpy) CapturePublicMomentWithPhoto(_ context.Context, _ content.PublicMomentAccess, id string, author bool) (content.PublicMomentPhotoReceipt, error) {
	s.captures++
	return content.PublicMomentPhotoReceipt{View: s.initial, IncludeAuthor: author}, nil
}
func (s *publicMomentPhotoHTTPSpy) FinalizePublicMomentWithPhoto(_ context.Context, _ content.PublicMomentAccess, r content.PublicMomentPhotoReceipt) (content.PublicMomentWithPhoto, error) {
	s.finalizes++
	return s.final, s.finalError
}
func publicMomentPhotoHTTPView() content.PublicMomentWithPhoto {
	at := time.Now().UTC()
	base := publicMomentAuthorHTTPFixture()
	base.SchemaVersion = content.PublicMomentPhotoSchemaVersion
	base.AuthorID = nil
	base.AuthorPublicProfile = nil
	photoID := "22222222-2222-4222-8222-222222222222"
	return content.PublicMomentWithPhoto{PublicMomentWithAuthor: base, ObservedAt: at, ValidUntil: at.Add(time.Minute), Photo: &mphoto.Metadata{MomentID: base.ID, MomentRevision: base.Revision, PhotoID: photoID, Version: 1, ThumbnailPath: mphoto.Path(base.ID, base.Revision, photoID, 1), Width: 320, Height: 180, ByteCount: 100, SHA256: strings.Repeat("a", 64), ExpiresAt: at.Add(50 * time.Second)}}
}
func TestPublicMomentPhotoHTTPClosedNegotiationAndFinalOriginalPayload(t *testing.T) {
	for _, mode := range []string{"photo", "retired", "unknown", "duplicate", "bad-query", "organization", "wrong-revision", "changed-base", "fresh-photo", "extended-lease", "absent-original"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, _ := privateProfileHTTPFixture(t)
			v := publicMomentPhotoHTTPView()
			spy := &publicMomentPhotoHTTPSpy{initial: v, final: v}
			s.catalog = spy
			req := httptest.NewRequest("GET", "/v1/moments/"+v.ID, nil)
			req.SetPathValue("momentID", v.ID)
			req.Header.Set("X-Birdtie-Public-Moment-Media", "photo-v1")
			want := 200
			switch mode {
			case "retired":
				spy.final.Photo = nil
			case "unknown":
				req.Header.Set("X-Birdtie-Public-Moment-Media", "future")
				want = 400
			case "duplicate":
				req.Header.Add("X-Birdtie-Public-Moment-Media", "photo-v1")
				want = 400
			case "bad-query":
				req.URL.RawQuery = "include=public-author&media=true"
				want = 400
			case "organization":
				req.Header.Set("X-Birdtie-Organization-Workspace", publicationHTTPPlace)
				want = 400
			case "wrong-revision":
				p := *v.Photo
				p.MomentRevision++
				spy.initial.Photo = &p
				want = 503
			case "changed-base":
				spy.final.Body = "替换正文"
				want = 503
			case "fresh-photo":
				p := *v.Photo
				p.Version++
				p.ThumbnailPath = mphoto.Path(p.MomentID, p.MomentRevision, p.PhotoID, p.Version)
				spy.final.Photo = &p
				want = 503
			case "extended-lease":
				p := *v.Photo
				p.ExpiresAt = p.ExpiresAt.Add(time.Second)
				spy.final.Photo = &p
				want = 503
			case "absent-original":
				spy.initial.Photo = nil
				want = 503
			}
			w := httptest.NewRecorder()
			s.getPublicMoment(w, req)
			if w.Code != want {
				t.Fatalf("%s status=%d want=%d %s", mode, w.Code, want, w.Body.String())
			}
			if want == 200 && (spy.captures != 1 || spy.finalizes != 1 || spy.legacyCalls != 0 || spy.authorCalls != 0 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "public-moment-v3")) {
				t.Fatal("media skipped actual original finalization")
			}
			if mode == "retired" && strings.Contains(w.Body.String(), "thumbnailPath") {
				t.Fatal("retired image leaked")
			}
		})
	}
}
func TestPublicMomentPhotoHTTPMapNegotiationAndCORS(t *testing.T) {
	for _, author := range []bool{false, true} {
		path := "/v1/cities/aberdeen-gb/map-layers?" + mapPublicAuthorHTTPBounds
		if author {
			path += "&scope=public-author&authorId=" + mapPublicAuthorHTTPID
		}
		r := httptest.NewRequest("GET", path, nil)
		r.SetPathValue("cityID", "aberdeen-gb")
		r.Header.Set("X-Birdtie-Map-Media", "public-moment-photo-v1")
		q, e := mapProjectionQuery(r, false)
		if e != nil || !q.MomentMedia || q.Media || (author && q.AuthorID != mapPublicAuthorHTTPID) {
			t.Fatal("closed v4 explicit scope", q, e)
		}
		if _, e = mapProjectionQuery(r, true); e == nil {
			t.Fatal("private accepted public photo header")
		}
	}
	called := false
	h := cors(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), []string{"http://localhost:7357"})
	r := httptest.NewRequest(http.MethodOptions, "/v1/moments/"+publicationHTTPMoment, nil)
	r.Header.Set("Origin", "http://localhost:7357")
	r.Header.Set("Access-Control-Request-Method", "GET")
	r.Header.Set("Access-Control-Request-Headers", "X-Birdtie-Public-Moment-Media")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || called || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "X-Birdtie-Public-Moment-Media") {
		t.Fatal("approved explicit read header CORS", w.Code, w.Header())
	}
}
