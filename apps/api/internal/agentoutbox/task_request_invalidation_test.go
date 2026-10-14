package agentoutbox

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func taskRequestCausalFixture() Envelope {
	e := memoryCausalFixture()
	e.SchemaVersion, e.EventType, e.Source.Type = TaskRequestSchema, TaskRequestChanged, TaskRequestSource
	e.Source.Revision, e.Source.Status = 0, TaskRequestActive
	e.LogicalOperationID = TaskRequestOperationID(e.Source.ID, e.Source.Fingerprint, e.EventType)
	e.RootTraceID = e.LogicalOperationID
	e.EventID = StableEventID(e)
	return e
}
func taskRequestCausalChild(e Envelope) Envelope {
	cause := e.EventID
	e.EventType, e.CausationID = TaskContextInvalidation, &cause
	e.LogicalOperationID = TaskRequestOperationID(e.Source.ID, e.Source.Fingerprint, e.EventType)
	e.EventID = StableEventID(e)
	return e
}
func TestTaskRequestInvalidationUnitOriginalEntryAndClosedSentinel(t *testing.T) {
	for _, status := range []SourceStatus{TaskRequestActive, TaskRequestCompleted, TaskRequestFailed} {
		e := taskRequestCausalFixture()
		e.Source.Status = status
		e.EventID = StableEventID(e)
		if ValidateEnvelope(e, e.ReceivedAt) != nil {
			t.Fatal("closed native lifecycle rejected", status)
		}
		r, err := NewPending(e, e.ReceivedAt)
		if err != nil || ValidateRecord(r) != nil {
			t.Fatal("actual original record path", err)
		}
		if got, err := DecodeEnvelope(mustTaskRequestJSON(t, e), e.ReceivedAt); err != nil || got != e {
			t.Fatal("original closed wire", err)
		}
		e.Source.Revision = 1
		e.EventID = StableEventID(e)
		if ValidateEnvelope(e, e.ReceivedAt) != ErrInvalid {
			t.Fatal("invented Task revision accepted")
		}
	}
	old := memoryCausalFixture()
	old.Source.Revision = 0
	old.LogicalOperationID = MemoryOperationID(old.Source.ID, 0, old.EventType)
	old.RootTraceID = old.LogicalOperationID
	old.EventID = StableEventID(old)
	if ValidateEnvelope(old, old.ReceivedAt) != ErrInvalid {
		t.Fatal("Task sentinel weakened old native revision")
	}
}
func mustTaskRequestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestTaskRequestInvalidationUnitExactDepthParentReceiptAndTTL(t *testing.T) {
	e := taskRequestCausalFixture()
	child := taskRequestCausalChild(e)
	parent, err := NewPending(e, e.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	parent.State, parent.Attempt, parent.Fence = MemoryComplete, 1, 1
	p := ConsumerRecord{EventID: e.EventID, Subject: e.Subject, HandlerVersion: TaskRequestHandler, State: MemoryComplete, Reason: ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: e.ReceivedAt, UpdatedAt: e.ReceivedAt}
	if ValidateEnvelope(child, child.ReceivedAt) != nil || ValidateTaskRequestCausation(child, parent, p) != nil {
		t.Fatal("native depth1 shape rejected")
	}
	for _, change := range []func(*Envelope){
		func(x *Envelope) { x.RootTraceID = x.LogicalOperationID },
		func(x *Envelope) { id := x.EventID; x.CausationID = &id },
		func(x *Envelope) { x.Source.Fingerprint = strings.Repeat("b", 64) },
		func(x *Envelope) { x.ExpiresAt = x.ExpiresAt.Add(time.Nanosecond) },
		func(x *Envelope) { x.EventType = TaskRequestChanged },
		func(x *Envelope) { x.Source.Type = MomentSource },
	} {
		x := child
		change(&x)
		if ValidateEnvelope(x, x.ReceivedAt) == nil {
			t.Fatal("loop/renewed/source-swapped event accepted")
		}
	}
	for _, change := range []func(*ConsumerRecord){
		func(x *ConsumerRecord) { x.Fence++ }, func(x *ConsumerRecord) { x.HandlerVersion = MemoryHandler },
		func(x *ConsumerRecord) { x.State = Pending; x.Reason = ReasonCleanupMore }, func(x *ConsumerRecord) { x.Subject.ID = x.EventID },
	} {
		x := p
		change(&x)
		if ValidateTaskRequestCausation(child, parent, x) == nil {
			t.Fatal("foreign/uncompleted receipt made child")
		}
	}
	if MaxTaskRequestRootAttempts != 6 || MaxTaskRequestCausalDepth != 1 || MaxTaskRequestPreviewCleanup != 128 || MaxTaskRequestGrantCleanup != 100 {
		t.Fatal("original bounded cleanup grew")
	}
}
func TestTaskRequestInvalidationUnitPrivateWireRejectsCapabilityAndReceiptInference(t *testing.T) {
	e := taskRequestCausalFixture()
	raw := string(mustTaskRequestJSON(t, e))
	for _, field := range []string{"query", "conversation", "filters", "session", "grant", "approved", "latitude"} {
		bad := strings.Replace(raw, `"schema_version":`, `"`+field+`":true,"schema_version":`, 1)
		if _, err := DecodeEnvelope([]byte(bad), e.ReceivedAt); err == nil {
			t.Fatal("raw body/capability field entered event", field)
		}
	}
	bad := strings.Replace(raw, `"revision":0`, `"revision":0,"revision":0`, 1)
	if _, err := DecodeEnvelope([]byte(bad), e.ReceivedAt); err == nil {
		t.Fatal("duplicate selector was discarded")
	}
	if raw, err := json.Marshal(Claim{}); !errors.Is(err, ErrAuthorityJSON) || len(raw) != 0 {
		t.Fatal("native claim serialized")
	}
	p := ConsumerRecord{EventID: e.EventID, Subject: e.Subject, HandlerVersion: TaskRequestHandler, State: MemoryComplete, Reason: ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: e.ReceivedAt, UpdatedAt: e.ReceivedAt}
	if ValidateConsumerRecord(p) != nil {
		t.Fatal("original common maintenance receipt rejected")
	}
	p.Subject.Type = actorref.Organization
	if ValidateConsumerRecord(p) != ErrInvalid {
		t.Fatal("organization inherits Person maintenance")
	}
}
