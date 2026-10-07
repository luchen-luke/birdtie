package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The action read is a proposal, not a substitute for binding the actual
// domain write to the terms that the person reviewed.
func TestEntityActionNativeJoinChangedAfterFinalReadWhileWriterWaits(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
	ref := ea.Ref{Type: "activity", ID: f.public}
	r, err := b.store.ReadEntityActions(b.ctx, a, ref)
	if err != nil {
		_, diagnostic := b.pool.Exec(b.ctx, entityActionSQL(ref.Type), ref.ID, a.Actor.ID, a.SessionDigest[:], false, ref.Type, false)
		t.Log("current native SQL diagnostic", diagnostic)
		t.Fatal(err)
	}
	if err = b.store.RevalidateEntityActions(b.ctx, a, ref, r); err != nil {
		t.Fatal(err)
	}
	hold, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(context.Background())
	if _, err = hold.Exec(b.ctx, `SELECT id FROM activities WHERE id=$1 FOR UPDATE`, ref.ID); err != nil {
		t.Fatal(err)
	}
	cfg := b.pool.Config().Copy()
	name := "actn-join-final-" + ref.ID
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, err := pgxpool.NewWithConfig(b.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool, false)
	done := make(chan error, 1)
	condition := ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Join, Operation: "JOIN"}
	go func() { _, _, e := store.JoinActivityBound(b.ctx, a, ref.ID, condition); done <- e }()
	waiting := false
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, name).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("original activity writer did not actually wait")
	}
	if _, err = hold.Exec(b.ctx, `UPDATE activities SET price_minor=1900,starts_at=starts_at+interval '1 hour',ends_at=ends_at+interval '1 hour' WHERE id=$1`, ref.ID); err != nil {
		t.Fatal(err)
	}
	if err = hold.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-done:
		if !errors.Is(e, ea.ErrChanged) {
			t.Fatal("changed concrete approval must fail with ErrChanged, not SQL failure", e)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("original writer did not finish")
	}
	var effects int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2 AND status IN ('going','pending')`, ref.ID, b.person.ID).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 0 {
		t.Fatalf("old concrete approval applied changed fee/date after writer lock wait: effects=%d", effects)
	}
}

func TestEntityActionNativeCitylessCommunitySaveUnavailable(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	c, err := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{Name: "合成无城市社群", Summary: "仅独占本地数据库", Visibility: "public", JoinPolicy: "open"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.exec(`DELETE FROM communities WHERE id=$1`, c.ID) })
	if c.CityID != nil {
		t.Fatal("fixture invented city")
	}
	a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
	r, err := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "community", ID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.store.Save(b.ctx, b.person.ID, "group", c.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("original cityless save contract", err)
	}
	if actionNativeFlag(t, r.View, ea.Save).State != ea.Unavailable {
		t.Fatal("cityless community falsely advertises original unavailable Save")
	}
}

func TestEntityActionNativeBoundJoinCancelStableIDAndExpiry(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
	ref := ea.Ref{Type: "activity", ID: f.public}
	read := func(op string) ea.BoundCondition {
		r, e := b.store.ReadEntityActions(b.ctx, a, ref)
		if e != nil {
			t.Fatal(e)
		}
		return ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Join, Operation: op}
	}
	condition := read("JOIN")
	out, changed, e := b.store.JoinActivityBound(b.ctx, a, ref.ID, condition)
	if e != nil || !changed || out.Status != "going" {
		t.Fatal("bound original join", out, changed, e)
	}
	if _, _, e = b.store.JoinActivityBound(b.ctx, a, ref.ID, condition); !errors.Is(e, ea.ErrChanged) {
		t.Fatal("old reviewed JOIN must not become CANCEL or replay", e)
	}
	cancel := read("CANCEL_RSVP")
	gone, e := b.store.CancelParticipationBound(b.ctx, a, ref.ID, cancel)
	if e != nil || gone.ID != out.ID || gone.Status != "cancelled" {
		t.Fatal("bound original cancel", gone, e)
	}
	// The old unbound writer remains idempotent; it is not claimed to protect
	// a new UI's concrete reviewed source/version.
	again, e := b.store.CancelParticipation(b.ctx, a.Actor.ID, ref.ID)
	if e != nil || again.ID != out.ID || !again.UpdatedAt.Equal(gone.UpdatedAt) {
		t.Fatal("legacy original cancel idempotency", again, e)
	}
	condition = read("JOIN")
	condition.ValidUntil = condition.ValidUntil.Add(-time.Minute)
	if _, _, e = b.store.JoinActivityBound(b.ctx, a, ref.ID, condition); !errors.Is(e, ea.ErrChanged) {
		t.Fatal("expired approval", e)
	}
	wrong := a
	wrong.SessionDigest = f.place.private.peer.SessionDigest
	if _, _, e = b.store.JoinActivityBound(b.ctx, wrong, ref.ID, read("JOIN")); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("cross-owner session must be Unauthorized", e)
	}
}

func TestEntityActionNativeBoundOriginalSaveCommunityFriend(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
	read := func(ref ea.Ref, kind, op string) ea.BoundCondition {
		r, e := b.store.ReadEntityActions(b.ctx, a, ref)
		if e != nil {
			t.Fatal(e)
		}
		return ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: kind, Operation: op}
	}
	ref := ea.Ref{Type: "place", ID: f.place.place}
	saveCondition := read(ref, ea.Save, "SAVE")
	id, e := b.store.SaveBound(b.ctx, a, "place", ref.ID, saveCondition)
	if e != nil {
		t.Fatal("original bound save", e)
	}
	if e = b.store.RemoveSavedBound(b.ctx, a, id, read(ref, ea.Save, "UNSAVE")); e != nil {
		t.Fatal("original bound unsave", e)
	}
	c, e := b.store.CreateSocialCommunity(b.ctx, b.other.ID, community.SocialInput{Name: "合成版本化邀请", Summary: "仅本地", Visibility: "hidden", JoinPolicy: "invite_only"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { b.exec(`DELETE FROM communities WHERE id=$1`, c.ID) })
	if _, e = b.store.InviteSocialMember(b.ctx, b.other.ID, c.ID, b.person.ID); e != nil {
		t.Fatal(e)
	}
	ref = ea.Ref{Type: "community", ID: c.ID}
	cond := read(ref, ea.Join, "DECLINE_INVITATION")
	out, e := b.store.MutateHumanCommunity(b.ctx, a.SessionDigest, a.Actor, community.HumanCommand{Operation: "leave", CommunityID: c.ID, ActionCondition: &cond})
	if e != nil {
		t.Fatal("original bound decline", out, e)
	}
	var status string
	if e = b.pool.QueryRow(b.ctx, `SELECT status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, c.ID, b.person.ID).Scan(&status); e != nil || status != "left" {
		t.Fatal("decline state", status, e)
	}
	ref = ea.Ref{Type: "person", ID: b.other.ID}
	friendCondition := read(ref, ea.Connect, "REQUEST_FRIEND")
	request, e := b.store.CreateFriendRequestBound(b.ctx, a, ref.ID, "合成本人明确输入", friendCondition)
	if e != nil {
		t.Fatal("original bound friend", e)
	}
	t.Cleanup(func() {
		b.exec(`DELETE FROM audit_events WHERE resource_id=$1`, request.ID)
		b.exec(`DELETE FROM connection_requests WHERE id=$1`, request.ID)
	})
	if request.ID == "" || request.OtherAccountID != ref.ID || request.Note != "合成本人明确输入" || request.Scope != "friend" {
		t.Fatal("wrong original request", request)
	}
	if _, e = b.store.CreateFriendRequestBound(b.ctx, a, ref.ID, "合成本人明确输入", friendCondition); !errors.Is(e, ea.ErrChanged) {
		t.Fatal("old concrete friend request reused", e)
	}
}

