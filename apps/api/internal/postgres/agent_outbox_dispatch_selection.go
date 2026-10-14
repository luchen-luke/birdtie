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
 AND (i.handler_version IN('mom-control-v1','mom-control-v2') OR i.handler_version IN('memory-invalidation-v1','preference-invalidation-v1','task-request-invalidation-v1','participation-invalidation-v1')))`
const outboxControlDispatchCountSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT stamp.n,(SELECT count(*) FROM agent_domain_outbox d WHERE ` + outboxControlActiveSQL + `) FROM stamp`

// Original eligibility includes expired/stale work so the original terminal
// control path can retire it; admission never renews expiry or source metadata.
const outboxControlDueSQL = `((d.schema_version='agent-outbox-v1' AND d.source_type='MOMENT' AND $1 IN('mom-control-v1','mom-control-v2'))
 OR(d.schema_version='agent-memory-outbox-v1' AND d.source_type='MEMORY' AND $1='memory-invalidation-v1')
 OR(d.schema_version='agent-preference-outbox-v1' AND d.source_type='PRIVATE_PREFERENCE' AND $1='preference-invalidation-v1')
 OR(d.schema_version='agent-task-request-outbox-v1' AND d.source_type='TASK_REQUEST' AND $1='task-request-invalidation-v1')
 OR(d.schema_version='agent-participation-outbox-v1' AND d.source_type='ACTIVITY_PARTICIPATION' AND $1='participation-invalidation-v1'))
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
 AND (i.handler_version IN('mom-control-v1','mom-control-v2') OR i.handler_version IN('memory-invalidation-v1','preference-invalidation-v1','task-request-invalidation-v1','participation-invalidation-v1'))),
 (SELECT max(progress_d.updated_at) FROM agent_domain_outbox progress_d
 WHERE progress_d.subject_id=due.subject_id AND ((progress_d.schema_version='agent-outbox-v1' AND progress_d.source_type='MOMENT')
 OR(progress_d.schema_version='agent-memory-outbox-v1' AND progress_d.source_type='MEMORY')
 OR(progress_d.schema_version='agent-preference-outbox-v1' AND progress_d.source_type='PRIVATE_PREFERENCE')
 OR(progress_d.schema_version='agent-task-request-outbox-v1' AND progress_d.source_type='TASK_REQUEST')
 OR(progress_d.schema_version='agent-participation-outbox-v1' AND progress_d.source_type='ACTIVITY_PARTICIPATION'))
 AND progress_d.delivery_state IN('INVALIDATED','EXPIRED','DEAD_LETTER')
 AND progress_d.attempt=0 AND progress_d.fence=0 AND progress_d.lease_owner IS NULL AND progress_d.lease_until IS NULL
 AND NOT EXISTS(SELECT 1 FROM agent_consumer_inbox progress_i
 WHERE progress_i.event_id=progress_d.event_id AND progress_i.subject_id=progress_d.subject_id))) last_served,
 due.next_due FROM due ORDER BY active,last_served NULLS FIRST,due.next_due,due.subject_id LIMIT $4`

// Mixed mode nominates across the five actual native maintenance families.
// It does not invent new event types or replay the optional Moment v2 handler.
const mixedOutboxHandlersSQL = `(VALUES('agent-outbox-v1','MOMENT','mom-control-v1'),
 ('agent-memory-outbox-v1','MEMORY','memory-invalidation-v1'),
 ('agent-preference-outbox-v1','PRIVATE_PREFERENCE','preference-invalidation-v1'),
 ('agent-task-request-outbox-v1','TASK_REQUEST','task-request-invalidation-v1'),
 ('agent-participation-outbox-v1','ACTIVITY_PARTICIPATION','participation-invalidation-v1'))`

var mixedOutboxDueSQL = strings.ReplaceAll(outboxControlDueSQL, "$1", "h.handler")
var mixedOutboxDispatchHeadsSQL = `WITH due AS MATERIALIZED(
 SELECT d.subject_id,min(d.next_attempt_at) next_due FROM agent_domain_outbox d
 JOIN ` + mixedOutboxHandlersSQL + ` h(schema,source,handler) ON d.schema_version=h.schema AND d.source_type=h.source
 WHERE ($2::uuid IS NULL OR d.subject_id=$2) AND ` + strings.ReplaceAll(mixedOutboxDueSQL, "clock_timestamp()", "$1::timestamptz") + ` GROUP BY d.subject_id),
 stamp AS MATERIALIZED(SELECT $1::timestamptz n)
 SELECT due.subject_id::text,
 (SELECT count(*) FROM agent_domain_outbox d CROSS JOIN stamp WHERE d.subject_id=due.subject_id AND ` + outboxControlActiveSQL + `) active,
 GREATEST((SELECT max(i.updated_at) FROM agent_consumer_inbox i WHERE i.subject_id=due.subject_id
 AND i.handler_version IN('mom-control-v1','mom-control-v2','memory-invalidation-v1','preference-invalidation-v1','task-request-invalidation-v1','participation-invalidation-v1')),
 (SELECT max(d.updated_at) FROM agent_domain_outbox d JOIN ` + mixedOutboxHandlersSQL + ` h(schema,source,handler)
 ON d.schema_version=h.schema AND d.source_type=h.source WHERE d.subject_id=due.subject_id
 AND d.delivery_state IN('INVALIDATED','EXPIRED','DEAD_LETTER') AND d.attempt=0 AND d.fence=0
 AND d.lease_owner IS NULL AND d.lease_until IS NULL
 AND NOT EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id))) last_served,
 due.next_due FROM due ORDER BY active,last_served NULLS FIRST,due.next_due,due.subject_id LIMIT $3`

