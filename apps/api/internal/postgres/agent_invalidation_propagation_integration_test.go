package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Deliberately do not call a Human Read before the bounded cleanup: that would
// perform lazy refresh and conceal a source-only propagation gap.
func TestInvalidationPropagationNativeSingleCleanupBeforeHumanRead(t *testing.T) {
	for _, change := range []string{"edit", "withdraw", "negative"} {
		t.Run(change, func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := pipelineNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			p, grant := f.approve(t)
			receipt, err := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, grant.ID)
			if err != nil || receipt.Candidate == nil {
				t.Fatal("original native stage", err)
			}
			independent := mustPutAgentMemory(t, f.f.f.place.private, agentMemoryID(t, f.f.f.place.private), agentMemoryInput("independent.propagation"))
			immutable := invalidationImmutable(t, b.pool, b.ctx, receipt.CandidateID, independent.ID)
			var observed time.Time
			if err = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil || !observed.Before(receipt.Candidate.ValidUntil) || !observed.Before(grant.ExpiresAt) {
				t.Fatal("must isolate source-only change before original native deadlines", err)
			}
			switch change {
			case "edit":
				_, err = b.store.UpdateMomentDraft(b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision, content.MomentInput{CityID: f.f.f.place.city, Title: f.f.moment.Title, Body: "新的本人羽毛球记录", TimePrecision: "unknown", LocationPrecision: "city"})
			case "withdraw":
				err = b.store.WithdrawMoment(b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision)
			case "negative":
				correctionNegative(t, f.f.f.place.private, p.Review.Proposal.Category)
			}
			if err != nil {
				t.Fatal("original source writer", err)
			}
			if change != "negative" {
				invalidationMarker(t, b.pool, b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision)
				var authorized, unchangedDeadline bool
				if err = b.pool.QueryRow(b.ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT birdtie_candidate_pipeline_current($1), EXISTS(SELECT 1 FROM consent_grants g JOIN agent_memory_candidates c ON c.id=$2 CROSS JOIN n WHERE g.id=$1 AND g.revoked_at IS NULL AND g.expires_at>n.at AND c.valid_until>n.at)`, grant.ID, receipt.CandidateID).Scan(&authorized, &unchangedDeadline); err != nil || authorized || !unchangedDeadline {
					t.Fatal("must prove source-current=false while original grant and candidate deadline remain unexpired", err)
				}
			}
			var cleaned int
			if err = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&cleaned); err != nil {
				t.Fatal("bounded cleanup", err)
			}
			want := 1
			if change == "negative" {
				want = 0
			} // Original correction already expires pending support in its own transaction.
			if cleaned != want {
				t.Fatalf("bounded cleanup count=%d want=%d", cleaned, want)
			}
			invalidationCleared(t, b.pool, b.ctx, receipt.CandidateID, cleaned)
			if invalidationImmutable(t, b.pool, b.ctx, receipt.CandidateID, independent.ID) != immutable {
				t.Fatal("cleanup rewrote original effect/checkpoints or independent EXPLICIT Memory")
			}
			if err = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&cleaned); err != nil || cleaned != 0 {
				t.Fatal("repeat cleanup must not create another transition", cleaned, err)
			}
		})
	}
}

func invalidationMarker(t *testing.T, pool *pgxpool.Pool, ctx context.Context, owner, source string, revision int64) {
	t.Helper()
	var present bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_source_invalidations WHERE owner_id=$1 AND source_type='MOMENT' AND source_id=$2 AND source_epoch=$3::bigint::text)`, owner, source, revision).Scan(&present); err != nil || !present {
		t.Fatal("original source transaction did not append its OLD revision marker", err)
	}
}

func invalidationImmutable(t *testing.T, pool *pgxpool.Pool, ctx context.Context, candidate, memory string) string {
	t.Helper()
	var raw string
	err := pool.QueryRow(ctx, `SELECT jsonb_build_object('effect',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(e),'xmin',e.xmin::text)) FROM agent_effect_ledger e WHERE candidate_id=$1),'inbox',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(i),'xmin',i.xmin::text)) FROM agent_consumer_inbox i JOIN agent_effect_ledger e ON e.event_id=i.event_id AND e.subject_id=i.subject_id AND e.handler_version=i.handler_version WHERE e.candidate_id=$1),'outbox',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(d),'xmin',d.xmin::text)) FROM agent_domain_outbox d JOIN agent_effect_ledger e ON e.event_id=d.event_id WHERE e.candidate_id=$1),'explicit',(SELECT jsonb_build_object('row',to_jsonb(m),'xmin',m.xmin::text) FROM agent_memories m WHERE id=$2))::text`, candidate, memory).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw // Only compared in memory; never log source bodies or authority.
}

func invalidationCleared(t *testing.T, pool *pgxpool.Pool, ctx context.Context, id string, count int) {
	t.Helper()
	var status string
	var empty bool
	if err := pool.QueryRow(ctx, `SELECT status, predicate IS NULL AND category IS NULL AND assessment IS NULL AND sources='[]'::jsonb FROM agent_memory_candidates WHERE id=$1`, id).Scan(&status, &empty); err != nil {
		t.Fatal(err)
	}
	if status != string(agentmemorycandidate.Expired) || !empty {
		t.Fatalf("before any HumanRead source-only invalidation must clear pending support: cleanup=%d status=%s payloadCleared=%t", count, status, empty)
	}
}

func TestInvalidationPropagationNativeMultiCleanupBeforeHumanRead(t *testing.T) {
	for _, change := range []string{"extra_source", "negative"} {
		t.Run(change, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			p, grant := f.approve(t)
			receipt, err := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, grant.ID)
			if err != nil || receipt.Candidate == nil {
				t.Fatal(err)
			}
			if change == "negative" {
				correctionNegative(t, f.f.f.place.private, p.Review.Proposal.Category)
			} else {
				source := p.Review.Sources[1]
				if err = b.store.WithdrawMoment(b.ctx, b.person.ID, source.Selector.ID, source.Version.Revision); err != nil {
					t.Fatal(err)
				}
				invalidationMarker(t, b.pool, b.ctx, b.person.ID, source.Selector.ID, source.Version.Revision)
			}
			var cleaned int
			if err = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&cleaned); err != nil {
				t.Fatal(err)
			}
			want := 1
			if change == "negative" {
				want = 0
			}
			if cleaned != want {
				t.Fatalf("bounded cleanup count=%d want=%d", cleaned, want)
			}
			invalidationCleared(t, b.pool, b.ctx, receipt.CandidateID, cleaned)
		})
	}
}

