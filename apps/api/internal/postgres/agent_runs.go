package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Run metadata is separate from the human Moment commit. This service never
// calls a model; staging remains the original purpose-bound 082 native writer.
type AgentRuns struct {
	store *Store
	flags *agentfeature.Controller
}

func NewAgentRuns(s *Store, c *agentfeature.Controller) *AgentRuns { return &AgentRuns{s, c} }

var _ ar.Gateway = (*AgentRuns)(nil)

type storedRun struct {
	record                            ar.Record
	session, authority, grant, effect string
	fence                             int64
	worker                            string
	lease                             *time.Time
}

const runColumns = `r.id::text,r.owner_id::text,r.agent_id::text,r.session_id::text,r.authority_binding,r.source_id::text,r.source_revision,r.event_id::text,r.logical_operation_id::text,COALESCE(r.retention_grant_id::text,''),r.state,r.checkpoint,r.version,r.attempt,r.fence,COALESCE(r.worker_id::text,''),r.lease_until,r.deadline,r.reason,COALESCE(r.candidate_id::text,''),COALESCE(r.effect_key,''),r.created_at,r.updated_at,clock_timestamp(),r.generation,COALESCE(r.recovery_root_id::text,''),COALESCE(r.recovery_reason,''),COALESCE(r.recovery_previous_version,0)`

func scanRun(row scanner) (storedRun, error) {
	var r storedRun
	var owner string
	v := &r.record
	e := row.Scan(&v.ID, &owner, &v.AgentID, &r.session, &r.authority, &v.Source.ID, &v.Source.Version.Revision, &v.EventID, &v.LogicalOperationID, &r.grant, &v.State, &v.Checkpoint, &v.Version, &v.Attempt, &r.fence, &r.worker, &r.lease, &v.Deadline, &v.Reason, &v.CandidateID, &r.effect, &v.CreatedAt, &v.UpdatedAt, &v.ObservedAt, &v.Generation, &v.RecoveryRootID, &v.RecoveryReason, &v.RecoveryPreviousVersion)
	v.SchemaVersion = ar.Schema
	v.RetentionGrantID = r.grant
	v.Owner = actorref.PrincipalRef{Type: actorref.Person, ID: owner}
	v.Source.Type = agentevent.MomentSource
	v.Source.Owner = v.Owner
	v.Source.Version.Kind = agentevent.RevisionVersion
	v.Committed = v.State == ar.Succeeded
	return r, e
}
func runError(e error) error {
	switch {
	case errors.Is(e, ar.ErrInvalid), errors.Is(e, acr.ErrInvalid):
		return ar.ErrInvalid
	case errors.Is(e, ar.ErrDenied), errors.Is(e, acr.ErrDenied), errors.Is(e, acp.ErrDenied), errors.Is(e, pgx.ErrNoRows):
		return ar.ErrDenied
	case errors.Is(e, ar.ErrExpired), errors.Is(e, acr.ErrExpired), errors.Is(e, acp.ErrExpired):
		return ar.ErrExpired
	case errors.Is(e, ar.ErrConflict), errors.Is(e, acr.ErrConflict), errors.Is(e, acp.ErrConflict), errors.Is(e, acp.ErrBusy):
		return ar.ErrConflict
	}
	var p *pgconn.PgError
	if errors.As(e, &p) && (p.Code == "55P03" || p.Code == "23505" || p.Code == "40P01") {
		return ar.ErrConflict
	}
	// Preserve native dispatch control after the existing authority/error
	// precedence. The bounded worker distinguishes throttling from real empty.
	if errors.Is(e, ar.ErrDispatchBusy) {
		return ar.ErrDispatchBusy
	}
	if errors.Is(e, ar.ErrNotFound) {
		return ar.ErrNotFound
	}
	return ar.ErrUnavailable
}

const runGateSQL = `SELECT (SELECT count(*)=4 FROM pg_tables WHERE schemaname='public' AND tablename IN('agent_enrichment_runs','agent_run_steps','agent_run_dispatches','agent_run_audit')) AND (SELECT count(*)=2 FROM pg_trigger t WHERE t.tgrelid=to_regclass('public.agent_enrichment_runs') AND NOT t.tgisinternal AND t.tgenabled='O' AND ((t.tgname='agent_run_guard' AND t.tgfoid=to_regprocedure('public.birdtie_agent_run_guard()')) OR(t.tgname='agent_run_checkpoint' AND t.tgfoid=to_regprocedure('public.birdtie_agent_run_checkpoint()'))))`

