package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5/pgconn"
)

func exhaustedRecoveryFixture(t *testing.T) (*retentionFixture, *AgentRuns, agentprofile.PrivateAccess, ar.Record) {
	t.Helper()
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	for i := int64(1); i <= ar.MaxAttempts; i++ {
		c, e := s.claimAgentRun(b.ctx, runWorker1, 100*time.Millisecond)
		if e != nil || c.RunID != r.ID || c.Attempt != i {
			t.Fatal("native original claim", c, e)
		}
		time.Sleep(time.Until(c.LeaseUntil) + 20*time.Millisecond)
	}
	if _, e = s.ClaimAgentRun(b.ctx, runWorker1); !errors.Is(e, ar.ErrClaimExhausted) {
		t.Fatal("exhaustion", e)
	}
	r, e = s.ReadOwn(b.ctx, a, r.ID)
	requireRun(t, r, e, ar.Failed)
	return f, s, a, r
}
func recoveryRootBytes(t *testing.T, f *retentionFixture, id string) string {
	t.Helper()
	b := f.f.f.place.private.base
	var raw string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('row',to_jsonb(r),'xmin',r.xmin::text,'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY version) FROM agent_run_audit a WHERE run_id=r.id))::text FROM agent_enrichment_runs r WHERE r.id=$1`, id).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestAgentRunRecoveryNativeHumanGenerationOneEffectAndHistory(t *testing.T) {
	f, s, a, r := exhaustedRecoveryFixture(t)
	b := f.f.f.place.private.base
	old := recoveryRootBytes(t, f, r.ID)
	before := pipelineCount(t, f)
	v, e := s.ReadOwnFailure(b.ctx, a, r.ID)
	if e != nil || ar.ValidateFailureView(v) != nil || !v.Recoverable || !v.DeadLetter {
		t.Fatal("native preview", v, e)
	}
	list, e := s.ListOwnFailures(b.ctx, a)
	if e != nil || len(list) != 1 || list[0].Recoverable {
		t.Fatal("bounded historical listing", list, e)
	}
	if pipelineCount(t, f) != before || recoveryRootBytes(t, f, r.ID) != old {
		t.Fatal("read mutated original")
	}
	var out [2]ar.Record
	var errs [2]error
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i], errs[i] = s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason})
		}(i)
	}
	wg.Wait()
	wins := 0
	var child ar.Record
	for i, e := range errs {
		if e == nil {
			wins++
			child = out[i]
		} else if !errors.Is(e, ar.ErrConflict) {
			t.Fatal("competing recovery", e)
		}
	}
	if wins != 1 || child.Generation != 1 || child.RecoveryRootID != r.ID || child.EventID != r.EventID || child.LogicalOperationID != r.LogicalOperationID || child.RetentionGrantID != r.RetentionGrantID || child.Deadline.After(r.Deadline) {
		t.Fatal("one exact child", wins, out, errs)
	}
	if recoveryRootBytes(t, f, r.ID) != old {
		t.Fatal("FAILED history rewritten")
	}
	c, e := s.ClaimAgentRun(b.ctx, runWorker2)
	if e != nil || c.RunID != child.ID || c.Attempt != 1 {
		t.Fatal(c, e)
	}
	success, e := s.ExecuteAgentRun(b.ctx, c)
	requireRun(t, success, e, ar.Succeeded)
	var count int
	var attempts int64
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*),coalesce(sum(attempt),0) FROM agent_enrichment_runs WHERE id=$1 OR recovery_root_id=$1`, r.ID).Scan(&count, &attempts); e != nil || count != 2 || attempts != 6 {
		t.Fatal("root count", count, attempts, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1 AND logical_operation_id=$2`, a.WorkspacePrincipal.ID, r.LogicalOperationID).Scan(&count); e != nil || count != 1 {
		t.Fatal("original effect exactly once", count, e)
	}
	stable := pipelineCount(t, f)
	for i := 0; i < 5; i++ {
		again, e := s.ExecuteAgentRun(b.ctx, c)
		requireRun(t, again, e, ar.Succeeded)
		if again.CandidateID != success.CandidateID {
			t.Fatal("new effect")
		}
	}
	if stable != pipelineCount(t, f) || old != recoveryRootBytes(t, f, r.ID) {
		t.Fatal("original history or effect changed")
	}
	if _, e = s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason}); !errors.Is(e, ar.ErrConflict) {
		t.Fatal("second generation", e)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "UNSELECTED") || strings.Contains(string(raw), "token") || strings.Contains(string(raw), "authority") {
		t.Fatal("DLQ private payload")
	}
}
func TestAgentRunRecoveryNativeChildSingleAttemptAndDatabaseGuard(t *testing.T) {
	f, s, a, r := exhaustedRecoveryFixture(t)
	b := f.f.f.place.private.base
	child, e := s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason})
	requireRun(t, child, e, ar.Queued)
	c, e := s.claimAgentRun(b.ctx, runWorker2, 100*time.Millisecond)
	if e != nil || c.RunID != child.ID {
		t.Fatal(c, e)
	}
	time.Sleep(time.Until(c.LeaseUntil) + 20*time.Millisecond)
	if _, e = s.ClaimAgentRun(b.ctx, runWorker2); !errors.Is(e, ar.ErrClaimExhausted) {
		t.Fatal("child retried", e)
	}
	out, e := s.ReadOwn(b.ctx, a, child.ID)
	requireRun(t, out, e, ar.Failed)
	if out.Attempt != 1 {
		t.Fatal(out)
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE agent_enrichment_runs SET state='QUEUED',reason='QUEUED',version=version+1 WHERE id=$1`, r.ID); e == nil {
		t.Fatal("resurrected root")
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE agent_enrichment_runs SET generation=0,recovery_root_id=NULL,recovery_reason=NULL,recovery_previous_version=NULL WHERE id=$1`, child.ID); e == nil {
		t.Fatal("changed child identity")
	}
}
func TestAgentRunRecoveryNativePermanentInvalidStopsImmediately(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	c, e := s.ClaimAgentRun(b.ctx, runWorker1)
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.reconcileAgentRun(b.ctx, c, acp.ErrInvalid)
	requireRun(t, out, e, ar.Failed)
	if out.Reason != "INVALID_INPUT" || out.Attempt != 1 {
		t.Fatal("permanent retry", out)
	}
	if _, e = s.ClaimAgentRun(b.ctx, runWorker2); !errors.Is(e, ar.ErrNotFound) {
		t.Fatal("permanent dispatched", e)
	}
	if _, e = s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: out.Version, Reason: ar.RecoveryReason}); !errors.Is(e, ar.ErrConflict) {
		t.Fatal("permanent manual retry", e)
	}
}
func TestAgentRunRecoveryNativeUnknownSourceAndActorDenyZeroEffects(t *testing.T) {
	for _, mode := range []string{"pendingDispatch", "grantRevoked", "profileABA", "otherOwner", "wrongVersion", "wrongReason", "expiredSession", "sourceABA", "taskABA", "accountABA"} {
		t.Run(mode, func(t *testing.T) {
			f, s, a, r := exhaustedRecoveryFixture(t)
			b := f.f.f.place.private.base
			in := ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason}
			switch mode {
			case "pendingDispatch":
				if _, e := b.pool.Exec(b.ctx, `UPDATE agent_run_dispatches SET state='PENDING_RECONCILIATION' WHERE run_id=$1 AND fence=1`, r.ID); e != nil {
					t.Fatal(e)
				}
			case "grantRevoked":
				if _, e := b.store.RevokeOwnCandidateRetention(b.ctx, a, r.RetentionGrantID, 1); e != nil {
					t.Fatal(e)
				}
			case "profileABA":
				if _, e := b.pool.Exec(b.ctx, `UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, r.AgentID); e != nil {
					t.Fatal(e)
				}
			case "otherOwner":
				a = f.f.f.place.private.peer
			case "wrongVersion":
				in.ExpectedVersion++
			case "wrongReason":
				in.Reason = "retry_all"
			case "sourceABA":
				if _, e := b.pool.Exec(b.ctx, `UPDATE moments SET updated_at=updated_at WHERE id=$1`, r.Source.ID); e != nil {
					t.Fatal(e)
				}
			case "taskABA":
				if _, e := b.pool.Exec(b.ctx, `UPDATE agent_tasks SET updated_at=updated_at WHERE id=(SELECT ep.task_id FROM consent_grants g JOIN agent_candidate_retention_bindings rb ON rb.grant_id=g.id JOIN agent_candidate_retention_previews rp ON rp.id=rb.preview_id JOIN agent_enrichment_purpose_previews ep ON ep.id=rp.analysis_preview_id WHERE g.id=$1)`, r.RetentionGrantID); e != nil {
					t.Fatal(e)
				}
			case "accountABA":
				if _, e := b.pool.Exec(b.ctx, `UPDATE accounts SET status=status WHERE id=$1`, r.Owner.ID); e != nil {
					t.Fatal(e)
				}
			case "expiredSession":
				if _, e := b.pool.Exec(b.ctx, `WITH t AS MATERIALIZED(SELECT clock_timestamp()-interval '1 microsecond' AS at) UPDATE sessions SET expires_at=t.at,idle_expires_at=t.at FROM t WHERE account_id=$1`, a.WorkspacePrincipal.ID); e != nil {
					t.Fatal(e)
				}
			}
			before := pipelineCount(t, f)
			old := recoveryRootBytes(t, f, r.ID)
			if mode == "pendingDispatch" {
				v, e := s.ReadOwnFailure(b.ctx, a, r.ID)
				if e != nil || v.RecoveryState != "RECONCILIATION_REQUIRED" || v.Recoverable || v.Failure != ar.FailureUnknown {
					t.Fatal(v, e)
				}
			}
			_, e := s.RecoverOwn(b.ctx, a, r.ID, in)
			if e == nil {
				t.Fatal("unauthorized recovery", mode)
			}
			var n int
			if x := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_enrichment_runs WHERE recovery_root_id=$1`, r.ID).Scan(&n); x != nil || n != 0 {
				t.Fatal("child residue", n, x)
			}
			if pipelineCount(t, f) != before || recoveryRootBytes(t, f, r.ID) != old {
				t.Fatal("denial mutated original")
			}
		})
	}
}

