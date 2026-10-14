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

// Existing 094 metadata records BEFORE-change epochs; timestamps here are
// native database watermarks, never token/fingerprint or string conversions.
const staleParticipationEvidenceSQL = `e.owner_id=$1 AND e.agent_id=$2 AND e.status='CURRENT' AND e.source_type='ACTIVITY_PARTICIPATION' AND e.source_id=$3::uuid AND(
 NOT $7::boolean OR e.source_token IS DISTINCT FROM $4::text OR EXISTS(SELECT 1 FROM agent_memory_source_invalidations inv WHERE inv.owner_id=e.owner_id AND inv.source_type=e.source_type AND inv.source_id=e.source_id AND inv.id<=$6::bigint AND inv.created_at>=e.observed_at))`
const staleParticipationCandidateSQL = `c.owner_id=$1 AND c.agent_id=$2 AND c.status IN('CANDIDATE','ACTIVE') AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.sources) s WHERE s->'selector'->>'type'='ACTIVITY_PARTICIPATION' AND s->'selector'->>'id'=$3::text AND(
 NOT $7::boolean OR s->'version'->>'token' IS DISTINCT FROM $4::text OR s->>'fingerprint' IS DISTINCT FROM $5::text OR EXISTS(SELECT 1 FROM agent_memory_source_invalidations inv WHERE inv.owner_id=c.owner_id AND inv.source_type='ACTIVITY_PARTICIPATION' AND inv.source_id=$3::uuid AND inv.id<=$6::bigint AND inv.created_at>=c.created_at)))`
const participationEvidenceCleanupSQL = `WITH args AS(SELECT $5::text candidate_fingerprint),stale AS(SELECT e.id,e.version FROM agent_memory_evidence e WHERE e.version<9223372036854775807 AND ` + staleParticipationEvidenceSQL + ` ORDER BY e.id LIMIT 128 FOR UPDATE OF e SKIP LOCKED)
 UPDATE agent_memory_evidence e SET status='REMOVED',version=e.version+1,source_type=NULL,source_id=NULL,source_version_kind=NULL,source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL FROM stale x WHERE e.id=x.id AND e.version=x.version AND e.status='CURRENT'`
const participationCandidateCleanupSQL = `WITH stale AS(SELECT c.id,c.version FROM agent_memory_candidates c WHERE c.version<9223372036854775807 AND ` + staleParticipationCandidateSQL + ` ORDER BY c.id LIMIT 128 FOR UPDATE OF c SKIP LOCKED)
 UPDATE agent_memory_candidates c SET status='EXPIRED',version=c.version+1,predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL FROM stale x WHERE c.id=x.id AND c.version=x.version AND c.status IN('CANDIDATE','ACTIVE')`
const participationCleanupMoreSQL = `SELECT EXISTS(SELECT 1 FROM agent_memory_evidence e WHERE ` + staleParticipationEvidenceSQL + `) OR EXISTS(SELECT 1 FROM agent_memory_candidates c WHERE ` + staleParticipationCandidateSQL + `)`

