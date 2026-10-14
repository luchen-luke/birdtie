package content

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

const publicationTestID = "ABCDEFAB-1234-4234-8234-1234567890AB"
const publicationPlaceID = "11111111-1111-4111-8111-111111111111"

func TestCollectionPublicationApprovalSelectorsAndDigest(t *testing.T) {
	in := CollectionPublicationInput{OperationID: publicationTestID, Revision: 7, Entries: []CollectionPublicationEntry{{PlaceID: publicationPlaceID, LocationVisibility: "hidden"}}}
	normalized, e := NormalizeCollectionPublication(in)
	if e != nil || normalized.OperationID != strings.ToLower(publicationTestID) {
		t.Fatal(normalized, e)
	}
	normalized.Entries[0].LocationVisibility = "place"
	if in.Entries[0].LocationVisibility != "hidden" {
		t.Fatal("normalization aliased caller approval")
	}
	original := CollectionPublicationDigest("journey", publicationPlaceID, "PUBLISH", in.Revision, in.Entries)
	changes := [][32]byte{CollectionPublicationDigest("theme_map", publicationPlaceID, "PUBLISH", 7, in.Entries), CollectionPublicationDigest("journey", strings.ToLower(publicationTestID), "PUBLISH", 7, in.Entries), CollectionPublicationDigest("journey", publicationPlaceID, "UNPUBLISH", 7, nil), CollectionPublicationDigest("journey", publicationPlaceID, "PUBLISH", 8, in.Entries), CollectionPublicationDigest("journey", publicationPlaceID, "PUBLISH", 7, normalized.Entries)}
	for _, digest := range changes {
		if digest == original {
			t.Fatal("changed concrete approval retained original digest")
		}
	}
	for _, change := range []func(*CollectionPublicationInput){func(v *CollectionPublicationInput) { v.Revision = 0 }, func(v *CollectionPublicationInput) { v.Revision = math.MaxInt64 }, func(v *CollectionPublicationInput) { v.OperationID = "invalid" }, func(v *CollectionPublicationInput) { v.Entries = nil }, func(v *CollectionPublicationInput) { v.Entries = append(v.Entries, v.Entries[0]) }, func(v *CollectionPublicationInput) { v.Entries[0].LocationVisibility = "public" }} {
		copy := in
		copy.Entries = append([]CollectionPublicationEntry(nil), in.Entries...)
		change(&copy)
		if _, e := NormalizeCollectionPublication(copy); e == nil {
			t.Fatal("invalid approval accepted", copy)
		}
	}
}
func TestCollectionPublicationPublicPrecisionHasNoPrivateSelectors(t *testing.T) {
	id := publicationPlaceID
	v := PublicCollection{ID: id, Kind: "journey", CityID: "aberdeen-gb", Title: "发布文本", Revision: 2, SourceExpiresAt: time.Now().Add(time.Minute), SourceBinding: "private-proof", ResponseProof: "private-native-object", Entries: []PublicCollectionEntry{{Note: "明示发布备注", LocationVisibility: "hidden"}, {LocationVisibility: "city"}, {LocationVisibility: "place", PlaceID: &id, PlaceName: "公开地点"}}}
	if e := ValidatePublicCollection(v, v.Kind, v.ID); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "private-proof") || strings.Contains(string(raw), "private-native-object") {
		t.Fatal("read fence leaked")
	}
	var dto map[string]any
	if json.Unmarshal(raw, &dto) != nil {
		t.Fatal("invalid wire")
	}
	entries := dto["entries"].([]any)
	for _, entry := range entries[:2] {
		m := entry.(map[string]any)
		for _, key := range []string{"placeId", "placeName", "latitude", "longitude"} {
			if _, ok := m[key]; ok {
				t.Fatal("redacted stop exported", key)
			}
		}
	}
	zero := 0.0
	for _, mutate := range []func(*PublicCollectionEntry){func(e *PublicCollectionEntry) { e.PlaceID = &id }, func(e *PublicCollectionEntry) { e.PlaceName = "leak" }, func(e *PublicCollectionEntry) { e.Latitude = &zero; e.Longitude = &zero }} {
		for _, precision := range []string{"hidden", "city"} {
			copy := v
			copy.Entries = append([]PublicCollectionEntry(nil), v.Entries...)
			copy.Entries[0].LocationVisibility = precision
			mutate(&copy.Entries[0])
			if ValidatePublicCollection(copy, copy.Kind, copy.ID) == nil {
				t.Fatal("private structured field accepted", precision)
			}
		}
	}
	copy := v
	copy.Entries = append([]PublicCollectionEntry(nil), v.Entries...)
	nan := math.NaN()
	copy.Entries[2].Latitude = &nan
	copy.Entries[2].Longitude = &zero
	if ValidatePublicCollection(copy, copy.Kind, copy.ID) == nil {
		t.Fatal("invalid coordinate")
	}
}

