package locationreference

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
	"time"
)

func TestReferenceStableIdentityAndClosedMomentSelection(t *testing.T) {
	id, kind, obj, ok := OSMIdentity("node/12345")
	if !ok {
		t.Fatal("identity")
	}
	at := time.Now().UTC()
	v := Candidate{ReferenceID: id, ObjectKind: kind, ObjectID: obj, Name: "Synthetic outside-city point", SourceURL: "https://www.openstreetmap.org/node/12345", Attribution: "© OpenStreetMap contributors · ODbL 1.0", CoordinateSystem: "wgs84", Latitude: 51.5, Longitude: -.1, ObservedAt: at, ValidUntil: at.Add(time.Hour)}
	if !v.Valid() {
		t.Fatal("valid source")
	}
	h := v.Version()
	v.Name = "Changed source label"
	if v.Version() == h {
		t.Fatal("metadata not versioned")
	}
	id2, _, _, _ := OSMIdentity("node/12345")
	if id2 != id {
		t.Fatal("stable identity")
	}
	links := []content.MomentLocationInput{{ID: "00000000-0000-4000-8000-000000000001", Kind: "REFERENCE", ReferenceID: id, Precision: "place"}}
	in := content.MomentInput{Body: "正文", TimePrecision: "unknown", LocationPrecision: "none", Locations: &links}
	if _, e := content.NormalizeMomentInput(in); e != nil {
		t.Fatal("operating city required", e)
	}
	raw, _ := json.Marshal(links)
	if e := content.DecodeMomentLocations(raw); e != nil {
		t.Fatal("closed reference", e)
	}
	t.Log("reference golden", content.MomentControlInputHash(&in))
	links[0].Precision = "city"
	if _, e := content.NormalizeMomentLocations(links); e == nil {
		t.Fatal("invented city precision")
	}
	for _, s := range []string{"node/0", "node/../1", "venue/12", "12345"} {
		if _, _, _, ok := OSMIdentity(s); ok {
			t.Fatal("malformed source identity")
		}
	}
}
