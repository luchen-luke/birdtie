package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	cg "github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type interestFixture struct {
	ctx                   context.Context
	pool                  *pgxpool.Pool
	s                     *Store
	a, other              agentprofile.PrivateAccess
	session, group, owner string
	t                     *testing.T
}

func interestNative(t *testing.T) *interestFixture {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(c)
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &interestFixture{ctx: ctx, pool: pool, s: New(pool, false), t: t}
	for n := 0; n < 2; n++ {
		var owner, agent, session string
		if e = f.row(`INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&owner); e != nil {
			t.Fatal(e)
		}
		if e = pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) RETURNING id`, owner).Scan(&agent); e != nil {
			t.Fatal(e)
		}
		_, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if e = pool.QueryRow(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour') RETURNING id`, owner, digest[:]).Scan(&session); e != nil {
			t.Fatal(e)
		}
		a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: owner}}
		if n == 0 {
			f.other = a
			f.owner = owner
		} else {
			f.a = a
			f.session = session
		}
	}
	g, e := f.s.CreateSocialCommunity(ctx, f.owner, community.SocialInput{Name: "本地明确兴趣社群", Summary: "非真实运营", Visibility: "public", JoinPolicy: "request"})
	if e != nil {
		t.Fatal(e)
	}
	f.group = g.ID
	return f
}
func (f *interestFixture) row(q string, args ...any) pgx.Row {
	return f.pool.QueryRow(f.ctx, q, args...)
}
func (f *interestFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f *interestFixture) preview(op string) cg.CommunityInterestPreview {
	f.t.Helper()
	p, e := f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: op})
	if e != nil {
		f.t.Fatal(op, e)
	}
	return p
}
func (f *interestFixture) approve(p cg.CommunityInterestPreview) cg.CommunityInterestView {
	f.t.Helper()
	v, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview)
	if e != nil {
		f.t.Fatal(p.Operation, e)
	}
	return v
}
func (f *interestFixture) snapshot() string {
	var raw string
	if e := f.row(`SELECT jsonb_build_object('contexts',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY c.id),'[]') FROM contexts c WHERE c.community_id=$1),'statements',(SELECT coalesce(jsonb_agg(to_jsonb(pc) ORDER BY pc.context_id),'[]') FROM person_contexts pc WHERE pc.person_account_id=$2),'audits',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM audit_events a WHERE actor_account_id=$2))::text`, f.group, f.a.WorkspacePrincipal.ID).Scan(&raw); e != nil {
		f.t.Fatal(e)
	}
	return raw
}

