package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
)

type newPeopleFixture struct {
	t            *testing.T
	ctx          context.Context
	pool         *pgxpool.Pool
	store        *Store
	ids          []string // owner, peer, another Person, Organization
	category     string
	cities       []string
	cityContexts []string
	places       []string
}

func newPeopleTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("new people matrix requires BIRDTIE_DATABASE_URL and a disposable DB")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	var installed bool
	if err = pool.QueryRow(context.Background(), `SELECT to_regclass('public.person_new_people_consent') IS NOT NULL`).Scan(&installed); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if !installed {
		pool.Close()
		t.Skip("052_new_people_consent not applied")
	}
	t.Cleanup(pool.Close)
	return pool
}

func newPeoplePair(t *testing.T, pool *pgxpool.Pool) *newPeopleFixture {
	t.Helper()
	f := &newPeopleFixture{t: t, ctx: context.Background(), pool: pool, store: New(pool, false), ids: make([]string, 4)}
	// Register cleanup before setup, including explicit consent rows, so the
	// migration's protected down never confuses test choices with user choices.
	t.Cleanup(func() {
		for _, statement := range []string{
			`DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`,
			`DELETE FROM community_memberships WHERE user_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM person_agent_relationship_consent WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM person_social_disclosure WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			ids := []string{}
			for _, id := range f.ids {
				if id != "" {
					ids = append(ids, id)
				}
			}
			if _, err := pool.Exec(context.Background(), statement, ids); err != nil {
				t.Errorf("new people cleanup: %v", err)
			}
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM places WHERE id=ANY($1::uuid[])`, f.places); err != nil {
			t.Errorf("Place cleanup: %v", err)
		}
		for _, statement := range []string{
			`DELETE FROM contexts WHERE city_id=ANY($1::text[])`,
			`DELETE FROM city_contexts WHERE city_id=ANY($1::text[])`,
			`DELETE FROM cities WHERE id=ANY($1::text[])`,
		} {
			if _, err := pool.Exec(context.Background(), statement, f.cities); err != nil {
				t.Errorf("City cleanup: %v", err)
			}
		}
	})
	for i := range f.ids {
		kind := "person"
		if i == 3 {
			kind = "organization"
		}
		if err := pool.QueryRow(f.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&f.ids[i]); err != nil {
			t.Fatal(err)
		}
		if i != 3 {
			f.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成新朋友测试','public')`, f.ids[i])
		}
	}
	f.category = "np52-category-" + f.ids[0]
	return f
}

