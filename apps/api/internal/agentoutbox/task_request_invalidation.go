package agentoutbox

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

// This native lifecycle narrows old TASK_CONTEXT_READ only. It is not the
// public air.event UserQuery, a per-message clock, a Task counter or a grant.
const TaskRequestSchema = "agent-task-request-outbox-v1"
const TaskRequestChanged EventType = "TASK_REQUEST_CHANGED"
const TaskContextInvalidation EventType = "TASK_CONTEXT_INVALIDATION"
const TaskRequestSource SourceType = "TASK_REQUEST"
const TaskRequestHandler HandlerVersion = "task-request-invalidation-v1"
const TaskRequestActive SourceStatus = "ACTIVE"
const TaskRequestCompleted SourceStatus = "COMPLETED"
const TaskRequestFailed SourceStatus = "FAILED"
const MaxTaskRequestRootAttempts = MaxMemoryRootAttempts
const MaxTaskRequestCausalDepth = 1
const MaxTaskRequestPreviewCleanup = 128
const MaxTaskRequestGrantCleanup = 100

// Fingerprint is the native source epoch/projection, not a human operation key
// or 076 purpose token. A retained snapshot cannot confer execution authority.
func TaskRequestOperationID(sourceID, fingerprint string, kind EventType) string {
	h := sha256.Sum256([]byte("birdtie.task-request.operation.v1\x00" + sourceID + "\x00" + fingerprint + "\x00" + string(kind)))
	h[6] = h[6]&0x0f | 0x80
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func validateTaskRequestEnvelope(e Envelope, now time.Time) error {
	if e.SchemaVersion != TaskRequestSchema || !validPerson(e.Subject) || e.Tenant != e.Subject || e.Actor.Type != actorref.Person || e.Actor.ID != e.Subject.ID ||
		!validID(e.AgentID) || !validID(e.EventID) || e.Source.Type != TaskRequestSource || !validID(e.Source.ID) || e.Source.Owner != e.Subject || e.Source.Revision != 0 ||
		!digest(e.Source.Fingerprint) || (e.Source.Status != TaskRequestActive && e.Source.Status != TaskRequestCompleted && e.Source.Status != TaskRequestFailed) ||
		!validTime(e.OccurredAt) || !validTime(e.ReceivedAt) || !validTime(e.ExpiresAt) || !validTime(now) || e.OccurredAt.After(e.ReceivedAt) || e.ReceivedAt.After(now) ||
		!e.ExpiresAt.Equal(e.OccurredAt.Add(MaxEventTTL)) || e.LogicalOperationID != TaskRequestOperationID(e.Source.ID, e.Source.Fingerprint, e.EventType) ||
		e.RootTraceID != TaskRequestOperationID(e.Source.ID, e.Source.Fingerprint, TaskRequestChanged) || e.EventID != StableEventID(e) {
		return ErrInvalid
	}
	switch e.EventType {
	case TaskRequestChanged:
		if e.CausationID != nil {
			return ErrInvalid
		}
	case TaskContextInvalidation:
		parent := e
		parent.EventType, parent.LogicalOperationID, parent.CausationID = TaskRequestChanged, e.RootTraceID, nil
		if e.CausationID == nil || *e.CausationID != StableEventID(parent) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if !e.ExpiresAt.After(now) {
		return ErrExpired
	}
	raw, err := json.Marshal(struct{ Envelope }{e})
	if err != nil || len(raw) > MaxEnvelopeBytes {
		return ErrInvalid
	}
	return nil
}

// Structural causality only. The native Store separately locks and rechecks
// this actual original completed parent and same-fence consumer receipt.
func ValidateTaskRequestCausation(child Envelope, parent Record, receipt ConsumerRecord) error {
	if child.EventType != TaskContextInvalidation || ValidateEnvelope(child, child.ReceivedAt) != nil || ValidateRecord(parent) != nil ||
		parent.Event.EventType != TaskRequestChanged || parent.State != MemoryComplete || parent.Event.RootTraceID != child.RootTraceID || parent.Event.Source != child.Source ||
		parent.Event.Subject != child.Subject || parent.Event.AgentID != child.AgentID || !parent.Event.OccurredAt.Equal(child.OccurredAt) || !parent.Event.ExpiresAt.Equal(child.ExpiresAt) ||
		child.CausationID == nil || *child.CausationID != parent.Event.EventID || ValidateConsumerRecord(receipt) != nil || receipt.EventID != parent.Event.EventID ||
		receipt.Subject != child.Subject || receipt.HandlerVersion != TaskRequestHandler || receipt.State != MemoryComplete || receipt.Fence != parent.Fence ||
		receipt.Attempt != parent.Attempt || !child.ReceivedAt.Equal(receipt.UpdatedAt) || receipt.UpdatedAt.Before(parent.CreatedAt) || receipt.UpdatedAt.After(parent.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}
