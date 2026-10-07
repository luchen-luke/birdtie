package agentoutbox

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"math"
	"time"
)

// A closed maintenance chain, not AIR's public event ingress or cognition grant.
const MemorySchema = "agent-memory-outbox-v1"
const MemoryUpdated EventType = "MEMORY_UPDATED"
const MemoryContextInvalidation EventType = "MEMORY_CONTEXT_INVALIDATION"
const MemorySource SourceType = "MEMORY"
const MemoryActive SourceStatus = "ACTIVE"
const MemoryDeleted SourceStatus = "DELETED"
const MemoryHandler HandlerVersion = "memory-invalidation-v1"
const MemoryComplete State = "INVALIDATION_COMPLETE"
const ReasonCleanupComplete ReasonCode = "CLEANUP_COMPLETE"
const ReasonCleanupMore ReasonCode = "CLEANUP_MORE"
const ReasonRootExhausted ReasonCode = "ROOT_BUDGET_EXHAUSTED"
const MaxMemoryRootAttempts = ar.MaxRootAttempts
const MaxMemoryCausalDepth = 1
const MaxMemoryPreviewCleanup = 128
const MaxMemoryGrantCleanup = 100

func MemoryOperationID(source string, revision int64, kind EventType) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("birdtie.memory.operation.v1\x00%s\x00%d\x00%s", source, revision, kind)))
	h[6] = h[6]&0x0f | 0x80
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func validateMemoryEnvelope(e Envelope, now time.Time) error {
	if e.SchemaVersion != MemorySchema || !validPerson(e.Subject) || e.Tenant != e.Subject || e.Actor.Type != actorref.Person || e.Actor.ID != e.Subject.ID ||
		!validID(e.AgentID) || !validID(e.EventID) || e.Source.Type != MemorySource || !validID(e.Source.ID) || e.Source.Owner != e.Subject || e.Source.Revision < 1 ||
		!digest(e.Source.Fingerprint) || (e.Source.Status != MemoryActive && e.Source.Status != MemoryDeleted) || !validTime(e.OccurredAt) || !validTime(e.ReceivedAt) || !validTime(e.ExpiresAt) || !validTime(now) ||
		e.OccurredAt.After(e.ReceivedAt) || e.ReceivedAt.After(now) || !e.ExpiresAt.Equal(e.OccurredAt.Add(MaxEventTTL)) ||
		e.LogicalOperationID != MemoryOperationID(e.Source.ID, e.Source.Revision, e.EventType) || e.RootTraceID != MemoryOperationID(e.Source.ID, e.Source.Revision, MemoryUpdated) || e.EventID != StableEventID(e) {
		return ErrInvalid
	}
	switch e.EventType {
	case MemoryUpdated:
		if e.CausationID != nil {
			return ErrInvalid
		}
	case MemoryContextInvalidation:
		parent := e
		parent.EventType = MemoryUpdated
		parent.LogicalOperationID = e.RootTraceID
		parent.CausationID = nil
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

// Structural comparison only; Store must resolve this original parent and its
// committed receipt under native locks. Deserialized objects confer no power.
func ValidateMemoryCausation(child Envelope, parent Record, p ConsumerRecord) error {
	if child.EventType != MemoryContextInvalidation || ValidateEnvelope(child, child.ReceivedAt) != nil || ValidateRecord(parent) != nil ||
		parent.Event.EventType != MemoryUpdated || parent.State != MemoryComplete || parent.Event.RootTraceID != child.RootTraceID || parent.Event.Source != child.Source ||
		parent.Event.Subject != child.Subject || parent.Event.AgentID != child.AgentID || !parent.Event.OccurredAt.Equal(child.OccurredAt) || !parent.Event.ExpiresAt.Equal(child.ExpiresAt) ||
		child.CausationID == nil || *child.CausationID != parent.Event.EventID || ValidateConsumerRecord(p) != nil || p.EventID != parent.Event.EventID || p.Subject != child.Subject || p.HandlerVersion != MemoryHandler ||
		p.State != MemoryComplete || p.Fence != parent.Fence || p.Attempt != parent.Attempt || !child.ReceivedAt.Equal(p.UpdatedAt) || p.UpdatedAt.Before(parent.CreatedAt) || p.UpdatedAt.After(parent.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

func ValidateMemoryRootBudget(attempts int64) error {
	if attempts < 0 || attempts == math.MaxInt64 {
		return ErrInvalid
	}
	if attempts >= MaxMemoryRootAttempts {
		return ErrConflict
	}
	return nil
}

func validateMemoryConsumer(r ConsumerRecord) error {
	switch r.State {
	case Leased:
		if r.Reason != "" {
			return ErrInvalid
		}
	case Pending:
		if r.Reason != ReasonCleanupMore {
			return ErrInvalid
		}
	case MemoryComplete:
		if r.Reason != ReasonCleanupComplete {
			return ErrInvalid
		}
	case Invalidated:
		if r.Reason != ReasonSourceInvalidated {
			return ErrInvalid
		}
	case Expired:
		if r.Reason != ReasonExpired {
			return ErrInvalid
		}
	case DeadLetter:
		if r.Reason != ReasonRootExhausted && r.Reason != ReasonAttemptsExhausted {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