func TestInvalidationPropagationNativeQueuedSourceCannotCommitCandidate(t *testing.T) {
	for _, change := range []string{"withdraw", "negative", "cancel"} {
		t.Run(change, func(t *testing.T) {
			ownedMigrationDatabase(t)
			f, runs, actor := runFixture(t, true)
			b := f.f.f.place.private.base
			p, grant := f.approve(t)
			run, err := runs.ScheduleOwn(b.ctx, actor, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: grant.ID})
			requireRun(t, run, err, ar.Queued)
			if change == "withdraw" {
				if err = b.store.WithdrawMoment(b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision); err != nil {
					t.Fatal(err)
				}
			} else if change == "negative" {
				correctionNegative(t, f.f.f.place.private, p.Review.Proposal.Category)
			}
			before := pipelineCount(t, f)
			if count, err := runs.ExpireAgentRuns(b.ctx, 100); err != nil || count != 0 {
				t.Fatal("future deadline is not source-driven expiry", count, err)
			}
			claim, err := runs.ClaimAgentRun(b.ctx, runWorker1)
			if err != nil {
				t.Fatal("actual claim", err)
			}
			if change == "cancel" {
				current, err := runs.ReadOwn(b.ctx, actor, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				cancelled, err := runs.CancelOwn(b.ctx, actor, run.ID, current.Version)
				requireRun(t, cancelled, err, ar.Cancelled)
				late, err := runs.ExecuteAgentRun(b.ctx, claim)
				requireRun(t, late, err, ar.Cancelled)
				if late.ID != cancelled.ID || late.Version != cancelled.Version {
					t.Fatal("late cancelled claim changed original terminal receipt")
				}
			} else {
				out, err := runs.ExecuteAgentRun(b.ctx, claim)
				requireRun(t, out, err, ar.Failed)
				if out.Reason != "AUTHORITY_CHANGED" {
					t.Fatal("wrong current-source failure class", out.Reason)
				}
				var state string
				if err = b.pool.QueryRow(b.ctx, `SELECT state FROM agent_run_dispatches WHERE run_id=$1 AND fence=$2`, run.ID, claim.Fence).Scan(&state); err != nil || state != "NO_EFFECT" {
					t.Fatal("original native outbox/fence no-effect proof missing", state, err)
				}
			}
			if pipelineCount(t, f) != before {
				t.Fatal("invalidated/late claim wrote a candidate/effect/Memory")
			}
		})
	}
}

