package agentoutbox

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const testOwner = "83000000-0000-4000-8000-000000000001"
const testAgent = "83000000-0000-4000-8000-000000000002"
const testSource = "83000000-0000-4000-8000-000000000003"
const testOperation = "83000000-0000-4000-8000-000000000004"
const testTrace = "83000000-0000-4000-8000-000000000005"
const testWorker = "83000000-0000-4000-8000-000000000006"
const testOther = "83000000-0000-4000-8000-000000000007"

func outboxTime() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }
func outboxEvent(kind EventType) Envelope {
	now := outboxTime()
	p := actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}
	e := Envelope{SchemaVersion: SchemaVersion, EventType: kind, Tenant: p, Subject: p, Actor: actorref.ActorRef{Type: actorref.Person, ID: testOwner}, AgentID: testAgent, LogicalOperationID: testOperation, OccurredAt: now, ReceivedAt: now, ExpiresAt: now.Add(MaxEventTTL), Source: SourceReference{Type: MomentSource, ID: testSource, Owner: p, Revision: 1, Status: Draft, Fingerprint: strings.Repeat("a", 64)}, RootTraceID: testTrace}
	if kind != MomentCreated {
		e.Source.Revision = 2
	}
	if kind == MomentWithdrawn {
		e.Source.Status = Withdrawn
	}
	e.EventID = StableEventID(e)
	return e
}
func outboxRecord() Record { r, _ := NewPending(outboxEvent(MomentCreated), outboxTime()); return r }
func outboxLease() Record {
	r := outboxRecord()
	until := outboxTime().Add(20 * time.Second)
	r.State = Leased
	r.Attempt = 1
	r.Fence = 1
	r.LeaseOwner = testWorker
	r.LeaseUntil = &until
	return r
}
func outboxClaim() Claim {
	r := outboxLease()
	return Claim{r.Event.EventID, r.Event.Subject, r.Event.AgentID, HandlerV1, testWorker, 1, *r.LeaseUntil}
}

func TestAgentOutboxNativeEnvelopeAndPending(t *testing.T) {
	for _, kind := range []EventType{MomentCreated, MomentUpdated, MomentWithdrawn} {
		t.Run(string(kind), func(t *testing.T) {
			e := outboxEvent(kind)
			r, err := NewPending(e, outboxTime())
			if err != nil || ValidateRecord(r) != nil || r.State != Pending || r.Fence != 0 || r.Attempt != 0 || r.Event.SchemaVersion == "air.event.v1" {
				t.Fatal("independent native metadata shape", err)
			}
			if e.EventID[14] != '8' {
				t.Fatal("event ID is not UUIDv8")
			}
		})
	}
	t.Run("deep_copy_causation", func(t *testing.T) {
		e := outboxEvent(MomentCreated)
		cause := testTrace
		e.CausationID = &cause
		r, err := NewPending(e, outboxTime())
		if err != nil {
			t.Fatal(err)
		}
		cause = testOther
		if *r.Event.CausationID != testTrace {
			t.Fatal("copied control pointer aliased")
		}
	})
	t.Run("expiry_not_renewed", func(t *testing.T) {
		e := outboxEvent(MomentCreated)
		now := outboxTime().Add(10 * time.Minute)
		r, err := NewPending(e, now)
		if err != nil || r.Event.ExpiresAt != e.ExpiresAt {
			t.Fatal("capture retry changed source expiry")
		}
	})
	t.Run("exact_expiry", func(t *testing.T) {
		e := outboxEvent(MomentCreated)
		r, err := NewPending(e, e.ExpiresAt)
		if err != ErrExpired || r != (Record{}) {
			t.Fatal("expiry accepted")
		}
	})
	t.Run("historical_shape_is_not_live_grant", func(t *testing.T) {
		r := outboxRecord()
		r.State = Expired
		r.UpdatedAt = r.Event.ExpiresAt.Add(time.Second)
		if ValidateRecord(r) != nil || ValidateEnvelope(r.Event, r.UpdatedAt) != ErrExpired {
			t.Fatal("storage shape confused with live event")
		}
	})
}

func TestAgentOutboxRejectsInvalidNativeMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Envelope)
	}{
		{"schema", func(e *Envelope) { e.SchemaVersion = "air.event.v1" }},
		{"physical_deleted_is_not_withdrawn", func(e *Envelope) { e.EventType = "MOMENT_DELETED" }},
		{"unregistered_task", func(e *Envelope) { e.EventType = "UserQuery" }},
		{"unknown", func(e *Envelope) { e.EventType = "model_selected" }},
		{"organization_tenant", func(e *Envelope) { e.Tenant.Type = actorref.Organization }},
		{"business_subject", func(e *Envelope) { e.Subject.Type = actorref.Business }},
		{"community_actor", func(e *Envelope) { e.Actor.Type = actorref.Community }},
		{"cross_subject", func(e *Envelope) { e.Subject.ID = testOther }},
		{"cross_actor", func(e *Envelope) { e.Actor.ID = testOther }},
		{"cross_source_owner", func(e *Envelope) { e.Source.Owner.ID = testOther }},
		{"missing_agent", func(e *Envelope) { e.AgentID = "" }},
		{"zero_agent", func(e *Envelope) { e.AgentID = "00000000-0000-0000-0000-000000000000" }},
		{"bad_operation", func(e *Envelope) { e.LogicalOperationID = "operation" }},
		{"space_trace", func(e *Envelope) { e.RootTraceID = " " + testTrace }},
		{"invalid_source", func(e *Envelope) { e.Source.ID = "place" }},
		{"wrong_source_type", func(e *Envelope) { e.Source.Type = "PROFILE" }},
		{"zero_revision", func(e *Envelope) { e.Source.Revision = 0 }},
		{"invented_create_revision", func(e *Envelope) { e.Source.Revision = 2 }},
		{"wrong_status", func(e *Envelope) { e.Source.Status = Withdrawn }},
		{"unknown_status", func(e *Envelope) { e.Source.Status = "published" }},
		{"short_fingerprint", func(e *Envelope) { e.Source.Fingerprint = "a" }},
		{"upper_fingerprint", func(e *Envelope) { e.Source.Fingerprint = strings.Repeat("A", 64) }},
		{"nonhex_fingerprint", func(e *Envelope) { e.Source.Fingerprint = strings.Repeat("z", 64) }},
		{"zero_occurrence", func(e *Envelope) { e.OccurredAt = time.Time{} }},
		{"future_occurrence", func(e *Envelope) { e.OccurredAt = e.ReceivedAt.Add(time.Second) }},
		{"future_receipt", func(e *Envelope) { e.ReceivedAt = e.ReceivedAt.Add(time.Second) }},
		{"zero_expiry", func(e *Envelope) { e.ExpiresAt = time.Time{} }},
		{"extended_expiry", func(e *Envelope) { e.ExpiresAt = e.ExpiresAt.Add(time.Second) }},
		{"time_outside_json", func(e *Envelope) { e.OccurredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"bad_causation", func(e *Envelope) { v := "verified"; e.CausationID = &v }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := outboxEvent(MomentCreated)
			tc.mutate(&e)
			e.EventID = StableEventID(e)
			if err := ValidateEnvelope(e, outboxTime()); err != ErrInvalid {
				t.Fatal("invalid native metadata accepted", err)
			}
		})
	}
	for _, kind := range []EventType{MomentUpdated, MomentWithdrawn} {
		t.Run(string(kind)+"_needs_actual_revision", func(t *testing.T) {
			e := outboxEvent(kind)
			e.Source.Revision = 1
			e.EventID = StableEventID(e)
			if ValidateEnvelope(e, outboxTime()) != ErrInvalid {
				t.Fatal("revision synthesized")
			}
		})
	}
	t.Run("event_id_tamper", func(t *testing.T) {
		e := outboxEvent(MomentCreated)
		e.EventID = testOther
		if ValidateEnvelope(e, outboxTime()) != ErrInvalid {
			t.Fatal("unbound ID")
		}
	})
	t.Run("nil_clock", func(t *testing.T) {
		if ValidateEnvelope(outboxEvent(MomentCreated), time.Time{}) != ErrInvalid {
			t.Fatal("missing clock")
		}
	})
}

