package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

// The actual native reader calls this exact DTO mapper. This test neither opens
// a database nor claims real PostgreSQL ACL/lock/migration verification.
func TestFieldEvidenceNativeMemoryMapperUsesOriginalRecordMetadata(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	input := agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:badminton", Summary: "我不偏好羽毛球活动", StructuredValue: json.RawMessage(`{"activityCategory":"badminton","nature":"human-correction"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: now.Add(time.Hour)}
	m, e := agentmemory.NewExplicit("42000000-0000-4000-8000-000000000001", "42000000-0000-4000-8000-000000000002", actorref.PrincipalRef{Type: actorref.Person, ID: "42000000-0000-4000-8000-000000000003"}, 1, input, now, now.Add(-24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	r, e := nativeContextReviewMemory(m)
	if e != nil || r.MemoryType != m.MemoryType || r.ValidFrom == nil || r.CreatedAt == nil || !r.ValidFrom.Equal(m.ValidFrom) || !r.CreatedAt.Equal(m.CreatedAt) || r.Confidence.Semantics != agentconfidence.DirectDeclaration {
		t.Fatal("native fields fabricated/lost", e)
	}
	r.StructuredValue[0] = 'x'
	*r.CreatedAt = now
	if string(m.StructuredValue)[0] != '{' || m.CreatedAt.Equal(*r.CreatedAt) {
		t.Fatal("native record mutated")
	}
	for _, status := range []agentmemory.Status{agentmemory.StatusDeleted, agentmemory.StatusExpired, agentmemory.StatusPendingReview} {
		copy := m
		copy.Status = status
		if _, e = nativeContextReviewMemory(copy); e == nil {
			t.Fatal("non-current memory", status)
		}
	}
	m.SourceType = agentmemory.SourceInferred
	m.Status = agentmemory.StatusPendingReview
	m.Confidence = 0.86
	if _, e = nativeContextReviewMemory(m); e == nil {
		t.Fatal("inference became explicit context")
	}
}

func TestFieldEvidenceNativePublicProjectionKeepsOriginalEnvelope(t *testing.T) {
	b := acb.Bundle{Mode: acb.RulesPublicQuery, Places: make([]acb.PublicPlace, 100), FieldEvidenceSet: &acb.FieldEvidenceSet{GrantsAuthority: true}}
	if e := contextBuilderFieldEvidence(&b); e != nil || b.FieldEvidenceSet != nil || len(b.Places) != 100 {
		t.Fatal("new metadata changed public 100-place projection", e)
	}
	// Missing mandatory native metadata cannot become an empty successful set.
	b.Mode = acb.MachineTaskContext
	if contextBuilderFieldEvidence(&b) == nil || b.FieldEvidenceSet != nil {
		t.Fatal("unavailable native context fabricated evidence")
	}
}
