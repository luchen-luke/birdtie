package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/audittrace"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/organizationannouncement"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func auditNativeCount(t *testing.T, p *pgxpool.Pool, actor, kind, action, request string) int {
	t.Helper()
	var n int
	if e := p.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type=$2 AND action=$3 AND request_id IS NOT DISTINCT FROM NULLIF($4,'')`, actor, kind, action, request).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestAuditCorrelationNativeOrganizationMapSchemaHistory(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "087_organization_map_audit_correlation.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "087_organization_map_audit_correlation.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(down)); e != nil {
		t.Fatal("unused point correlation down", e)
	}
	var org, actor string
	if e = pool.QueryRow(ctx, `SELECT o.id,a.id FROM organizations o CROSS JOIN accounts a WHERE a.account_type='person' ORDER BY o.id,a.id LIMIT 1`).Scan(&org, &actor); e != nil {
		t.Fatal(e)
	}
	var id, old string
	if e = pool.QueryRow(ctx, `INSERT INTO organization_map_location_audit(organization_id,actor_account_id,action,revision) VALUES($1,$2,'submit',1) RETURNING id::text,to_jsonb(organization_map_location_audit)::text`, org, actor).Scan(&id, &old); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(up)); e != nil {
		t.Fatal(e)
	}
	var current string
	var unknown bool
	if e = pool.QueryRow(ctx, `SELECT (to_jsonb(a)-'request_id')::text,request_id IS NULL FROM organization_map_location_audit a WHERE id=$1`, id).Scan(&current, &unknown); e != nil || current != old || !unknown {
		t.Fatal("historical point audit fabricated or changed", e)
	}
	for _, bad := range []string{"short", "unsafe\nnewline", strings.Repeat("a", 65), "请求编号不允许"} {
		if _, e = pool.Exec(ctx, `INSERT INTO organization_map_location_audit(organization_id,actor_account_id,action,revision,request_id) VALUES($1,$2,'hide',2,$3)`, org, actor, bad); e == nil {
			t.Fatal("dirty point request accepted")
		}
	}
	if _, e = pool.Exec(ctx, `INSERT INTO organization_map_location_audit(organization_id,actor_account_id,action,revision,request_id) VALUES($1,$2,'hide',2,'map_used_history')`, org, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(down)); e == nil {
		t.Fatal("used point audit correlation erased")
	}
	// Clear an aborted migration transaction if this pooled connection retained
	// it; ROLLBACK is harmless if the pool already discarded that connection.
	if _, e = pool.Exec(ctx, `ROLLBACK`); e != nil {
		t.Fatal(e)
	}
	var guards int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='audit_events'::regclass AND tgenabled='O' AND tgname IN('community_interest_audit_guard','community_interest_audit_truncate_guard','participation_disclosure_audit_guard','participation_disclosure_audit_truncate_guard')`).Scan(&guards); e != nil || guards != 4 {
		t.Fatal("original 077/078 lineage guards changed", guards, e)
	}
}

