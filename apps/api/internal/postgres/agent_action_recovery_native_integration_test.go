package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reuse the original native Preview/Confirm/Commit/Claim and private sandbox.
// No domain effect, approval, dispatch, effectKey or receipt is fabricated.
// The only HTTP route tested is existing approval-address recovery.
func recoveryNativeSession(t *testing.T, x *actionFixture, owner string) (string, string) {
	t.Helper()
	b := x.f.f.native.private.base
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	var id string
	e = b.pool.QueryRow(b.ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour') RETURNING id`, owner, digest[:]).Scan(&id)
	if e != nil {
		t.Fatal("owned current recovery session", e)
	}
	return token, id
}
func recoveryNativeHandler(s *Store, x *actionFixture, access identity.AccessStore, gateway aa.HumanRecoveryGateway) http.Handler {
	if access == nil {
		access = s
	}
	if gateway == nil {
		gateway = NewHumanSandboxRecovery(s, x.f.gate)
	}
	return httpapi.New(nil, access, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, httpapi.WithSandboxRecovery(gateway))
}
func recoveryNativeCall(t *testing.T, h http.Handler, ctx context.Context, token, id, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/me/agent-sandbox-approvals/"+id+"/reconcile", strings.NewReader(body)).WithContext(ctx)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func recoveryNativeReceipt(t *testing.T, w *httptest.ResponseRecorder, x *actionFixture, want string) aa.HumanRecoveryReceipt {
	t.Helper()
	var out struct{ Data aa.HumanRecoveryReceipt }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("native registered recovery code=%d body=%s", w.Code, w.Body.String())
	}
	if aa.ValidateHumanRecoveryReceipt(out.Data, x.f.f.native.private.base.person, x.p.Binding.ApprovalID) != nil || out.Data.Status != want {
		t.Fatal("not exact original closed receipt", out.Data)
	}
	for _, secret := range []string{"binding", "payload", "session", "effect_key", "effectKey", "authority", "sourceGeneration", "grantId", x.p.Proposal.Value, x.p.BindingDigest} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("private capability/body leaked", secret)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("historical private receipt cacheable")
	}
	return out.Data
}
func recoveryNativeDenied(t *testing.T, w *httptest.ResponseRecorder, code int, x *actionFixture) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("native denial code=%d want=%d body=%s", w.Code, code, w.Body.String())
	}
	for _, s := range []string{`"data"`, x.p.Binding.ApprovalID, x.p.Proposal.Value, "dispatchId", "effectId"} {
		if strings.Contains(w.Body.String(), s) {
			t.Fatal("denied old receipt escaped", s)
		}
	}
}
func recoveryNativeRows(t *testing.T, x *actionFixture) string {
	t.Helper()
	b := x.f.f.native.private.base
	var raw string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('approvals',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM agent_action_approvals a WHERE owner_id=ANY($1::uuid[])), 'dispatches',(SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM agent_action_dispatches d WHERE owner_id=ANY($1::uuid[])), 'writes',(SELECT jsonb_agg(to_jsonb(w) ORDER BY w.id) FROM agent_sandbox_writes w WHERE owner_id=ANY($1::uuid[])))::text`, b.accounts).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func recoveryNativeOtherEffects(t *testing.T, x *actionFixture) string {
	t.Helper()
	// Use the original owned SQL observer after deliberate account retirement;
	// a suspended owner must not regain a readable model-budget API session.
	outputAssertRevokedBudgetSQL(t, x.f, 0)
	b := x.f.f.native.private.base
	var allocated int64
	if e := b.pool.QueryRow(b.ctx, `SELECT COALESCE(sum(n),0)::bigint FROM (
 SELECT allocated_input+allocated_output+allocated_cost n FROM model_budget_accounts WHERE owner_id=$1
 UNION ALL SELECT allocated_input+allocated_output+allocated_cost FROM model_budget_roots WHERE owner_id=$1 AND root_trace_id=$2
 UNION ALL SELECT allocated_input+allocated_output+allocated_cost FROM model_budget_tasks WHERE owner_id=$1 AND root_trace_id=$2) v`, b.person.ID, x.f.root).Scan(&allocated); e != nil || allocated != 0 {
		t.Fatal("sandbox recovery allocated model tokens/cost", e, allocated)
	}
	var raw string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('messages',(SELECT count(*) FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])), 'inbox',(SELECT count(*) FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])), 'saved',(SELECT count(*) FROM saved_items WHERE owner_account_id=ANY($1::uuid[])), 'ties',(SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])), 'memories',(SELECT jsonb_agg(to_jsonb(m)||jsonb_build_object('xmin',m.xmin::text) ORDER BY m.id) FROM agent_memories m WHERE owner_id=ANY($1::uuid[])), 'privateProfiles',(SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('xmin',p.xmin::text) ORDER BY p.agent_id) FROM agent_private_profiles p WHERE owner_id=ANY($1::uuid[])), 'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY g.id) FROM consent_grants g WHERE owner_account_id=ANY($1::uuid[]) AND purpose='OWN_SANDBOX_ACTION'))::text`, b.accounts).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	return toolBusinessSnapshot(t, x.f) + raw
}