func runGate(ctx context.Context, tx pgx.Tx, write bool) error {
	var ready bool
	if tx.QueryRow(ctx, runGateSQL).Scan(&ready) != nil || !ready {
		return ar.ErrUnavailable
	}
	mode := "ACCESS SHARE"
	if write {
		mode = "ROW EXCLUSIVE"
	}
	if _, e := tx.Exec(ctx, `LOCK TABLE agent_enrichment_runs,agent_run_steps,agent_run_dispatches,agent_run_audit IN `+mode+` MODE`); e != nil {
		return runError(e)
	}
	if tx.QueryRow(ctx, runGateSQL).Scan(&ready) != nil || !ready {
		return ar.ErrUnavailable
	}
	return nil
}
func (s *AgentRuns) begin(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, contextBuilderBinding, error) {
	if s == nil || s.store == nil {
		return nil, contextBuilderBinding{}, ar.ErrUnavailable
	}
	tx, b, e := s.store.beginRetention(ctx, a, write)
	if e != nil {
		return nil, b, runError(e)
	}
	if write {
		if e = setAuditRequest(ctx, tx); e != nil {
			tx.Rollback(context.Background())
			return nil, b, ar.ErrUnavailable
		}
	}
	if e = runGate(ctx, tx, write); e != nil {
		tx.Rollback(context.Background())
		return nil, b, e
	}
	return tx, b, nil
}
func runGrant(ctx context.Context, s *Store, tx pgx.Tx, b contextBuilderBinding, gid string) (retentionStored, retentionCapture, error) {
	g, p, e := retentionGrantTx(ctx, tx, b, gid)
	if e != nil {
		return p, retentionCapture{}, runError(e)
	}
	if g.RevokedAt != nil || p.session != b.session {
		return p, retentionCapture{}, ar.ErrDenied
	}
	c, e := s.captureRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm)
	if e != nil {
		return p, c, runError(e)
	}
	if !retentionMatch(p, c) || g.ExpiresAt.After(p.end) {
		return p, c, ar.ErrConflict
	}
	contextPurposeLimit(&c.end, g.ExpiresAt)
	return p, c, nil
}
func (s *AgentRuns) ScheduleOwn(ctx context.Context, a agentprofile.PrivateAccess, in ar.Input) (ar.Record, error) {
	if ar.ValidateInput(in) != nil {
		return ar.Record{}, ar.ErrInvalid
	}
	tx, b, e := s.begin(ctx, a, true)
	if e != nil {
		return ar.Record{}, e
	}
	defer tx.Rollback(context.Background())
	var rev int64
	var updated time.Time
	if e = tx.QueryRow(ctx, `SELECT revision,updated_at FROM moments WHERE id=$1 AND author_account_id=$2 AND visibility='private' AND status='draft' FOR SHARE`, in.MomentID, b.owner).Scan(&rev, &updated); e != nil {
		return ar.Record{}, runError(e)
	}
	kind := agentoutbox.MomentCreated
	if rev > 1 {
		kind = agentoutbox.MomentUpdated
	}
	ev, _, e := resolveOutboxMomentTx(ctx, tx, in.MomentID, kind)
	if e != nil || ev.Subject.ID != b.owner || ev.AgentID != b.agent {
		return ar.Record{}, ar.ErrDenied
	}
	var captured bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_domain_outbox WHERE event_id=$1 AND subject_id=$2 AND source_id=$3 AND source_revision=$4 AND agent_id=$5 AND logical_operation_id=$6)`, ev.EventID, b.owner, in.MomentID, rev, b.agent, ev.LogicalOperationID).Scan(&captured) != nil || !captured {
		return ar.Record{}, ar.ErrDenied
	}
	at, e := s.store.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return ar.Record{}, runError(e)
	}
	end := at.Add(ar.MaxLifetime)
	contextPurposeLimit(&end, updated.Add(ar.MaxLifetime))
	contextPurposeLimit(&end, b.limit)
	state, reason := ar.WaitingConfirmation, "WAITING_APPROVAL"
	var grant any
	if in.RetentionGrantID != "" {
		p, c, x := runGrant(ctx, s.store, tx, b, in.RetentionGrantID)
		if x != nil {
			return ar.Record{}, x
		}
		if p.event != ev.EventID || p.operation != ev.LogicalOperationID || c.review.Source.Selector.ID != in.MomentID || c.review.Source.Version.Revision != rev {
			return ar.Record{}, ar.ErrConflict
		}
		contextPurposeLimit(&end, c.end)
		state, reason, grant = ar.Queued, "QUEUED", in.RetentionGrantID
	}
	if !end.After(at) {
		return ar.Record{}, ar.ErrExpired
	}
	// Idempotence keeps the original deadline and Session. A new grant or login
	// cannot renew an old logical operation; attach requires a separate own CAS.
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO agent_enrichment_runs(owner_id,agent_id,session_id,authority_binding,source_id,source_revision,event_id,logical_operation_id,retention_grant_id,state,checkpoint,deadline,reason,created_at,updated_at,next_attempt_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'SCHEDULED',$11,$12,$13,$13,$13) ON CONFLICT(owner_id,agent_id,logical_operation_id,generation) DO NOTHING RETURNING id::text`, b.owner, b.agent, b.session, b.authority, in.MomentID, rev, ev.EventID, ev.LogicalOperationID, grant, state, end, reason, at).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(ctx, `SELECT id::text FROM agent_enrichment_runs WHERE owner_id=$1 AND agent_id=$2 AND logical_operation_id=$3 AND generation=0`, b.owner, b.agent, ev.LogicalOperationID).Scan(&id)
	}
	if e != nil {
		return ar.Record{}, runError(e)
	}
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 AND r.owner_id=$2`, id, b.owner))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if _, e = s.store.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return ar.Record{}, runError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return ar.Record{}, runError(e)
	}
	return r.record, nil
}
func (s *AgentRuns) ReadOwn(ctx context.Context, a agentprofile.PrivateAccess, id string) (ar.Record, error) {
	if !aep.ValidID(id) {
		return ar.Record{}, ar.ErrInvalid
	}
	tx, b, e := s.begin(ctx, a, false)
	if e != nil {
		return ar.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 AND r.owner_id=$2 AND r.agent_id=$3`, id, b.owner, b.agent))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if _, e = s.store.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return ar.Record{}, runError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return ar.Record{}, runError(e)
	}
	return r.record, nil
}
func (s *AgentRuns) CancelOwn(ctx context.Context, a agentprofile.PrivateAccess, id string, version int64) (ar.Record, error) {
	if !aep.ValidID(id) || version < 1 {
		return ar.Record{}, ar.ErrInvalid
	}
	tx, b, e := s.begin(ctx, a, true)
	if e != nil {
		return ar.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 AND r.owner_id=$2 AND r.agent_id=$3 FOR UPDATE`, id, b.owner, b.agent))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if r.record.Version != version || ar.Terminal(r.record.State) {
		return ar.Record{}, ar.ErrConflict
	}
	if _, e = s.store.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return ar.Record{}, runError(e)
	}
	r, e = scanRun(tx.QueryRow(ctx, `UPDATE agent_enrichment_runs r SET state='CANCELLED',reason='CANCELLED',worker_id=NULL,lease_until=NULL,version=version+1,updated_at=clock_timestamp() WHERE r.id=$1 RETURNING `+runColumns, id))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return ar.Record{}, runError(e)
	}
	return r.record, nil
}
func (s *AgentRuns) AttachOwnGrant(ctx context.Context, a agentprofile.PrivateAccess, id string, version int64, gid string) (ar.Record, error) {
	if !aep.ValidID(id) || !aep.ValidID(gid) || version < 1 {
		return ar.Record{}, ar.ErrInvalid
	}
	tx, b, e := s.begin(ctx, a, true)
	if e != nil {
		return ar.Record{}, e
	}
	defer tx.Rollback(context.Background())
	// Source/grant locks precede Run locks, matching the original candidate writer.
	p, c, e := runGrant(ctx, s.store, tx, b, gid)
	if e != nil {
		return ar.Record{}, e
	}
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 AND r.owner_id=$2 AND r.agent_id=$3 FOR UPDATE NOWAIT`, id, b.owner, b.agent))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if r.record.State != ar.WaitingConfirmation || r.record.Version != version || r.session != b.session || r.authority != b.authority || r.record.EventID != p.event || r.record.LogicalOperationID != p.operation || !r.record.Deadline.After(c.at) {
		return ar.Record{}, ar.ErrConflict
	}
	// Attaching cannot extend the fixed Run deadline; the original writer also
	// checks the independently shorter consent/source/Session deadline each time.
	r, e = scanRun(tx.QueryRow(ctx, `UPDATE agent_enrichment_runs r SET state='QUEUED',reason='QUEUED',retention_grant_id=$2,version=version+1,updated_at=clock_timestamp(),next_attempt_at=clock_timestamp() WHERE r.id=$1 AND r.deadline>clock_timestamp() RETURNING `+runColumns, id, gid))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if _, e = s.store.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return ar.Record{}, runError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return ar.Record{}, runError(e)
	}
	return r.record, nil
}