func TestAuditCorrelationNativeOriginalWritesAndNoop(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	const req = "audit_original_writes"
	ctx := audittrace.WithRequestID(b.ctx, req)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM activity_participations WHERE participant_account_id=$1`, `DELETE FROM activity_plans WHERE owner_account_id=$1`, `DELETE FROM saved_items WHERE owner_account_id=$1`, `DELETE FROM connection_requests WHERE sender_account_id=$1 OR recipient_account_id=$1`} {
			if _, e := b.pool.Exec(context.Background(), q, b.person.ID); e != nil {
				t.Error(e)
			}
		}
	})
	p, fresh, e := b.store.JoinActivity(ctx, b.person.ID, f.public)
	if e != nil || !fresh {
		t.Fatal(e)
	}
	again, fresh, e := b.store.JoinActivity(ctx, b.person.ID, f.public)
	if e != nil || fresh || again.ID != p.ID {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "activity_participation", "join", req) != 1 {
		t.Fatal("RSVP retry duplicated audit")
	}
	if _, e = b.store.CancelParticipation(ctx, b.person.ID, f.public); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.CancelParticipation(ctx, b.person.ID, f.public); e != nil {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "activity_participation", "cancel", req) != 1 {
		t.Fatal("cancel retry duplicated audit")
	}
	plan, e := b.store.PlanActivity(ctx, b.person.ID, f.public)
	if e != nil {
		t.Fatal(e)
	}
	againID, e := b.store.PlanActivity(ctx, b.person.ID, f.public)
	if e != nil || againID != plan {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "activity_plan", "create", req) != 1 {
		t.Fatal("reminder repeat duplicated audit")
	}
	if e = b.store.RemoveActivityPlan(ctx, b.person.ID, plan); e != nil {
		t.Fatal(e)
	}
	saved, e := b.store.Save(ctx, b.person.ID, "place", f.place.place)
	if e != nil {
		t.Fatal(e)
	}
	againID, e = b.store.Save(ctx, b.person.ID, "place", f.place.place)
	if e != nil || againID != saved {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "saved_item", "create", req) != 1 {
		t.Fatal("bookmark repeat duplicated audit")
	}
	if e = b.store.RemoveSaved(ctx, b.person.ID, saved); e != nil {
		t.Fatal(e)
	}
	original, e := b.store.CreateFriendRequest(ctx, b.person.ID, b.other.ID, "PRIVATE_NOTE_NOT_IN_AUDIT")
	if e != nil {
		t.Fatal(e)
	}
	if original.ID == "" {
		t.Fatal("original request ID missing")
	}
	if e = b.store.BlockAccount(ctx, b.person.ID, b.other.ID); e != nil {
		t.Fatal(e)
	}
	if e = b.store.BlockAccount(ctx, b.person.ID, b.other.ID); e != nil {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "account", "block", req) != 1 {
		t.Fatal("repeat block pretended another effect")
	}
	var trace string
	if e = b.pool.QueryRow(ctx, `SELECT request_id FROM audit_events WHERE actor_account_id=$1 AND resource_type='connection_request' AND resource_id=$2 AND action='request'`, b.person.ID, original.ID).Scan(&trace); e != nil || trace != req {
		t.Fatal("original connection trace", e)
	}
}

func TestAuditCorrelationNativePrivateProfilePolicyMemoryAndGrant(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	const req = "audit_private_permissions"
	ctx := audittrace.WithRequestID(b.ctx, req)
	current, e := b.store.ReadOwnAgentPrivateProfile(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	r, e := b.store.ReplaceOwnAgentPrivateProfile(ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: current.Profile.ProfileVersion, Fields: privateProfileCanaries()})
	if e != nil {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "agent_private_profile", "replace", req) != 1 {
		t.Fatal("private profile audit absent")
	}
	if _, e = b.store.ReplaceOwnAgentPrivateProfile(ctx, f.peer, agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: privateProfileCanaries()}); !errors.Is(e, agentprofile.ErrConflict) {
		t.Fatal("crossowner version boundary", e)
	}
	if _, e = b.store.PutOwnPolicy(ctx, f.owner, agentpolicysettings.Autonomy, policyNativeInput(agentpolicysettings.Autonomy, 0, time.Now().Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	id := agentMemoryID(t, f)
	input := agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "audit.explicit", Summary: "PRIVATE_MEMORY_CANARY", StructuredValue: json.RawMessage(`{"private":"body"}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
	m, e := b.store.PutOwnMemory(ctx, f.owner, id, input)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PutOwnMemory(ctx, f.owner, id, input); e != nil {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "agent_memory", "put", req) != 1 {
		t.Fatal("memory retry duplicated audit")
	}
	if _, e = b.store.DeleteOwnMemory(ctx, f.owner, id, m.Version); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.DeleteOwnMemory(ctx, f.owner, id, m.Version); e != nil {
		t.Fatal(e)
	}
	if auditNativeCount(t, b.pool, b.person.ID, "agent_memory", "delete", req) != 1 {
		t.Fatal("delete retry duplicated audit")
	}
	g, e := b.store.GrantProfileRead(ctx, b.person.ID, b.other.ID, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if e = b.store.RevokeProfileGrant(ctx, b.person.ID, g.ID); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_id=$1::uuid::text AND target_resource_id=$2 AND request_id=$3 AND action IN('grant','revoke')`, b.person.ID, g.ID, req).Scan(&n); e != nil || n != 2 {
		t.Fatal("original owner resource + exact concrete grant", n, e)
	}
	var body string
	if e = b.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a))::text FROM audit_events a WHERE actor_account_id=$1 AND request_id=$2`, b.person.ID, req).Scan(&body); e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"合成私密", "PRIVATE_MEMORY_CANARY", "structured_value", "fields", "SessionDigest"} {
		if strings.Contains(body, secret) {
			t.Fatal("private payload logged", secret)
		}
	}
}

