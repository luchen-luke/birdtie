package agentdecay

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func sample() (Metadata, time.Time) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	return Metadata{MemoryID: "91000000-0000-0000-0000-000000000001", AgentID: "91000000-0000-0000-0000-000000000002", OwnerID: "91000000-0000-0000-0000-000000000003", Version: 3, SourceType: agentmemory.SourceInferred, Status: agentmemory.StatusPendingReview, Baseline: .8, CreatedAt: now.Add(-Grace - HalfLife), UpdatedAt: now.Add(-time.Hour), ValidFrom: now.Add(-180 * 24 * time.Hour), ValidUntil: now.Add(30 * 24 * time.Hour)}, now
}
func TestDecayBaselineWithoutCompounding(t *testing.T) {
	m, now := sample()
	original := m
	first, e := Project(m, now)
	if e != nil || *first.Assessment.Value != .4 || !first.Decayed || first.Assessment.Semantics != agentconfidence.UncalibratedScore || first.Status != agentmemory.StatusPendingReview || first.ModelAccess != "UNAVAILABLE" {
		t.Fatalf("half-life projection %+v %v", first, e)
	}
	second, e := Project(m, now)
	if e != nil || !reflect.DeepEqual(first, second) || !reflect.DeepEqual(original, m) {
		t.Fatal("read compounded or changed native input")
	}
	later, e := Project(m, now.Add(10*24*time.Hour))
	if e != nil || *later.Assessment.Value >= *first.Assessment.Value || later.MemoryVersion != m.Version {
		t.Fatal("time did not lower projected confidence")
	}
	m.UpdatedAt = now
	again, e := Project(m, now)
	if e != nil || *again.Assessment.Value != .4 || !again.AnchorAt.Equal(m.CreatedAt) {
		t.Fatal("updated/read time was invented as reinforcement")
	}
}
func TestDecayGraceAndRealReinforcement(t *testing.T) {
	for _, days := range []int{0, 29, 30, 31, 120, 210} {
		t.Run(strconv.Itoa(days)+"_days", func(t *testing.T) {
			m, now := sample()
			m.CreatedAt = now.Add(-time.Duration(days) * 24 * time.Hour)
			m.UpdatedAt = now
			v, e := Project(m, now)
			if e != nil {
				t.Fatal(e)
			}
			want := .8
			if days > 30 {
				want *= math.Exp2(-float64(days-30) / 90)
			}
			if math.Abs(*v.Assessment.Value-want) > 1e-12 || v.Decayed != (days > 30) {
				t.Fatalf("got %v want %v", *v.Assessment.Value, want)
			}
		})
	}
	m, now := sample()
	anchor := now.Add(-20 * 24 * time.Hour)
	m.LastReinforcedAt = &anchor
	v, e := Project(m, now)
	if e != nil || v.Decayed || *v.Assessment.Value != .8 || !v.AnchorAt.Equal(anchor) {
		t.Fatal("real reinforcement anchor not respected")
	}
	m.Baseline = 0
	v, e = Project(m, now)
	if e != nil || *v.Assessment.Value != 0 || v.Decayed {
		t.Fatal("zero baseline increased")
	}
}
func TestDecayExplicitNeverAutomatic(t *testing.T) {
	m, now := sample()
	m.SourceType = agentmemory.SourceExplicit
	m.Status = agentmemory.StatusActive
	m.Baseline = 1
	v, e := Project(m, now)
	if e != nil || v.Decayed || *v.Assessment.Value != 1 || v.Assessment.Semantics != agentconfidence.DirectDeclaration {
		t.Fatal("explicit declaration decayed or became probability")
	}
}
func TestDecayRejectsInvalidAndInactive(t *testing.T) {
	for _, name := range []string{"unknown_source", "inferred_active", "explicit_pending", "explicit_score", "explicit_anchor", "nan", "infinity", "negative", "above_one", "missing_now", "future_created", "future_updated", "bad_anchor", "future_anchor", "bad_id", "bad_owner", "zero_version", "future_validity", "expired", "deleted", "stored_expired", "too_long"} {
		t.Run(name, func(t *testing.T) {
			m, now := sample()
			want := ErrInvalid
			switch name {
			case "unknown_source":
				m.SourceType = "MODEL_CONFIRMED"
			case "inferred_active":
				m.Status = agentmemory.StatusActive
			case "explicit_pending":
				m.SourceType = agentmemory.SourceExplicit
				m.Baseline = 1
			case "explicit_score":
				m.SourceType = agentmemory.SourceExplicit
				m.Status = agentmemory.StatusActive
			case "explicit_anchor":
				m.SourceType = agentmemory.SourceExplicit
				m.Status = agentmemory.StatusActive
				m.Baseline = 1
				anchor := m.UpdatedAt
				m.LastReinforcedAt = &anchor
			case "nan":
				m.Baseline = math.NaN()
			case "infinity":
				m.Baseline = math.Inf(1)
			case "negative":
				m.Baseline = -.1
			case "above_one":
				m.Baseline = 1.1
			case "missing_now":
				now = time.Time{}
			case "future_created":
				m.CreatedAt = now.Add(time.Hour)
				m.UpdatedAt = m.CreatedAt
			case "future_updated":
				m.UpdatedAt = now.Add(time.Hour)
			case "bad_anchor":
				x := m.CreatedAt.Add(-time.Second)
				m.LastReinforcedAt = &x
			case "future_anchor":
				x := now.Add(time.Hour)
				m.LastReinforcedAt = &x
			case "bad_id":
				m.MemoryID = ""
			case "bad_owner":
				m.OwnerID = "business"
			case "zero_version":
				m.Version = 0
			case "future_validity":
				m.ValidFrom = now.Add(time.Hour)
				want = ErrNotFound
			case "expired":
				m.ValidUntil = now
				want = ErrNotFound
			case "deleted":
				m.Status = agentmemory.StatusDeleted
				want = ErrNotFound
			case "stored_expired":
				m.Status = agentmemory.StatusExpired
				want = ErrNotFound
			case "too_long":
				m.ValidFrom = now.Add(-366 * 24 * time.Hour)
			}
			v, e := Project(m, now)
			if !errors.Is(e, want) || !reflect.DeepEqual(v, View{}) {
				t.Fatalf("denied %s got %v want %v", name, e, want)
			}
		})
	}
}
