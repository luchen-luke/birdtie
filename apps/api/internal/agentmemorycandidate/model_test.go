package agentmemorycandidate

import (
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"math"
	"strings"
	"testing"
	"time"
)

var candidateIDs = []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"}

func TestMemoryCandidateOfflineClosedDraft(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	a, _ := agentconfidence.NewOrdinal(agentconfidence.Low)
	d := Draft{Predicate: "ACTIVITY_CATEGORY", Category: "sports", Assessment: a, Sources: []Selector{{agentevent.MomentSource, candidateIDs[0]}}, ValidUntil: now.Add(time.Hour)}
	for _, category := range []string{"badminton", "basketball", "football", "sports", "culture", "hiking"} {
		t.Run(category, func(t *testing.T) {
			copy := d
			copy.Category = category
			if _, e := NormalizeDraft(copy, now); e != nil || Statement(category) == "" {
				t.Fatal(e)
			}
		})
	}
	for _, name := range []string{"sensitive", "unknown", "predicate", "expired-exact", "over-lease", "infinite-year", "empty", "duplicate", "unknown-source", "bad-id", "declaration", "calibrated", "nan", "infinity", "above-one", "negative", "bad-level"} {
		t.Run(name, func(t *testing.T) {
			copy := d
			copy.Sources = append([]Selector(nil), d.Sources...)
			switch name {
			case "sensitive":
				copy.Category = "health"
			case "unknown":
				copy.Category = "nightlife"
			case "predicate":
				copy.Predicate = "personality"
			case "expired-exact":
				copy.ValidUntil = now
			case "over-lease":
				copy.ValidUntil = now.Add(MaxLease + time.Nanosecond)
			case "infinite-year":
				copy.ValidUntil = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "empty":
				copy.Sources = nil
			case "duplicate":
				copy.Sources = append(copy.Sources, copy.Sources[0])
			case "unknown-source":
				copy.Sources[0].Type = "IMAGE"
			case "bad-id":
				copy.Sources[0].ID = "unknown"
			case "declaration":
				copy.Assessment = agentconfidence.NewDirectDeclaration()
			case "calibrated":
				v := 0.82
				copy.Assessment = agentconfidence.Assessment{Semantics: agentconfidence.CalibratedProbability, Value: &v}
			case "nan", "infinity", "above-one", "negative":
				v := map[string]float64{"nan": math.NaN(), "infinity": math.Inf(1), "above-one": 1.1, "negative": -0.1}[name]
				copy.Assessment = agentconfidence.Assessment{Semantics: agentconfidence.UncalibratedScore, Value: &v}
			case "bad-level":
				copy.Assessment.Level = "CONFIRMED"
			}
			if _, e := NormalizeDraft(copy, now); e == nil {
				t.Fatal("invalid accepted")
			}
		})
	}
}
func TestMemoryCandidateOfflineClusters(t *testing.T) {
	s := []Source{{Selector: Selector{agentevent.MomentSource, candidateIDs[0]}, Fingerprint: strings.Repeat("a", 64), Anchors: []string{"ACTIVITY:" + candidateIDs[2]}}, {Selector: Selector{agentevent.ParticipationSource, candidateIDs[1]}, Fingerprint: strings.Repeat("b", 64), Anchors: []string{"ACTIVITY:" + candidateIDs[2]}}}
	t.Run("same-activity", func(t *testing.T) {
		n, e := ClusterCount(s)
		if e != nil || n != 1 {
			t.Fatal(n, e)
		}
	})
	t.Run("independent", func(t *testing.T) {
		copy := append([]Source(nil), s...)
		copy[1].Anchors = []string{"PLACE:" + candidateIDs[2]}
		n, e := ClusterCount(copy)
		if e != nil || n != 2 {
			t.Fatal(n, e)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		if _, e := ClusterCount(append(s, s[0])); e == nil {
			t.Fatal("duplicate")
		}
	})
	t.Run("bad-anchor", func(t *testing.T) {
		copy := append([]Source(nil), s...)
		copy[1].Anchors = []string{"IMAGE:" + candidateIDs[2]}
		if _, e := ClusterCount(copy); e == nil {
			t.Fatal("image vote")
		}
	})
}