func nominateParticipationOutboxTx(ctx context.Context, tx pgx.Tx, subject string) (agentoutbox.Record, time.Time, error) {
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
	rows, err := tx.Query(ctx, outboxControlDispatchHeadsSQL, agentoutbox.ParticipationHandler, at, filter, agentoutbox.MaxControlTenantHeads)
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
	r, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.subject_id=$2 AND `+outboxControlDueSQL+` ORDER BY d.occurred_at,d.event_id LIMIT 1`, agentoutbox.ParticipationHandler, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, at, agentoutbox.ErrDispatchBusy
	}
	if err != nil || r.Event.Subject.ID != owner || r.Event.SchemaVersion != agentoutbox.ParticipationSchema {
		return agentoutbox.Record{}, at, agentoutbox.ErrUnavailable
	}
	return r, at, nil
}

func claimParticipationOutboxTx(ctx context.Context, tx pgx.Tx, worker, subject string) (agentoutbox.Record, agentoutbox.Claim, error) {
	if err := memoryMaintenanceTx(ctx, tx); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	r, admission, err := nominateParticipationOutboxTx(ctx, tx, subject)
	if err != nil {
		return r, agentoutbox.Claim{}, err
	}
	if err = lockMemoryMaintenanceBindingTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	_, valid, err := resolveParticipationOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	locked, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.event_id=$1 AND d.subject_id=$2 AND `+strings.ReplaceAll(outboxControlDueSQL, "$1", "$3")+` FOR UPDATE OF d`, r.Event.EventID, r.Event.Subject.ID, agentoutbox.ParticipationHandler))
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
	n, err := participationRootAttemptsTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	state := agentoutbox.State("")
	if !valid {
		state = agentoutbox.Invalidated
	} else if !r.Event.ExpiresAt.After(at) {
		state = agentoutbox.Expired
	} else if n >= agentoutbox.MaxParticipationRootAttempts || r.Fence == math.MaxInt64 {
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
		return agentoutbox.Record{}, agentoutbox.Claim{}, errParticipationTerminalControl
	}

	r, err = scanAgentOutbox(tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE agent_domain_outbox d
 SET delivery_state='LEASED',attempt=d.attempt+1,fence=d.fence+1,lease_owner=$2,updated_at=clk.at,lease_until=LEAST(clk.at+interval '30 seconds',d.expires_at)
 FROM clk WHERE d.event_id=$1 AND d.expires_at>clk.at RETURNING `+outboxColumns, r.Event.EventID, worker))
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, `INSERT INTO agent_consumer_inbox(event_id,subject_id,handler_version,control_state,fence,attempt)
 VALUES($1,$2,'participation-invalidation-v1','LEASED',$3,$4) ON CONFLICT(event_id,subject_id,handler_version) DO UPDATE
 SET control_state='LEASED',reason_code='',fence=EXCLUDED.fence,attempt=EXCLUDED.attempt,updated_at=clock_timestamp()
 WHERE agent_consumer_inbox.control_state IN('LEASED','PENDING')`, r.Event.EventID, r.Event.Subject.ID, r.Fence, r.Attempt)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrConflict
	}
	c := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID, HandlerVersion: agentoutbox.ParticipationHandler, WorkerID: worker, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
	if err = participationFinalTx(ctx, tx, r, c, admission); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	return r, c, nil
}

var errParticipationTerminalControl = errors.New("native participation terminal control pending commit")

func (s *Store) claimParticipationOutbox(ctx context.Context, worker, subject string) (agentoutbox.Record, agentoutbox.Claim, error) {
	if ctx == nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	defer tx.Rollback(ctx)
	r, c, err := claimParticipationOutboxTx(ctx, tx, worker, subject)
	if err == errParticipationTerminalControl {
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

func consumeParticipationOutboxTx(ctx context.Context, tx pgx.Tx, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	if err := memoryMaintenanceTx(ctx, tx); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if agentoutbox.ValidateClaim(c) != nil || c.HandlerVersion != agentoutbox.ParticipationHandler {
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
	if r.Event.SchemaVersion != agentoutbox.ParticipationSchema || r.Event.AgentID != c.AgentID || r.Event.Subject != c.Subject {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	if err = lockMemoryMaintenanceBindingTx(ctx, tx, r.Event); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	source, current, err := resolveParticipationOutboxTx(ctx, tx, r.Event)
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
	actual := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID, HandlerVersion: agentoutbox.ParticipationHandler, WorkerID: r.LeaseOwner, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
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
	n, err := participationRootAttemptsTx(ctx, tx, r.Event)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	state, reason := agentoutbox.MemoryComplete, agentoutbox.ReasonCleanupComplete
	if !current {
		state, reason = agentoutbox.Invalidated, agentoutbox.ReasonSourceInvalidated
	} else {

		args := []any{c.Subject.ID, c.AgentID, r.Event.Source.ID, source.snapshot, source.candidate, source.watermark, source.usable}
		if _, err = tx.Exec(ctx, participationEvidenceCleanupSQL, args...); err != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		if _, err = tx.Exec(ctx, participationCandidateCleanupSQL, args...); err != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		var more bool
		if tx.QueryRow(ctx, participationCleanupMoreSQL, args...).Scan(&more) != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
		if more {
			state, reason = agentoutbox.Pending, agentoutbox.ReasonCleanupMore
			if n >= agentoutbox.MaxParticipationRootAttempts {
				state, reason = agentoutbox.DeadLetter, agentoutbox.ReasonRootExhausted
			}
		}
	}
	// Before checkpoint, waits in cleanup cannot turn a late lease into permission.
	if current {
		if err = participationFinalTx(ctx, tx, r, c, at, &source); err != nil {
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
	if current {
		if err = participationFinalTx(ctx, tx, r, c, at, &source); err != nil {
			return agentoutbox.ConsumerRecord{}, err
		}
	}
	if ctx.Err() != nil {
		return agentoutbox.ConsumerRecord{}, ctx.Err()
	}
	return p, nil
}

func (s *Store) consumeParticipationOutbox(ctx context.Context, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	if ctx == nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	defer tx.Rollback(ctx)
	p, err := consumeParticipationOutboxTx(ctx, tx, c)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	return commitMemoryReceiptTx(ctx, tx, p)
}
