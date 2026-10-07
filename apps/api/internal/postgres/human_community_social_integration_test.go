package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
	"time"
)

type commNativeStoreFixture struct {
	base  *agentPrivateFixture
	id    string
	actor identity.Actor
}

func commNativeStore(t *testing.T) *commNativeStoreFixture {
	t.Helper()
	f := &commNativeStoreFixture{base: agentPrivateTestFixture(t)}
	b := f.base.base
	f.actor = identity.Actor{ID: b.person.ID, AccountType: "person"}
	c, e := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{Name: "合成原生成员持久化", Summary: "不得写入私密Profile或Memory", Visibility: "public", JoinPolicy: "open"})
	if e != nil {
		t.Fatal(e)
	}
	f.id = c.ID
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, `DELETE FROM communities WHERE id=$1`} {
			if _, e := b.pool.Exec(ctx, q, f.id); e != nil {
				t.Error("owned native Community cleanup", e)
			}
		}
	})
	return f
}
func (f *commNativeStoreFixture) effects(t *testing.T) string {
	t.Helper()
	b := f.base.base
	var out string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_array((SELECT to_jsonb(c) FROM communities c WHERE id=$1),(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]'::jsonb) FROM community_memberships m WHERE community_id=$1),(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb) FROM audit_events a WHERE resource_type='community' AND resource_id=$1::text))::text`, f.id).Scan(&out)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestCommunityUXNativeStoreDurableRestartWithoutAgentGrant(t *testing.T) {
	f := commNativeStore(t)
	b := f.base.base
	other := identity.Actor{ID: b.other.ID, AccountType: "person"}
	// Removing metadata is permitted fixture cleanup, never a source of consent.
	b.exec(`DELETE FROM agent_profiles WHERE owner_id=$1`, b.other.ID)
	beforePrivate := agentPrivateFullSnapshot(t, b.pool, b.accounts)
	out, e := b.store.MutateHumanCommunity(b.ctx, f.base.peer.SessionDigest, other, community.HumanCommand{Operation: "join", CommunityID: f.id})
	if e != nil || out.Member == nil || out.Member.Status != "active" || out.Member.UpdatedAt.IsZero() {
		t.Fatal("ordinary current native Person membership", e, out)
	}
	restarted := New(b.pool, false)
	got, e := restarted.ReadHumanCommunity(b.ctx, f.base.peer.SessionDigest, other, community.HumanRead{Kind: "detail", CommunityID: f.id})
	if e != nil || got.Community == nil || got.Community.MyStatus == nil || *got.Community.MyStatus != "active" {
		t.Fatal("new Store reads actual durable membership", e)
	}
	if beforePrivate != agentPrivateFullSnapshot(t, b.pool, b.accounts) {
		t.Fatal("ordinary Community action changed private/profile/metadata sources")
	}
	if _, e = restarted.MutateHumanCommunity(b.ctx, f.base.peer.SessionDigest, other, community.HumanCommand{Operation: "leave", CommunityID: f.id}); e != nil {
		t.Fatal("leave", e)
	}
	if _, e = restarted.ReadHumanCommunity(b.ctx, f.base.peer.SessionDigest, other, community.HumanRead{Kind: "members", CommunityID: f.id}); !errors.Is(e, community.ErrNotFound) {
		t.Fatal("left member no longer reads roster", e)
	}
}
func TestCommunityUXNativeInternalApprovalClockLimits(t *testing.T) {
	for _, mode := range []string{"expired", "future", "extended", "other_session"} {
		t.Run(mode, func(t *testing.T) {
			f := commNativeStore(t)
			b := f.base.base
			d := f.base.owner.SessionDigest
			cmd := community.HumanCommand{Operation: "archive", CommunityID: f.id}
			a, e := b.store.beginHumanCommunity(b.ctx, d, f.actor)
			if e != nil {
				t.Fatal(e)
			}
			if e = socialLockCommunity(b.ctx, a.tx, f.id); e != nil {
				t.Fatal(e)
			}
			material, e := communityApprovalMaterial(b.ctx, a, cmd, "")
			if e != nil {
				t.Fatal(e)
			}
			var now time.Time
			if e = a.tx.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
				t.Fatal(e)
			}
			issued, expiry := now.UnixNano(), now.Add(communityApprovalTTL).UnixNano()
			switch mode {
			case "expired":
				issued = now.Add(-31 * time.Second).UnixNano()
				expiry = issued + int64(communityApprovalTTL)
			case "future":
				issued = now.Add(time.Second).UnixNano()
				expiry = issued + int64(communityApprovalTTL)
			case "extended":
				expiry = issued + int64(communityApprovalTTL+time.Second)
			case "other_session":
				d[0] ^= 1
			}
			cmd.Snapshot = communityApprovalToken(d, material, issued, expiry)
			a.tx.Rollback(context.Background())
			before := f.effects(t)
			if _, e = b.store.MutateHumanCommunity(b.ctx, f.base.owner.SessionDigest, f.actor, cmd); !errors.Is(e, community.ErrApprovalStale) || before != f.effects(t) {
				t.Fatal("native PG clock and signature boundary, not human preview", mode, e)
			}
		})
	}
}
func TestCommunityUXNativeExplicitReadCommittedAndUTC(t *testing.T) {
	f := commNativeStore(t)
	b := f.base.base
	cfg, e := pgxpool.ParseConfig(b.pool.Config().ConnString())
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	cfg.ConnConfig.RuntimeParams["TimeZone"] = "Pacific/Auckland"
	pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store := New(pool, false)
	a, e := store.beginHumanCommunity(b.ctx, f.base.owner.SessionDigest, f.actor)
	if e != nil {
		t.Fatal(e)
	}
	var iso, zone string
	e = a.tx.QueryRow(b.ctx, `SELECT current_setting('transaction_isolation'),current_setting('TimeZone')`).Scan(&iso, &zone)
	a.tx.Rollback(context.Background())
	if e != nil || iso != "read committed" || zone != "UTC" {
		t.Fatal("real pool default overridden", iso, zone, e)
	}
	cmd := community.HumanCommand{Operation: "archive", CommunityID: f.id, Preview: true}
	p, e := store.MutateHumanCommunity(b.ctx, f.base.owner.SessionDigest, f.actor, cmd)
	if e != nil || p.Approval == nil {
		t.Fatal(e)
	}
	cmd.Preview = false
	cmd.Snapshot = p.Approval.Snapshot
	if _, e = b.store.MutateHumanCommunity(b.ctx, f.base.owner.SessionDigest, f.actor, cmd); e != nil {
		t.Fatal("UTC source snapshot works across distinct pool time zones", e)
	}
}
func TestCommunityUXNativeMalformedAndNilAccess(t *testing.T) {
	f := commNativeStore(t)
	b := f.base.base
	before := f.effects(t)
	for _, cmd := range []community.HumanCommand{{Operation: "unknown", CommunityID: f.id}, {Operation: "join", CommunityID: "BAD"}, {Operation: "role", CommunityID: f.id, TargetID: b.other.ID, Role: "owner"}, {Operation: "join", CommunityID: f.id, Preview: true}, {Operation: "create", Input: community.SocialInput{Name: "x", Visibility: "public", JoinPolicy: "open"}}} {
		if _, e := b.store.MutateHumanCommunity(b.ctx, f.base.owner.SessionDigest, f.actor, cmd); !errors.Is(e, community.ErrValidation) {
			t.Fatal("closed domain invalid shape", e)
		}
	}
	if _, e := b.store.ReadHumanCommunity(b.ctx, [32]byte{}, f.actor, community.HumanRead{Kind: "list"}); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("zero credential", e)
	}
	if _, e := b.store.ReadHumanCommunity(nil, f.base.owner.SessionDigest, f.actor, community.HumanRead{Kind: "list"}); !errors.Is(e, community.ErrUnavailable) {
		t.Fatal("nil current context", e)
	}
	if before != f.effects(t) {
		t.Fatal("invalid native commands wrote domain rows")
	}
}

