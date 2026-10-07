package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
	"math"
	"strings"
	"time"
)

// Source identity comes from the original 076 native snapshot. A clear makes
// every selected private-profile reference stale; no deleted row is invented.
const stalePreferencePurposeSQL = `p.owner_id=$1 AND p.agent_id=$2 AND EXISTS(SELECT 1 FROM jsonb_array_elements(p.sources) s
 WHERE s->>'kind'='PURPOSE_PRIVATE_PROFILE' AND s->>'id'=$3 AND s->'version'->>'kind'='REVISION'
 AND (NOT $6::boolean OR s->'version'->>'revision' IS DISTINCT FROM $4::bigint::text OR s->>'rowToken' IS DISTINCT FROM $5))`
const preferencePreviewCleanupSQL = `WITH stale AS(SELECT p.id FROM agent_context_purpose_previews p WHERE ` + stalePreferencePurposeSQL + `
 AND NOT EXISTS(SELECT 1 FROM agent_context_purpose_bindings b WHERE b.preview_id=p.id)
 ORDER BY p.id LIMIT 128 FOR UPDATE OF p)
 DELETE FROM agent_context_purpose_previews p USING stale x WHERE p.id=x.id`
const preferencePreviewMoreSQL = `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_previews p WHERE ` + stalePreferencePurposeSQL + `
 AND NOT EXISTS(SELECT 1 FROM agent_context_purpose_bindings b WHERE b.preview_id=p.id))`
const preferenceGrantScopeSQL = `g.owner_account_id=$1 AND g.recipient_account_id=$1 AND g.resource_type='agent_context'
 AND g.resource_id=p.id::text AND g.purpose='TASK_CONTEXT_READ' AND g.actions=ARRAY['read']::text[]
 AND g.revoked_at IS NULL AND ` + stalePreferencePurposeSQL
const preferenceGrantCleanupSQL = `WITH stale AS(SELECT g.id,g.revision FROM consent_grants g
 JOIN agent_context_purpose_bindings b ON b.grant_id=g.id JOIN agent_context_purpose_previews p ON p.id=b.preview_id
 WHERE g.revision<9223372036854775807 AND ` + preferenceGrantScopeSQL + ` ORDER BY g.id LIMIT 100 FOR UPDATE OF g), stamp AS MATERIALIZED(SELECT clock_timestamp() at)
 UPDATE consent_grants g SET revoked_at=stamp.at,revision=g.revision+1 FROM stale x,stamp
 WHERE g.id=x.id AND g.revision=x.revision AND g.revoked_at IS NULL`

// The more query deliberately retains exhausted-revision anomalies: a skipped
// grant cannot be reported as completely revoked.
const preferenceGrantMoreSQL = `SELECT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_context_purpose_bindings b ON b.grant_id=g.id
 JOIN agent_context_purpose_previews p ON p.id=b.preview_id WHERE ` + preferenceGrantScopeSQL + `)`

