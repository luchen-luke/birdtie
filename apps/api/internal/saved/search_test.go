package saved

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSavedSearchCanonicalQueryAndPrivateNoteBoundary(t *testing.T) {
	q, e := NormalizeQuery(Query{Text: "  地点%_  ", Kind: "place", CityID: "aberdeen-gb"})
	if e != nil || q.Text != "地点%_" || !q.Filtered() {
		t.Fatal("literal public query", e)
	}
	for _, q := range []Query{{Text: strings.Repeat("界", 81)}, {Text: "名字\n备注"}, {Text: string([]byte{255})}, {Kind: "person"}, {CityID: strings.Repeat("x", 81)}} {
		if _, e := NormalizeQuery(q); e == nil {
			t.Fatal("unbounded or unknown query accepted")
		}
	}
	item := Item{ID: "10000000-0000-0000-0000-000000000001", TargetID: "20000000-0000-0000-0000-000000000001", Kind: "place", Available: true, CityID: "aberdeen-gb", Note: "私人备注不查询"}
	if ValidateSearchPage(Page{Items: []Item{item}}, Query{Kind: "place"}, "") != nil {
		t.Fatal("human note incorrectly removed")
	}
	item.Available = false
	if ValidateSearchPage(Page{Items: []Item{item}}, Query{Kind: "place"}, "") == nil {
		t.Fatal("hidden source entered filtered page")
	}
}
func TestSavedSearchCursorClosedAlphabetAndBounds(t *testing.T) {
	valid := base64.RawURLEncoding.EncodeToString(make([]byte, 48))
	if !ValidSearchCursor(valid) {
		t.Fatal("opaque location hint rejected")
	}
	for _, c := range []string{"x", valid + "=", valid + "+", valid + "/", strings.Repeat("A", 1025), " " + valid} {
		if ValidSearchCursor(c) {
			t.Fatal("malformed opaque cursor accepted")
		}
	}
}
