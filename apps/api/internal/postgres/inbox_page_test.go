package postgres

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
)

// Each caller creates only a verified owned random child database. No parent,
// deployed runtime, real actor, provider or external notification is mutated.
func inboxPageFixture(t *testing.T) *notificationDeliveryFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	// Run before the older fixture helper deletes messages. Meaningful explicit
	// conversation reads create a cursor FK which must be cleaned, never ignored.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := b.pool.Exec(ctx, `DELETE FROM conversation_member_states WHERE conversation_id IN(SELECT id FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`, b.accounts); err != nil {
			t.Error("owned notification-page cursor cleanup", err)
		}
	})
	return f
}

func inboxPageTask(t *testing.T, f *notificationDeliveryFixture) string {
	t.Helper()
	b := f.private.base
	task, err := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.other.ID, ActingUserID: b.other.ID, CityID: "aberdeen-gb", Query: "本地分页合成任务", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if err != nil {
		t.Fatal("native task", err)
	}
	task.Status = agentworkspace.TaskCompleted
	if _, err = b.store.UpdateTask(b.ctx, task); err != nil {
		t.Fatal("native completion notification", err)
	}
	var id string
	if err = b.pool.QueryRow(b.ctx, `SELECT i.id FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE d.kind=$1 AND d.source_id=$2 AND i.recipient_account_id=$3`, string(agentnotification.KindAgentTaskCompleted), task.ID, b.other.ID).Scan(&id); err != nil {
		t.Fatal("actual task inbox row", err)
	}
	return id
}

