package httpapi

import (
	"context"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMapMediaHTTPCORSKeepsOriginBoundary(t *testing.T) {
	for _, origin := range []string{"http://localhost:7357", "https://untrusted.invalid"} {
		t.Run(origin, func(t *testing.T) {
			called := false
			h := cors(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), []string{"http://localhost:7357"})
			r := httptest.NewRequest(http.MethodOptions, "/v1/cities/aberdeen-gb/map-layers", nil)
			r.Header.Set("Origin", origin)
			r.Header.Set("Access-Control-Request-Method", "GET")
			r.Header.Set("Access-Control-Request-Headers", "X-Birdtie-Map-Media")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if called {
				t.Fatal("preflight reached canonical handler")
			}
			if origin == "http://localhost:7357" {
				if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != origin || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "X-Birdtie-Map-Media") {
					t.Fatal("media negotiation unavailable to approved browser origin", w.Code, w.Header())
				}
			} else if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("media header broadened origin boundary", w.Code, w.Header())
			}
		})
	}
}

type mapMediaHTTPSpy struct {
	mapPublicAuthorHTTPSpy
	final       mp.View
	mediaFinals int
}

func (s *mapMediaHTTPSpy) FinalizeMapLayers(_ context.Context, a mp.Access, r mp.Receipt) (mp.View, error) {
	s.mediaFinals++
	if a != s.access || r.Query != s.query || !r.Query.Media {
		return mp.View{}, mp.ErrChanged
	}
	return s.final, s.finalErr
}
func TestMapMediaHTTPNegotiationKeepsClosedQueryAndScopes(t *testing.T) {
	for _, mode := range []string{"old", "media", "duplicate", "unknown", "private", "author", "query-injection"} {
		t.Run(mode, func(t *testing.T) {
			path := "/v1/cities/aberdeen-gb/map-layers?" + mapPublicAuthorHTTPBounds
			if mode == "author" {
				path += "&scope=public-author&authorId=" + mapPublicAuthorHTTPID
			}
			if mode == "query-injection" {
				path += "&media=true"
			}
			r := httptest.NewRequest("GET", path, nil)
			r.SetPathValue("cityID", "aberdeen-gb")
			if mode != "old" {
				r.Header.Set("X-Birdtie-Map-Media", "place-photo-v1")
			}
			if mode == "duplicate" {
				r.Header.Add("X-Birdtie-Map-Media", "place-photo-v1")
			}
			if mode == "unknown" {
				r.Header.Set("X-Birdtie-Map-Media", "all-private")
			}
			q, e := mapProjectionQuery(r, mode == "private")
			if (e == nil) != (mode == "old" || mode == "media") || (mode == "media" && !q.Media) {
				t.Fatal("closed negotiation", q, e)
			}
		})
	}
}
func TestMapMediaHTTPEncodesFinalRetiredDecoration(t *testing.T) {
	s, _, _, _ := privateProfileHTTPFixture(t)
	at := time.Now().UTC()
	id := mapPublicAuthorHTTPID
	v := mp.View{SchemaVersion: mp.MediaSchemaVersion, CityID: "aberdeen-gb", Scope: mp.Public, ObservedAt: at, ValidUntil: at.Add(2 * time.Minute), Items: []mp.Item{{Kind: "PLACE", ID: id, Title: "隔离实际最终边界", Entity: mp.Ref{Type: "PLACE", ID: id}, Detail: mp.Ref{Type: "PLACE", ID: id}, Anchor: mp.Anchor{PlaceID: id, Label: "隔离实际最终边界", CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.15, Longitude: -2.1}, SourceVersion: "base-original", PlacePhoto: &pp.Metadata{EntityType: "place", EntityID: id, Version: 1, ThumbnailPath: pp.Path(id, 1), Width: 320, Height: 180, ByteCount: 100, SHA256: strings.Repeat("a", 64), ExpiresAt: at.Add(time.Minute)}, RecentMoments: &mp.RecentMoments{Count: 37, CheckedAt: at, WindowStart: at.Add(-30 * 24 * time.Hour), ValidUntil: at.Add(time.Minute)}}}}
	final := mp.RetireExpiredMedia(v, at.Add(time.Minute))
	spy := &mapMediaHTTPSpy{mapPublicAuthorHTTPSpy: mapPublicAuthorHTTPSpy{view: v}, final: final}
	s.catalog = spy
	r := httptest.NewRequest("GET", "/v1/cities/aberdeen-gb/map-layers?"+mapPublicAuthorHTTPBounds, nil)
	r.SetPathValue("cityID", "aberdeen-gb")
	r.Header.Set("X-Birdtie-Map-Media", "place-photo-v1")
	w := httptest.NewRecorder()
	s.getPublicMapLayers(w, r)
	if w.Code != 200 || spy.mediaFinals != 1 || spy.finals != 0 || !strings.Contains(w.Body.String(), id) || !strings.Contains(w.Body.String(), "base-original") || strings.Contains(w.Body.String(), "placePhoto") || strings.Contains(w.Body.String(), "recentMoments") {
		t.Fatal("encoded stale initial decoration / lost base", w.Code, w.Body.String())
	}
}