func TestEntityActionNativeBoundFriendExpiredHousekeeping(t *testing.T) {
	for _, mode := range []string{"unrelatedExpired", "sameTargetExpired"} {
		t.Run(mode, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
			recipient := b.other.ID
			if mode == "unrelatedExpired" {
				if err := b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&recipient); err != nil {
					t.Fatal(err)
				}
				b.accounts = append(b.accounts, recipient)
			}
			var oldID string
			if err := b.pool.QueryRow(b.ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,created_at,expires_at) VALUES($1,$2,NULL,'保留的合成旧申请','friend','pending',clock_timestamp()-interval '2 days',clock_timestamp()-interval '1 day') RETURNING id`, a.Actor.ID, recipient).Scan(&oldID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				b.exec(`DELETE FROM audit_events WHERE resource_id IN(SELECT id::text FROM connection_requests WHERE sender_account_id=$1)`, a.Actor.ID)
				b.exec(`DELETE FROM connection_requests WHERE sender_account_id=$1`, a.Actor.ID)
			})
			r, err := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "person", ID: b.other.ID})
			if err != nil {
				t.Fatal(err)
			}
			condition := ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Connect, Operation: "REQUEST_FRIEND"}
			request, err := b.store.CreateFriendRequestBound(b.ctx, a, b.other.ID, "明确的新合成申请", condition)
			if err != nil {
				t.Fatal("original expired housekeeping must permit reviewed new request", err)
			}
			if request.ID == "" || request.ID == oldID || request.Scope != "friend" || request.State != "pending" {
				t.Fatal("wrong original new request", request)
			}
			var exact bool
			if err = b.pool.QueryRow(b.ctx, `SELECT state='expired' AND note='保留的合成旧申请' AND decided_at IS NOT NULL FROM connection_requests WHERE id=$1`, oldID).Scan(&exact); err != nil || !exact {
				t.Fatal("old request must remain as exact expired original row", exact, err)
			}
		})
	}
}

func TestEntityActionNativeBoundFriendTailAfterRecipientFKWait(t *testing.T) {
	for _, mutation := range []string{"fieldMemberABA", "fieldCommunityABA", "fieldNaturalExpiry"} {
		t.Run(mutation, func(t *testing.T) {
			f := agentVisibilityTestFixture(t)
			b := f.private.base
			rules := agentprofile.DefaultFieldRules()
			rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{f.communityID}}
			replaceVisibilityRules(t, f, rules)
			a := ea.Access{Actor: identity.Actor{ID: b.other.ID, AccountType: "person"}, SessionDigest: f.private.peer.SessionDigest}
			ref := ea.Ref{Type: "person", ID: b.person.ID}
			barrier := entityActionTailAuditBarrier(t, b, a.Actor.ID, "human_friend", "request", "connection_request", "")
			defer barrier.close()
			if mutation == "fieldNaturalExpiry" {
				b.exec(`UPDATE communities SET expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE id=$1`, f.communityID)
			}
			r, err := b.store.ReadEntityActions(b.ctx, a, ref)
			if err != nil {
				t.Fatal(err)
			}
			condition := ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Connect, Operation: "REQUEST_FRIEND"}
			// Include exact original rows/xmin for every side effect family,
			// rather than accepting an empty active-request count after rollback.
			snapshot := func() string {
				var value string
				if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
 'requests',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text) ORDER BY r.id),'[]') FROM connection_requests r WHERE r.sender_account_id IN($1,$2) OR r.recipient_account_id IN($1,$2)),
 'audit',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text) ORDER BY r.id),'[]') FROM audit_events r WHERE r.actor_account_id IN($1,$2)),
 'notifications',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text) ORDER BY r.id),'[]') FROM native_notification_decisions r WHERE r.actor_id IN($1,$2) OR r.recipient_id IN($1,$2)),
 'inbox',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text) ORDER BY r.id),'[]') FROM inbox_items r WHERE r.recipient_account_id IN($1,$2)),
 'outbox',(SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text) ORDER BY r.event_id),'[]') FROM agent_domain_outbox r WHERE r.actor_id IN($1,$2)))::text`, a.Actor.ID, ref.ID).Scan(&value); e != nil {
					t.Fatal(e)
				}
				return value
			}
			before := snapshot()
			store := New(barrier.pool, false)
			done := make(chan error, 1)
			go func() {
				_, e := store.CreateFriendRequestBound(barrier.ctx, a, ref.ID, "明确的本地合成申请", condition)
				done <- e
			}()
			barrier.wait(done)
			switch mutation {
			case "fieldMemberABA":
				b.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, ref.ID)
				b.exec(`UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, ref.ID)
			case "fieldCommunityABA":
				b.exec(`UPDATE communities SET summary=summary WHERE id=$1`, f.communityID)
			case "fieldNaturalExpiry":
				time.Sleep(time.Until(r.View.ValidUntil) + 50*time.Millisecond)
			}
			if err = barrier.hold.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if !errors.Is(err, ea.ErrChanged) {
					t.Fatal("late changed source must reject with native ErrChanged", err)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("original writer did not finish")
			}
			if after := snapshot(); after != before {
				t.Fatal("failed original write must rollback all request/audit/notification/inbox/outbox rows and xmin")
			}
		})
	}
}

func TestEntityActionNativeBoundOpenChatStableIDAndLateTieWait(t *testing.T) {
	for _, mode := range []string{"positive", "targetProfileABA", "currentBlock", "sessionNaturalExpiry"} {
		t.Run(mode, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
			request, e := b.store.CreateFriendRequest(b.ctx, a.Actor.ID, b.other.ID, "本地明确申请")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = b.store.DecideRequest(b.ctx, b.other.ID, request.ID, "accept"); e != nil {
				t.Fatal(e)
			}
			ties, e := b.store.ListTies(b.ctx, a.Actor.ID)
			if e != nil || len(ties) != 1 {
				t.Fatal(ties, e)
			}
			t.Cleanup(func() {
				entityActionTailCleanup(t, b.pool, `DELETE FROM conversations WHERE request_id=$1`, request.ID)
				entityActionTailCleanup(t, b.pool, `DELETE FROM person_ties WHERE request_id=$1`, request.ID)
				entityActionTailCleanup(t, b.pool, `DELETE FROM audit_events WHERE actor_account_id IN($1,$2)`, a.Actor.ID, b.other.ID)
				entityActionTailCleanup(t, b.pool, `DELETE FROM connection_requests WHERE id=$1`, request.ID)
				entityActionTailCleanup(t, b.pool, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, a.Actor.ID, b.other.ID)
			})
			if mode == "sessionNaturalExpiry" {
				b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1200 milliseconds',idle_expires_at=clock_timestamp()+interval '1000 milliseconds' WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			r, e := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "person", ID: b.other.ID})
			if e != nil {
				t.Fatal(e)
			}
			condition := ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Message, Operation: "OPEN_CHAT"}
			if mode == "positive" {
				chat, e := b.store.StartFriendConversationBound(b.ctx, a, ties[0].ID, condition)
				if e != nil || chat.ID == "" || chat.OtherAccountID != b.other.ID {
					t.Fatal(chat, e)
				}
				if _, e = b.store.StartFriendConversationBound(b.ctx, a, ties[0].ID, condition); !errors.Is(e, ea.ErrChanged) {
					t.Fatal("old source before conversation creation must not be silently renewed", e)
				}
				fresh, e := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "person", ID: b.other.ID})
				if e != nil {
					t.Fatal(e)
				}
				condition = ea.BoundCondition{SourceVersion: fresh.View.SourceVersion, ValidUntil: fresh.View.ValidUntil, Kind: ea.Message, Operation: "OPEN_CHAT"}
				again, e := b.store.StartFriendConversationBound(b.ctx, a, ties[0].ID, condition)
				if e != nil || again.ID != chat.ID {
					t.Fatal("same original conversation must be reused", again, e)
				}
				var count int
				if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, chat.ID).Scan(&count); e != nil || count != 0 {
					t.Fatal("opening conversation must not send any message", count, e)
				}
				return
			}
			if mode == "targetProfileABA" || mode == "currentBlock" {
				// Routing holds SHARE locks on Profile/Block. External mutations
				// here would wait behind the writer, not commit after its read.
				// This explicitly labelled same-Tx trigger disturbance tests the
				// final source proof after the actual native audit wait instead.
				disturbance := fmt.Sprintf("UPDATE user_profiles SET display_name=display_name WHERE account_id='%s'::uuid;", b.other.ID)
				if mode == "currentBlock" {
					disturbance = fmt.Sprintf("INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES('%s'::uuid,'%s'::uuid);", a.Actor.ID, b.other.ID)
				}
				barrier := entityActionTailAuditBarrier(t, b, a.Actor.ID, "human_friend_chat", "start", "conversation", disturbance)
				defer barrier.close()
				done := make(chan error, 1)
				go func() {
					_, err := New(barrier.pool, false).StartFriendConversationBound(barrier.ctx, a, ties[0].ID, condition)
					done <- err
				}()
				barrier.wait(done)
				if e = barrier.hold.Commit(b.ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case e = <-done:
					if !errors.Is(e, ea.ErrChanged) {
						t.Fatal("same-Tx late source disturbance must fail exact final source proof", e)
					}
				case <-time.After(4 * time.Second):
					t.Fatal("chat writer did not finish")
				}
				var count int
				if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM conversations WHERE request_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$2 AND purpose='human_friend_chat')`, request.ID, a.Actor.ID).Scan(&count); e != nil || count != 0 {
					t.Fatal("late denial must create no conversation or audit", count, e)
				}
				var original bool
				if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2)`, a.Actor.ID, b.other.ID).Scan(&original); e != nil || !original {
					t.Fatal("same-Tx block disturbance must rollback", original, e)
				}
				return
			}
			hold, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Rollback(context.Background())
			var holderPID int
			if e = hold.QueryRow(b.ctx, `SELECT pg_backend_pid() FROM person_ties WHERE id=$1 FOR UPDATE`, ties[0].ID).Scan(&holderPID); e != nil {
				t.Fatal(e)
			}
			cfg := b.pool.Config().Copy()
			name := "actn-chat-tie-" + ties[0].ID
			cfg.ConnConfig.RuntimeParams["application_name"] = name
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = hold.Rollback(context.Background()); pool.Close() }()
			store := New(pool, false)
			done := make(chan error, 1)
			go func() { _, err := store.StartFriendConversationBound(b.ctx, a, ties[0].ID, condition); done <- err }()
			waiting := false
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND $2=ANY(pg_blocking_pids(pid)) AND query LIKE '%FOR UPDATE OF t%')`, name, holderPID).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("actual original Tie row wait absent")
			}
			want := ea.ErrChanged
			switch mode {
			case "sessionNaturalExpiry":
				time.Sleep(time.Until(r.View.ValidUntil) + 50*time.Millisecond)
				want = identity.ErrUnauthorized
			}
			if e = hold.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if !errors.Is(e, want) {
					t.Fatal("exact late current source classification", e, want)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("chat writer did not finish")
			}
			var count int
			if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM conversations WHERE request_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$2 AND purpose='human_friend_chat')`, request.ID, a.Actor.ID).Scan(&count); e != nil || count != 0 {
				t.Fatal("late denial must create no conversation or audit", count, e)
			}
		})
	}
}

func TestEntityActionNativeBoundConversationOriginalIntentCityAndLateSource(t *testing.T) {
	for _, mode := range []string{"positive", "wrongCity", "intentABAAfterFKWait", "cityABAAfterFKWait", "intentNaturalExpiry"} {
		t.Run(mode, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
			var intentID string
			if e := b.pool.QueryRow(b.ctx, `INSERT INTO intents(owner_account_id,city_id,topic,details,available_from,available_until,time_zone,coarse_area_label,audience,state,expires_at,owner_confirmed_at) VALUES($1,$2,'合成明确私信意图','不得进入动作合同正文',clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 day','Europe/London','城市范围','public','active',clock_timestamp()+interval '1 day',clock_timestamp()) RETURNING id`, b.other.ID, f.place.city).Scan(&intentID); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				entityActionTailCleanup(t, b.pool, `DELETE FROM audit_events WHERE actor_account_id=$1`, a.Actor.ID)
				entityActionTailCleanup(t, b.pool, `DELETE FROM connection_requests WHERE sender_account_id=$1`, a.Actor.ID)
				entityActionTailCleanup(t, b.pool, `DELETE FROM intents WHERE id=$1`, intentID)
			})
			var barrier *entityActionTailBarrier
			if mode != "positive" && mode != "wrongCity" {
				// Intent/City SHARE locks serialize external changes. This
				// same-Tx trigger disturbance is not an external committed ABA.
				disturbance := ""
				if mode == "intentABAAfterFKWait" {
					disturbance = fmt.Sprintf("UPDATE intents SET state='withdrawn' WHERE id='%s'::uuid;UPDATE intents SET state='active' WHERE id='%s'::uuid;", intentID, intentID)
				}
				if mode == "cityABAAfterFKWait" {
					disturbance = fmt.Sprintf("UPDATE cities SET name=name WHERE id='%s';", f.place.city)
				}
				barrier = entityActionTailAuditBarrier(t, b, a.Actor.ID, "human_contact", "request", "connection_request", disturbance)
				defer barrier.close()
			}
			if mode == "intentNaturalExpiry" {
				b.exec(`UPDATE intents SET available_until=clock_timestamp()+interval '1200 milliseconds' WHERE id=$1`, intentID)
			}
			ref := ea.Ref{Type: "person", ID: b.other.ID}
			r, e := b.store.ReadEntityActions(b.ctx, a, ref)
			if e != nil {
				t.Fatal(e)
			}
			if !r.View.Valid() {
				t.Fatal("invalid two-operation CONNECT", r.View)
			}
			d := actionNativeFlag(t, r.View, ea.Connect)
			found := false
			for _, op := range d.AllowedOperations {
				found = found || op == "REQUEST_CONVERSATION"
			}
			if d.State != ea.Available || !found {
				t.Fatal("original public intent conversation operation absent", d)
			}
			condition := ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Connect, Operation: "REQUEST_CONVERSATION"}
			if mode == "positive" || mode == "wrongCity" {
				city := f.place.city
				if mode == "wrongCity" {
					city = "not-the-reviewed-city"
				}
				out, e := b.store.CreateRequestBound(b.ctx, a, ref.ID, city, "明确申请私信而非好友", condition)
				if mode == "wrongCity" {
					if !errors.Is(e, ea.ErrChanged) {
						t.Fatal("different city must reject", e)
					}
					return
				}
				if e != nil || out.ID == "" || out.CityID != city || out.Scope != "conversation" || out.Note != "明确申请私信而非好友" {
					t.Fatal("wrong original conversation request", out, e)
				}
				var ties int
				if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM person_ties WHERE person_a_account_id=LEAST($1::uuid,$2::uuid) AND person_b_account_id=GREATEST($1::uuid,$2::uuid)`, a.Actor.ID, ref.ID).Scan(&ties); e != nil || ties != 0 {
					t.Fatal("requesting private conversation must not create friend Tie", ties, e)
				}
				return
			}
			store := New(barrier.pool, false)
			done := make(chan error, 1)
			go func() {
				_, err := store.CreateRequestBound(barrier.ctx, a, ref.ID, f.place.city, "明确私信", condition)
				done <- err
			}()
			barrier.wait(done)
			switch mode {
			case "intentNaturalExpiry":
				time.Sleep(time.Until(r.View.ValidUntil) + 50*time.Millisecond)
			}
			if e = barrier.hold.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if !errors.Is(e, ea.ErrChanged) {
					t.Fatal("late original Intent/City change must reject", e)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("writer did not finish")
			}
			var effects int
			if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM connection_requests WHERE sender_account_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='human_contact')+(SELECT count(*) FROM native_notification_decisions WHERE actor_id=$1 AND kind='connection_request')`, a.Actor.ID).Scan(&effects); e != nil || effects != 0 {
				t.Fatal("denied conversation must rollback all original effects", effects, e)
			}
		})
	}
}

