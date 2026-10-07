package agentcandidateretention

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"testing"
	"time"
)

const retentionTestID = "44000000-0000-4000-8000-000000000001"

func TestCandidateRetentionClosedSelection(t *testing.T) {
	s := Selection{AnalysisGrantID: retentionTestID, RetainUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)}
	if _, e := Normalize(s); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Selection){func(s *Selection) { s.AnalysisGrantID = "confirmed" }, func(s *Selection) { s.AnalysisGrantID = "00000000-0000-0000-0000-000000000000" }, func(s *Selection) { s.RetainUntil = time.Time{} }, func(s *Selection) { s.RetainUntil = s.RetainUntil.Add(time.Nanosecond) }} {
		bad := s
		mutate(&bad)
		if _, e := Normalize(bad); e == nil {
			t.Fatal(bad)
		}
	}
}
func TestCandidateRetentionMetadataReceiptDoesNotEnableWrite(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	p := Preview{SchemaVersion: Schema, State: "RECEIPT_ONLY", ID: retentionTestID, Purpose: Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: retentionTestID}, AgentID: retentionTestID, Selection: Selection{AnalysisGrantID: retentionTestID, RetainUntil: now.Add(time.Minute)}, ObservedAt: now, ExpiresAt: now.Add(time.Minute), ConsumedGrantID: retentionTestID, Explanation: "核实原提交；未写入候选"}
	if ValidatePreview(p) != nil {
		t.Fatal(p)
	}
	p.CandidateWriteAvailable = true
	if ValidatePreview(p) == nil {
		t.Fatal("permission receipt activated writer")
	}
	p.CandidateWriteAvailable = false
	p.Review = &Review{}
	if ValidatePreview(p) == nil {
		t.Fatal("receipt leaked proposal")
	}
}
func TestCandidateRetentionResolutionIsServerOnly(t *testing.T) {
	if _, e := json.Marshal(Resolution{}); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	var r Resolution
	if e := json.Unmarshal([]byte(`{}`), &r); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
}