// Only actual private/native rows, excluding Session last_seen and Community.
func agentPrivateFullSnapshot(t *testing.T, pool *pgxpool.Pool, owners []string) string {
	t.Helper()
	var out string
	e := pool.QueryRow(context.Background(), `SELECT jsonb_build_object('public',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.account_id),'[]') FROM user_profiles p WHERE account_id=ANY($1::uuid[])),'private',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.agent_id),'[]') FROM agent_private_profiles p WHERE owner_id=ANY($1::uuid[])),'metadata',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.agent_id),'[]') FROM agent_profiles p WHERE owner_id=ANY($1::uuid[])),'memory',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.id),'[]') FROM agent_memories p WHERE owner_id=ANY($1::uuid[])),'grants',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.id),'[]') FROM consent_grants p WHERE owner_account_id=ANY($1::uuid[])))::text`, owners).Scan(&out)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out, "SYNTHETIC_TRANSPORT_ONLY") {
		t.Fatal("unexpected marker")
	}
	return out
}

func TestCommunityUXNativeTransferPreserves059And061SourceAuthority(t *testing.T) {
	f := commNativeStore(t)
	b := f.base.base
	peer := identity.Actor{ID: b.other.ID, AccountType: "person"}
	if _, e := b.store.MutateHumanCommunity(b.ctx, f.base.peer.SessionDigest, peer, community.HumanCommand{Operation: "join", CommunityID: f.id}); e != nil {
		t.Fatal(e)
	}
	command := community.HumanCommand{Operation: "transfer", CommunityID: f.id, TargetID: peer.ID, Preview: true}
	p, e := b.store.MutateHumanCommunity(b.ctx, f.base.owner.SessionDigest, f.actor, command)
	if e != nil || p.Approval == nil {
		t.Fatal("current native transfer preview", e)
	}
	command.Preview, command.Snapshot = false, p.Approval.Snapshot
	if _, e = b.store.MutateHumanCommunity(b.ctx, f.base.owner.SessionDigest, f.actor, command); e != nil {
		t.Fatal("current native transfer", e)
	}
	var third, conversation, message string
	var candidates []string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, item := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM activity_candidates WHERE id=ANY($1::uuid[])`, []any{candidates}},
			{`DELETE FROM community_conversations WHERE community_id=$1`, []any{f.id}},
			{`DELETE FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, []any{f.id, third}},
			{`DELETE FROM accounts WHERE id=$1`, []any{third}},
		} {
			if third != "" {
				if _, err := b.pool.Exec(ctx, item.query, item.args...); err != nil {
					t.Error("owned source-boundary cleanup", err)
				}
			}
		}
	})
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type,status) VALUES(gen_random_uuid(),'person','active') RETURNING id`).Scan(&third); e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, f.id, third)
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO community_conversations(community_id) VALUES($1) RETURNING id`, f.id).Scan(&conversation); e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO community_conversation_members(conversation_id,account_id) VALUES($1,$2),($1,$3)`, conversation, third, peer.ID)
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO community_conversation_messages(conversation_id,sender_account_id,client_message_id,body) VALUES($1,$2,gen_random_uuid(),'LOCAL_SYNTHETIC_MESSAGE_ONLY') RETURNING id`, conversation, third).Scan(&message); e != nil {
		t.Fatal(e)
	}
	countSource := func(want int) {
		t.Helper()
		var n int
		if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM birdtie_native_notification_source('community_message',$1,$2)`, message, peer.ID).Scan(&n); err != nil || n != want {
			t.Fatal("actual061 current retained source", n, want, err)
		}
	}
	checkCandidate := func(person string, allowed bool) {
		t.Helper()
		var id string
		if err := b.pool.QueryRow(b.ctx, `INSERT INTO activity_candidates(city_id,submitted_by,title,host_label,starts_at,ends_at,time_zone,source_label,source_url,rights_note,expires_at) VALUES('aberdeen-gb',$1,'本地合成源权限检查','LOCAL_SYNTHETIC_ONLY',clock_timestamp()+interval '1 day',clock_timestamp()+interval '2 days','Europe/London','LOCAL_SYNTHETIC_ONLY','https://synthetic.invalid','SQL_SHAPE_TEST_NOT_A_CITY_APPROVAL',clock_timestamp()+interval '3 days') RETURNING id`, person).Scan(&id); err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, id)
		_, err := b.pool.Exec(b.ctx, `INSERT INTO city_activity_candidate_organizers(candidate_id,selected_by,community_id) VALUES($1,$2,$3)`, id, person, f.id)
		if allowed && err != nil || !allowed && err == nil {
			t.Fatal("actual059 retained source guard", allowed, err)
		}
		var n int
		if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM city_activity_candidate_organizers WHERE candidate_id=$1`, id).Scan(&n); err != nil || allowed && n != 1 || !allowed && n != 0 {
			t.Fatal("guard rejection retained no selector", n, err)
		}
	}
	checkCandidate(peer.ID, true)
	checkCandidate(third, false)
	countSource(1)
	// The new membership owner remains active. Only the original retained
	// source creator changes, and the independent sender remains active.
	b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.actor.ID)
	checkCandidate(peer.ID, false)
	countSource(0)
	b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.actor.ID)
	checkCandidate(peer.ID, true)
	countSource(1)
	var creator, role string
	if e = b.pool.QueryRow(b.ctx, `SELECT c.owner_account_id,m.role FROM communities c JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=$2 WHERE c.id=$1`, f.id, peer.ID).Scan(&creator, &role); e != nil || creator != f.actor.ID || role != "owner" {
		t.Fatal("current membership ownership and retained creator are distinct", e)
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY: actual059 SQL selector guard + actual061 metadata source; no publication, notification enqueue/delivery, new creator or source permission handover")
}
