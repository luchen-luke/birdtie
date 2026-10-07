package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"testing"
	"time"
)

func TestNotificationDestinationNativeFinalSessionWaitRetirementIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	task, err := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: "aberdeen-gb", Query: "等待时不能借旧身份", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	var sessionID string
	if err = blocker.QueryRow(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, f.private.owner.SessionDigest[:]).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- b.store.ValidateHumanAgentTask(b.ctx, f.private.owner.SessionDigest, identity.Actor{ID: b.person.ID, AccountType: "person"}, agentworkspace.SanitizeTaskForResponse(task))
	}()
	reached := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM sessions%' AND query LIKE '%FOR SHARE%')`, int(blocker.Conn().PgConn().PID())).Scan(&reached); err != nil {
			t.Fatal(err)
		}
		if reached {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !reached {
		t.Fatal("did not reach actual final Session lock wait")
	}
	b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
	if err = blocker.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if !errors.Is(err, agentworkspace.ErrNotFound) {
			t.Fatal("Agent retirement during final Session wait released original task", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final boundary wait did not finish")
	}
}

func TestNotificationDestinationNativeTaskTargetIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	task, err := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: "aberdeen-gb", Query: "仅本地原任务", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = agentworkspace.TaskCompleted
	task, err = b.store.UpdateTask(b.ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	decision := f.delivered(t, agentnotification.KindAgentTaskCompleted, task.ID, b.person.ID, agentnotification.Normal, 1)
	actor := identity.Actor{ID: b.person.ID, AccountType: "person"}
	for range 2 {
		item, e := b.store.ReadHumanInboxItem(b.ctx, f.private.owner.SessionDigest, actor, decision.InboxID)
		if e != nil || item.TargetTaskID == nil || *item.TargetTaskID != task.ID || item.ResourceID == task.ID || item.TargetCommunityID != nil {
			t.Fatal("native decision ID masqueraded as task target", e)
		}
	}
	b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
	item, e := b.store.ReadHumanInboxItem(b.ctx, f.private.owner.SessionDigest, actor, decision.InboxID)
	if !errors.Is(e, inbox.ErrNotFound) || item.TargetTaskID != nil {
		t.Fatal("retired Agent leaked current task target", e)
	}
}

func TestNotificationDestinationNativeCommunityTargetIntegration(t *testing.T) {
	f := newNotificationDeliveryFixture(t)
	b := f.private.base
	c, err := b.store.CreateSocialCommunity(b.ctx, b.person.ID, community.SocialInput{Name: "隔离通知社群", Summary: "非真实供给", Visibility: "public", JoinPolicy: "open", CityID: "aberdeen-gb"})
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
	msg, err := b.store.SendCommunityChatMessage(b.ctx, b.person.ID, c.ID, operation, "PRIVATE_BODY_NOT_IN_INBOX")
	if err != nil {
		t.Fatal(err)
	}
	decision := f.delivered(t, agentnotification.KindCommunityMessage, msg.ID, b.other.ID, agentnotification.Normal, 1)
	actor := identity.Actor{ID: b.other.ID, AccountType: "person"}
	item, err := b.store.ReadHumanInboxItem(b.ctx, f.private.peer.SessionDigest, actor, decision.InboxID)
	if err != nil || item.TargetCommunityID == nil || *item.TargetCommunityID != c.ID || item.ResourceID == c.ID || item.ResourceID == msg.ID || item.TargetTaskID != nil {
		t.Fatal("native message/decision was mistaken for Community ID", err)
	}
	if err = b.store.LeaveCommunityChat(b.ctx, b.other.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	item, err = b.store.ReadHumanInboxItem(b.ctx, f.private.peer.SessionDigest, actor, decision.InboxID)
	if !errors.Is(err, inbox.ErrNotFound) || item.TargetCommunityID != nil {
		t.Fatal("left community target leaked", err)
	}
}
