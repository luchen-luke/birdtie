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

// NativeTime, rowToken and this exact 076 purpose token jointly determine
// staleness. The event fingerprint is deliberately not this hash namespace.
const staleTaskRequestPurposeSQL = `p.owner_id=$1 AND p.agent_id=$2 AND p.task_id=$3::uuid AND EXISTS(SELECT 1 FROM jsonb_array_elements(p.sources) s
 WHERE s->>'kind'='CURRENT_TASK_REQUEST' AND s->>'id'=$3::text AND (NOT $6::boolean
 OR s->'version'->>'kind' IS DISTINCT FROM 'UPDATED_AT_DIGEST' OR s->'version'->>'token' IS DISTINCT FROM $7
 OR s->>'nativeTime' IS DISTINCT FROM $4 OR s->>'rowToken' IS DISTINCT FROM $5))`
const taskRequestPreviewCleanupSQL = `WITH stale AS(SELECT p.id FROM agent_context_purpose_previews p WHERE ` + staleTaskRequestPurposeSQL + `
 AND NOT EXISTS(SELECT 1 FROM agent_context_purpose_bindings b WHERE b.preview_id=p.id)
 ORDER BY p.id LIMIT 128 FOR UPDATE OF p)
 DELETE FROM agent_context_purpose_previews p USING stale x WHERE p.id=x.id`
const taskRequestPreviewMoreSQL = `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_previews p WHERE ` + staleTaskRequestPurposeSQL + `
 AND NOT EXISTS(SELECT 1 FROM agent_context_purpose_bindings b WHERE b.preview_id=p.id))`
const taskRequestGrantScopeSQL = `g.owner_account_id=$1 AND g.recipient_account_id=$1 AND g.resource_type='agent_context'
 AND g.resource_id=p.id::text AND g.purpose='TASK_CONTEXT_READ' AND g.actions=ARRAY['read']::text[]
 AND g.revoked_at IS NULL AND ` + staleTaskRequestPurposeSQL
const taskRequestGrantCleanupSQL = `WITH stale AS(SELECT g.id,g.revision FROM consent_grants g
 JOIN agent_context_purpose_bindings b ON b.grant_id=g.id JOIN agent_context_purpose_previews p ON p.id=b.preview_id
 WHERE g.revision<9223372036854775807 AND ` + taskRequestGrantScopeSQL + ` ORDER BY g.id LIMIT 100 FOR UPDATE OF g), stamp AS MATERIALIZED(SELECT clock_timestamp() at)
 UPDATE consent_grants g SET revoked_at=stamp.at,revision=g.revision+1 FROM stale x,stamp
 WHERE g.id=x.id AND g.revision=x.revision AND g.revoked_at IS NULL`

// A revision-exhausted stale grant must remain unfinished, not fake complete.
const taskRequestGrantMoreSQL = `SELECT EXISTS(SELECT 1 FROM consent_grants g JOIN agent_context_purpose_bindings b ON b.grant_id=g.id
 JOIN agent_context_purpose_previews p ON p.id=b.preview_id WHERE ` + taskRequestGrantScopeSQL + `)`

