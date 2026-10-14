package legacyarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

const fixtureID = "a3000000-0000-4000-8000-000000000001"

type privateProof struct{}

func (privateProof) RevalidateHumanControl(context.Context) error { return nil }
func fixtureSummary() Summary {
	return Summary{ID: fixtureID, Source: SourceSelector{System: SourceSystem, Kind: "note", ID: fixtureID}, SourceVersion: 19, SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "", SourceVisibility: "public", SourceModerationStatus: "ok", ImportedAt: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), ResponseProof: privateProof{}}
}
func TestLegacyArchiveEmptyTitleUnknownDateLiteralBodyAndPrivateProof(t *testing.T) {
	r := Relationships{Places: []PlaceSelector{{RawName: "原未匹配", Position: 7}}, Media: []MediaSelector{{SourceMediaID: fixtureID, Position: 3, State: "UNAVAILABLE"}}, MediaLocations: []MediaLocation{}, ThreadLegs: []ThreadLeg{}}
	d := Detail{Summary: fixtureSummary(), Body: "<script>仅原文，不执行</script>", Relationships: &r}
	if e := ValidateDetail(d); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range [][]byte{[]byte("ResponseProof"), []byte("approval"), []byte("public_url"), []byte("ownerAccountId"), []byte("Payload"), []byte("RawSource")} {
		if bytes.Contains(raw, key) {
			t.Fatal("non-DTO authority leaked")
		}
	}
	var x map[string]any
	if e = json.Unmarshal(raw, &x); e != nil {
		t.Fatal(e)
	}
	if x["title"] != "" || x["recordedStartDate"] != nil || x["body"] != d.Body {
		t.Fatal("original empty/unknown/literal values replaced")
	}
}
func TestLegacyArchiveDeletedCannotRestoreTextDateRelationships(t *testing.T) {
	s := fixtureSummary()
	s.Deleted = true
	s.SourceVisibility = ""
	s.SourceModerationStatus = ""
	d := Detail{Summary: s}
	if e := ValidateDetail(d); e != nil {
		t.Fatal(e)
	}
	changes := []func(*Detail){func(d *Detail) { d.Title = "旧题" }, func(d *Detail) { d.Body = "旧正文" }, func(d *Detail) { v := "2020-01-01"; d.RecordedStartDate = &v }, func(d *Detail) { d.Relationships = &Relationships{} }, func(d *Detail) { d.SourceVisibility = "public" }}
	for i, change := range changes {
		x := d
		change(&x)
		if ValidateDetail(x) == nil {
			t.Fatalf("tombstone branch %d reintroduced content", i)
		}
	}
}
func TestLegacyArchiveRelationshipDecoderRejectsURLPermissionAndPreservesNumber(t *testing.T) {
	raw := []byte(`{"places":[],"media":[{"sourceMediaId":"a3000000-0000-4000-8000-000000000001","position":0,"state":"UNAVAILABLE"}],"mediaLocations":[{"sourceMediaId":"a3000000-0000-4000-8000-000000000001","latitude":25.123456789123456789,"longitude":null,"mapEnabled":true,"sourcePlaceIds":[],"capturedDate":null}],"thread":null,"threadLegs":[]}`)
	v, e := DecodeRelationships(raw)
	if e != nil || v.MediaLocations[0].Latitude.String() != "25.123456789123456789" || !v.MediaLocations[0].MapEnabled {
		t.Fatal("historical numeric/flag lost")
	}
	for _, bad := range [][]byte{bytes.Replace(raw, []byte(`"UNAVAILABLE"`), []byte(`"READY"`), 1), bytes.Replace(raw, []byte(`"state":"UNAVAILABLE"`), []byte(`"state":"READY","state":"UNAVAILABLE"`), 1), bytes.Replace(raw, []byte(`"state":"UNAVAILABLE"`), []byte(`"state":"UNAVAILABLE","public_url":"secret"`), 1), append(append([]byte(nil), raw...), []byte(`{}`)...)} {
		if _, e = DecodeRelationships(bad); e == nil {
			t.Fatal("media capability or trailing source accepted")
		}
	}
	n := json.Number("true")
	v.MediaLocations[0].Latitude = &n
	if ValidateRelationships(v) == nil {
		t.Fatal("non-number disguised as source coordinate")
	}
}
