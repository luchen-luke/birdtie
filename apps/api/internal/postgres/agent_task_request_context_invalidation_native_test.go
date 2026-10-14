package postgres

import (
	"context"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	maint "github.com/birdtie/birdtie/apps/api/internal/agentoutboxmaintenance"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Every top-level chain owns a fresh migrated child. Existing actual human
// source/076 approval/claim/receipt producers are used, never forged queue rows.
type taskRequestNativeFixture struct{ purpose *contextPurposeFixture }

func taskRequestNative(t *testing.T) *taskRequestNativeFixture {
	t.Helper()
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	b.ctx = ctx
	f.selection.ProfileFields = nil
	f.selection.PlaceIDs = nil
	f.selection.ActivityIDs = nil
	f.selection.RelationshipTieIDs = nil
	f.selection.PolicyFamilies = nil
	return &taskRequestNativeFixture{f}
}
func (f *taskRequestNativeFixture) id(t *testing.T) string {
	return (&memoryStormFixture{purpose: f.purpose}).id(t)
}
func (f *taskRequestNativeFixture) write(t *testing.T, query string) agentworkspace.Task {
	t.Helper()
	b := f.purpose.f.place.private.base
	task := f.purpose.f.task
	task.Status = agentworkspace.TaskActive
	task.Intent = agentworkspace.FindActivity
	filters := map[string]string{}
	for k, v := range task.Filters {
		filters[k] = v
	}
	filters["currentQuery"] = query
	task.Filters = filters
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: query})
	updated, e := b.store.UpdateTask(b.ctx, task)
	if e != nil {
		t.Fatal("original native Task writer", e)
	}
	f.purpose.f.task = updated
	f.purpose.selection.TaskUpdatedAt = updated.UpdatedAt
	f.purpose.selection.CurrentQuery = query
	f.purpose.selection.QueryDigest = "" // Original Normalize fills exact actual query digest.
	return updated
}
func (f *taskRequestNativeFixture) claim(t *testing.T) (agentoutbox.Record, agentoutbox.Claim) {
	t.Helper()
	b := f.purpose.f.place.private.base
	for i := 0; i < 32; i++ {
		var candidate string
		if e := b.pool.QueryRow(b.ctx, `SELECT event_id::text FROM agent_domain_outbox WHERE subject_id=$1 AND schema_version=$2 AND delivery_state IN('PENDING','LEASED') AND (lease_until IS NULL OR lease_until<=clock_timestamp()) ORDER BY occurred_at,event_id LIMIT 1`, b.person.ID, agentoutbox.TaskRequestSchema).Scan(&candidate); e != nil {
			t.Fatal("actual Task control absent", e)
		}
		r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.TaskRequestHandler)
		if e == nil {
			return r, c
		}
		var state string
		var attempt int64
		if e == agentoutbox.ErrUnavailable && r == (agentoutbox.Record{}) && c == (agentoutbox.Claim{}) && b.pool.QueryRow(b.ctx, `SELECT delivery_state,attempt FROM agent_domain_outbox WHERE event_id=$1`, candidate).Scan(&state, &attempt) == nil && state == string(agentoutbox.Invalidated) && attempt == 0 {
			continue
		}
		t.Fatal("Task fixed claim failed without acknowledged native retirement", e)
	}
	t.Fatal("bounded Task retirement did not reach real current work")
	return agentoutbox.Record{}, agentoutbox.Claim{}
}
func (f *taskRequestNativeFixture) consume(t *testing.T, c agentoutbox.Claim) agentoutbox.ConsumerRecord {
	t.Helper()
	b := f.purpose.f.place.private.base
	p, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil {
		t.Fatal("actual Task cleanup/receipt", e)
	}
	return p
}
func (f *taskRequestNativeFixture) run(t *testing.T) maint.Report {
	t.Helper()
	b := f.purpose.f.place.private.base
	// A later explicit invocation may retire stale controls. A single fixed run
	// stops on its original unavailable retirement; mixed safely records it.
	r := maint.Run(b.ctx, b.store, maint.Options{Subject: b.person, WorkerID: f.id(t), Handler: agentoutbox.HandlerV1, Batch: 64, Timeout: 10 * time.Second, MixedHandlers: true})
	if r.Status != "FINISHED" || r.BusinessExecution != "UNAVAILABLE" {
		t.Fatal("real mixed native maintenance", r)
	}
	return r
}
func (f *taskRequestNativeFixture) taskHistory(t *testing.T) string {
	t.Helper()
	b := f.purpose.f.place.private.base
	var raw string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('events',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('_xmin',d.xmin::text) ORDER BY event_id) FROM agent_domain_outbox d WHERE subject_id=$1 AND schema_version=$2),'receipts',(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('_xmin',i.xmin::text) ORDER BY event_id) FROM agent_consumer_inbox i WHERE subject_id=$1 AND handler_version=$3))::text`, b.person.ID, agentoutbox.TaskRequestSchema, agentoutbox.TaskRequestHandler).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw // Private comparison only, never output native rows or content.
}
func (f *taskRequestNativeFixture) assertGrant(t *testing.T, id string, version int64, revoked bool) {
	t.Helper()
	b := f.purpose.f.place.private.base
	var got int64
	var at *time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, id).Scan(&got, &at); e != nil || got != version || (at != nil) != revoked {
		t.Fatal("original exact grant lifecycle", e, got)
	}
}

func TestTaskRequestContextNativeSameTxCaptureAndOriginalReply(t *testing.T) {
	ownedMigrationDatabase(t)
	f := taskRequestNative(t)
	b := f.purpose.f.place.private.base
	var originalCount int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_domain_outbox WHERE subject_id=$1 AND source_id=$2 AND schema_version=$3`, b.person.ID, f.purpose.f.task.ID, agentoutbox.TaskRequestSchema).Scan(&originalCount); e != nil || originalCount != 1 {
		t.Fatal("real SaveTask INSERT was not captured once", e, originalCount)
	}
	if _, e := b.store.UpdateTask(b.ctx, f.purpose.f.task); e != nil {
		t.Fatal(e)
	}
	var sameCount int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_domain_outbox WHERE subject_id=$1 AND source_id=$2 AND schema_version=$3`, b.person.ID, f.purpose.f.task.ID, agentoutbox.TaskRequestSchema).Scan(&sameCount); e != nil || sameCount != originalCount {
		t.Fatal("exact original Store retry made another event", e)
	}
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	var id string
	e = tx.QueryRow(b.ctx, `INSERT INTO agent_tasks(principal_type,owner_account_id,acting_user_account_id,city_id,city_context_id,query,intent,status,filters,conversation,context_type,context_id) SELECT principal_type,owner_account_id,acting_user_account_id,city_id,city_context_id,query,intent,status,filters,conversation,context_type,context_id FROM agent_tasks WHERE id=$1 RETURNING id::text`, f.purpose.f.task.ID).Scan(&id)
	if e != nil {
		_ = tx.Rollback(b.ctx)
		t.Fatal(e)
	}
	var captured bool
	if e = tx.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_domain_outbox WHERE source_id=$1 AND schema_version=$2)`, id, agentoutbox.TaskRequestSchema).Scan(&captured); e != nil || !captured {
		t.Fatal("source/outbox did not share original Tx", e)
	}
	if e = tx.Rollback(b.ctx); e != nil {
		t.Fatal(e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_tasks WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM agent_domain_outbox WHERE source_id=$1)`, id).Scan(&captured); e != nil || !captured {
		t.Fatal("source rollback retained event", e)
	}
	before, bound := f.purpose.approve(t)
	// The original helper appends a space before its term. An empty term is
	// not a valid 076 CurrentQuery, so keep a real nonempty native query.
	started := replyNativeStart(t, f.purpose.f, "activity", "羽毛球")
	f.purpose.selection.TaskUpdatedAt = started.UpdatedAt
	f.purpose.selection.CurrentQuery = started.Filters["currentQuery"]
	f.purpose.selection.QueryDigest = ""
	active, activeGrant := f.purpose.approve(t)
	completed, e := b.store.CaptureOwnHumanReply(b.ctx, replyNativeAccess(t, f.purpose.f, started))
	if e != nil || completed.Status != agentworkspace.TaskCompleted {
		t.Fatal("real native human reply completion", e)
	}
	if agentworkspace.ValidateReplyMembership(completed, len(completed.Conversation)-1) != nil {
		t.Fatal("original latest reply membership absent")
	}
	var hasCompleted bool
	if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_domain_outbox WHERE source_id=$1 AND schema_version=$2 AND source_status='COMPLETED' AND occurred_at=$3 AND source_revision=0)`, completed.ID, agentoutbox.TaskRequestSchema, completed.UpdatedAt).Scan(&hasCompleted); e != nil || !hasCompleted {
		t.Fatal("CaptureOwnHumanReply did not use original sameTx Task hook", e)
	}
	f.run(t)
	f.assertGrant(t, bound.ID, bound.Revision+1, true)
	f.assertGrant(t, activeGrant.ID, activeGrant.Revision+1, true)
	var kept bool
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*)=2 FROM agent_context_purpose_previews WHERE id=ANY($1::uuid[])`, []string{before.ID, active.ID}).Scan(&kept); e != nil || !kept {
		t.Fatal("bound approval history deleted", e)
	}
}

func TestTaskRequestContextNativeActual076CleanupAndPreservedAuthority(t *testing.T) {
	ownedMigrationDatabase(t)
	f := taskRequestNative(t)
	b := f.purpose.f.place.private.base
	f.run(t)
	old, grant := f.purpose.approve(t)
	unbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, f.purpose.selection)
	if e != nil {
		t.Fatal(e)
	}
	// Genuine independently approved existing Run+candidate effect, in this
	// same owned child, must not be touched by a different Task cleanup family.
	independent, runs, access := runFixture(t, true)
	ib := independent.f.f.place.private.base
	_, retained := independent.approve(t)
	run, e := runs.ScheduleOwn(ib.ctx, access, ar.Input{MomentID: independent.f.moment.ID, RetentionGrantID: retained.ID})
	requireRun(t, run, e, ar.Queued)
	claim, e := runs.ClaimAgentRun(ib.ctx, runWorker1)
	if e != nil {
		t.Fatal(e)
	}
	result, e := runs.ExecuteAgentRun(ib.ctx, claim)
	requireRun(t, result, e, ar.Succeeded)
	protected := func() string {
		var raw string
		if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('memories',(SELECT jsonb_agg(to_jsonb(m)||jsonb_build_object('_xmin',m.xmin::text) ORDER BY id) FROM agent_memories m),'runs',(SELECT jsonb_agg(to_jsonb(r)||jsonb_build_object('_xmin',r.xmin::text) ORDER BY id) FROM agent_enrichment_runs r),'effects',(SELECT jsonb_agg(to_jsonb(e)||jsonb_build_object('_xmin',e.xmin::text) ORDER BY effect_key) FROM agent_effect_ledger e),'candidates',(SELECT jsonb_agg(to_jsonb(c)||jsonb_build_object('_xmin',c.xmin::text) ORDER BY id) FROM agent_memory_candidates c))::text`).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	beforeProtected := protected()
	f.write(t, "真实用户修改后的原任务查询")
	current, currentGrant := f.purpose.approve(t)
	currentUnbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, f.purpose.selection)
	if e != nil {
		t.Fatal(e)
	}
	r, root := f.claim(t)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	source, valid, e := resolveTaskRequestOutboxTx(b.ctx, tx, r.Event)
	if e != nil || !valid || source.purposeToken == source.fingerprint {
		t.Fatal("original 076 hash namespace conflated", e)
	}
	var sourceToken string
	if e = tx.QueryRow(b.ctx, `SELECT s->'version'->>'token' FROM agent_context_purpose_previews p CROSS JOIN LATERAL jsonb_array_elements(p.sources) s WHERE p.id=$1 AND s->>'kind'='CURRENT_TASK_REQUEST'`, current.ID).Scan(&sourceToken); e != nil || sourceToken != source.purposeToken {
		t.Fatal("actual current native076 token differs", e)
	}
	_ = tx.Rollback(b.ctx)
	if p := f.consume(t, root); p.State != agentoutbox.MemoryComplete {
		t.Fatal("real root cleanup incomplete")
	}
	_, child := f.claim(t)
	if p := f.consume(t, child); p.State != agentoutbox.MemoryComplete {
		t.Fatal("real child cleanup incomplete")
	}
	var removed, history, latest bool
	if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1),EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$2),count(*)=2 FROM agent_context_purpose_previews WHERE id=ANY($3::uuid[])`, unbound.ID, old.ID, []string{current.ID, currentUnbound.ID}).Scan(&removed, &history, &latest); e != nil || !removed || !history || !latest {
		t.Fatal("real stale preview/current/bound history invariant", e)
	}
	f.assertGrant(t, grant.ID, grant.Revision+1, true)
	f.assertGrant(t, currentGrant.ID, currentGrant.Revision, false)
	if protected() != beforeProtected {
		t.Fatal("Task narrowing rewrote separately approved Memory/Run/effect/candidate")
	}
	before := f.taskHistory(t)
	again, e := b.store.ConsumeAgentOutboxControl(b.ctx, child)
	if e != nil || again.State != agentoutbox.MemoryComplete || before != f.taskHistory(t) {
		t.Fatal("original terminal receipt re-executed cleanup", e)
	}
}

func TestTaskRequestContextNativeEpochABAAndWaitFence(t *testing.T) {
	t.Run("same-time-source-ABA", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := taskRequestNative(t)
		b := f.purpose.f.place.private.base
		f.run(t)
		_, old := f.purpose.approve(t)
		var firstEpoch string
		if e := b.pool.QueryRow(b.ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, f.purpose.f.task.ID).Scan(&firstEpoch); e != nil {
			t.Fatal(e)
		}
		b.exec(`UPDATE agent_tasks SET updated_at=updated_at WHERE id=$1`, f.purpose.f.task.ID)
		var secondEpoch string
		if e := b.pool.QueryRow(b.ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, f.purpose.f.task.ID).Scan(&secondEpoch); e != nil || firstEpoch == secondEpoch {
			t.Fatal("real same-value UPDATE did not advance epoch", e)
		}
		f.run(t)
		f.assertGrant(t, old.ID, old.Revision+1, true)
		f.write(t, "原查询A")
		_, prior := f.purpose.approve(t)
		f.write(t, "原查询B")
		f.write(t, "原查询A")
		_, current := f.purpose.approve(t)
		f.run(t)
		f.assertGrant(t, prior.ID, prior.Revision+1, true)
		f.assertGrant(t, current.ID, current.Revision, false)
	})
	t.Run("natural-lease-expiry-rolls-back-grant-cleanup", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := taskRequestNative(t)
		b := f.purpose.f.place.private.base
		f.run(t)
		_, grant := f.purpose.approve(t)
		f.write(t, "等待期间重新查活动")
		_, root := f.claim(t)
		f.consume(t, root)
		_, child := f.claim(t)
		held, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer held.Rollback(context.Background())
		var blocker int
		var id string
		if e = held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&blocker); e != nil {
			t.Fatal(e)
		}
		if e = held.QueryRow(b.ctx, `SELECT id::text FROM consent_grants WHERE id=$1 FOR UPDATE`, grant.ID).Scan(&id); e != nil {
			t.Fatal(e)
		}
		consumer, pid := memoryStormPool(t, b.ctx, "task-lease-consumer-"+b.person.ID)
		type result struct {
			receipt agentoutbox.ConsumerRecord
			err     error
		}
		done := make(chan result, 1)
		go func() { p, e := New(consumer, false).ConsumeAgentOutboxControl(b.ctx, child); done <- result{p, e} }()
		memoryStormWait(t, b.pool, b.ctx, pid, blocker, "original-Task-grant-cleanup")
		memoryStormAwaitDeadline(t, b.pool, b.ctx, child.LeaseUntil)
		if e = held.Rollback(b.ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case out := <-done:
			if out.err == nil || out.receipt != (agentoutbox.ConsumerRecord{}) {
				t.Fatal("late lease committed cleanup")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("lease wait unresolved")
		}
		f.assertGrant(t, grant.ID, grant.Revision, false)
		_, fresh := f.claim(t)
		if fresh.EventID != child.EventID || fresh.Fence <= child.Fence {
			t.Fatal("explicit new original claim did not advance fence")
		}
		f.consume(t, fresh)
		f.assertGrant(t, grant.ID, grant.Revision+1, true)
		if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, child); !errors.Is(e, agentoutbox.ErrConflict) {
			t.Fatal("stale old worker fence survived", e)
		}
	})
}

func TestTaskRequestContextNativeSharedRootBudgetAndOriginalCausation(t *testing.T) {
	ownedMigrationDatabase(t)
	f := taskRequestNative(t)
	b := f.purpose.f.place.private.base
	f.run(t)
	for i := 0; i < 501; i++ {
		f.purpose.approve(t)
	}
	f.write(t, "根预算真实清理501项原上下文批准")
	r, root := f.claim(t)
	f.consume(t, root)
	var last agentoutbox.ConsumerRecord
	for i := 0; i < 5; i++ {
		_, child := f.claim(t)
		last = f.consume(t, child)
	}
	if last.State != agentoutbox.DeadLetter || last.Reason != agentoutbox.ReasonRootExhausted {
		t.Fatal("root6 falsely completed real remainder", last.State, last.Reason)
	}
	var attempts, revoked, remaining, children int
	if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT sum(attempt)::int FROM agent_domain_outbox WHERE root_trace_id=$1 AND subject_id=$2),(SELECT count(*) FROM consent_grants WHERE owner_account_id=$2 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NOT NULL),(SELECT count(*) FROM consent_grants WHERE owner_account_id=$2 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NULL),(SELECT count(*) FROM agent_domain_outbox WHERE root_trace_id=$1 AND causation_id IS NOT NULL)`, r.Event.RootTraceID, b.person.ID).Scan(&attempts, &revoked, &remaining, &children); e != nil || attempts != 6 || revoked != 500 || remaining != 1 || children != 1 {
		t.Fatal("actual shared root budget/depth/CAS", e, attempts, revoked, remaining, children)
	}
	before := f.taskHistory(t)
	if _, _, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.TaskRequestHandler); e != agentoutbox.ErrNotFound || f.taskHistory(t) != before {
		t.Fatal("terminal budget chain resurrected", e)
	}
}

