package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"testing"
	"time"
)

func TestSocialNowNativeAcceptedTieAndVisibleIntent(t *testing.T) {
	pool := newPeopleTestPool(t)
	f := newPeoplePair(t, pool)
	request, err := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[1], "Owned explicit connection")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.DecideRequest(f.ctx, f.ids[1], request.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	shared, err := f.store.CreateSocialIntentDraft(f.ctx, f.ids[1], socialintent.DraftInput{Type: "FIND_COMPANION", Title: "Owned explicit friend intent", Constraints: json.RawMessage(`{}`), Audience: "FRIENDS", Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ActivateSocialIntent(f.ctx, f.ids[1], shared.ID); err != nil {
		t.Fatal(err)
	}
	t.Run("nativeHumanBindingWithoutAgentOrMachineGrant", func(t *testing.T) {
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		_ = token
		f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[0], digest[:])
		defer f.exec(`DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
		var agents int
		if e = pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.ids[0]).Scan(&agents); e != nil || agents != 0 {
			t.Fatalf("ordinary fixture unexpectedly has Agent: %d %v", agents, e)
		}
		actor := identity.Actor{ID: f.ids[0], AccountType: "person"}
		if ties, e := f.store.ListHumanTies(f.ctx, digest, actor); e != nil || len(ties) != 1 {
			t.Fatalf("ordinary current read: %+v %v", ties, e)
		}
		t.Run("validTieWithoutPeerProfileRemainsVisible", func(t *testing.T) {
			f.exec(`DELETE FROM user_profiles WHERE account_id=$1`, f.ids[1])
			defer f.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成新朋友测试','public')`, f.ids[1])
			ties, e := f.store.ListHumanTies(f.ctx, digest, actor)
			if e != nil || len(ties) != 1 || ties[0].OtherName != "Birdtie 成员" {
				t.Fatalf("legitimate no-Profile Tie lost: %+v %v", ties, e)
			}
		})
		if _, e = f.store.GetHumanVisibleSocialIntent(f.ctx, digest, actor, shared.ID); e != nil {
			t.Fatal(e)
		}
		for _, bad := range []identity.Actor{{ID: f.ids[1], AccountType: "person"}, {ID: f.ids[0], AccountType: "organization"}, {ID: "unknown", AccountType: "person"}} {
			if _, e = f.store.ListHumanTies(f.ctx, digest, bad); !errors.Is(e, identity.ErrUnauthorized) {
				t.Fatalf("unbound actor accepted: %+v %v", bad, e)
			}
		}
		if _, e = f.store.ListHumanTies(f.ctx, [32]byte{}, actor); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatalf("zero native credential accepted: %v", e)
		}
		cancelled, cancel := context.WithCancel(f.ctx)
		cancel()
		if _, e = f.store.ListHumanTies(cancelled, digest, actor); e == nil {
			t.Fatal("late cancelled read allowed")
		}
	})
	t.Run("creatorUsesOriginalOwnSurfaceAndVisibleACL", func(t *testing.T) {
		_, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[1], digest[:])
		defer f.exec(`DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
		actor := identity.Actor{ID: f.ids[1], AccountType: "person"}
		_, legacy := f.store.GetVisibleSocialIntent(f.ctx, actor.ID, shared.ID)
		_, current := f.store.GetHumanVisibleSocialIntent(f.ctx, digest, actor, shared.ID)
		if !errors.Is(legacy, socialintent.ErrNotFound) || !errors.Is(current, socialintent.ErrNotFound) {
			t.Fatalf("creator visible audience ACL changed: old=%v new=%v", legacy, current)
		}
		own, e := f.store.ListOwnSocialIntents(f.ctx, actor.ID)
		if e != nil || len(own) != 1 || own[0].ID != shared.ID {
			t.Fatalf("original own surface: %+v %v", own, e)
		}
	})
	t.Run("acceptedExplicitSource", func(t *testing.T) {
		ties, e := f.store.ListTies(f.ctx, f.ids[0])
		if e != nil || len(ties) != 1 || ties[0].OtherAccountID != f.ids[1] {
			t.Fatalf("ties=%+v err=%v", ties, e)
		}
		visible, e := f.store.GetVisibleSocialIntent(f.ctx, f.ids[0], shared.ID)
		if e != nil || visible.ID != shared.ID {
			t.Fatalf("visible=%+v err=%v", visible, e)
		}
	})
	t.Run("ordinaryStrangerCannotReadFriends", func(t *testing.T) {
		if _, e := f.store.GetVisibleSocialIntent(f.ctx, f.ids[2], shared.ID); e == nil {
			t.Fatal("Stranger read FRIENDS intent")
		}
	})
	t.Run("withdrawnRequestIsNotAcceptedFriend", func(t *testing.T) {
		// Change the actual request source while the old derived Tie remains.
		// This adversarial native source change is not a new user withdrawal API.
		f.exec(`UPDATE connection_requests SET state='withdrawn' WHERE id=$1`, request.ID)
		defer f.exec(`UPDATE connection_requests SET state='accepted' WHERE id=$1`, request.ID)
		ties, e := f.store.ListTies(f.ctx, f.ids[0])
		if e != nil {
			t.Fatal(e)
		}
		if len(ties) != 0 {
			t.Fatalf("nonaccepted request remains a current friend: %+v", ties)
		}
	})
	t.Run("blockRemovesBothSources", func(t *testing.T) {
		if e := f.store.BlockAccount(f.ctx, f.ids[0], f.ids[1]); e != nil {
			t.Fatal(e)
		}
		ties, e := f.store.ListTies(f.ctx, f.ids[0])
		if e != nil || len(ties) != 0 {
			t.Fatalf("blocked ties=%+v err=%v", ties, e)
		}
		if _, e = f.store.GetVisibleSocialIntent(f.ctx, f.ids[0], shared.ID); e == nil {
			t.Fatal("blocked FRIENDS intent visible")
		}
		f.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.ids[0], f.ids[1])
	})
}

// These sources are exclusively synthetic, native rows. They are neither pilot
// attendance nor evidence of a model processing purpose.
func TestSocialNowNativeFieldNamesAndExpiredCommunity(t *testing.T) {
	f := agentVisibilityTestFixture(t)
	b := f.private.base
	f.acceptedTie(t)
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	_ = token
	b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, b.other.ID, digest[:])
	defer b.exec(`DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
	actor := identity.Actor{ID: b.other.ID, AccountType: "person"}
	b.exec(`UPDATE accounts SET handle='sn-hidden-'||substring(id::text,1,8) WHERE id=$1`, b.person.ID)
	b.exec(`UPDATE user_profiles SET display_name='明确公开姓名',visibility='private' WHERE account_id=$1`, b.person.ID)
	t.Run("fieldPublicNameDespiteCoarsePrivate", func(t *testing.T) {
		rules := agentprofile.DefaultFieldRules()
		rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
		replaceVisibilityRules(t, f, rules)
		ties, e := b.store.ListHumanTies(b.ctx, digest, actor)
		if e != nil || len(ties) != 1 || ties[0].OtherName != "明确公开姓名" {
			t.Fatalf("lawful field name lost: %+v %v", ties, e)
		}
	})
	t.Run("fieldPrivateNeverHandleFallback", func(t *testing.T) {
		rules := agentprofile.DefaultFieldRules()
		rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
		replaceVisibilityRules(t, f, rules)
		ties, e := b.store.ListHumanTies(b.ctx, digest, actor)
		if e != nil || len(ties) != 1 || ties[0].OtherName != "Birdtie 成员" {
			t.Fatalf("field DENY not generic: %+v %v", ties, e)
		}
	})
	intent, e := b.store.CreateSocialIntentDraft(b.ctx, b.person.ID, socialintent.DraftInput{Type: "FIND_COMPANION", Title: "合成社群意图", Constraints: json.RawMessage(`{}`), Audience: "COMMUNITY", CommunityID: f.communityID, Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	defer b.exec(`DELETE FROM social_intents WHERE id=$1`, intent.ID)
	if _, e = b.store.ActivateSocialIntent(b.ctx, b.person.ID, intent.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.GetHumanVisibleSocialIntent(b.ctx, digest, actor, intent.ID); e != nil {
		t.Fatal(e)
	}
	t.Run("expiredCommunityHidesExplicitIntentAndMembershipSignal", func(t *testing.T) {
		b.exec(`UPDATE communities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.communityID)
		if _, e = b.store.GetHumanVisibleSocialIntent(b.ctx, digest, actor, intent.ID); !errors.Is(e, socialintent.ErrNotFound) {
			t.Errorf("expired community intent visible: %v", e)
		}
		p, e := b.store.humanSocialRead(b.ctx, digest, actor, "opportunities", "")
		if e != nil {
			t.Fatal(e)
		}
		if p.Inputs.JoinedCommunities[f.communityID] {
			t.Error("expired community retained membership ranking signal")
		}
	})
}
func TestSocialNowNativeLocalCityAndExpiry(t *testing.T) {
	pool := newPeopleTestPool(t)
	f := newPeoplePair(t, pool)
	f.scopes()
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	_ = token
	f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[0], digest[:])
	defer f.exec(`DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
	f.exec(`UPDATE places SET name='Owned Sports' WHERE id=ANY($1::uuid[])`, f.places)
	actor := identity.Actor{ID: f.ids[0], AccountType: "person"}
	var activities []string
	defer func() {
		for _, id := range activities {
			f.exec(`DELETE FROM audit_events WHERE resource_type='activity' AND resource_id=$1`, id)
			f.exec(`DELETE FROM activities WHERE id=$1`, id)
		}
	}()
	for i := 0; i < 2; i++ {
		a, e := f.store.CreateSocialDraft(f.ctx, f.ids[1], activitypublish.Input{CityID: f.cities[i], PlaceID: f.places[i], Title: "合成同文本跨城活动", CategoryCode: "badminton", Visibility: "public", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour), TimeZone: "Europe/London", Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.ids[1]}})
		if e != nil {
			t.Fatal(e)
		}
		activities = append(activities, a.ID)
		if _, e = f.store.PublishSocialActivity(f.ctx, f.ids[1], a.ID); e != nil {
			t.Fatal(e)
		}
	}
	intent, e := f.store.CreateSocialIntentDraft(f.ctx, f.ids[0], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "明确当前城市", Constraints: json.RawMessage(`{"category":"badminton","areaLabel":"Owned Sports"}`), Audience: "LOCAL", CityID: f.cities[0], Modality: "IN_PERSON", ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.ActivateSocialIntent(f.ctx, f.ids[0], intent.ID); e != nil {
		t.Fatal(e)
	}
	t.Run("localTargetDTOAndSupplyIntersect", func(t *testing.T) {
		p, e := f.store.humanSocialRead(f.ctx, digest, actor, "opportunities", "")
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Inputs.Intents) != 1 || p.Inputs.Intents[0].CityID != f.cities[0] {
			t.Errorf("actual LOCAL city omitted: %+v", p.Inputs.Intents)
		}
		if len(p.Inputs.Supply) != 1 || p.Inputs.Supply[0].Activity.CityID != f.cities[0] {
			t.Errorf("LOCAL crossed explicit city: %+v", p.Inputs.Supply)
		}
	})
	t.Run("conflictingExplicitContextCannotOverrideLocalCity", func(t *testing.T) {
		f.exec(`UPDATE social_intents SET context_id=$1 WHERE id=$2`, f.cityContexts[1], intent.ID)
		defer f.exec(`UPDATE social_intents SET context_id=NULL WHERE id=$1`, intent.ID)
		out, e := f.store.ListHumanOpportunities(f.ctx, digest, actor)
		if e != nil {
			t.Fatal(e)
		}
		if len(out) != 0 {
			t.Errorf("conflicting explicit cities yield candidates: %+v", out)
		}
	})
	t.Run("expiredCityRemovesSupplyAndDeclaredSource", func(t *testing.T) {
		f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation) VALUES($1,$2,'current')`, f.ids[0], f.cityContexts[0])
		f.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.cities[0])
		p, e := f.store.humanSocialRead(f.ctx, digest, actor, "opportunities", "")
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Inputs.Supply) != 0 || len(p.Inputs.DeclaredCities) != 0 {
			t.Errorf("expired explicit city remains usable: %+v", p.Inputs)
		}
	})
}

// Both operations are real Store methods. The Profile row barrier lets the
// human writer hold Account/Session; the validator must wait on Account first.
func TestSocialNowNativeFinalValidationAndHumanProfileLockOrder(t *testing.T) {
	pool := newPeopleTestPool(t)
	f := newPeoplePair(t, pool)
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[0], digest[:])
	defer f.exec(`DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
	actor := identity.Actor{ID: f.ids[0], AccountType: "person"}
	var before int64
	if e = pool.QueryRow(f.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	lock, e := pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(f.ctx)
	var id string
	if e = lock.QueryRow(f.ctx, `SELECT account_id FROM user_profiles WHERE account_id=$1 FOR UPDATE`, actor.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	deadlineCtx, cancel := context.WithTimeout(f.ctx, 6*time.Second)
	defer cancel()
	writerDone := make(chan error, 1)
	go func() {
		_, e := f.store.UpdateHumanProfile(deadlineCtx, digest, actor, identity.ProfileInput{DisplayName: "合成锁序明确姓名", Visibility: "public"})
		writerDone <- e
	}()
	var writerPID int32
	for limit := time.Now().Add(3 * time.Second); ; {
		e = pool.QueryRow(f.ctx, `SELECT coalesce((SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%SELECT p.account_id%' LIMIT 1),0)`, int(lock.Conn().PgConn().PID())).Scan(&writerPID)
		if e != nil {
			t.Fatal(e)
		}
		if writerPID != 0 {
			break
		}
		if time.Now().After(limit) {
			t.Fatal("actual human Profile source wait not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
	validatorDone := make(chan error, 1)
	go func() { validatorDone <- f.store.ValidateHumanSocialResponse(deadlineCtx, digest, actor) }()
	for limit := time.Now().Add(3 * time.Second); ; {
		var waiting bool
		e = pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%SELECT id FROM accounts%' AND query LIKE '%FOR SHARE%')`, writerPID).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(limit) {
			t.Fatal("final validator did not wait on Account before Session")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-writerDone; e != nil {
		t.Fatal("actual human Profile writer:", e)
	}
	if e = <-validatorDone; e != nil {
		t.Fatal("actual final validator:", e)
	}
	var after int64
	if e = pool.QueryRow(f.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&after); e != nil || before != after {
		t.Fatalf("native deadlock delta: %d -> %d %v", before, after, e)
	}
}
