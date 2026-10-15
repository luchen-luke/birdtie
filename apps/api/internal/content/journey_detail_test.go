package content

import "testing"

func TestJourneyDetailsUnicodeOrderAndOriginalControlDigest(t *testing.T) {
	day := 1
	v := JourneyDetails{SchemaVersion: JourneyDetailsSchema, StartDate: "2026-10-15", DateLabel: "两天 · 自由行", Days: 2, Itinerary: []JourneyEntry{
		{ID: "15800000-0000-4000-8000-000000000001", EntryType: "STOP", Position: 90, Title: "城市 · 一"},
		{ID: "15800000-0000-4000-8000-000000000002", EntryType: "ARRANGEMENT", Position: 99, Title: "无地点安排", Note: "先逛街\n再休息", Kind: "custom", DayIndex: &day, StopID: "15800000-0000-4000-8000-000000000001", SourceLabel: "手动整理", SourceText: "保留原始说明"},
	}}
	in := CollectionControlInput{SchemaVersion: CollectionControlSchemaVersion, OperationID: "15800000-0000-4000-8000-000000000100", Action: "UPDATE", Kind: "journey", CollectionID: "15800000-0000-4000-8000-000000000200", ExpectedRevision: 3, Input: &CollectionControlBody{Kind: "journey", Summary: "旅程备注", Entries: []CollectionControlEntry{}, JourneyDetails: &v}}
	n, e := NormalizeCollectionControl(in)
	if e != nil {
		t.Fatal(e)
	}
	if n.Input.JourneyDetails.Itinerary[0].Position != 1 || n.Input.JourneyDetails.Itinerary[1].Position != 2 || n.Input.JourneyDetails.Itinerary[0].DayIndex != nil {
		t.Fatal("ordered material or nullable day changed")
	}
	if got := JourneyDetailsDigest(n.Input.JourneyDetails); got != "18ad141cc09e8c1f1e09948ecdfed4de76e7c92901a1c5a5dadf474b6f127aaa" {
		t.Fatal("schedule digest", got)
	}
	if got := CollectionControlDigest(n); got != "c4a544d9835305d9a781602585aefccc71041994e629b40db841ae030de34d78" {
		t.Fatal("147 digest", got)
	}
	empty := CollectionInput{Kind: "journey", Entries: []CollectionEntry{}, JourneyDetails: &JourneyDetails{SchemaVersion: JourneyDetailsSchema, Itinerary: []JourneyEntry{}}}
	if _, e = NormalizeCollectionInput(empty); e != nil {
		t.Fatal("empty no-City draft", e)
	}
	v = *n.Input.JourneyDetails
	v.Itinerary = append([]JourneyEntry{}, v.Itinerary...)
	v.Itinerary[0].Origin = &JourneyEntryOrigin{SourceID: v.Itinerary[0].ID}
	if _, e = NormalizeJourneyDetails(v); e == nil {
		t.Fatal("read provenance accepted as editable authority")
	}
}
