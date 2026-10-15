package content

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMomentLocationsClosedInputNilGoldenAndExplicitClear(t *testing.T) {
	v := MomentInput{Body: "正文", TimePrecision: "unknown", LocationPrecision: "none"}
	if got := MomentControlInputHash(&v); got != "fa2320f7ee1ce98114609609d8f8a3f099f5bce837360ce648e3394c67c5e0d5" {
		t.Fatal("nil locations changed original byte digest", got)
	}
	old := MomentControlInputHash(&v)
	empty := []MomentLocationInput{}
	v.Locations = &empty
	if MomentControlInputHash(&v) == old {
		t.Fatal("explicit clear collapsed to omission")
	}
	in := MomentControlInput{OperationID: momentControlTestOp, Action: "CREATE", Input: &v}
	raw, _ := json.Marshal(in)
	if got, e := DecodeMomentControl(raw, false); e != nil || got.Input.Locations == nil || len(*got.Input.Locations) != 0 {
		t.Fatal("explicit clear not preserved", e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"locations":[]`, `"locations":null`, 1), strings.Replace(string(raw), `"locations":[]`, `"locations":[],"locations":[]`, 1)} {
		if _, e := DecodeMomentControl([]byte(bad), false); e == nil {
			t.Fatal("null or duplicate selector accepted")
		}
	}
	selected := []MomentLocationInput{{ID: momentControlTestID, Kind: "PLACE", PlaceID: momentControlTestOp, Precision: "place"}}
	v.Locations = &selected
	raw, _ = json.Marshal(in)
	if _, e := DecodeMomentControl(raw, false); e != nil {
		t.Fatal("complete selector rejected", e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"kind":"PLACE"`, `"kind":"PLACE","latitude":1`, 1), strings.Replace(string(raw), `"kind":"PLACE"`, `"kind":"PLACE","kind":"PLACE"`, 1), strings.Replace(string(raw), `"kind":"PLACE"`, `"kind":"PLACE","sourceVersion":"fake"`, 1)} {
		if _, e := DecodeMomentControl([]byte(bad), false); e == nil {
			t.Fatal("untrusted location authority accepted")
		}
	}
	v.CityID = "old-city"
	if _, e := NormalizeMomentInput(v); e == nil {
		t.Fatal("two geographic sources accepted")
	}
	all := []MomentLocationInput{}
	for i := 1; i <= MomentLocationMaxLinks; i++ {
		all = append(all, MomentLocationInput{ID: fmt.Sprintf("%08x-0000-4000-8000-000000000000", i), Kind: "CITY", CityID: fmt.Sprintf("city-%d", i), Precision: "city"})
	}
	if _, e := NormalizeMomentLocations(all); e != nil {
		t.Fatal("full50 selection rejected", e)
	}
	all = append(all, MomentLocationInput{ID: momentControlTestID, Kind: "CITY", CityID: "extra", Precision: "city"})
	if _, e := NormalizeMomentLocations(all); e == nil {
		t.Fatal("51 selection truncated")
	}
	duplicate := []MomentLocationInput{selected[0], {ID: momentControlTestOp, Kind: "PLACE", PlaceID: momentControlTestOp, Precision: "city"}}
	if _, e := NormalizeMomentLocations(duplicate); e == nil {
		t.Fatal("duplicate target via precision accepted")
	}
}
