package content

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestMapContentThreadRouteClosedSelectorsAndGeometry(t *testing.T) {
	endpoint := MapContentRouteEndpoint{ViewportPath: "/v1/me/map-content?west=-3&south=57&east=-2&north=58&zoom=14&cityId=aberdeen-gb", MarkerID: "anchor:33333333-3333-3333-3333-333333333333", SourceVersion: strings.Repeat("a", 64)}
	if _, err := endpoint.Query(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"https://route.invalid/v1/me/map-content?west=-3&south=57&east=-2&north=58&zoom=14", endpoint.ViewportPath + "&provider=custom", endpoint.ViewportPath + "&zoom=12", endpoint.ViewportPath + "#fragment", "//route.invalid/v1/me/map-content", strings.Replace(endpoint.ViewportPath, "/map-content?", "/map-content/%2e%2e?", 1)} {
		v := endpoint
		v.ViewportPath = bad
		if _, err := v.Query(); err == nil {
			t.Fatalf("accepted noncanonical selector %q", bad)
		}
	}
	v := endpoint
	v.ViewportPath += "&cursor=signed.current.cursor&sourceVersion=" + strings.Repeat("b", 64)
	q, err := v.Query()
	if err != nil || q.Cursor != "signed.current.cursor" {
		t.Fatal("original continuation lost", err)
	}
	at := time.Now().UTC()
	route := MapContentThreadRoute{SchemaVersion: MapContentThreadRouteSchema, ViewerAccountID: "11111111-1111-1111-1111-111111111111", ThreadID: "22222222-2222-2222-2222-222222222222", FromMarkerID: endpoint.MarkerID, ToMarkerID: "anchor:44444444-4444-4444-4444-444444444444", FromSourceVersion: endpoint.SourceVersion, ToSourceVersion: endpoint.SourceVersion, SourceVersion: endpoint.SourceVersion, ObservedAt: at, ValidUntil: at.Add(30 * time.Second), Status: "NOT_AVAILABLE", CoordinateSystem: "wgs84", Segments: [][]ThreadRoutePoint{}}
	if !route.Valid(at) {
		t.Fatal("bounded negative unavailable")
	}
	road := route
	road.Status = "AVAILABLE"
	road.Mode = "walking"
	road.Provider = "synthetic"
	road.GeometryKind = "ROAD"
	road.DistanceMeters = 100
	road.DurationSeconds = 60
	road.Segments = [][]ThreadRoutePoint{{{Latitude: 57, Longitude: -2.5}, {Latitude: 57.01, Longitude: -2.49}}, {{Latitude: 57.02, Longitude: -2.48}, {Latitude: 57.03, Longitude: -2.47}}}
	if !road.Valid(at) {
		t.Fatal("independently separated legitimate segments")
	}
	bad := road
	bad.Segments = [][]ThreadRoutePoint{{{Latitude: math.NaN(), Longitude: -2.5}, {Latitude: 57, Longitude: -2.49}}}
	if bad.Valid(at) {
		t.Fatal("nonfinite geometry")
	}
	bad = road
	bad.Segments = nil
	if bad.Valid(at) {
		t.Fatal("missing geometry became endpoint line")
	}
	bad = road
	bad.DurationSeconds = 0
	if bad.Valid(at) {
		t.Fatal("invented road metrics")
	}
	transfer := road
	transfer.GeometryKind = "TRANSFER"
	transfer.Mode = "flight"
	transfer.Provider = "saved-transfer"
	transfer.DistanceMeters = 0
	transfer.DurationSeconds = 0
	if !transfer.Valid(at) {
		t.Fatal("explicit transfer")
	}
	transfer.Mode = "walking"
	if transfer.Valid(at) {
		t.Fatal("road failure disguised as transfer")
	}

	if ThreadRouteMatchesExpectedProvider(ThreadRouteRequest{Mode: "walking", From: ThreadRoutePoint{CountryCode: "CN"}, To: ThreadRoutePoint{CountryCode: "CN"}}, "mapbox") {
		t.Fatal("background mainland selector bypassed current Mapbox privacy scope")
	}
	if !ThreadRouteMatchesExpectedProvider(ThreadRouteRequest{Mode: "walking", From: ThreadRoutePoint{CountryCode: "HK"}, To: ThreadRoutePoint{CountryCode: "TW"}}, "mapbox") {
		t.Fatal("HK/TW incorrectly sent to mainland supplier")
	}
	if ThreadRouteMatchesExpectedProvider(ThreadRouteRequest{Mode: "walking", From: ThreadRoutePoint{}, To: ThreadRoutePoint{CountryCode: "GB"}}, "mapbox") {
		t.Fatal("unknown country promoted to supplier authority")
	}
	if road.Valid(route.ValidUntil) {
		t.Fatal("route extended original lease")
	}
}
