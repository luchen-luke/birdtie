package postgres

import (
	"context"
	"errors"
	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A failed claim is never retried merely because its public error is unavailable.
// This one rollback-only diagnostic preserves the native SQL failure without
// emitting query parameters, retained source bodies or database credentials.
type participationClaimDiagnosticTx struct {
	pgx.Tx
	statement, failed, sqlState, message string
}

func (d *participationClaimDiagnosticTx) stage(q string) {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 120 {
		q = q[:120]
	}
	d.statement = q
}

func (d *participationClaimDiagnosticTx) failure(err error) {
	if err == nil || d.failed != "" {
		return
	}
	d.failed = d.statement
	var native *pgconn.PgError
	if errors.As(err, &native) {
		d.sqlState, d.message = native.Code, native.Message
	} else {
		d.message = err.Error()
	}
}

type participationClaimDiagnosticRow struct {
	pgx.Row
	tx *participationClaimDiagnosticTx
}

func (r participationClaimDiagnosticRow) Scan(values ...any) error {
	err := r.Row.Scan(values...)
	r.tx.failure(err)
	return err
}

func (d *participationClaimDiagnosticTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	d.stage(q)
	return participationClaimDiagnosticRow{Row: d.Tx.QueryRow(ctx, q, args...), tx: d}
}

func (d *participationClaimDiagnosticTx) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	d.stage(q)
	rows, err := d.Tx.Query(ctx, q, args...)
	d.failure(err)
	return rows, err
}

func (d *participationClaimDiagnosticTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	d.stage(q)
	tag, err := d.Tx.Exec(ctx, q, args...)
	d.failure(err)
	return tag, err
}

func participationClaimFailureNative(t *testing.T, pool *pgxpool.Pool, ctx context.Context, owner, worker string) {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Log("native participation diagnostic transaction", err)
		return
	}
	defer tx.Rollback(context.Background())
	d := &participationClaimDiagnosticTx{Tx: tx}
	_, _, err = claimParticipationOutboxTx(ctx, d, worker, owner)
	t.Logf("native participation rollback diagnostic: result=%v lastStatement=%q failedStatement=%q sqlState=%q message=%q", err, d.statement, d.failed, d.sqlState, d.message)
}

