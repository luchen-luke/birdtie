package agentreinforcement

import (
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"strings"
	"testing"
	"time"
)

var testIDs = []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"}

func testEntry(i int, anchors ...string) Entry {
	return Entry{EvidenceID: testIDs[i], EvidenceVersion: 1, Fingerprint: strings.Repeat("a", 64), Anchors: anchors}
}

func TestOfflineContractExplicitAssessmentAndTime(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	m, e := agentmemory.NewExplicit(testIDs[0], testIDs[1], actorref.PrincipalRef{Type: actorref.Person, ID: testIDs[2]}, 1, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "hiking", Summary: "本人合成声明", StructuredValue: []byte(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: now.Add(time.Hour)}, now, now)
	if e != nil {
		t.Fatal(e)
	}
	entry := testEntry(0, "MOMENT:"+testIDs[0])
	v, e := BuildView(m, 1, []Entry{entry}, &now, now)
	if e != nil || v.Assessment.Value == nil || *v.Assessment.Value != 1 || v.ModelAccess != "UNAVAILABLE" {
		t.Fatal("offline declared semantics", e)
	}
	for _, name := range []string{"inferred", "inactive", "confidence", "native-last-reinforced", "expired-exact", "future-support", "negative-version", "missing-support-time", "empty-with-support-time"} {
		t.Run(name, func(t *testing.T) {
			copy := m
			current := now
			last := now
			version := int64(1)
			entries := []Entry{entry}
			var lastPtr *time.Time = &last
			switch name {
			case "inferred":
				copy.SourceType = agentmemory.SourceInferred
				copy.Status = agentmemory.StatusPendingReview
				copy.Confidence = .5
			case "inactive":
				copy.Status = agentmemory.StatusExpired
			case "confidence":
				copy.Confidence = .9
			case "native-last-reinforced":
				copy.LastReinforcedAt = &now
			case "expired-exact":
				current = m.ValidUntil
			case "future-support":
				last = now.Add(time.Nanosecond)
			case "negative-version":
				version = -1
			case "missing-support-time":
				lastPtr = nil
			case "empty-with-support-time":
				entries = nil
			}
			if _, e := BuildView(copy, version, entries, lastPtr, current); e == nil {
				t.Fatal("invalid assessment or time accepted")
			}
		})
	}
}
func TestOfflineContractSourceClusters(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
		count   int
	}{
		{"empty", nil, 0},
		{"single", []Entry{testEntry(0, "ACTIVITY:"+testIDs[0])}, 1},
		{"same-event-cross-type", []Entry{testEntry(0, "ACTIVITY:"+testIDs[0]), testEntry(1, "ACTIVITY:"+testIDs[0])}, 1},
		{"independent-event", []Entry{testEntry(0, "ACTIVITY:"+testIDs[0]), testEntry(1, "ACTIVITY:"+testIDs[1])}, 2},
		{"transitive-overlap", []Entry{testEntry(0, "ACTIVITY:"+testIDs[0]), testEntry(1, "ACTIVITY:"+testIDs[0], "ACTIVITY:"+testIDs[1]), testEntry(2, "ACTIVITY:"+testIDs[1])}, 1},
		{"typed-namespaces", []Entry{testEntry(0, "MOMENT:"+testIDs[0]), testEntry(1, "PLACE:"+testIDs[0])}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, e := ClusterCount(c.entries)
			if e != nil || n != c.count {
				t.Fatalf("%d %v", n, e)
			}
			for i, j := 0, len(c.entries)-1; i < j; i, j = i+1, j-1 {
				c.entries[i], c.entries[j] = c.entries[j], c.entries[i]
			}
			n, e = ClusterCount(c.entries)
			if e != nil || n != c.count {
				t.Fatal("order dependence")
			}
		})
	}
}
func TestOfflineContractRejectMalformedSupport(t *testing.T) {
	cases := map[string]func(*Entry){"id": func(e *Entry) { e.EvidenceID = "unknown" }, "version": func(e *Entry) { e.EvidenceVersion = 0 }, "hash": func(e *Entry) { e.Fingerprint = strings.Repeat("A", 64) }, "empty-anchors": func(e *Entry) { e.Anchors = nil }, "unknown-kind": func(e *Entry) { e.Anchors = []string{"PERSON:" + testIDs[0]} }, "unknown-id": func(e *Entry) { e.Anchors = []string{"ACTIVITY:fake"} }, "duplicate-anchor": func(e *Entry) { e.Anchors = append(e.Anchors, e.Anchors[0]) }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			v := testEntry(0, "MOMENT:"+testIDs[0])
			change(&v)
			if _, e := NormalizeEntries([]Entry{v}); e != agentmemory.ErrInvalid {
				t.Fatal(e)
			}
		})
	}
	t.Run("duplicate-evidence", func(t *testing.T) {
		v := testEntry(0, "MOMENT:"+testIDs[0])
		if _, e := NormalizeEntries([]Entry{v, v}); e == nil {
			t.Fatal("duplicate accepted")
		}
	})
	t.Run("capacity", func(t *testing.T) {
		if _, e := NormalizeEntries(make([]Entry, 101)); e == nil {
			t.Fatal("capacity accepted")
		}
	})
	t.Run("copy", func(t *testing.T) {
		v := testEntry(0, "MOMENT:"+testIDs[0])
		out, e := NormalizeEntries([]Entry{v})
		if e != nil {
			t.Fatal(e)
		}
		out[0].Anchors[0] = "bad"
		if v.Anchors[0] == "bad" {
			t.Fatal("aliased input")
		}
	})
}