func TestTaskRequestContextNativeMixedFiveFamily(t *testing.T) {
	ownedMigrationDatabase(t)
	f := taskRequestNative(t)
	b := f.purpose.f.place.private.base
	f.run(t)
	old, grant := f.purpose.approve(t)
	unbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, f.purpose.selection)
	if e != nil {
		t.Fatal(e)
	}
	memory := (&memoryStormFixture{purpose: f.purpose}).write(t, "五族真实明确Memory修改")
	(&preferenceNativeFixture{purpose: f.purpose}).write(t, "五族真实本人偏好修改")
	access := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.purpose.f.place.private.owner.SessionDigest}
	var operations []struct{ operationID, effectID string }
	// Register after the original fixture cleanups so LIFO removes only this
	// chain's new 117 immutable receipts and original invitation first. This
	// teardown cannot run against a parent/non-owned database, and temporarily
	// disabling the one immutable-receipt guard is confined to this cleanup Tx;
	// success re-enables it and every error rolls the entire change back.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tx, err := b.pool.Begin(ctx)
		if err != nil {
			t.Error("owned five-family cleanup transaction", err)
			return
		}
		defer tx.Rollback(context.Background())
		var owned bool
		if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || tx.QueryRow(ctx, `SELECT current_database()=$1 AND strpos(current_database(),'birdtie_owned_migration_')=1`, b.pool.Config().ConnConfig.Database).Scan(&owned) != nil || !owned {
			t.Error("five-family cleanup requires its actual owned migration child")
			return
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE human_participation_operation_receipts DISABLE TRIGGER human_participation_operation_guard`); err != nil {
			t.Error("owned five-family exact receipt teardown", err)
			return
		}
		for _, original := range operations {
			if _, err = tx.Exec(ctx, `DELETE FROM human_participation_operation_receipts r WHERE r.owner_account_id=$1 AND r.operation_id=$2 AND r.activity_id=$3 AND r.effect_id=$4 AND EXISTS(SELECT 1 FROM activity_participations p WHERE p.id=r.effect_id AND p.participant_account_id=$1 AND p.activity_id=$3)`, b.person.ID, original.operationID, f.purpose.f.public, original.effectID); err != nil {
				t.Error("owned five-family original operation/effect tuple cleanup", err)
				return
			}
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE human_participation_operation_receipts ENABLE TRIGGER human_participation_operation_guard`); err != nil {
			t.Error("owned five-family restore immutable receipt guard", err)
			return
		}
		if _, err = tx.Exec(ctx, `DELETE FROM activity_invitations i WHERE i.activity_id=$1 AND i.invitee_account_id=$2 AND EXISTS(SELECT 1 FROM activities a WHERE a.id=i.activity_id AND a.created_by_account_id=$3)`, f.purpose.f.private, b.person.ID, b.other.ID); err != nil {
			t.Error("owned five-family exact invitation cleanup", err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			t.Error("owned five-family cleanup commit", err)
		}
	})
	join := func(op string) {
		t.Helper()
		source, e := b.store.ReadEntityActions(b.ctx, access, ea.Ref{Type: "activity", ID: f.purpose.f.public})
		if e != nil {
			t.Fatal(e)
		}
		operationID := f.id(t)
		receipt, fresh, e := b.store.WriteHumanParticipationOperation(b.ctx, access, f.purpose.f.public, ap.OperationInput{OperationID: operationID, Operation: op}, ea.BoundCondition{SourceVersion: source.View.SourceVersion, ValidUntil: source.View.ValidUntil, Kind: ea.Join, Operation: op})
		if e != nil || !fresh || receipt.EffectID == "" {
			t.Fatal("original participation operation", e)
		}
		operations = append(operations, struct{ operationID, effectID string }{operationID, receipt.EffectID})
	}
	join("JOIN")
	part, e := b.store.GetParticipation(b.ctx, b.person.ID, f.purpose.f.public)
	if e != nil {
		t.Fatal(e)
	}
	evidence, e := b.store.PutOwnMemoryEvidence(b.ctx, f.purpose.f.place.private.owner, memory.ID, f.id(t), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: memory.Version, SourceType: agentevent.ParticipationSource, SourceID: part.ID})
	if e != nil {
		t.Fatal("original explicit evidence", e)
	}
	join("CANCEL_RSVP")
	// 094 records invalidation without taking a Memory lock. A direct private
	// observation proves the actual stale evidence awaits this consumer; do
	// not call a provenance/reinforcement GET that would clean it first.
	var awaiting bool
	if e = b.pool.QueryRow(b.ctx, `SELECT status='CURRENT' AND source_id=$2 AND source_token IS NOT NULL FROM agent_memory_evidence WHERE id=$1`, evidence.ID, part.ID).Scan(&awaiting); e != nil || !awaiting {
		t.Fatal("real stale evidence was already cleared before mixed dispatch", e)
	}
	f.write(t, "五族真实原Task查询修改")
	if _, e = b.store.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput()); e != nil {
		t.Fatal(e)
	}
	report := f.run(t)
	for _, handler := range []agentoutbox.HandlerVersion{agentoutbox.HandlerV1, agentoutbox.MemoryHandler, agentoutbox.PreferenceHandler, agentoutbox.TaskRequestHandler, agentoutbox.ParticipationHandler} {
		var seen bool
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE subject_id=$1 AND handler_version=$2 AND control_state IN('INVALIDATION_COMPLETE','UNAVAILABLE'))`, b.person.ID, handler).Scan(&seen); e != nil || !seen {
			t.Fatal("actual five-family consumer not serviced", handler, e)
		}
	}
	if report.ConfirmedReceipts < 8 || report.BusinessExecution != "UNAVAILABLE" {
		t.Fatal("mixed five real families were only nominated", report)
	}
	var removed, bound, scrubbed bool
	if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1),EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$2),EXISTS(SELECT 1 FROM agent_memory_evidence WHERE id=$3 AND status='REMOVED' AND source_id IS NULL AND source_token IS NULL AND weight=0)`, unbound.ID, old.ID, evidence.ID).Scan(&removed, &bound, &scrubbed); e != nil || !removed || !bound || !scrubbed {
		t.Fatal("mixed Task/participation actual cleanup absent", e)
	}
	f.assertGrant(t, grant.ID, grant.Revision+1, true)
	before := f.taskHistory(t)
	again := mixedMaintenanceCommand(t, b.person.ID)
	if again.ConfirmedReceipts != 0 || again.Counts.RetiredControls != 0 || before != f.taskHistory(t) {
		t.Fatal("current compiled CLI rewrote completed native history", again)
	}
}