func TestAgentRunRecoveryNativeMigrationUsedDownRefuses(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s, a, r := exhaustedRecoveryFixture(t)
	b := f.f.f.place.private.base
	child, e := s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason})
	requireRun(t, child, e, ar.Queued)
	old := recoveryRootBytes(t, f, r.ID)
	// Actual current history guard, not a synthetic CHECK-only proxy.
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "090_agent_run_recovery.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = conn.Exec(b.ctx, string(down))
	var pgerr *pgconn.PgError
	if !errors.As(e, &pgerr) || pgerr.Code != "55000" {
		conn.Release()
		t.Fatal("used downgrade expected native history protection", e)
	}
	if _, rollbackErr := conn.Exec(context.Background(), "ROLLBACK"); rollbackErr != nil {
		t.Error(rollbackErr)
	}
	conn.Release()
	if recoveryRootBytes(t, f, r.ID) != old {
		t.Fatal("history lost")
	}
	var got ar.Record
	got, e = s.ReadOwn(b.ctx, a, child.ID)
	if e != nil || got.ID != child.ID {
		t.Fatal(got, e)
	}
}

// Actual legacy pending dispatches are not made safe by an exhausted label.
func TestAgentRunRecoveryNativeExhaustionNoEffectProofMatrix(t *testing.T) {
	for _, mode := range []string{"pendingNoEffect", "leasedOriginal", "confirmedOriginal", "unavailableOriginal"} {
		t.Run(mode, func(t *testing.T) {
			f, s, a := runFixture(t, true)
			b := f.f.f.place.private.base
			_, g := f.approve(t)
			r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
			requireRun(t, r, e, ar.Queued)
			for i := int64(1); i <= ar.MaxAttempts; i++ {
				c, e := s.claimAgentRun(b.ctx, runWorker1, 100*time.Millisecond)
				if e != nil || c.Attempt != i {
					t.Fatal(c, e)
				}
				time.Sleep(time.Until(c.LeaseUntil) + 20*time.Millisecond)
			}
			switch mode {
			case "leasedOriginal", "unavailableOriginal":
				_, claim, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, a.WorkspacePrincipal, runWorker2, agentoutbox.HandlerV1)
				if e != nil {
					t.Fatal(e)
				}
				if mode == "unavailableOriginal" {
					receipt, e := b.store.ConsumeAgentOutboxControl(b.ctx, claim)
					if !errors.Is(e, agentoutbox.ErrUnavailable) || receipt.State != agentoutbox.Unavailable {
						t.Fatal(receipt, e)
					}
				}
			case "confirmedOriginal":
				receipt, e := acp.NewService(&candidatePipelineExecutor{store: b.store}, s.flags).StageOwnMomentCandidate(b.ctx, a, g.ID)
				if e != nil || receipt.CandidateID == "" {
					t.Fatal(receipt, e)
				}
			}
			effects := pipelineCount(t, f)
			if _, e = s.ClaimAgentRun(b.ctx, runWorker1); !errors.Is(e, ar.ErrClaimExhausted) {
				t.Fatal(e)
			}
			var pending int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_run_dispatches WHERE run_id=$1 AND state='PENDING_RECONCILIATION'`, r.ID).Scan(&pending); e != nil {
				t.Fatal(e)
			}
			safe := mode == "pendingNoEffect" || mode == "unavailableOriginal"
			if safe && pending != 0 || !safe && pending != 5 {
				t.Fatal("retirement must follow native proof", mode, pending)
			}
			v, e := s.ReadOwnFailure(b.ctx, a, r.ID)
			if e != nil || v.Recoverable != safe {
				t.Fatal(v, e)
			}
			if !safe {
				if v.RecoveryState != "RECONCILIATION_REQUIRED" {
					t.Fatal(v)
				}
				if _, e = s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: v.Run.Version, Reason: ar.RecoveryReason}); !errors.Is(e, ar.ErrConflict) {
					t.Fatal(e)
				}
			}
			if pipelineCount(t, f) != effects {
				t.Fatal("reconciliation changed original effects/controls")
			}
		})
	}
}

func TestAgentRunRecoveryNativeAuditWaitOriginalDeadlineRollback(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	f.selection.RetainUntil = time.Now().UTC().Truncate(time.Microsecond).Add(4 * time.Second)
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	for i := int64(1); i <= ar.MaxAttempts; i++ {
		c, e := s.claimAgentRun(b.ctx, runWorker1, 100*time.Millisecond)
		if e != nil {
			t.Fatal(e)
		}
		time.Sleep(time.Until(c.LeaseUntil) + 20*time.Millisecond)
	}
	if _, e = s.ClaimAgentRun(b.ctx, runWorker1); !errors.Is(e, ar.ErrClaimExhausted) {
		t.Fatal(e)
	}
	r, e = s.ReadOwn(b.ctx, a, r.ID)
	requireRun(t, r, e, ar.Failed)
	old := recoveryRootBytes(t, f, r.ID)
	original := pipelineCount(t, f)
	const key int64 = 900017421
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(b.ctx, `SELECT pg_advisory_xact_lock($1)`, key); e != nil {
		t.Fatal(e)
	}
	// Owned database only: actual AFTER checkpoint waits inside its audit insert.
	sql := `CREATE FUNCTION air017_test_audit_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF EXISTS(SELECT 1 FROM agent_enrichment_runs WHERE id=NEW.run_id AND generation=1) THEN PERFORM pg_advisory_xact_lock(900017421); END IF; RETURN NEW; END $$; CREATE TRIGGER air017_test_audit_wait BEFORE INSERT ON agent_run_audit FOR EACH ROW EXECUTE FUNCTION air017_test_audit_wait()`
	if _, e = b.pool.Exec(b.ctx, sql); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason})
		done <- e
	}()
	waited := false
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		var n int
		if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE 'INSERT INTO agent_enrichment_runs%'`).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			waited = true
			break
		}
		select {
		case e := <-done:
			t.Fatal("ended before audit lock", e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waited {
		t.Fatal("native audit lock absent")
	}
	t.Log("ACTUAL_CHILD_INSERT_AUDIT_WAIT_CROSSED_ORIGINAL_APPROVED_DEADLINE")
	time.Sleep(time.Until(r.Deadline) + 30*time.Millisecond)
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, ar.ErrExpired) && !errors.Is(e, ar.ErrConflict) {
			t.Fatal("late recovery", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not return")
	}
	var counts int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_enrichment_runs WHERE recovery_root_id=$1)+(SELECT count(*) FROM agent_run_steps x JOIN agent_enrichment_runs r ON r.id=x.run_id WHERE r.recovery_root_id=$1)+(SELECT count(*) FROM agent_run_audit x JOIN agent_enrichment_runs r ON r.id=x.run_id WHERE r.recovery_root_id=$1)`, r.ID).Scan(&counts); e != nil || counts != 0 {
		t.Fatal("partial child history", counts, e)
	}
	if old != recoveryRootBytes(t, f, r.ID) || original != pipelineCount(t, f) {
		t.Fatal("rollback altered FAILED/source/effect")
	}
}

func recoveryMigrationExec(t *testing.T, f *retentionFixture, name string) {
	t.Helper()
	b := f.f.f.place.private.base
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
}
func recoveryHistoryCatalog(t *testing.T, f *retentionFixture) (string, string) {
	t.Helper()
	b := f.f.f.place.private.base
	var rows, cat string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('runs',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(r)-'generation'-'recovery_root_id'-'recovery_reason'-'recovery_previous_version','xmin',r.xmin::text) ORDER BY id) FROM agent_enrichment_runs r),'steps',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(x),'xmin',x.xmin::text) ORDER BY run_id,step_name) FROM agent_run_steps x),'dispatches',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(x),'xmin',x.xmin::text) ORDER BY run_id,fence) FROM agent_run_dispatches x),'audit',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(x),'xmin',x.xmin::text) ORDER BY run_id,version) FROM agent_run_audit x))::text`).Scan(&rows); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('constraints',(SELECT jsonb_agg(jsonb_build_object('table',conrelid::regclass::text,'name',conname,'def',pg_get_constraintdef(oid)) ORDER BY conrelid::regclass::text,conname) FROM pg_constraint WHERE conrelid IN('agent_enrichment_runs'::regclass,'agent_run_steps'::regclass,'agent_run_dispatches'::regclass,'agent_run_audit'::regclass)),'functions',(SELECT jsonb_agg(pg_get_functiondef(oid) ORDER BY proname) FROM pg_proc WHERE proname IN('birdtie_agent_run_guard','birdtie_agent_run_checkpoint')),'columns',(SELECT jsonb_agg(jsonb_build_object('table',table_name,'column',column_name,'default',column_default,'nullable',is_nullable,'type',data_type) ORDER BY table_name,ordinal_position) FROM information_schema.columns WHERE table_schema='public' AND table_name IN('agent_enrichment_runs','agent_run_steps','agent_run_dispatches','agent_run_audit')),'indexes',(SELECT jsonb_agg(indexdef ORDER BY indexname) FROM pg_indexes WHERE schemaname='public' AND tablename IN('agent_enrichment_runs','agent_run_steps','agent_run_dispatches','agent_run_audit')))::text`).Scan(&cat); e != nil {
		t.Fatal(e)
	}
	return rows, cat
}
func TestAgentRunRecoveryNativeNonemptyOriginal089Roundtrip(t *testing.T) {
	ownedMigrationDatabase(t)
	f, _, _, r := exhaustedRecoveryFixture(t)
	recoveryMigrationExec(t, f, "090_agent_run_recovery.down.sql")
	before, cat := recoveryHistoryCatalog(t, f)
	if !strings.Contains(before, r.ID) || !strings.Contains(before, "FAILED") || !strings.Contains(before, "RECONCILE_EFFECT") {
		t.Fatal("nonempty native history absent")
	}
	recoveryMigrationExec(t, f, "090_agent_run_recovery.sql")
	after, _ := recoveryHistoryCatalog(t, f)
	if after != before {
		t.Fatal("up changed original rows/xmin")
	}
	recoveryMigrationExec(t, f, "090_agent_run_recovery.down.sql")
	down, cat2 := recoveryHistoryCatalog(t, f)
	if down != before || cat2 != cat {
		t.Fatal("down did not restore current089 rows/xmin/catalog")
	}
	recoveryMigrationExec(t, f, "090_agent_run_recovery.sql")
	again, _ := recoveryHistoryCatalog(t, f)
	if again != before {
		t.Fatal("reapply changed old history")
	}
	t.Log("ACTUAL_NONEMPTY_GENERATION0_FAILED_STEPS_DISPATCH_AUDIT_XMIN_AND_089_CATALOG_ROUNDTRIP")
}

func TestAgentRunRecoveryNativeIdleRefreshPreservesOriginalAuthority(t *testing.T) {
	f, s, a, r := exhaustedRecoveryFixture(t)
	b := f.f.f.place.private.base
	if _, e := b.pool.Exec(b.ctx, `UPDATE sessions SET idle_expires_at=least(expires_at,clock_timestamp()+interval '20 minutes') WHERE account_id=$1`, a.WorkspacePrincipal.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.ReadOwnFailure(b.ctx, a, r.ID)
	if e != nil || !v.Recoverable {
		t.Fatal("legitimate idle refresh", v, e)
	}
	child, e := s.RecoverOwn(b.ctx, a, r.ID, ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason})
	requireRun(t, child, e, ar.Queued)
	if child.Deadline.After(r.Deadline) || child.EventID != r.EventID {
		t.Fatal("refresh renewed original permission")
	}
}
