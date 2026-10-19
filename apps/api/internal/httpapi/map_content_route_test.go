package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type threadRouteHTTPSpy struct {
	*mapDisplayHTTPSpy
	query   content.MapContentThreadRouteQuery
	canPlan bool
	edit    func(*content.MapContentThreadRoutePlan)
}

func (p *threadRouteHTTPSpy) ReadHumanMapContentThreadRoutePlan(_ context.Context, _ [32]byte, a identity.Actor, q content.MapContentThreadRouteQuery) (content.MapContentThreadRoutePlan, error) {
	p.query = q
	at := time.Now().UTC()
	v := content.MapContentThreadRoutePlan{CanPlan: p.canPlan, Route: content.MapContentThreadRoute{SchemaVersion: content.MapContentThreadRouteSchema, ViewerAccountID: a.ID, ThreadID: q.ThreadID, FromMarkerID: q.From.MarkerID, ToMarkerID: q.To.MarkerID, FromSourceVersion: q.From.SourceVersion, ToSourceVersion: q.To.SourceVersion, Status: "NOT_AVAILABLE", CoordinateSystem: "wgs84", Segments: [][]content.ThreadRoutePoint{}, ObservedAt: at, ValidUntil: at.Add(30 * time.Second), SourceVersion: strings.Repeat("c", 64), ResponseProof: p.currentProof()}, Request: content.ThreadRouteRequest{Mode: "walking", From: content.ThreadRoutePoint{Latitude: 57.1, Longitude: -2.1, CountryCode: "GB", City: "Aberdeen"}, To: content.ThreadRoutePoint{Latitude: 57.2, Longitude: -2.2, CountryCode: "GB", City: "Aberdeen"}, DepartureAt: at}}
	if p.edit != nil {
		p.edit(&v)
	}
	return v, p.readErr
}

type threadRouteHTTPProvider struct {
	calls    int
	after    func()
	geometry content.ThreadRouteGeometry
}

