package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	oso "github.com/birdtie/birdtie/apps/api/internal/onlinesocialopportunity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

type onlineSocialFixture struct {
	f                                                    *placeMemoryFixture
	a                                                    oso.Access
	own, public, friend, community, activity, tie, group string
}

func TestOnlineSocialOpportunityNativeFinalWaitsRecheckSourceAndSession(t *testing.T) {
	for _, mode := range []string{"pool-source-ABA", "pool-agent-ABA", "table-source-private", "table-membership-withdrawn", "session-revoked", "session-expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := onlineSocialNative(t)
			b := f.f.private.base
			r, e := b.store.ReadOnlineSocialOpportunities(b.ctx, f.a, f.own)
			if e != nil {
				t.Fatal(e)
			}
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal("owned pool config")
			}
			cfg.MaxConns = 2
			if strings.HasPrefix(mode, "pool-") {
				cfg.MaxConns = 1
			}
			cfg.ConnConfig.RuntimeParams["application_name"] = "online005-wait-" + b.person.ID
			p, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal("owned pool")
			}
			defer p.Close()
			store := New(p, false)
			done := make(chan error, 1)
			if strings.HasPrefix(mode, "pool-") {
				held, e := p.Acquire(b.ctx)
				if e != nil {
					t.Fatal("hold owned pool")
				}
				go func() { done <- store.RevalidateOnlineSocialOpportunities(b.ctx, f.a, r) }()
				time.Sleep(30 * time.Millisecond)
				select {
				case e := <-done:
					held.Release()
					t.Fatal("no real pool wait", e)
				default:
				}
				if mode == "pool-source-ABA" {
					b.exec(`UPDATE social_intents SET title=title WHERE id=$1`, f.public)
				} else {
					b.exec(`UPDATE agents SET status=status WHERE principal_account_id=$1`, b.person.ID)
				}
				held.Release()
			} else {
				held, e := b.pool.Begin(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer held.Rollback(context.Background())
				if mode == "session-expiry" {
					b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '400 milliseconds' WHERE token_sha256=$1`, f.a.Digest[:])
					r, e = b.store.ReadOnlineSocialOpportunities(b.ctx, f.a, f.own)
					if e != nil {
						t.Fatal(e)
					}
				}
				if strings.HasPrefix(mode, "table-") {
					_, e = held.Exec(b.ctx, `LOCK TABLE agent_profiles IN ACCESS EXCLUSIVE MODE`)
				} else {
					_, e = held.Exec(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, f.a.Digest[:])
				}
				if e != nil {
					t.Fatal(e)
				}
				go func() { done <- store.RevalidateOnlineSocialOpportunities(b.ctx, f.a, r) }()
				waited := false
				for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
					if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, cfg.ConnConfig.RuntimeParams["application_name"]).Scan(&waited); e != nil {
						t.Fatal(e)
					}
					if waited {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !waited {
					t.Fatal("actual native lock wait absent", mode)
				}
				switch mode {
				case "table-source-private":
					b.exec(`UPDATE social_intents SET audience='PRIVATE' WHERE id=$1`, f.public)
				case "table-membership-withdrawn":
					b.exec(`UPDATE community_memberships SET status='left' WHERE user_account_id=$1 AND community_id=$2`, b.person.ID, f.group)
				case "session-revoked":
					_, e = held.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.Digest[:])
				case "session-expiry":
					time.Sleep(500 * time.Millisecond)
				}
				if e != nil {
					t.Fatal(e)
				}
				if e = held.Commit(b.ctx); e != nil {
					t.Fatal(e)
				}
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("late source/session accepted", mode)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("wait did not finish")
			}
		})
	}
}

func TestOnlineSocialOpportunityNativeCrossCityWithoutRequesterContext(t *testing.T) {
	f := onlineSocialNative(t)
	b := f.f.private.base
	var requesterContexts int
	b.pool.QueryRow(b.ctx, `SELECT count(*) FROM person_contexts WHERE person_account_id=$1`, b.person.ID).Scan(&requesterContexts)
	if requesterContexts != 0 {
		t.Fatal("requester must not have required city/context", requesterContexts)
	}
	secondCity := "online005-second-" + strings.ReplaceAll(b.person.ID, "-", "")
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'合成另一城市','LOCAL','GB','Asia/Tokyo','published','LOCAL_SYNTHETIC','local:NOW005','合成维护者',$2)`, secondCity, b.other.ID)
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	act, e := b.store.CreateSocialDraft(b.ctx, b.other.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.other.ID}, CityID: secondCity, Title: "另城线上 badminton", Summary: "私密描述不在发现DTO", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Asia/Tokyo", Visibility: "public", Modality: "online", PhysicalPlaceStatus: "not_applicable"})
	if e != nil {
		t.Fatal(e)
	}
	act, e = b.store.PublishSocialActivity(b.ctx, b.other.ID, act.ID)
	if e != nil || act.PlaceID != nil {
		t.Fatal("no physical point", e)
	}
	r, e := b.store.ReadOnlineSocialOpportunities(b.ctx, f.a, f.own)
	if e != nil {
		t.Fatal(e)
	}
	found := map[string]bool{}
	for _, i := range r.View.Items {
		found[i.Source.ID] = true
	}
	if !found[f.activity] || !found[act.ID] {
		t.Fatal("cross-city source missing")
	}
	current, e := b.store.GetActivity(b.ctx, act.ID, b.person.ID)
	if e != nil || current.ID != act.ID {
		t.Fatal("original stable detail", e)
	}
	t.Log("LOCAL_SYNTHETIC requester has zero city/context declarations; published online Activities from two cities, no Place/GPS; native detail same original ID")
}

func TestOnlineSocialOpportunityNativePublicCommunityDeadlineWithoutMembership(t *testing.T) {
	f := onlineSocialNative(t)
	b := f.f.private.base
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	act, e := b.store.CreateSocialDraft(b.ctx, b.other.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "COMMUNITY", ID: f.group}, CityID: f.f.city, Title: "合成公开 Community online badminton", Summary: "不在投影", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "UTC", Visibility: "public", Modality: "online", PhysicalPlaceStatus: "not_applicable"})
	if e != nil {
		t.Fatal(e)
	}
	act, e = b.store.PublishSocialActivity(b.ctx, b.other.ID, act.ID)
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, f.group, b.person.ID)
	var deadline time.Time
	if e = b.pool.QueryRow(b.ctx, `UPDATE communities SET expires_at=clock_timestamp()+interval '700 milliseconds' WHERE id=$1 RETURNING expires_at`, f.group).Scan(&deadline); e != nil {
		t.Fatal(e)
	}
	r, e := b.store.ReadOnlineSocialOpportunities(b.ctx, f.a, f.own)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, item := range r.View.Items {
		if item.Source.ID == act.ID {
			found = true
			if item.Relation != "PUBLIC" || item.ExpiresAt.After(deadline) {
				t.Fatal("non-member public Community source exceeds current deadline", item.ExpiresAt, deadline)
			}
		}
	}
	if !found || r.View.ValidUntil.After(deadline) {
		t.Fatal("whole human lease must clamp real public Community deadline", r.View.ValidUntil, deadline)
	}
	time.Sleep(800 * time.Millisecond)
	if e = b.store.RevalidateOnlineSocialOpportunities(b.ctx, f.a, r); e == nil {
		t.Fatal("natural Community expiry accepted")
	}
}

