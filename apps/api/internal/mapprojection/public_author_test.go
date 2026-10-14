package mapprojection

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

const mapAuthorUnitID = "27000000-0000-4000-8000-000000000011"

func mapAuthorUnitQuery(t *testing.T, author string, private bool) Query {
	t.Helper()
	var q Query
	raw, _ := json.Marshal(map[string]any{"CityID": "aberdeen-gb", "West": -2.2, "South": 57.1, "East": -2.0, "North": 57.3, "Private": private, "AuthorID": author})
	if e := json.Unmarshal(raw, &q); e != nil {
		t.Fatal(e)
	}
	return q
}

func mapAuthorUnitView(t *testing.T) View {
	t.Helper()
	var v View
	raw := fmt.Sprintf(`{"schemaVersion":"typed-map-layers-v2","cityId":"aberdeen-gb","scope":"PUBLIC_AUTHOR","authorId":%q,"observedAt":"2026-10-09T00:00:00Z","validUntil":"2026-10-09T00:01:00Z","truncated":false,"items":[]}`, mapAuthorUnitID)
	if e := json.Unmarshal([]byte(raw), &v); e != nil {
		t.Fatal(e)
	}
	return v
}

func TestMapPublicAuthorQueryClosedScope(t *testing.T) {
	for _, tc := range []struct {
		name, id       string
		private, valid bool
	}{
		{"public_author", mapAuthorUnitID, false, true},
		{"v1_public", "", false, true},
		{"v1_private", "", true, true},
		{"private_author", mapAuthorUnitID, true, false},
		{"invalid_author", "not-an-id", false, false},
		{"trimmed_author", " " + mapAuthorUnitID, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := mapAuthorUnitQuery(t, tc.id, tc.private)
			if (ValidateQuery(q) == nil) != tc.valid {
				t.Fatal("author/private scope", q)
			}
		})
	}
	a, b := mapAuthorUnitQuery(t, mapAuthorUnitID, false), mapAuthorUnitQuery(t, "27000000-0000-4000-8000-000000000012", false)
	if a == b {
		t.Fatal("different public authors collapsed into one receipt query")
	}
}

func TestMapPublicAuthorViewClosedWireAndKinds(t *testing.T) {
	for _, mode := range []string{"empty", "moment", "place", "activity", "organization", "business", "opportunity", "missing_author", "bad_author", "v1_schema", "public_scope", "private_scope", "duplicate", "over_bound"} {
		t.Run(mode, func(t *testing.T) {
			v := mapAuthorUnitView(t)
			i := Item{Kind: "MOMENT", ID: mapAuthorUnitID, Title: "明确公开动态", Entity: Ref{Type: "MOMENT", ID: mapAuthorUnitID}, Detail: Ref{Type: "MOMENT", ID: mapAuthorUnitID}, Anchor: Anchor{PlaceID: "27000000-0000-4000-8000-000000000013", Label: "合法地点", CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.2, Longitude: -2.1}, SourceVersion: "3"}
			valid := mode == "empty" || mode == "moment"
			switch mode {
			case "moment":
				v.Items = []Item{i}
			case "place", "activity", "organization", "business", "opportunity":
				kinds := map[string]string{"place": "PLACE", "activity": "ACTIVITY", "organization": "ORGANIZATION", "business": "BUSINESS", "opportunity": "OPPORTUNITY"}
				i.Kind = kinds[mode]
				i.Entity.Type, i.Detail.Type = i.Kind, i.Kind
				v.Items = []Item{i}
			case "missing_author", "bad_author":
				raw, _ := json.Marshal(v)
				var fields map[string]any
				json.Unmarshal(raw, &fields)
				if mode == "missing_author" {
					delete(fields, "authorId")
				} else {
					fields["authorId"] = "fake"
				}
				raw, _ = json.Marshal(fields)
				v = View{}
				json.Unmarshal(raw, &v)
			case "v1_schema":
				v.SchemaVersion = SchemaVersion
			case "public_scope":
				v.Scope = Public
			case "private_scope":
				v.Scope = SelfPrivate
			case "duplicate":
				v.Items = []Item{i, i}
			case "over_bound":
				for n := 0; n < 31; n++ {
					item := i
					item.ID = fmt.Sprintf("27000000-0000-4000-8000-%012d", n+100)
					item.Entity.ID, item.Detail.ID = item.ID, item.ID
					v.Items = append(v.Items, item)
				}
			}
			if (ValidateView(v) == nil) != valid {
				t.Fatal("author view closed scope", mode, v)
			}
		})
	}
}

func TestMapPublicAuthorDoesNotChangeV1Shape(t *testing.T) {
	at := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for _, scope := range []string{Public, SelfPrivate} {
		v := View{SchemaVersion: SchemaVersion, CityID: "aberdeen-gb", Scope: scope, ObservedAt: at, ValidUntil: at.Add(time.Minute), Items: []Item{}}
		if ValidateView(v) != nil {
			t.Fatal("original empty v1 invalid", scope)
		}
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		var fields map[string]json.RawMessage
		json.Unmarshal(raw, &fields)
		for _, key := range []string{"schemaVersion", "cityId", "scope", "observedAt", "validUntil", "truncated", "items"} {
			if fields[key] == nil {
				t.Fatal("original v1 key missing", key)
			}
		}
		if len(fields) != 7 || fields["authorId"] != nil {
			t.Fatal("v1 shape changed", string(raw))
		}
	}
}

func TestMapPublicAuthorViewQueryIdentity(t *testing.T) {
	for _, mode := range []string{"current", "another_author", "no_author", "private", "other_city"} {
		t.Run(mode, func(t *testing.T) {
			v := mapAuthorUnitView(t)
			q := mapAuthorUnitQuery(t, mapAuthorUnitID, false)
			switch mode {
			case "another_author":
				q.AuthorID = "27000000-0000-4000-8000-000000000012"
			case "no_author":
				q.AuthorID = ""
			case "private":
				q.Private = true
			case "other_city":
				q.CityID = "different-city"
			}
			if (ValidateViewForQuery(v, q) == nil) != (mode == "current") {
				t.Fatal("author projection substituted into another query", mode)
			}
		})
	}
}
