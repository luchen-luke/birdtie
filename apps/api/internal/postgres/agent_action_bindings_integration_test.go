package postgres

import (
	"context"
	"encoding/json"
	"errors"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

type actionFixture struct {
	f *egressFixture
	p aa.Preview
}
type actionErrorTracer struct{ t *testing.T }

func (tr *actionErrorTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (tr *actionErrorTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	if d.Err != nil {
		var pe *pgconn.PgError
		if errors.As(d.Err, &pe) {
			tr.t.Logf("owned native SQL diagnostic code=%s message=%s", pe.Code, pe.Message)
		}
	}
}

func actionNativeFixture(t *testing.T) *actionFixture {
	return actionNativeFixtureLifetime(t, time.Hour)
}
func actionNativeFixtureLifetime(t *testing.T, lifetime time.Duration) *actionFixture {
	t.Helper()
	f := outputActivityFixture(t, 4)
	b := f.f.native.private.base
	if _, e := b.store.PutOwnPolicy(b.ctx, f.f.native.private.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(lifetime))); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Tracer = &actionErrorTracer{t}
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	old := b.store
	b.store = New(pool, false)
	t.Cleanup(func() { b.store = old; pool.Close() })
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		for _, q := range []string{`DELETE FROM agent_sandbox_writes WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_action_dispatches WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_action_approvals WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) AND purpose='OWN_SANDBOX_ACTION'`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error("own action fixture cleanup", e)
			}
		}
	})
	g, p := toolSandboxGoal(t, f)
	v, e := aa.NewService(b.store).Preview(b.ctx, f.f.native.access, g, p, f.gate)
	if e != nil || v.State != aa.Pending {
		t.Fatal("actual current native sandbox preview", e, v)
	}
	return &actionFixture{f, v}
}
func actionApprove(t *testing.T, x *actionFixture) aa.Preview {
	t.Helper()
	b := x.f.f.native.private.base
	p, e := aa.NewService(b.store).Confirm(b.ctx, x.f.f.native.access, x.p.Binding.ApprovalID, x.p.BindingDigest, x.f.gate)
	if e != nil || p.State != aa.Approved {
		t.Fatal("explicit native current owner approval", e, p)
	}
	return p
}
func actionCommitNative(t *testing.T, x *actionFixture) (aa.Dispatch, aa.Commitment) {
	t.Helper()
	b := x.f.f.native.private.base
	d, h, e := aa.NewService(b.store).Commit(b.ctx, x.f.f.native.access, x.p.Binding.ApprovalID, x.p.Proposal, x.f.gate)
	if e != nil || d.State != aa.Committed || h == nil {
		t.Fatal("native atomic dispatch commitment", e, d, h)
	}
	return d, h
}
func actionRows(t *testing.T, x *actionFixture) (int, int) {
	t.Helper()
	b := x.f.f.native.private.base
	var d, w int
	if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_action_dispatches WHERE owner_id=$1),(SELECT count(*) FROM agent_sandbox_writes WHERE owner_id=$1)`, b.person.ID).Scan(&d, &w); e != nil {
		t.Fatal(e)
	}
	return d, w
}
func actionWire(t *testing.T, name string, v any) {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	outputSaveWire(t, name, raw)
}
func TestAgentActionNativeWrongVersionIdentityAndABAReject(t *testing.T) {
	for _, kind := range []string{"body", "target", "operation", "action", "digest", "task-aba", "account-aba", "agent-aba", "profile-aba", "policy-aba", "session-aba", "peer", "org", "business", "revoked", "off"} {
		t.Run(kind, func(t *testing.T) {
			x := actionNativeFixture(t)
			f := x.f
			b := f.f.native.private.base
			actionApprove(t, x)
			a := f.f.native.access
			p := x.p.Proposal
			switch kind {
			case "body":
				p.Value = "另一版本"
			case "target":
				p.TargetID = b.other.ID
			case "operation":
				p.LogicalOperationID = egressID(t, f)
			case "action":
				p.ActionID = egressID(t, f)
			case "digest":
				x.p.BindingDigest = "changed"
				if _, e := b.store.ApproveOwnSandboxAction(b.ctx, a, x.p.Binding.ApprovalID, x.p.BindingDigest, f.gate); e == nil {
					t.Fatal("wrong exact digest accepted")
				}
				return
			case "task-aba":
				b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, f.f.native.task.ID)
			case "account-aba":
				b.exec(`UPDATE accounts SET status=status WHERE id=$1`, b.person.ID)
			case "agent-aba":
				b.exec(`UPDATE agents SET status=status WHERE id=$1`, b.personID)
			case "profile-aba":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
			case "policy-aba":
				b.exec(`WITH old AS(DELETE FROM agent_policy_settings WHERE agent_id=$1 AND family='AUTONOMY' RETURNING *) INSERT INTO agent_policy_settings(agent_id,owner_id,owner_type,family,schema_version,native_revision,settings,valid_from,expires_at,updated_at) SELECT agent_id,owner_id,owner_type,family,schema_version,native_revision,settings,valid_from,expires_at,updated_at FROM old`, b.personID)
			case "peer":
				a = agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
			case "session-aba":
				b.exec(`UPDATE sessions SET expires_at=expires_at WHERE token_sha256=$1`, a.SessionDigest[:])
			case "org":
				a = agentevent.Access{SessionDigest: f.f.native.private.org.SessionDigest}
			case "business":
				a = agentevent.Access{SessionDigest: f.f.native.private.biz.SessionDigest}
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			case "off":
				_ = f.gate.Disable("agent_enrichment")
			}
			if d, h, e := b.store.CommitOwnSandboxAction(b.ctx, a, x.p.Binding.ApprovalID, p, f.gate); e == nil || h != nil || d.ID != "" {
				t.Fatal("stale/foreign action committed", kind, e, d)
			}
			d, w := actionRows(t, x)
			if d != 0 || w != 0 {
				t.Fatal("denied action wrote effect")
			}
		})
	}
}

func TestAgentActionNativeForeignChangedInputIsAlwaysDenied(t *testing.T) {
	for _, who := range []string{"peer", "anonymous", "expired", "unknown-id"} {
		for _, variant := range []string{"approve-correct", "approve-wrong", "commit-correct", "commit-wrong"} {
			t.Run(who+"/"+variant, func(t *testing.T) {
				x := actionNativeFixture(t)
				f := x.f
				b := f.f.native.private.base
				a := agentevent.Access{}
				if who == "peer" {
					a = agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
				}
				id := x.p.Binding.ApprovalID
				if who == "expired" {
					a = f.f.native.access
					b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp()-interval '1 microsecond' AS expired_at)
						UPDATE sessions s SET expires_at=n.expired_at,idle_expires_at=n.expired_at FROM n WHERE s.token_sha256=$1`, a.SessionDigest[:])
				}
				if who == "unknown-id" {
					a = f.f.native.access
					id = egressID(t, f)
				}
				var e error
				if strings.HasPrefix(variant, "approve") {
					digest := x.p.BindingDigest
					if variant == "approve-wrong" {
						digest = strings.Repeat("0", 64)
					}
					_, e = b.store.ApproveOwnSandboxAction(b.ctx, a, id, digest, f.gate)
				} else {
					p := x.p.Proposal
					if variant == "commit-wrong" {
						p.Value = "猜测私人内容"
					}
					_, _, e = b.store.CommitOwnSandboxAction(b.ctx, a, id, p, f.gate)
				}
				if !errors.Is(e, aa.ErrDenied) {
					t.Fatal("foreign private equality oracle: body/digest changed classification before native owner check", e)
				}
			})
		}
	}
}

func TestAgentActionNativeCurrentOwnerChangedVersionStillRequiresNewConfirmation(t *testing.T) {
	for _, kind := range []string{"digest", "body"} {
		t.Run(kind, func(t *testing.T) {
			x := actionNativeFixture(t)
			f := x.f
			b := f.f.native.private.base
			var e error
			if kind == "digest" {
				_, e = b.store.ApproveOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, strings.Repeat("0", 64), f.gate)
			} else {
				p := x.p.Proposal
				p.Value = "本人编辑后新版本"
				_, _, e = b.store.CommitOwnSandboxAction(b.ctx, f.f.native.access, x.p.Binding.ApprovalID, p, f.gate)
			}
			if !errors.Is(e, aa.ErrChanged) {
				t.Fatal("current actual owner changed-version classification lost", e)
			}
			if d, w := actionRows(t, x); d != 0 || w != 0 {
				t.Fatal("edited version caused effect", d, w)
			}
		})
	}
}
