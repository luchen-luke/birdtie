package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganization"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5/pgxpool"
)

type organizationKnowledgeFixture struct {
	p          *agentPrivateFixture
	faq        organization.FAQ
	activities []string
}

func TestOrganizationCapabilityNativeAccountBeforeSessionAndHumanWrite(t *testing.T) {
	f := organizationKnowledgeNativeFixture(t)
	b := f.p.base
	cfg := b.pool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["application_name"] = "org001-authorder-" + b.person.ID
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store := New(pool, false)
	account, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer account.Rollback(context.Background())
	if _, e = account.Exec(b.ctx, `SELECT id FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, b.person.ID); e != nil {
		t.Fatal(e)
	}
	var deadlocksBefore int64
	if e = b.pool.QueryRow(b.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocksBefore); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		actor, e := store.AuthenticateOrganizationAgent(b.ctx, f.p.owner.SessionDigest)
		if e == nil && actor.ID != b.person.ID {
			e = identity.ErrUnauthorized
		}
		done <- e
	}()
	waiting := false
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid)))`, "org001-authorder-"+b.person.ID, int(account.Conn().PgConn().PID())).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("initial auth did not reach real Account wait")
	}
	probe, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer probe.Rollback(context.Background())
	if _, e = probe.Exec(b.ctx, `SET LOCAL lock_timeout='150ms'`); e != nil {
		t.Fatal(e)
	}
	// A Session-exclusive-before-Account join would block this actual native
	// SHARE probe. Human Profile needs this exact lock after its Account lock.
	if _, e = probe.Exec(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR SHARE`, f.p.owner.SessionDigest[:]); e != nil {
		t.Fatalf("premature exclusive Session lock creates reverse human edge: %v", e)
	}
	if e = account.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	if e = probe.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("auth blocked after both native locks released")
	}
	profile, e := b.store.UpdateHumanProfile(b.ctx, f.p.owner.SessionDigest, identity.Actor{ID: b.person.ID, AccountType: "person"}, identity.ProfileInput{DisplayName: "合成仍可编辑的本人资料", Bio: "正常明确编辑", Visibility: "public"})
	if e != nil || profile.DisplayName != "合成仍可编辑的本人资料" {
		t.Fatal("original human Profile write lost compatibility", e)
	}
	var deadlocksAfter int64
	if e = b.pool.QueryRow(b.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocksAfter); e != nil || deadlocksAfter != deadlocksBefore {
		t.Fatal("native database deadlock count changed", e)
	}
	t.Log("LOCAL_NATIVE_ONLY: actual Account wait, Session SHARE edge remains available, original human Profile write succeeds; no promise about all unrelated legacy writers")
}

func organizationKnowledgeNativeFixture(t *testing.T) *organizationKnowledgeFixture {
	t.Helper()
	p := agentPrivateTestFixture(t)
	b := p.base
	f := &organizationKnowledgeFixture{p: p, activities: []string{}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, sql := range []string{`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`, `DELETE FROM organization_faqs WHERE organization_id IN(SELECT id FROM organizations WHERE account_id=ANY($1::uuid[]))`} {
			if _, e := b.pool.Exec(ctx, sql, b.accounts); e != nil {
				t.Errorf("owned organization knowledge cleanup: %v", e)
			}
		}
	})
	b.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, b.orgID, b.person.ID, b.other.ID)
	// This is an explicit LOCAL synthetic verification-status fixture. No real
	// organization identity/CSSA approval or external verification is claimed.
	b.exec(`UPDATE organizations SET verification_status='verified',description='合成公开组织介绍',official_links='["https://example.invalid/organization"]' WHERE id=$1`, b.orgID)
	var err error
	f.faq, err = b.store.CreateFAQ(b.ctx, b.person.ID, b.orgID, organization.FAQInput{Question: "如何报名", Answer: "合成主办方公开答案：请在活动详情报名。", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *organizationKnowledgeFixture) activity(t *testing.T, title, visibility string) activitypublish.Activity {
	t.Helper()
	b := f.p.base
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	input := activitypublish.Input{CityID: "aberdeen-gb", Title: title, Summary: "合成组织公开来源", Description: "不得复制的完整活动正文", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Europe/London", Modality: "online", PhysicalPlaceStatus: "not_applicable", Visibility: visibility}
	draft, err := b.store.CreateDraft(b.ctx, b.person.ID, b.orgID, input)
	if err != nil {
		t.Fatal(err)
	}
	f.activities = append(f.activities, draft.ID)
	a, err := b.store.PublishActivity(b.ctx, b.person.ID, b.orgID, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestOrganizationCapabilityNativeCurrentGroundingAndNoEffects(t *testing.T) {
	f := organizationKnowledgeNativeFixture(t)
	b := f.p.base
	private := savePrivateCanaries(t, f.p)
	memory := mustPutAgentMemory(t, f.p, agentMemoryID(t, f.p), agentMemoryInput("org-grounding-private"))
	public := f.activity(t, "合成公开羽毛球活动", "public")
	secret := f.activity(t, "合成仅组织成员活动", "organizer_members")
	before := agentMemoryOwnedRowsSnapshot(t, f.p) + purposeOwnedControlRows(t, f.p)
	for _, c := range []struct{ query, kind, id string }{{"如何报名", "faq", f.faq.ID}, {"组织介绍", "profile", b.orgID}, {"官网", "link", b.orgID}, {"合成公开羽毛球活动", "activity", public.ID}} {
		t.Run(c.kind, func(t *testing.T) {
			answer, err := b.store.AnswerOrganization(b.ctx, b.orgID, b.person.ID, c.query)
			if err != nil || answer.Status != "known" || len(answer.Sources) != 1 || answer.Sources[0].Type != c.kind || answer.Sources[0].ID != c.id || !agentorganization.ValidAnswer(answer, b.orgID) {
				t.Fatalf("actual native grounding: %v %#v", err, answer)
			}
			for _, canary := range []string{private.Fields.AgentNotes, memory.Summary, secret.Title, "不得复制的完整活动正文"} {
				if canary != "" && strings.Contains(answer.Answer, canary) {
					t.Fatal("public pack copied an unrelated private/full source")
				}
			}
		})
	}
	for _, viewer := range []string{"", b.person.ID, b.other.ID} {
		t.Run("private_activity_not_public_knowledge_"+viewer, func(t *testing.T) {
			a, e := b.store.AnswerOrganization(b.ctx, b.orgID, viewer, secret.Title)
			if e != nil || a.Status != "unknown" || len(a.Sources) != 0 {
				t.Fatal("organization role widened public Agent sources")
			}
		})
	}
	if after := agentMemoryOwnedRowsSnapshot(t, f.p) + purposeOwnedControlRows(t, f.p); before != after {
		t.Fatal("native READ ONLY question changed identity/Profile/Memory/Evidence/candidate/Task/outbox/effect/session")
	}
	reopened := New(b.pool, false)
	answer, e := reopened.AnswerOrganization(b.ctx, b.orgID, "", "如何报名")
	if e != nil || answer.Sources[0].ID != f.faq.ID {
		t.Fatal("reopened Store failed current FAQ")
	}
	updated, e := b.store.UpdateFAQ(b.ctx, b.other.ID, b.orgID, f.faq.ID, organization.FAQInput{Question: f.faq.Question, Answer: "合成管理员更新后的当前答案", Published: true})
	if e != nil {
		t.Fatal(e)
	}
	answer, e = reopened.AnswerOrganization(b.ctx, b.orgID, "", "如何报名")
	if e != nil || answer.Answer != updated.Answer {
		t.Fatal("old FAQ answer persisted")
	}
	if _, e = b.store.UpdateFAQ(b.ctx, b.person.ID, b.orgID, f.faq.ID, organization.FAQInput{Question: f.faq.Question, Answer: updated.Answer, Published: false}); e != nil {
		t.Fatal(e)
	}
	answer, e = reopened.AnswerOrganization(b.ctx, b.orgID, "", "如何报名")
	if e != nil || answer.Status != "unknown" || len(answer.Sources) != 0 {
		t.Fatal("unpublished source still cited")
	}
	if _, e = b.store.CancelActivity(b.ctx, b.person.ID, b.orgID, public.ID); e != nil {
		t.Fatal(e)
	}
	answer, e = reopened.AnswerOrganization(b.ctx, b.orgID, "", public.Title)
	if e != nil || answer.Status != "unknown" {
		t.Fatal("cancelled activity still cited")
	}
	t.Log("LOCAL_SYNTHETIC_NATIVE_ONLY: existing organization/FAQ/activity writers and shared public capability actually consumed; no external verification/model/organization Memory permission")
}

func TestOrganizationCapabilityNativeInvalidatedIdentityAndSources(t *testing.T) {
	for _, c := range []struct {
		name, sql string
		notFound  bool
	}{
		{"org_private", `UPDATE organizations SET visibility='private' WHERE id=$1`, true},
		{"org_inactive", `UPDATE organizations SET status='suspended' WHERE id=$1`, true},
		{"verification_revoked", `UPDATE organizations SET verification_status='unverified' WHERE id=$1`, false},
		{"account_inactive", `UPDATE accounts SET status='suspended' WHERE id=$2`, true},
		{"agent_inactive", `UPDATE agents SET status='suspended' WHERE id=$3`, false},
		{"metadata_removed", `DELETE FROM agent_profiles WHERE agent_id=$3`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := organizationKnowledgeNativeFixture(t)
			b := f.p.base
			if _, e := b.pool.Exec(b.ctx, c.sql+` AND $1::uuid IS NOT NULL AND $2::uuid IS NOT NULL AND $3::uuid IS NOT NULL`, b.orgID, b.org.ID, b.orgAgentID); e != nil {
				t.Fatal(e)
			}
			a, e := b.store.AnswerOrganization(b.ctx, b.orgID, "", "如何报名")
			if c.notFound {
				if !errors.Is(e, organization.ErrNotFound) || !reflect.DeepEqual(a, organization.AgentAnswer{}) {
					t.Fatal("invalidated principal/org released a response")
				}
			} else if e != nil || a.Status != "unknown" || len(a.Sources) != 0 || strings.Contains(a.Answer, f.faq.Answer) {
				t.Fatal("invalidated speaker/verification retained source")
			}
		})
	}
	t.Run("block_organization_and_namespace", func(t *testing.T) {
		f := organizationKnowledgeNativeFixture(t)
		b := f.p.base
		if e := b.store.BlockAccount(b.ctx, b.other.ID, b.org.ID); e != nil {
			t.Fatal(e)
		}
		if a, e := b.store.AnswerOrganization(b.ctx, b.orgID, b.other.ID, "如何报名"); !errors.Is(e, organization.ErrNotFound) || !reflect.DeepEqual(a, organization.AgentAnswer{}) {
			t.Fatal("blocked organization still answered")
		}
		for _, wrong := range []string{b.org.ID, b.orgAgentID, b.person.ID} {
			if a, e := b.store.AnswerOrganization(b.ctx, wrong, "", "如何报名"); !errors.Is(e, organization.ErrNotFound) || !reflect.DeepEqual(a, organization.AgentAnswer{}) {
				t.Fatal("principal/Agent/entity namespace merged")
			}
		}
	})
}

// The FAQ relation wait deliberately sits after the old reader's separate
// public-profile query. The new coherent statement must observe revocation
// when its real relation lock is released, even with a default RR pool.
func TestOrganizationCapabilityNativeCoherentSourceWait(t *testing.T) {
	for _, change := range []string{"verification_revoked", "org_private", "faq_unpublished", "cancelled_request"} {
		t.Run(change, func(t *testing.T) {
			f := organizationKnowledgeNativeFixture(t)
			b := f.p.base
			cfg := b.pool.Config().Copy()
			cfg.ConnConfig.RuntimeParams["application_name"] = "org001-coherent-" + b.person.ID
			cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(b.ctx, `LOCK TABLE organization_faqs IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			type result struct {
				answer organization.AgentAnswer
				err    error
			}
			done := make(chan result, 1)
			go func() { a, e := store.AnswerOrganization(ctx, b.orgID, "", "如何报名"); done <- result{a, e} }()
			waiting := false
			until := time.Now().Add(4 * time.Second)
			for time.Now().Before(until) {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND $2::integer=ANY(pg_blocking_pids(pid)))`, "org001-coherent-"+b.person.ID, int(lock.Conn().PgConn().PID())).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("actual FAQ relation wait not reached")
			}
			switch change {
			case "verification_revoked":
				b.exec(`UPDATE organizations SET verification_status='unverified' WHERE id=$1`, b.orgID)
			case "org_private":
				b.exec(`UPDATE organizations SET visibility='private' WHERE id=$1`, b.orgID)
			case "faq_unpublished":
				if _, e = lock.Exec(b.ctx, `UPDATE organization_faqs SET published=false WHERE id=$1`, f.faq.ID); e != nil {
					t.Fatal(e)
				}
			case "cancelled_request":
				cancel()
			}
			if e = lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				if change == "org_private" || change == "cancelled_request" {
					if got.err == nil || !reflect.DeepEqual(got.answer, organization.AgentAnswer{}) {
						t.Fatalf("late deny released reply: %v %#v", got.err, got.answer)
					}
				} else if got.err != nil || got.answer.Status != "unknown" || len(got.answer.Sources) != 0 || strings.Contains(got.answer.Answer, f.faq.Answer) {
					t.Fatalf("old public snapshot reused after wait: %v %#v", got.err, got.answer)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("source read remained stuck after native lock release")
			}
		})
	}
}
