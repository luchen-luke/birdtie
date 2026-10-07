package postgres

import (
	"context"
	"errors"
	"time"

	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
)

var _ ar.RecoveryGateway = (*AgentRuns)(nil)

// A closed native reconciliation, not a claim from an error string. A receipt
// or pending dispatch without proof of no effect cannot authorize replay.
func runNoEffectTx(ctx context.Context, tx pgx.Tx, r storedRun) (bool, error) {
	return runNoEffectQueryTx(ctx, tx, r, "")
}
func runNoEffectQueryTx(ctx context.Context, tx pgx.Tx, r storedRun, retiringRun string) (bool, error) {
	var state string
	e := tx.QueryRow(ctx, `SELECT delivery_state FROM agent_domain_outbox WHERE event_id=$1 AND subject_id=$2 AND agent_id=$3 AND logical_operation_id=$4 AND source_id=$5 AND source_revision=$6 FOR UPDATE NOWAIT`, r.record.EventID, r.record.Owner.ID, r.record.AgentID, r.record.LogicalOperationID, r.record.Source.ID, r.record.Source.Version.Revision).Scan(&state)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, runError(e)
	}
	if state != "PENDING" && state != "UNAVAILABLE" {
		return false, nil
	}
	var safe bool
	var excluding any
	if retiringRun != "" {
		excluding = retiringRun
	}
	e = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_effect_ledger WHERE subject_id=$1 AND agent_id=$2 AND logical_operation_id=$3)
 AND NOT EXISTS(SELECT 1 FROM agent_run_dispatches d JOIN agent_enrichment_runs x ON x.id=d.run_id WHERE x.owner_id=$1 AND x.agent_id=$2 AND x.logical_operation_id=$3 AND d.state<>'NO_EFFECT' AND ($5::uuid IS NULL OR d.run_id<>$5))
 AND NOT EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE event_id=$4 AND subject_id=$1 AND control_state IN('LEASED','CANDIDATE_STAGED'))`, r.record.Owner.ID, r.record.AgentID, r.record.LogicalOperationID, r.record.EventID, excluding).Scan(&safe)
	return safe, runErrorIf(e)
}
func runErrorIf(e error) error {
	if e == nil {
		return nil
	}
	return runError(e)
}

func recoveryShape(r storedRun) bool {
	return r.record.Generation == 0 && r.record.State == ar.Failed && r.record.Reason == "ATTEMPTS_EXHAUSTED" && r.grant != ""
}
func (s *AgentRuns) recoverySource(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r storedRun) (time.Time, error) {
	if r.record.Owner.ID != b.owner || r.record.AgentID != b.agent || r.session != b.session || r.authority != b.authority {
		return time.Time{}, ar.ErrDenied
	}
	p, c, e := runGrant(ctx, s.store, tx, b, r.grant)
	if e != nil {
		return time.Time{}, e
	}
	if p.event != r.record.EventID || p.operation != r.record.LogicalOperationID || c.review.Source.Selector.ID != r.record.Source.ID || c.review.Source.Version.Revision != r.record.Source.Version.Revision {
		return time.Time{}, ar.ErrConflict
	}
	end := r.record.Deadline
	contextPurposeLimit(&end, c.end)
	if !end.After(c.at) {
		return time.Time{}, ar.ErrExpired
	}
	return end, nil
}
func (s *AgentRuns) finishRecovery(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r storedRun, end time.Time) error {
	if _, e := s.store.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return runError(e)
	}
	var current bool
	e := tx.QueryRow(ctx, `SELECT birdtie_candidate_pipeline_current($1::uuid)
 AND $2::timestamptz>clock_timestamp()
 AND EXISTS(SELECT 1 FROM agent_enrichment_runs x WHERE x.id=$3 AND x.owner_id=$4 AND x.agent_id=$5 AND x.session_id=$6 AND x.authority_binding=$7 AND x.version=$8 AND x.state='FAILED' AND x.reason='ATTEMPTS_EXHAUSTED' AND x.generation=0 AND x.deadline>clock_timestamp())`, r.grant, end, r.record.ID, b.owner, b.agent, b.session, b.authority, r.record.Version).Scan(&current)
	if e != nil {
		return runError(e)
	}
	if !current {
		return ar.ErrConflict
	}
	return nil
}

func failureView(r storedRun) ar.FailureView {
	return ar.FailureView{Run: r.record, Failure: ar.ClassifyReason(r.record.Reason), DeadLetter: r.record.State == ar.Failed, RecoveryState: "NOT_ELIGIBLE", RootAttemptLimit: ar.MaxRootAttempts, Explanation: "这里只显示本人运行元数据；检查不会执行。恢复须明确确认原版本，且不会延长原期限或替换许可。"}
}
func (s *AgentRuns) ReadOwnFailure(ctx context.Context, a agentprofile.PrivateAccess, id string) (ar.FailureView, error) {
	var empty ar.FailureView
	if !aep.ValidID(id) {
		return empty, ar.ErrInvalid
	}
	tx, b, e := s.begin(ctx, a, false)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(context.Background())
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 AND r.owner_id=$2 AND r.agent_id=$3`, id, b.owner, b.agent))
	if e != nil {
		return empty, runError(e)
	}
	v := failureView(r)
	nativeFinal := false
	if recoveryShape(r) {
		end, sourceErr := s.recoverySource(ctx, tx, b, r)
		if sourceErr != nil {
			v.RecoveryState = "SOURCE_UNAVAILABLE"
		} else {
			safe, e := runNoEffectTx(ctx, tx, r)
			if e != nil {
				return empty, e
			}
			var child bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_enrichment_runs WHERE recovery_root_id=$1)`, id).Scan(&child); e != nil {
				return empty, runError(e)
			}
			switch {
			case child:
				v.RecoveryState = "ALREADY_RECOVERED"
			case !safe:
				v.RecoveryState = "RECONCILIATION_REQUIRED"
				v.Failure = ar.FailureUnknown
			default:
				v.RecoveryState = "AVAILABLE"
				v.Recoverable = true
			}
			if e = s.finishRecovery(ctx, tx, b, r, end); e != nil {
				return empty, e
			}
			nativeFinal = true
		}
	}
	if !nativeFinal {
		if _, e = s.store.finishContextBuilder(ctx, tx, b, nil); e != nil {
			return empty, runError(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, runError(e)
	}
	return v, nil
}
func (s *AgentRuns) ListOwnFailures(ctx context.Context, a agentprofile.PrivateAccess) ([]ar.FailureView, error) {
	tx, b, e := s.begin(ctx, a, false)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	// Listing is historical metadata only, never a recoverability promise. A
	// concrete GET followed by the explicit native POST rechecks current sources.
	rows, e := tx.Query(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.owner_id=$1 AND r.agent_id=$2 AND r.state='FAILED' ORDER BY r.updated_at DESC,r.id LIMIT 50`, b.owner, b.agent)
	if e != nil {
		return nil, runError(e)
	}
	out := []ar.FailureView{}
	for rows.Next() {
		r, e := scanRun(rows)
		if e != nil {
			rows.Close()
			return nil, runError(e)
		}
		out = append(out, failureView(r))
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, runError(e)
	}
	if _, e = s.store.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return nil, runError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, runError(e)
	}
	return out, nil
}
func (s *AgentRuns) RecoverOwn(ctx context.Context, a agentprofile.PrivateAccess, id string, in ar.RecoveryInput) (ar.Record, error) {
	if !aep.ValidID(id) || ar.ValidateRecoveryInput(in) != nil {
		return ar.Record{}, ar.ErrInvalid
	}
	tx, b, e := s.begin(ctx, a, true)
	if e != nil {
		return ar.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 AND r.owner_id=$2 AND r.agent_id=$3`, id, b.owner, b.agent))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if !recoveryShape(r) || r.record.Version != in.ExpectedVersion {
		return ar.Record{}, ar.ErrConflict
	}
	end, e := s.recoverySource(ctx, tx, b, r)
	if e != nil {
		return ar.Record{}, e
	}
	safe, e := runNoEffectTx(ctx, tx, r)
	if e != nil {
		return ar.Record{}, e
	}
	if !safe {
		return ar.Record{}, ar.ErrConflict
	}
	// Source/grant/outbox precede root, matching the original 082 writer. NOWAIT
	// avoids waiting in reverse of background Claim's Run-first lock sequence.
	locked, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1 FOR UPDATE NOWAIT`, id))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	if !recoveryShape(locked) || locked.record.Version != in.ExpectedVersion {
		return ar.Record{}, ar.ErrConflict
	}
	var child bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_enrichment_runs WHERE recovery_root_id=$1)`, id).Scan(&child); e != nil {
		return ar.Record{}, runError(e)
	}
	if child {
		return ar.Record{}, ar.ErrConflict
	}
	var newID string
	e = tx.QueryRow(ctx, `INSERT INTO agent_enrichment_runs(owner_id,agent_id,session_id,authority_binding,source_id,source_revision,event_id,logical_operation_id,retention_grant_id,state,checkpoint,deadline,reason,recovery_root_id,generation,recovery_reason,recovery_previous_version)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'QUEUED','SCHEDULED',$10,'QUEUED',$11,1,$12,$13) RETURNING id::text`, b.owner, b.agent, r.session, r.authority, r.record.Source.ID, r.record.Source.Version.Revision, r.record.EventID, r.record.LogicalOperationID, r.grant, end, id, in.Reason, in.ExpectedVersion).Scan(&newID)
	if e != nil {
		return ar.Record{}, runError(e)
	}
	out, e := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_enrichment_runs r WHERE r.id=$1`, newID))
	if e != nil {
		return ar.Record{}, runError(e)
	}
	// AFTER checkpoint/audit/FK waits, recheck the original current permission
	// and fixed deadline. No database mutation follows this final check.
	if e = s.finishRecovery(ctx, tx, b, r, end); e != nil {
		return ar.Record{}, e
	}
	if ar.ValidateRecord(out.record) != nil {
		return ar.Record{}, ar.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return ar.Record{}, runError(e)
	}
	return out.record, nil
}