func onlineSocialNative(t *testing.T) *onlineSocialFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	out := &onlineSocialFixture{f: f, a: oso.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, Digest: f.private.owner.SessionDigest}}
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`, b.accounts); e != nil {
			t.Error("owned Activity before Community cleanup", e)
		}
		for _, q := range []string{`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(context.Background(), q, b.accounts); e != nil {
				t.Error(e)
			}
		}
		if out.group != "" {
			if _, e := b.pool.Exec(context.Background(), `DELETE FROM communities WHERE id=$1`, out.group); e != nil {
				t.Error(e)
			}
		}
	})
	var e error
	req, e := b.store.CreateFriendRequest(b.ctx, b.person.ID, b.other.ID, "合成本人明确好友申请")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.DecideRequest(b.ctx, b.other.ID, req.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT id::text FROM person_ties WHERE request_id=$1`, req.ID).Scan(&out.tie); e != nil {
		t.Fatal(e)
	}
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO communities(id,name,summary,visibility,lifecycle_status,publication_status,owner_account_id,owner_confirmed_at,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),'合成线上社群','非真实合作方','public','active','published',$1,clock_timestamp(),'LOCAL_SYNTHETIC','local:NOW005','合成维护者') RETURNING id`, b.other.ID).Scan(&out.group); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{b.person.ID, b.other.ID} {
		b.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active') ON CONFLICT(community_id,user_account_id) DO NOTHING`, out.group, id)
	}
	newSource := func() string {
		var id string
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		b.accounts = append(b.accounts, id)
		b.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'独立合成线上来源','public')`, id)
		return id
	}
	publicCreator, communityCreator := newSource(), newSource()
	b.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, out.group, communityCreator)
	makeIntent := func(owner, audience string) string {
		r, e := b.store.CreateSocialIntentDraft(b.ctx, owner, socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "合成线上 badminton 交流", Constraints: json.RawMessage(`{"category":"badminton","onlinePlatform":"合成本地平台"}`), Audience: audience, Modality: "ONLINE", CommunityID: func() string {
			if audience == "COMMUNITY" {
				return out.group
			}
			return ""
		}(), ExpiresAt: time.Now().UTC().Add(time.Hour)})
		if e != nil {
			t.Fatal("native draft", e)
		}
		if _, e = b.store.ActivateSocialIntent(b.ctx, owner, r.ID); e != nil {
			t.Fatal("native explicit activation", e)
		}
		return r.ID
	}
	out.own = makeIntent(b.person.ID, "PRIVATE")
	out.public = makeIntent(publicCreator, "PUBLIC")
	out.friend = makeIntent(b.other.ID, "FRIENDS")
	out.community = makeIntent(communityCreator, "COMMUNITY")
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	act, e := b.store.CreateSocialDraft(b.ctx, b.other.ID, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.other.ID}, CityID: f.city, Title: "明确合成线上 badminton 活动", Summary: "不能投影的描述", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "UTC", Visibility: "public", Modality: "online", PhysicalPlaceStatus: "not_applicable"})
	if e != nil {
		t.Fatal("native noPlace ONLINE draft", e)
	}
	act, e = b.store.PublishSocialActivity(b.ctx, b.other.ID, act.ID)
	if e != nil {
		t.Fatal("native ONLINE publish", e)
	}
	if act.PlaceID != nil {
		t.Fatal("fake physical anchor")
	}
	out.activity = act.ID
	return out
}
func TestOnlineSocialOpportunityNativeRealSourcesNoCityOrLocation(t *testing.T) {
	f := onlineSocialNative(t)
	b := f.f.private.base
	options, e := b.store.ReadOnlineSocialOpportunityOptions(b.ctx, f.a)
	if e != nil || len(options.View.Intents) != 1 || len(options.View.Items) != 0 {
		t.Fatal("options", e, options.View)
	}
	r, e := b.store.ReadOnlineSocialOpportunities(b.ctx, f.a, f.own)
	if e != nil || len(r.View.Items) != 4 {
		t.Fatal("friend community public activity actual sources", e, r.View)
	}
	found := map[string]bool{}
	relations := map[string]bool{}
	for _, i := range r.View.Items {
		found[i.Source.ID] = true
		relations[i.Relation] = true
		if i.ID != f.own+":"+i.Source.Type+":"+i.Source.ID {
			t.Fatal("stable ref", i)
		}
	}
	if len(relations) != 3 {
		t.Fatal("independent real friend/community/public paths", relations)
	}
	for _, id := range []string{f.public, f.friend, f.community, f.activity} {
		if !found[id] {
			t.Fatal("missing source", id)
		}
	}
	raw, _ := json.Marshal(r.View)
	for _, canary := range []string{"latitude", "longitude", "constraints", "合成本地平台", "不能投影的描述", "creatorAccountId", "xmin", "Proof", "Seal"} {
		if strings.Contains(string(raw), canary) {
			t.Fatal("private disclosure", canary)
		}
	}
	if e = b.store.RevalidateOnlineSocialOpportunities(b.ctx, f.a, r); e != nil {
		t.Fatal("stable read", e)
	}
	// A separate native Store models API process reconstruction, no second ledger.
	restarted := New(b.pool, false)
	again, e := restarted.ReadOnlineSocialOpportunities(b.ctx, f.a, f.own)
	if e != nil || len(again.View.Items) != 4 {
		t.Fatal("native process reconstruction", e)
	}
	for index, item := range again.View.Items {
		if item.ID != r.View.Items[index].ID {
			t.Fatal("IDs lost on reconstruction")
		}
	}
	wrong := f.a
	wrong.Actor.ID = b.other.ID
	if _, e = b.store.ReadOnlineSocialOpportunities(b.ctx, wrong, f.own); e == nil {
		t.Fatal("cross-owner/session accepted")
	}
	b.exec(`UPDATE social_intents SET title=title WHERE id=$1`, f.public)
	if e = b.store.RevalidateOnlineSocialOpportunities(b.ctx, f.a, r); !errors.Is(e, oso.ErrChanged) {
		t.Fatal("same-value source ABA", e)
	}
}
func TestOnlineSocialOpportunityNativeRevocations(t *testing.T) {
	for _, change := range []string{"ownerCancel", "sourcePrivate", "sourceCancel", "sourceExpiry", "blockForward", "blockReverse", "tieRevoked", "communityWithdrawn", "membershipRemoved", "profilePrivate", "sourceAccount", "agentABA", "sessionRevoked", "activityCancelled", "activityOffline", "cityHidden"} {
		t.Run(change, func(t *testing.T) {
			f := onlineSocialNative(t)
			b := f.f.private.base
			r, e := b.store.ReadOnlineSocialOpportunities(b.ctx, f.a, f.own)
			if e != nil {
				t.Fatal(e)
			}
			switch change {
			case "ownerCancel":
				_, e = b.store.CancelSocialIntent(b.ctx, b.person.ID, f.own)
			case "sourcePrivate":
				b.exec(`UPDATE social_intents SET audience='PRIVATE' WHERE id=$1`, f.public)
			case "sourceCancel":
				var creator string
				b.pool.QueryRow(b.ctx, `SELECT creator_account_id FROM social_intents WHERE id=$1`, f.public).Scan(&creator)
				_, e = b.store.CancelSocialIntent(b.ctx, creator, f.public)
			case "sourceExpiry":
				b.exec(`UPDATE social_intents SET created_at=clock_timestamp()-interval '1 hour',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.public)
			case "blockForward":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
			case "blockReverse":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.other.ID, b.person.ID)
			case "tieRevoked":
				b.exec(`UPDATE person_ties SET status='removed' WHERE id=$1`, f.tie)
			case "communityWithdrawn":
				b.exec(`UPDATE communities SET publication_status='draft' WHERE id=$1`, f.group)
			case "membershipRemoved":
				b.exec(`UPDATE community_memberships SET status='left' WHERE user_account_id=$1 AND community_id=$2`, b.person.ID, f.group)
			case "profilePrivate":
				b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=(SELECT creator_account_id FROM social_intents WHERE id=$1)`, f.public)
			case "sourceAccount":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.other.ID)
			case "agentABA":
				b.exec(`UPDATE agents SET status=status WHERE principal_account_id=$1`, b.person.ID)
			case "sessionRevoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.Digest[:])
			case "activityCancelled":
				_, e = b.store.CancelSocialActivity(b.ctx, b.other.ID, f.activity)
			case "activityOffline":
				b.exec(`UPDATE activities SET modality='unspecified',physical_place_status='unknown' WHERE id=$1`, f.activity)
			case "cityHidden":
				b.exec(`UPDATE cities SET publication_status='draft' WHERE id=$1`, f.f.city)
			}
			if e != nil {
				t.Fatal("original action", e)
			}
			if e = b.store.RevalidateOnlineSocialOpportunities(b.ctx, f.a, r); e == nil {
				t.Fatal("withdrawn current source accepted", change)
			}
		})
	}
}