func TestTaskRequestContextNativeCommitReplyLossNoRetry(t *testing.T) {
	ownedMigrationDatabase(t)
	f := taskRequestNative(t)
	b := f.purpose.f.place.private.base
	f.run(t)
	unbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, f.purpose.selection)
	if e != nil {
		t.Fatal(e)
	}
	f.write(t, "Task真正提交成功但COMMIT回包丢失")
	_, claim := f.claim(t)
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.TLSConfig != nil {
		t.Fatal("owned loopback non-TLS fault fixture required")
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	var seen atomic.Bool
	cfg.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, e := net.SplitHostPort(address)
		if e != nil || network != "tcp" || host != "127.0.0.1" {
			return nil, errors.New("owned loopback dial required")
		}
		conn, e := (&net.Dialer{}).DialContext(ctx, network, address)
		if e != nil {
			return nil, e
		}
		return &mixedCommitReplyLossConn{Conn: conn, seen: &seen}, nil
	}
	faulty, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal("owned fault connection unavailable")
	}
	defer faulty.Close()
	p, e := New(faulty, false).ConsumeAgentOutboxControl(b.ctx, claim)
	if !seen.Load() || e == nil || p != (agentoutbox.ConsumerRecord{}) {
		t.Fatal("real COMMIT reply loss released success/receipt")
	}
	var actual bool
	if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1) AND EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE event_id=$2 AND handler_version=$3 AND control_state='INVALIDATION_COMPLETE')`, unbound.ID, claim.EventID, agentoutbox.TaskRequestHandler).Scan(&actual); e != nil || !actual {
		t.Fatal("independent native observer saw no actual committed cleanup", e)
	}
	before := f.taskHistory(t)
	p, e = b.store.ConsumeAgentOutboxControl(b.ctx, claim)
	if e != nil || p.State != agentoutbox.MemoryComplete || f.taskHistory(t) != before {
		t.Fatal("explicit same claim receipt recovery repeated cleanup", e)
	}
	f.run(t)
}

func TestTaskRequestContextNativeMigrationRoundtripAndUsedHistory(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal("owned migrated child unavailable")
	}
	defer pool.Close()
	conn, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	sql := func(name string) string {
		t.Helper()
		raw, e := os.ReadFile("../../migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		return string(raw)
	}
	var partInstalled bool
	if e = conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) LIKE '%agent-participation-outbox-v1%' FROM pg_constraint WHERE conrelid='agent_domain_outbox'::regclass AND conname='agent_domain_outbox_schema_version_check'`).Scan(&partInstalled); e != nil {
		t.Fatal(e)
	}
	// Migration stack order is real: 133 extends 132's shared guards and must
	// be empty-downed first, then reapplied after the 132 empty roundtrip.
	if partInstalled {
		if _, e = conn.Exec(ctx, sql("133_agent_participation_invalidation.down.sql")); e != nil {
			t.Fatal("empty133 before132", e)
		}
	}
	snapshot := func() string {
		var s string
		if e := conn.QueryRow(ctx, `SELECT jsonb_build_object('accounts',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY id) FROM accounts a),'agents',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY id) FROM agents a),'tasks',(SELECT jsonb_agg(to_jsonb(t)||jsonb_build_object('_xmin',t.xmin::text) ORDER BY id) FROM agent_tasks t),'grants',(SELECT jsonb_agg(to_jsonb(g)||jsonb_build_object('_xmin',g.xmin::text) ORDER BY id) FROM consent_grants g),'outbox',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('_xmin',d.xmin::text) ORDER BY event_id) FROM agent_domain_outbox d))::text`).Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	before := snapshot()
	if _, e = conn.Exec(ctx, sql("132_agent_task_request_context_invalidation.down.sql")); e != nil {
		t.Fatal("empty132 down", e)
	}
	if snapshot() != before {
		t.Fatal("132 down rewrote original rows/xmin")
	}
	if _, e = conn.Exec(ctx, sql("132_agent_task_request_context_invalidation.sql")); e != nil {
		t.Fatal("actual132 reapply", e)
	}
	if snapshot() != before {
		t.Fatal("132 up rewrote original rows/xmin")
	}
	if partInstalled {
		if _, e = conn.Exec(ctx, sql("133_agent_participation_invalidation.sql")); e != nil {
			t.Fatal("actual133 restored after132", e)
		}
	}
	conn.Release()
	f := taskRequestNative(t)
	b := f.purpose.f.place.private.base
	beforeHistory := f.taskHistory(t)
	conn, e = pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	// 132 refuses used history before touching shared constraints or guards.
	_, e = conn.Exec(ctx, sql("132_agent_task_request_context_invalidation.down.sql"))
	var native *pgconn.PgError
	if !errors.As(e, &native) || native.Code != "55000" {
		t.Fatal("used132 down discarded original event history", e)
	}
	if _, e = conn.Exec(ctx, "ROLLBACK"); e != nil {
		t.Fatal(e)
	}
	if f.taskHistory(t) != beforeHistory {
		t.Fatal("failed used down changed native event/xmin")
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*)>0 FROM agent_domain_outbox WHERE subject_id=$1 AND schema_version=$2`, b.person.ID, agentoutbox.TaskRequestSchema).Scan(&partInstalled); e != nil || !partInstalled {
		t.Fatal("reapplied actual Task producer absent", e)
	}
}
