package agentmemorycorrection

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"math"
	"strings"
	"testing"
	"time"
)

const testID = "01100000-0000-4000-8000-000000000001"

func TestMemoryCorrectionClosedActions(t *testing.T) {
	for _, a := range []string{"DELETE", "NEGATE", "REJECT", "EDIT"} {
		t.Run(a, func(t *testing.T) {
			v := Input{ID: testID, TargetID: testID, ExpectedVersion: 1, TargetKind: "MEMORY", Action: a}
			if a == "NEGATE" {
				v.Category = "hiking"
			}
			if a == "REJECT" {
				v.TargetKind = "CANDIDATE"
			}
			if a == "EDIT" {
				v.Replacement = &agentmemory.PutInput{ExpectedVersion: 1, MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:hiking", Summary: "我偏好徒步活动", StructuredValue: json.RawMessage(`{"activityCategory":"hiking"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
			}
			if _, e := NormalizeInput(v); e != nil {
				t.Fatal(e)
			}
			v.ExpectedVersion = math.MaxInt64
			if _, e := NormalizeInput(v); e == nil {
				t.Fatal("overflow")
			}
		})
	}
	for _, s := range []string{"", "HIKING", "sensitive", "../hiking"} {
		if ValidCategory(s) {
			t.Fatal(s)
		}
	}
	if NegativeStatement("hiking") != "我不偏好徒步活动" {
		t.Fatal("statement")
	}
}
func TestMemoryCorrectionPreviewReceiptShape(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	in := Input{ID: testID, TargetID: testID, ExpectedVersion: 1, TargetKind: "MEMORY", Action: "DELETE"}
	p := Preview{SchemaVersion: Schema, ID: testID, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: testID}, AgentID: testID, Input: in, Memories: []agentmemory.Record{testMemoryRecord(t, now)}, Affected: []Target{{Kind: "MEMORY", ID: testID, Version: 1}}, PlanDigest: strings.Repeat("a", 64), ObservedAt: now, ExpiresAt: now.Add(time.Minute), Explanation: Explanation}
	if ValidatePreview(p) != nil {
		t.Fatal("shape")
	}
	p.Affected = append(p.Affected, p.Affected[0])
	if ValidatePreview(p) == nil {
		t.Fatal("duplicate")
	}
	r := Receipt{SchemaVersion: Schema, ID: testID, Owner: p.Owner, AgentID: testID, Target: p.Affected[0], Action: "DELETE", PlanDigest: p.PlanDigest, State: "PENDING", ObservedAt: now, ExpiresAt: p.ExpiresAt}
	if ValidateReceipt(r) != nil {
		t.Fatal("pending")
	}
	r.CurrentResultMatches = true
	if ValidateReceipt(r) == nil {
		t.Fatal("pending cannot claim success")
	}
}