func TestCommunityInterestNativeTruncateGuard(t *testing.T) {
	ownedMigrationDatabase(t)
	f := interestNative(t)
	maintenanceSnapshot := func() string {
		var raw string
		if e := f.row(`SELECT jsonb_build_object('contexts',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY c.id),'[]') FROM contexts c),'statements',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.context_id,p.person_account_id),'[]') FROM person_contexts p),'audits',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM audit_events a),'admin_audits',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM admin_audit_events a),'observations',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY o.id),'[]') FROM agent_enrichment_observations o))::text`).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	t.Run("unprotected_legacy_history_explicit_fk_maintenance", func(t *testing.T) {
		f.exec(`INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'legacy_test','legacy_test','legacy_test','allowed','legacy_test')`, f.a.WorkspacePrincipal.ID)
		var observations int
		if e := f.row(`SELECT count(*) FROM agent_enrichment_observations`).Scan(&observations); e != nil || observations != 0 {
			t.Fatal("maintenance fixture must have no derived observations", e, observations)
		}
		before := maintenanceSnapshot()
		_, e := f.pool.Exec(f.ctx, `TRUNCATE TABLE audit_events`)
		var pgErr *pgconn.PgError
		if !errors.As(e, &pgErr) || pgErr.Code != "0A000" {
			t.Fatal("089 referencing audit FK must reject single-table TRUNCATE", e)
		}
		if maintenanceSnapshot() != before {
			t.Fatal("single-table FK refusal changed original or derived data")
		}
		// 089 narrows single-table SQL compatibility. Explicit dependencies,
		// rather than CASCADE, retain the original 077 history protection.
		f.exec(`TRUNCATE TABLE audit_events, agent_enrichment_observations`)
		var count int
		if e := f.row(`SELECT count(*) FROM audit_events`).Scan(&count); e != nil || count != 0 {
			t.Fatal(e, count)
		}
	})
	f.approve(f.preview("PUBLIC"))
	t.Run("statement_guard_disabled_or_missing_refuses_all_entries_zero_write", func(t *testing.T) {
		p := f.preview("PRIVATE")
		before := f.snapshot()
		f.exec(`ALTER TABLE audit_events DISABLE TRIGGER community_interest_audit_truncate_guard`)
		for _, op := range []string{"PUBLIC", "PRIVATE", "DELETE"} {
			if _, e := f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: op}); !errors.Is(e, cg.ErrInterestUnavailable) {
				t.Fatal("disabled statement guard preview", e)
			}
		}
		if _, e := f.s.ReadOwnCommunityInterests(f.ctx, f.a, false); !errors.Is(e, cg.ErrInterestUnavailable) {
			t.Fatal("disabled statement guard GET", e)
		}
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); !errors.Is(e, cg.ErrInterestUnavailable) {
			t.Fatal("disabled statement guard approve", e)
		}
		f.exec(`ALTER TABLE audit_events ENABLE TRIGGER community_interest_audit_truncate_guard`)
		f.exec(`DROP TRIGGER community_interest_audit_truncate_guard ON audit_events`)
		if _, e := f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: "PRIVATE"}); !errors.Is(e, cg.ErrInterestUnavailable) {
			t.Fatal("missing statement guard accepted", e)
		}
		f.exec(`CREATE TRIGGER community_interest_audit_truncate_guard BEFORE TRUNCATE ON audit_events FOR EACH STATEMENT EXECUTE FUNCTION birdtie_community_interest_audit_guard()`)
		if f.snapshot() != before {
			t.Fatal("invalid statement protection entries wrote")
		}
	})
	before := maintenanceSnapshot()
	_, e := f.pool.Exec(f.ctx, `TRUNCATE TABLE audit_events, agent_enrichment_observations`)
	var pgErr *pgconn.PgError
	if !errors.As(e, &pgErr) || pgErr.Code != "P0001" || pgErr.Message != "community interest audit history prevents truncate" {
		t.Fatal("explicit maintenance must reach the original 077 protected-epoch guard", e)
	}
	if maintenanceSnapshot() != before {
		t.Fatal("refused truncate changed original data")
	}
	old := f.preview("PRIVATE")
	f.approve(f.preview("PRIVATE"))
	f.approve(f.preview("PUBLIC"))
	if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, old.Preview); !errors.Is(e, cg.ErrInterestChanged) {
		t.Fatal("old preview revived after refusal and legal ABA", e)
	}
}
func TestCommunityInterestNativeLifecycleApprovalAuditAndMigration(t *testing.T) {
	ownedMigrationDatabase(t)
	f := interestNative(t)
	t.Run("empty_history_down_reapply_retains_original", func(t *testing.T) {
		before := f.snapshot()
		for _, name := range []string{"077_person_community_interest_audit_guard.down.sql", "077_person_community_interest_audit_guard.sql"} {
			raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
			if e != nil {
				t.Fatal(e)
			}
			f.exec(string(raw))
			if strings.HasSuffix(name, ".down.sql") {
				old := f.snapshot()
				_, e := f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: "PUBLIC"})
				if !errors.Is(e, cg.ErrInterestUnavailable) {
					t.Fatal("076 without guard was accepted", e)
				}
				if f.snapshot() != old {
					t.Fatal("missing077 wrote")
				}
			}

		}
		if f.snapshot() != before {
			t.Fatal("migration modified original sources")
		}
	})
	t.Run("disabled_native_guard_rejects_read_preview_approval_zero_write", func(t *testing.T) {
		p := f.preview("PUBLIC")
		before := f.snapshot()
		f.exec(`ALTER TABLE audit_events DISABLE TRIGGER community_interest_audit_guard`)
		if _, e := f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: "PUBLIC"}); !errors.Is(e, cg.ErrInterestUnavailable) {
			t.Fatal(e)
		}
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); !errors.Is(e, cg.ErrInterestUnavailable) {
			t.Fatal(e)
		}
		if _, e := f.s.ReadOwnCommunityInterests(f.ctx, f.a, true); !errors.Is(e, cg.ErrInterestUnavailable) {
			t.Fatal(e)
		}
		f.exec(`ALTER TABLE audit_events ENABLE TRIGGER community_interest_audit_guard`)
		if f.snapshot() != before {
			t.Fatal("disabled guard path wrote")
		}
	})

	t.Run("read_and_preview_zero_write_nonmember_private_default", func(t *testing.T) {
		before := f.snapshot()
		v, e := f.s.ReadOwnCommunityInterests(f.ctx, f.a, true)
		if e != nil || len(v.Options) == 0 {
			t.Fatal(e, v)
		}
		p := f.preview("PRIVATE")
		if p.State != "ABSENT" || p.TargetState != "PRIVATE" || p.MembershipGranted || p.ModelAccess || p.SendAllowed || p.ExpiresAt.Sub(p.ObservedAt) > 90*time.Second {
			t.Fatal(p)
		}
		if f.snapshot() != before {
			t.Fatal("GET/preview wrote")
		}
		v = f.approve(p)
		if v.Records[0].State != "PRIVATE" {
			t.Fatal(v)
		}
		if _, e = f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal("repeat preview", e)
		}
	})
	t.Run("specific_public_then_absence_legal_ABA_denies", func(t *testing.T) {
		f.approve(f.preview("DELETE"))
		old := f.preview("PUBLIC")
		pub := f.preview("PUBLIC")
		v := f.approve(pub)
		if v.Records[0].State != "PUBLIC" {
			t.Fatal(v)
		}
		if e := f.s.RemoveContextDeclaration(f.ctx, f.a.WorkspacePrincipal.ID, v.Records[0].ContextID, "interest"); !errors.Is(e, cg.ErrNotFound) {
			t.Fatal("old delete bypass", e)
		}
		f.approve(f.preview("DELETE"))
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, old.Preview); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal("absence ABA revived", e)
		}
		f.approve(f.preview("PUBLIC"))
	})
	t.Run("public_private_public_ABA_and_competing_approval", func(t *testing.T) {
		old := f.preview("PRIVATE")
		f.approve(f.preview("PRIVATE"))
		f.approve(f.preview("PUBLIC"))
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, old.Preview); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal(e)
		}
		p := f.preview("PRIVATE")
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for n := 0; n < 2; n++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); errs <- e }()
		}
		wg.Wait()
		close(errs)
		ok, bad := 0, 0
		for e := range errs {
			if e == nil {
				ok++
			} else if errors.Is(e, cg.ErrInterestChanged) {
				bad++
			} else {
				t.Fatal(e)
			}
		}
		if ok != 1 || bad != 1 {
			t.Fatal(ok, bad)
		}
	})
	t.Run("protected_audit_immutable_history_down_atomic_denial", func(t *testing.T) {
		before := f.snapshot()
		for _, q := range []string{`DELETE FROM audit_events WHERE resource_type='person_community_declaration'`, `UPDATE audit_events SET resource_type='other' WHERE resource_type='person_community_declaration'`, `UPDATE audit_events SET occurred_at=clock_timestamp() WHERE resource_type='person_community_declaration'`, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'PUBLIC','person_community_declaration','bad','allowed','HUMAN_COMMUNITY_INTEREST_DECLARATION')`} {
			args := []any{}
			if strings.Contains(q, "$1") {
				args = append(args, f.a.WorkspacePrincipal.ID)
			}
			if _, e := f.pool.Exec(f.ctx, q, args...); e == nil {
				t.Fatal("native guard accepted", q)
			}
		}
		if f.snapshot() != before {
			t.Fatal("protected audit changed")
		}
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", "077_person_community_interest_audit_guard.down.sql"))
		if e != nil {
			t.Fatal(e)
		}
		conn, e := f.pool.Acquire(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = conn.Exec(f.ctx, string(raw))
		if e == nil {
			t.Fatal("down erased audit guard")
		}
		conn.Exec(f.ctx, "ROLLBACK")
		conn.Release()
		if f.snapshot() != before {
			t.Fatal("refused down changed rows")
		}
		var active bool
		if e = f.row(`SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='community_interest_audit_guard')`).Scan(&active); e != nil || !active {
			t.Fatal(e)
		}
	})
	t.Run("no_membership_model_or_grants_and_opaque_authority", func(t *testing.T) {
		f.exec(`UPDATE communities SET rights_note='PRIVATE_RIGHTS_CANARY' WHERE id=$1`, f.group)
		p := f.preview("PUBLIC")
		inside, e := openInterest(p.Preview)
		if e != nil {
			t.Fatal(e)
		}
		sealedRaw, _ := json.Marshal(inside)
		if strings.Contains(string(sealedRaw), "PRIVATE_RIGHTS_CANARY") {
			t.Fatal("private rights loaded into sealed authority")
		}
		raw, _ := json.Marshal(p)
		for _, x := range []string{"Authority", "StatementToken", "SessionDigest", "xmin", "authentication_method"} {
			if strings.Contains(string(raw), x) {
				t.Fatal(x)
			}
		}
		var effects int
		if e := f.row(`SELECT (SELECT count(*) FROM community_memberships WHERE user_account_id=$1)+(SELECT count(*) FROM agent_memories WHERE owner_id=$1)+(SELECT count(*) FROM consent_grants WHERE owner_account_id=$1)+(SELECT count(*) FROM follows WHERE follower_account_id=$1)`, f.a.WorkspacePrincipal.ID).Scan(&effects); e != nil || effects != 0 {
			t.Fatal(e, effects)
		}
	})
	t.Run("two_explicit_human_writers_feed_current_introduction_with_separate_policies", func(t *testing.T) {
		for _, a := range []agentprofile.PrivateAccess{f.a, f.other} {
			f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'合成公开兴趣声明人','','public')`, a.WorkspacePrincipal.ID)
			if _, e := f.s.SetNewPeopleConsent(f.ctx, a.WorkspacePrincipal.ID, true); e != nil {
				t.Fatal(e)
			}
			if _, e := f.s.PutOwnPolicy(f.ctx, a, agentpolicysettings.Social, introductionPolicyInput(0, true, true, time.Now().UTC().Add(time.Hour))); e != nil {
				t.Fatal(e)
			}
			p, e := f.s.PreviewOwnCommunityInterest(f.ctx, a, cg.CommunityInterestInput{CommunityID: f.group, Operation: "PUBLIC"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ApproveOwnCommunityInterest(f.ctx, a, p.Preview); e != nil {
				t.Fatal(e)
			}
		}
		sources := []socialintent.Record{}
		for _, a := range []agentprofile.PrivateAccess{f.a, f.other} {
			d, e := f.s.CreateNewPeopleIntent(f.ctx, a.WorkspacePrincipal.ID, newpeople.DraftInput{Title: "本地真实领域公开声明", Category: "badminton", Modality: "ONLINE", ExpiresAt: time.Now().UTC().Add(time.Hour)})
			if e != nil {
				t.Fatal(e)
			}
			d, e = f.s.ActivateSocialIntent(f.ctx, a.WorkspacePrincipal.ID, d.ID)
			if e != nil {
				t.Fatal(e)
			}
			sources = append(sources, d)
		}
		v, e := f.s.ReadOwnIntroductionSuggestions(f.ctx, f.a, sources[0].ID)
		if e != nil || len(v.Candidates) != 1 || len(v.Candidates[0].Basis) != 2 {
			t.Fatal("actual writers→public reader", e, v)
		}
		f.approve(f.preview("PRIVATE"))
		v, e = f.s.ReadOwnIntroductionSuggestions(f.ctx, f.a, sources[0].ID)
		if e != nil || len(v.Candidates) != 1 || len(v.Candidates[0].Basis) != 1 {
			t.Fatal("private still used as common declaration", e, v)
		}
	})
}
func TestCommunityInterestNativeStaleScopeSourceAndRealWait(t *testing.T) {
	ownedMigrationDatabase(t)
	f := interestNative(t)
	t.Run("tampered_token_other_actor_and_process_key_restart", func(t *testing.T) {
		p := f.preview("PUBLIC")
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.other, p.Preview); !errors.Is(e, cg.ErrInterestDenied) {
			t.Fatal(e)
		}
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview+"x"); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal(e)
		}
		command := exec.Command(os.Args[0], "-test.run=^TestCommunityInterestAEADProcessBoundaryHelper$")
		command.Env = append(os.Environ(), "BIRDTIE_INTEREST_RESTART_HELPER=1", "BIRDTIE_INTEREST_TEST_SEALED="+p.Preview)
		raw, e := command.CombinedOutput()
		if e != nil {
			t.Fatal("actual new process rejected check failed", e, string(raw))
		}

	})
	t.Run("source_current_version_and_visibility", func(t *testing.T) {
		p := f.preview("PUBLIC")
		f.exec(`UPDATE communities SET name='本地新具体版本' WHERE id=$1`, f.group)
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal(e)
		}
		f.approve(f.preview("PUBLIC"))
		f.exec(`UPDATE communities SET visibility='hidden' WHERE id=$1`, f.group)
		v, e := f.s.ReadOwnCommunityInterests(f.ctx, f.a, false)
		if e != nil || v.Records[0].SourceAvailable || strings.Contains(v.Records[0].Name, "新具体版本") {
			t.Fatal(e, v)
		}
		if _, e = f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: "PUBLIC"}); e == nil {
			t.Fatal("hidden public")
		}
		f.approve(f.preview("PRIVATE"))
		f.approve(f.preview("DELETE"))
		if _, e = f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: "PRIVATE"}); e == nil {
			t.Fatal("hidden guessed-id created")
		}
		f.exec(`UPDATE communities SET visibility='public' WHERE id=$1`, f.group)
	})
	t.Run("source_public_private_public_native_xmin_ABA", func(t *testing.T) {
		p := f.preview("PUBLIC")
		f.exec(`UPDATE communities SET visibility='private' WHERE id=$1`, f.group)
		f.exec(`UPDATE communities SET visibility='public' WHERE id=$1`, f.group)
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal("resource ABA revived", e)
		}
	})
	t.Run("metadata_actual_revision_ABA_and_session_idle_extension", func(t *testing.T) {
		p := f.preview("PUBLIC")
		f.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE owner_id=$1`, f.a.WorkspacePrincipal.ID)
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal("metadata changed accepted", e)
		}
		p = f.preview("PUBLIC")
		f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '80 minutes' WHERE id=$1`, f.session)
		f.approve(p)
		f.approve(f.preview("DELETE"))
	})
	t.Run("current_hidden_draft_archived_expired_source_never_options", func(t *testing.T) {
		for _, q := range []string{`UPDATE communities SET publication_status='draft' WHERE id=$1`, `UPDATE communities SET lifecycle_status='archived' WHERE id=$1`, `UPDATE communities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, `UPDATE communities SET publication_status='draft',owner_confirmed_at=NULL WHERE id=$1`} {
			f.exec(q, f.group)
			v, e := f.s.ReadOwnCommunityInterests(f.ctx, f.a, true)
			if e != nil {
				t.Fatal(e)
			}
			for _, o := range v.Options {
				if o.CommunityID == f.group {
					t.Fatal("invalid source in options")
				}
			}
			if _, e = f.s.PreviewOwnCommunityInterest(f.ctx, f.a, cg.CommunityInterestInput{CommunityID: f.group, Operation: "PUBLIC"}); e == nil {
				t.Fatal("unavailable source allowed")
			}
			f.exec(`UPDATE communities SET publication_status='published',lifecycle_status='active',expires_at=NULL,owner_confirmed_at=clock_timestamp() WHERE id=$1`, f.group)
		}
	})

	t.Run("public_city_expiry_and_restore_current_source_gate", func(t *testing.T) {
		f.exec(`UPDATE communities SET city_id='aberdeen-gb' WHERE id=$1`, f.group)
		p := f.preview("PUBLIC")
		f.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id='aberdeen-gb'`)
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); e == nil {
			t.Fatal("city expiry accepted")
		}
		f.exec(`UPDATE cities SET expires_at=NULL WHERE id='aberdeen-gb'`)
		if _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); !errors.Is(e, cg.ErrInterestChanged) {
			t.Fatal("city restore ABA revived", e)
		}
		f.exec(`UPDATE communities SET city_id=NULL WHERE id=$1`, f.group)
	})
	t.Run("101_public_options_explicit_bounded_truncated_zero_write", func(t *testing.T) {
		f.exec(`INSERT INTO communities(owner_account_id,name,summary,visibility,join_policy,lifecycle_status,publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label) SELECT $1,'本地有界社群'||x,'本地合成','public','request','active','published',clock_timestamp(),'本地合成','disposable://bounded/'||x,'本地合成' FROM generate_series(1,101) x`, f.owner)
		before := f.snapshot()
		v, e := f.s.ReadOwnCommunityInterests(f.ctx, f.a, true)
		if e != nil || len(v.Options) != 100 || !v.Truncated || v.Limit != 100 {
			t.Fatal(e, v)
		}
		if f.snapshot() != before {
			t.Fatal("options wrote source")
		}
	})

	for _, kind := range []string{"revoke", "natural-expiry", "agent-retirement"} {
		t.Run("account_lock_wait_then_"+kind, func(t *testing.T) {
			p := f.preview("PUBLIC")
			hold, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Rollback(context.Background())
			if _, e = hold.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.a.WorkspacePrincipal.ID); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); done <- e }()
			interestWaitFor(t, f, "%FROM accounts%FOR UPDATE%")
			switch kind {
			case "revoke":
				_, e = hold.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
			case "natural-expiry":
				_, e = hold.Exec(f.ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp()+interval '60 milliseconds' AS deadline) UPDATE sessions SET expires_at=stamp.deadline,idle_expires_at=stamp.deadline FROM stamp WHERE id=$1`, f.session)
			case "agent-retirement":
				_, e = hold.Exec(f.ctx, `UPDATE agents SET status='retired' WHERE principal_account_id=$1`, f.a.WorkspacePrincipal.ID)
			}
			if e != nil {
				t.Fatal(e)
			}
			if kind == "natural-expiry" {
				time.Sleep(90 * time.Millisecond)
			}
			if e = hold.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			if e = <-done; e == nil {
				t.Fatal("waiting approval wrote after loss")
			}
			f.exec(`UPDATE sessions SET revoked_at=NULL,expires_at=clock_timestamp()+interval '2 hours',idle_expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.session)
			if kind == "agent-retirement" {
				f.exec(`UPDATE agents SET status='active' WHERE principal_account_id=$1`, f.a.WorkspacePrincipal.ID)
			}
		})
	}
	t.Run("audit_insert_wait_clock_expiry_rolls_back_domain", func(t *testing.T) {
		p := f.preview("PUBLIC")
		p0, e := openInterest(p.Preview)
		if e != nil {
			t.Fatal(e)
		}
		p0.Expires = time.Now().UTC().Add(180 * time.Millisecond)
		p.Preview, e = sealInterest(p0)
		if e != nil {
			t.Fatal(e)
		}
		before := f.snapshot()
		hold, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer hold.Rollback(context.Background())
		f.exec(`CREATE FUNCTION interest_test_audit_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(9043077);RETURN NEW;END $$;CREATE TRIGGER interest_test_audit_wait BEFORE INSERT ON audit_events FOR EACH ROW WHEN(NEW.resource_type='person_community_declaration') EXECUTE FUNCTION interest_test_audit_wait()`)
		if _, e = hold.Exec(f.ctx, `SELECT pg_advisory_xact_lock(9043077)`); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { _, e := f.s.ApproveOwnCommunityInterest(f.ctx, f.a, p.Preview); done <- e }()
		interestWaitFor(t, f, "%INSERT INTO audit_events%")
		time.Sleep(260 * time.Millisecond)
		hold.Commit(f.ctx)
		if e = <-done; e == nil {
			t.Fatal("expired final insert wait accepted")
		}
		if f.snapshot() != before {
			t.Fatal("expired write/audit not rolled back")
		}
		f.exec(`DROP TRIGGER interest_test_audit_wait ON audit_events;DROP FUNCTION interest_test_audit_wait()`)

	})
}

func TestCommunityInterestAEADProcessBoundaryHelper(t *testing.T) {
	if os.Getenv("BIRDTIE_INTEREST_RESTART_HELPER") != "1" {
		return
	}
	if _, e := openInterest(os.Getenv("BIRDTIE_INTEREST_TEST_SEALED")); !errors.Is(e, cg.ErrInterestChanged) {
		t.Fatal("new OS process did not reject original process preview", e)
	}
}

func interestWaitFor(t *testing.T, f *interestFixture, pattern string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1`, pattern).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual PostgreSQL lock wait was not observed", pattern)
}