func TestAuditCorrelationNativeAuditWaitExpiryRollsBack(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	const req = "audit_wait_expiry"
	id := agentMemoryID(t, f)
	b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET expires_at=n.at+interval '900 milliseconds',idle_expires_at=n.at+interval '700 milliseconds' FROM n WHERE id=$1`, f.ownerSession)
	hold, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(context.Background())
	if _, e = hold.Exec(b.ctx, `LOCK TABLE audit_events IN SHARE MODE`); e != nil {
		t.Fatal(e)
	}
	cfg := b.pool.Config().Copy()
	name := "audit-wait-" + id
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	done := make(chan error, 1)
	go func() {
		_, e := New(pool, false).PutOwnMemory(audittrace.WithRequestID(b.ctx, req), f.owner, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "audit.wait", Summary: "PRIVATE_WAIT", StructuredValue: json.RawMessage(`{}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().Add(time.Hour)})
		done <- e
	}()
	waiting := false
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND $2::int=ANY(pg_blocking_pids(pid)))`, name, int(hold.Conn().PgConn().PID())).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("audit writer did not actually block")
	}
	for {
		var expired bool
		if e = b.pool.QueryRow(b.ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE id=$1`, f.ownerSession).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e = hold.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, agentmemory.ErrForbidden) {
			t.Fatal("late audit must retain current session denial", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer hung")
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_memories WHERE id=$1)+(SELECT count(*) FROM audit_events WHERE request_id=$2)`, id, req).Scan(&count); e != nil || count != 0 {
		t.Fatal("rollback left memory or success audit", count, e)
	}
}

func TestAuditCorrelationNativeBusinessAndBackgroundNull(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	ctx := audittrace.WithRequestID(f.ctx, "audit_business_claim")
	in := businessconsole.ClaimInput{Name: "合成审计商家", Description: "PRIVATE_DESCRIPTION", SourceURL: "https://example.invalid/PRIVATE_SOURCE", RightsNote: "PRIVATE_RIGHTS"}
	c, e := f.store.SubmitBusinessClaim(ctx, f.who(0, id), in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.SubmitBusinessClaim(ctx, f.who(0, id), in); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1 AND actor_account_id=$2 AND request_id='audit_business_claim' AND resource_version=$3`, id, f.people[0], c.Version).Scan(&n); e != nil || n != 1 {
		t.Fatal("business original claim retry", n, e)
	}
	f.grant(t, id)
	if _, e = f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: c.Version, Decision: "reject", Note: "local synthetic"}); e != nil {
		t.Fatal(e)
	}
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1 AND action='claim_reject' AND request_id IS NULL`, id).Scan(&n); e != nil || n != 1 {
		t.Fatal("background inherited old trace", n, e)
	}
}

func TestAuditCorrelationNativeMigrationRoundtripAndHistoryGuard(t *testing.T) {
	ownedMigrationDatabase(t)
	pool, e := pgxpool.New(context.Background(), os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	ctx := context.Background()
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "086_social_agent_audit_correlation.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "086_social_agent_audit_correlation.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(down)); e != nil {
		t.Fatal("unused down", e)
	}
	var id string
	if e = pool.QueryRow(ctx, `INSERT INTO audit_events(action,resource_type,resource_id,decision,purpose) VALUES('legacy','legacy','unknown','allowed','legacy') RETURNING id::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(up)); e != nil {
		t.Fatal(e)
	}
	var unknown bool
	if e = pool.QueryRow(ctx, `SELECT request_id IS NULL AND target_resource_id IS NULL FROM audit_events WHERE id=$1::bigint`, id).Scan(&unknown); e != nil || !unknown {
		t.Fatal("historical trace fabricated", e)
	}
	for _, bad := range []string{"short", "unsafe\nnewline", strings.Repeat("a", 65), "请求编号不允许"} {
		if _, e = pool.Exec(ctx, `INSERT INTO audit_events(action,resource_type,resource_id,decision,purpose,request_id) VALUES('test','test','test','allowed','test',$1)`, bad); e == nil {
			t.Fatal("SQL dirty request accepted")
		}
	}
	if _, e = pool.Exec(ctx, `INSERT INTO audit_events(action,resource_type,resource_id,decision,purpose,request_id) VALUES('test','test','test','allowed','test','audit_used_history')`); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(down)); e == nil {
		t.Fatal("used trace history downgraded")
	}
	var guard int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='audit_events'::regclass AND tgenabled='O' AND tgname IN('community_interest_audit_guard','community_interest_audit_truncate_guard','participation_disclosure_audit_guard','participation_disclosure_audit_truncate_guard')`).Scan(&guard); e != nil || guard != 4 {
		t.Fatal("original lineage guards changed", guard, e)
	}
}

func TestAuditCorrelationNativeIndependentAuditSystems(t *testing.T) {
	t.Run("announcementAndAdmin", func(t *testing.T) {
		f, id, in := announcementFixture(t)
		b := f.p.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_announcement_publish")
		r, e := b.store.PutOrganizationAnnouncement(ctx, f.access, id, in)
		if e != nil {
			t.Fatal(e)
		}
		p, e := b.store.PreviewOrganizationAnnouncement(ctx, f.access, id, r.Revision)
		if e != nil {
			t.Fatal(e)
		}
		_, e = b.store.PublishOrganizationAnnouncement(ctx, f.access, id, organizationannouncement.PublishInput{ExpectedRevision: r.Revision, PreviewID: p.ID})
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM organization_announcement_audit WHERE announcement_id=$1 AND request_id='audit_announcement_publish'`, id).Scan(&n); e != nil || n != 2 {
			t.Fatal("announcement original versions", n, e)
		}
		m, e := b.store.InviteMember(ctx, b.person.ID, f.org.ID, b.other.ID, "member")
		if e != nil {
			t.Fatal(e)
		}
		if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit_events WHERE organization_id=$1 AND actor_account_id=$2 AND resource_id=$3 AND request_id='audit_announcement_publish'`, f.org.ID, b.person.ID, m.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("admin native target", n, e)
		}
	})
	t.Run("businessPublicationPermission", func(t *testing.T) {
		f, id, in := supplierNativeFixture(t)
		ctx := audittrace.WithRequestID(f.ctx, "audit_business_permission")
		first, e := f.store.ChangeBusinessPublicPermission(ctx, f.who(0, id), in)
		if e != nil {
			t.Fatal(e)
		}
		again, e := f.store.ChangeBusinessPublicPermission(ctx, f.who(0, id), in)
		if e != nil || again.Version != first.Version {
			t.Fatal(e)
		}
		var n int
		if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM business_public_profile_audit WHERE business_id=$1 AND request_id='audit_business_permission'`, id).Scan(&n); e != nil || n != 1 {
			t.Fatal("public permission retry", n, e)
		}
	})
	t.Run("modelBudgetHumanReviewOnly", func(t *testing.T) {
		f := newEgressFixture(t, 10)
		b := f.f.native.private.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_model_budget_review")
		p, e := b.store.PreviewOwnModelEgress(ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, PriceVersion: f.price.Version, MaxOutputTokens: 64, DeadlineAt: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)})
		if e != nil {
			t.Fatal(e)
		}
		if e = b.store.ApproveOwnModelEgress(ctx, f.f.native.access, p.ID, p.RequestDigest); e != nil {
			t.Fatal(e)
		}
		var n int
		if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM model_budget_audit WHERE owner_id=$1 AND root_trace_id=$2 AND request_id='audit_model_budget_review'`, b.person.ID, f.root).Scan(&n); e != nil || n < 1 {
			t.Fatal("original model review audit", n, e)
		}
	})
	t.Run("runHumanAndBackground", func(t *testing.T) {
		f, s, a := runFixture(t, false)
		b := f.f.f.place.private.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_run_human_review")
		r, e := s.ScheduleOwn(ctx, a, agentrun.Input{MomentID: f.f.moment.ID})
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 3; i++ {
			if _, e = s.ScheduleOwn(ctx, a, agentrun.Input{MomentID: f.f.moment.ID}); e != nil {
				t.Fatal(e)
			}
		}
		var n int
		if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM agent_run_audit WHERE run_id=$1 AND request_id='audit_run_human_review'`, r.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("run repeat duplicated version", n, e)
		}
		if _, e = s.CancelOwn(b.ctx, a, r.ID, r.Version); e != nil {
			t.Fatal(e)
		}
		if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM agent_run_audit WHERE run_id=$1 AND request_id IS NULL`, r.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("background inherited request", n, e)
		}
	})
	t.Run("socialDisclosureNoop", func(t *testing.T) {
		f := agentPrivateTestFixture(t)
		b := f.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_social_disclosure")
		in := socialcontext.Disclosure{SharedCommunities: true}
		if _, e := b.store.SetSocialDisclosure(ctx, b.person.ID, in); e != nil {
			t.Fatal(e)
		}
		if _, e := b.store.SetSocialDisclosure(ctx, b.person.ID, in); e != nil {
			t.Fatal(e)
		}
		if auditNativeCount(t, b.pool, b.person.ID, "social_disclosure", "update", "audit_social_disclosure") != 1 {
			t.Fatal("unchanged disclosure fabricated success")
		}
	})
}

func TestAuditCorrelationNativeThreePurposeConcreteTargets(t *testing.T) {
	check := func(t *testing.T, p *pgxpool.Pool, ctx context.Context, owner, id, request string) {
		t.Helper()
		var n int
		if e := p.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND target_resource_id=$2 AND request_id=$3 AND action IN('grant','revoke')`, owner, id, request).Scan(&n); e != nil || n != 2 {
			t.Fatal("concrete original grant audit", n, e)
		}
	}
	t.Run("taskContext", func(t *testing.T) {
		f := contextPurposeNative(t)
		b := f.f.place.private.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_task_context_purpose")
		p, e := b.store.PreviewOwnContextPurpose(ctx, f.f.place.private.owner, f.selection)
		if e != nil {
			t.Fatal(e)
		}
		g, e := b.store.ApproveOwnContextPurpose(ctx, f.f.place.private.owner, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.RevokeOwnContextPurpose(ctx, f.f.place.private.owner, g.ID, g.Revision); e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.RevokeOwnContextPurpose(ctx, f.f.place.private.owner, g.ID, g.Revision); e != nil {
			t.Fatal(e)
		}
		check(t, b.pool, ctx, b.person.ID, g.ID, "audit_task_context_purpose")
	})
	t.Run("momentAnalysis", func(t *testing.T) {
		f := enrichmentPurposeNative(t)
		b := f.f.place.private.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_moment_analysis")
		p, e := b.store.PreviewOwnEnrichmentPurpose(ctx, f.f.place.private.owner, f.selection)
		if e != nil {
			t.Fatal(e)
		}
		g, e := b.store.ApproveOwnEnrichmentPurpose(ctx, f.f.place.private.owner, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.RevokeOwnEnrichmentPurpose(ctx, f.f.place.private.owner, g.ID, 1); e != nil {
			t.Fatal(e)
		}
		check(t, b.pool, ctx, b.person.ID, g.ID, "audit_moment_analysis")
	})
	t.Run("candidateRetention", func(t *testing.T) {
		f := retentionNative(t)
		b := f.f.f.place.private.base
		a := f.f.f.place.private.owner
		ctx := audittrace.WithRequestID(b.ctx, "audit_candidate_retention")
		p, e := b.store.PreviewOwnCandidateRetention(ctx, a, f.selection)
		if e != nil {
			t.Fatal(e)
		}
		g, e := b.store.ApproveOwnCandidateRetention(ctx, a, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.RevokeOwnCandidateRetention(ctx, a, g.ID, 1); e != nil {
			t.Fatal(e)
		}
		check(t, b.pool, ctx, b.person.ID, g.ID, "audit_candidate_retention")
	})
}

func TestAuditCorrelationNativeConversionAndProtectedDisclosures(t *testing.T) {
	f := conversionNative(t)
	b := f.f.f
	ctx := audittrace.WithRequestID(b.ctx, "audit_explicit_conversion")
	p := f.preview(t)
	if _, e := f.g.ApproveOwn(ctx, f.a, f.intent.ID, p.PreviewID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.g.ApproveOwn(ctx, f.a, f.intent.ID, p.PreviewID); e != nil {
		t.Fatal(e)
	}
	var n int
	if e := b.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_id=$2 AND target_resource_id=$3 AND request_id='audit_explicit_conversion' AND action='convert'`, f.a.Actor.ID, f.intent.ID, f.f.p).Scan(&n); e != nil || n != 1 {
		t.Fatal("conversion original participation target/noop", n, e)
	}
	// Public disclosure remains a distinct human approval, never inferred by conversion.
	pub := f.f.preview(t, "PUBLIC")
	if _, e := b.s.ApproveOwnParticipationDisclosure(ctx, b.a, pub.Preview); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND request_id='audit_explicit_conversion' AND resource_type='activity_participation_disclosure'`, b.a.WorkspacePrincipal.ID).Scan(&n); e != nil || n != 1 {
		t.Fatal("078 original audit lost", n, e)
	}
}

func TestAuditCorrelationNativeOriginalAgentTaskAndCommunityDeclaration(t *testing.T) {
	t.Run("task", func(t *testing.T) {
		f := contextBuilderNative(t)
		b := f.place.private.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_current_agent_task")
		original := f.task
		original.Query = "PRIVATE_TASK_QUERY_NOT_LOGGED"
		r, e := b.store.SaveTask(ctx, original)
		if e != nil {
			t.Fatal(e)
		}
		r.Status = agentworkspace.TaskCompleted
		r, e = b.store.UpdateTask(ctx, r)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.UpdateTask(ctx, r); e != nil {
			t.Fatal(e)
		}
		var n int
		var body string
		if e = b.pool.QueryRow(ctx, `SELECT count(*),coalesce(jsonb_agg(to_jsonb(a))::text,'') FROM audit_events a WHERE resource_type='agent_task' AND resource_id=$1 AND actor_account_id=$2 AND request_id='audit_current_agent_task'`, r.ID, b.person.ID).Scan(&n, &body); e != nil || n != 2 {
			t.Fatal("Task original create/update/no-op", n, e)
		}
		if strings.Contains(body, original.Query) {
			t.Fatal("query logged")
		}
	})
	t.Run("communityExplicitDeclaration", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := interestNative(t)
		ctx := audittrace.WithRequestID(f.ctx, "audit_community_declaration")
		p := f.preview("PRIVATE")
		if _, e := f.s.ApproveOwnCommunityInterest(ctx, f.a, p.Preview); e != nil {
			t.Fatal(e)
		}
		var n int
		if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND request_id='audit_community_declaration' AND resource_type='person_community_declaration'`, f.a.WorkspacePrincipal.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("077 original audit", n, e)
		}
	})
}

func TestAuditCorrelationNativeAdditionalPermissionAndCandidateProducers(t *testing.T) {
	t.Run("relationshipAndNewPeople", func(t *testing.T) {
		f := agentPrivateTestFixture(t)
		b := f.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_explicit_social_permissions")
		for i := 0; i < 2; i++ {
			if _, e := b.store.SetRelationshipConsent(ctx, b.person.ID, relationshipcontext.Consent{Enabled: true}); e != nil {
				t.Fatal(e)
			}
			if _, e := b.store.SetNewPeopleConsent(ctx, b.person.ID, true); e != nil {
				t.Fatal(e)
			}
		}
		for _, kind := range []string{"agent_relationship_consent", "new_people_consent"} {
			if auditNativeCount(t, b.pool, b.person.ID, kind, "update", "audit_explicit_social_permissions") != 1 {
				t.Fatal("permission repeat fabricated success", kind)
			}
		}
	})
	t.Run("organizationMemory", func(t *testing.T) {
		f := newOrgMemoryFixture(t)
		b := f.p.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_org_manual_memory")
		id := agentMemoryID(t, f.p)
		in := orgMemoryTestInput(agentmemory.OrgPolicy, "audit")
		if _, e := b.store.PutOrganizationMemory(ctx, f.access, id, in); e != nil {
			t.Fatal(e)
		}
		if _, e := b.store.PutOrganizationMemory(ctx, f.access, id, in); e != nil {
			t.Fatal(e)
		}
		var n int
		if e := b.pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit_events WHERE organization_id=$1 AND resource_type='organization_memory' AND resource_id=$2 AND request_id='audit_org_manual_memory'`, f.org.ID, id).Scan(&n); e != nil || n != 1 {
			t.Fatal("manual org Memory original audit", n, e)
		}
	})
	t.Run("candidateHumanPromotion", func(t *testing.T) {
		f, s := memoryCandidateFixture(t)
		b := f.base.private.base
		ctx := audittrace.WithRequestID(b.ctx, "audit_candidate_human_approval")
		r := memoryCandidateSave(t, f, s)
		p := memoryCandidatePreview(t, f, s, r)
		if _, e := s.AcceptOwnCandidate(ctx, f.base.private.owner, p); e != nil {
			t.Fatal(e)
		}
		if _, e := s.AcceptOwnCandidate(ctx, f.base.private.owner, p); e != nil {
			t.Fatal(e)
		}
		if auditNativeCount(t, b.pool, b.person.ID, "memory_candidate", "approve", "audit_candidate_human_approval") != 1 {
			t.Fatal("candidate retry duplicated success")
		}
	})
}

