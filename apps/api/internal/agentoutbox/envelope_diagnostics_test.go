package agentoutbox

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func diagnosticContractError(c EnvelopeConditions) error {
	v := reflect.ValueOf(c)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		if name != "ExpiresAfterNow" && name != "JSONMarshalOK" && name != "EncodedBytesWithin4096" && !v.Field(i).Bool() {
			return ErrInvalid
		}
	}
	if !c.ExpiresAfterNow {
		return ErrExpired
	}
	if !c.JSONMarshalOK || !c.EncodedBytesWithin4096 {
		return ErrInvalid
	}
	return nil
}

func TestAgentOutboxEnvelopeDiagnosticsOriginalPredicateMatrix(t *testing.T) {
	type sample struct {
		name, failed string
		mutate       func(*Envelope, *time.Time)
		keepID       bool
		want         error
	}
	invalid := []sample{
		{"schema", "Schema", func(e *Envelope, _ *time.Time) { e.SchemaVersion = "private-schema" }, false, ErrInvalid},
		{"tenant_type", "TenantPerson", func(e *Envelope, _ *time.Time) { e.Tenant.Type = actorref.Organization }, false, ErrInvalid},
		{"tenant_id", "TenantID", func(e *Envelope, _ *time.Time) { e.Tenant.ID = "invalid" }, false, ErrInvalid},
		{"subject_type", "SubjectPerson", func(e *Envelope, _ *time.Time) { e.Subject.Type = actorref.Business }, false, ErrInvalid},
		{"subject_id", "SubjectID", func(e *Envelope, _ *time.Time) { e.Subject.ID = "invalid" }, false, ErrInvalid},
		{"tenant_subject", "TenantEqualsSubject", func(e *Envelope, _ *time.Time) { e.Tenant.ID = testOther }, false, ErrInvalid},
		{"actor_type", "ActorPerson", func(e *Envelope, _ *time.Time) { e.Actor.Type = actorref.Community }, false, ErrInvalid},
		{"actor_subject", "ActorEqualsSubject", func(e *Envelope, _ *time.Time) { e.Actor.ID = testOther }, false, ErrInvalid},
		{"agent", "AgentID", func(e *Envelope, _ *time.Time) { e.AgentID = "invalid" }, false, ErrInvalid},
		{"event", "EventID", func(e *Envelope, _ *time.Time) { e.EventID = "invalid" }, true, ErrInvalid},
		{"operation", "LogicalOperationID", func(e *Envelope, _ *time.Time) { e.LogicalOperationID = "invalid" }, false, ErrInvalid},
		{"trace", "RootTraceID", func(e *Envelope, _ *time.Time) { e.RootTraceID = "invalid" }, false, ErrInvalid},
		{"causation", "CausationIDAbsentOrValid", func(e *Envelope, _ *time.Time) { cause := "invalid"; e.CausationID = &cause }, false, ErrInvalid},
		{"source_type", "SourceIsMoment", func(e *Envelope, _ *time.Time) { e.Source.Type = "PROFILE" }, false, ErrInvalid},
		{"source_id", "SourceID", func(e *Envelope, _ *time.Time) { e.Source.ID = "invalid" }, false, ErrInvalid},
		{"source_owner", "SourceOwnerEqualsSubject", func(e *Envelope, _ *time.Time) { e.Source.Owner.ID = testOther }, false, ErrInvalid},
		{"revision", "SourceRevisionAtLeastOne", func(e *Envelope, _ *time.Time) { e.Source.Revision = 0 }, false, ErrInvalid},
		{"fingerprint", "SourceFingerprintValid", func(e *Envelope, _ *time.Time) { e.Source.Fingerprint = strings.Repeat("z", 64) }, false, ErrInvalid},
		{"occurred", "OccurredTimeValid", func(e *Envelope, _ *time.Time) { e.OccurredAt = time.Time{} }, false, ErrInvalid},
		{"received", "ReceivedTimeValid", func(e *Envelope, _ *time.Time) { e.ReceivedAt = time.Time{} }, false, ErrInvalid},
		{"expires", "ExpiresTimeValid", func(e *Envelope, _ *time.Time) { e.ExpiresAt = time.Time{} }, false, ErrInvalid},
		{"now", "NowValid", func(_ *Envelope, now *time.Time) { *now = time.Time{} }, false, ErrInvalid},
		{"strict_future_source", "OccurredNotAfterReceived", func(e *Envelope, _ *time.Time) {
			e.OccurredAt = e.ReceivedAt.Add(time.Second)
			e.ExpiresAt = e.OccurredAt.Add(MaxEventTTL)
		}, false, ErrInvalid},
		{"future_receipt", "ReceivedNotAfterNow", func(e *Envelope, _ *time.Time) { e.ReceivedAt = e.ReceivedAt.Add(time.Second) }, false, ErrInvalid},
		{"ttl", "ExpiryEqualsOriginalOccurrencePlus15m", func(e *Envelope, _ *time.Time) { e.ExpiresAt = e.ExpiresAt.Add(time.Second) }, false, ErrInvalid},
		{"identity", "StableEventIDMatches", func(e *Envelope, _ *time.Time) { e.EventID = testOther }, true, ErrInvalid},
		{"unregistered", "EventTypeRegistered", func(e *Envelope, _ *time.Time) { e.EventType = "model-private-event" }, false, ErrInvalid},
		{"status", "EventStatusMatchesType", func(e *Envelope, _ *time.Time) { e.Source.Status = Withdrawn }, false, ErrInvalid},
		{"kind_revision", "EventRevisionMatchesType", func(e *Envelope, _ *time.Time) { e.Source.Revision = 2 }, false, ErrInvalid},
		{"strict_expiry", "ExpiresAfterNow", func(e *Envelope, now *time.Time) { *now = e.ExpiresAt }, false, ErrExpired},
		{"marshal", "JSONMarshalOK", func(e *Envelope, _ *time.Time) { e.OccurredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, false, ErrInvalid},
		{"oversize_zero_record", "EncodedBytesWithin4096", func(e *Envelope, _ *time.Time) { e.SchemaVersion = strings.Repeat("private", MaxEnvelopeBytes) }, false, ErrInvalid},
		{"invalid_before_expiry", "Schema", func(e *Envelope, now *time.Time) { e.SchemaVersion = "invalid"; *now = e.ExpiresAt }, false, ErrInvalid},
	}
	seen := map[string]bool{}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			e, now := outboxEvent(MomentCreated), outboxTime()
			tc.mutate(&e, &now)
			if !tc.keepID {
				e.EventID = StableEventID(e)
			}
			before := cloneEnvelope(e)
			c := DiagnoseEnvelope(e, now)
			v := reflect.ValueOf(c)
			if v.NumField() != 32 || v.FieldByName(tc.failed).Bool() {
				t.Fatal("closed original predicate not observed", tc.failed)
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).Kind() != reflect.Bool {
					t.Fatal("source value added to diagnostic")
				}
				if !v.Field(i).Bool() {
					seen[v.Type().Field(i).Name] = true
				}
			}
			r, err := NewPending(e, now)
			if err != tc.want || ValidateEnvelope(e, now) != tc.want || diagnosticContractError(c) != tc.want || !reflect.DeepEqual(r, Record{}) || !reflect.DeepEqual(e, before) {
				t.Fatal("observation changed original validation/error/zero-record contract")
			}
		})
	}
	if len(seen) != 32 {
		t.Fatal("matrix does not cover all32 rejection predicates", len(seen))
	}
	for _, kind := range []EventType{MomentCreated, MomentUpdated, MomentWithdrawn} {
		t.Run(string(kind), func(t *testing.T) {
			e, now := outboxEvent(kind), outboxTime()
			c := DiagnoseEnvelope(e, now)
			r, err := NewPending(e, now)
			if diagnosticContractError(c) != nil || ValidateEnvelope(e, now) != nil || err != nil || ValidateRecord(r) != nil {
				t.Fatal("valid original predicate matrix changed")
			}
		})
	}
}
