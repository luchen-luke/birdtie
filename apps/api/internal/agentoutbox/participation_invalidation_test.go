package agentoutbox

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"testing"
	"time"
)

func participationUnitEvent() Envelope {
	id := "27000000-0000-4000-8000-000000000001"
	p := actorref.PrincipalRef{Type: actorref.Person, ID: id}
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	e := Envelope{SchemaVersion: ParticipationSchema, EventType: ParticipationUpdated, Tenant: p, Subject: p, Actor: actorref.ActorRef{Type: p.Type, ID: id}, AgentID: id, Source: SourceReference{Type: ParticipationSource, ID: id, Owner: p, Status: ParticipationGoing, Fingerprint: strings.Repeat("a", 64)}, OccurredAt: at, ReceivedAt: at, ExpiresAt: at.Add(MaxEventTTL)}
	e.LogicalOperationID = ParticipationOperationID(id, e.Source.Fingerprint)
	e.RootTraceID = e.LogicalOperationID
	e.EventID = StableEventID(e)
	return e
}
func TestParticipationInvalidationEnvelopeIsNativeRootNoAttendanceOrChild(t *testing.T) {
	e := participationUnitEvent()
	if ValidateEnvelope(e, e.ReceivedAt) != nil {
		t.Fatal("closed native metadata root rejected")
	}
	for _, change := range []func(*Envelope){func(x *Envelope) { x.Source.Revision = 1 }, func(x *Envelope) { x.Source.Status = "attended" }, func(x *Envelope) { x.CausationID = &x.EventID }, func(x *Envelope) { x.EventType = "ACTIVITY_ATTENDED" }, func(x *Envelope) { x.RootTraceID = x.AgentID }, func(x *Envelope) { x.Actor.Type = actorref.Organization }, func(x *Envelope) { x.SchemaVersion = SchemaVersion }} {
		q := e
		change(&q)
		if ValidateEnvelope(q, q.ReceivedAt) == nil {
			t.Fatal("invalid participation root accepted")
		}
	}
	if !errors.Is(ValidateEnvelope(e, e.ExpiresAt), ErrExpired) {
		t.Fatal("expired metadata renewed")
	}
	if ParticipationOperationID(e.Source.ID, strings.Repeat("b", 64)) == e.LogicalOperationID {
		t.Fatal("native ABA generation merged")
	}
	if ValidateParticipationRootBudget(MaxParticipationRootAttempts-1) != nil || !errors.Is(ValidateParticipationRootBudget(MaxParticipationRootAttempts), ErrConflict) {
		t.Fatal("root budget extended")
	}
}
