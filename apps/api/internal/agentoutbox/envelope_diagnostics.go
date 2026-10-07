package agentoutbox

import (
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

// EnvelopeConditions is a closed observation of the existing shape predicates.
// It contains no source values, clocks, authority or replacement validation.
type EnvelopeConditions struct {
	Schema, TenantPerson, TenantID, SubjectPerson, SubjectID              bool
	TenantEqualsSubject, ActorPerson, ActorEqualsSubject                  bool
	AgentID, EventID, LogicalOperationID, RootTraceID                     bool
	CausationIDAbsentOrValid, SourceIsMoment, SourceID                    bool
	SourceOwnerEqualsSubject, SourceRevisionAtLeastOne                    bool
	SourceFingerprintValid                                                bool
	OccurredTimeValid, ReceivedTimeValid, ExpiresTimeValid                bool
	NowValid, OccurredNotAfterReceived, ReceivedNotAfterNow               bool
	ExpiryEqualsOriginalOccurrencePlus15m, StableEventIDMatches           bool
	EventTypeRegistered, EventStatusMatchesType, EventRevisionMatchesType bool
	ExpiresAfterNow, JSONMarshalOK, EncodedBytesWithin4096                bool
}

// DiagnoseEnvelope observes only the supplied envelope and already acquired
// clock. ValidateEnvelope/NewPending remain the sole shape/error contract.
func DiagnoseEnvelope(e Envelope, now time.Time) EnvelopeConditions {
	known, status, revision := false, false, false
	switch e.EventType {
	case MomentCreated:
		known, status, revision = true, e.Source.Status == Draft, e.Source.Revision == 1
	case MomentUpdated:
		known, status, revision = true, e.Source.Status == Draft, e.Source.Revision > 1
	case MomentWithdrawn:
		known, status, revision = true, e.Source.Status == Withdrawn, e.Source.Revision > 1
	}
	raw, err := json.Marshal(struct{ Envelope }{e})
	return EnvelopeConditions{
		Schema:       e.SchemaVersion == SchemaVersion,
		TenantPerson: e.Tenant.Type == actorref.Person, TenantID: validID(e.Tenant.ID),
		SubjectPerson: e.Subject.Type == actorref.Person, SubjectID: validID(e.Subject.ID),
		TenantEqualsSubject: e.Tenant == e.Subject,
		ActorPerson:         e.Actor.Type == actorref.Person, ActorEqualsSubject: e.Actor.ID == e.Subject.ID,
		AgentID: validID(e.AgentID), EventID: validID(e.EventID),
		LogicalOperationID: validID(e.LogicalOperationID), RootTraceID: validID(e.RootTraceID),
		CausationIDAbsentOrValid: e.CausationID == nil || validID(*e.CausationID),
		SourceIsMoment:           e.Source.Type == MomentSource, SourceID: validID(e.Source.ID),
		SourceOwnerEqualsSubject: e.Source.Owner == e.Subject,
		SourceRevisionAtLeastOne: e.Source.Revision >= 1, SourceFingerprintValid: digest(e.Source.Fingerprint),
		OccurredTimeValid: validTime(e.OccurredAt), ReceivedTimeValid: validTime(e.ReceivedAt),
		ExpiresTimeValid: validTime(e.ExpiresAt), NowValid: validTime(now),
		OccurredNotAfterReceived: !e.OccurredAt.After(e.ReceivedAt), ReceivedNotAfterNow: !e.ReceivedAt.After(now),
		ExpiryEqualsOriginalOccurrencePlus15m: e.ExpiresAt.Equal(e.OccurredAt.Add(MaxEventTTL)),
		StableEventIDMatches:                  e.EventID == StableEventID(e),
		EventTypeRegistered:                   known, EventStatusMatchesType: status, EventRevisionMatchesType: revision,
		ExpiresAfterNow: e.ExpiresAt.After(now), JSONMarshalOK: err == nil,
		EncodedBytesWithin4096: err == nil && len(raw) <= MaxEnvelopeBytes,
	}
}
