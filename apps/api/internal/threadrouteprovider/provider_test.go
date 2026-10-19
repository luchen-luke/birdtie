package threadrouteprovider

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func stub(p *Provider, f func(*http.Request) string) {
	p.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(f(r)))}, nil
	})
}
func req(country string) content.ThreadRouteRequest {
	return content.ThreadRouteRequest{Mode: "recommended", From: content.ThreadRoutePoint{Latitude: 31.2, Longitude: 121.5, City: "上海市", CountryCode: country}, To: content.ThreadRoutePoint{Latitude: 31.201, Longitude: 121.501, City: "上海", CountryCode: country}, DepartureAt: time.Unix(1000000, 0)}
}
func response(seconds string) string {
	return `{"status":"1","infocode":"10000","route":{"paths":[{"distance":"133","duration":"` + seconds + `","cost":{"duration":"` + seconds + `"},"steps":[{"polyline":"121.504428,31.197966;121.504528,31.197966;121.504528,31.198066"}]}]}}`
}
func TestCoordinateRoundTripMatchesClientBoundary(t *testing.T) {
	for _, p := range []content.ThreadRoutePoint{{Latitude: 31.2, Longitude: 121.5}, {Latitude: 39.9, Longitude: 116.4}, {Latitude: 57.15, Longitude: -2.1}} {
		display := toAMap(p)
		back, ok := fromAMap(display)
		if !ok || math.Abs(back.Latitude-p.Latitude) > 1e-8 || math.Abs(back.Longitude-p.Longitude) > 1e-8 {
			t.Fatal("coordinate boundary must invert exactly once")
		}
	}
	p := toAMap(content.ThreadRoutePoint{Latitude: 31.2, Longitude: 121.5})
	if math.Abs(p.Latitude-31.1979666854059) > 1e-7 || math.Abs(p.Longitude-121.5044286589165) > 1e-7 {
		t.Fatal("client canonical calibration changed")
	}
}
func TestRoadGeometryRetainsBendAndCanonicalCoordinates(t *testing.T) {
	p := New("local-test", "")
	calls := 0
	stub(p, func(r *http.Request) string {
		calls++
		if r.URL.Host != "restapi.amap.com" || r.URL.Path != "/v5/direction/walking" || r.URL.Query().Get("origin") != "121.504429,31.197967" {
			t.Fatalf("incorrect provider/input coordinate boundary")
		}
		return response("120")
	})
	v, e := p.Plan(context.Background(), req("CN"))
	if e != nil || v.Status != "AVAILABLE" || len(v.Segments) != 1 || len(v.Segments[0]) != 3 || v.Mode != "walking" {
		t.Fatalf("road geometry missing: %v", v.Status)
	}
	if math.Abs(v.Segments[0][0].Latitude-31.2) > .00001 {
		t.Fatal("response must be WGS84")
	}
	v.Segments[0][0].Latitude = 0
	next, _ := p.Plan(context.Background(), req("CN"))
	if calls != 1 || next.Segments[0][0].Latitude == 0 {
		t.Fatal("cache aliases response or repeats provider")
	}
}
func TestRecommendationUsesRealDurationsAndCivuPriority(t *testing.T) {
	p := New("local-test", "")
	paths := []string{}
	stub(p, func(r *http.Request) string {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "walking") {
			return response("901")
		}
		return response("900")
	})
	v, _ := p.Plan(context.Background(), req("CN"))
	if v.Mode != "cycling" || len(paths) != 2 || paths[1] != "/v5/direction/bicycling" {
		t.Fatal("15 minute priority violated")
	}
	r := req("CN")
	r.Mode = "driving"
	v, _ = p.Plan(context.Background(), r)
	if v.Mode != "driving" || len(paths) != 3 {
		t.Fatal("explicit mode was overridden")
	}
	r = req("CN")
	r.To.City = "青岛"
	p = New("local-test", "")
	paths = nil
	stub(p, func(r *http.Request) string { paths = append(paths, r.URL.Path); return response("901") })
	v, _ = p.Plan(context.Background(), r)
	if v.Status != "BLOCKED" || len(paths) != 2 {
		t.Fatal("intercity cannot silently substitute driving or straight geometry")
	}
}
func TestUnavailableConfigRegionOrGeometryNeverDrawsStraightFallback(t *testing.T) {
	cases := []content.ThreadRouteRequest{req(""), req("XX"), req("CN")}
	cases = append(cases, req("GB"))
	cases[3].To.CountryCode = "CN"
	for _, r := range cases {
		v, _ := New("", "").Plan(context.Background(), r)
		if v.Status != "BLOCKED" || len(v.Segments) != 0 {
			t.Fatal("unavailable prerequisite must not draw")
		}
	}
	p := New("local-test", "")
	stub(p, func(*http.Request) string { return `{"status":"1","route":{"paths":[]}}` })
	r := req("CN")
	r.Mode = "walking"
	v, _ := p.Plan(context.Background(), r)
	if v.Status != "NOT_AVAILABLE" || len(v.Segments) != 0 {
		t.Fatal("missing geometry fabricated")
	}
	for _, raw := range []string{"", "1,2", "1,2;NaN,3", "1,2;1,2", "1,2;200,3"} {
		if _, ok := parseAMapPolyline(raw); ok {
			t.Fatal("malformed polyline accepted")
		}
	}
}
func TestAMapPreservesRealProviderGapAsSeparateSegments(t *testing.T) {
	p := New("local-test", "")
	stub(p, func(*http.Request) string {
		return `{"status":"1","route":{"paths":[{"distance":"133","cost":{"duration":"120"},"steps":[{"polyline":"121.5,31.2;121.501,31.201"},{"polyline":"121.51,31.21;121.511,31.211"}]}]}}`
	})
	v, _ := p.Plan(context.Background(), req("CN"))
	if v.Status != "AVAILABLE" || len(v.Segments) != 2 {
		t.Fatal("discontinuity was bridged")
	}
}
func TestMapboxUsesFullWGS84RoadShape(t *testing.T) {
	p := New("", "test-public")
	stub(p, func(r *http.Request) string {
		if r.URL.Host != "api.mapbox.com" || r.URL.Query().Get("geometries") != "geojson" || r.URL.Query().Get("overview") != "full" {
			t.Fatal("mapbox geometry request wrong")
		}
		return `{"code":"Ok","routes":[{"distance":130,"duration":120,"geometry":{"type":"LineString","coordinates":[[-2.1,57.1],[-2.101,57.1],[-2.101,57.101]]}}]}`
	})
	r := req("GB")
	r.From.Latitude, r.From.Longitude = 57.1, -2.1
	r.To.Latitude, r.To.Longitude = 57.101, -2.101
	v, e := p.Plan(context.Background(), r)
	if e != nil || v.Status != "AVAILABLE" || v.Provider != "mapbox" || v.Segments[0][1].Latitude != 57.1 || v.Segments[0][1].Longitude != -2.101 {
		t.Fatal("real Mapbox shape must remain unchanged")
	}
}
func TestOnlyExplicitTrainFlightMayUseSavedTransfer(t *testing.T) {
	for _, mode := range []string{"train", "flight"} {
		r := req("CN")
		r.Mode = mode
		v, _ := New("", "").Plan(context.Background(), r)
		if v.Status != "AVAILABLE" || v.GeometryKind != "TRANSFER" || v.Provider != "saved-transfer" || v.DurationSeconds != 0 {
			t.Fatal("explicit transfer changed")
		}
	}
}
func TestCanceledRequestDoesNotPopulateCache(t *testing.T) {
	p := New("local-test", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stub(p, func(*http.Request) string { return response("1") })
	_, err := p.Plan(ctx, req("CN"))
	if err == nil || len(p.cache) != 0 {
		t.Fatal("cancellation must retire lookup")
	}
}