// candidateRunGuard is not the test fault hook. It is only supplied by the
// persistent worker, and locks/checks/completes the Run in the actual effect Tx.
type candidateRunGuard interface {
	begin(context.Context, pgx.Tx, contextBuilderBinding, retentionStored, string) error
	complete(context.Context, pgx.Tx, acp.Receipt) error
}
type runLeaseGuard struct{ claim ar.Claim }

func (g runLeaseGuard) begin(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, p retentionStored, grantID string) error {
	if e := setAuditRequest(ctx, tx); e != nil {
		return ar.ErrUnavailable
	}
	if ar.ValidateClaim(g.claim) != nil {
		return ar.ErrInvalid
	}
	if e := runGate(ctx, tx, true); e != nil {
		return e
	}
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 FOR UPDATE NOWAIT`, g.claim.RunID))
	if e != nil {
		return runError(e)
	}
	if r.record.State != ar.Running || r.record.Owner.ID != b.owner || r.record.AgentID != b.agent || r.session != b.session || r.authority != b.authority || r.grant != grantID || r.record.EventID != p.event || r.record.LogicalOperationID != p.operation || r.lease == nil {
		return ar.ErrDenied
	}
	current := ar.Claim{RunID: r.record.ID, WorkerID: r.worker, Fence: r.fence, Attempt: r.record.Attempt, LeaseUntil: *r.lease}
	if e = ar.CheckLease(r.record.ObservedAt, current, g.claim); e != nil {
		return e
	}
	if !r.record.Deadline.After(r.record.ObservedAt) {
		return ar.ErrExpired
	}
	return nil
}
func (g runLeaseGuard) complete(ctx context.Context, tx pgx.Tx, receipt acp.Receipt) error {
	// This is after native deferred effect checks and all their waits. The same
	// commit records candidate+effect+consumer checkpoint+Run result. No second
	// metadata success can stand in for an unknown original side effect.
	tag, e := tx.Exec(ctx, `UPDATE agent_enrichment_runs SET state='SUCCEEDED',checkpoint='EFFECT_CONFIRMED',reason='COMMITTED',candidate_id=$5,effect_key=$6,worker_id=NULL,lease_until=NULL,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND state='RUNNING' AND worker_id=$2 AND fence=$3 AND attempt=$4 AND lease_until=$7 AND lease_until>clock_timestamp() AND deadline>clock_timestamp()`, g.claim.RunID, g.claim.WorkerID, g.claim.Fence, g.claim.Attempt, receipt.CandidateID, receipt.EffectKey, g.claim.LeaseUntil)
	if e != nil {
		return runError(e)
	}
	if tag.RowsAffected() != 1 {
		return ar.ErrExpired
	}
	_, e = tx.Exec(ctx, `UPDATE agent_run_dispatches SET state='COMMITTED',candidate_id=$3,effect_key=$4,updated_at=clock_timestamp() WHERE run_id=$1 AND fence=$2 AND state='PENDING_RECONCILIATION'`, g.claim.RunID, g.claim.Fence, receipt.CandidateID, receipt.EffectKey)
	if e != nil {
		return runError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE agent_run_dispatches SET state='NO_EFFECT',updated_at=clock_timestamp() WHERE run_id=$1 AND fence<$2 AND state='PENDING_RECONCILIATION'`, g.claim.RunID, g.claim.Fence)
	if e != nil {
		return runError(e)
	}
	// Run UPDATE triggers and dispatch rows can wait after its WHERE predicate.
	// Recheck the exact original authority and fixed claim lease after ALL those
	// waits. No subsequent database write/lock may precede the caller's commit.
	var current bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_enrichment_runs r WHERE r.id=$1 AND r.state='SUCCEEDED' AND r.fence=$2 AND r.attempt=$3 AND r.candidate_id=$4 AND r.effect_key=$5 AND r.retention_grant_id=$6 AND birdtie_candidate_pipeline_current(r.retention_grant_id) AND r.deadline>clock_timestamp() AND $7::timestamptz>clock_timestamp())`, g.claim.RunID, g.claim.Fence, g.claim.Attempt, receipt.CandidateID, receipt.EffectKey, receipt.RetentionGrantID, g.claim.LeaseUntil).Scan(&current)
	if e != nil {
		return runError(e)
	}
	if !current {
		return ar.ErrExpired
	}
	return nil
}

// ClaimAgentRun is a bounded native worker call, not a human/provider API. A
// new fence is committed before work; an expired lease first enters recovery.
func (s *AgentRuns) ClaimAgentRun(ctx context.Context, worker string) (ar.Claim, error) {
	return s.claimAgentRun(ctx, worker, ar.MaxLease)
}
func (s *AgentRuns) claimAgentRun(ctx context.Context, worker string, duration time.Duration) (ar.Claim, error) {
	if !aep.ValidID(worker) || duration <= 0 || duration > ar.MaxLease {
		return ar.Claim{}, ar.ErrInvalid
	}
	if s == nil || s.store == nil || s.flags == nil {
		return ar.Claim{}, ar.ErrUnavailable
	}
	ticket, e := s.flags.Capture(agentfeature.Memory)
	if e != nil {
		return ar.Claim{}, ar.ErrUnavailable
	}
	tx, e := s.store.pool.Begin(ctx)
	if e != nil {
		return ar.Claim{}, ar.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = setAuditRequest(ctx, tx); e != nil {
		return ar.Claim{}, ar.ErrUnavailable
	}
	if e = runGate(ctx, tx, true); e != nil {
		return ar.Claim{}, e
	}
	r, e := selectAgentRunDispatch(ctx, tx)
	if errors.Is(e, pgx.ErrNoRows) {
		return ar.Claim{}, ar.ErrNotFound
	}
	if e != nil {
		return ar.Claim{}, runError(e)
	}
	if r.record.State == ar.Running {
		_, e = tx.Exec(ctx, `UPDATE agent_enrichment_runs SET state='RETRY_WAIT',checkpoint='RECONCILE_EFFECT',reason='RECONCILING',version=version+1,worker_id=NULL,lease_until=NULL,updated_at=clock_timestamp(),next_attempt_at=clock_timestamp() WHERE id=$1`, r.record.ID)
		if e != nil {
			return ar.Claim{}, runError(e)
		}
	}
	limit := ar.MaxAttempts
	if r.record.Generation == 1 {
		limit = ar.MaxRecoveryAttempts
	}
	if r.record.Attempt >= limit {
		// The locked Run fences out old guarded 082 writers. Independently lock
		// the original outbox NOWAIT and prove no ledger/inbox effect before
		// retiring this exact Run's pending dispatches. Exhaustion alone is not
		// evidence that a dispatch had no effect.
		noEffect, checkErr := runNoEffectQueryTx(ctx, tx, r, r.record.ID)
		if checkErr != nil {
			return ar.Claim{}, checkErr
		}
		_, e = tx.Exec(ctx, `UPDATE agent_enrichment_runs SET state='FAILED',reason='ATTEMPTS_EXHAUSTED',version=version+1,worker_id=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1`, r.record.ID)
		if e != nil {
			return ar.Claim{}, runError(e)
		}
		if noEffect {
			if _, e = tx.Exec(ctx, `UPDATE agent_run_dispatches SET state='NO_EFFECT',updated_at=clock_timestamp() WHERE run_id=$1 AND state='PENDING_RECONCILIATION'`, r.record.ID); e != nil {
				return ar.Claim{}, runError(e)
			}
		}
		if !s.flags.Current(ticket) {
			return ar.Claim{}, ar.ErrUnavailable
		}
		if e = tx.Commit(ctx); e != nil {
			return ar.Claim{}, runError(e)
		}
		return ar.Claim{}, ar.ErrClaimExhausted
	}
	reason, checkpoint := "DISPATCHING", ar.DispatchPending
	if r.record.Attempt > 0 {
		reason, checkpoint = "RECONCILING", ar.ReconcileEffect
	}
	r, e = scanRun(tx.QueryRow(ctx, `UPDATE agent_enrichment_runs r SET state='RUNNING',checkpoint=$2,reason=$3,worker_id=$4,lease_until=least(deadline,clock_timestamp()+$5::interval),attempt=attempt+1,fence=fence+1,version=version+1,updated_at=clock_timestamp() WHERE r.id=$1 AND r.deadline>clock_timestamp() RETURNING `+runColumns, r.record.ID, checkpoint, reason, worker, duration.String()))
	if e != nil || r.lease == nil {
		return ar.Claim{}, runError(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO agent_run_dispatches(run_id,fence,state) VALUES($1,$2,'PENDING_RECONCILIATION')`, r.record.ID, r.fence)
	if e != nil {
		return ar.Claim{}, runError(e)
	}
	if !s.flags.Current(ticket) {
		return ar.Claim{}, ar.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return ar.Claim{}, runError(e)
	}
	return ar.Claim{RunID: r.record.ID, WorkerID: worker, Fence: r.fence, Attempt: r.record.Attempt, LeaseUntil: *r.lease}, nil
}

