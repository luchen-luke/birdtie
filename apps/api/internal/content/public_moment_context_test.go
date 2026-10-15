package content

import (
	"strings"
	"testing"
	"time"
)

func TestPublicMomentContextCityIsExplicitAndPhotoRemainsPlaceOnly(t *testing.T) {
	at := time.Now().UTC()
	v := PublicMomentWithContext{PublicMomentWithPhoto: PublicMomentWithPhoto{PublicMomentWithAuthor: PublicMomentWithAuthor{PublicMoment: PublicMoment{SchemaVersion: PublicMomentContextSchemaVersion, ID: momentControlTestID, Title: "城市文字", Body: "不推断地点和位置", CityID: "aberdeen-gb", Revision: 2, PublishedAt: at, SourceBinding: strings.Repeat("a", 64)}}, ObservedAt: at, ValidUntil: at.Add(time.Minute)}, CityName: "阿伯丁", LocationPrecision: "city"}
	if err := ValidatePublicMomentWithContext(v, v.ID); err != nil {
		t.Fatal("valid explicit city", err)
	}
	for _, mode := range []string{"guessed-place", "guessed-name", "no-context", "legacy", "long-city", "lease"} {
		bad := v
		switch mode {
		case "guessed-place":
			bad.PlaceID = momentControlTestID
		case "guessed-name":
			bad.PlaceName = "市中心"
		case "no-context":
			bad.LocationPrecision = "none"
		case "legacy":
			bad.SchemaVersion = PublicMomentPhotoSchemaVersion
		case "long-city":
			bad.CityName = strings.Repeat("中", 214)
		case "lease":
			bad.ValidUntil = at.Add(61 * time.Second)
		}
		if ValidatePublicMomentWithContext(bad, bad.ID) == nil {
			t.Fatal("invalid explicit context accepted", mode)
		}
	}
	place := v
	place.PlaceID, place.PlaceName, place.LocationPrecision = momentControlTestID, "明确地点", "place"
	if ValidatePublicMomentWithContext(place, place.ID) != nil {
		t.Fatal("Place text compatibility")
	}
	if ValidatePublicMoment(place.PublicMoment, place.ID) == nil {
		t.Fatal("v4 silently accepted by closed legacy wire")
	}
}
