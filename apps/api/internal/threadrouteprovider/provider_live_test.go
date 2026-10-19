package threadrouteprovider

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"os"
	"testing"
	"time"
)

// Explicit opt-in only. Public-city endpoints exercise the real provider parser;
// no Birdtie API/database/content/session is touched. Keys and URLs are not logged.
func TestLiveRoadProviders(t *testing.T) {
	if os.Getenv("BIRDTIE_ROUTE_PROVIDER_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit live-provider opt-in absent")
	}
	p := New(os.Getenv("BIRDTIE_AMAP_WEB_SERVICE_KEY"), os.Getenv("BIRDTIE_MAPBOX_DIRECTIONS_TOKEN"))
	for _, mode := range []string{"walking", "cycling", "driving"} {
		t.Run("amap_"+mode, func(t *testing.T) {
			r := content.ThreadRouteRequest{Mode: mode, From: content.ThreadRoutePoint{Latitude: 31.2304, Longitude: 121.4737, City: "上海", CountryCode: "CN"}, To: content.ThreadRoutePoint{Latitude: 31.2314, Longitude: 121.4747, City: "上海", CountryCode: "CN"}, DepartureAt: time.Now().UTC()}
			v, e := p.Plan(context.Background(), r)
			if e != nil || v.Status != "AVAILABLE" || v.Provider != "amap" || v.CoordinateSystem != "wgs84" || len(v.Segments) == 0 {
				t.Fatalf("live route unavailable: status=%s", v.Status)
			}
			n := 0
			for _, s := range v.Segments {
				n += len(s)
			}
			t.Logf("segments=%d shapePoints=%d mode=%s", len(v.Segments), n, v.Mode)
		})
	}
	t.Run("mapbox_walking", func(t *testing.T) {
		r := content.ThreadRouteRequest{Mode: "walking", From: content.ThreadRoutePoint{Latitude: 57.1497, Longitude: -2.0943, City: "Aberdeen", CountryCode: "GB"}, To: content.ThreadRoutePoint{Latitude: 57.151, Longitude: -2.094, City: "Aberdeen", CountryCode: "GB"}, DepartureAt: time.Now().UTC()}
		v, e := p.Plan(context.Background(), r)
		if e != nil || v.Status != "AVAILABLE" || v.Provider != "mapbox" || v.CoordinateSystem != "wgs84" || len(v.Segments) == 0 {
			t.Fatalf("live route unavailable: status=%s", v.Status)
		}
		t.Logf("segments=%d shapePoints=%d mode=%s", len(v.Segments), len(v.Segments[0]), v.Mode)
	})
}