func TestAgentOutboxEventIdentityBindings(t *testing.T) {
	e := outboxEvent(MomentCreated)
	for _, tc := range []struct {
		name   string
		mutate func(*Envelope)
	}{
		{"tenant", func(e *Envelope) { e.Tenant.ID = testOther }},
		{"subject", func(e *Envelope) { e.Subject.ID = testOther }},
		{"agent", func(e *Envelope) { e.AgentID = testOther }},
		{"operation", func(e *Envelope) { e.LogicalOperationID = testOther }},
		{"source", func(e *Envelope) { e.Source.ID = testOther }},
		{"revision", func(e *Envelope) { e.Source.Revision++ }},
		{"fingerprint", func(e *Envelope) { e.Source.Fingerprint = strings.Repeat("b", 64) }},
		{"status", func(e *Envelope) { e.Source.Status = Withdrawn }},
		{"type", func(e *Envelope) { e.EventType = MomentUpdated }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := e
			tc.mutate(&copy)
			if StableEventID(copy) == e.EventID {
				t.Fatal("metadata identity collided")
			}
		})
	}
	t.Run("trace_receipt_not_effect_identity", func(t *testing.T) {
		copy := e
		copy.RootTraceID = testOther
		copy.ReceivedAt = copy.ReceivedAt.Add(time.Second)
		if StableEventID(copy) != e.EventID {
			t.Fatal("retry trace created new event identity")
		}
	})
}

func TestAgentOutboxControlRecordMatrix(t *testing.T) {
	for _, state := range []State{Pending, Unavailable, Invalidated, Expired, DeadLetter} {
		t.Run(string(state), func(t *testing.T) {
			r := outboxRecord()
			r.State = state
			if ValidateRecord(r) != nil {
				t.Fatal("terminal shape rejected")
			}
		})
	}
	t.Run("leased", func(t *testing.T) {
		if ValidateRecord(outboxLease()) != nil {
			t.Fatal("valid lease shape rejected")
		}
	})
	for _, tc := range []struct {
		name   string
		mutate func(*Record)
	}{
		{"unknown_state", func(r *Record) { r.State = "SUCCEEDED" }},
		{"negative_attempt", func(r *Record) { r.Attempt = -1 }},
		{"excess_attempt", func(r *Record) { r.Attempt = MaxAttempts + 1 }},
		{"negative_fence", func(r *Record) { r.Fence = -1 }},
		{"attempt_without_fence", func(r *Record) { r.Attempt = 1 }},
		{"terminal_owner", func(r *Record) { r.LeaseOwner = testWorker }},
		{"terminal_until", func(r *Record) { v := outboxTime().Add(time.Second); r.LeaseUntil = &v }},
		{"zero_created", func(r *Record) { r.CreatedAt = time.Time{} }},
		{"before_receipt", func(r *Record) { r.CreatedAt = r.CreatedAt.Add(-time.Second) }},
		{"updated_before_created", func(r *Record) { r.UpdatedAt = r.UpdatedAt.Add(-time.Second) }},
		{"next_before_created", func(r *Record) { r.NextAttemptAt = r.NextAttemptAt.Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := outboxRecord()
			tc.mutate(&r)
			if ValidateRecord(r) != ErrInvalid {
				t.Fatal("invalid control record accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Record)
	}{
		{"missing_owner", func(r *Record) { r.LeaseOwner = "" }},
		{"bad_owner", func(r *Record) { r.LeaseOwner = "trusted" }},
		{"missing_lease", func(r *Record) { r.LeaseUntil = nil }},
		{"zero_fence", func(r *Record) { r.Fence = 0 }},
		{"overflow_fence", func(r *Record) { r.Fence = math.MaxInt64 }},
		{"zero_attempt", func(r *Record) { r.Attempt = 0 }},
		{"expired_at_updated", func(r *Record) { v := r.UpdatedAt; r.LeaseUntil = &v }},
		{"unbounded_lease", func(r *Record) { v := r.UpdatedAt.Add(MaxLeaseDuration + time.Second); r.LeaseUntil = &v }},
		{"after_source_expiry", func(r *Record) {
			r.UpdatedAt = r.Event.ExpiresAt.Add(-time.Second)
			v := r.Event.ExpiresAt.Add(time.Second)
			r.LeaseUntil = &v
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := outboxLease()
			tc.mutate(&r)
			if ValidateRecord(r) != ErrInvalid {
				t.Fatal("invalid lease record accepted")
			}
		})
	}
}

func TestAgentOutboxCandidateCheckpointMetadataDoesNotAuthorizeOldClaim(t *testing.T) {
	r := outboxRecord()
	r.State = CandidateStaged
	r.Attempt = 1
	r.Fence = 1
	if ValidateRecord(r) != nil {
		t.Fatal("checkpoint metadata rejected")
	}
	if ValidateHandlerVersion("mom-candidate-local-v1") == nil {
		t.Fatal("old control may claim candidate consumer")
	}
}
