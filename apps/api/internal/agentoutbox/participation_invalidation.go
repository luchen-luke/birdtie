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

// A native participation row is retained RSVP state, never attendance or an
// autonomous action grant. This root narrows old evidence/candidate support.
const ParticipationSchema = "agent-participation-outbox-v1"
const ParticipationUpdated EventType = "ACTIVITY_PARTICIPATION_UPDATED"
const ParticipationSource SourceType = "ACTIVITY_PARTICIPATION"
const ParticipationGoing SourceStatus = "going"
const ParticipationCancelled SourceStatus = "cancelled"
const ParticipationHandler HandlerVersion = "participation-invalidation-v1"
const MaxParticipationRootAttempts = ar.MaxRootAttempts
const MaxParticipationCausalDepth = 0
const MaxParticipationEvidenceCleanup = 128
const MaxParticipationCandidateCleanup = 128

func ParticipationOperationID(source, fingerprint string) string {
	h := sha256.Sum256([]byte("birdtie.participation.operation.v1\x00" + source + "\x00" + fingerprint))
	h[6] = h[6]&0x0f | 0x80
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
func validateParticipationEnvelope(e Envelope, now time.Time) error {
	if e.SchemaVersion != ParticipationSchema || e.EventType != ParticipationUpdated || !validPerson(e.Subject) || e.Tenant != e.Subject || e.Actor.Type != actorref.Person || e.Actor.ID != e.Subject.ID || !validID(e.AgentID) || !validID(e.EventID) || e.Source.Type != ParticipationSource || e.Source.Owner != e.Subject || !validID(e.Source.ID) || e.Source.Revision != 0 || !digest(e.Source.Fingerprint) || (e.Source.Status != ParticipationGoing && e.Source.Status != ParticipationCancelled) || e.CausationID != nil || !validTime(e.OccurredAt) || !validTime(e.ReceivedAt) || !validTime(e.ExpiresAt) || !validTime(now) || e.OccurredAt.After(e.ReceivedAt) || e.ReceivedAt.After(now) || !e.ExpiresAt.Equal(e.OccurredAt.Add(MaxEventTTL)) || e.LogicalOperationID != ParticipationOperationID(e.Source.ID, e.Source.Fingerprint) || e.RootTraceID != e.LogicalOperationID || e.EventID != StableEventID(e) {
		return ErrInvalid
	}
	if !e.ExpiresAt.After(now) {
		return ErrExpired
	}
	raw, err := json.Marshal(e)
	if err != nil || len(raw) > MaxEnvelopeBytes {
		return ErrInvalid
	}
	return nil
}
func ValidateParticipationRootBudget(attempts int64) error {
	if attempts < 0 || attempts == math.MaxInt64 {
		return ErrInvalid
	}
	if attempts >= MaxParticipationRootAttempts {
		return ErrConflict
	}
	return nil
}
func validateParticipationConsumer(r ConsumerRecord) error { return validateMemoryConsumer(r) }
