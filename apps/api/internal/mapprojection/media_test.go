package mapprojection

import (
	"encoding/json"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"strings"
	"testing"
	"time"
)

func mediaMapView() View {
	at := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	id := "11111111-1111-4111-8111-111111111111"
	return View{SchemaVersion: MediaSchemaVersion, CityID: "aberdeen-gb", Scope: Public,
		ObservedAt: at, ValidUntil: at.Add(2 * time.Minute), Items: []Item{{
			Kind: "PLACE", ID: id, Title: "隔离获准地点", Entity: Ref{Type: "PLACE", ID: id}, Detail: Ref{Type: "PLACE", ID: id},
			Anchor: Anchor{PlaceID: id, Label: "隔离获准地点", CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.15, Longitude: -2.1}, SourceVersion: "original",
			PlacePhoto:    &pp.Metadata{EntityType: "place", EntityID: id, Version: 1, ThumbnailPath: pp.Path(id, 1), Width: 320, Height: 180, ByteCount: 100, SHA256: strings.Repeat("a", 64), ExpiresAt: at.Add(time.Minute), Attribution: "隔离权利样例"},
			RecentMoments: &RecentMoments{Count: 37, CheckedAt: at, WindowStart: at.Add(-30 * 24 * time.Hour), ValidUntil: at.Add(time.Minute)},
		}}}
}

func TestMapMediaOptionalLeaseRetirementPreservesBase(t *testing.T) {
	v := mediaMapView()
	if ValidateView(v) != nil {
		t.Fatal("valid closed v3")
	}
	out := RetireExpiredMedia(v, v.ObservedAt.Add(time.Minute))
	if len(out.Items) != 1 || out.Items[0].PlacePhoto != nil || out.Items[0].RecentMoments != nil || out.ValidUntil != v.ValidUntil || out.Items[0].ID != v.Items[0].ID || out.Items[0].Anchor != v.Items[0].Anchor || out.Items[0].SourceVersion != "original" {
		t.Fatal("media lease removed/renewed original point", out)
	}
	if v.Items[0].PlacePhoto == nil || v.Items[0].RecentMoments == nil {
		t.Fatal("retirement mutated original sealed receipt")
	}
	v.Items[0].RecentMoments.Count = 0
	if ValidateView(v) != nil {
		t.Fatal("known zero is valid; rendering omits hint")
	}
	v.Items[0].RecentMoments = nil
	if ValidateView(v) != nil {
		t.Fatal("unknown is nullable, not forged zero")
	}
}

func TestMapMediaClosedScopeTargetAndOldWire(t *testing.T) {
	for _, mode := range []string{"legacy", "private", "author", "wrong-target", "wrong-window", "renewed-count", "negative-count", "wrong-photo-path", "wrong-photo-size"} {
		t.Run(mode, func(t *testing.T) {
			v := mediaMapView()
			switch mode {
			case "legacy":
				v.SchemaVersion = SchemaVersion
			case "private":
				v.Scope = SelfPrivate
			case "author":
				v.SchemaVersion = PublicAuthorSchemaVersion
				v.Scope = PublicAuthor
				v.AuthorID = v.Items[0].ID
			case "wrong-target":
				v.Items[0].PlacePhoto.EntityID = "22222222-2222-4222-8222-222222222222"
			case "wrong-window":
				v.Items[0].RecentMoments.WindowStart = v.ObservedAt.Add(-29 * 24 * time.Hour)
			case "renewed-count":
				v.Items[0].RecentMoments.ValidUntil = v.ObservedAt.Add(90 * time.Second)
			case "negative-count":
				v.Items[0].RecentMoments.Count = -1
			case "wrong-photo-path":
				v.Items[0].PlacePhoto.ThumbnailPath = "https://outside.invalid/photo.jpg"
			case "wrong-photo-size":
				v.Items[0].PlacePhoto.Width = 319
			}
			if ValidateView(v) == nil {
				t.Fatal("invalid decoration accepted", mode)
			}
		})
	}
	v := mediaMapView()
	v.SchemaVersion = SchemaVersion
	v.Items[0].PlacePhoto = nil
	v.Items[0].RecentMoments = nil
	raw, e := json.Marshal(v)
	if e != nil || strings.Contains(string(raw), "placePhoto") || strings.Contains(string(raw), "recentMoments") || ValidateView(v) != nil {
		t.Fatal("legacy wire changed", string(raw), e)
	}
	q := Query{CityID: v.CityID, West: -2.2, South: 57.1, East: -2, North: 57.3, Media: true}
	if ValidateViewForQuery(v, q) == nil {
		t.Fatal("v1 silently substituted for negotiated native v3")
	}
}
