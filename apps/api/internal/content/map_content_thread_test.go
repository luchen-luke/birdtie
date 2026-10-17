package content

import (
	"testing"
	"time"
)

func mapThreadTestView(now time.Time) MapContentThread {
	owner, seed := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	m := MapContentMoment{SchemaVersion: MapContentMomentSchema, ViewerAccountID: owner, ID: seed, Revision: 1, Status: "draft", Visibility: "private", VisibilitySource: "SELF_PRIVATE", Body: "原正文", CreatedAt: now.Add(-time.Hour), Locations: []MapContentLocation{}, Media: []MapContentMedia{}, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), SourceVersion: MomentControlHash("moment")}
	return MapContentThread{SchemaVersion: MapContentThreadSchema, ViewerAccountID: owner, SeedMomentID: seed, SeedRevision: 1, SeedVisibilitySource: "SELF_PRIVATE", Status: "AVAILABLE", ThreadID: "33333333-3333-4333-8333-333333333333", Entries: []MapContentThreadEntry{{0, "UNAVAILABLE", nil}, {1, "AVAILABLE", &m}}, ObservedAt: now, ValidUntil: now.Add(20 * time.Second), SourceVersion: MomentControlHash("thread")}
}

func TestMapContentThreadSequenceRedactionAndLease(t *testing.T) {
	now := time.Now().UTC()
	if !mapThreadTestView(now).Valid(now) {
		t.Fatal("valid sequence with hidden first position rejected")
	}
	for name, edit := range map[string]func(*MapContentThread){
		"hidden text":       func(v *MapContentThread) { v.Entries[0].Detail = v.Entries[1].Detail },
		"duplicate":         func(v *MapContentThread) { v.Entries[0] = MapContentThreadEntry{0, "AVAILABLE", v.Entries[1].Detail} },
		"reordered":         func(v *MapContentThread) { v.Entries[0].Position = 2 },
		"short child lease": func(v *MapContentThread) { v.Entries[1].Detail.ValidUntil = now.Add(time.Second) },
		"foreign viewer":    func(v *MapContentThread) { v.Entries[1].Detail.ViewerAccountID = v.ThreadID },
		"changed seed":      func(v *MapContentThread) { v.SeedRevision++ },
		"no seed":           func(v *MapContentThread) { v.Entries[1] = MapContentThreadEntry{1, "UNAVAILABLE", nil} },
		"expired":           func(v *MapContentThread) { v.ValidUntil = now.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			v := mapThreadTestView(now)
			edit(&v)
			if v.Valid(now) {
				t.Fatal("unsafe thread accepted")
			}
		})
	}
	v := mapThreadTestView(now)
	v.Status = "NOT_A_THREAD"
	v.ThreadID = ""
	v.Entries = []MapContentThreadEntry{}
	if !v.Valid(now) {
		t.Fatal("explicit no-thread result rejected")
	}
	// A no-thread status may never hide an attached sequence.
	v.Entries = mapThreadTestView(now).Entries
	if v.Valid(now) {
		t.Fatal("no-thread response smuggled entries")
	}
}