func TestEntityActionNativeBoundCommunitySpecificConsequences(t *testing.T) {
	for _, mode := range []string{"ACCEPT_INVITATION", "DECLINE_INVITATION", "REQUEST_JOIN", "JOIN"} {
		t.Run(mode, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
			policy := "invite_only"
			if mode == "REQUEST_JOIN" {
				policy = "request"
			}
			if mode == "JOIN" {
				policy = "open"
			}
			c, e := b.store.CreateSocialCommunity(b.ctx, b.other.ID, community.SocialInput{Name: "合成明确后果", Summary: "独占库", Visibility: "public", JoinPolicy: policy})
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { b.exec(`DELETE FROM communities WHERE id=$1`, c.ID) })
			var originalMemberID string
			if policy == "invite_only" {
				if _, e = b.store.InviteSocialMember(b.ctx, b.other.ID, c.ID, a.Actor.ID); e != nil {
					t.Fatal(e)
				}
				if e = b.pool.QueryRow(b.ctx, `SELECT id FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, c.ID, a.Actor.ID).Scan(&originalMemberID); e != nil {
					t.Fatal(e)
				}
			}
			r, e := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "community", ID: c.ID})
			if e != nil {
				t.Fatal(e)
			}
			condition := ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Join, Operation: mode}
			operation := "join"
			want := "active"
			if mode == "DECLINE_INVITATION" {
				operation = "leave"
				want = "left"
			}
			if mode == "REQUEST_JOIN" {
				want = "pending"
			}
			out, e := b.store.MutateHumanCommunity(b.ctx, a.SessionDigest, a.Actor, community.HumanCommand{Operation: operation, CommunityID: c.ID, ActionCondition: &condition})
			if e != nil {
				t.Fatal("specific reviewed operation", mode, e)
			}
			var resultMemberID, resultStatus string
			if e = b.pool.QueryRow(b.ctx, `SELECT id,status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, c.ID, a.Actor.ID).Scan(&resultMemberID, &resultStatus); e != nil {
				t.Fatal(e)
			}
			if resultStatus != want || resultMemberID == "" || (originalMemberID != "" && resultMemberID != originalMemberID) {
				t.Fatal("exact native approved consequence and original membership ID", mode, resultMemberID, resultStatus)
			}
			if operation == "join" && (out.Member == nil || out.Member.ID != resultMemberID || out.Member.Status != want) {
				t.Fatal("original join response must match native row", out)
			}
			if !condition.ValidUntil.Equal(r.View.ValidUntil) {
				t.Fatal("original approval deadline extended")
			}
			if mode == "JOIN" || mode == "REQUEST_JOIN" || mode == "ACCEPT_INVITATION" {
				fresh, e := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "community", ID: c.ID})
				if e != nil {
					t.Fatal(e)
				}
				leave := ea.BoundCondition{SourceVersion: fresh.View.SourceVersion, ValidUntil: fresh.View.ValidUntil, Kind: ea.Join, Operation: "LEAVE"}
				gone, e := b.store.MutateHumanCommunity(b.ctx, a.SessionDigest, a.Actor, community.HumanCommand{Operation: "leave", CommunityID: c.ID, ActionCondition: &leave})
				var goneID, goneStatus string
				if readErr := b.pool.QueryRow(b.ctx, `SELECT id,status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, c.ID, a.Actor.ID).Scan(&goneID, &goneStatus); readErr != nil {
					t.Fatal(readErr)
				}
				if e != nil || goneID != resultMemberID || goneStatus != "left" {
					t.Fatal("exact original leave transition", gone, e)
				}
			}
		})
	}
}

func TestEntityActionNativeBusinessVenueNavigationCurrentSource(t *testing.T) {
	for _, mutation := range []string{"valid", "relationRevoked", "venueExpired", "candidateWithdrawn", "operatorHidden", "operatorABA"} {
		t.Run(mutation, func(t *testing.T) {
			f := newSponsorFixture(t, true)
			f.b.exec(t, `UPDATE places SET latitude=0,longitude=0,coordinate_system='wgs84',location_precision='point' WHERE id=$1`, f.place)
			access := ea.Access{Public: true}
			ref := ea.Ref{Type: "activity", ID: f.activity}
			r, err := f.b.store.ReadEntityActions(f.b.ctx, access, ref)
			if err != nil {
				t.Fatal(err)
			}
			if actionNativeFlag(t, r.View, ea.Navigate).State != ea.Available {
				t.Fatal("valid original venue not navigable")
			}
			switch mutation {
			case "relationRevoked":
				f.b.exec(t, `UPDATE business_venue_relations SET status='revoked' WHERE business_id=$1 AND place_id=$2`, f.business, f.place)
			case "venueExpired":
				f.b.exec(t, `UPDATE venues SET expires_at=clock_timestamp()-interval '1 millisecond' WHERE place_id=$1`, f.place)
			case "candidateWithdrawn":
				f.b.exec(t, `UPDATE venue_candidates SET status='rejected' WHERE place_id=$1`, f.place)
			case "operatorHidden":
				f.b.exec(t, `UPDATE organizations SET status='suspended' WHERE id=$1`, f.operator)
			case "operatorABA":
				f.b.exec(t, `UPDATE organizations SET status='suspended' WHERE id=$1`, f.operator)
				f.b.exec(t, `UPDATE organizations SET status='active' WHERE id=$1`, f.operator)
			}
			if mutation == "valid" {
				if err = f.b.store.RevalidateEntityActions(f.b.ctx, access, ref, r); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err = f.b.store.RevalidateEntityActions(f.b.ctx, access, ref, r); err == nil {
				t.Fatal("old public venue approval survived", mutation)
			}
			if mutation == "operatorABA" {
				return
			}
			n, err := f.b.store.ReadEntityActions(f.b.ctx, access, ref)
			if errors.Is(err, ea.ErrNotFound) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if actionNativeFlag(t, n.View, ea.Navigate).State != ea.Unavailable {
				t.Fatal("invalid venue advertised navigation", mutation)
			}
		})
	}
}

func TestEntityActionNativeInvitationOriginalAcceptDeclineAndABA(t *testing.T) {
	for _, decision := range []string{"accept", "decline", "revokeABA"} {
		t.Run(decision, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			c, err := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{Name: "合成邀请社群", Summary: "仅本地一次性", Visibility: "hidden", JoinPolicy: "invite_only"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { b.exec(`DELETE FROM communities WHERE id=$1`, c.ID) })
			if _, err = b.store.InviteSocialMember(b.ctx, b.person.ID, c.ID, b.other.ID); err != nil {
				t.Fatal(err)
			}
			a := ea.Access{Actor: identity.Actor{ID: b.other.ID, AccountType: "person"}, SessionDigest: f.place.private.peer.SessionDigest}
			ref := ea.Ref{Type: "community", ID: c.ID}
			r, err := b.store.ReadEntityActions(b.ctx, a, ref)
			if err != nil {
				t.Fatal(err)
			}
			join := actionNativeFlag(t, r.View, ea.Join)
			if join.State != ea.Available || len(join.AllowedOperations) != 2 || join.AllowedOperations[0] != "ACCEPT_INVITATION" || join.AllowedOperations[1] != "DECLINE_INVITATION" {
				t.Fatal("actual invite choices", join)
			}
			if decision == "revokeABA" {
				b.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, c.ID, b.other.ID)
				b.exec(`UPDATE community_memberships SET status='invited' WHERE community_id=$1 AND user_account_id=$2`, c.ID, b.other.ID)
				if err = b.store.RevalidateEntityActions(b.ctx, a, ref, r); !errors.Is(err, ea.ErrChanged) {
					t.Fatal("old both-choice approval survived invite ABA", err)
				}
				return
			}
			if err = b.store.RevalidateEntityActions(b.ctx, a, ref, r); err != nil {
				t.Fatal(err)
			}
			if decision == "accept" {
				_, err = b.store.JoinSocialCommunity(b.ctx, b.other.ID, c.ID)
			} else {
				err = b.store.LeaveSocialCommunity(b.ctx, b.other.ID, c.ID)
			}
			if err != nil {
				t.Fatal("original chosen domain action", err)
			}
			var status string
			if err = b.pool.QueryRow(b.ctx, `SELECT status FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, c.ID, b.other.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			want := "active"
			if decision == "decline" {
				want = "left"
			}
			if status != want {
				t.Fatal("wrong chosen domain consequence", status)
			}
		})
	}
}

