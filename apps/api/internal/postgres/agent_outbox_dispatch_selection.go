package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
)

// Only metadata-control admissions use this lock; it is not the Run, candidate,
// authorization or effect lock. Busy lock holders are never polled in-process.
const outboxControlDispatchLockSQL = `SELECT pg_try_advisory_xact_lock(hashtextextended('birdtie:outbox-control-dispatch:v1',0))`
const outboxControlActiveSQL = `d.delivery_state='LEASED' AND d.lease_until>stamp.n AND d.expires_at>stamp.n
 AND EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id
 AND i.fence=d.fence AND i.attempt=d.attempt AND i.control_state='LEASED'
 AND (i.handler_version IN('mom-control-v1','mom-control-v2') OR i.handler_version IN('memory-invalidation-v1','preference-invalidation-v1')))`
const outboxControlDispatchCountSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT stamp.n,(SELECT count(*) FROM agent_domain_outbox d WHERE ` + outboxControlActiveSQL + `) FROM stamp`

// Original eligibility includes expired/stale work so the original terminal
// control path can retire it; admission never renews expiry or source metadata.
const outboxControlDueSQL = `((d.schema_version='agent-outbox-v1' AND d.source_type='MOMENT' AND $1 IN('mom-control-v1','mom-control-v2'))
 OR(d.schema_version='agent-memory-outbox-v1' AND d.source_type='MEMORY' AND $1='memory-invalidation-v1')
 OR(d.schema_version='agent-preference-outbox-v1' AND d.source_type='PRIVATE_PREFERENCE' AND $1='preference-invalidation-v1'))
 AND d.next_attempt_at<=clock_timestamp() AND (
 d.delivery_state='PENDING' OR (d.delivery_state='LEASED' AND d.lease_until<=clock_timestamp()
 AND EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id
 AND i.handler_version=$1 AND i.fence=d.fence AND i.control_state='LEASED'))
 OR(d.delivery_state='UNAVAILABLE' AND NOT EXISTS(SELECT 1 FROM agent_consumer_inbox i
 WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id AND i.handler_version=$1)))`

// Ranking combines retained control service/confirmation with terminal queue
// progress. Initial invalidation/expiry and refresh coalescing may have no inbox;
// their updated_at is progress, never an invented claim or authority receipt.
var outboxControlDispatchHeadsSQL = `WITH due AS MATERIALIZED(
 SELECT d.subject_id,min(d.next_attempt_at) next_due FROM agent_domain_outbox d
 WHERE ($3::uuid IS NULL OR d.subject_id=$3) AND ` + strings.ReplaceAll(outboxControlDueSQL, "clock_timestamp()", "$2::timestamptz") + ` GROUP BY d.subject_id),
 stamp AS MATERIALIZED(SELECT $2::timestamptz n)
 SELECT due.subject_id::text,
 (SELECT count(*) FROM agent_domain_outbox d CROSS JOIN stamp WHERE d.subject_id=due.subject_id AND ` + outboxControlActiveSQL + `) active,
 GREATEST((SELECT max(i.updated_at) FROM agent_consumer_inbox i WHERE i.subject_id=due.subject_id
 AND (i.handler_version IN('mom-control-v1','mom-control-v2') OR i.handler_version IN('memory-invalidation-v1','preference-invalidation-v1'))),
 (SELECT max(progress_d.updated_at) FROM agent_domain_outbox progress_d
 WHERE progress_d.subject_id=due.subject_id AND ((progress_d.schema_version='agent-outbox-v1' AND progress_d.source_type='MOMENT')
 OR(progress_d.schema_version='agent-memory-outbox-v1' AND progress_d.source_type='MEMORY')
 OR(progress_d.schema_version='agent-preference-outbox-v1' AND progress_d.source_type='PRIVATE_PREFERENCE'))
 AND progress_d.delivery_state IN('INVALIDATED','EXPIRED','DEAD_LETTER')
 AND progress_d.attempt=0 AND progress_d.fence=0 AND progress_d.lease_owner IS NULL AND progress_d.lease_until IS NULL
 AND NOT EXISTS(SELECT 1 FROM agent_consumer_inbox progress_i
 WHERE progress_i.event_id=progress_d.event_id AND progress_i.subject_id=progress_d.subject_id))) last_served,
 due.next_due FROM due ORDER BY active,last_served NULLS FIRST,due.next_due,due.subject_id LIMIT $4`

func outboxControlSelectionError(err error) error {
	switch {
	case errors.Is(err, agentoutbox.ErrInvalid):
		return agentoutbox.ErrInvalid
	case errors.Is(err, agentoutbox.ErrExpired):
		return agentoutbox.ErrExpired
	case errors.Is(err, agentoutbox.ErrConflict):
		return agentoutbox.ErrConflict
	}
	return agentoutbox.ErrUnavailable
}

func outboxControlPick(at time.Time, active int, heads []ar.DispatchTenant) (string, error) {
	owner, err := ar.PickDispatchTenant(at, active, heads)
	switch err {
	case nil:
		return owner, nil
	case ar.ErrDispatchBusy:
		return "", agentoutbox.ErrDispatchBusy
	case ar.ErrNotFound:
		return "", agentoutbox.ErrNotFound
	default:
		return "", agentoutbox.ErrUnavailable
	}
}

// Called by the original claim transaction before its outbox/source row locks.
// The admission lock survives through the existing commit/rollback. Original
// current source, lease/fence, terminal checks and last PG clock stay in caller.
func selectAgentOutboxControlTx(ctx context.Context, tx pgx.Tx, handler agentoutbox.HandlerVersion, subjectID string) (agentoutbox.Record, time.Time, error) {
	if ctx == nil || tx == nil || agentoutbox.ValidateHandlerVersion(handler) != nil {
		return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrInvalid
	}
	if v := reflect.ValueOf(tx); v.Kind() == reflect.Pointer && v.IsNil() {
		return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrInvalid
	}
	if subjectID != "" {
		ref, err := actorref.ParsePrincipal("PERSON", subjectID)
		if err != nil || ref.ID != subjectID || subjectID == "00000000-0000-0000-0000-000000000000" {
			return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return agentoutbox.Record{}, time.Time{}, err
	}
	var locked bool
	if err := tx.QueryRow(ctx, outboxControlDispatchLockSQL).Scan(&locked); err != nil {
		return agentoutbox.Record{}, time.Time{}, outboxControlSelectionError(err)
	}
	if !locked {
		return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrDispatchBusy
	}
	var at time.Time
	var active int
	if err := tx.QueryRow(ctx, outboxControlDispatchCountSQL).Scan(&at, &active); err != nil {
		return agentoutbox.Record{}, time.Time{}, outboxControlSelectionError(err)
	}
	if _, err := outboxControlPick(at, active, nil); err != nil && err != agentoutbox.ErrNotFound {
		return agentoutbox.Record{}, time.Time{}, err
	}
	var subjectFilter any
	if subjectID != "" {
		subjectFilter = subjectID
	}
	rows, err := tx.Query(ctx, outboxControlDispatchHeadsSQL, handler, at, subjectFilter, agentoutbox.MaxControlTenantHeads)
	if err != nil {
		return agentoutbox.Record{}, time.Time{}, outboxControlSelectionError(err)
	}
	var heads []ar.DispatchTenant
	for rows.Next() {
		var head ar.DispatchTenant
		if err = rows.Scan(&head.Owner, &head.Active, &head.LastServed, &head.NextDue); err != nil {
			rows.Close()
			return agentoutbox.Record{}, time.Time{}, outboxControlSelectionError(err)
		}
		heads = append(heads, head)
		if len(heads) > agentoutbox.MaxControlTenantHeads || subjectID != "" && head.Owner != subjectID {
			rows.Close()
			return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrUnavailable
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return agentoutbox.Record{}, time.Time{}, outboxControlSelectionError(err)
	}
	owner, err := outboxControlPick(at, active, heads)
	if err != nil {
		return agentoutbox.Record{}, time.Time{}, err
	}
	r, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d
	WHERE d.subject_id=$2 AND `+outboxControlDueSQL+`
 ORDER BY d.occurred_at,d.event_id LIMIT 1 FOR UPDATE OF d SKIP LOCKED`, handler, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		// Only this chosen head disappeared/was locked, not proof of global empty.
		return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrDispatchBusy
	}
	if err != nil {
		return agentoutbox.Record{}, time.Time{}, outboxControlSelectionError(err)
	}
	if r.Event.Subject.ID != owner {
		return agentoutbox.Record{}, time.Time{}, agentoutbox.ErrUnavailable
	}
	return r, at, nil
}

// The original claim checks its lease and expiry first. This additional guard
// prevents a clock regression before admission from reviving leases excluded
// by the first count. It is not a clock tolerance or permission source.
func outboxControlAdmissionClock(admission, final time.Time) error {
	if admission.IsZero() || final.IsZero() || admission.UTC().Year() < 1 || admission.UTC().Year() > 9999 || final.UTC().Year() < 1 || final.UTC().Year() > 9999 || final.Before(admission) {
		return agentoutbox.ErrUnavailable
	}
	return nil
}