func participationClaimNative(t *testing.T, s *Store, pool *pgxpool.Pool, ctx context.Context, owner, worker, source string) (agentoutbox.Record, agentoutbox.Claim) {
	t.Helper()
	var retired int
	// The public selector nominates the owner's earliest native event, not a
	// particular source. This isolated fixture also owns an ActivityLeft source;
	// its real retirement is progress even when it precedes the requested source.
	if pool.QueryRow(ctx, `SELECT count(*) FROM agent_domain_outbox WHERE schema_version='agent-participation-outbox-v1' AND subject_id=$1 AND delivery_state IN('INVALIDATED','EXPIRED','DEAD_LETTER')`, owner).Scan(&retired) != nil {
		t.Fatal("native retirement before")
	}
	for i := 0; i < 16; i++ {
		r, c, e := s.ClaimAgentOutboxControlForSubject(ctx, actorref.PrincipalRef{Type: actorref.Person, ID: owner}, worker, agentoutbox.ParticipationHandler)
		if e == nil {
			if r.Event.Subject.ID != owner || c.Subject.ID != owner || r.Event.SchemaVersion != agentoutbox.ParticipationSchema || c.HandlerVersion != agentoutbox.ParticipationHandler {
				t.Fatal("native claim escaped owned participation fixture")
			}
			if r.Event.Source.ID == source {
				return r, c
			}
			// Finish a competing fixture-owned source through the actual native
			// consumer. Never drop it, fake a checkpoint or return its claim as
			// though it exercised the requested source's cleanup assertions.
			progress, consumeErr := s.ConsumeAgentOutboxControl(ctx, c)
			if consumeErr != nil || progress.EventID != r.Event.EventID || progress.State != agentoutbox.MemoryComplete || progress.Reason != agentoutbox.ReasonCleanupComplete {
				t.Fatal("competing owned participation source cleanup", consumeErr, progress.State)
			}
			continue
		}
		if !errors.Is(e, agentoutbox.ErrUnavailable) {
			t.Fatal("native participation claim", e)
		}
		var now int
		if pool.QueryRow(ctx, `SELECT count(*) FROM agent_domain_outbox WHERE schema_version='agent-participation-outbox-v1' AND subject_id=$1 AND delivery_state IN('INVALIDATED','EXPIRED','DEAD_LETTER')`, owner).Scan(&now) != nil || now <= retired {
			participationClaimFailureNative(t, pool, ctx, owner, worker)
			t.Fatal("unknown retirement must not be retried")
		}
		retired = now
	}
	t.Fatal("bounded source retirement did not expose current event")
	return agentoutbox.Record{}, agentoutbox.Claim{}
}
func participationFixtureAccess(f *memoryEvidenceFixture) ea.Access {
	b := f.native.base.private.base
	return ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.native.base.private.owner.SessionDigest}
}
func participationOperationFixtureCleanup(t *testing.T, f *memoryEvidenceFixture, activity, source, operation string) {
	t.Helper()
	b := f.native.base.private.base
	owner := b.person.ID
	// Registered after the legacy source fixture: LIFO removes only this test's
	// new immutable causal receipt before its Participation/account FK cleanup.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tx, e := b.pool.Begin(ctx)
		if e != nil {
			t.Error("owned participation operation cleanup", e)
			return
		}
		defer tx.Rollback(context.Background())
		for _, q := range []string{`ALTER TABLE human_participation_operation_receipts DISABLE TRIGGER human_participation_operation_guard`, `DELETE FROM human_participation_operation_receipts WHERE owner_account_id=$1 AND activity_id=$2 AND operation_id=$3 AND effect_id=$4`, `ALTER TABLE human_participation_operation_receipts ENABLE TRIGGER human_participation_operation_guard`} {
			var args []any
			if q[0] == 'D' {
				args = []any{owner, activity, operation, source}
			}
			if _, e = tx.Exec(ctx, q, args...); e != nil {
				t.Error("owned participation operation cleanup", e)
				return
			}
		}
		if e = tx.Commit(ctx); e != nil {
			t.Error("owned participation operation cleanup commit", e)
		}
	})
}
func participationFixtureCancel(t *testing.T, f *memoryEvidenceFixture) (string, ap.OperationReceipt) {
	t.Helper()
	b := f.native.base.private.base
	source := f.sources[agentevent.ParticipationSource]
	var activity string
	if e := b.pool.QueryRow(b.ctx, `SELECT activity_id FROM activity_participations WHERE id=$1 AND participant_account_id=$2`, source, b.person.ID).Scan(&activity); e != nil {
		t.Fatal(e)
	}
	a := participationFixtureAccess(f)
	r, e := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "activity", ID: activity})
	if e != nil {
		t.Fatal(e)
	}
	operation := agentMemoryID(t, f.native.base.private)
	participationOperationFixtureCleanup(t, f, activity, source, operation)
	out, fresh, e := b.store.WriteHumanParticipationOperation(b.ctx, a, activity, ap.OperationInput{OperationID: operation, Operation: "CANCEL_RSVP"}, ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Join, Operation: "CANCEL_RSVP"})
	if e != nil || !fresh || out.State != "cancelled" {
		t.Fatal("actual Human cancellation", e)
	}
	return activity, out
}
func TestParticipationInvalidationNativeSameTimestampABAAllReadDoorsAndFreshRef(t *testing.T) {
	ownedMigrationDatabase(t)
	f := memoryEvidenceNativeFixture(t)
	b := f.native.base.private.base
	source := f.sources[agentevent.ParticipationSource]
	old := f.attach(t, agentevent.ParticipationSource)
	var before, original string
	if e := b.pool.QueryRow(b.ctx, `SELECT(to_jsonb(m)||jsonb_build_object('_xmin',m.xmin::text))::text FROM agent_memories m WHERE id=$1`, f.memory.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(p)::text FROM activity_participations p WHERE id=$1`, source).Scan(&original); e != nil {
		t.Fatal(e)
	}
	candidateService := NewMemoryCandidateService(b.store, reinforcementFlags(t))
	score, _ := agentconfidence.NewUncalibratedScore(.25)
	candidate, e := candidateService.SaveOwnCandidate(b.ctx, f.native.base.private.owner, agentMemoryID(t, f.native.base.private), agentmemorycandidate.Draft{Predicate: "ACTIVITY_CATEGORY", Category: "sports", Assessment: score, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond), Sources: []agentmemorycandidate.Selector{{Type: agentevent.ParticipationSource, ID: source}}}, "", 0)
	if e != nil {
		t.Fatal("actual manually proposed candidate", e)
	}
	_, cancel := participationFixtureCancel(t, f)
	// Restore the exact prior retained timestamp/body; native xmin and 094
	// history still prove that the old source lifetime was retired.
	b.exec(`UPDATE activity_participations p SET status=o.status,cancelled_at=o.cancelled_at,updated_at=o.updated_at FROM jsonb_populate_record(NULL::activity_participations,$2::jsonb) o WHERE p.id=$1`, source, original)
	reinforcement := NewMemoryReinforcementService(b.store, reinforcementFlags(t))
	if preview, e := reinforcement.PreviewOwnReinforcement(b.ctx, f.native.base.private.owner, f.memory.ID, f.memory.Version, 0, []string{old.ID}); e == nil || preview != nil {
		t.Fatal("unrun maintenance allowed old support to revive")
	}
	if replay, e := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, old.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: f.memory.Version, SourceType: agentevent.ParticipationSource, SourceID: source}); !errors.Is(e, agentmemory.ErrConflict) || replay.ID != "" {
		t.Fatal("old evidence ID silently rebased", e)
	}
	if p := f.provenance(t); len(p.Evidence) != 0 {
		t.Fatal("lazy provenance resurrected timestamp/body ABA")
	}
	time.Sleep(time.Millisecond)
	fresh := f.attach(t, agentevent.ParticipationSource)
	if p := f.provenance(t); len(p.Evidence) != 1 || p.Evidence[0].ID != fresh.ID {
		t.Fatal("094 prior waterline erased new actual attachment")
	}
	reopened := New(b.pool, true)
	r, c := participationClaimNative(t, reopened, b.pool, b.ctx, b.person.ID, agentMemoryID(t, f.native.base.private), source)
	result, e := reopened.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil || result.State != agentoutbox.MemoryComplete || r.Event.Source.ID != source {
		t.Fatal("actual bounded source cleanup", e)
	}
	var status, after string
	if e = b.pool.QueryRow(b.ctx, `SELECT status FROM agent_memory_candidates WHERE id=$1`, candidate.ID).Scan(&status); e != nil || status != "EXPIRED" {
		t.Fatal("prior candidate support remained live", e, status)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT(to_jsonb(m)||jsonb_build_object('_xmin',m.xmin::text))::text FROM agent_memories m WHERE id=$1`, f.memory.ID).Scan(&after); e != nil || before != after {
		t.Fatal("independent EXPLICIT Memory changed", e)
	}
	if p := f.provenance(t); len(p.Evidence) != 1 || p.Evidence[0].ID != fresh.ID {
		t.Fatal("consumer erased new reference")
	}
	if read, e := b.store.ReadHumanParticipationOperation(b.ctx, participationFixtureAccess(f), cancel.ActivityID, cancel.OperationID); e != nil || read.State != "cancelled" || read.EffectID != source {
		t.Fatal("source ABA changed historical Human receipt", e)
	}
}