func nominateTaskRequestOutboxTx(ctx context.Context, tx pgx.Tx, subject string) (agentoutbox.Record, time.Time, error) {
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
	rows, err := tx.Query(ctx, outboxControlDispatchHeadsSQL, agentoutbox.TaskRequestHandler, at, filter, agentoutbox.MaxControlTenantHeads)
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
	r, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.subject_id=$2 AND `+outboxControlDueSQL+` ORDER BY d.occurred_at,d.event_id LIMIT 1`, agentoutbox.TaskRequestHandler, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, at, agentoutbox.ErrDispatchBusy
	}
	if err != nil || r.Event.Subject.ID != owner || r.Event.SchemaVersion != agentoutbox.TaskRequestSchema {
		return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
	}
	return r, at, nil
}

func claimTaskRequestOutboxTx(ctx context.Context, tx pgx.Tx, worker, subject string) (agentoutbox.Record, agentoutbox.Claim, error) {
	if err := memoryMaintenanceTx(ctx, tx); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	r, admission, err := nominateTaskRequestOutboxTx(ctx, tx, subject)
	if err != nil {
		return r, agentoutbox.Claim{}, err
	}
	if err = lockMemoryMaintenanceBindingTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	_, valid, err := resolveTaskRequestOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	locked, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.event_id=$1 AND d.subject_id=$2 AND `+strings.ReplaceAll(outboxControlDueSQL, "$1", "$3")+` FOR UPDATE OF d`, r.Event.EventID, r.Event.Subject.ID, agentoutbox.TaskRequestHandler))
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
	n, err := taskRequestRootAttemptsTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	state := agentoutbox.State("")
	if !valid {
		state = agentoutbox.Invalidated
	} else if !r.Event.ExpiresAt.After(at) {
		state = agentoutbox.Expired
	} else if n >= agentoutbox.MaxTaskRequestRootAttempts || r.Fence == math.MaxInt64 {
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
		return agentoutbox.Record{}, agentoutbox.Claim{}, errTaskRequestTerminalControl
	}
	if err = taskRequestParentCurrentTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	r, err = scanAgentOutbox(tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE agent_domain_outbox d
 SET delivery_state='LEASED',attempt=d.attempt+1,fence=d.fence+1,lease_owner=$2,updated_at=clk.at,lease_until=LEAST(clk.at+interval '30 seconds',d.expires_at)
 FROM clk WHERE d.event_id=$1 AND d.expires_at>clk.at RETURNING `+outboxColumns, r.Event.EventID, worker))
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, `INSERT INTO agent_consumer_inbox(event_id,subject_id,handler_version,control_state,fence,attempt)
 VALUES($1,$2,'task-request-invalidation-v1','LEASED',$3,$4) ON CONFLICT(event_id,subject_id,handler_version) DO UPDATE
 SET control_state='LEASED',reason_code='',fence=EXCLUDED.fence,attempt=EXCLUDED.attempt,updated_at=clock_timestamp()
 WHERE agent_consumer_inbox.control_state IN('LEASED','PENDING')`, r.Event.EventID, r.Event.Subject.ID, r.Fence, r.Attempt)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrConflict
	}
	c := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID, HandlerVersion: agentoutbox.TaskRequestHandler, WorkerID: worker, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
	if err = taskRequestFinalTx(ctx, tx, r, c, admission, nil); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	return r, c, nil
}

var errTaskRequestTerminalControl = errors.New("native task request terminal control pending commit")

func (s *Store) claimTaskRequestOutbox(ctx context.Context, worker, subject string) (agentoutbox.Record, agentoutbox.Claim, error) {
	if ctx == nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrInvalid
	}
	if ctx.Err() != nil || s == nil || s.pool == nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	if _, err := agentoutbox.NormalizeWorkerID(worker); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	defer tx.Rollback(ctx)
	r, c, err := claimTaskRequestOutboxTx(ctx, tx, worker, subject)
	if err == errTaskRequestTerminalControl {
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

func consumeTaskRequestOutboxTx(ctx context.Context, tx pgx.Tx, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	if err := memoryMaintenanceTx(ctx, tx); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if agentoutbox.ValidateClaim(c) != nil || c.HandlerVersion != agentoutbox.TaskRequestHandler {
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
	if r.Event.SchemaVersion != agentoutbox.TaskRequestSchema || r.Event.AgentID != c.AgentID || r.Event.Subject != c.Subject {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	if err = lockMemoryMaintenanceBindingTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	source, current, err := resolveTaskRequestOutboxTx(ctx, tx, r.Event)
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
	actual := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID, HandlerVersion: agentoutbox.TaskRequestHandler, WorkerID: r.LeaseOwner, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
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
	n, err := taskRequestRootAttemptsTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	state, reason := agentoutbox.MemoryComplete, agentoutbox.ReasonCleanupComplete
	if !current {
		state, reason = agentoutbox.Invalidated, agentoutbox.ReasonSourceInvalidated
	} else {
		if err = taskRequestParentCurrentTx(ctx, tx, r.Event); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
		q, moreQ := taskRequestPreviewCleanupSQL, taskRequestPreviewMoreSQL
		if r.Event.EventType == agentoutbox.TaskContextInvalidation {
			q, moreQ = taskRequestGrantCleanupSQL, taskRequestGrantMoreSQL
		}
		args := []any{c.Subject.ID, c.AgentID, r.Event.Source.ID, source.at.Format(time.RFC3339Nano), source.row, source.contextCurrent, source.purposeToken}
		if _, err = tx.Exec(ctx, q, args...); err != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		var more bool
		if tx.QueryRow(ctx, moreQ, args...).Scan(&more) != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		if more {
			state, reason = agentoutbox.Pending, agentoutbox.ReasonCleanupMore
			if n >= agentoutbox.MaxTaskRequestRootAttempts {
				state, reason = agentoutbox.DeadLetter, agentoutbox.ReasonRootExhausted
			}
		}
	}
	// Before checkpoint, waits in cleanup cannot turn a late lease into permission.
	if current {
		if err = taskRequestFinalTx(ctx, tx, r, c, at, &source); err != nil {
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
	if state == agentoutbox.MemoryComplete && r.Event.EventType == agentoutbox.TaskRequestChanged {
		child := r.Event
		child.EventType = agentoutbox.TaskContextInvalidation
		child.LogicalOperationID = agentoutbox.TaskRequestOperationID(child.Source.ID, child.Source.Fingerprint, child.EventType)
		cause := r.Event.EventID
		child.CausationID = &cause
		child.ReceivedAt = p.UpdatedAt
		child.EventID = agentoutbox.StableEventID(child)
		if agentoutbox.ValidateEnvelope(child, child.ReceivedAt) != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrExpired
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_domain_outbox(schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,causation_id,occurred_at,received_at,expires_at)
 VALUES($1,$2,$3,$4,$4,$4,$5,'TASK_REQUEST',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, child.SchemaVersion, child.EventID, child.EventType, child.Subject.ID, child.AgentID, child.Source.ID, child.Source.Revision, child.Source.Fingerprint, child.Source.Status, child.LogicalOperationID, child.RootTraceID, *child.CausationID, child.OccurredAt, child.ReceivedAt, child.ExpiresAt)
		if err != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
	}
	if current {
		if err = taskRequestFinalTx(ctx, tx, r, c, at, &source); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
	}
	if ctx.Err() != nil {
		return agentoutbox.ConsumerRecord{}, ctx.Err()
	}
	return p, nil
}

func (s *Store) consumeTaskRequestOutbox(ctx context.Context, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	if ctx == nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	if ctx.Err() != nil || s == nil || s.pool == nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	defer tx.Rollback(ctx)
	p, err := consumeTaskRequestOutboxTx(ctx, tx, c)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	return commitMemoryReceiptTx(ctx, tx, p)
}