func TestEntityActionNativeAnonymousPublicPointSourceAndExpiry(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	b.exec(`UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=57.1,longitude=-2.1 WHERE id=$1`, f.place.place)
	access := ea.Access{Public: true}
	ref := ea.Ref{Type: "place", ID: f.place.place}
	r, err := b.store.ReadEntityActions(b.ctx, access, ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range r.View.Actions {
		want := ea.Unavailable
		if action.Kind == ea.Navigate || action.Kind == ea.Share {
			want = ea.Available
		}
		if action.State != want {
			t.Fatal("public NAV and explicit public system export only", action)
		}
	}
	if err = b.store.RevalidateEntityActions(b.ctx, access, ref, r); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"sourceABA", "noPoint", "expired"} {
		b.exec(`UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=57.1,longitude=-2.1,expires_at=NULL WHERE id=$1`, ref.ID)
		r, err = b.store.ReadEntityActions(b.ctx, access, ref)
		if err != nil {
			t.Fatal(err)
		}
		switch mutation {
		case "sourceABA":
			b.exec(`UPDATE places SET name=name WHERE id=$1`, ref.ID)
		case "noPoint":
			b.exec(`UPDATE places SET location_precision='none',latitude=NULL,longitude=NULL WHERE id=$1`, ref.ID)
		case "expired":
			b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, ref.ID)
		}
		if err = b.store.RevalidateEntityActions(b.ctx, access, ref, r); err == nil {
			t.Fatal("public stale source", mutation)
		}
	}
	if _, err = b.store.ReadEntityActions(b.ctx, access, ea.Ref{Type: "activity", ID: f.private}); !errors.Is(err, ea.ErrNotFound) {
		t.Fatal("private activity anonymous leak", err)
	}
	access.Actor = identity.Actor{ID: b.person.ID, AccountType: "person"}
	if _, err = b.store.ReadEntityActions(b.ctx, access, ref); !errors.Is(err, ea.ErrInvalid) {
		t.Fatal("anonymous invented actor", err)
	}
}