func (p *threadRouteHTTPProvider) Plan(_ context.Context, q content.ThreadRouteRequest) (content.ThreadRouteGeometry, error) {
	p.calls++
	if p.after != nil {
		p.after()
	}
	return p.geometry, nil
}
func routeHTTPQuery() content.MapContentThreadRouteQuery {
	path := "/v1/me/map-content?west=-3&south=57&east=-2&north=58&zoom=14&cityId=aberdeen-gb"
	return content.MapContentThreadRouteQuery{ThreadID: mapDisplayHTTPPlace, ExpectedProvider: "mapbox", From: content.MapContentRouteEndpoint{ViewportPath: path, MarkerID: "anchor:" + mapDisplayHTTPMoment, SourceVersion: strings.Repeat("a", 64)}, To: content.MapContentRouteEndpoint{ViewportPath: path, MarkerID: "anchor:" + mapDisplayHTTPMedia, SourceVersion: strings.Repeat("b", 64)}}
}
func TestMapContentThreadRouteHTTPAuthorityAndNoFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		code, calls int
		mutate      func(*threadRouteHTTPSpy, *threadRouteHTTPProvider, *http.Request)
	}{
		{"road", 200, 1, func(*threadRouteHTTPSpy, *threadRouteHTTPProvider, *http.Request) {}},
		{"missing_saved_leg", 200, 0, func(p *threadRouteHTTPSpy, _ *threadRouteHTTPProvider, _ *http.Request) { p.canPlan = false }},
		{"retired_before_egress", 409, 0, func(p *threadRouteHTTPSpy, _ *threadRouteHTTPProvider, _ *http.Request) {
			p.proofErr = identity.ErrControlChanged
		}},
		{"retired_during_provider", 409, 1, func(p *threadRouteHTTPSpy, provider *threadRouteHTTPProvider, _ *http.Request) {
			provider.after = func() { p.proofErr = identity.ErrControlChanged }
		}},
		{"bad_geometry_no_straight_fallback", 503, 1, func(_ *threadRouteHTTPSpy, p *threadRouteHTTPProvider, _ *http.Request) { p.geometry.Segments = nil }},
		{"wrong_saved_mode", 503, 1, func(_ *threadRouteHTTPSpy, p *threadRouteHTTPProvider, _ *http.Request) { p.geometry.Mode = "driving" }},
		{"wrong_provider_echo", 503, 1, func(_ *threadRouteHTTPSpy, p *threadRouteHTTPProvider, _ *http.Request) { p.geometry.Provider = "amap" }},
		{"provider_blocked", 200, 1, func(_ *threadRouteHTTPSpy, p *threadRouteHTTPProvider, _ *http.Request) {
			p.geometry = content.ThreadRouteGeometry{Status: "BLOCKED"}
		}},
		{"supplier_scope_mismatch", 200, 0, func(p *threadRouteHTTPSpy, _ *threadRouteHTTPProvider, _ *http.Request) {
			p.edit = func(v *content.MapContentThreadRoutePlan) {
				v.Request.From.CountryCode = "CN"
				v.Request.To.CountryCode = "CN"
			}
		}},
		{"foreign_source_echo", 503, 0, func(p *threadRouteHTTPSpy, _ *threadRouteHTTPProvider, _ *http.Request) {
			p.edit = func(v *content.MapContentThreadRoutePlan) { v.Route.FromSourceVersion = strings.Repeat("d", 64) }
		}},
		{"organization", 400, 0, func(_ *threadRouteHTTPSpy, _ *threadRouteHTTPProvider, r *http.Request) {
			r.Header.Set("X-Birdtie-Organization-Workspace", "other")
		}},
		{"query_coordinate_proxy", 400, 0, func(_ *threadRouteHTTPSpy, _ *threadRouteHTTPProvider, r *http.Request) {
			r.URL.RawQuery = "lat=57&lon=-2"
		}},
		{"saved_transfer_no_provider", 200, 0, func(p *threadRouteHTTPSpy, _ *threadRouteHTTPProvider, _ *http.Request) {
			p.edit = func(v *content.MapContentThreadRoutePlan) { v.Request.Mode = "train" }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &threadRouteHTTPSpy{mapDisplayHTTPSpy: newMapDisplayHTTPSpy(t), canPlan: true}
			provider := &threadRouteHTTPProvider{geometry: content.ThreadRouteGeometry{Status: "AVAILABLE", Mode: "walking", Provider: "mapbox", CoordinateSystem: "wgs84", GeometryKind: "ROAD", DistanceMeters: 100, DurationSeconds: 60, Segments: [][]content.ThreadRoutePoint{{{Latitude: 57.1, Longitude: -2.1}, {Latitude: 57.15, Longitude: -2.15}}, {{Latitude: 57.16, Longitude: -2.16}, {Latitude: 57.2, Longitude: -2.2}}}}}
			raw, _ := json.Marshal(routeHTTPQuery())
			r := hcRequest("POST", "/v1/me/map-content/thread-route", string(raw))
			tc.mutate(p, provider, r)
			s := &server{access: p, content: p}
			WithThreadRouteProvider(provider)(s)
			mux := http.NewServeMux()
			s.registerMapContentThreadRoute(mux)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.code || provider.calls != tc.calls {
				t.Fatalf("status %d want %d provider calls %d want %d: %s", w.Code, tc.code, provider.calls, tc.calls, w.Body.String())
			}
			if tc.code == 200 {
				var body struct {
					Data content.MapContentThreadRoute `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || !body.Data.Valid(time.Now().UTC()) {
					t.Fatal("invalid response")
				}
				if tc.name == "missing_saved_leg" || tc.name == "provider_blocked" {
					if len(body.Data.Segments) != 0 || body.Data.DistanceMeters != 0 || body.Data.DurationSeconds != 0 {
						t.Fatal("failed query drew fallback")
					}
				}
				if tc.name == "road" && len(body.Data.Segments) != 2 {
					t.Fatal("separate roads bridged")
				}
				if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Fatal("missing privacy header")
				}
			} else if strings.Contains(w.Body.String(), "segments") {
				t.Fatal("denied geometry escaped")
			}
		})
	}
}