// Nominate without locking the event, then use original metadata -> owner/root
// ordering before private-source and outbox locks. No reverse Moment lock path.
func nominatePreferenceOutboxTx(ctx context.Context, tx pgx.Tx, subject string) (agentoutbox.Record, time.Time, error) {
	var locked bool
	if err := tx.QueryRow(ctx, outboxControlDispatchLockSQL).Scan(&locked); err != nil {
		return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrUnavailable
	}
	if !locked {
		return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrDispatchBusy
	}
	var at time.Time
	var active int
	if err := tx.QueryRow(ctx, outboxControlDispatchCountSQL).Scan(&at, &active); err != nil {
		return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
	}
	if _, err := outboxControlPick(at, active, nil); err != nil && err != agentoutbox.ErrNotFound {
		return agentoutbox.Record{}, at, err
	}
	var filter any
	if subject != "" {
		filter = subject
	}
	rows, err := tx.Query(ctx, outboxControlDispatchHeadsSQL, agentoutbox.PreferenceHandler, at, filter, agentoutbox.MaxControlTenantHeads)
	if err != nil {
		return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
	}
	heads := []ar.DispatchTenant{}
	for rows.Next() {
		var h ar.DispatchTenant
		if err = rows.Scan(&h.Owner, &h.Active, &h.LastServed, &h.NextDue); err != nil {
			rows.Close()
			return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
		}
		heads = append(heads, h)
		if len(heads) > agentoutbox.MaxControlTenantHeads || subject != "" && h.Owner != subject {
			rows.Close()
			return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
	}
	owner, err := outboxControlPick(at, active, heads)
	if err != nil {
		return agentoutbox.Record{}, at, err
	}
	r, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.subject_id=$2 AND `+outboxControlDueSQL+` ORDER BY d.occurred_at,d.event_id LIMIT 1`, agentoutbox.PreferenceHandler, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, at, agentoutbox.ErrDispatchBusy
	}
	if err != nil || r.Event.Subject.ID != owner || r.Event.SchemaVersion != agentoutbox.PreferenceSchema {
		return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
	}
	return r, at, nil
}

func claimPreferenceOutboxTx(ctx context.Context, tx pgx.Tx, worker, subject string) (agentoutbox.Record, agentoutbox.Claim, error) {
	if err := memoryMaintenanceTx(ctx, tx); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	r, admission, err := nominatePreferenceOutboxTx(ctx, tx, subject)
	if err != nil {
		return r, agentoutbox.Claim{}, err
	}
	if err = lockMemoryMaintenanceBindingTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	_, valid, err := resolvePreferenceOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	locked, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.event_id=$1 AND d.subject_id=$2 AND `+strings.ReplaceAll(outboxControlDueSQL, "$1", "$3")+` FOR UPDATE OF d`, r.Event.EventID, r.Event.Subject.ID, agentoutbox.PreferenceHandler))
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrConflict
	}
	if locked.Event != r.Event && !sameMemoryEvent(locked.Event, r.Event) {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrConflict
	}
	r = locked
	var at time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	if err = outboxControlAdmissionClock(admission, at); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	n, err := preferenceRootAttemptsTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	state := agentoutbox.State("")
	if !valid {
		state = agentoutbox.Invalidated
	} else if !r.Event.ExpiresAt.After(at) {
		state = agentoutbox.Expired
	} else if n >= agentoutbox.MaxPreferenceRootAttempts || r.Fence == math.MaxInt64 {
		state = agentoutbox.DeadLetter
	}
	if state != "" {
		tag, terminalErr := tx.Exec(ctx, `UPDATE agent_domain_outbox SET delivery_state=$2,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE event_id=$1`, r.Event.EventID, state)
		if terminalErr != nil {
			return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrConflict
		}
		return agentoutbox.Record{}, agentoutbox.Claim{}, errPreferenceTerminalControl
	}
	if err = preferenceParentCurrentTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	r, err = scanAgentOutbox(tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE agent_domain_outbox d
 SET delivery_state='LEASED',attempt=d.attempt+1,fence=d.fence+1,lease_owner=$2,updated_at=clk.at,lease_until=LEAST(clk.at+interval '30 seconds',d.expires_at)
 FROM clk WHERE d.event_id=$1 AND d.expires_at>clk.at RETURNING `+outboxColumns, r.Event.EventID, worker))
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, `INSERT INTO agent_consumer_inbox(event_id,subject_id,handler_version,control_state,fence,attempt)
 VALUES($1,$2,'preference-invalidation-v1','LEASED',$3,$4) ON CONFLICT(event_id,subject_id,handler_version) DO UPDATE
 SET control_state='LEASED',reason_code='',fence=EXCLUDED.fence,attempt=EXCLUDED.attempt,updated_at=clock_timestamp()
 WHERE agent_consumer_inbox.control_state IN('LEASED','PENDING')`, r.Event.EventID, r.Event.Subject.ID, r.Fence, r.Attempt)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrConflict
	}
	c := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID, HandlerVersion: agentoutbox.PreferenceHandler, WorkerID: worker, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
	if err = preferenceFinalTx(ctx, tx, r, c, admission); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	return r, c, nil
}

var errPreferenceTerminalControl = errors.New("native preference terminal control pending commit")

func (s *Store) claimPreferenceOutbox(ctx context.Context, worker, subject string) (agentoutbox.Record, agentoutbox.Claim, error) {
	if ctx == nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	defer tx.Rollback(ctx)
	r, c, err := claimPreferenceOutboxTx(ctx, tx, worker, subject)
	if err == errPreferenceTerminalControl {
		if e := tx.Commit(ctx); e != nil {
			return r, c, e
		}
		return r, c, agentoutbox.ErrUnavailable
	}
	if err != nil {
		return r, c, err
	}
	return commitMemoryClaimTx(ctx, tx, r, c)
}

func consumePreferenceOutboxTx(ctx context.Context, tx pgx.Tx, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	if err := memoryMaintenanceTx(ctx, tx); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if agentoutbox.ValidateClaim(c) != nil || c.HandlerVersion != agentoutbox.PreferenceHandler {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	// Read nomination without an event lock, then serialize in native owner order.
	r, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE event_id=$1 AND subject_id=$2`, c.EventID, c.Subject.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrNotFound
	}
	if err != nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
	}
	if r.Event.SchemaVersion != agentoutbox.PreferenceSchema || r.Event.AgentID != c.AgentID || r.Event.Subject != c.Subject {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	if err = lockMemoryMaintenanceBindingTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	source, current, err := resolvePreferenceOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	r, err = scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE event_id=$1 AND subject_id=$2 FOR UPDATE OF d`, c.EventID, c.Subject.ID))
	if err != nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	if r.Fence != c.Fence {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	if r.State != agentoutbox.Leased {
		p, readErr := scanMemoryReceipt(tx.QueryRow(ctx, `SELECT control_state,fence,attempt,reason_code,created_at,updated_at FROM agent_consumer_inbox WHERE event_id=$1 AND subject_id=$2 AND handler_version=$3`, c.EventID, c.Subject.ID, c.HandlerVersion), c)
		if readErr != nil || p.Fence != c.Fence || p.Attempt != r.Attempt || p.State != r.State {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
		}
		// Immutable history only. It neither proves present source validity nor
		// performs or retries any cleanup; pending progress is not a final outcome.
		return p, ctx.Err()
	}
	if r.LeaseUntil == nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	actual := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID, HandlerVersion: agentoutbox.PreferenceHandler, WorkerID: r.LeaseOwner, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
	var at time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
	}
	if err = agentoutbox.CheckFence(at, actual, c); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if !r.Event.ExpiresAt.After(at) {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrExpired
	}
	n, err := preferenceRootAttemptsTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	state, reason := agentoutbox.MemoryComplete, agentoutbox.ReasonCleanupComplete
	if !current {
		state, reason = agentoutbox.Invalidated, agentoutbox.ReasonSourceInvalidated
	} else {
		if err = preferenceParentCurrentTx(ctx, tx, r.Event); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
		q, moreQ := preferencePreviewCleanupSQL, preferencePreviewMoreSQL
		if r.Event.EventType == agentoutbox.PreferenceContextInvalidation {
			q, moreQ = preferenceGrantCleanupSQL, preferenceGrantMoreSQL
		}
		args := []any{c.Subject.ID, c.AgentID, r.Event.Source.ID, r.Event.Source.Revision, source.row, source.status == agentoutbox.PreferenceConfigured}
		if _, err = tx.Exec(ctx, q, args...); err != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		var more bool
		if tx.QueryRow(ctx, moreQ, args...).Scan(&more) != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		if more {
			state, reason = agentoutbox.Pending, agentoutbox.ReasonCleanupMore
			if n >= agentoutbox.MaxPreferenceRootAttempts {
				state, reason = agentoutbox.DeadLetter, agentoutbox.ReasonRootExhausted
			}
		}
	}
	// Before checkpoint, waits in cleanup cannot turn a late lease into permission.
	if current {
		if err = preferenceFinalTx(ctx, tx, r, c, at); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
	} else {
		var final time.Time
		if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&final) != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		if err = agentoutbox.CheckFence(final, actual, c); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
		if err = outboxControlAdmissionClock(at, final); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
	}
	p, err := scanMemoryReceipt(tx.QueryRow(ctx, `UPDATE agent_consumer_inbox SET control_state=$5,reason_code=$6,updated_at=clock_timestamp()
 WHERE event_id=$1 AND subject_id=$2 AND handler_version=$3 AND fence=$4 AND control_state='LEASED'
 RETURNING control_state,fence,attempt,reason_code,created_at,updated_at`, c.EventID, c.Subject.ID, c.HandlerVersion, c.Fence, state, reason), c)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE agent_domain_outbox SET delivery_state=$4,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp()
 WHERE event_id=$1 AND fence=$2 AND lease_owner=$3 AND delivery_state='LEASED' AND lease_until>clock_timestamp() AND expires_at>clock_timestamp()`, c.EventID, c.Fence, c.WorkerID, state)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	if state == agentoutbox.MemoryComplete && r.Event.EventType == agentoutbox.PreferenceUpdated {
		child := r.Event
		child.EventType = agentoutbox.PreferenceContextInvalidation
		child.LogicalOperationID = agentoutbox.PreferenceOperationID(child.Source.ID, child.Source.Revision, child.EventType)
		cause := r.Event.EventID
		child.CausationID = &cause
		child.ReceivedAt = p.UpdatedAt
		child.EventID = agentoutbox.StableEventID(child)
		if agentoutbox.ValidateEnvelope(child, child.ReceivedAt) != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrExpired
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_domain_outbox(schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,causation_id,occurred_at,received_at,expires_at)
 VALUES($1,$2,$3,$4,$4,$4,$5,'PRIVATE_PREFERENCE',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, child.SchemaVersion, child.EventID, child.EventType, child.Subject.ID, child.AgentID, child.Source.ID, child.Source.Revision, child.Source.Fingerprint, child.Source.Status, child.LogicalOperationID, child.RootTraceID, *child.CausationID, child.OccurredAt, child.ReceivedAt, child.ExpiresAt)
		if err != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
	}
	if current {
		if err = preferenceFinalTx(ctx, tx, r, c, at); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
	}
	if ctx.Err() != nil {
		return agentoutbox.ConsumerRecord{}, ctx.Err()
	}
	return p, nil
}

func (s *Store) consumePreferenceOutbox(ctx context.Context, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	if ctx == nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	defer tx.Rollback(ctx)
	p, err := consumePreferenceOutboxTx(ctx, tx, c)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	return commitMemoryReceiptTx(ctx, tx, p)
}