func TestInvalidationPropagationNativeDispatchCommitOrderingAndUnknownAccounting(t *testing.T) {
	for _, order := range []string{"revoke_before_begin", "commit_before_revoke", "unknown_after_commit"} {
		t.Run(order, func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := newEgressFixture(t, 3)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			if order == "revoke_before_begin" {
				r := f.reserve(t, p)
				cfg := b.pool.Config().Copy()
				cfg.MaxConns = 1
				pool, err := pgxpool.NewWithConfig(b.ctx, cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer pool.Close()
				ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
				defer cancel()
				held, err := pool.Acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer held.Release()
				before := pool.Stat().EmptyAcquireCount()
				done := make(chan error, 1)
				started := make(chan struct{})
				go func() {
					close(started)
					request, e := New(pool, false).BeginOwnLocalModelAttempt(ctx, f.f.native.access, r.OperationID, f.gate)
					if len(request.Messages) != 0 {
						done <- errors.New("private request escaped revoked dispatch")
						return
					}
					done <- e
				}()
				<-started
				if pool.Stat().AcquiredConns() != 1 {
					t.Fatal("exclusive Begin pool not held")
				}
				select {
				case err := <-done:
					t.Fatal("Begin did not wait for held native pool", err)
				case <-time.After(30 * time.Millisecond):
				}
				if err = b.store.RevokeOwnModelEgress(ctx, f.f.native.access, p.ID); err != nil {
					t.Fatal(err)
				}
				held.Release()
				if err = <-done; !errors.Is(err, modelegressbudget.ErrDenied) {
					t.Fatal("revoked before commit", err)
				}
				if pool.Stat().EmptyAcquireCount() <= before {
					t.Fatal("actual completed Begin acquire wait not counted")
				}
				read, err := b.store.ReadOwnLocalModelAttempt(ctx, f.f.native.access, r.OperationID)
				if err != nil || read.State != "RESERVED" {
					t.Fatal("invalid Begin changed original accounting", err)
				}
				return
			}
			in := nativeAttemptInput(t, f, p)
			adapter := &nativeLocalAttemptAdapter{unknown: order == "unknown_after_commit", after: func() {
				if err := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p.ID); err != nil {
					t.Error(err)
				}
			}}
			driver := nativeAttemptDriver(t, b.store, f.gate, adapter)
			out, err := driver.Once(b.ctx, f.f.native.access, in)
			if len(out.EncodedResult) != 0 || adapter.calls.Load() != 1 {
				t.Fatal("late result escaped or dispatch duplicated", err)
			}
			control, err := driver.Recover(b.ctx, f.f.native.access, in.OperationID)
			want := "SETTLED"
			if order == "unknown_after_commit" {
				want = "UNKNOWN"
			}
			if err != nil || control.State != want {
				t.Fatal("original accounting lost after revoke", control.State, err)
			}
			if _, err = b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, in.OperationID); !errors.Is(err, modelegressbudget.ErrConflict) {
				t.Fatal("committed request falsely refunded", err)
			}
			if next, _ := driver.Once(b.ctx, f.f.native.access, in); len(next.EncodedResult) != 0 || adapter.calls.Load() != 1 {
				t.Fatal("original ID recovery retried transport")
			}
			views, err := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
			if err != nil || len(views) != 4 {
				t.Fatal(err)
			}
			for _, v := range views {
				if v.Allocated.Requests != 1 || (want == "UNKNOWN" && v.Allocated.CostMicros == 0) {
					t.Fatal("unknown accounting lost held bound")
				}
			}
		})
	}
}