// Cancel only after the original successful native effect transaction COMMIT.
// This does not substitute its result or execute another tool.
type recoveryNativeEffectReplyLost struct {
	armed  bool
	cancel context.CancelFunc
}

func (tr *recoveryNativeEffectReplyLost) TraceQueryStart(c context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "INSERT INTO agent_sandbox_writes") {
		tr.armed = true
	}
	return context.WithValue(c, actionCrashKey{}, strings.EqualFold(strings.TrimSpace(d.SQL), "commit"))
}
func (tr *recoveryNativeEffectReplyLost) TraceQueryEnd(c context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	committed, _ := c.Value(actionCrashKey{}).(bool)
	if tr.armed && committed && d.Err == nil {
		tr.armed = false
		tr.cancel()
	}
}
func recoveryNativePool(t *testing.T, x *actionFixture, name string, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Tracer = tracer
	if name != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = name
	}
	pool, e := pgxpool.NewWithConfig(x.f.f.native.private.base.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	return pool
}
func TestSandboxApprovalRecoveryNativeLostEffectReplyAndStableRegisteredReceipt(t *testing.T) {
	x := actionNativeFixture(t)
	b := x.f.f.native.private.base
	actionApprove(t, x)
	before := recoveryNativeOtherEffects(t, x)
	ctx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	tr := &recoveryNativeEffectReplyLost{cancel: cancel}
	s := New(recoveryNativePool(t, x, "", tr), false)
	d, h, e := s.CommitOwnSandboxAction(b.ctx, x.f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, x.f.gate)
	if e != nil || h == nil {
		t.Fatal("original native commit", e)
	}
	_, claim, e := h.Begin(b.ctx, x.f.f.native.access, x.f.gate)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := claim.Execute(ctx, x.f.f.native.access, x.f.gate); !errors.Is(e, aa.ErrUnknown) || out.ID != "" {
		t.Fatal("actual committed effect reply not lost", e, out)
	}
	if dc, wc := actionRows(t, x); dc != 1 || wc != 1 {
		t.Fatal("actual effect not uniquely persisted", dc, wc)
	}
	token, _ := recoveryNativeSession(t, x, b.person.ID)
	fresh := New(b.pool, false)
	handler := recoveryNativeHandler(fresh, x, nil, nil)
	first := recoveryNativeReceipt(t, recoveryNativeCall(t, handler, b.ctx, token, x.p.Binding.ApprovalID, "", nil), x, aa.Succeeded)
	if first.DispatchID != d.ID || first.EffectID == nil {
		t.Fatal("lost address changed original effect", first)
	}
	rows := recoveryNativeRows(t, x)
	// An already original Claim may read its persisted success again, but
	// must not execute the private effect a second time. HTTP never returned
	// or recreated that Claim; this is the same server-held original handle.
	replay, e := claim.Execute(b.ctx, x.f.f.native.access, x.f.gate)
	if e != nil || replay.State != aa.Succeeded || replay.ID != first.DispatchID || replay.EffectID == nil || *replay.EffectID != *first.EffectID {
		t.Fatal("same original effect execution did not return persisted result", e)
	}
	again := recoveryNativeReceipt(t, recoveryNativeCall(t, recoveryNativeHandler(New(b.pool, false), x, nil, nil), b.ctx, token, x.p.Binding.ApprovalID, "", nil), x, aa.Succeeded)
	if !reflect.DeepEqual(first, again) || rows != recoveryNativeRows(t, x) || before != recoveryNativeOtherEffects(t, x) {
		t.Fatal("repeat recovery changed effect/approval/other domain")
	}
	if dc, wc := actionRows(t, x); dc != 1 || wc != 1 {
		t.Fatal(dc, wc)
	}
	t.Log("actual effect COMMIT acknowledgement lost; original approval-address HTTP recovered one original dispatch/effect twice without sender capability")
}
func TestSandboxApprovalRecoveryNativeLostCommitAddressWithoutRedispatch(t *testing.T) {
	x := actionNativeFixture(t)
	b := x.f.f.native.private.base
	actionApprove(t, x)
	before := recoveryNativeOtherEffects(t, x)
	ctx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	s := New(recoveryNativePool(t, x, "", &actionCommitLostTracer{cancel: cancel}), false)
	if d, h, e := s.CommitOwnSandboxAction(ctx, x.f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, x.f.gate); !errors.Is(e, aa.ErrUnknown) || h != nil || d.ID != "" {
		t.Fatal("original commit response loss", e, d)
	}
	token, _ := recoveryNativeSession(t, x, b.person.ID)
	out := recoveryNativeReceipt(t, recoveryNativeCall(t, recoveryNativeHandler(New(b.pool, false), x, nil, nil), b.ctx, token, x.p.Binding.ApprovalID, "", nil), x, aa.NoEffect)
	var exact string
	if e := b.pool.QueryRow(b.ctx, `SELECT id FROM agent_action_dispatches WHERE approval_id=$1 AND owner_id=$2`, x.p.Binding.ApprovalID, b.person.ID).Scan(&exact); e != nil || out.DispatchID != exact {
		t.Fatal("original dispatch address not recovered", e)
	}
	rows := recoveryNativeRows(t, x)
	second := recoveryNativeReceipt(t, recoveryNativeCall(t, recoveryNativeHandler(New(b.pool, false), x, nil, nil), b.ctx, token, x.p.Binding.ApprovalID, "", nil), x, aa.NoEffect)
	if !reflect.DeepEqual(out, second) || rows != recoveryNativeRows(t, x) || before != recoveryNativeOtherEffects(t, x) {
		t.Fatal("reconcile resent/altered original approval")
	}
	if dc, wc := actionRows(t, x); dc != 1 || wc != 0 {
		t.Fatal(dc, wc)
	}
}
func TestSandboxApprovalRecoveryNativeUnknownAndPermanentNoEffectFence(t *testing.T) {
	t.Run("no existing dispatch is UNKNOWN", func(t *testing.T) {
		x := actionNativeFixture(t)
		b := x.f.f.native.private.base
		actionApprove(t, x)
		token, _ := recoveryNativeSession(t, x, b.person.ID)
		before := recoveryNativeRows(t, x)
		other := recoveryNativeOtherEffects(t, x)
		for _, id := range []string{x.p.Binding.ApprovalID, egressID(t, x.f)} {
			w := recoveryNativeCall(t, recoveryNativeHandler(b.store, x, nil, nil), b.ctx, token, id, "", nil)
			recoveryNativeDenied(t, w, 503, x)
			if !strings.Contains(w.Body.String(), "sandbox_recovery_unknown") {
				t.Fatal("absence became known failure/no effect")
			}
		}
		if before != recoveryNativeRows(t, x) || other != recoveryNativeOtherEffects(t, x) {
			t.Fatal("unknown lookup created dispatch or changed approval")
		}
		if dc, wc := actionRows(t, x); dc != 0 || wc != 0 {
			t.Fatal(dc, wc)
		}
	})
	t.Run("existing exclusive sandbox closes every old claim", func(t *testing.T) {
		x := actionNativeFixture(t)
		b := x.f.f.native.private.base
		actionApprove(t, x)
		d, h := actionCommitNative(t, x)
		_, claim, e := h.Begin(b.ctx, x.f.f.native.access, x.f.gate)
		if e != nil {
			t.Fatal(e)
		}
		var oldFence int64
		if e = b.pool.QueryRow(b.ctx, `SELECT fence FROM agent_action_dispatches WHERE id=$1`, d.ID).Scan(&oldFence); e != nil {
			t.Fatal(e)
		}
		// Nothing else has closed this original claim before HTTP recovery.
		// Prove it is still the live original fence, then let the registered
		// recovery alone make its absence authoritative and permanent.
		original, ok := claim.(*nativeActionClaim)
		if !ok || original.fence != oldFence {
			t.Fatal("not the live original native claim")
		}
		var live bool
		if e = b.pool.QueryRow(b.ctx, `SELECT state='IN_FLIGHT' AND fence=$2 AND lease_until>clock_timestamp() FROM agent_action_dispatches WHERE id=$1`, d.ID, oldFence).Scan(&live); e != nil || !live {
			t.Fatal("original claim had already been closed before recovery", e)
		}
		token, _ := recoveryNativeSession(t, x, b.person.ID)
		before := recoveryNativeOtherEffects(t, x)
		out := recoveryNativeReceipt(t, recoveryNativeCall(t, recoveryNativeHandler(New(b.pool, false), x, nil, nil), b.ctx, token, x.p.Binding.ApprovalID, "", nil), x, aa.NoEffect)
		var fence int64
		if e = b.pool.QueryRow(b.ctx, `SELECT fence FROM agent_action_dispatches WHERE id=$1`, d.ID).Scan(&fence); e != nil || fence <= oldFence || out.DispatchID != d.ID {
			t.Fatal("native absence lacked permanent fence", e, fence, oldFence)
		}
		if _, e = claim.Execute(b.ctx, x.f.f.native.access, x.f.gate); e == nil {
			t.Fatal("old claim resurrected NO_EFFECT")
		}
		rows := recoveryNativeRows(t, x)
		recoveryNativeReceipt(t, recoveryNativeCall(t, recoveryNativeHandler(New(b.pool, false), x, nil, nil), b.ctx, token, x.p.Binding.ApprovalID, "", nil), x, aa.NoEffect)
		if rows != recoveryNativeRows(t, x) || before != recoveryNativeOtherEffects(t, x) {
			t.Fatal("closed recovery repeated side effect")
		}
		if dc, wc := actionRows(t, x); dc != 1 || wc != 0 {
			t.Fatal(dc, wc)
		}
	})
}
func TestSandboxApprovalRecoveryNativeRegisteredActorAndPayloadBoundaries(t *testing.T) {
	x := actionNativeFixture(t)
	b := x.f.f.native.private.base
	actionApprove(t, x)
	_, _ = actionCommitNative(t, x)
	token, session := recoveryNativeSession(t, x, b.person.ID)
	peer, _ := recoveryNativeSession(t, x, b.other.ID)
	org, _ := recoveryNativeSession(t, x, b.org.ID)
	expired, expiryID := recoveryNativeSession(t, x, b.person.ID)
	b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET created_at=n.at-interval '2 minutes',expires_at=n.at-interval '1 second',idle_expires_at=n.at-interval '1 second' FROM n WHERE id=$1`, expiryID)
	revoked, revokedID := recoveryNativeSession(t, x, b.person.ID)
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, revokedID)
	if token == "" || session == "" {
		t.Fatal("missing actual session")
	}
	before := recoveryNativeRows(t, x)
	other := recoveryNativeOtherEffects(t, x)
	cases := []struct {
		name, token, body string
		code              int
		edit              func(*http.Request)
	}{
		{name: "anonymous", code: 401}, {name: "invalid session", token: "invalid-token", code: 401}, {name: "expired session", token: expired, code: 401}, {name: "revoked session", token: revoked, code: 401},
		{name: "foreign person gets no address", token: peer, code: 503}, {name: "organization actor", token: org, code: 403},
		{name: "organization workspace header", token: token, code: 403, edit: func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", "") }},
		{name: "body cannot approve", token: token, body: `{"confirmed":true}`, code: 400}, {name: "query cannot choose actor", token: token, code: 400, edit: func(r *http.Request) { r.URL.RawQuery = "ownerId=" + b.person.ID }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := recoveryNativeCall(t, recoveryNativeHandler(b.store, x, nil, nil), b.ctx, c.token, x.p.Binding.ApprovalID, c.body, c.edit)
			recoveryNativeDenied(t, w, c.code, x)
			if before != recoveryNativeRows(t, x) || other != recoveryNativeOtherEffects(t, x) {
				t.Fatal("denied route altered native ledger/domain")
			}
		})
	}
}
func TestSandboxApprovalRecoveryNativeHistoricalSourceAndApprovalCannotReapprove(t *testing.T) {
	for _, phase := range []string{"source ABA", "expired approval"} {
		t.Run(phase, func(t *testing.T) {
			lifetime := time.Hour
			if phase == "expired approval" {
				lifetime = 2 * time.Second
			}
			x := actionNativeFixtureLifetime(t, lifetime)
			b := x.f.f.native.private.base
			actionApprove(t, x)
			d, _ := actionCommitNative(t, x)
			if phase == "source ABA" {
				b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, x.f.f.native.task.ID)
			} else {
				actionWaitExpiry(t, x)
			}
			before := recoveryNativeOtherEffects(t, x)
			if retry, h, e := b.store.CommitOwnSandboxAction(b.ctx, x.f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, x.f.gate); e == nil || h != nil || retry.ID != "" {
				t.Fatal("stale/expired approval rebuilt sending ability", e)
			}
			token, _ := recoveryNativeSession(t, x, b.person.ID)
			r := recoveryNativeReceipt(t, recoveryNativeCall(t, recoveryNativeHandler(New(b.pool, false), x, nil, nil), b.ctx, token, x.p.Binding.ApprovalID, "", nil), x, aa.NoEffect)
			if r.DispatchID != d.ID || before != recoveryNativeOtherEffects(t, x) {
				t.Fatal("historical recovery changed source/approval or used another effect")
			}
			if dc, wc := actionRows(t, x); dc != 1 || wc != 0 {
				t.Fatal(dc, wc)
			}
		})
	}
}

// Only the real final validator is observed. Its result never gets replaced.
type recoveryNativeFinalAccess struct {
	*Store
	before func()
	calls  int
}

func (s *recoveryNativeFinalAccess) ValidateHumanSocialResponse(c context.Context, d [32]byte, a identity.Actor) error {
	s.calls++
	if s.before != nil {
		s.before()
	}
	return s.Store.ValidateHumanSocialResponse(c, d, a)
}
func TestSandboxApprovalRecoveryNativeEncodedReceiptCurrentSessionAndCancel(t *testing.T) {
	for _, phase := range []string{"unchanged", "revoke after encoding", "suspend after encoding", "cancel after encoding", "already canceled request"} {
		t.Run(phase, func(t *testing.T) {
			x := actionNativeFixture(t)
			b := x.f.f.native.private.base
			actionApprove(t, x)
			d, _ := actionCommitNative(t, x)
			token, sid := recoveryNativeSession(t, x, b.person.ID)
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			s := &recoveryNativeFinalAccess{Store: b.store}
			if phase == "revoke after encoding" {
				s.before = func() { b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, sid) }
			}
			if phase == "suspend after encoding" {
				s.before = func() { b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID) }
			}
			if phase == "cancel after encoding" {
				s.before = cancel
			}
			if phase == "already canceled request" {
				cancel()
			}
			before := recoveryNativeOtherEffects(t, x)
			w := recoveryNativeCall(t, recoveryNativeHandler(b.store, x, s, nil), ctx, token, x.p.Binding.ApprovalID, "", nil)
			switch phase {
			case "unchanged":
				recoveryNativeReceipt(t, w, x, aa.NoEffect)
			case "revoke after encoding", "suspend after encoding":
				recoveryNativeDenied(t, w, 401, x)
			default:
				recoveryNativeDenied(t, w, 503, x)
			}
			if phase == "already canceled request" {
				if s.calls != 0 {
					t.Fatal("pre-cancel reached final response")
				}
			} else {
				if s.calls != 1 {
					t.Fatal("native final guard not called once", s.calls)
				}
			}
			var state string
			if e := b.pool.QueryRow(b.ctx, `SELECT state FROM agent_action_dispatches WHERE id=$1`, d.ID).Scan(&state); e != nil {
				t.Fatal(e)
			}
			want := aa.NoEffect
			if phase == "already canceled request" {
				want = aa.Committed
			}
			if state != want {
				t.Fatal("reply denial pretended undo or made effect", state, want)
			}
			if before != recoveryNativeOtherEffects(t, x) {
				t.Fatal("reply retirement changed unrelated effects")
			}
		})
	}
}
func TestSandboxApprovalRecoveryNativeExactDispatchWaitCancelAndCurrentClock(t *testing.T) {
	for _, phase := range []string{"cancel while original row waits", "session expires during row wait"} {
		t.Run(phase, func(t *testing.T) {
			x := actionNativeFixture(t)
			b := x.f.f.native.private.base
			actionApprove(t, x)
			d, _ := actionCommitNative(t, x)
			token, sid := recoveryNativeSession(t, x, b.person.ID)
			if phase == "session expires during row wait" {
				// Original HTTP authentication renews idle only. Bound the actual
				// absolute session expiry too, so that legitimate renewal cannot
				// turn this current-clock fixture into a never-expiring session.
				b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET expires_at=n.at+interval '1500 milliseconds',idle_expires_at=n.at+interval '1500 milliseconds' FROM n WHERE id=$1`, sid)
			}
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			var holder int
			if e = lock.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&holder); e != nil {
				t.Fatal(e)
			}
			var held string
			if e = lock.QueryRow(b.ctx, `SELECT id FROM agent_action_dispatches WHERE id=$1 FOR UPDATE`, d.ID).Scan(&held); e != nil {
				t.Fatal(e)
			}
			name := "rec038-" + d.ID
			s := New(recoveryNativePool(t, x, name, nil), false)
			ctx, cancel := context.WithTimeout(b.ctx, 6*time.Second)
			defer cancel()
			done := make(chan *httptest.ResponseRecorder, 1)
			before := recoveryNativeRows(t, x)
			other := recoveryNativeOtherEffects(t, x)
			go func() {
				done <- recoveryNativeCall(t, recoveryNativeHandler(s, x, nil, nil), ctx, token, x.p.Binding.ApprovalID, "", nil)
			}()
			end := time.Now().Add(3 * time.Second)
			seen := false
			for time.Now().Before(end) {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE application_name=$1 AND wait_event_type='Lock' AND $2=ANY(pg_blocking_pids(a.pid)) AND query LIKE '%FROM agent_action_dispatches WHERE id=$1 AND owner_id=$2 FOR UPDATE%' AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND NOT l.granted))`, name, holder).Scan(&seen); e != nil {
					t.Fatal(e)
				}
				if seen {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !seen {
				t.Fatal("specific original dispatch row/blocker wait not observed")
			}
			if phase == "cancel while original row waits" {
				cancel()
			} else {
				expired := false
				end = time.Now().Add(3 * time.Second)
				for time.Now().Before(end) {
					if e = b.pool.QueryRow(b.ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE id=$1`, sid).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !expired {
					t.Fatal("actual PG current session expiry not reached")
				}
			}
			if e = lock.Rollback(b.ctx); e != nil {
				t.Fatal(e)
			}
			var w *httptest.ResponseRecorder
			select {
			case w = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("bounded real wait did not finish")
			}
			code := 503
			if phase == "session expires during row wait" {
				code = 403
			}
			recoveryNativeDenied(t, w, code, x)
			if before != recoveryNativeRows(t, x) || other != recoveryNativeOtherEffects(t, x) {
				t.Fatal("canceled/expired wait committed a fence or unrelated effect")
			}
			fresh, _ := recoveryNativeSession(t, x, b.person.ID)
			recoveryNativeReceipt(t, recoveryNativeCall(t, recoveryNativeHandler(New(b.pool, false), x, nil, nil), b.ctx, fresh, x.p.Binding.ApprovalID, "", nil), x, aa.NoEffect)
			if dc, wc := actionRows(t, x); dc != 1 || wc != 0 {
				t.Fatal(dc, wc)
			}
			t.Log("exact tagged registered request waited on original dispatch row and actual blocker; denial rolled native recovery back; new explicit current read closed original fence")
		})
	}
}
