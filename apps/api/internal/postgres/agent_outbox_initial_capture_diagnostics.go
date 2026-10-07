package postgres

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
)

type outboxPendingFailurePhase uint8

const (
	outboxInitialPendingFailure outboxPendingFailurePhase = iota
	outboxRetryPendingFailure
)

const outboxPendingFailureMessage = "原生事件控制形状校验失败"

// No raw source value or error text can enter these structured attributes.
// Callers pass the original error and the clock already read by the resolver.
func warnOutboxPendingFailure(ctx context.Context, phase outboxPendingFailurePhase, e agentoutbox.Envelope, now time.Time, err error) {
	if err == nil {
		return
	}
	var phaseName string
	switch phase {
	case outboxInitialPendingFailure:
		phaseName = "INITIAL_NEW_PENDING"
	case outboxRetryPendingFailure:
		phaseName = "RETRY_NEW_PENDING"
	default:
		return
	}
	class := "UNKNOWN"
	if errors.Is(err, agentoutbox.ErrInvalid) {
		class = "INVALID"
	} else if errors.Is(err, agentoutbox.ErrExpired) {
		class = "EXPIRED"
	}
	c := agentoutbox.DiagnoseEnvelope(e, now)
	slog.WarnContext(ctx, outboxPendingFailureMessage,
		"phase", phaseName, "error_class", class,
		slog.Group("conditions",
			"schema", c.Schema, "tenant_person", c.TenantPerson, "tenant_id", c.TenantID,
			"subject_person", c.SubjectPerson, "subject_id", c.SubjectID,
			"tenant_equals_subject", c.TenantEqualsSubject, "actor_person", c.ActorPerson,
			"actor_equals_subject", c.ActorEqualsSubject, "agent_id", c.AgentID,
			"event_id", c.EventID, "logical_operation_id", c.LogicalOperationID, "root_trace_id", c.RootTraceID,
			"causation_id_absent_or_valid", c.CausationIDAbsentOrValid,
			"source_is_moment", c.SourceIsMoment, "source_id", c.SourceID,
			"source_owner_equals_subject", c.SourceOwnerEqualsSubject,
			"source_revision_at_least_one", c.SourceRevisionAtLeastOne, "source_fingerprint_valid", c.SourceFingerprintValid,
			"occurred_time_valid", c.OccurredTimeValid, "received_time_valid", c.ReceivedTimeValid,
			"expires_time_valid", c.ExpiresTimeValid, "now_valid", c.NowValid,
			"occurred_not_after_received", c.OccurredNotAfterReceived, "received_not_after_now", c.ReceivedNotAfterNow,
			"expiry_equals_original_occurrence_plus_15m", c.ExpiryEqualsOriginalOccurrencePlus15m,
			"stable_event_id_matches", c.StableEventIDMatches, "event_type_registered", c.EventTypeRegistered,
			"event_status_matches_type", c.EventStatusMatchesType, "event_revision_matches_type", c.EventRevisionMatchesType,
			"expires_after_now", c.ExpiresAfterNow, "json_marshal_ok", c.JSONMarshalOK,
			"encoded_bytes_within_4096", c.EncodedBytesWithin4096))
}
