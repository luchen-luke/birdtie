package postgres

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityartwork"
)

type cityArtworkRow struct{ artwork []byte }

func (r cityArtworkRow) Scan(dest ...any) error {
	now := time.Now().UTC()
	values := []any{"synthetic-city", "合成目录", "合成地区", "CN", "Asia/Shanghai", "empty", "来源", "reference", "维护者", now, (*time.Time)(nil), (*time.Time)(nil), (*string)(nil), (*float64)(nil), (*float64)(nil), (*float64)(nil), (*string)(nil), r.artwork}
	for i, value := range values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}
func TestCityArtworkCatalogKeepsNoMapCitiesAndOptionalArtwork(t *testing.T) {
	city, e := scanCity(cityArtworkRow{})
	if e != nil || city.ID != "synthetic-city" || city.Map != nil || city.Artwork != nil {
		t.Fatal(city, e)
	}
	version := cityArtworkProjectionVersion(strings.Repeat("a", 64), strings.Repeat("b", 64))
	metadata := cityartwork.Metadata{Version: version, DescriptorPath: cityartwork.DescriptorPath(city.ID, version)}
	raw, _ := json.Marshal(metadata)
	city, e = scanCity(cityArtworkRow{raw})
	if e != nil || city.Map != nil || city.Artwork == nil || *city.Artwork != metadata {
		t.Fatal(city, e)
	}
	metadata.DescriptorPath = "https://old.example/private-artwork"
	raw, _ = json.Marshal(metadata)
	city, e = scanCity(cityArtworkRow{raw})
	if e != nil || city.ID != "synthetic-city" || city.Artwork != nil {
		t.Fatal("invalid artwork must preserve the selectable city without exposing the path", city, e)
	}
}
