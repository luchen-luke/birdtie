package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type joinNativeFixture struct {
	f     *egressFixture
	g     *AgentActivityJoin
	a     agentprofile.PrivateAccess
	input aa.ActivityJoinInput
}

func joinNative(t *testing.T) *joinNativeFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	f := &egressFixture{f: &configurationFixture{native: eventIntegrationFixture(t)}, gate: egressGate(t, true)}
	b := f.f.native.private.base
	f.f.native.task.Intent = agentworkspace.FindActivity
	var e error
	f.f.native.task, e = b.store.UpdateTask(b.ctx, f.f.native.task)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	activity := outputPublishedActivity(t, f, "public")
	// The original native reply writer selects its authorized refs and completes
	// this Task. Never hand-fill COMPLETED or manufacture result membership.
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(f.f.native.task))
	if e != nil {
		t.Fatal(e)
	}
	f.f.native.task, e = b.store.CaptureOwnHumanReply(b.ctx, arp.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.f.native.private.owner.SessionDigest, TaskID: f.f.native.task.ID, ExpectedTask: raw})
	if e != nil {
		t.Fatal("original native activity reply", e)
	}
	last := len(f.f.native.task.Conversation) - 1
	if agentworkspace.ValidateReplyMembership(f.f.native.task, last) != nil {
		t.Fatal("native membership missing")
	}
	x := &joinNativeFixture{f: f, g: NewAgentActivityJoin(b.store, f.gate, true), a: f.f.native.private.owner, input: aa.ActivityJoinInput{TaskID: f.f.native.task.ID, LogicalOperationID: egressID(t, f), ActionID: egressID(t, f), ActivityID: activity, ExpectedTaskUpdatedAt: f.f.native.task.UpdatedAt.UTC().Format(time.RFC3339Nano), ReplyTurnDigest: f.f.native.task.Conversation[last].ResultMembership.TurnDigest}}
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		tx, e := b.pool.Begin(ctx)
		if e != nil {
			t.Error(e)
			return
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(ctx, `ALTER TABLE agent_activity_join_effects DISABLE TRIGGER USER`); e != nil {
			t.Error(e)
			return
		}
		if _, e = tx.Exec(ctx, `DELETE FROM agent_activity_join_effects WHERE owner_id=ANY($1::uuid[])`, b.accounts); e != nil {
			t.Error("owned immutable JOIN effect cleanup", e)
			return
		}
		if _, e = tx.Exec(ctx, `ALTER TABLE agent_activity_join_effects ENABLE TRIGGER USER`); e != nil {
			t.Error(e)
			return
		}
		for _, q := range []string{`DELETE FROM agent_action_dispatches WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_action_approvals WHERE owner_id=ANY($1::uuid[]) AND action_kind='ACTIVITY_JOIN'`, `DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) AND purpose='OWN_ACTIVITY_JOIN_ACTION'`} {
			if _, e = tx.Exec(ctx, q, b.accounts); e != nil {
				t.Error(e)
				return
			}
		}
		if e = tx.Commit(ctx); e != nil {
			t.Error(e)
		}
	})
	return x
}
func (x *joinNativeFixture) preview(t *testing.T) aa.ActivityJoinPreview {
	t.Helper()
	b := x.f.f.native.private.base
	v, e := x.g.PreviewOwn(b.ctx, x.a, x.input)
	if e != nil || v.State != aa.Pending {
		t.Fatal("native JOIN preview", e)
	}
	if e = x.g.ValidatePreviewOwn(b.ctx, x.a, v); e != nil {
		t.Fatal("native preview final", e)
	}
	return v
}
func (x *joinNativeFixture) confirm(t *testing.T, v aa.ActivityJoinPreview) {
	t.Helper()
	b := x.f.f.native.private.base
	if _, e := x.g.ConfirmOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest); e != nil {
		t.Fatal("native concrete JOIN approval", e)
	}
}
func TestAgentActivityJoinDefaultOFFAndTypedNilProof(t *testing.T) {
	a := agentprofile.PrivateAccess{}
	for _, g := range []*AgentActivityJoin{nil, NewAgentActivityJoin(nil, nil, false), NewAgentActivityJoin(&Store{}, nil, true)} {
		if _, e := g.PreviewOwn(context.Background(), a, aa.ActivityJoinInput{}); !errors.Is(e, aa.ErrUnavailable) {
			t.Fatal("missing/default gateway grants new action", e)
		}
	}
	g := NewAgentActivityJoin(nil, nil, false)
	var p *joinProof
	if e := g.ValidatePreviewOwn(context.Background(), a, aa.ActivityJoinPreview{Proof: p}); !errors.Is(e, aa.ErrDenied) {
		t.Fatal(e)
	}
	if e := g.ValidateReceiptOwn(context.Background(), a, aa.ActivityJoinReceipt{Proof: p}); !errors.Is(e, aa.ErrDenied) {
		t.Fatal(e)
	}
}

