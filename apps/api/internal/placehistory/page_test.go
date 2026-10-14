package placehistory

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const pageTestPlace = "22222222-2222-4222-8222-222222222222"

func pageTestCursor() PageCursor {
	n := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	return PageCursor{Version: 1, PlaceID: pageTestPlace, WindowEnd: PageStamp(n), ValidUntil: PageStamp(n.Add(PageLease)), BeforeAt: PageStamp(n.Add(-time.Hour)), BeforeID: "11111111-1111-4111-8111-111111111111", Source: strings.Repeat("a", 64)}
}
func TestPlaceHistoryPageCursorClosedAndBound(t *testing.T) {
	c := pageTestCursor()
	raw, e := EncodePageCursor(c)
	if e != nil {
		t.Fatal(e)
	}
	got, e := DecodePageCursor(raw, c.PlaceID, false)
	if e != nil || got != c {
		t.Fatal("canonical roundtrip", e)
	}
	for _, mode := range []string{"other_place", "other_author", "padding", "duplicate", "unknown", "future_last", "expiry", "uppercase"} {
		t.Run(mode, func(t *testing.T) {
			copy := c
			place, author := c.PlaceID, false
			candidate := raw
			switch mode {
			case "other_place":
				place = c.BeforeID
			case "other_author":
				author = true
			case "padding":
				candidate += "="
			case "duplicate":
				b, _ := base64.RawURLEncoding.DecodeString(raw)
				candidate = base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(b), `"version":1`, `"version":1,"version":1`, 1)))
			case "unknown":
				b, _ := base64.RawURLEncoding.DecodeString(raw)
				candidate = base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(b), `"version":1`, `"private":"no","version":1`, 1)))
			case "future_last":
				copy.BeforeAt = "2026-10-12T00:00:00.000000Z"
			case "expiry":
				copy.ValidUntil = "2026-10-11T00:03:00.000000Z"
			case "uppercase":
				copy.Source = strings.Repeat("A", 64)
			}
			if copy != c {
				if _, e := EncodePageCursor(copy); e == nil {
					t.Fatal("invalid cursor encoded")
				}
				return
			}
			if _, e := DecodePageCursor(candidate, place, author); e == nil {
				t.Fatal("invalid cross-scope cursor accepted")
			}
		})
	}
}
func TestPlaceHistoryPageStableSameTimestampOrder(t *testing.T) {
	c := pageTestCursor()
	end, until, _, _ := CursorTimes(c)
	m := Moment{ID: c.BeforeID, Title: "公开", Revision: 2, PublishedAt: end.Add(-time.Hour)}
	p := Page{SchemaVersion: PageSchemaVersion, PlaceID: c.PlaceID, CityID: "test", AuthorMode: "NONE", WindowDays: 30, WindowStart: end.Add(-30 * 24 * time.Hour), WindowEnd: end, ObservedAt: end, ValidUntil: until, SourceVersion: c.Source, Count: 2, Items: []MomentWithAuthor{{Moment: m}}}
	second := m
	second.ID = "00000000-0000-4000-8000-000000000000"
	p.Items = append(p.Items, MomentWithAuthor{Moment: second})
	q := PageQuery{PlaceID: c.PlaceID}
	if ValidatePage(p, q) != nil {
		t.Fatal("descending tie order refused")
	}
	p.Items[0], p.Items[1] = p.Items[1], p.Items[0]
	if ValidatePage(p, q) == nil {
		t.Fatal("ascending tie order accepted")
	}
}
