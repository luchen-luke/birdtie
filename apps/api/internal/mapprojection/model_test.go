package mapprojection

import (
	"math"
	"testing"
	"time"
)

func TestMapProjectionUntitledMomentPreservesRealAnchor(t *testing.T) {
	id, place := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	item := Item{Kind: "MOMENT", ID: id, Entity: Ref{Type: "MOMENT", ID: id}, Detail: Ref{Type: "MOMENT", ID: id}, Anchor: Anchor{PlaceID: place, Label: "真实获准地点", CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.2, Longitude: -2.1}, SourceVersion: "2"}
	for _, scope := range []string{Public, PublicAuthor} {
		if !ValidItem(item, scope) {
			t.Fatal("untitled Moment lost its canonical public point", scope)
		}
	}
	if item.Title != "" {
		t.Fatal("invented author title")
	}
	for _, kind := range []string{"PLACE", "ACTIVITY", "ORGANIZATION", "BUSINESS"} {
		bad := item
		bad.Kind = kind
		bad.Entity.Type, bad.Detail.Type = kind, kind
		if ValidItem(bad, Public) {
			t.Fatal("unrelated entity lost its name requirement", kind)
		}
	}
	for _, change := range []func(*Item){func(v *Item) { v.Anchor.PlaceID = "" }, func(v *Item) { v.Anchor.Precision = "area" }, func(v *Item) { v.Anchor.CoordinateSystem = "gcj02" }, func(v *Item) { v.Anchor.Latitude = math.NaN() }, func(v *Item) { v.Detail.ID = place }} {
		bad := item
		change(&bad)
		if ValidItem(bad, Public) {
			t.Fatal("optional title admitted a fake point or substituted detail")
		}
	}
}

func TestMapProjectionClosedKindsAndBounds(t *testing.T) {
	for _, k := range []string{"PLACE", "ACTIVITY", "MOMENT", "ORGANIZATION", "BUSINESS", "OPPORTUNITY"} {
		if !ValidKind(k) {
			t.Fatal(k)
		}
	}
	for _, k := range []string{"PERSON", "ONLINE_INTENT", "MEMORY", "place", ""} {
		if ValidKind(k) {
			t.Fatal(k)
		}
	}
	q := Query{CityID: "aberdeen-gb", West: -2.2, South: 57.1, East: -2, North: 57.3}
	if ValidateQuery(q) != nil {
		t.Fatal("valid real viewport")
	}
	q.West = math.NaN()
	if ValidateQuery(q) == nil {
		t.Fatal("nan")
	}
	q.West = -2.2
	q.North = 57
	if ValidateQuery(q) == nil {
		t.Fatal("inverted")
	}
}
func TestMapProjectionNoPrivateSourceOrFakePoint(t *testing.T) {
	at := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	i := Item{Kind: "PLACE", ID: "11111111-1111-4111-8111-111111111111", Title: "原公开地点", Entity: Ref{Type: "PLACE", ID: "11111111-1111-4111-8111-111111111111"}, Detail: Ref{Type: "PLACE", ID: "11111111-1111-4111-8111-111111111111"}, Anchor: Anchor{PlaceID: "11111111-1111-4111-8111-111111111111", Label: "原公开地点", CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.2, Longitude: -2.1}, SourceVersion: "2026-10-04T01:00:00Z"}
	v := View{SchemaVersion: SchemaVersion, CityID: "aberdeen-gb", Scope: Public, ObservedAt: at, ValidUntil: at.Add(time.Minute), Items: []Item{i}}
	if ValidateView(v) != nil {
		t.Fatal("public")
	}
	v.Items[0].Kind = "OPPORTUNITY"
	if ValidateView(v) == nil {
		t.Fatal("private in public")
	}
	v.Items[0] = i
	v.Items[0].Anchor.Precision = "area"
	if ValidateView(v) == nil {
		t.Fatal("noPoint")
	}
	v.Items[0] = i
	v.ValidUntil = at
	if ValidateView(v) == nil {
		t.Fatal("no finite future deadline")
	}
}