func TestAuditCorrelationNativeMemoryDeadlineDuringAuditWaitRollsBack(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	before := agentMemoryOwnedSourceSnapshot(t, f)
	var deadline time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '900 milliseconds'`).Scan(&deadline); e != nil {
		t.Fatal(e)
	}
	hold, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(context.Background())
	if _, e = hold.Exec(b.ctx, `LOCK TABLE audit_events IN SHARE MODE`); e != nil {
		t.Fatal(e)
	}
	cfg := b.pool.Config().Copy()
	name := "audit-source-wait-" + id
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	done := make(chan error, 1)
	go func() {
		_, e := New(pool, false).PutOwnMemory(audittrace.WithRequestID(b.ctx, "audit_memory_deadline"), f.owner, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "audit.current.deadline", Summary: "PRIVATE_NO_LOG", StructuredValue: json.RawMessage(`{}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: deadline})
		done <- e
	}()
	observed := false
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		var waiting, expired bool
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid))),clock_timestamp()>=$3`, name, int(hold.Conn().PgConn().PID()), deadline).Scan(&waiting, &expired); e != nil {
			t.Fatal(e)
		}
		if waiting {
			observed = true
		}
		if observed && expired {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !observed {
		t.Fatal("actual audit wait absent")
	}
	if e = hold.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, agentmemory.ErrInvalid) {
			t.Fatal("expired input became committed success", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer hung")
	}
	if before != agentMemoryOwnedSourceSnapshot(t, f) {
		t.Fatal("expired audit wait left original source changes")
	}
	if auditNativeCount(t, b.pool, b.person.ID, "agent_memory", "put", "audit_memory_deadline") != 0 {
		t.Fatal("rollback left success audit")
	}
}