// Family service is durable original inbox/terminal progress, not queue length.
// An owner's burst in one family cannot monopolize every turn of mixed mode.
var mixedOutboxFamilySQL = `SELECT h.handler FROM ` + mixedOutboxHandlersSQL + ` h(schema,source,handler)
 JOIN LATERAL(SELECT min(d.next_attempt_at) next_due FROM agent_domain_outbox d
 WHERE d.subject_id=$1 AND d.schema_version=h.schema AND d.source_type=h.source AND ` + strings.ReplaceAll(mixedOutboxDueSQL, "clock_timestamp()", "$2::timestamptz") + `) due ON due.next_due IS NOT NULL
 ORDER BY GREATEST((SELECT max(i.updated_at) FROM agent_consumer_inbox i WHERE i.subject_id=$1 AND i.handler_version=h.handler),
 (SELECT max(d.updated_at) FROM agent_domain_outbox d WHERE d.subject_id=$1 AND d.schema_version=h.schema AND d.source_type=h.source
 AND d.delivery_state IN('INVALIDATED','EXPIRED','DEAD_LETTER') AND d.attempt=0 AND d.fence=0
 AND d.lease_owner IS NULL AND d.lease_until IS NULL
 AND NOT EXISTS(SELECT 1 FROM agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id))) NULLS FIRST,
 due.next_due,h.handler LIMIT 1`

func selectMixedOutboxControlTx(ctx context.Context, tx pgx.Tx, subject string) (string, agentoutbox.HandlerVersion, time.Time, error) {
	var at time.Time
	var locked bool
	if e := tx.QueryRow(ctx, outboxControlDispatchLockSQL).Scan(&locked); e != nil {
		return "", "", at, outboxControlSelectionError(e)
	}
	if !locked {
		return "", "", at, agentoutbox.ErrDispatchBusy
	}
	var active int
	if e := tx.QueryRow(ctx, outboxControlDispatchCountSQL).Scan(&at, &active); e != nil {
		return "", "", at, outboxControlSelectionError(e)
	}
	if _, e := outboxControlPick(at, active, nil); e != nil && e != agentoutbox.ErrNotFound {
		return "", "", at, e
	}
	var filter any
	if subject != "" {
		filter = subject
	}
	rows, e := tx.Query(ctx, mixedOutboxDispatchHeadsSQL, at, filter, agentoutbox.MaxControlTenantHeads)
	if e != nil {
		return "", "", at, outboxControlSelectionError(e)
	}
	var heads []ar.DispatchTenant
	for rows.Next() {
		var h ar.DispatchTenant
		if e = rows.Scan(&h.Owner, &h.Active, &h.LastServed, &h.NextDue); e != nil {
			rows.Close()
			return "", "", at, outboxControlSelectionError(e)
		}
		heads = append(heads, h)
		if len(heads) > agentoutbox.MaxControlTenantHeads || subject != "" && h.Owner != subject {
			rows.Close()
			return "", "", at, agentoutbox.ErrUnavailable
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return "", "", at, outboxControlSelectionError(e)
	}
	owner, e := outboxControlPick(at, active, heads)
	if e != nil {
		return "", "", at, e
	}
	var handler agentoutbox.HandlerVersion
	if e = tx.QueryRow(ctx, mixedOutboxFamilySQL, owner, at).Scan(&handler); errors.Is(e, pgx.ErrNoRows) {
		return "", "", at, agentoutbox.ErrDispatchBusy
	} else if e != nil {
		return "", "", at, outboxControlSelectionError(e)
	}
	if handler != agentoutbox.HandlerV1 && handler != agentoutbox.MemoryHandler && handler != agentoutbox.PreferenceHandler && handler != agentoutbox.TaskRequestHandler && handler != agentoutbox.ParticipationHandler {
		return "", "", at, agentoutbox.ErrUnavailable
	}
	return owner, handler, at, nil
}

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
