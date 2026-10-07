package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
	"time"
)

// These selectors consume only actual native revision, row epoch and clocks.
// No private fields enter the outbox or maintenance consumer.
const preferenceOutboxConfiguredSQL = `SELECT pp.written_profile_version,'configured'::text,pp.updated_at,pp.xmin::text,
 birdtie_preference_outbox_fingerprint(pp.agent_id,pp.owner_id,pp.written_profile_version,'configured',pp.updated_at,pp.xmin::text),clock_timestamp()
 FROM agent_private_profiles pp WHERE pp.agent_id=$1 AND pp.owner_id=$2 AND pp.owner_type='PERSON'
 AND pp.written_profile_version>=2 FOR SHARE OF pp`
const preferenceOutboxClearedSQL = `SELECT ap.profile_version,'cleared'::text,ap.updated_at,ap.xmin::text,
 birdtie_preference_outbox_fingerprint(ap.agent_id,ap.owner_id,ap.profile_version,'cleared',ap.updated_at,ap.xmin::text),clock_timestamp()
 FROM agent_profiles ap WHERE ap.agent_id=$1 AND ap.owner_id=$2 AND ap.owner_type='PERSON' AND ap.profile_version>=2
 AND NOT EXISTS(SELECT 1 FROM agent_private_profiles pp WHERE pp.agent_id=ap.agent_id) FOR UPDATE OF ap`

func resolvePreferenceOutboxTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (memoryOutboxSource, bool, error) {
	var s memoryOutboxSource
	q := preferenceOutboxConfiguredSQL
	if e.Source.Status == agentoutbox.PreferenceCleared {
		q = preferenceOutboxClearedSQL
	}
	err := tx.QueryRow(ctx, q, e.AgentID, e.Subject.ID).Scan(&s.revision, &s.status, &s.at, &s.row, &s.fingerprint, &s.observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	if err != nil {
		return s, false, agentoutbox.ErrUnavailable
	}
	current := s.revision == e.Source.Revision && s.status == e.Source.Status && s.fingerprint == e.Source.Fingerprint && s.at.Equal(e.OccurredAt) && s.row != "" && !s.at.After(s.observed)
	return s, current, nil
}

const preferenceRootAttemptsSQL = `SELECT COALESCE(sum(attempt),0)::bigint FROM agent_domain_outbox
 WHERE subject_id=$1 AND agent_id=$2 AND root_trace_id=$3 AND schema_version='agent-preference-outbox-v1'`

func preferenceRootAttemptsTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (int64, error) {
	var n int64
	if err := tx.QueryRow(ctx, preferenceRootAttemptsSQL, e.Subject.ID, e.AgentID, e.RootTraceID).Scan(&n); err != nil || n < 0 || n > agentoutbox.MaxPreferenceRootAttempts {
		return 0, agentoutbox.ErrUnavailable
	}
	return n, nil
}
func preferenceParentCurrentTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) error {
	if e.EventType == agentoutbox.PreferenceUpdated {
		return nil
	}
	if e.CausationID == nil {
		return agentoutbox.ErrInvalid
	}
	parent, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.event_id=$1 AND d.subject_id=$2`, *e.CausationID, e.Subject.ID))
	if err != nil {
		return agentoutbox.ErrUnavailable
	}
	c := agentoutbox.Claim{EventID: parent.Event.EventID, Subject: e.Subject, HandlerVersion: agentoutbox.PreferenceHandler}
	p, err := scanMemoryReceipt(tx.QueryRow(ctx, `SELECT control_state,fence,attempt,reason_code,created_at,updated_at FROM agent_consumer_inbox WHERE event_id=$1 AND subject_id=$2 AND handler_version=$3`, c.EventID, c.Subject.ID, c.HandlerVersion), c)
	if err != nil {
		return agentoutbox.ErrUnavailable
	}
	return agentoutbox.ValidatePreferenceCausation(e, parent, p)
}
func preferenceFinalTx(ctx context.Context, tx pgx.Tx, r agentoutbox.Record, c agentoutbox.Claim, admission time.Time) error {
	_, current, err := resolvePreferenceOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return err
	}
	if !current {
		return agentoutbox.ErrConflict
	}
	if err = preferenceParentCurrentTx(ctx, tx, r.Event); err != nil {
		return err
	}
	var now time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
		return agentoutbox.ErrUnavailable
	}
	// Structural/TTL check only; the caller's held native row and exact fenced
	// UPDATE establish current ownership. This is not an independent reread.
	if err = agentoutbox.CheckFence(now, c, c); err != nil {
		return err
	}
	if !r.Event.ExpiresAt.After(now) {
		return agentoutbox.ErrExpired
	}
	if err = outboxControlAdmissionClock(admission, now); err != nil {
		return err
	}
	if _, err = preferenceRootAttemptsTx(ctx, tx, r.Event); err != nil {
		return err
	}
	return ctx.Err()
}