func TestInvalidationPropagationNativeCleanupBoundsAndAfterRealWait(t *testing.T) {
	ownedMigrationDatabase(t)
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	before := correctionPairs(t, b.pool, b.ctx)
	for _, bound := range []any{nil, 0, 101} {
		var n int
		err := b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline($1::integer)`, bound).Scan(&n)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "P0001" || pg.Message != "bounded candidate cleanup required" {
			t.Fatal("invalid maintenance bound did not reject exactly", bound, err)
		}
		if correctionPairs(t, b.pool, b.ctx) != before {
			t.Fatal("invalid maintenance bound changed any public row/xmin")
		}
	}
	var clock time.Time
	if err := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	f.selection.RetainUntil = clock.Add(8 * time.Second).Truncate(time.Microsecond)
	_, grant := f.approve(t)
	r, err := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, grant.ID)
	if err != nil || r.Candidate == nil {
		t.Fatal("actual finite original stage", err)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
	held, err := b.pool.Begin(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	consumed := false
	defer func() {
		cancel()
		_ = held.Rollback(context.Background())
		if !consumed {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("cleanup goroutine did not drain after rollback/cancel")
			}
		}
	}()
	var holder int
	if err = held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if _, err = held.Exec(ctx, `LOCK TABLE agent_memory_candidates IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	go func() {
		var count int
		err := b.pool.QueryRow(ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&count)
		done <- result{count, err}
	}()
	until := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if err = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE 'SELECT birdtie_expire_candidate_pipeline(100)%')`, holder).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			t.Log("exact original cleanup relation waiter reached before original deadline")
			break
		}
		select {
		case got := <-done:
			consumed = true
			t.Fatal("cleanup returned before lock barrier", got.err)
		default:
		}
		if time.Now().After(until) {
			t.Fatal("specific maintenance waiter not observed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&clock); err != nil || !clock.Before(r.Candidate.ValidUntil) {
		t.Fatal("barrier must be reached before original candidate deadline", err)
	}
	for clock.Before(r.Candidate.ValidUntil.Add(25 * time.Millisecond)) {
		time.Sleep(20 * time.Millisecond)
		if err = b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&clock); err != nil {
			t.Fatal(err)
		}
	}
	if err = held.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var got result
	select {
	case got = <-done:
		consumed = true
	case <-ctx.Done():
		t.Fatal("cleanup did not complete after release")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	invalidationCleared(t, b.pool, b.ctx, r.CandidateID, got.count)
	if got.count != 1 {
		t.Fatal("expired original candidate did not clear exactly once", got.count)
	}
	for i := 0; i < 3; i++ {
		var count int
		if err = b.pool.QueryRow(ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&count); err != nil || count != 0 {
			t.Fatal("repeated maintenance renewed/duplicated transition", count, err)
		}
	}
}

func TestInvalidationPropagationNativeCleanupSkipLockedConcurrentAndManualPreserved(t *testing.T) {
	ownedMigrationDatabase(t)
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	_, grant := f.approve(t)
	r, err := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, grant.ID)
	if err != nil || r.Candidate == nil {
		t.Fatal("original native stage", err)
	}
	manualFixture, manualService := memoryCandidateFixture(t)
	manual := memoryCandidateSave(t, manualFixture, manualService)
	manualRow := func() string {
		t.Helper()
		var raw string
		if err := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('row',to_jsonb(c),'xmin',c.xmin::text)::text FROM agent_memory_candidates c WHERE id=$1`, manual.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw // Compare only; do not print private proposal/support.
	}
	beforeManual := manualRow()
	if err = b.store.WithdrawMoment(b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()
	held, err := b.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(context.Background())
	var id string
	if err = held.QueryRow(ctx, `SELECT id::text FROM agent_memory_candidates WHERE id=$1 FOR UPDATE`, r.CandidateID).Scan(&id); err != nil || id != r.CandidateID {
		t.Fatal("original candidate row lock", err)
	}
	var skipped int
	if err = b.pool.QueryRow(ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&skipped); err != nil || skipped != 0 {
		t.Fatal("locked original candidate must be skipped without blocking/clearing manual candidate", skipped, err)
	}
	if manualRow() != beforeManual {
		t.Fatal("maintenance touched nonpipeline candidate")
	}
	if err = held.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			var n int
			err := b.pool.QueryRow(ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&n)
			done <- result{n, err}
		}()
	}
	total := 0
	for i := 0; i < 2; i++ {
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatal("concurrent bounded cleanup", got.err)
			}
			total += got.count
		case <-ctx.Done():
			t.Fatal("concurrent bounded cleanup did not complete", ctx.Err())
		}
	}
	if total != 1 {
		t.Fatal("concurrent cleanup duplicated or lost the original transition", total)
	}
	invalidationCleared(t, b.pool, b.ctx, r.CandidateID, total)
	if manualRow() != beforeManual {
		t.Fatal("concurrent maintenance rewrote nonpipeline row/xmin")
	}
	var repeated int
	if err = b.pool.QueryRow(ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&repeated); err != nil || repeated != 0 {
		t.Fatal("repeat after concurrent cleanup was not a no-op", repeated, err)
	}
}