func TestEntityActionNativeAnonymousReceiptAfterPoolWait(t *testing.T) {
	for _, mutation := range []string{"hidden", "coarse", "noCoordinates", "sourceABA", "cityExpiry", "receiptExpiry"} {
		t.Run(mutation, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			b.exec(`UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=57.1,longitude=-2.1 WHERE id=$1`, f.place.place)
			if mutation == "cityExpiry" {
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()+interval '500 milliseconds' WHERE id=$1`, f.place.city)
			}
			cfg, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if err != nil {
				t.Fatal("owned config")
			}
			cfg.MaxConns = 1
			pool, err := pgxpool.NewWithConfig(b.ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			store := New(pool, false)
			access := ea.Access{Public: true}
			ref := ea.Ref{Type: "place", ID: f.place.place}
			r, err := store.ReadEntityActions(b.ctx, access, ref)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := pool.Acquire(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { result <- store.RevalidateEntityActions(b.ctx, access, ref, r) }()
			select {
			case err = <-result:
				conn.Release()
				t.Fatal("no actual pool wait", err)
			case <-time.After(50 * time.Millisecond):
			}
			switch mutation {
			case "hidden":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, ref.ID)
			case "coarse":
				b.exec(`UPDATE places SET location_precision='area' WHERE id=$1`, ref.ID)
			case "noCoordinates":
				b.exec(`UPDATE places SET location_precision='none',latitude=NULL,longitude=NULL WHERE id=$1`, ref.ID)
			case "sourceABA":
				b.exec(`UPDATE places SET name=name WHERE id=$1`, ref.ID)
			case "cityExpiry":
				if r.View.ValidUntil.After(r.View.ObservedAt.Add(time.Second)) {
					conn.Release()
					t.Fatal("public city deadline not clamped")
				}
				time.Sleep(550 * time.Millisecond)
			case "receiptExpiry":
				time.Sleep(time.Until(r.View.ValidUntil) + 20*time.Millisecond)
			}
			conn.Release()
			if err = <-result; err == nil {
				t.Fatal("anonymous wait returned stale proposal", mutation)
			}
		})
	}
}

func TestEntityActionNativePersonCommunityFieldSourceABA(t *testing.T) {
	for _, mutation := range []string{"targetMember", "communityMetadata", "communityNaturalExpiry"} {
		t.Run(mutation, func(t *testing.T) {
			f := agentVisibilityTestFixture(t)
			b := f.private.base
			rules := agentprofile.DefaultFieldRules()
			rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{f.communityID}}
			replaceVisibilityRules(t, f, rules)
			a := ea.Access{Actor: identity.Actor{ID: b.other.ID, AccountType: "person"}, SessionDigest: f.private.peer.SessionDigest}
			ref := ea.Ref{Type: "person", ID: b.person.ID}
			if mutation == "communityNaturalExpiry" {
				b.exec(`UPDATE communities SET expires_at=clock_timestamp()+interval '250 milliseconds' WHERE id=$1`, f.communityID)
			}
			r, err := b.store.ReadEntityActions(b.ctx, a, ref)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "targetMember" {
				b.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, b.person.ID)
				b.exec(`UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, f.communityID, b.person.ID)
			} else if mutation == "communityMetadata" {
				b.exec(`UPDATE communities SET summary=summary WHERE id=$1`, f.communityID)
			} else {
				if r.View.ValidUntil.After(r.View.ObservedAt.Add(time.Second)) {
					t.Fatal("field Community expiry not bound")
				}
				time.Sleep(300 * time.Millisecond)
			}
			if err = b.store.RevalidateEntityActions(b.ctx, a, ref, r); !errors.Is(err, ea.ErrChanged) {
				t.Fatal("community field source ABA escaped exact receipt", err)
			}
		})
	}
}