func inboxPageCommunity(t *testing.T, f *notificationDeliveryFixture) (string, string) {
	t.Helper()
	b := f.private.base
	c, err := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{Name: "本地分页社群", Summary: "隔离验收", Visibility: "public", JoinPolicy: "open", CityID: "aberdeen-gb"})
	if err != nil {
		t.Fatal(err)
	}
	f.communities = append(f.communities, c.ID)
	if _, err = b.store.JoinSocialCommunity(b.ctx, b.other.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{b.person.ID, b.other.ID} {
		if _, err = b.store.JoinCommunityChat(b.ctx, id, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	var operation string
	if err = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&operation); err != nil {
		t.Fatal(err)
	}
	message, err := b.store.SendCommunityChatMessage(b.ctx, b.person.ID, c.ID, operation, "本地明确社群消息")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err = b.pool.QueryRow(b.ctx, `SELECT i.id FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE d.kind=$1 AND d.source_id=$2 AND i.recipient_account_id=$3`, string(agentnotification.KindCommunityMessage), message.ID, b.other.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return c.ID, id
}

// Bulk history belongs only to this test's owned child and already accepted
// conversation. Keep the native notification source/policy guards, rather than
// disabling the product's 60/hour send limit or manufacturing inbox grants.
func inboxPageHistoryMessage(t *testing.T, f *notificationDeliveryFixture, conversationID string) {
	t.Helper()
	b := f.private.base
	tx, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal("owned history transaction", err)
	}
	defer tx.Rollback(context.Background())
	var id string
	if err = tx.QueryRow(b.ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body)
VALUES($1,$2,'本地分页合成历史消息') RETURNING id`, conversationID, b.person.ID).Scan(&id); err != nil {
		t.Fatal("owned history source", err)
	}
	delivered, err := routeNativeNotification(b.ctx, tx, agentnotification.KindDirectMessage, id, b.other.ID)
	if err != nil || !delivered {
		t.Fatal("actual source-guarded history notification", delivered, err)
	}
	if err = tx.Commit(b.ctx); err != nil {
		t.Fatal("owned history commit", err)
	}
}

func TestInboxPageNativeMixedBeyond100AndIndependentUnread(t *testing.T) {
	f := inboxPageFixture(t)
	b := f.private.base
	actor := identity.Actor{ID: b.other.ID, AccountType: "person"}
	digest := f.private.peer.SessionDigest
	policy := nativeNotificationInput(t, f.private, agentnotification.Normal)
	policy.Rules = []agentnotification.Rule{{Category: agentnotification.CategoryMessage, Route: agentnotification.Immediate}, {Category: agentnotification.CategoryAgent, Route: agentnotification.Immediate}}
	if _, err := b.store.PutOwnNotificationPolicy(b.ctx, f.private.peer, policy); err != nil {
		t.Fatal(err)
	}
	cv, _ := f.friend(t)
	type rank struct {
		id       string
		priority int
		stamp    time.Time
	}
	ranks := []rank{}
	base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 125; i++ {
		id := inboxPageTask(t, f)
		stamp := base.Add(time.Duration(i/2) * time.Second)
		b.exec(`UPDATE inbox_items SET created_at=$2 WHERE id=$1`, id, stamp)
		ranks = append(ranks, rank{id, 100, stamp})
	}
	for i := 0; i < 3; i++ {
		_, id := inboxPageCommunity(t, f)
		stamp := base.Add(100*24*time.Hour + time.Duration(i)*time.Second)
		b.exec(`UPDATE inbox_items SET created_at=$2 WHERE id=$1`, id, stamp)
		ranks = append(ranks, rank{id, 50, stamp})
	}
	// One current native send proves the real accepted relationship. Additional
	// bulk history is synthetic child data, not 135 automatic product sends.
	f.message(t, cv)
	for range 134 {
		inboxPageHistoryMessage(t, f, cv.ID)
	}
	var actualDirect int
	if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id
WHERE i.recipient_account_id=$1 AND i.resource_type='conversation_message' AND i.target_conversation_id=$2 AND birdtie_native_notification_visible(d)`, actor.ID, cv.ID).Scan(&actualDirect); err != nil || actualDirect != 135 {
		t.Fatal("actual visible mixed history fixture", actualDirect, err)
	}
	old, err := b.store.ListHumanInbox(b.ctx, digest, actor)
	if err != nil || len(old) != 100 {
		t.Fatal("mixed legacy fixture", len(old), err)
	}
	for _, item := range old {
		if item.ResourceType != "conversation_message" {
			t.Fatal("fixture did not reproduce messages crowding notifications")
		}
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].priority != ranks[j].priority {
			return ranks[i].priority > ranks[j].priority
		}
		if ranks[i].stamp.Equal(ranks[j].stamp) {
			return ranks[i].id > ranks[j].id
		}
		return ranks[i].stamp.After(ranks[j].stamp)
	})
	query := inbox.PageRequest{View: inbox.NotificationsView, Limit: 3}
	first, err := b.store.ListHumanInboxPage(b.ctx, digest, actor, query)
	if err != nil || len(first.Items) != 3 || first.UnreadCount != len(ranks) || first.NextCursor == nil {
		t.Fatal("first-three/global-notification count", len(first.Items), first.UnreadCount, err)
	}
	if err = b.store.ValidateHumanInboxPageResponse(b.ctx, digest, actor, query, first); err != nil {
		t.Fatal("fresh encoded page", err)
	}
	seen := []string{}
	cursor := ""
	for turn := 0; turn < 10; turn++ {
		page, e := b.store.ListHumanInboxPage(b.ctx, digest, actor, inbox.PageRequest{View: inbox.NotificationsView, Cursor: cursor, Limit: 20})
		if e != nil || page.UnreadCount != len(ranks) {
			t.Fatal("complete notification history", e)
		}
		for _, item := range page.Items {
			if item.ResourceType == "conversation_message" {
				t.Fatal("human message in pure notifications")
			}
			seen = append(seen, item.ID)
		}
		if page.NextCursor == nil {
			break
		}
		if *page.NextCursor == cursor {
			t.Fatal("cursor loop")
		}
		cursor = *page.NextCursor
	}
	if len(seen) != len(ranks) || len(seen) <= 100 {
		t.Fatal("complete notifications capped", len(seen))
	}
	for i := range ranks {
		if ranks[i].id != seen[i] {
			t.Fatal("priority/time/id duplicate, gap or reorder", i)
		}
	}
	last := ranks[len(ranks)-1]
	hint, _ := inbox.EncodePageCursor(inbox.PageCursor{Version: 1, OwnerID: actor.ID, View: inbox.NotificationsView, Priority: last.priority, Time: last.stamp, ID: last.id})
	end, err := b.store.ListHumanInboxPage(b.ctx, digest, actor, inbox.PageRequest{View: inbox.NotificationsView, Cursor: hint})
	if err != nil || len(end.Items) != 0 || end.NextCursor != nil || end.UnreadCount != len(ranks) {
		t.Fatal("empty end lost global count", err)
	}
	var beforeState string
	if err = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(s)::text FROM conversation_member_states s WHERE conversation_id=$1 AND member_account_id=$2`, cv.ID, actor.ID).Scan(&beforeState); err != nil {
		t.Fatal(err)
	}
	if _, err = b.store.ReadHumanInboxItem(b.ctx, digest, actor, last.id); err != nil {
		t.Fatal("explicit original notification read", err)
	}
	var afterState string
	if err = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(s)::text FROM conversation_member_states s WHERE conversation_id=$1 AND member_account_id=$2`, cv.ID, actor.ID).Scan(&afterState); err != nil || beforeState != afterState {
		t.Fatal("notification read changed human read cursor", err)
	}
	fresh, err := b.store.ListHumanInboxPage(b.ctx, digest, actor, query)
	if err != nil || fresh.UnreadCount != len(ranks)-1 {
		t.Fatal("outside-page read not reflected", fresh.UnreadCount, err)
	}
	if err = b.store.ValidateHumanInboxPageResponse(b.ctx, digest, actor, query, first); !errors.Is(err, inbox.ErrSourceChanged) {
		t.Fatal("old aggregate escaped", err)
	}
	var messageID string
	if err = b.pool.QueryRow(b.ctx, `SELECT id FROM conversation_messages WHERE conversation_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, cv.ID).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.store.MarkRead(b.ctx, actor.ID, cv.ID, messageID); err != nil {
		t.Fatal(err)
	}
	afterMessageRead, err := b.store.ListHumanInboxPage(b.ctx, digest, actor, query)
	if err != nil || afterMessageRead.UnreadCount != fresh.UnreadCount || !reflect.DeepEqual(fresh.Items, afterMessageRead.Items) {
		t.Fatal("human read mixed notification aggregate", err)
	}
}

func TestInboxPageNativeOwnerSessionAndCurrentTargetPermission(t *testing.T) {
	f := inboxPageFixture(t)
	b := f.private.base
	communityID, _ := inboxPageCommunity(t, f)
	actor := identity.Actor{ID: b.other.ID, AccountType: "person"}
	digest := f.private.peer.SessionDigest
	q := inbox.PageRequest{View: inbox.NotificationsView, Limit: 1}
	page, err := b.store.ListHumanInboxPage(b.ctx, digest, actor, q)
	if err != nil || len(page.Items) != 1 || page.Items[0].TargetCommunityID == nil || *page.Items[0].TargetCommunityID != communityID {
		t.Fatal("current native destination", err)
	}
	if got, e := b.store.ListHumanInboxPage(b.ctx, f.private.owner.SessionDigest, actor, q); !errors.Is(e, identity.ErrUnauthorized) || len(got.Items) != 0 {
		t.Fatal("mismatched actual session/owner released notification", e)
	}
	hint, _ := inbox.EncodePageCursor(inbox.PageCursor{Version: 1, OwnerID: actor.ID, View: inbox.NotificationsView, Priority: 50, Time: page.Items[0].CreatedAt, ID: page.Items[0].ID})
	if got, e := b.store.ListHumanInboxPage(b.ctx, f.private.owner.SessionDigest, identity.Actor{ID: b.person.ID, AccountType: "person"}, inbox.PageRequest{View: inbox.NotificationsView, Cursor: hint}); !errors.Is(e, inbox.ErrInvalidPage) || len(got.Items) != 0 {
		t.Fatal("foreign hint supplied authorization", e)
	}
	if err = b.store.LeaveCommunityChat(b.ctx, actor.ID, communityID); err != nil {
		t.Fatal("actual current membership withdrawal", err)
	}
	if err = b.store.ValidateHumanInboxPageResponse(b.ctx, digest, actor, q, page); !errors.Is(err, inbox.ErrSourceChanged) {
		t.Fatal("encoded revoked destination escaped", err)
	}
	fresh, err := b.store.ListHumanInboxPage(b.ctx, digest, actor, q)
	if err != nil || len(fresh.Items) != 0 || fresh.UnreadCount != 0 {
		t.Fatal("revoked source remained", err)
	}
	// Policy revocation is checked again for every older cursor, never cached.
	inboxPageTask(t, f)
	if _, err = b.store.CreateFriendRequest(b.ctx, b.person.ID, actor.ID, "本地明确待处理好友申请"); err != nil {
		t.Fatal("native friend request notification", err)
	}
	allQuery := inbox.PageRequest{View: inbox.NotificationsView, Limit: 50}
	page, err = b.store.ListHumanInboxPage(b.ctx, digest, actor, allQuery)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("own task plus actual pending request", err)
	}
	if err = b.store.BlockAccount(b.ctx, actor.ID, b.person.ID); err != nil {
		t.Fatal("actual explicit block", err)
	}
	if err = b.store.ValidateHumanInboxPageResponse(b.ctx, digest, actor, allQuery, page); !errors.Is(err, inbox.ErrSourceChanged) {
		t.Fatal("blocked actor's encoded request escaped", err)
	}
	page, err = b.store.ListHumanInboxPage(b.ctx, digest, actor, q)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(err)
	}
	if page.Items[0].ResourceType != "agent_task" || page.UnreadCount != 1 {
		t.Fatal("block removed own task or retained peer request")
	}
	f.policy(t, f.private.peer, agentnotification.Block)
	if err = b.store.ValidateHumanInboxPageResponse(b.ctx, digest, actor, q, page); !errors.Is(err, inbox.ErrSourceChanged) {
		t.Fatal("policy downgrade released old projection", err)
	}
}

func TestInboxPageNativeFinalSessionWaitRechecksProjectionAndGlobalUnread(t *testing.T) {
	for _, phase := range []string{"outside_page_read", "projection_retired", "session_revoked", "session_expired"} {
		t.Run(phase, func(t *testing.T) {
			f := inboxPageFixture(t)
			b := f.private.base
			actor := identity.Actor{ID: b.other.ID, AccountType: "person"}
			digest := f.private.peer.SessionDigest
			ids := []string{}
			for range 5 {
				ids = append(ids, inboxPageTask(t, f))
			}
			q := inbox.PageRequest{View: inbox.NotificationsView, Limit: 1}
			page, err := b.store.ListHumanInboxPage(b.ctx, digest, actor, q)
			if err != nil || page.UnreadCount != 5 {
				t.Fatal(err)
			}
			blocker, err := b.pool.Begin(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			var sessionID string
			if err = blocker.QueryRow(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]).Scan(&sessionID); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- b.store.ValidateHumanInboxPageResponse(ctx, digest, actor, q, page) }()
			reached := false
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM sessions%' AND query LIKE '%FOR SHARE%')`, int(blocker.Conn().PgConn().PID())).Scan(&reached); err != nil {
					t.Fatal(err)
				}
				if reached {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !reached {
				t.Fatal("did not reach actual Session row wait")
			}
			want := inbox.ErrSourceChanged
			switch phase {
			case "outside_page_read":
				id := ids[0]
				if id == page.Items[0].ID {
					id = ids[1]
				}
				b.exec(`UPDATE inbox_items SET read_at=clock_timestamp() WHERE id=$1 AND recipient_account_id=$2`, id, actor.ID)
			case "projection_retired":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.otherID)
			case "session_revoked":
				if _, err = blocker.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, sessionID); err != nil {
					t.Fatal(err)
				}
				want = identity.ErrUnauthorized
			case "session_expired":
				// Both deadlines share one future native timestamp so the fixture
				// satisfies idle_expiry even for a newly created session. Hold the
				// row until that clock has actually passed before releasing the read.
				var expiry time.Time
				if err = blocker.QueryRow(b.ctx, `WITH cutoff AS MATERIALIZED (SELECT clock_timestamp()+interval '50 milliseconds' AS n)
UPDATE sessions SET expires_at=cutoff.n,idle_expires_at=cutoff.n FROM cutoff WHERE id=$1 RETURNING expires_at`, sessionID).Scan(&expiry); err != nil {
					t.Fatal(err)
				}
				if _, err = blocker.Exec(b.ctx, `SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())),0)+0.003)`, expiry); err != nil {
					t.Fatal(err)
				}
				want = identity.ErrUnauthorized
			}
			if err = blocker.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if !errors.Is(err, want) {
					t.Fatal("old encoded projection/count escaped real wait", phase, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("actual final wait did not finish")
			}
		})
	}
}
