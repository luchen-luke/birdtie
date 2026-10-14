package content

import (
	"strings"
	"testing"
)

func collectionValid() CollectionInput {
	return CollectionInput{Kind: "journey", CityID: "aberdeen-gb", Title: "周末路线", Entries: []CollectionEntry{{PlaceID: "11111111-1111-4111-8111-111111111111", LocationVisibility: "hidden"}}}
}
func TestPersonalCollectionNormalizeBoundaries(t *testing.T) {
	for _, change := range []func(*CollectionInput){func(p *CollectionInput) { p.Kind = "public" }, func(p *CollectionInput) { p.Title = strings.Repeat("路", 54) }, func(p *CollectionInput) { p.Entries = nil }, func(p *CollectionInput) { p.Entries = append(p.Entries, p.Entries[0]) }, func(p *CollectionInput) { p.Entries[0].LocationVisibility = "precise" }, func(p *CollectionInput) { p.Entries[0].Available = true }, func(p *CollectionInput) { p.Entries[0].PlaceName = "forged" }, func(p *CollectionInput) { p.Summary = string([]byte{255}) }} {
		p := collectionValid()
		change(&p)
		if _, e := NormalizeCollectionInput(p); e != ErrInvalid {
			t.Fatal("invalid collection accepted", p, e)
		}
	}
	p := collectionValid()
	p.Kind = "theme_map"
	p.Title = " 路线 "
	v, e := NormalizeCollectionInput(p)
	if e != nil || v.Title != "路线" {
		t.Fatal(v, e)
	}
	first := CollectionInputDigest(v)
	v.Entries[0].Note = "修改"
	if first == CollectionInputDigest(v) {
		t.Fatal("changed draft reused operation digest")
	}
}
