package postgres

import (
	"context"
	"encoding/json"
	"errors"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func joinMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestAgentActivityJoinNativeMigrationRoundtripPreservesOriginal(t *testing.T) {
	x := joinNative(t)
	b := x.f.f.native.private.base
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	conn, e := b.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	snapshot := func() string {
		t.Helper()
		var s string
		if e := conn.QueryRow(ctx, `SELECT jsonb_build_object('accounts',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY a.id) FROM accounts a),'agents',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY a.id) FROM agents a),'profiles',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY a.agent_id) FROM agent_profiles a),'activities',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY a.id) FROM activities a),'consent',(SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('_xmin',a.xmin::text) ORDER BY a.id) FROM consent_grants a))::text`).Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	before := snapshot()
	if _, e = conn.Exec(ctx, joinMigrationSQL(t, "131_agent_activity_join_reply_source.down.sql")); e != nil {
		t.Fatal("actual unused131 down", e)
	}
	if snapshot() != before {
		t.Fatal("131 down changed original rows/xmin")
	}
	if _, e = conn.Exec(ctx, joinMigrationSQL(t, "130_agent_activity_join.down.sql")); e != nil {
		t.Fatal("actual empty130 down", e)
	}
	if snapshot() != before {
		t.Fatal("down altered original rows/xmin")
	}
	if _, e = conn.Exec(ctx, joinMigrationSQL(t, "130_agent_activity_join.sql")); e != nil {
		t.Fatal("actual130 reapply", e)
	}
	if snapshot() != before {
		t.Fatal("up changed existing principal/source/consent rows")
	}
	// A genuine 130-era ACTIVE approval is constructed under its actual old
	// guards solely to verify upgrade history; no 131 consumer source is forged.
	conn.Release()
	legacy := joinLegacy130Approval(t, x)
	conn, e = b.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var oldRow string
	if e = conn.QueryRow(ctx, `SELECT(to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text))::text FROM agent_action_approvals p WHERE id=$1`, legacy.ApprovalID).Scan(&oldRow); e != nil {
		t.Fatal(e)
	}
	originalRows := snapshot()
	if _, e = conn.Exec(ctx, joinMigrationSQL(t, "131_agent_activity_join_reply_source.sql")); e != nil {
		t.Fatal("actual131 upgrade", e)
	}
	if snapshot() != originalRows {
		t.Fatal("131 altered original identities/source/consent")
	}
	var afterRow string
	if e = conn.QueryRow(ctx, `SELECT(to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text))::text FROM agent_action_approvals p WHERE id=$1`, legacy.ApprovalID).Scan(&afterRow); e != nil || oldRow != afterRow {
		t.Fatal("legacy28 rewritten during131 upgrade", e)
	}
	conn.Release()
	if _, _, e = x.g.CommitOwn(ctx, x.a, legacy.ApprovalID, legacy.BindingDigest); e == nil {
		t.Fatal("legacy ACTIVE approval minted new effect")
	}
	if _, e = x.g.ConfirmOwn(ctx, x.a, legacy.ApprovalID, legacy.BindingDigest); e == nil {
		t.Fatal("legacy approval upgraded implicitly")
	}
	if _, e = x.g.ReconcileOwn(ctx, x.a, legacy.ApprovalID); !errors.Is(e, aa.ErrUnknown) {
		t.Fatal("legacy no-dispatch fabricated effect absence", e)
	}
	v, e := x.g.RevokeOwn(ctx, x.a, legacy.ApprovalID)
	if e != nil || v.State != aa.Cancelled || v.DispatchCommitted || v.Review != nil {
		t.Fatal("legacy history revoke blocked", e)
	}
	if e = x.g.ValidatePreviewOwn(ctx, x.a, v); e != nil {
		t.Fatal("legacy canceled final read", e)
	}
	conn, e = b.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Exec(ctx, joinMigrationSQL(t, "131_agent_activity_join_reply_source.down.sql")); e != nil {
		t.Fatal("legacy-only131 down refused", e)
	}
	var keys int
	if e = conn.QueryRow(ctx, `SELECT count(*) FROM agent_action_approvals p,LATERAL jsonb_object_keys(p.binding_canonical::jsonb) WHERE p.id=$1`, legacy.ApprovalID).Scan(&keys); e != nil || keys != 28 {
		t.Fatal("down changed legacy discriminator", e)
	}
	if _, e = conn.Exec(ctx, joinMigrationSQL(t, "131_agent_activity_join_reply_source.sql")); e != nil {
		t.Fatal("restore131 baseline", e)
	}
}

func joinLegacy130Approval(t *testing.T, x *joinNativeFixture) aa.ActivityJoinPreview {
	t.Helper()
	b := x.f.f.native.private.base
	b.exec(`UPDATE agent_tasks SET status='ACTIVE' WHERE id=$1`, x.input.TaskID)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	cur, e := lockModelConfigurationTask(b.ctx, tx, agentevent.Access{SessionDigest: x.a.SessionDigest}, x.input.TaskID, b.store.devPhoneEnabled)
	if e != nil {
		t.Fatal(e)
	}
	source, e := x.g.source(b.ctx, tx, x.a, x.input.ActivityID)
	if e != nil {
		t.Fatal(e)
	}
	policy, e := captureNativeToolPolicy(b.ctx, tx, agentPrivateBinding{cur.sessionID, cur.agent.Principal.ID, cur.agent.AgentID}, source.View.ValidUntil)
	if e != nil {
		t.Fatal(e)
	}
	id, e := actionUUID()
	if e != nil {
		t.Fatal(e)
	}
	grant, e := actionUUID()
	if e != nil {
		t.Fatal(e)
	}
	input := x.input
	input.ExpectedTaskUpdatedAt = ""
	input.ReplyTurnDigest = ""
	p := joinApproval{p: input, state: aa.Pending, b: aa.ActivityJoinBinding{Binding: aa.Binding{Schema: aa.ActivityJoinSchema, ApprovalID: id, TenantID: x.a.WorkspacePrincipal.ID, ActorID: x.a.WorkspacePrincipal.ID, SubjectType: "PERSON", SubjectID: x.a.WorkspacePrincipal.ID, AgentID: cur.agent.AgentID, SessionID: cur.sessionID, TaskID: input.TaskID, LogicalOperationID: input.LogicalOperationID, ActionID: input.ActionID, Tool: aa.ActivityJoinTool, ToolVersion: aa.ActivityJoinToolVersion, TargetID: input.ActivityID, PayloadDigest: aa.ActivityJoinPayloadDigest(input), SourceVersion: cur.version.Token, PolicyVersion: policy.token, ConsentPurpose: aa.ActivityJoinPurpose, GrantID: grant, GrantRevision: 1, MembershipVersion: "NONE_PERSON_ONLY"}, TargetVersion: source.Proof}}
	if e = tx.QueryRow(b.ctx, `SELECT t.xmin::text,birdtie_activity_join_authority($1,$2,$3),birdtie_activity_join_target_tail($4,$2),birdtie_activity_join_participant_version($4,$2),clock_timestamp(),least($5::timestamptz,s.expires_at,s.idle_expires_at,clock_timestamp()+interval '30 seconds') FROM agent_tasks t JOIN sessions s ON s.id=$1 WHERE t.id=$6`, cur.sessionID, p.b.ActorID, p.b.AgentID, input.ActivityID, policy.until, input.TaskID).Scan(&p.b.SourceGeneration, &p.b.AuthorityVersion, &p.b.TargetTailVersion, &p.b.ParticipationVersion, &p.b.ObservedAt, &p.b.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	p.b.ObservedAt = p.b.ObservedAt.UTC()
	p.b.ExpiresAt = p.b.ExpiresAt.UTC()
	raw, digest, e := aa.ActivityJoinCanonical(p.b, input)
	if e != nil {
		t.Fatal(e)
	}
	p.digest = digest
	payload, _ := json.Marshal(input)
	if _, e = tx.Exec(b.ctx, `INSERT INTO agent_action_approvals(id,owner_id,agent_id,task_id,session_id,grant_id,binding_canonical,binding_digest,payload_canonical,observed_at,expires_at,action_kind)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'ACTIVITY_JOIN')`, id, p.b.ActorID, p.b.AgentID, p.b.TaskID, p.b.SessionID, grant, raw, digest, string(payload), p.b.ObservedAt, p.b.ExpiresAt); e != nil {
		t.Fatal("actual130 legacy insert", e)
	}
	if e = tx.Commit(b.ctx); e != nil {
		t.Fatal("actual130 legacy commit", e)
	}
	return joinPreview(p)
}
func TestAgentActivityJoinNativeSQLGuardsUsedDown(t *testing.T) {
	x := joinNative(t)
	b := x.f.f.native.private.base
	v := x.preview(t)
	reject := func(q string, args ...any) {
		t.Helper()
		tx, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		_, e = tx.Exec(b.ctx, q, args...)
		if e == nil {
			e = tx.Commit(b.ctx)
		}
		if e == nil {
			t.Fatal("native closed JOIN SQL tamper accepted")
		}
	}
	reject(`UPDATE agent_action_approvals SET action_kind='SANDBOX' WHERE id=$1`, v.ApprovalID)
	reject(`UPDATE agent_action_approvals SET consumed_at=clock_timestamp() WHERE id=$1`, v.ApprovalID)
	reject(`UPDATE agent_action_approvals SET payload_canonical=replace(payload_canonical,'activityId','confirmed') WHERE id=$1`, v.ApprovalID)
	reject(`UPDATE agent_action_approvals SET binding_canonical=replace(binding_canonical,'LATEST_ACTIVITY_REPLY_V1','ACTIVE_OR_ANY_TASK') WHERE id=$1`, v.ApprovalID)
	for _, mode := range []string{"disable", "drop"} {
		var definition string
		if e := b.pool.QueryRow(b.ctx, `SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgrelid='agent_activity_join_effects'::regclass AND tgname='agent_activity_join_effect_atomic_guard'`).Scan(&definition); e != nil {
			t.Fatal(e)
		}
		change := `ALTER TABLE agent_activity_join_effects DISABLE TRIGGER agent_activity_join_effect_atomic_guard`
		if mode == "drop" {
			change = `DROP TRIGGER agent_activity_join_effect_atomic_guard ON agent_activity_join_effects`
		}
		if _, e := b.pool.Exec(b.ctx, change); e != nil {
			t.Fatal(e)
		}
		_, denied := x.g.PreviewOwn(b.ctx, x.a, x.input)
		restore := `ALTER TABLE agent_activity_join_effects ENABLE TRIGGER agent_activity_join_effect_atomic_guard`
		if mode == "drop" {
			restore = definition
		}
		if _, e := b.pool.Exec(b.ctx, restore); e != nil {
			t.Fatal(e)
		}
		if !errors.Is(denied, aa.ErrUnavailable) {
			t.Fatal("missing deferred final guard permitted native JOIN", mode, denied)
		}
	}
	x.confirm(t, v)
	reject(`UPDATE consent_grants SET purpose='OWN_SANDBOX_ACTION' WHERE id=(SELECT grant_id FROM agent_action_approvals WHERE id=$1)`, v.ApprovalID)
	r, h, e := x.g.CommitOwn(b.ctx, x.a, v.ApprovalID, v.BindingDigest)
	if e != nil || h == nil {
		t.Fatal(e)
	}
	reject(`UPDATE agent_action_dispatches SET state='SUCCEEDED',effect_id=gen_random_uuid(),applied_at=clock_timestamp() WHERE id=$1`, r.DispatchID)
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Exec(b.ctx, joinMigrationSQL(t, "130_agent_activity_join.down.sql")); e == nil {
		t.Fatal("used Agent JOIN history silently removed")
	}
	if _, e = conn.Exec(b.ctx, `ROLLBACK`); e != nil {
		t.Fatal(e)
	}
	var exists bool
	if e = conn.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_action_approvals WHERE id=$1 AND action_kind='ACTIVITY_JOIN')`, v.ApprovalID).Scan(&exists); e != nil || !exists {
		t.Fatal("refused down lost original approval", e)
	}
	if _, e = conn.Exec(b.ctx, joinMigrationSQL(t, "131_agent_activity_join_reply_source.down.sql")); e == nil {
		t.Fatal("used latest-reply history silently downgraded")
	}
	if _, e = conn.Exec(b.ctx, `ROLLBACK`); e != nil {
		t.Fatal(e)
	}
}