func TestEntityActionNativeSQLShape(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	for _, kind := range []string{"activity", "place", "person", "community", "organization", "business"} {
		if _, e = conn.Conn().Prepare(b.ctx, "entity_action_"+kind, entityActionSQL(kind)); e != nil {
			t.Fatalf("%s SQL: %v", kind, e)
		}
	}
}

func TestEntityActionNativeAfterRealPoolAndSessionWait(t *testing.T) {
	for _, wait := range []string{"poolNaturalExpiry", "sessionSourceABA"} {
		t.Run(wait, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
			ref := ea.Ref{Type: "place", ID: f.place.place}
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal("owned config")
			}
			cfg.MaxConns = 1
			cfg.ConnConfig.RuntimeParams["application_name"] = "actn-wait-" + ref.ID
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			r, e := store.ReadEntityActions(b.ctx, a, ref)
			if e != nil {
				t.Fatal(e)
			}
			if wait == "poolNaturalExpiry" {
				conn, e := pool.Acquire(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '250 milliseconds' WHERE id=$1`, ref.ID)
				result := make(chan error, 1)
				go func() { result <- store.RevalidateEntityActions(b.ctx, a, ref, r) }()
				select {
				case e := <-result:
					conn.Release()
					t.Fatal("expected actual exhausted pool wait", e)
				case <-time.After(50 * time.Millisecond):
				}
				if pool.Stat().AcquiredConns() != 1 || pool.Stat().MaxConns() != 1 {
					conn.Release()
					t.Fatal("sole owned pool connection not held")
				}
				time.Sleep(300 * time.Millisecond)
				conn.Release()
				if e := <-result; !errors.Is(e, ea.ErrNotFound) {
					t.Fatal("stale pool payload escaped", e)
				}
			} else {
				tx, e := b.pool.Begin(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(context.Background())
				var id string
				if e = tx.QueryRow(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.SessionDigest[:]).Scan(&id); e != nil {
					t.Fatal(e)
				}
				result := make(chan error, 1)
				go func() { result <- store.RevalidateEntityActions(b.ctx, a, ref, r) }()
				deadline := time.Now().Add(2 * time.Second)
				blocked := false
				for !blocked && time.Now().Before(deadline) {
					if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, cfg.ConnConfig.RuntimeParams["application_name"]).Scan(&blocked); e != nil {
						t.Fatal(e)
					}
					time.Sleep(time.Millisecond)
				}
				if !blocked {
					t.Fatal("actual Session row wait absent")
				}
				b.exec(`UPDATE places SET summary=summary WHERE id=$1`, ref.ID)
				if e = tx.Commit(b.ctx); e != nil {
					t.Fatal(e)
				}
				if e := <-result; !errors.Is(e, ea.ErrChanged) {
					t.Fatal("late source ABA escaped", e)
				}
			}
		})
	}
}

func TestEntityActionNativeSourceABAExpiryAndIdentity(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
	ref := ea.Ref{Type: "place", ID: f.place.place}
	r, e := b.store.ReadEntityActions(b.ctx, a, ref)
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE places SET summary=summary WHERE id=$1`, ref.ID)
	if e = b.store.RevalidateEntityActions(b.ctx, a, ref, r); !errors.Is(e, ea.ErrChanged) {
		t.Fatal("source ABA", e)
	}
	r, e = b.store.ReadEntityActions(b.ctx, a, ref)
	if e != nil {
		t.Fatal(e)
	}
	other := a
	other.Actor.ID = b.other.ID
	if e = b.store.RevalidateEntityActions(b.ctx, other, ref, r); e == nil {
		t.Fatal("cross owner")
	}
	b.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '250 milliseconds' WHERE id=$1`, ref.ID)
	r, e = b.store.ReadEntityActions(b.ctx, a, ref)
	if e != nil {
		t.Fatal(e)
	}
	if r.View.ValidUntil.After(r.View.ObservedAt.Add(time.Second)) {
		t.Fatal("source deadline not bounded")
	}
	time.Sleep(300 * time.Millisecond)
	if e = b.store.RevalidateEntityActions(b.ctx, a, ref, r); e == nil {
		t.Fatal("natural source expiry")
	}
}
func actionNativeFlag(t *testing.T, v ea.View, kind string) ea.Descriptor {
	t.Helper()
	for _, d := range v.Actions {
		if d.Kind == kind {
			return d
		}
	}
	t.Fatal("missing closed action", kind)
	return ea.Descriptor{}
}
func TestEntityActionNativePublicPrivateSaveAndReceipt(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
	for _, id := range []string{f.public, f.private} {
		ref := ea.Ref{Type: "activity", ID: id}
		r, e := b.store.ReadEntityActions(b.ctx, a, ref)
		if e != nil {
			t.Fatal(e)
		}
		if !r.View.Valid() {
			t.Fatal("invalid native contract")
		}
		want := ea.Available
		if id == f.private {
			want = ea.Unavailable
		}
		if actionNativeFlag(t, r.View, ea.Save).State != want {
			t.Fatal("private view incorrectly enabled save", r.View)
		}
		if e = b.store.RevalidateEntityActions(b.ctx, a, ref, r); e != nil {
			t.Fatal("current receipt", e)
		}
		r.View.Actions[0].State = ea.Available
		if e = b.store.RevalidateEntityActions(b.ctx, a, ref, r); !errors.Is(e, ea.ErrInvalid) && !errors.Is(e, ea.ErrChanged) {
			t.Fatal("wire permission tampering", e)
		}
	}
}

// entityActionTailBarrier is native test infrastructure only. Its audit trigger
// is reached after the original concrete source check and actual domain writes,
// before the final source/session/clock proof. postWait is explicitly a same-Tx
// source disturbance, not evidence of an external committed source mutation.
type entityActionTailBarrier struct {
	t      *testing.T
	base   *agentProfileFixture
	hold   pgx.Tx
	pool   *pgxpool.Pool
	ctx    context.Context
	cancel context.CancelFunc
	pid    int
	name   string
}

func entityActionTailAuditBarrier(t *testing.T, b *agentProfileFixture, actor, purpose, action, resource, postWait string) *entityActionTailBarrier {
	t.Helper()
	if !(ea.Ref{Type: "person", ID: actor}).Valid() {
		t.Fatal("invalid synthetic audit actor")
	}
	suffix := strings.ReplaceAll(actor, "-", "")
	function := "actn_tail_" + suffix
	trigger := "actn_tail_" + suffix
	key := "actn-tail-" + actor
	b.exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(9050701,hashtext('%s'));%s RETURN NEW;END $$;CREATE TRIGGER %s BEFORE INSERT ON audit_events FOR EACH ROW WHEN(NEW.actor_account_id='%s'::uuid AND NEW.purpose='%s' AND NEW.action='%s' AND NEW.resource_type='%s') EXECUTE FUNCTION %s()`, function, key, postWait, trigger, actor, purpose, action, resource, function))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := b.pool.Exec(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON audit_events;DROP FUNCTION IF EXISTS %s()`, trigger, function)); err != nil {
			t.Error("owned audit fixture cleanup", err)
		}
	})
	hold, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	ready := false
	defer func() {
		if !ready {
			_ = hold.Rollback(context.Background())
		}
	}()
	var pid int
	if err = hold.QueryRow(b.ctx, `SELECT pg_backend_pid(),pg_advisory_xact_lock(9050701,hashtext($1))`, key).Scan(&pid, new(any)); err != nil {
		t.Fatal(err)
	}
	cfg := b.pool.Config().Copy()
	name := "actn-tail-audit-" + actor
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	pool, err := pgxpool.NewWithConfig(b.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(b.ctx)
	ready = true
	return &entityActionTailBarrier{t: t, base: b, hold: hold, pool: pool, ctx: ctx, cancel: cancel, pid: pid, name: name}
}

func (w *entityActionTailBarrier) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = w.hold.Rollback(ctx) // release holder BEFORE closing the waiting writer pool
	w.cancel()
	w.pool.Close()
}

func (w *entityActionTailBarrier) wait(done <-chan error) {
	w.t.Helper()
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		select {
		case err := <-done:
			w.t.Fatal("writer completed before actual after-source audit wait", err)
		default:
		}
		var waiting bool
		if err := w.base.pool.QueryRow(w.base.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND $2=ANY(pg_blocking_pids(pid)) AND query LIKE '%INSERT INTO audit_events%')`, w.name, w.pid).Scan(&waiting); err != nil {
			w.t.Fatal(err)
		}
		if waiting {
			w.t.Log("actual after-source audit wait", w.name, w.pid)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.t.Fatal("actual after-source audit wait absent")
}

func entityActionTailCleanup(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Error("owned EntityAction fixture cleanup", err)
	}
}