func TestMemoryCorrectionMemoryRejectAndReceiptBinding(t *testing.T) {
	in := Input{ID: testID, TargetKind: "MEMORY", TargetID: testID, ExpectedVersion: 1, Action: "REJECT"}
	if _, e := NormalizeInput(in); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	mid := testID
	v := int64(2)
	at := now.Add(-time.Second)
	r := Receipt{SchemaVersion: Schema, ID: testID, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: testID}, AgentID: testID, Target: Target{Kind: "MEMORY", ID: testID, Version: 1}, Action: "REJECT", PlanDigest: strings.Repeat("a", 64), State: "COMMITTED", ObservedAt: now, ExpiresAt: now.Add(time.Minute), CommittedAt: &at, ResultMemoryID: &mid, ResultMemoryVersion: &v, CurrentResultMatches: true}
	if ValidateReceipt(r) != nil {
		t.Fatal("memory rejection receipt")
	}
	r.Target.Version = math.MaxInt64
	if ValidateReceipt(r) == nil {
		t.Fatal("overflow")
	}
	r.Target.Version = 1
	v = 3
	if ValidateReceipt(r) == nil {
		t.Fatal("wrong version")
	}
	v = 2
	r.Target.Kind = "CANDIDATE"
	if ValidateReceipt(r) == nil {
		t.Fatal("candidate cannot claim memory deletion")
	}
	r.ResultMemoryID = nil
	r.ResultMemoryVersion = nil
	r.CurrentResultMatches = false
	if ValidateReceipt(r) != nil {
		t.Fatal("candidate receipt")
	}
}
func TestMemoryCorrectionNegativePreviewDeadlineAndTarget(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	until := now.Add(agentmemory.MaxValidity)
	p := Preview{SchemaVersion: Schema, ID: testID, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: testID}, AgentID: testID, Input: Input{ID: testID, TargetKind: "MEMORY", TargetID: testID, ExpectedVersion: 1, Action: "NEGATE", Category: "hiking"}, Memories: []agentmemory.Record{testMemoryRecord(t, now)}, Affected: []Target{{Kind: "MEMORY", ID: testID, Version: 1}}, PlanDigest: strings.Repeat("a", 64), ObservedAt: now, ExpiresAt: now.Add(time.Minute), Explanation: Explanation, NewMemoryValidUntil: &until}
	if ValidatePreview(p) != nil {
		t.Fatal("negative preview")
	}
	until = until.Add(time.Second)
	if ValidatePreview(p) == nil {
		t.Fatal("extended declaration")
	}
	until = now.Add(agentmemory.MaxValidity)
	p.Affected[0].Version = 2
	if ValidatePreview(p) == nil {
		t.Fatal("wrong specific version")
	}
	p.Affected[0].Version = 1
	p.ModelAccess = true
	if ValidatePreview(p) == nil {
		t.Fatal("model authorization")
	}
}

func testMemoryRecord(t *testing.T, now time.Time) agentmemory.Record {
	t.Helper()
	m, e := agentmemory.NewExplicit(testID, testID, actorref.PrincipalRef{Type: actorref.Person, ID: testID}, 1, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:hiking", Summary: "我偏好徒步活动", StructuredValue: json.RawMessage(`{"activityCategory":"hiking"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: now.Add(time.Hour)}, now, now)
	if e != nil {
		t.Fatal(e)
	}
	return m
}

func TestMemoryCorrectionPreviewRequiresAffectedMemoryContents(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: testID}
	m, err := agentmemory.NewExplicit(testID, testID, owner, 1, agentmemory.PutInput{
		MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:hiking",
		Summary: "我偏好徒步活动", StructuredValue: json.RawMessage(`{"activityCategory":"hiking"}`),
		Visibility: agentmemory.VisibilityPrivate, ValidUntil: now.Add(time.Hour),
	}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	base := Preview{SchemaVersion: Schema, ID: testID, Owner: owner, AgentID: testID,
		Input:    Input{ID: testID, TargetKind: "MEMORY", TargetID: testID, ExpectedVersion: 1, Action: "DELETE"},
		Memories: []agentmemory.Record{m}, Affected: []Target{{Kind: "MEMORY", ID: testID, Version: 1}},
		PlanDigest: strings.Repeat("a", 64), ObservedAt: now, ExpiresAt: now.Add(time.Minute), Explanation: Explanation}
	if err := ValidatePreview(base); err != nil {
		t.Fatal("control preview rejected", err)
	}
	t.Run("missing_specific_target_memory", func(t *testing.T) {
		p := base
		p.Memories = []agentmemory.Record{}
		if ValidatePreview(p) == nil {
			t.Fatal("preview accepted without original target Memory contents")
		}
	})
	t.Run("missing_other_affected_memory", func(t *testing.T) {
		p := base
		p.Affected = append([]Target{}, base.Affected...)
		p.Affected = append(p.Affected, Target{Kind: "MEMORY", ID: "01100000-0000-4000-8000-000000000002", Version: 1})
		if ValidatePreview(p) == nil {
			t.Fatal("preview accepted with undisclosed affected Memory contents")
		}
	})
	t.Run("candidate_only_does_not_require_memory", func(t *testing.T) {
		p := base
		p.Input.TargetKind = "CANDIDATE"
		p.Input.Action = "REJECT"
		p.Affected = []Target{{Kind: "CANDIDATE", ID: testID, Version: 1}}
		p.Memories = []agentmemory.Record{}
		if err := ValidatePreview(p); err != nil {
			t.Fatal("candidate-only control rejected", err)
		}
	})
}
