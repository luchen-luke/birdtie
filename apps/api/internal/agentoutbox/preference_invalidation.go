package agentoutbox

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

// A closed metadata cleanup chain; neither ordinary human preferences nor its
// control receipts become a task grant, Memory write or model permission.
const PreferenceSchema = "agent-preference-outbox-v1"
const PreferenceUpdated EventType = "PREFERENCE_UPDATED"
const PreferenceContextInvalidation EventType = "PREFERENCE_CONTEXT_INVALIDATION"
const PreferenceSource SourceType = "PRIVATE_PREFERENCE"
const PreferenceConfigured SourceStatus = "configured"
const PreferenceCleared SourceStatus = "cleared"
const PreferenceHandler HandlerVersion = "preference-invalidation-v1"
const MaxPreferenceRootAttempts = MaxMemoryRootAttempts
const MaxPreferenceCausalDepth = MaxMemoryCausalDepth

func PreferenceOperationID(source string, revision int64, kind EventType) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("birdtie.preference.operation.v1\x00%s\x00%d\x00%s", source, revision, kind)))
	h[6] = h[6]&0x0f | 0x80
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
func validatePreferenceEnvelope(e Envelope, now time.Time) error {
	if e.SchemaVersion != PreferenceSchema || !validPerson(e.Subject) || e.Tenant != e.Subject || e.Actor.Type != actorref.Person || e.Actor.ID != e.Subject.ID ||
		!validID(e.AgentID) || !validID(e.EventID) || e.Source.Type != PreferenceSource || e.Source.ID != e.AgentID || e.Source.Owner != e.Subject || e.Source.Revision < 2 ||
		!digest(e.Source.Fingerprint) || (e.Source.Status != PreferenceConfigured && e.Source.Status != PreferenceCleared) || !validTime(e.OccurredAt) || !validTime(e.ReceivedAt) || !validTime(e.ExpiresAt) || !validTime(now) ||
		e.OccurredAt.After(e.ReceivedAt) || e.ReceivedAt.After(now) || !e.ExpiresAt.Equal(e.OccurredAt.Add(MaxEventTTL)) ||
		e.LogicalOperationID != PreferenceOperationID(e.Source.ID, e.Source.Revision, e.EventType) || e.RootTraceID != PreferenceOperationID(e.Source.ID, e.Source.Revision, PreferenceUpdated) || e.EventID != StableEventID(e) {
		return ErrInvalid
	}
	switch e.EventType {
	case PreferenceUpdated:
		if e.CausationID != nil {
			return ErrInvalid
		}
	case PreferenceContextInvalidation:
		parent := e
		parent.EventType = PreferenceUpdated
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

// Store must separately verify this exact completed parent and receipt under
// current native locks; a deserialized comparison does not confer permission.
func ValidatePreferenceCausation(child Envelope, parent Record, p ConsumerRecord) error {
	if child.EventType != PreferenceContextInvalidation || ValidateEnvelope(child, child.ReceivedAt) != nil || ValidateRecord(parent) != nil ||
		parent.Event.EventType != PreferenceUpdated || parent.State != MemoryComplete || parent.Event.RootTraceID != child.RootTraceID || parent.Event.Source != child.Source ||
		parent.Event.Subject != child.Subject || parent.Event.AgentID != child.AgentID || !parent.Event.OccurredAt.Equal(child.OccurredAt) || !parent.Event.ExpiresAt.Equal(child.ExpiresAt) ||
		child.CausationID == nil || *child.CausationID != parent.Event.EventID || ValidateConsumerRecord(p) != nil || p.EventID != parent.Event.EventID || p.Subject != child.Subject || p.HandlerVersion != PreferenceHandler ||
		p.State != MemoryComplete || p.Fence != parent.Fence || p.Attempt != parent.Attempt || !child.ReceivedAt.Equal(p.UpdatedAt) || p.UpdatedAt.Before(parent.CreatedAt) || p.UpdatedAt.After(parent.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}