func TestCollectionPublicationCloseExactSelectorsAndClosedWire(t *testing.T) {
	valid := `{"kind":"journey","id":"` + publicationTestID + `","action":"PUBLISH","revision":7}`
	in, e := DecodeCollectionPublicationClose([]byte(valid))
	if e != nil || in.Kind != "journey" || in.ID != strings.ToLower(publicationTestID) || in.Action != "PUBLISH" || in.Revision != 7 {
		t.Fatal("original selectors not preserved", in, e)
	}
	for _, raw := range []string{
		`{}`, `[]`, `null`, valid + `{}`, valid + `null`,
		strings.Replace(valid, `"revision":7`, `"revision":7,"revision":8`, 1),
		strings.Replace(valid, `"action":"PUBLISH"`, `"action":null`, 1),
		strings.Replace(valid, `"revision":7`, `"revision":7,"entries":[]`, 1),
		strings.Replace(valid, `"revision":7`, `"revision":"7"`, 1),
		strings.Replace(valid, `"revision":7`, `"revision":1.5`, 1),
		strings.Replace(valid, `"revision":7`, `"revision":0`, 1),
		strings.Replace(valid, `"revision":7`, `"revision":9223372036854775807`, 1),
		strings.Replace(valid, `"kind":"journey"`, `"kind":"moment"`, 1),
		strings.Replace(valid, `"action":"PUBLISH"`, `"action":"CANCEL"`, 1),
		strings.Replace(valid, publicationTestID, "not-an-id", 1),
		strings.Repeat(" ", 4097) + valid,
	} {
		if _, e := DecodeCollectionPublicationClose([]byte(raw)); e == nil {
			t.Fatal("unbound or additional approval accepted", raw)
		}
	}
	for _, action := range []string{"PUBLISH", "UNPUBLISH"} {
		in.Action = action
		in.Kind = "theme_map"
		if _, e := NormalizeCollectionPublicationClose(in); e != nil {
			t.Fatal("valid original selector rejected", e)
		}
	}
}

func TestCollectionPublicationTerminalWireDoesNotInventResult(t *testing.T) {
	for _, outcome := range []string{"COMMITTED", "NO_EFFECT"} {
		meta := CollectionPublicationOperation{OperationID: strings.ToLower(publicationTestID), Action: "PUBLISH", Outcome: outcome, ExpectedRevision: 7}
		if outcome == "COMMITTED" {
			meta.ResultRevision = 8
		}
		raw, e := json.Marshal(meta)
		if e != nil {
			t.Fatal(e)
		}
		var wire map[string]any
		if json.Unmarshal(raw, &wire) != nil || wire["outcome"] != outcome {
			t.Fatal("terminal outcome missing", string(raw))
		}
		result, present := wire["resultRevision"]
		if outcome == "NO_EFFECT" && present || outcome == "COMMITTED" && (!present || result != float64(8)) {
			t.Fatal("terminal causality changed", string(raw))
		}
		if len(wire) != 4+boolCollectionPublicationInt(present) {
			t.Fatal("receipt carried extra body/proof", string(raw))
		}
	}
}

func boolCollectionPublicationInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