type joinLostHTTPReply struct {
	header http.Header
	status int
}

func (w *joinLostHTTPReply) Header() http.Header       { return w.header }
func (w *joinLostHTTPReply) WriteHeader(n int)         { w.status = n }
func (w *joinLostHTTPReply) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestAgentActivityJoinNativeHTTPApprovalLostReplyAndOffRecovery(t *testing.T) {
	x := joinNative(t)
	b := x.f.f.native.private.base
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal("synthetic native token")
	}
	b.exec(`UPDATE sessions SET token_sha256=$2 WHERE token_sha256=$1`, x.a.SessionDigest[:], digest[:])
	x.a.SessionDigest = digest
	h := httpapi.New(nil, b.store, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, httpapi.WithAgentActivityJoin(x.g))
	call := func(path string, body []byte, w http.ResponseWriter) {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(w, r)
	}
	input, _ := json.Marshal(x.input)
	w := httptest.NewRecorder()
	call("/v1/me/agent-activity-join/previews", input, w)
	if w.Code != 201 {
		t.Fatal("real registered preview", w.Code, w.Body.String())
	}
	var p struct {
		Data aa.ActivityJoinPreview `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("real preview wire")
	}
	body, _ := json.Marshal(map[string]string{"bindingDigest": p.Data.BindingDigest})
	base := "/v1/me/agent-activity-join/approvals/" + p.Data.ApprovalID + "/"
	w = httptest.NewRecorder()
	call(base+"confirm", body, w)
	if w.Code != 200 {
		t.Fatal("real native authenticator refresh invalidated confirmation", w.Code, w.Body.String())
	}
	lost := &joinLostHTTPReply{header: http.Header{}}
	call(base+"execute", body, lost)
	if lost.status != 200 {
		t.Fatal("actual JOIN did not reach lost response", lost.status)
	}
	x.g.Disable()
	x.f.gate.Disable(agentfeature.Enrichment)
	w = httptest.NewRecorder()
	call(base+"reconcile", nil, w)
	if w.Code != 200 {
		t.Fatal("OFF original outcome unavailable", w.Code, w.Body.String())
	}
	var result struct {
		Data aa.ActivityJoinReceipt `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Status != aa.Succeeded {
		t.Fatal("lost reply claimed success without causal row")
	}
	var n int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_activity_join_effects WHERE approval_id=$1`, p.Data.ApprovalID).Scan(&n); e != nil || n != 1 {
		t.Fatal("lost reply duplicate effect", e, n)
	}
	w = httptest.NewRecorder()
	call(base+"execute", body, w)
	if w.Code != 503 {
		t.Fatal("OFF minted another execution", w.Code)
	}
}

func TestAgentActivityJoinNativeAuthRefreshConcurrencyAndCausalHistory(t *testing.T) {
	x := joinNative(t)
	b := x.f.f.native.private.base
	authenticate := func() {
		t.Helper()
		if actor, e := b.store.AuthenticateHumanSocial(b.ctx, x.a.SessionDigest); e != nil || actor.ID != b.person.ID {
			t.Fatal("real HTTP authenticator", e)
		}
	}
	authenticate()
	v := x.preview(t)
	var gen int64
	if e := b.pool.QueryRow(b.ctx, `SELECT authorization_generation FROM sessions WHERE token_sha256=$1`, x.a.SessionDigest[:]).Scan(&gen); e != nil {
		t.Fatal(e)
	}
	authenticate()
	var refreshed int64
	if e := b.pool.QueryRow(b.ctx, `SELECT authorization_generation FROM sessions WHERE token_sha256=$1`, x.a.SessionDigest[:]).Scan(&refreshed); e != nil || refreshed != gen {
		t.Fatal("ordinary live idle renewal changed authority", e)
	}
	x.confirm(t, v)
	authenticate()
	type result struct {
		r aa.ActivityJoinReceipt
		h aa.ActivityJoinCommitment
		e error
	}
	out := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, h, e := x.g.CommitOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest)
			out <- result{r, h, e}
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	var h aa.ActivityJoinCommitment
	handles := 0
	for r := range out {
		if r.e != nil {
			t.Fatal("competing native Commit", r.e)
		}
		if r.h != nil {
			h = r.h
			handles++
		}
	}
	if handles != 1 {
		t.Fatal("more than one original dispatch capability", handles)
	}
	if _, e := json.Marshal(h); !errors.Is(e, aa.ErrServerOnly) {
		t.Fatal("capability serialized", e)
	}
	_, cl, e := h.Begin(b.ctx, x.a)
	if e != nil || cl == nil {
		t.Fatal(e)
	}
	if _, _, e = h.Begin(b.ctx, x.a); !errors.Is(e, aa.ErrConsumed) {
		t.Fatal("Begin replay", e)
	}
	r, e := cl.Execute(b.ctx, x.a)
	if e != nil || r.Status != aa.Succeeded || r.ParticipationID == nil {
		t.Fatal("actual JOIN effect", e, r.Status)
	}
	if e = x.g.ValidateReceiptOwn(b.ctx, x.a, r); e != nil {
		t.Fatal(e)
	}
	if _, e = cl.Execute(b.ctx, x.a); !errors.Is(e, aa.ErrConsumed) {
		t.Fatal("Execute replay", e)
	}
	var effects, human int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_activity_join_effects WHERE owner_id=$1),(SELECT count(*) FROM human_participation_operation_receipts WHERE owner_account_id=$1)`, b.person.ID).Scan(&effects, &human); e != nil || effects != 1 || human != 0 {
		t.Fatal("Agent effect mixed Human receipt", effects, human, e)
	}
	if _, e = b.store.ReadOwnSandboxDispatch(b.ctx, x.f.f.native.access, r.DispatchID, x.f.gate); e == nil {
		t.Fatal("old sandbox read disclosed new JOIN")
	}
	if _, e = b.store.ReconcileOwnSandboxApproval(b.ctx, x.f.f.native.access, v.ApprovalID, x.f.gate); e == nil {
		t.Fatal("sandbox absence reclassified JOIN")
	}
	if _, e = b.store.CancelParticipation(b.ctx, b.person.ID, x.input.ActivityID); e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE agent_tasks SET status='FAILED' WHERE id=$1`, x.input.TaskID)
	b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, v.AgentID)
	x.g.Disable()
	x.f.gate.Disable(agentfeature.Enrichment)
	recovered, e := x.g.ReconcileOwn(b.ctx, x.a, v.ApprovalID)
	if e != nil || recovered.Status != aa.Succeeded || *recovered.ParticipationID != *r.ParticipationID || !recovered.AppliedAt.Equal(*r.AppliedAt) {
		t.Fatal("later human cancel or OFF rewrote causal result", e)
	}
	if e = x.g.ValidateReceiptOwn(b.ctx, x.a, recovered); e != nil {
		t.Fatal("OFF final read", e)
	}
	if _, e = x.g.RevokeOwn(b.ctx, x.a, v.ApprovalID); e != nil {
		t.Fatal("OFF exact original revoke unavailable", e)
	}
	if _, e = x.g.PreviewOwn(b.ctx, x.a, x.input); !errors.Is(e, aa.ErrUnavailable) {
		t.Fatal("OFF new write", e)
	}
}
func TestAgentActivityJoinNativeIdentitySourceABAAndPermanentFence(t *testing.T) {
	x := joinNative(t)
	b := x.f.f.native.private.base
	v := x.preview(t)
	peer := x.f.f.native.private.peer
	if _, e := x.g.ConfirmOwn(b.ctx, peer, v.ApprovalID, v.BindingDigest); e == nil {
		t.Fatal("cross owner confirm")
	}
	if _, e := x.g.ConfirmOwn(b.ctx, x.a, v.ApprovalID, strings.Repeat("0", 64)); !errors.Is(e, aa.ErrChanged) {
		t.Fatal("wrong digest", e)
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, x.a.SessionDigest[:])
	b.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, x.a.SessionDigest[:])
	if _, e := x.g.ConfirmOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest); e == nil {
		t.Fatal("Session authorization ABA reused old approval")
	}
	v = x.preview(t)
	x.confirm(t, v)
	b.exec(`UPDATE activities SET title=title WHERE id=$1`, x.input.ActivityID)
	if _, h, e := x.g.CommitOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest); e == nil || h != nil {
		t.Fatal("source ABA minted capability")
	}
	v = x.preview(t)
	x.confirm(t, v)
	_, h, e := x.g.CommitOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest)
	if e != nil || h == nil {
		t.Fatal(e)
	}
	_, cl, e := h.Begin(b.ctx, x.a)
	if e != nil {
		t.Fatal(e)
	}
	x.g.Disable()
	r, e := x.g.ReconcileOwn(b.ctx, x.a, v.ApprovalID)
	if e != nil || r.Status != aa.NoEffect {
		t.Fatal("closed native absence", e)
	}
	if _, e = cl.Execute(b.ctx, x.a); e == nil {
		t.Fatal("late claim crossed permanent fence")
	}
	var n int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2`, x.input.ActivityID, b.person.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal("retired claim mutated original participation", e, n)
	}
}
func TestAgentActivityJoinNativeAfterEffectTriggerWaitExpires(t *testing.T) {
	x := joinNative(t)
	b := x.f.f.native.private.base
	var until time.Time
	if e := b.pool.QueryRow(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '4 seconds' WHERE token_sha256=$1 RETURNING idle_expires_at`, x.a.SessionDigest[:]).Scan(&until); e != nil {
		t.Fatal(e)
	}
	v := x.preview(t)
	x.confirm(t, v)
	_, h, e := x.g.CommitOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest)
	if e != nil {
		t.Fatal(e)
	}
	_, cl, e := h.Begin(b.ctx, x.a)
	if e != nil {
		t.Fatal(e)
	}
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(b.ctx, `SELECT pg_advisory_xact_lock(250130)`); e != nil {
		t.Fatal(e)
	}
	b.exec(`CREATE FUNCTION join130_wait() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(250130);RETURN NEW;END$$;CREATE TRIGGER join130_wait AFTER INSERT ON agent_activity_join_effects FOR EACH ROW EXECUTE FUNCTION join130_wait()`)
	t.Cleanup(func() {
		b.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS join130_wait ON agent_activity_join_effects;DROP FUNCTION IF EXISTS join130_wait()`)
	})
	done := make(chan error, 1)
	go func() { _, e := cl.Execute(b.ctx, x.a); done <- e }()
	seen := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO agent_activity_join_effects%')`).Scan(&seen); e != nil {
			t.Fatal(e)
		}
		if seen {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !seen {
		t.Fatal("actual effect AFTER trigger did not wait")
	}
	if wait := time.Until(until) + 50*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("waiting beyond original Session committed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("effect wait did not resolve")
	}
	var n int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_activity_join_effects WHERE owner_id=$1)+(SELECT count(*) FROM activity_participations WHERE participant_account_id=$1 AND activity_id=$2)`, b.person.ID, x.input.ActivityID).Scan(&n); e != nil || n != 0 {
		t.Fatal("expired transition/effect not atomic rollback", n, e)
	}
}

type joinCrashInput struct {
	Digest                                [32]byte
	Owner, Approval, DigestBinding, Phase string
}
type joinCrashKey struct{}
type joinCrashTracer struct{ effect bool }

func (tr *joinCrashTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "INSERT INTO agent_activity_join_effects") {
		tr.effect = true
	}
	return context.WithValue(ctx, joinCrashKey{}, strings.EqualFold(strings.TrimSpace(d.SQL), "commit"))
}
func (tr *joinCrashTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	commit, _ := ctx.Value(joinCrashKey{}).(bool)
	if tr.effect && commit && d.Err == nil {
		os.Exit(0)
	}
}
func TestAgentActivityJoinNativeRecoveryProcess(t *testing.T) {
	path := os.Getenv("BIRDTIE_JOIN_CRASH_PRIVATE_INPUT")
	if path == "" {
		return
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("private child input unavailable")
	}
	var in joinCrashInput
	if json.Unmarshal(raw, &in) != nil {
		t.Fatal("private child input invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal("owned child database config")
	}
	if in.Phase == "effect" {
		cfg.ConnConfig.Tracer = &joinCrashTracer{}
	}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("owned child pool")
	}
	defer pool.Close()
	g := NewAgentActivityJoin(New(pool, false), egressGate(t, true), true)
	a := agentprofile.PrivateAccess{SessionDigest: in.Digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: in.Owner}}
	_, h, e := g.CommitOwn(ctx, a, in.Approval, in.DigestBinding)
	if e != nil || h == nil {
		t.Fatal("child actual Commit", e)
	}
	if in.Phase == "commit" {
		os.Exit(0)
	}
	_, cl, e := h.Begin(ctx, a)
	if e != nil {
		t.Fatal("child Begin", e)
	}
	if in.Phase == "begin" {
		os.Exit(0)
	}
	if _, e = cl.Execute(ctx, a); e != nil {
		t.Fatal("child actual effect", e)
	}
	t.Fatal("actual effect commit crash checkpoint not reached")
}
func TestAgentActivityJoinNativeProcessCrashCommitBeginEffectNoResend(t *testing.T) {
	for _, phase := range []string{"commit", "begin", "effect"} {
		t.Run(phase, func(t *testing.T) {
			x := joinNative(t)
			b := x.f.f.native.private.base
			v := x.preview(t)
			x.confirm(t, v)
			path := filepath.Join(t.TempDir(), "private-child-input.json")
			raw, _ := json.Marshal(joinCrashInput{Digest: x.a.SessionDigest, Owner: b.person.ID, Approval: v.ApprovalID, DigestBinding: v.BindingDigest, Phase: phase})
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal("private child input write")
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestAgentActivityJoinNativeRecoveryProcess$", "-test.timeout=25s")
			cmd.Env = append(os.Environ(), "BIRDTIE_JOIN_CRASH_PRIVATE_INPUT="+path)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("owned child phase %s failed: %v %s", phase, e, out)
			}
			g := NewAgentActivityJoin(New(b.pool, false), egressGate(t, false), false)
			r, e := g.ReconcileOwn(b.ctx, x.a, v.ApprovalID)
			want := aa.NoEffect
			if phase == "effect" {
				want = aa.Succeeded
			}
			if e != nil || r.Status != want {
				t.Fatal("restart causal reconcile", phase, e, r.Status)
			}
			again, e := g.ReconcileOwn(b.ctx, x.a, v.ApprovalID)
			if e != nil || again.DispatchID != r.DispatchID || again.Status != want {
				t.Fatal("restart changed effect", e)
			}
			var d, w int
			if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_action_dispatches WHERE approval_id=$1),(SELECT count(*) FROM agent_activity_join_effects WHERE approval_id=$1)`, v.ApprovalID).Scan(&d, &w); e != nil || d != 1 || w != map[bool]int{true: 1, false: 0}[phase == "effect"] {
				t.Fatal("restart reissued effect", d, w, e)
			}
		})
	}
}