func TestEntityActionNativeBoundOpenChatBlockedBeforeAdmission(t *testing.T) {
	f := contextBuilderNative(t)
	b := f.place.private.base
	a := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.place.private.owner.SessionDigest}
	request, e := b.store.CreateFriendRequest(b.ctx, a.Actor.ID, b.other.ID, "本地明确申请")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.DecideRequest(b.ctx, b.other.ID, request.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	ties, e := b.store.ListTies(b.ctx, a.Actor.ID)
	if e != nil || len(ties) != 1 {
		t.Fatal(ties, e)
	}
	t.Cleanup(func() {
		entityActionTailCleanup(t, b.pool, `DELETE FROM conversations WHERE request_id=$1`, request.ID)
		entityActionTailCleanup(t, b.pool, `DELETE FROM person_ties WHERE request_id=$1`, request.ID)
		entityActionTailCleanup(t, b.pool, `DELETE FROM audit_events WHERE actor_account_id IN($1,$2)`, a.Actor.ID, b.other.ID)
		entityActionTailCleanup(t, b.pool, `DELETE FROM connection_requests WHERE id=$1`, request.ID)
		entityActionTailCleanup(t, b.pool, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, a.Actor.ID, b.other.ID)
	})
	r, e := b.store.ReadEntityActions(b.ctx, a, ea.Ref{Type: "person", ID: b.other.ID})
	if e != nil {
		t.Fatal(e)
	}
	condition := ea.BoundCondition{SourceVersion: r.View.SourceVersion, ValidUntil: r.View.ValidUntil, Kind: ea.Message, Operation: "OPEN_CHAT"}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, a.Actor.ID, b.other.ID)
	if _, e = b.store.StartFriendConversationBound(b.ctx, a, ties[0].ID, condition); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("current block must deny at current original message route", e)
	}
	var effects int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM conversations WHERE request_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$2 AND purpose='human_friend_chat')+(SELECT count(*) FROM conversation_messages WHERE conversation_id IN(SELECT id FROM conversations WHERE request_id=$1))`, request.ID, a.Actor.ID).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("blocked admission creates no conversation, audit or message", effects, e)
	}
}