func (s *AgentRuns) ExecuteAgentRun(ctx context.Context, c ar.Claim) (ar.Record, error) {
	if ar.ValidateClaim(c) != nil {
		return ar.Record{}, ar.ErrInvalid
	}
	if s == nil || s.store == nil || s.flags == nil {
		return ar.Record{}, ar.ErrUnavailable
	}
	var a agentprofile.PrivateAccess
	var owner, gid, digest string
	// This is only a native, already human-scheduled operation under the exact
	// original Session and concrete grant. No token is stored in Run or audit.
	e := s.store.pool.QueryRow(ctx, `SELECT r.owner_id::text,r.retention_grant_id::text,encode(se.token_sha256,'hex') FROM agent_enrichment_runs r JOIN sessions se ON se.id=r.session_id AND se.account_id=r.owner_id WHERE r.id=$1 AND r.state='RUNNING' AND r.worker_id=$2 AND r.fence=$3 AND r.attempt=$4 AND r.lease_until=$5 AND r.lease_until>clock_timestamp() AND r.deadline>clock_timestamp() AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()`, c.RunID, c.WorkerID, c.Fence, c.Attempt, c.LeaseUntil).Scan(&owner, &gid, &digest)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return s.reconcileAgentRun(ctx, c, ar.ErrDenied)
		}
		return ar.Record{}, ar.ErrUnavailable
	}
	raw, e := hex.DecodeString(digest)
	if e != nil || len(raw) != 32 {
		return ar.Record{}, ar.ErrUnavailable
	}
	copy(a.SessionDigest[:], raw)
	a.WorkspacePrincipal = actorref.PrincipalRef{Type: actorref.Person, ID: owner}
	executor := &candidatePipelineExecutor{store: s.store, runGuard: runLeaseGuard{c}}
	service := acp.NewService(executor, s.flags)
	_, e = service.StageOwnMomentCandidate(ctx, a, gid)
	if e != nil {
		// Even post-commit response cancellation can produce an error. Reconcile
		// durable Run/effect first; a terminal success is never downgraded/retried.
		return s.reconcileAgentRun(ctx, c, e)
	}
	r, e := scanRun(s.store.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1`, c.RunID))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	return r.record, nil
}
func (s *AgentRuns) reconcileAgentRun(ctx context.Context, c ar.Claim, cause error) (ar.Record, error) {
	if ctx.Err() != nil {
		return ar.Record{}, ar.ErrUnavailable
	} // recover on a later bounded invocation
	tx, e := s.store.pool.Begin(ctx)
	if e != nil {
		return ar.Record{}, ar.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = setAuditRequest(ctx, tx); e != nil {
		return ar.Record{}, ar.ErrUnavailable
	}
	if e = runGate(ctx, tx, true); e != nil {
		return ar.Record{}, e
	}
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 FOR UPDATE`, c.RunID))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if ar.Terminal(r.record.State) {
		if e = tx.Commit(ctx); e != nil {
			return ar.Record{}, runError(e)
		}
		return r.record, nil
	}
	if r.record.State != ar.Running || r.lease == nil || r.worker != c.WorkerID || r.fence != c.Fence || r.record.Attempt != c.Attempt || !r.lease.Equal(c.LeaseUntil) {
		return ar.Record{}, ar.ErrConflict
	}
	state, reason := ar.RetryWait, "RETRY_UNAVAILABLE"
	if !r.record.Deadline.After(r.record.ObservedAt) {
		state, reason = ar.Expired, "EXPIRED"
	} else if errors.Is(cause, ar.ErrDenied) || errors.Is(cause, acp.ErrDenied) || errors.Is(cause, acp.ErrExpired) || errors.Is(cause, acp.ErrConflict) {
		state, reason = ar.Failed, "AUTHORITY_CHANGED"
	} else if errors.Is(cause, ar.ErrInvalid) || errors.Is(cause, acp.ErrInvalid) || errors.Is(cause, acr.ErrInvalid) {
		state, reason = ar.Failed, "INVALID_INPUT"
	} else if r.record.Attempt >= ar.MaxAttempts || (r.record.Generation == 1 && r.record.Attempt >= ar.MaxRecoveryAttempts) {
		state, reason = ar.Failed, "ATTEMPTS_EXHAUSTED"
	} else if errors.Is(cause, acp.ErrBusy) || errors.Is(cause, ar.ErrConflict) {
		reason = "RETRY_BUSY"
	}
	// A completed original effect under a different original grant is not a
	// license to dispatch another. The pipeline checks original stable effect
	// addressing and refuses that conflict; failures never renew source/consent.
	noEffect, checkErr := runNoEffectQueryTx(ctx, tx, r, r.record.ID)
	if checkErr != nil {
		return ar.Record{}, checkErr
	}
	// Error type alone cannot retire a dispatch. The original outbox lock and
	// this Run's fence exclude an in-progress same-transaction 082 effect;
	// unknown durable receipts remain pending for explicit reconciliation.
	r, e = scanRun(tx.QueryRow(ctx, `UPDATE agent_enrichment_runs r SET state=$2,reason=$3,checkpoint='RECONCILE_EFFECT',version=version+1,worker_id=NULL,lease_until=NULL,updated_at=clock_timestamp(),next_attempt_at=clock_timestamp()+interval '1 second' WHERE r.id=$1 RETURNING `+runColumns, c.RunID, state, reason))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if noEffect {
		if _, e = tx.Exec(ctx, `UPDATE agent_run_dispatches SET state='NO_EFFECT',updated_at=clock_timestamp() WHERE run_id=$1 AND fence=$2 AND state='PENDING_RECONCILIATION'`, c.RunID, c.Fence); e != nil {
			return ar.Record{}, runError(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return ar.Record{}, runError(e)
	}
	return r.record, nil
}

// ExpireAgentRuns maintains terminal metadata only, in a bounded SKIP LOCKED
// batch. It never dispatches a model, changes a Moment or promotes Memory.
func (s *AgentRuns) ExpireAgentRuns(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 100 {
		return 0, ar.ErrInvalid
	}
	if s == nil || s.store == nil {
		return 0, ar.ErrUnavailable
	}
	tx, e := s.store.pool.Begin(ctx)
	if e != nil {
		return 0, ar.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = setAuditRequest(ctx, tx); e != nil {
		return 0, ar.ErrUnavailable
	}
	if e = runGate(ctx, tx, true); e != nil {
		return 0, e
	}
	tag, e := tx.Exec(ctx, `WITH due AS MATERIALIZED(SELECT id FROM agent_enrichment_runs WHERE state IN('QUEUED','RUNNING','WAITING_CONFIRMATION','RETRY_WAIT') AND deadline<=clock_timestamp() ORDER BY deadline,id LIMIT $1 FOR UPDATE SKIP LOCKED) UPDATE agent_enrichment_runs r SET state='EXPIRED',reason='EXPIRED',version=version+1,worker_id=NULL,lease_until=NULL,updated_at=clock_timestamp() FROM due WHERE r.id=due.id`, limit)
	if e != nil {
		return 0, runError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, runError(e)
	}
	return tag.RowsAffected(), nil
}
