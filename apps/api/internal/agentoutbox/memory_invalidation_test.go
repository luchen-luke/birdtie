package agentoutbox

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"testing"
	"time"
)

func memoryCausalFixture() Envelope {
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	p := actorref.PrincipalRef{Type: actorref.Person, ID: "82000000-0000-4000-8000-000000000001"}
	e := Envelope{SchemaVersion: "agent-memory-outbox-v1", EventType: "MEMORY_UPDATED", Tenant: p, Subject: p,
		Actor: actorref.ActorRef{Type: actorref.Person, ID: p.ID}, AgentID: "82000000-0000-4000-8000-000000000003",
		LogicalOperationID: "82000000-0000-4000-8000-000000000005", RootTraceID: "82000000-0000-4000-8000-000000000005",
		Source:     SourceReference{Type: "MEMORY", ID: "82000000-0000-4000-8000-000000000004", Owner: p, Revision: 2, Status: "ACTIVE", Fingerprint: strings.Repeat("a", 64)},
		OccurredAt: at, ReceivedAt: at, ExpiresAt: at.Add(MaxEventTTL)}
	e.EventID = StableEventID(e)
	e.LogicalOperationID = MemoryOperationID(e.Source.ID, e.Source.Revision, e.EventType)
	e.RootTraceID = e.LogicalOperationID
	e.EventID = StableEventID(e)
	return e
}

func TestMemoryCausalUnitActualEntry(t *testing.T) {
	e := memoryCausalFixture()
	if err := ValidateEnvelope(e, e.ReceivedAt); err != nil {
		t.Fatalf("真实已有 Envelope 入口仍拒绝受限 Memory 失效事件: %v", err)
	}
}

func memoryCausalChild(e Envelope) Envelope {
	cause := e.EventID
	e.EventType = MemoryContextInvalidation
	e.LogicalOperationID = MemoryOperationID(e.Source.ID, e.Source.Revision, e.EventType)
	e.CausationID = &cause
	e.EventID = StableEventID(e)
	return e
}

func TestMemoryCausalUnitClosedRootAndDepth(t *testing.T) {
	e := memoryCausalFixture()
	child := memoryCausalChild(e)
	if ValidateEnvelope(child, child.ReceivedAt) != nil || child.RootTraceID != e.RootTraceID || child.EventID == e.EventID {
		t.Fatal("fixed child rejected")
	}
	for _, change := range []func(*Envelope){
		func(x *Envelope) { x.RootTraceID = x.LogicalOperationID },
		func(x *Envelope) { cause := x.EventID; x.CausationID = &cause },
		func(x *Envelope) { x.EventType = MemoryUpdated },
		func(x *Envelope) { x.Source.Revision++ },
		func(x *Envelope) { x.Subject.ID = x.AgentID },
	} {
		x := child
		change(&x)
		if ValidateEnvelope(x, x.ReceivedAt) == nil {
			t.Fatal("untrusted/root/self-loop metadata accepted")
		}
	}
	if ValidateMemoryRootBudget(5) != nil || ValidateMemoryRootBudget(6) != ErrConflict || ValidateMemoryRootBudget(-1) != ErrInvalid {
		t.Fatal("shared root cap")
	}
	r, err := NewPending(e, e.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	r.State, r.Attempt, r.Fence = MemoryComplete, 1, 1
	p := ConsumerRecord{EventID: e.EventID, Subject: e.Subject, HandlerVersion: MemoryHandler, State: MemoryComplete, Reason: ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: e.ReceivedAt, UpdatedAt: e.ReceivedAt}
	if ValidateMemoryCausation(child, r, p) != nil {
		t.Fatal("same source completed parent rejected")
	}
	p.Fence++
	if ValidateMemoryCausation(child, r, p) == nil {
		t.Fatal("other receipt fence authorizes child")
	}
	if raw, err := json.Marshal(Claim{}); !errors.Is(err, ErrAuthorityJSON) || len(raw) != 0 {
		t.Fatal("claim serialized")
	}
}

func TestMemoryCausalUnitCommonReceiptShapePreserved(t *testing.T) {
	e := memoryCausalFixture()
	p := ConsumerRecord{EventID: e.EventID, Subject: e.Subject, HandlerVersion: MemoryHandler, State: MemoryComplete, Reason: ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: e.ReceivedAt, UpdatedAt: e.ReceivedAt}
	if ValidateConsumerRecord(p) != nil {
		t.Fatal("valid closed receipt rejected")
	}
	for _, change := range []func(*ConsumerRecord){
		func(x *ConsumerRecord) { x.Subject.ID = "" },
		func(x *ConsumerRecord) { x.Subject.Type = actorref.Organization },
		func(x *ConsumerRecord) { x.Fence = 0 },
		func(x *ConsumerRecord) { x.Attempt = 0 },
		func(x *ConsumerRecord) { x.Attempt = 2 },
		func(x *ConsumerRecord) { x.CreatedAt = time.Time{} },
		func(x *ConsumerRecord) { x.UpdatedAt = x.CreatedAt.Add(-time.Second) },
	} {
		x := p
		change(&x)
		if ValidateConsumerRecord(x) != ErrInvalid {
			t.Fatal("Memory dispatch bypassed original common shape")
		}
	}
}
