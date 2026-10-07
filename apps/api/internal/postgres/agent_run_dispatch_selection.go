package postgres

import (
	"context"
	"errors"
	"time"

	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
)

const agentRunDispatchLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended('birdtie:agent-run-dispatch:v1',0))`
const agentRunDispatchCountSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT stamp.n,(SELECT count(*) FROM agent_enrichment_runs r WHERE r.state='RUNNING' AND r.deadline>stamp.n AND r.lease_until>stamp.n) FROM stamp`
const agentRunDispatchHeadsSQL = `WITH due AS MATERIALIZED(
 SELECT r.owner_id,min(r.next_attempt_at) AS next_due FROM agent_enrichment_runs r
 WHERE r.deadline>$1 AND ((r.state IN('QUEUED','RETRY_WAIT') AND r.next_attempt_at<=$1) OR(r.state='RUNNING' AND r.lease_until<=$1)) GROUP BY r.owner_id)
 SELECT d.owner_id::text,
 (SELECT count(*) FROM agent_enrichment_runs running WHERE running.owner_id=d.owner_id AND running.state='RUNNING' AND running.deadline>$1 AND running.lease_until>$1) AS active,
 (SELECT max(a.occurred_at) FROM agent_run_audit a JOIN agent_enrichment_runs served ON served.id=a.run_id WHERE served.owner_id=d.owner_id AND a.to_state='RUNNING') AS last_served,
 d.next_due FROM due d ORDER BY active,last_served NULLS FIRST,d.next_due,d.owner_id LIMIT $2`

// Called by the original claim transaction before any candidate is updated.
// Admission is serialized across new workers and released with their commit or
// rollback; the original per-run lock/fence/effect reconciliation is retained.
// Unit spies cannot prove actual PG fairness, lock behavior or throughput.
func selectAgentRunDispatch(ctx context.Context, tx pgx.Tx) (storedRun, error) {
	if _, e := tx.Exec(ctx, agentRunDispatchLockSQL); e != nil {
		return storedRun{}, runError(e)
	}
	var at time.Time
	var active int
	if e := tx.QueryRow(ctx, agentRunDispatchCountSQL).Scan(&at, &active); e != nil {
		return storedRun{}, runError(e)
	}
	if _, e := ar.PickDispatchTenant(at, active, nil); e != nil && !errors.Is(e, ar.ErrNotFound) {
		return storedRun{}, e
	}
	rows, e := tx.Query(ctx, agentRunDispatchHeadsSQL, at, ar.MaxDispatchTenantHeads)
	if e != nil {
		return storedRun{}, runError(e)
	}
	var heads []ar.DispatchTenant
	for rows.Next() {
		var h ar.DispatchTenant
		if e = rows.Scan(&h.Owner, &h.Active, &h.LastServed, &h.NextDue); e != nil {
			rows.Close()
			return storedRun{}, runError(e)
		}
		heads = append(heads, h)
		if len(heads) > ar.MaxDispatchTenantHeads {
			rows.Close()
			return storedRun{}, ar.ErrUnavailable
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return storedRun{}, runError(e)
	}
	owner, e := ar.PickDispatchTenant(at, active, heads)
	if e != nil {
		return storedRun{}, e
	}
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.owner_id=$1 AND r.deadline>clock_timestamp() AND ((r.state IN('QUEUED','RETRY_WAIT') AND r.next_attempt_at<=clock_timestamp()) OR(r.state='RUNNING' AND r.lease_until<=clock_timestamp())) ORDER BY r.next_attempt_at,r.id LIMIT 1 FOR UPDATE SKIP LOCKED`, owner))
	if errors.Is(e, pgx.ErrNoRows) {
		// The chosen row may have retired/locked after the metadata read. This
		// is not proof that every tenant's queue is empty.
		return storedRun{}, ar.ErrDispatchBusy
	}
	if e != nil {
		return storedRun{}, runError(e)
	}
	return r, nil
}