func TestInvalidationPropagationNativeNonemptyCurrentDataAndUsedDownNeverRevives(t *testing.T) {
	ownedMigrationDatabase(t)
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	migration := func(name string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = b.pool.Exec(b.ctx, string(raw)); err != nil {
			t.Fatal("actual maintenance migration", name, err)
		}
	}
	migration("095_agent_candidate_invalidation_cleanup.down.sql")
	_, grant := f.approve(t)
	r, err := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, grant.ID)
	if err != nil || r.Candidate == nil {
		t.Fatal("actual pre095 candidate/effect", err)
	}
	independent := mustPutAgentMemory(t, f.f.f.place.private, agentMemoryID(t, f.f.f.place.private), agentMemoryInput("independent.current-data"))
	immutable := invalidationImmutable(t, b.pool, b.ctx, r.CandidateID, independent.ID)
	if err = b.store.WithdrawMoment(b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&n); err != nil || n != 0 {
		t.Fatal("094 maintenance control must retain this actual source-only pending candidate", n, err)
	}
	beforeUp := correctionPairs(t, b.pool, b.ctx)
	migration("095_agent_candidate_invalidation_cleanup.sql")
	if correctionPairs(t, b.pool, b.ctx) != beforeUp {
		t.Fatal("095 up rewrote nonempty old rows/xmin")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&n); err != nil || n != 1 {
		t.Fatal("095 must clear the same original pending support", n, err)
	}
	invalidationCleared(t, b.pool, b.ctx, r.CandidateID, n)
	if invalidationImmutable(t, b.pool, b.ctx, r.CandidateID, independent.ID) != immutable {
		t.Fatal("current-data maintenance rewrote original effect/checkpoints or EXPLICIT Memory")
	}
	beforeUsedDown := correctionPairs(t, b.pool, b.ctx)
	migration("095_agent_candidate_invalidation_cleanup.down.sql")
	if correctionPairs(t, b.pool, b.ctx) != beforeUsedDown {
		t.Fatal("used095 down rewrote rows/xmin or revived scrubbed support")
	}
	invalidationCleared(t, b.pool, b.ctx, r.CandidateID, 0)
	migration("095_agent_candidate_invalidation_cleanup.sql")
	if correctionPairs(t, b.pool, b.ctx) != beforeUsedDown {
		t.Fatal("used095 reapply rewrote rows/xmin")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&n); err != nil || n != 0 {
		t.Fatal("down/reapply created another transition", n, err)
	}
}
