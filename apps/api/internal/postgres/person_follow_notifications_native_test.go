package postgres

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
)

func TestPersonFollowNotificationNativeCurrentEventAndCompatibility(t *testing.T) {
	f := savedPageNative(t) // ownedMigrationDatabase: random marked child, never runtime DB.
	actor := interactionNativeActor(t, f)
	third := interactionNativeActor(t, f)
	for _, id := range []string{f.actor.ID, actor.actor.ID, third.actor.ID} {
		f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'仅隔离关注测试','public')`, id)
	}
	for _, file := range []string{"175_person_follow_notifications.down.sql", "175_person_follow_notifications.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", file))
		if err != nil {
			t.Fatal(err)
		}
		f.exec(t, string(raw))
	}
	counts := func() (int, int) {
		var events, pushes int
		if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM person_follow_events),(SELECT count(*) FROM system_push_events)`).Scan(&events, &pushes); err != nil {
			t.Fatal(err)
		}
		return events, pushes
	}
	_, initialPushes := counts()
	target := follow.Target{Type: follow.Person, ID: f.actor.ID}
	first, err := f.store.Follow(f.ctx, actor.actor.ID, target)
	if err != nil {
		t.Fatal("native follow", err)
	}
	again, err := f.store.Follow(f.ctx, actor.actor.ID, target)
	if err != nil || again != first {
		t.Fatal("same follow retry changed", err)
	}
	if events, pushes := counts(); events != 1 || pushes != initialPushes {
		t.Fatal("duplicate event or push", events, pushes)
	}
	q := inbox.PageRequest{View: inbox.NotificationsView, SupportsPersonFollow: true}
	page, err := f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, q)
	if err != nil || len(page.Items) != 1 || page.UnreadCount != 1 || page.Items[0].ResourceType != "person_follow" {
		t.Fatal("new notification page", err, page)
	}
	id := page.Items[0].ID
	current, err := f.store.GetInboxPersonTarget(f.ctx, f.digest, f.actor, id)
	if err != nil || current["accountId"] != actor.actor.ID {
		t.Fatal("current actor target", err)
	}
	if err = f.store.ValidateHumanInboxPageResponse(f.ctx, f.digest, f.actor, q, page); err != nil {
		t.Fatal("final page", err)
	}
	old, err := f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, inbox.PageRequest{View: inbox.NotificationsView})
	if err != nil || len(old.Items) != 0 || old.UnreadCount != 0 {
		t.Fatal("old projection exposed unsupported event/count", err)
	}
	legacy, err := f.store.ListHumanInbox(f.ctx, f.digest, f.actor)
	if err != nil || len(legacy) != 0 {
		t.Fatal("old nonpaged projection", err)
	}
	if _, err = f.store.GetInboxPersonTarget(f.ctx, third.digest, third.actor, id); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatal("foreign recipient target", err)
	}
	// A generic event conveys no right to a now-private follower profile.
	f.exec(t, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, actor.actor.ID)
	if _, err = f.store.GetInboxPersonTarget(f.ctx, f.digest, f.actor, id); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatal("private profile target released", err)
	}
	page, err = f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, q)
	if err != nil || len(page.Items) != 1 || page.Items[0].ActorID != nil || page.Items[0].ActorAvatar != nil {
		t.Fatal("generic notice leaked profile", err)
	}
	f.exec(t, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, actor.actor.ID)
	// Current block retires both the visible event and the independent resolver.
	f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.actor.ID, actor.actor.ID)
	if _, err = f.store.GetInboxPersonTarget(f.ctx, f.digest, f.actor, id); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatal("blocked target", err)
	}
	page, err = f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, q)
	if err != nil || len(page.Items) != 0 || page.UnreadCount != 0 {
		t.Fatal("blocked event remains", err)
	}
	f.exec(t, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.actor.ID, actor.actor.ID)
	if err = f.store.Unfollow(f.ctx, actor.actor.ID, target); !errors.Is(err, follow.ErrNotFound) {
		t.Fatal("block must already retire the follow relation", err)
	}
	if _, err = f.store.GetInboxPersonTarget(f.ctx, f.digest, f.actor, id); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatal("unfollow target", err)
	}
	refollow, err := f.store.Follow(f.ctx, actor.actor.ID, target)
	if err != nil || refollow.ID == first.ID {
		t.Fatal("new explicit follow", err)
	}
	page, err = f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, q)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID == id {
		t.Fatal("new event missing or old revived", err)
	}
	newID := page.Items[0].ID
	if _, err = f.store.ReadHumanInboxItem(f.ctx, f.digest, f.actor, newID); err != nil {
		t.Fatal("mark read", err)
	}
	page, err = f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, q)
	if err != nil || page.UnreadCount != 0 || page.Items[0].ReadAt == nil {
		t.Fatal("read state", err)
	}
	f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, actor.actor.ID)
	if _, err = f.store.GetInboxPersonTarget(f.ctx, f.digest, f.actor, newID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatal("inactive actor", err)
	}
	f.exec(t, `UPDATE accounts SET status='active' WHERE id=$1`, actor.actor.ID)
	if err = f.store.Unfollow(f.ctx, actor.actor.ID, target); err != nil {
		t.Fatal("explicit unfollow", err)
	}
	if _, err = f.store.GetInboxPersonTarget(f.ctx, f.digest, f.actor, newID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatal("explicit unfollow target survived", err)
	}
	// Existing/legacy-style rows are never backfilled by an ordinary Follow retry.
	f.exec(t, `INSERT INTO follows(follower_account_id,person_account_id) VALUES($1,$2)`, third.actor.ID, f.actor.ID)
	if _, err = f.store.Follow(f.ctx, third.actor.ID, target); err != nil {
		t.Fatal("existing follow", err)
	}
	if events, pushes := counts(); events != 2 || pushes != initialPushes {
		t.Fatal("historical backfill or push", events, pushes)
	}
	var noEscalation bool
	if err = f.pool.QueryRow(f.ctx, `SELECT NOT EXISTS(SELECT 1 FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])) AND NOT EXISTS(SELECT 1 FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`, []string{f.actor.ID, actor.actor.ID, third.actor.ID}).Scan(&noEscalation); err != nil || !noEscalation {
		t.Fatal("follow created friendship/conversation", err)
	}
}
