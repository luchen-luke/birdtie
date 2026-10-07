package agentoutbox

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func preferenceUnitOperation(source string, version int64, kind string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("birdtie.preference.operation.v1\x00%s\x00%d\x00%s", source, version, kind)))
	h[6] = h[6]&0x0f | 0x80
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
func preferenceUnitEnvelope() Envelope {
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	p := actorref.PrincipalRef{Type: actorref.Person, ID: "20000000-0000-4000-8000-000000000001"}
	id := "20000000-0000-4000-8000-000000000002"
	op := preferenceUnitOperation(id, 3, "PREFERENCE_UPDATED")
	e := Envelope{SchemaVersion: "agent-preference-outbox-v1", EventType: "PREFERENCE_UPDATED", Tenant: p, Subject: p, Actor: actorref.ActorRef{Type: actorref.Person, ID: p.ID}, AgentID: id,
		LogicalOperationID: op, RootTraceID: op, OccurredAt: at, ReceivedAt: at, ExpiresAt: at.Add(MaxEventTTL), Source: SourceReference{Type: "PRIVATE_PREFERENCE", ID: id, Owner: p, Revision: 3, Status: "configured", Fingerprint: strings.Repeat("a", 64)}}
	e.EventID = StableEventID(e)
	return e
}
func TestPreferenceInvalidationClosedSourceContract(t *testing.T) {
	e := preferenceUnitEnvelope()
	if err := ValidateEnvelope(e, e.ReceivedAt); err != nil {
		t.Fatalf("actual closed contract has no real private-preference source: %v", err)
	}
}

func TestPreferenceInvalidationCausalAndCommonShape(t *testing.T) {
	e := preferenceUnitEnvelope()
	parent, err := NewPending(e, e.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	parent.State = MemoryComplete
	parent.Attempt = 1
	parent.Fence = 1
	parent.UpdatedAt = e.ReceivedAt.Add(time.Second)
	receipt := ConsumerRecord{EventID: e.EventID, Subject: e.Subject, HandlerVersion: PreferenceHandler, State: MemoryComplete, Reason: ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: parent.CreatedAt, UpdatedAt: parent.UpdatedAt}
	child := e
	child.EventType = PreferenceContextInvalidation
	child.LogicalOperationID = PreferenceOperationID(e.Source.ID, e.Source.Revision, child.EventType)
	child.CausationID = &e.EventID
	child.ReceivedAt = receipt.UpdatedAt
	child.EventID = StableEventID(child)
	if err = ValidatePreferenceCausation(child, parent, receipt); err != nil {
		t.Fatal(err)
	}
	receipt.Fence++
	if ValidatePreferenceCausation(child, parent, receipt) == nil {
		t.Fatal("mismatched original parent fence accepted")
	}
	for _, tc := range []struct {
		name   string
		change func(*Envelope)
	}{
		{"fake_revision", func(e *Envelope) { e.Source.Revision = 1 }},
		{"foreign_source", func(e *Envelope) { e.Source.ID = e.Subject.ID }},
		{"self_loop", func(e *Envelope) { e.CausationID = &e.EventID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := e
			tc.change(&v)
			if ValidateEnvelope(v, v.ReceivedAt) == nil {
				t.Fatal("unsupported source/depth accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*ConsumerRecord)
	}{
		{"bad_subject", func(r *ConsumerRecord) { r.Subject.ID = "invalid" }},
		{"bad_fence", func(r *ConsumerRecord) { r.Fence = 0 }},
		{"bad_attempt", func(r *ConsumerRecord) { r.Attempt = 0 }},
		{"bad_time", func(r *ConsumerRecord) { r.UpdatedAt = time.Time{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := receipt
			v.Fence = 1
			tc.change(&v)
			if ValidateConsumerRecord(v) == nil {
				t.Fatal("new family bypassed original common shape")
			}
		})
	}
	clear := e
	clear.Source.Status = PreferenceCleared
	clear.EventID = StableEventID(clear)
	if ValidateEnvelope(clear, clear.ReceivedAt) != nil || MaxPreferenceRootAttempts != 6 || MaxPreferenceCausalDepth != 1 {
		t.Fatal("closed clear/budget contract")
	}
}