func (f *newPeopleFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *newPeopleFixture) count(sql string, args ...any) int {
	f.t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

func (f *newPeopleFixture) audience(intentID, audience, targetID string) {
	f.t.Helper()
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `UPDATE social_intents SET audience=$2 WHERE id=$1`, intentID, audience); err != nil {
		f.t.Fatal(err)
	}
	switch audience {
	case "INVITE_ONLY":
		_, err = tx.Exec(f.ctx, `INSERT INTO social_intent_invitations(intent_id,invitee_account_id) VALUES($1,$2)`, intentID, targetID)
	case "COMMUNITY":
		_, err = tx.Exec(f.ctx, `INSERT INTO social_intent_audience_targets(intent_id,community_id) VALUES($1,$2)`, intentID, targetID)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
}

func (f *newPeopleFixture) enable() {
	f.t.Helper()
	for _, id := range f.ids[:2] {
		if consent, err := f.store.SetNewPeopleConsent(f.ctx, id, true); err != nil || !consent.Enabled {
			f.t.Fatalf("explicit enable: %+v %v", consent, err)
		}
	}
}

func (f *newPeopleFixture) scopes() {
	f.t.Helper()
	if len(f.cities) > 0 {
		return
	}
	for i := 0; i < 2; i++ {
		city := "np52-city-" + f.ids[i]
		f.cities = append(f.cities, city)
		f.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)
 VALUES($1,'Synthetic new people City','test','GB','Europe/London','published','development fixture','test-only','Synthetic')`, city)
		f.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, city)
		var node, place string
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM contexts WHERE context_type='CITY' AND city_id=$1`, city).Scan(&node); err != nil {
			f.t.Fatal(err)
		}
		f.cityContexts = append(f.cityContexts, node)
		if err := f.pool.QueryRow(f.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label)
 VALUES(gen_random_uuid(),$1,'Synthetic new people Place','sport','published','development fixture','test-only','Synthetic') RETURNING id`, city).Scan(&place); err != nil {
			f.t.Fatal(err)
		}
		f.places = append(f.places, place)
	}
}

func (f *newPeopleFixture) draft(who int, modality string, city int, place, area, platform string) socialintent.Record {
	f.t.Helper()
	cityID := ""
	if city >= 0 {
		f.scopes()
		cityID = f.cities[city]
	}
	record, err := f.store.CreateNewPeopleIntent(f.ctx, f.ids[who], newpeople.DraftInput{
		Title: "RAW_PRIVATE_TITLE_SENTINEL", Category: f.category, Modality: modality, CityID: cityID,
		PlaceID: place, AreaLabel: area, OnlinePlatform: platform, ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *newPeopleFixture) activate(record socialintent.Record) socialintent.Record {
	f.t.Helper()
	active, err := f.store.ActivateSocialIntent(f.ctx, record.CreatorID, record.ID)
	if err != nil || active.Status != "ACTIVE" {
		f.t.Fatalf("owner-confirmed activation: %+v %v", active, err)
	}
	return active
}

func (f *newPeopleFixture) online() (socialintent.Record, socialintent.Record) {
	f.t.Helper()
	f.enable()
	return f.activate(f.draft(0, "ONLINE", -1, "", "", "PLATFORM_CONTACT_SENTINEL")), f.activate(f.draft(1, "ONLINE", -1, "", "", "platform_contact_sentinel"))
}

func (f *newPeopleFixture) assertCandidate(source, peer socialintent.Record) newpeople.Response {
	f.t.Helper()
	out, err := f.store.FindNewPeople(f.ctx, f.ids[0], source.ID)
	if err != nil || len(out.Candidates) != 1 || out.Candidates[0].CandidateIntentID != peer.ID || out.Candidates[0].AccountID != f.ids[1] {
		f.t.Fatalf("expected only policy-approved peer: %+v %v", out, err)
	}
	return out
}

func (f *newPeopleFixture) assertRevoked(source, peer socialintent.Record, sourceMissing bool) {
	f.t.Helper()
	out, err := f.store.FindNewPeople(f.ctx, f.ids[0], source.ID)
	if sourceMissing {
		if !errors.Is(err, newpeople.ErrNotFound) {
			f.t.Fatalf("revoked own source: %+v %v", out, err)
		}
	} else if err != nil || len(out.Candidates) != 0 {
		f.t.Fatalf("revoked peer still present: %+v %v", out, err)
	}
	if request, err := f.store.InviteNewPeople(f.ctx, f.ids[0], source.ID, peer.ID, "显式测试邀请"); !errors.Is(err, newpeople.ErrNotFound) {
		f.t.Fatalf("stale result could invite: %+v %v", request, err)
	}
}

func TestNewPeopleRoutingIntegration(t *testing.T) {
	pool := newPeopleTestPool(t)
	t.Run("default_choice_is_separate_and_active_person_trigger", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		f.exec(`INSERT INTO person_agent_relationship_consent(account_id,enabled) VALUES($1,true)`, f.ids[0])
		f.exec(`INSERT INTO person_social_disclosure(account_id,shared_activities) VALUES($1,true)`, f.ids[0])
		request, err := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[2], "Independent accepted friendship")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.store.DecideRequest(f.ctx, f.ids[2], request.ID, "accept"); err != nil {
			t.Fatal(err)
		}
		if consent, err := f.store.GetNewPeopleConsent(f.ctx, f.ids[0]); err != nil || consent.Enabled {
			t.Fatalf("unrelated public/relationship consent backfilled matching: %+v %v", consent, err)
		}
		if count := f.count(`SELECT count(*) FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`, f.ids); count != 0 {
			t.Fatalf("implicit choice rows: %d", count)
		}
		if _, err := pool.Exec(f.ctx, `INSERT INTO person_new_people_consent(account_id,enabled) VALUES($1,true)`, f.ids[3]); err == nil {
			t.Fatal("Organization accepted matching choice")
		}
		f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.ids[2])
		if _, err := pool.Exec(f.ctx, `INSERT INTO person_new_people_consent(account_id,enabled) VALUES($1,true)`, f.ids[2]); err == nil {
			t.Fatal("inactive Person accepted matching choice")
		}
		if _, err := f.store.SetNewPeopleConsent(f.ctx, f.ids[3], true); !errors.Is(err, newpeople.ErrNotFound) {
			t.Fatalf("Organization store choice: %v", err)
		}
		if _, err := f.store.SetNewPeopleConsent(f.ctx, f.ids[2], true); !errors.Is(err, newpeople.ErrNotFound) {
			t.Fatalf("inactive store choice: %v", err)
		}
		if _, err := f.store.SetNewPeopleConsent(f.ctx, f.ids[0], true); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.SetNewPeopleConsent(f.ctx, f.ids[0], false); err != nil {
			t.Fatal(err)
		}
		if count := f.count(`SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='new_people_consent'
 AND action='update' AND purpose IN('explicit_enable','explicit_revoke')`, f.ids[0]); count != 2 {
			t.Fatalf("choice audit not atomic: %d", count)
		}
	})
	t.Run("online_drafts_require_activation_and_do_not_use_private_person_context", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		f.enable()
		f.scopes()
		source, peer := f.draft(0, "ONLINE", -1, "", "", "PLATFORM_CONTACT_SENTINEL"), f.draft(1, "ONLINE", -1, "", "", "platform_contact_sentinel")
		if source.ContextID != nil || peer.ContextID != nil {
			t.Fatal("online intent was assigned a City")
		}
		if _, err := f.store.FindNewPeople(f.ctx, f.ids[0], source.ID); !errors.Is(err, newpeople.ErrNotFound) {
			t.Fatalf("draft discovered: %v", err)
		}
		source = f.activate(source)
		if out, err := f.store.FindNewPeople(f.ctx, f.ids[0], source.ID); err != nil || len(out.Candidates) != 0 {
			t.Fatalf("peer draft became supply: %+v %v", out, err)
		}
		peer = f.activate(peer)
		if count := f.count(`SELECT count(*) FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`, f.ids); count != 0 {
			t.Fatal("route created private Person Context")
		}
		for i := 0; i < 2; i++ {
			f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation,visibility) VALUES($1,$2,'current','private')`, f.ids[i], f.cityContexts[i])
		}
		out := f.assertCandidate(source, peer)
		payload, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"RAW_PRIVATE_TITLE_SENTINEL", "PLATFORM_CONTACT_SENTINEL", "platform_contact_sentinel", "cityId", "placeId", "areaLabel", "onlinePlatform", "contextId", "latitude", "longitude", "inviteeAccountIds", "body", "friendIds", f.cities[0], f.cities[1], f.cityContexts[0]} {
			if strings.Contains(string(payload), forbidden) {
				t.Fatalf("matching disclosure includes %q: %s", forbidden, payload)
			}
		}
		f.exec(`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`, f.ids)
		f.assertCandidate(source, peer)
		items, err := f.store.ListNewPeopleIntents(f.ctx, f.ids[0])
		if err != nil || len(items) != 1 || items[0].ID != source.ID {
			t.Fatalf("owner-only companion list: %+v %v", items, err)
		}
		if _, err := f.store.FindNewPeople(f.ctx, f.ids[2], source.ID); !errors.Is(err, newpeople.ErrNotFound) {
			t.Fatalf("other person borrowed source: %v", err)
		}
		if _, err := f.store.FindNewPeople(f.ctx, f.ids[3], source.ID); !errors.Is(err, newpeople.ErrNotFound) {
			t.Fatalf("Organization borrowed source: %v", err)
		}
		if _, err := f.store.InviteNewPeople(f.ctx, f.ids[2], source.ID, peer.ID, "not owner"); !errors.Is(err, newpeople.ErrNotFound) {
			t.Fatalf("other person used source to invite: %v", err)
		}
	})
	for _, tc := range []struct {
		name   string
		own    bool
		change func(*newPeopleFixture, socialintent.Record, socialintent.Record)
	}{
		{"peer_opt_out", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			if _, err := f.store.SetNewPeopleConsent(f.ctx, f.ids[1], false); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"owner_opt_out", true, func(f *newPeopleFixture, s, p socialintent.Record) {
			if _, err := f.store.SetNewPeopleConsent(f.ctx, f.ids[0], false); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"private_peer_profile", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[1])
		}},
		{"private_owner_profile", true, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[0])
		}},
		{"suspended_peer", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.ids[1])
		}},
		{"suspended_owner", true, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.ids[0])
		}},
		{"peer_private_intent", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE social_intents SET audience='PRIVATE' WHERE id=$1`, p.ID)
		}},
		{"source_private_intent", true, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE social_intents SET audience='PRIVATE' WHERE id=$1`, s.ID)
		}},
		{"peer_draft", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE social_intents SET status='DRAFT' WHERE id=$1`, p.ID)
		}},
		{"peer_cancelled", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			if _, err := f.store.CancelSocialIntent(f.ctx, f.ids[1], p.ID); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"source_cancelled", true, func(f *newPeopleFixture, s, p socialintent.Record) {
			if _, err := f.store.CancelSocialIntent(f.ctx, f.ids[0], s.ID); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"peer_expired", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE social_intents SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE id=$1`, p.ID)
		}},
		{"source_expired", true, func(f *newPeopleFixture, s, p socialintent.Record) {
			f.exec(`UPDATE social_intents SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE id=$1`, s.ID)
		}},
		{"block_owner_to_peer", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			if err := f.store.BlockAccount(f.ctx, f.ids[0], f.ids[1]); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"block_peer_to_owner", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			if err := f.store.BlockAccount(f.ctx, f.ids[1], f.ids[0]); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"existing_accepted_tie", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			r, e := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[1], "already connected")
			if e != nil {
				f.t.Fatal(e)
			}
			if _, e = f.store.DecideRequest(f.ctx, f.ids[1], r.ID, "accept"); e != nil {
				f.t.Fatal(e)
			}
		}},
		{"pending_forward_request", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			if _, e := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[1], "waiting"); e != nil {
				f.t.Fatal(e)
			}
		}},
		{"pending_reverse_request", false, func(f *newPeopleFixture, s, p socialintent.Record) {
			if _, e := f.store.CreateFriendRequest(f.ctx, f.ids[1], f.ids[0], "waiting"); e != nil {
				f.t.Fatal(e)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPeoplePair(t, pool)
			source, peer := f.online()
			f.assertCandidate(source, peer)
			tc.change(f, source, peer)
			f.assertRevoked(source, peer, tc.own)
		})
	}
	t.Run("mutual_audience_revalidated_and_membership_invitation_revocation", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		source, peer := f.online()
		// The source is visible only to a different explicitly invited Person.
		f.audience(source.ID, "INVITE_ONLY", f.ids[2])
		f.assertRevoked(source, peer, false)
		f.exec(`INSERT INTO social_intent_invitations(intent_id,invitee_account_id) VALUES($1,$2)`, source.ID, f.ids[1])
		f.assertCandidate(source, peer)
		f.exec(`UPDATE social_intent_invitations SET status='revoked' WHERE intent_id=$1 AND invitee_account_id=$2`, source.ID, f.ids[1])
		f.assertRevoked(source, peer, false)
	})
	t.Run("community_audience_requires_both_current_members", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		source, peer := f.online()
		const community = "b1700000-0000-4000-8000-000000000030"
		for _, id := range f.ids[:2] {
			f.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, community, id)
		}
		f.audience(peer.ID, "COMMUNITY", community)
		f.assertCandidate(source, peer)
		f.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, community, f.ids[0])
		f.assertRevoked(source, peer, false)
	})
	t.Run("physical_same_published_place_and_scope_revalidation", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		f.enable()
		f.scopes()
		source := f.activate(f.draft(0, "IN_PERSON", 0, f.places[0], "SCOPE_SENTINEL", ""))
		peer := f.activate(f.draft(1, "IN_PERSON", 0, f.places[0], "SCOPE_SENTINEL", ""))
		f.assertCandidate(source, peer)
		if _, err := f.store.CreateNewPeopleIntent(f.ctx, f.ids[2], newpeople.DraftInput{Title: "wrong City", Category: f.category, Modality: "IN_PERSON", CityID: f.cities[1], PlaceID: f.places[0], ExpiresAt: time.Now().Add(24 * time.Hour)}); !errors.Is(err, newpeople.ErrInvalid) {
			t.Fatalf("contradictory City/Place create: %v", err)
		}
		f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.places[0])
		f.assertRevoked(source, peer, true)
		f.exec(`UPDATE places SET publication_status='published',expires_at=now()-interval '1 day' WHERE id=$1`, f.places[0])
		f.assertRevoked(source, peer, true)
		f.exec(`UPDATE places SET expires_at=NULL WHERE id=$1`, f.places[0])
		f.assertCandidate(source, peer)
		f.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.cities[0])
		f.assertRevoked(source, peer, true)
	})
	t.Run("physical_area_needs_same_explicit_city_and_no_private_fallback", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		f.enable()
		f.scopes()
		source := f.activate(f.draft(0, "IN_PERSON", 0, "", " City   Centre ", ""))
		peer := f.activate(f.draft(1, "IN_PERSON", 0, "", "city centre", ""))
		f.assertCandidate(source, peer)
		f.exec(`UPDATE social_intents SET context_id=$2 WHERE id=$1`, peer.ID, f.cityContexts[1])
		f.assertRevoked(source, peer, false)
		f.exec(`UPDATE social_intents SET context_id=NULL WHERE id=ANY($1::uuid[])`, []string{source.ID, peer.ID})
		for _, id := range f.ids[:2] {
			f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation,visibility) VALUES($1,$2,'current','private')`, id, f.cityContexts[0])
		}
		f.assertRevoked(source, peer, true)
	})
	t.Run("legacy_contradictory_city_place_intent_is_not_matching_supply", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		f.enable()
		f.scopes()
		source := f.activate(f.draft(0, "IN_PERSON", 0, f.places[0], "", ""))
		peer := f.activate(f.draft(1, "IN_PERSON", 0, f.places[0], "", ""))
		f.assertCandidate(source, peer)
		f.exec(`UPDATE social_intents SET context_id=$2 WHERE id=$1`, peer.ID, f.cityContexts[1])
		f.assertRevoked(source, peer, false)
		f.exec(`UPDATE social_intents SET context_id=$2 WHERE id=$1`, source.ID, f.cityContexts[1])
		f.assertRevoked(source, peer, true)
	})
	t.Run("invite_is_pending_atomic_and_repeat_has_no_extra_side_effects", func(t *testing.T) {
		f := newPeoplePair(t, pool)
		source, peer := f.online()
		f.assertCandidate(source, peer)
		r, err := f.store.InviteNewPeople(f.ctx, f.ids[0], source.ID, peer.ID, "Human confirmed introduction")
		if err != nil || r.Scope != "friend" || r.State != "pending" || r.OtherAccountID != f.ids[1] || r.CityID != "" {
			t.Fatalf("real pending friend request: %+v %v", r, err)
		}
		if _, err := f.store.InviteNewPeople(f.ctx, f.ids[0], source.ID, peer.ID, "retry"); err == nil {
			t.Fatal("duplicate matching invitation succeeded")
		}
		if count := f.count(`SELECT count(*) FROM connection_requests WHERE sender_account_id=$1 AND recipient_account_id=$2`, f.ids[0], f.ids[1]); count != 1 {
			t.Fatalf("duplicate requests: %d", count)
		}
		if count := f.count(`SELECT count(*) FROM inbox_items WHERE recipient_account_id=$1 AND resource_type='connection_request' AND resource_id=$2`, f.ids[1], r.ID); count != 1 {
			t.Fatalf("duplicate Inbox: %d", count)
		}
		if count := f.count(`SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='social_intent' AND resource_id=$2 AND purpose='explicit_new_people_invitation'`, f.ids[0], peer.ID); count != 1 {
			t.Fatalf("missing/duplicate invitation audit: %d", count)
		}
		if count := f.count(`SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, f.ids); count != 0 {
			t.Fatal("invitation auto-created Tie")
		}
		if count := f.count(`SELECT count(*) FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, f.ids); count != 0 {
			t.Fatal("invitation auto-created chat")
		}
		if _, err := f.store.DecideRequest(f.ctx, f.ids[0], r.ID, "accept"); !errors.Is(err, connection.ErrNotFound) {
			t.Fatalf("sender accepted own invitation: %v", err)
		}
		if _, err := f.store.SetNewPeopleConsent(f.ctx, f.ids[1], false); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.DecideRequest(f.ctx, f.ids[1], r.ID, "accept"); err != nil {
			t.Fatalf("explicitly sent request lost ordinary recipient decision semantics: %v", err)
		}
		if count := f.count(`SELECT count(*) FROM person_ties WHERE request_id=$1 AND status='active'`, r.ID); count != 1 {
			t.Fatal("recipient accept did not create real Tie")
		}
		if count := f.count(`SELECT count(*) FROM conversations WHERE request_id=$1`, r.ID); count != 0 {
			t.Fatal("recipient accept auto-created chat")
		}
	})
}

// The lock holder provides a real database barrier. Both operations must be
// observed waiting on that transaction before releasing it; no sleep or assumed
// goroutine timing is used to manufacture a concurrency result.
// Regression: locking accounts FOR UPDATE before the Intent created a
// 40P01 deadlock against Cancel's audit foreign-key KEY SHARE account lock.
// FOR NO KEY UPDATE preserves serialization while allowing that FK check.
func TestNewPeopleInvitationConcurrencyIntegration(t *testing.T) {
	pool := newPeopleTestPool(t)
	for _, kind := range []string{"consent", "block", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newPeoplePair(t, pool)
			source, peer := f.online()
			f.assertCandidate(source, peer)
			ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
			defer cancel()
			barrier, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer barrier.Rollback(context.Background())
			var blockerPID int
			if err = barrier.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			if kind == "cancel" {
				var id string
				if err = barrier.QueryRow(ctx, `SELECT id FROM social_intents WHERE id=$1 FOR UPDATE`, source.ID).Scan(&id); err != nil {
					t.Fatal(err)
				}
			} else {
				var id string
				if err = barrier.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.ids[1]).Scan(&id); err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			inviteDone := make(chan error, 1)
			mutationDone := make(chan error, 1)
			go func() {
				<-start
				_, e := f.store.InviteNewPeople(ctx, f.ids[0], source.ID, peer.ID, "Concurrent human-confirmed invitation")
				inviteDone <- e
			}()
			go func() {
				<-start
				var e error
				switch kind {
				case "consent":
					_, e = f.store.SetNewPeopleConsent(ctx, f.ids[1], false)
				case "block":
					e = f.store.BlockAccount(ctx, f.ids[0], f.ids[1])
				case "cancel":
					_, e = f.store.CancelSocialIntent(ctx, f.ids[0], source.ID)
				}
				mutationDone <- e
			}()
			close(start)
			for {
				var waiting int
				err = pool.QueryRow(ctx, `WITH RECURSIVE waiters(pid) AS (
 SELECT pid FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))
 UNION SELECT activity.pid FROM pg_stat_activity activity JOIN waiters ON waiters.pid=ANY(pg_blocking_pids(activity.pid)))
 SELECT count(*) FROM waiters`, blockerPID).Scan(&waiting)
				if err != nil {
					t.Fatalf("concurrent operations never reached database barrier: %v", err)
				}
				if waiting >= 2 {
					break
				}
			}
			if err = barrier.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var inviteErr, mutationErr error
			select {
			case inviteErr = <-inviteDone:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case mutationErr = <-mutationDone:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mutationErr != nil {
				t.Fatalf("%s failed: %v", kind, mutationErr)
			}
			if inviteErr != nil && !errors.Is(inviteErr, newpeople.ErrNotFound) && !errors.Is(inviteErr, connection.ErrNotFound) {
				t.Fatalf("concurrent invitation failed unexpectedly: %v", inviteErr)
			}
			f.assertRevoked(source, peer, kind == "cancel")
			if kind == "block" && f.count(`SELECT count(*) FROM connection_requests WHERE sender_account_id=$1 AND recipient_account_id=$2 AND state='pending'`, f.ids[0], f.ids[1]) != 0 {
				t.Fatal("Block left a pending invitation")
			}
			if count := f.count(`SELECT count(*) FROM connection_requests WHERE sender_account_id=$1 AND recipient_account_id=$2`, f.ids[0], f.ids[1]); count > 1 {
				t.Fatalf("concurrent duplicate requests: %d", count)
			}
			if count := f.count(`SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, f.ids); count != 0 {
				t.Fatal("concurrency created Tie without recipient accept")
			}
			if count := f.count(`SELECT count(*) FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, f.ids); count != 0 {
				t.Fatal("concurrency created chat without explicit start")
			}
		})
	}
}
