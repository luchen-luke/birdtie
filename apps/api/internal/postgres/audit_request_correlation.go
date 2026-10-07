package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/audittrace"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

// Transaction-local and parameterized: never changes pooled connection state or
// native identity. Empty/background contexts reset to NULL for SQL audit defaults.
// The original writer must still perform its final authority/clock check AFTER
// this write and any audit-trigger waits, before committing its original effect.
func setAuditRequest(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT set_config('birdtie.audit_request_id',$1,true)`, audittrace.RequestID(ctx))
	return err
}

func auditExec(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := setAuditRequest(ctx, tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	return tx.Exec(ctx, sql, args...)
}

type auditFailedRow struct{ err error }

func (r auditFailedRow) Scan(...any) error { return r.err }
func auditQueryRow(ctx context.Context, tx pgx.Tx, sql string, args ...any) pgx.Row {
	if err := setAuditRequest(ctx, tx); err != nil {
		return auditFailedRow{err}
	}
	return tx.QueryRow(ctx, sql, args...)
}

type auditPoolRow struct {
	ctx context.Context
	tx  pgx.Tx
	row pgx.Row
}

func (r auditPoolRow) Scan(dest ...any) error {
	defer r.tx.Rollback(context.Background())
	if err := r.row.Scan(dest...); err != nil {
		return err
	}
	return r.tx.Commit(r.ctx)
}

// Existing single-statement CTE domain writers already insert their audit in
// the statement. Preserve that statement and wrap only its transaction-local
// correlation setup; there is no second domain write or detached audit commit.
func auditPoolQueryRow(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) pgx.Row {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return auditFailedRow{err}
	}
	if err = setAuditRequest(ctx, tx); err != nil {
		tx.Rollback(context.Background())
		return auditFailedRow{err}
	}
	return auditPoolRow{ctx, tx, tx.QueryRow(ctx, sql, args...)}
}

// No private body, selection, URL, coordinates, policy JSON or permission proof.
// resourceID retains the domain's existing stable target; targetUUID optionally
// correlates a new concrete grant/revocation without rewriting historical IDs.
func insertDomainAudit(ctx context.Context, tx pgx.Tx, actor, action, kind, resource, purpose string, targetUUID *string) error {
	_, err := auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose,target_resource_id)
 VALUES($1,$2,$3,$4,'allowed',$5,$6)`, actor, action, kind, resource, purpose, targetUUID)
	return err
}

// Called only after the original writer knows its locked, actual transition.
// Classification is attached in the SAME transaction as the unchanged audit;
// retries, rollbacks and older audits never become inferred lifecycle counts.
func insertEnrichmentDomainAudit(ctx context.Context, tx pgx.Tx, actor, agent, action, kind, resource, purpose, metric string) error {
	var id int64
	var occurred time.Time
	err := auditQueryRow(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
 VALUES($1,$2,$3,$4,'allowed',$5) RETURNING id,occurred_at`, actor, action, kind, resource, purpose).Scan(&id, &occurred)
	if err != nil {
		return err
	}
	return insertEnrichmentObservation(ctx, tx, "PERSON", actor, agent, metric, &id, nil, occurred)
}
