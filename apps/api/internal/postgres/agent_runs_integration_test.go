package postgres

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	ew "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentworker"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const runWorker1 = "89d59b97-dabe-4345-ac34-dfc73bcbe374"
const runWorker2 = "8c61b3fc-d3c9-4802-a0d6-53ff359bfa59"

func runFixture(t *testing.T, on bool) (*retentionFixture, *AgentRuns, agentprofile.PrivateAccess) {
	t.Helper()
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	return f, NewAgentRuns(b.store, pipelineFlags(t, on)), f.f.f.place.private.owner
}
func requireRun(t *testing.T, r ar.Record, e error, state ar.State) {
	t.Helper()
	if e != nil || ar.ValidateRecord(r) != nil || r.State != state {
		t.Fatal("Run", r, e)
	}
}
func TestAgentRunNativeHumanCommitWaitingOffAndCancelTerminal(t *testing.T) {
	f, s, a := runFixture(t, false)
	b := f.f.f.place.private.base
	before := pipelineCount(t, f)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID})
	requireRun(t, r, e, ar.WaitingConfirmation)
	for i := 0; i < 20; i++ {
		v, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID})
		if e != nil || v.ID != r.ID || v.Version != r.Version || !v.Deadline.Equal(r.Deadline) {
			t.Fatal("renewed retry", v, e)
		}
	}
	if _, e = s.ClaimAgentRun(b.ctx, runWorker1); !errors.Is(e, ar.ErrUnavailable) {
		t.Fatal("OFF dispatched", e)
	}
	if pipelineCount(t, f) != before {
		t.Fatal("waiting/OFF changed original source effects")
	}
	var title string
	if e = b.pool.QueryRow(b.ctx, `SELECT title FROM moments WHERE id=$1`, f.f.moment.ID).Scan(&title); e != nil || title != "UNSELECTED_PRIVATE_TITLE" {
		t.Fatal("human Moment commit lost", title, e)
	}
	c, e := s.CancelOwn(b.ctx, a, r.ID, r.Version)
	requireRun(t, c, e, ar.Cancelled)
	if _, e = s.CancelOwn(b.ctx, a, r.ID, c.Version); !errors.Is(e, ar.ErrConflict) {
		t.Fatal("terminal cancel", e)
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE agent_enrichment_runs SET state='QUEUED',reason='QUEUED',version=version+1 WHERE id=$1`, r.ID); e == nil {
		t.Fatal("SQL resurrected terminal")
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "UNSELECTED") || strings.Contains(string(raw), "session") || strings.Contains(string(raw), "token") || strings.Contains(string(raw), "body") {
		t.Fatal("metadata leaked", string(raw))
	}
	if pipelineCount(t, f) != before {
		t.Fatal("cancel wrote candidate/Memory")
	}
}
func TestAgentRunNativeTwoWorkersAtomicSuccessAnd100Retries(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	var wg sync.WaitGroup
	var claims [2]ar.Claim
	var errs [2]error
	for i, w := range []string{runWorker1, runWorker2} {
		wg.Add(1)
		go func(i int, w string) { defer wg.Done(); claims[i], errs[i] = s.ClaimAgentRun(b.ctx, w) }(i, w)
	}
	wg.Wait()
	var c ar.Claim
	n := 0
	for i, e := range errs {
		if e == nil {
			c = claims[i]
			n++
		} else if !errors.Is(e, ar.ErrNotFound) {
			t.Fatal("claim", e)
		}
	}
	if n != 1 || c.RunID != r.ID {
		t.Fatal("competing claims", n, claims, errs)
	}
	out, e := s.ExecuteAgentRun(b.ctx, c)
	requireRun(t, out, e, ar.Succeeded)
	if out.CandidateID == "" || out.Version != 3 || out.Attempt != 1 {
		t.Fatal(out)
	}
	stable := pipelineCount(t, f)
	for i := 0; i < 100; i++ {
		v, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
		if e != nil || v.ID != r.ID || v.CandidateID != out.CandidateID || !v.Deadline.Equal(r.Deadline) {
			t.Fatal("retry", i, v, e)
		}
		v, e = s.ExecuteAgentRun(b.ctx, c)
		requireRun(t, v, e, ar.Succeeded)
	}
	if stable != pipelineCount(t, f) {
		t.Fatal("repeated worker duplicated effect")
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('steps',(SELECT jsonb_agg(to_jsonb(x)) FROM agent_run_steps x WHERE run_id=$1),'dispatch',(SELECT jsonb_agg(to_jsonb(x)) FROM agent_run_dispatches x WHERE run_id=$1),'audit',(SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM agent_run_audit x WHERE run_id=$1))::text`, r.ID).Scan(&raw); e != nil || strings.Contains(raw, "羽毛球") || strings.Contains(raw, "token") || !strings.Contains(raw, "EFFECT_CONFIRMED") {
		t.Fatal("checkpoint audit", raw, e)
	}
	if _, e = s.CancelOwn(b.ctx, a, r.ID, out.Version); !errors.Is(e, ar.ErrConflict) {
		t.Fatal("cancel successful effect", e)
	}
}
func TestAgentRunNativeExplicitGrantAttachCurrentSessionAndCAS(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID})
	requireRun(t, r, e, ar.WaitingConfirmation)
	_, g := f.approve(t)
	if _, e = s.AttachOwnGrant(b.ctx, a, r.ID, r.Version+1, g.ID); !errors.Is(e, ar.ErrConflict) {
		t.Fatal("wrong version attached", e)
	}
	if _, e = s.ReadOwn(b.ctx, f.f.f.place.private.peer, r.ID); e == nil {
		t.Fatal("other owner read runtime")
	}
	queued, e := s.AttachOwnGrant(b.ctx, a, r.ID, r.Version, g.ID)
	requireRun(t, queued, e, ar.Queued)
	if !queued.Deadline.Equal(r.Deadline) {
		t.Fatal("attach extended deadline")
	}
	if _, e = s.AttachOwnGrant(b.ctx, a, r.ID, queued.Version, g.ID); !errors.Is(e, ar.ErrConflict) {
		t.Fatal("reattached", e)
	}
	c, e := s.ClaimAgentRun(b.ctx, runWorker1)
	if e != nil {
		t.Fatal(e)
	}
	cancelled, e := s.CancelOwn(b.ctx, a, r.ID, queued.Version+1)
	requireRun(t, cancelled, e, ar.Cancelled)
	before := pipelineCount(t, f)
	actual, e := s.ExecuteAgentRun(b.ctx, c)
	requireRun(t, actual, e, ar.Cancelled)
	if pipelineCount(t, f) != before {
		t.Fatal("cancelled worker wrote")
	}
}
func TestAgentRunNativeExpiredFenceAndCurrentConsent(t *testing.T) {
	for _, mode := range []string{"fence", "revoke", "metadataChange"} {
		t.Run(mode, func(t *testing.T) {
			f, s, a := runFixture(t, true)
			b := f.f.f.place.private.base
			_, g := f.approve(t)
			r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
			requireRun(t, r, e, ar.Queued)
			old, e := s.claimAgentRun(b.ctx, runWorker1, 20*time.Millisecond)
			if e != nil {
				t.Fatal(e)
			}
			before := pipelineCount(t, f)
			if mode == "fence" {
				time.Sleep(30 * time.Millisecond)
				current, e := s.ClaimAgentRun(b.ctx, runWorker2)
				if e != nil || current.Fence <= old.Fence || current.Attempt != 2 {
					t.Fatal(current, e)
				}
				if _, e = s.ExecuteAgentRun(b.ctx, old); !errors.Is(e, ar.ErrConflict) {
					t.Fatal("late worker accepted", e)
				}
				if pipelineCount(t, f) != before {
					t.Fatal("late worker wrote")
				}
				v, e := s.ExecuteAgentRun(b.ctx, current)
				requireRun(t, v, e, ar.Succeeded)
			} else {
				// Use the current longer lease to isolate consent/source revalidation.
				time.Sleep(30 * time.Millisecond)
				current, e := s.ClaimAgentRun(b.ctx, runWorker2)
				if e != nil {
					t.Fatal(e)
				}
				if mode == "revoke" {
					if _, e = b.store.RevokeOwnCandidateRetention(b.ctx, a, g.ID, 1); e != nil {
						t.Fatal(e)
					}
				} else {
					if _, e = b.pool.Exec(b.ctx, `UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, r.AgentID); e != nil {
						t.Fatal(e)
					}
				}
				v, e := s.ExecuteAgentRun(b.ctx, current)
				requireRun(t, v, e, ar.Failed)
				var n int
				if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1`, a.WorkspacePrincipal.ID).Scan(&n); e != nil || n != 0 {
					t.Fatal("revoked/ABA wrote", n, e)
				}
			}
		})
	}
}

func TestAgentRunProcessCrashHelper(t *testing.T) {
	mode := os.Getenv("BIRDTIE_TEST_RUN_CRASH_MODE")
	if mode == "" {
		return
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("disposable-only helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := NewAgentRuns(New(pool, true), pipelineFlags(t, true))
	c, e := s.claimAgentRun(ctx, runWorker1, 2*time.Second)
	if e != nil || c.RunID != os.Getenv("BIRDTIE_TEST_RUN_ID") {
		t.Fatal("native claim", c, e)
	}
	signal := func() { fmt.Println("RUN_NATIVE_CRASH_READY"); <-ctx.Done() }
	if mode == "after_claim" {
		signal()
		return
	}
	if mode == "after_commit" {
		out, e := s.ExecuteAgentRun(ctx, c)
		if e != nil || out.State != ar.Succeeded {
			t.Fatal("native commit", out, e)
		}
		signal()
		return
	}
	var owner, gid, encoded string
	if e = pool.QueryRow(ctx, `SELECT r.owner_id::text,r.retention_grant_id::text,encode(se.token_sha256,'hex') FROM agent_enrichment_runs r JOIN sessions se ON se.id=r.session_id WHERE r.id=$1`, c.RunID).Scan(&owner, &gid, &encoded); e != nil {
		t.Fatal(e)
	}
	raw, e := hex.DecodeString(encoded)
	if e != nil || len(raw) != 32 {
		t.Fatal("native access unavailable")
	}
	a := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: owner}}
	copy(a.SessionDigest[:], raw)
	x := &candidatePipelineExecutor{store: s.store, runGuard: runLeaseGuard{c}, hook: func(stage string) error {
		if stage == "after_effect" {
			signal()
		}
		return nil
	}}
	if _, e = acp.NewService(x, s.flags).StageOwnMomentCandidate(ctx, a, gid); e != nil {
		t.Fatal(e)
	}
}
func TestAgentRunNativeActualProcessKillRestartReconcilesOriginalEffect(t *testing.T) {
	for _, mode := range []string{"after_claim", "after_effect", "after_commit"} {
		t.Run(mode, func(t *testing.T) {
			f, s, a := runFixture(t, true)
			b := f.f.f.place.private.base
			_, g := f.approve(t)
			r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
			requireRun(t, r, e, ar.Queued)
			before := pipelineCount(t, f)
			cmd := exec.CommandContext(b.ctx, os.Args[0], "-test.run=^TestAgentRunProcessCrashHelper$", "-test.v")
			cmd.Env = append(os.Environ(), "BIRDTIE_TEST_RUN_CRASH_MODE="+mode, "BIRDTIE_TEST_RUN_ID="+r.ID)
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			var logs strings.Builder
			cmd.Stderr = &logs
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					t.Log("native child:", scanner.Text())
					if scanner.Text() == "RUN_NATIVE_CRASH_READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					cmd.Wait()
					t.Fatal("helper not ready", logs.String())
				}
			case <-time.After(15 * time.Second):
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatal("native helper timeout", logs.String())
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			if e = cmd.Wait(); e == nil {
				t.Fatal("process did not actually die")
			}
			if mode != "after_commit" && before != pipelineCount(t, f) {
				t.Fatal("killed effect Tx partially committed")
			}
			if mode == "after_commit" {
				stable := pipelineCount(t, f)
				v, e := NewAgentRuns(b.store, pipelineFlags(t, false)).ReadOwn(b.ctx, a, r.ID)
				requireRun(t, v, e, ar.Succeeded)
				if stable != pipelineCount(t, f) {
					t.Fatal("restarting metadata read replayed effect")
				}
				if _, e = s.ClaimAgentRun(b.ctx, runWorker2); !errors.Is(e, ar.ErrNotFound) {
					t.Fatal("terminal Run reclaimed", e)
				}
				return
			}
			// Poll actual native PG clock until the original persisted lease, never
			// shortening or resetting its deadline in the fixture.
			timeout := time.Now().Add(5 * time.Second)
			var c ar.Claim
			for time.Now().Before(timeout) {
				c, e = s.ClaimAgentRun(b.ctx, runWorker2)
				if e == nil {
					break
				}
				if !errors.Is(e, ar.ErrNotFound) {
					t.Fatal(e)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if e != nil || c.RunID != r.ID || c.Attempt != 2 || c.Fence != 2 {
				t.Fatal("restart claim", c, e)
			}
			resumed := NewAgentRuns(b.store, pipelineFlags(t, true))
			out, e := resumed.ExecuteAgentRun(b.ctx, c)
			requireRun(t, out, e, ar.Succeeded)
			stable := pipelineCount(t, f)
			again, e := resumed.ExecuteAgentRun(b.ctx, c)
			requireRun(t, again, e, ar.Succeeded)
			if again.CandidateID != out.CandidateID || stable != pipelineCount(t, f) {
				t.Fatal("restart duplicated effect")
			}
		})
	}
}

func TestAgentRunNativeNaturalDeadlineTerminalAndUsedDownGuard(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	// Native requested candidate TTL, not a forged Run deadline or fake clock.
	f.selection.RetainUntil = time.Now().UTC().Truncate(time.Microsecond).Add(600 * time.Millisecond)
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	before := pipelineCount(t, f)
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "083_agent_enrichment_runs.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = conn.Exec(b.ctx, string(down))
	if e == nil {
		t.Fatal("used down erased Run")
	}
	conn.Exec(b.ctx, "ROLLBACK")
	conn.Release()
	if before != pipelineCount(t, f) {
		t.Fatal("failed down altered source")
	}
	time.Sleep(time.Until(r.Deadline) + 30*time.Millisecond)
	n, e := s.ExpireAgentRuns(b.ctx, 100)
	if e != nil || n != 1 {
		t.Fatal("expire", n, e)
	}
	v, e := s.ReadOwn(b.ctx, a, r.ID)
	requireRun(t, v, e, ar.Expired)
	n, e = s.ExpireAgentRuns(b.ctx, 100)
	if e != nil || n != 0 {
		t.Fatal("expiry not idempotent", n, e)
	}
	if _, e = s.AttachOwnGrant(b.ctx, a, r.ID, v.Version, g.ID); e == nil {
		t.Fatal("expired Run revived")
	}
	if pipelineCount(t, f) != before {
		t.Fatal("expiry wrote candidate/source/Memory")
	}
}
func TestAgentRunNativeRealFinalWaitExpiresFenceAndRollsBackEffects(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	c, e := s.claimAgentRun(b.ctx, runWorker1, 800*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	before := pipelineCount(t, f)
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	const key int64 = 830016777
	if _, e = lock.Exec(b.ctx, `SELECT pg_advisory_xact_lock($1)`, key); e != nil {
		t.Fatal(e)
	}
	atBoundary := make(chan struct{})
	done := make(chan error, 1)
	executor := &candidatePipelineExecutor{store: b.store, runGuard: runLeaseGuard{c}, txHook: func(ctx context.Context, tx pgx.Tx, stage string) error {
		if stage == "before_commit" {
			close(atBoundary)
			_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key)
			return e
		}
		return nil
	}}
	go func() { _, e := acp.NewService(executor, s.flags).StageOwnMomentCandidate(b.ctx, a, g.ID); done <- e }()
	select {
	case <-atBoundary:
	case e := <-done:
		t.Fatal("did not reach final wait", e)
	case <-time.After(5 * time.Second):
		t.Fatal("final wait timeout")
	}
	// Confirm the actual PG wait before letting its real persisted lease expire.
	var waits int
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock%'`).Scan(&waits); e != nil {
			t.Fatal(e)
		}
		if waits > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if waits < 1 {
		t.Fatal("no actual native advisory wait")
	}
	time.Sleep(time.Until(c.LeaseUntil) + 20*time.Millisecond)
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	e = <-done
	if !errors.Is(e, ar.ErrExpired) {
		t.Fatal("late final Run fence committed", e)
	}
	if before != pipelineCount(t, f) {
		t.Fatal("expired final wait partially committed candidate/effect")
	}
	v, e := s.ReadOwn(b.ctx, a, r.ID)
	requireRun(t, v, e, ar.Running)
}

func TestAgentRunNativeMissingAuditAndFeatureFailureNeverBlockHumanMoment(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	if _, e = b.pool.Exec(b.ctx, `ALTER TABLE agent_enrichment_runs DISABLE TRIGGER agent_run_checkpoint`); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e = b.pool.Exec(b.ctx, `ALTER TABLE agent_enrichment_runs ENABLE TRIGGER agent_run_checkpoint`); e != nil {
			t.Error(e)
		}
	}()
	if _, e = s.ClaimAgentRun(b.ctx, runWorker1); !errors.Is(e, ar.ErrUnavailable) {
		t.Fatal("missing durable audit dispatched", e)
	}
	// Actual original domain action is already independent of the new runtime,
	// not a mocked success returned while an enrichment transaction failed.
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.f.f.place.city, Title: "本人领域提交不等待异步处理", Body: "明确私密草稿", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil || m.ID == "" || m.Revision != 1 {
		t.Fatal("enrichment failure blocked Moment", m, e)
	}
	var n int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_domain_outbox WHERE source_id=$1 AND source_revision=1`, m.ID).Scan(&n); e != nil || n != 1 {
		t.Fatal("native human event absent", n, e)
	}
	if _, e = s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: m.ID}); !errors.Is(e, ar.ErrUnavailable) {
		t.Fatal("missing audit fabricated Run", e)
	}
	if _, e = b.pool.Exec(b.ctx, `ALTER TABLE agent_enrichment_runs ENABLE TRIGGER agent_run_checkpoint`); e != nil {
		t.Fatal(e)
	}
	c, e := s.ClaimAgentRun(b.ctx, runWorker1)
	if e != nil {
		t.Fatal(e)
	}
	before := pipelineCount(t, f)
	x := &candidatePipelineExecutor{store: b.store, runGuard: runLeaseGuard{c}, hook: func(stage string) error {
		if stage == "before_commit" {
			return s.flags.Disable(agentfeature.Memory)
		}
		return nil
	}}
	if _, e = acp.NewService(x, s.flags).StageOwnMomentCandidate(b.ctx, a, g.ID); !errors.Is(e, acp.ErrUnavailable) {
		t.Fatal("flag revocation committed Run", e)
	}
	if pipelineCount(t, f) != before {
		t.Fatal("flag failure committed effect")
	}
	v, e := s.ReadOwn(b.ctx, a, r.ID)
	requireRun(t, v, e, ar.Running)
}

func TestAgentRunNativeActualCLIIsBoundedDefaultOffAndRedactsConfiguration(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	binary := os.Getenv("BIRDTIE_AGENT_RUN_TEST_BINARY")
	if binary == "" {
		t.Fatal("native runner must build original command from same source frame")
	}
	env := []string{}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "BIRDTIE_AGENT_FEATURE_FLAGS=") {
			env = append(env, item)
		}
	}
	before := pipelineCount(t, f)
	for i := 0; i < 2; i++ {
		cmd := exec.CommandContext(b.ctx, binary)
		cmd.Env = env
		out, e := cmd.CombinedOutput()
		if e != nil || !strings.Contains(string(out), `"claimed":0`) || !strings.Contains(string(out), `"unavailable":true`) {
			t.Fatal("actual CLI default OFF", string(out), e)
		}
	}
	v, e := s.ReadOwn(b.ctx, a, r.ID)
	requireRun(t, v, e, ar.Queued)
	if before != pipelineCount(t, f) || v.Version != r.Version {
		t.Fatal("default OFF CLI wrote source/effects or renewed Run")
	}
	cmd := exec.CommandContext(b.ctx, binary)
	cmd.Env = append(env, "BIRDTIE_AGENT_FEATURE_FLAGS=PRIVATE_CONFIGURATION_CANARY")
	out, e := cmd.CombinedOutput()
	if e == nil || strings.Contains(string(out), "CANARY") || strings.Contains(string(out), "postgres://") {
		t.Fatal("configuration exposed", string(out), e)
	}
}

func TestAgentRunNativeChildCheckpointWaitMustRecheckFinalLease(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	// Allow the real pipeline to reach its child-row lock under whole-suite
	// concurrent database load; expiry is still the original fixed native lease.
	c, e := s.claimAgentRun(b.ctx, runWorker1, 5*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	before := pipelineCount(t, f)
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var step string
	if e = blocker.QueryRow(b.ctx, `SELECT step_name FROM agent_run_steps WHERE run_id=$1 FOR UPDATE`, r.ID).Scan(&step); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	x := &candidatePipelineExecutor{store: b.store, runGuard: runLeaseGuard{c}}
	go func() { _, e := acp.NewService(x, s.flags).StageOwnMomentCandidate(b.ctx, a, g.ID); done <- e }()
	var waits int
	for end := time.Now().Add(4 * time.Second); time.Now().Before(end); {
		if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%UPDATE agent_enrichment_runs SET state=%SUCCEEDED%'`).Scan(&waits); e != nil {
			t.Fatal(e)
		}
		if waits > 0 {
			break
		}
		select {
		case e := <-done:
			t.Fatal("writer ended before native child wait", e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if waits < 1 {
		t.Fatal("actual Run child row wait absent")
	}
	t.Log("ACTUAL_STEP_ROW_WAIT_CROSSED_FIXED_RUN_LEASE")
	time.Sleep(time.Until(c.LeaseUntil) + 50*time.Millisecond)
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	e = <-done
	if !errors.Is(e, ar.ErrExpired) {
		t.Fatal("child checkpoint wait passed lease: effect must roll back", e)
	}
	if pipelineCount(t, f) != before {
		t.Fatal("expired tail child wait committed original candidate/effect")
	}
}

func TestAgentRunNativeExhaustedHeadDoesNotHideQueuedWork(t *testing.T) {
	f, s, a := runFixture(t, true)
	b := f.f.f.place.private.base
	_, g := f.approve(t)
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: g.ID})
	requireRun(t, r, e, ar.Queued)
	before := pipelineCount(t, f)
	for i := int64(1); i <= ar.MaxAttempts; i++ {
		c, e := s.claimAgentRun(b.ctx, runWorker1, 100*time.Millisecond)
		if e != nil || c.RunID != r.ID || c.Attempt != i {
			t.Fatal("actual native attempts", c, e)
		}
		time.Sleep(time.Until(c.LeaseUntil) + 20*time.Millisecond)
	}
	f2, _, a2 := runFixture(t, true)
	_, g2 := f2.approve(t)
	r2, e := s.ScheduleOwn(b.ctx, a2, ar.Input{MomentID: f2.f.moment.ID, RetentionGrantID: g2.ID})
	requireRun(t, r2, e, ar.Queued)
	result, e := ew.RunOnce(b.ctx, s, runWorker2, 2)
	if e != nil || result.Failed != 1 || result.Claimed != 1 || result.Succeeded != 1 {
		t.Fatal("exhausted head must count failure and continue bounded original queued work", result, e)
	}
	v, e := s.ReadOwn(b.ctx, a, r.ID)
	requireRun(t, v, e, ar.Failed)
	if v.Reason != "ATTEMPTS_EXHAUSTED" || pipelineCount(t, f) != before {
		t.Fatal("exhausted run changed source/effects", v)
	}
	v, e = s.ReadOwn(b.ctx, a2, r2.ID)
	requireRun(t, v, e, ar.Succeeded)
	var pending int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_run_dispatches WHERE run_id=$1 AND state='PENDING_RECONCILIATION'`, r.ID).Scan(&pending); e != nil || pending != 0 {
		t.Fatal("failed head kept pending dispatch", pending, e)
	}
}