func TestParticipationInvalidationNativeBoundedCleanupAndLeaseWaitRollback(t *testing.T) {
	ownedMigrationDatabase(t)
	f := memoryEvidenceNativeFixture(t)
	b := f.native.base.private.base
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	b.ctx = ctx
	source := f.sources[agentevent.ParticipationSource]
	// Real owner declarations and their original manual references, not a fake
	// consumer effect, exercise the per-claim 128-row bound.
	for i := 0; i < 129; i++ {
		id := agentMemoryID(t, f.native.base.private)
		m := mustPutAgentMemory(t, f.native.base.private, id, agentMemoryInput(id))
		if _, e := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, m.ID, agentMemoryID(t, f.native.base.private), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: m.Version, SourceType: agentevent.ParticipationSource, SourceID: source}); e != nil {
			t.Fatal("real reference setup", e)
		}
	}
	participationFixtureCancel(t, f)
	worker := agentMemoryID(t, f.native.base.private)
	_, c := participationClaimNative(t, b.store, b.pool, b.ctx, b.person.ID, worker, source)
	first, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil || first.State != agentoutbox.Pending {
		t.Fatal("bounded checkpoint claimed complete", e, first.State)
	}
	var left int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE owner_id=$1 AND source_type='ACTIVITY_PARTICIPATION' AND source_id=$2 AND status='CURRENT'`, b.person.ID, source).Scan(&left); e != nil || left != 1 {
		t.Fatal("128 bound not actual", e, left)
	}
	// Wait beyond the retained native 30-second claim, without mutating it.
	_, c = participationClaimNative(t, b.store, b.pool, b.ctx, b.person.ID, worker, source)
	until := c.LeaseUntil
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(b.ctx, `SELECT pg_advisory_xact_lock(270133)`); e != nil {
		t.Fatal(e)
	}
	b.exec(`CREATE FUNCTION participation133_wait() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(270133);RETURN NEW;END$$;CREATE TRIGGER participation133_wait AFTER UPDATE OF status ON agent_memory_evidence FOR EACH ROW WHEN(OLD.status='CURRENT' AND NEW.status='REMOVED') EXECUTE FUNCTION participation133_wait()`)
	t.Cleanup(func() {
		b.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS participation133_wait ON agent_memory_evidence;DROP FUNCTION IF EXISTS participation133_wait()`)
	})
	done := make(chan error, 1)
	go func() { _, e := b.store.ConsumeAgentOutboxControl(b.ctx, c); done <- e }()
	seen := false
	limit := time.Now().Add(time.Second)
	for time.Now().Before(limit) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%UPDATE agent_memory_evidence e SET status=%')`).Scan(&seen); e != nil {
			t.Fatal(e)
		}
		if seen {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !seen {
		t.Fatal("actual cleanup AFTER wait absent")
	}
	memoryStormAwaitDeadline(t, b.pool, b.ctx, until)
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("expired actual lease committed cleanup")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup wait stuck")
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE owner_id=$1 AND source_type='ACTIVITY_PARTICIPATION' AND source_id=$2 AND status='CURRENT'`, b.person.ID, source).Scan(&left); e != nil || left != 1 {
		t.Fatal("late cleanup did not rollback", e, left)
	}
	b.exec(`DROP TRIGGER participation133_wait ON agent_memory_evidence;DROP FUNCTION participation133_wait()`)
	_, c = participationClaimNative(t, b.store, b.pool, b.ctx, b.person.ID, worker, source)
	last, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil || last.State != agentoutbox.MemoryComplete {
		t.Fatal("new native fence recovery failed", e)
	}
}

func TestParticipation133NativeUnusedRoundtripRestores132AndUsedRefuses(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	conn, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	read := func(name string) string {
		t.Helper()
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		return string(raw)
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if e := conn.QueryRow(ctx, `SELECT jsonb_build_object('accounts',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY id) FROM accounts a),'tasks',(SELECT jsonb_agg(to_jsonb(t)||jsonb_build_object('_xmin',t.xmin::text) ORDER BY id) FROM agent_tasks t),'participation',(SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text) ORDER BY id) FROM activity_participations p),'memory',(SELECT jsonb_agg(to_jsonb(m)||jsonb_build_object('_xmin',m.xmin::text) ORDER BY id) FROM agent_memories m))::text`).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	before := snapshot()
	if _, e = conn.Exec(ctx, read("133_agent_participation_invalidation.down.sql")); e != nil {
		t.Fatal("unused133 down", e)
	}
	if snapshot() != before {
		t.Fatal("down changed native rows/xmin")
	}
	var oldGuard string
	if e = conn.QueryRow(ctx, `SELECT pg_get_functiondef(to_regprocedure('birdtie_guard_agent_outbox()'))`).Scan(&oldGuard); e != nil || !containsParticipationWire([]byte(oldGuard), "agent-task-request-outbox-v1") || containsParticipationWire([]byte(oldGuard), "agent-participation-outbox-v1") {
		t.Fatal("down did not restore132 guard", e)
	}
	var oldInbox string
	if e = conn.QueryRow(ctx, `SELECT pg_get_functiondef(to_regprocedure('birdtie_guard_agent_consumer_inbox()'))`).Scan(&oldInbox); e != nil || !containsParticipationWire([]byte(oldInbox), "task-request-invalidation-v1") || containsParticipationWire([]byte(oldInbox), "participation-invalidation-v1") {
		t.Fatal("down did not restore132 inbox guard", e)
	}
	if _, e = conn.Exec(ctx, read("133_agent_participation_invalidation.sql")); e != nil {
		t.Fatal("actual133 reapply", e)
	}
	if snapshot() != before {
		t.Fatal("up changed native rows/xmin")
	}
	var newInbox string
	if e = conn.QueryRow(ctx, `SELECT pg_get_functiondef(to_regprocedure('birdtie_guard_agent_consumer_inbox()'))`).Scan(&newInbox); e != nil || !containsParticipationWire([]byte(newInbox), "participation-invalidation-v1") || !containsParticipationWire([]byte(newInbox), "participation progress requires new fenced claim") {
		t.Fatal("reapplied133 lacks bounded progress inbox guard", e)
	}
	// A second owned source fixture creates actual native metadata, then a down
	// refusal is tested against that fixture's own pool, preserving its history.
	conn.Release()
	f := rsvpPG(t)
	activity := f.activity()
	r, _, e := f.store.WriteHumanParticipationOperation(f.ctx, f.a, activity, ap.OperationInput{OperationID: f.uuid(), Operation: "JOIN"}, f.bound(activity, "JOIN"))
	if e != nil {
		t.Fatal(e)
	}
	used, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer used.Release()
	if _, e = used.Exec(f.ctx, read("133_agent_participation_invalidation.down.sql")); e == nil {
		t.Fatal("used133 history silently dropped")
	}
	used.Exec(f.ctx, `ROLLBACK`)
	if participationNativeCount(t, f, r.EffectID) != 1 {
		t.Fatal("refused down lost native capture")
	}
}

func TestParticipationInvalidationNativeSixthMoreDeadLetterNoSeventhClaim(t *testing.T) {
	ownedMigrationDatabase(t)
	f := memoryEvidenceNativeFixture(t)
	b := f.native.base.private.base
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	b.ctx = ctx
	source := f.sources[agentevent.ParticipationSource]
	for i := 0; i < 129; i++ {
		id := agentMemoryID(t, f.native.base.private)
		m := mustPutAgentMemory(t, f.native.base.private, id, agentMemoryInput(id))
		if _, e := b.store.PutOwnMemoryEvidence(ctx, f.native.base.private.owner, m.ID, agentMemoryID(t, f.native.base.private), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: m.Version, SourceType: agentevent.ParticipationSource, SourceID: source}); e != nil {
			t.Fatal("real root budget reference setup", e)
		}
	}
	participationFixtureCancel(t, f)
	worker := agentMemoryID(t, f.native.base.private)
	_, claim := participationClaimNative(t, b.store, b.pool, ctx, b.person.ID, worker, source)
	first, e := b.store.ConsumeAgentOutboxControl(ctx, claim)
	if e != nil || first.State != agentoutbox.Pending {
		t.Fatal("first actual bounded checkpoint", e)
	}
	blocker, e := b.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var retained string
	if e = blocker.QueryRow(ctx, `SELECT id FROM agent_memory_evidence WHERE owner_id=$1 AND source_type='ACTIVITY_PARTICIPATION' AND source_id=$2 AND status='CURRENT' FOR UPDATE`, b.person.ID, source).Scan(&retained); e != nil {
		t.Fatal("actual remaining evidence row lock", e)
	}
	var last agentoutbox.ConsumerRecord
	for attempt := int64(2); attempt <= 6; attempt++ {
		_, claim = participationClaimNative(t, b.store, b.pool, ctx, b.person.ID, worker, source)
		last, e = b.store.ConsumeAgentOutboxControl(ctx, claim)
		if e != nil || last.Attempt != attempt {
			t.Fatal("actual fixed root checkpoint", attempt, e)
		}
		if attempt < 6 && (last.State != agentoutbox.Pending || last.Reason != agentoutbox.ReasonCleanupMore) {
			t.Fatal("locked row falsely completed", attempt)
		}
	}
	if last.State != agentoutbox.DeadLetter || last.Reason != agentoutbox.ReasonRootExhausted {
		t.Fatal("sixth more rolled back or invented completion")
	}
	var state, reason string
	var attempt, fence, left int64
	if e = b.pool.QueryRow(ctx, `SELECT d.delivery_state,d.attempt,d.fence,i.reason_code,(SELECT count(*) FROM agent_memory_evidence e WHERE e.owner_id=d.subject_id AND e.source_type='ACTIVITY_PARTICIPATION' AND e.source_id=d.source_id AND e.status='CURRENT') FROM agent_domain_outbox d JOIN agent_consumer_inbox i ON i.event_id=d.event_id AND i.subject_id=d.subject_id AND i.handler_version='participation-invalidation-v1' WHERE d.event_id=$1`, claim.EventID).Scan(&state, &attempt, &fence, &reason, &left); e != nil || state != string(agentoutbox.DeadLetter) || attempt != 6 || fence != 6 || reason != string(agentoutbox.ReasonRootExhausted) || left != 1 {
		t.Fatal("root budget native rows inconsistent", e)
	}
	if _, _, e = b.store.ClaimAgentOutboxControlForSubject(ctx, actorref.PrincipalRef{Type: actorref.Person, ID: b.person.ID}, worker, agentoutbox.ParticipationHandler); e == nil {
		t.Fatal("seventh claim created")
	}
	if e = b.pool.QueryRow(ctx, `SELECT attempt,fence FROM agent_domain_outbox WHERE event_id=$1`, claim.EventID).Scan(&attempt, &fence); e != nil || attempt != 6 || fence != 6 {
		t.Fatal("rejected seventh claim altered root", e)
	}
	if e = blocker.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
}
