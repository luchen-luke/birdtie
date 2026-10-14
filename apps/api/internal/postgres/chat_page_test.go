package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestChatPageCursorClosedOwnerConversationAndOrdering(t *testing.T) {
	owner := "11111111-1111-4111-8111-111111111111"
	other := "22222222-2222-4222-8222-222222222222"
	id := "33333333-3333-4333-8333-333333333333"
	stamp := time.Date(2026, 10, 12, 0, 1, 2, 345000, time.UTC)
	original := connection.PageCursor{Version: 1, OwnerID: owner, ConversationID: other, Time: stamp, ID: id}
	raw, err := connection.EncodePageCursor(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := connection.DecodePageCursor(raw, owner, other)
	if err != nil || decoded == nil || !reflect.DeepEqual(*decoded, original) {
		t.Fatal("canonical cursor lost timestamp/id", err)
	}
	bytes, _ := base64.RawURLEncoding.DecodeString(raw)
	for _, altered := range []string{
		strings.TrimSuffix(string(bytes), "}") + `,"grant":true}`,
		strings.TrimSuffix(string(bytes), "}") + `,"owner":"` + owner + `"}`,
		string(bytes) + " ",
		strings.Replace(string(bytes), `"v":1`, `"v":2`, 1),
		strings.Replace(string(bytes), "Z\"", "+00:00\"", 1),
	} {
		if _, err = connection.DecodePageCursor(base64.RawURLEncoding.EncodeToString([]byte(altered)), owner, other); !errors.Is(err, connection.ErrInvalidPage) {
			t.Fatal("noncanonical/unknown hint became a grant")
		}
	}
	for _, tuple := range [][2]string{{other, other}, {owner, id}, {owner, ""}} {
		if _, err = connection.DecodePageCursor(raw, tuple[0], tuple[1]); !errors.Is(err, connection.ErrInvalidPage) {
			t.Fatal("foreign owner/conversation cursor accepted")
		}
	}
	for _, limit := range []int{-1, 51, 9999} {
		if _, _, err = connection.NormalizeChatPage(connection.PageRequest{Limit: limit}, owner, ""); !errors.Is(err, connection.ErrInvalidPage) {
			t.Fatal("unbounded page")
		}
	}
	in, cursor, err := connection.NormalizeChatPage(connection.PageRequest{}, owner, "")
	if err != nil || cursor != nil || in.Limit != 20 {
		t.Fatal("default is the first recent 20")
	}
}

// All mutations below are synthetic fixtures in ownedMigrationDatabase's random
// child. The native runtime/parent is never selected as the test's write target.
func chatPageNativeConversation(t *testing.T, f *nativeEntityShareFixture, peerID string, created time.Time) string {
	t.Helper()
	b := f.f.private.base
	var request, conversation string
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at,created_at,decided_at)
 VALUES($1,$2,$3,'本地分页合成已接受许可','conversation','accepted',clock_timestamp()+interval '1 hour',$4,$4) RETURNING id`, f.a.actor.ID, peerID, f.f.city, created).Scan(&request); err != nil {
		t.Fatal("owned scoped request", err)
	}
	if err := b.pool.QueryRow(b.ctx, `INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id,created_at)
 VALUES($1,$2,$3,$4) RETURNING id`, request, f.a.actor.ID, peerID, created).Scan(&conversation); err != nil {
		t.Fatal("owned original conversation", err)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM conversation_member_states WHERE conversation_id=$1`, `DELETE FROM conversation_messages WHERE conversation_id=$1`, `DELETE FROM conversations WHERE id=$1`, `DELETE FROM connection_requests WHERE id=$1`} {
			id := conversation
			if strings.Contains(q, "DELETE FROM connection_requests") {
				id = request
			}
			if _, err := b.pool.Exec(context.Background(), q, id); err != nil {
				t.Error("owned pagination fixture cleanup", err)
			}
		}
	})
	return conversation
}

func TestChatPageNativeConversationsBeyond100RecencyUnreadAndDirect(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	b.exec(`UPDATE conversations SET created_at=$2 WHERE id=$1`, f.conversation, old.Add(-time.Hour))
	type rank struct {
		id string
		at time.Time
	}
	expected := []rank{{f.conversation, old.Add(-time.Hour)}}
	for i := 0; i < 135; i++ {
		var peer string
		if err := b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type,status) VALUES(gen_random_uuid(),'person','active') RETURNING id`).Scan(&peer); err != nil {
			t.Fatal(err)
		}
		b.accounts = append(b.accounts, peer)
		b.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,$2,'public')`, peer, fmt.Sprintf("本地分页联系人%03d", i))
		// Newest-created is deliberately NOT newest-contacted. Equal message
		// times test the deterministic UUID tiebreak across a page boundary.
		conversation := chatPageNativeConversation(t, f, peer, old.Add(time.Duration(135-i)*time.Second))
		contactAt := old.Add(24*time.Hour + time.Duration(i/2)*time.Second)
		b.exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body,created_at) VALUES($1,$2,'本地明确历史消息',$3)`, conversation, peer, contactAt)
		expected = append(expected, rank{conversation, contactAt})
	}
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].at.Equal(expected[j].at) {
			return expected[i].id > expected[j].id
		}
		return expected[i].at.After(expected[j].at)
	})
	first, err := b.store.ListConversationsPage(b.ctx, a, connection.PageRequest{Limit: 4})
	if err != nil || len(first.Items) != 4 || first.UnreadCount != 135 || first.NextCursor == nil {
		t.Fatal("recent-four/global unread", len(first.Items), first.UnreadCount, err)
	}
	for i, item := range first.Items {
		if item.ID != expected[i].id || !strings.HasPrefix(item.OtherName, "本地分页联系人") {
			t.Fatal("last-contact order or public human name missing", i)
		}
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, a, chatPageIDs(first), first); err != nil {
		t.Fatal("fresh exact page final authorization", err)
	}
	seen := []string{}
	cursor := ""
	for turn := 0; turn < 10; turn++ {
		page, err := b.store.ListConversationsPage(b.ctx, a, connection.PageRequest{Limit: 20, Cursor: cursor})
		if err != nil || page.UnreadCount != 135 {
			t.Fatal("authorized continuation", err)
		}
		for _, item := range page.Items {
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
	if len(seen) != len(expected) || len(seen) <= 100 {
		t.Fatal("whole history truncated", len(seen))
	}
	for i := range expected {
		if expected[i].id != seen[i] {
			t.Fatal("page boundary duplicated/dropped/reordered conversation", i)
		}
	}
	current, err := b.store.ConversationCurrent(b.ctx, a, f.conversation)
	if err != nil || current.ID != f.conversation {
		t.Fatal("original notification destination older than first100 unreachable", err)
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{current.ID}, current); err != nil {
		t.Fatal(err)
	}
	// Reading a page does not mark messages/notifications read. One separately
	// authorized original read cursor reduces the global count outside page one.
	lastUnread := expected[len(expected)-2].id
	var messageID string
	if err = b.pool.QueryRow(b.ctx, `SELECT id FROM conversation_messages WHERE conversation_id=$1 LIMIT 1`, lastUnread).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.store.MarkRead(b.ctx, a.Actor.ID, lastUnread, messageID); err != nil {
		t.Fatal(err)
	}
	now, err := b.store.ListConversationsPage(b.ctx, a, connection.PageRequest{Limit: 4})
	if err != nil || now.UnreadCount != 134 || now.Items[0].UnreadCount != 1 {
		t.Fatal("global count was calculated from only visible four", now.UnreadCount, err)
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, a, chatPageIDs(first), first); !errors.Is(err, connection.ErrConflict) {
		t.Fatal("stale aggregate after outside-page read escaped", err)
	}
}

func chatPageIDs(page connection.ConversationPage) []string {
	ids := make([]string, len(page.Items))
	for i, item := range page.Items {
		ids[i] = item.ID
	}
	return ids
}

func TestChatPageNativeMessagesBeyond100ClosedHintsAndCurrentDenial(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	peer := ea.Access{Actor: f.b.actor, SessionDigest: f.b.digest}
	stamp := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	b.exec(`UPDATE conversation_member_states SET baseline_at=$2 WHERE conversation_id=$1`, f.conversation, stamp)
	b.exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body,created_at)
 SELECT $1,$2,'本地完整历史'||n,$3::timestamptz+(n/2+1)*interval '1 second' FROM generate_series(1,137) n`, f.conversation, f.b.actor.ID, stamp)
	rows, err := b.pool.Query(b.ctx, `SELECT id FROM conversation_messages WHERE conversation_id=$1 ORDER BY created_at,id`, f.conversation)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	first, err := b.store.ListMessagesPage(b.ctx, a, f.conversation, connection.PageRequest{})
	if err != nil || len(first.Items) != 20 || first.NextCursor == nil || first.Items[0].ID != expected[117] {
		t.Fatal("latest20 chronological", len(first.Items), err)
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{f.conversation}, first); err != nil {
		t.Fatal("fresh history final projection", err)
	}
	cursor := ""
	seen := []string{}
	for turn := 0; turn < 10; turn++ {
		page, err := b.store.ListMessagesPage(b.ctx, a, f.conversation, connection.PageRequest{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(page.Items))
		for i, item := range page.Items {
			ids[i] = item.ID
			if i > 0 && (item.CreatedAt.Before(page.Items[i-1].CreatedAt) || item.CreatedAt.Equal(page.Items[i-1].CreatedAt) && item.ID <= page.Items[i-1].ID) {
				t.Fatal("page not chronological")
			}
		}
		seen = append(ids, seen...)
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if !reflect.DeepEqual(seen, expected) || len(seen) <= 100 {
		t.Fatal("full history lost/duplicated more-than100 messages", len(seen))
	}
	oldestTime := stamp.Add(time.Second)
	end, _ := connection.EncodePageCursor(connection.PageCursor{Version: 1, OwnerID: a.Actor.ID, ConversationID: f.conversation, Time: oldestTime, ID: "00000000-0000-4000-8000-000000000001"})
	empty, err := b.store.ListMessagesPage(b.ctx, a, f.conversation, connection.PageRequest{Cursor: end})
	if err != nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatal("empty end page must terminate", err)
	}
	if _, err = b.store.ListMessagesPage(b.ctx, peer, f.conversation, connection.PageRequest{Cursor: *first.NextCursor}); !errors.Is(err, connection.ErrInvalidPage) {
		t.Fatal("cursor bound to the original actor", err)
	}
	stranger := momentPublicationOrdinary(t, f.f)
	outsider := ea.Access{Actor: stranger.actor, SessionDigest: stranger.digest}
	if _, err = b.store.ConversationCurrent(b.ctx, outsider, f.conversation); !errors.Is(err, connection.ErrNotFound) {
		t.Fatal("stranger single conversation read", err)
	}
	if _, err = b.store.ListMessagesPage(b.ctx, outsider, f.conversation, connection.PageRequest{}); !errors.Is(err, connection.ErrNotFound) {
		t.Fatal("stranger history read", err)
	}
	foreign, err := b.store.ListConversationsPage(b.ctx, outsider, connection.PageRequest{})
	if err != nil || foreign.UnreadCount != 0 || len(foreign.Items) != 0 || foreign.NextCursor != nil {
		t.Fatal("stranger fabricated or private conversation list", err)
	}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.b.actor.ID, f.a.actor.ID)
	if _, err = b.store.ListMessagesPage(b.ctx, a, f.conversation, connection.PageRequest{Cursor: *first.NextCursor}); !errors.Is(err, connection.ErrNotFound) {
		t.Fatal("old cursor bypassed current bilateral block", err)
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{f.conversation}, first); !errors.Is(err, connection.ErrNotFound) {
		t.Fatal("late original page after block returned private history", err)
	}
	blocked, err := b.store.ListConversationsPage(b.ctx, a, connection.PageRequest{})
	if err != nil || len(blocked.Items) != 0 || blocked.UnreadCount != 0 {
		t.Fatal("blocked private badge/list retained", err)
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND token_sha256=$2`, a.Actor.ID, a.SessionDigest[:])
	if _, err = b.store.ListConversationsPage(b.ctx, a, connection.PageRequest{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked actor page allowed", err)
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{f.conversation}, first); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked late response allowed", err)
	}
}

func TestChatPageNativeFinalProfileAndEntityRecheck(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	peer := ea.Access{Actor: f.b.actor, SessionDigest: f.b.digest}
	current, err := b.store.ConversationCurrent(b.ctx, a, f.conversation)
	if err != nil || current.OtherName != "本地合成分享好友" {
		t.Fatal("ordinary public human name needs no Agent", err)
	}
	b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.b.actor.ID)
	if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{f.conversation}, current); !errors.Is(err, connection.ErrConflict) {
		t.Fatal("late private profile name escaped encoded final gate", err)
	}
	hidden, err := b.store.ConversationCurrent(b.ctx, a, f.conversation)
	if err != nil || hidden.OtherName != "Birdtie 成员" {
		t.Fatal("private profile must fall back without hiding authorized conversation", err)
	}
	b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.b.actor.ID)
	// A real active Agent's private field restriction remains stronger than the
	// profile source's PUBLIC setting. It is not bypassed by the human fallback.
	protectedID := f.f.private.base.person.ID
	protectedConversation := chatPageNativeConversation(t, f, protectedID, time.Now().UTC())
	before, err := b.store.ConversationCurrent(b.ctx, a, protectedConversation)
	if err != nil || before.OtherName != "合成公开资料" {
		t.Fatal("active metadata public source", err)
	}
	visibility, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.f.private.owner)
	if err != nil {
		t.Fatal(err)
	}
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
	if _, err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.f.private.owner, agentprofile.ReplaceVisibilityInput{ExpectedVersion: visibility.Profile.ProfileVersion, Rules: rules}); err != nil {
		t.Fatal(err)
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{protectedConversation}, before); !errors.Is(err, connection.ErrConflict) {
		t.Fatal("late restricted Agent field leaked old human name", err)
	}
	protected, err := b.store.ConversationCurrent(b.ctx, a, protectedConversation)
	if err != nil || protected.OtherName != "Birdtie 成员" {
		t.Fatal("Agent field restriction bypassed", err)
	}
	if _, err = b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, nativeShareInput(t, f)); err != nil {
		t.Fatal("owned explicit native entity share", err)
	}
	page, err := b.store.ListMessagesPage(b.ctx, peer, f.conversation, connection.PageRequest{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Entity == nil || !page.Items[0].Entity.Available {
		t.Fatal("current public source card", err)
	}
	encoded, _ := json.Marshal(page)
	if !strings.Contains(string(encoded), "本人明确公开的合成记录") {
		t.Fatal("fixture must exercise a previously encoded visible title")
	}
	if err = b.store.WithdrawHumanMoment(b.ctx, f.a.digest, f.a.actor, f.moment, f.revision); err != nil {
		t.Fatal(err)
	}
	if err = b.store.ValidateChatPageResponse(b.ctx, peer, []string{f.conversation}, page); !errors.Is(err, connection.ErrConflict) {
		t.Fatal("withdrawn entity title escaped after serialization", err)
	}
	after, err := b.store.ListMessagesPage(b.ctx, peer, f.conversation, connection.PageRequest{})
	if err != nil || len(after.Items) != 1 || after.Items[0].Entity == nil || after.Items[0].Entity.Available || after.Items[0].Entity.ID != "" || after.Items[0].Entity.Title != "" {
		t.Fatal("authorized history must retain only unavailable card after withdrawal", err)
	}
}

func TestChatPageNativeSessionExpiryOrRevokeAcrossActualReadWait(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, change := range []string{"revoke", "expiry"} {
		t.Run(change, func(t *testing.T) {
			f := newNativeEntityShareFixture(t)
			b := f.f.private.base
			a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
			b.exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body) VALUES($1,$2,'本地等待期间私密消息')`, f.conversation, f.b.actor.ID)
			lock, err := b.pool.Begin(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback(context.Background())
			if _, err = lock.Exec(b.ctx, `LOCK TABLE conversation_messages IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			type response struct {
				page connection.MessagePage
				err  error
			}
			result := make(chan response, 1)
			ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
			defer cancel()
			go func() {
				page, err := b.store.ListMessagesPage(ctx, a, f.conversation, connection.PageRequest{})
				result <- response{page, err}
			}()
			placeMemoryWaitForBlock(t, f.f, lock.Conn().PgConn().PID())
			if change == "revoke" {
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND token_sha256=$2`, a.Actor.ID, a.SessionDigest[:])
			} else {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '100 milliseconds' WHERE account_id=$1 AND token_sha256=$2`, a.Actor.ID, a.SessionDigest[:])
				time.Sleep(200 * time.Millisecond)
			}
			if err = lock.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			got := <-result
			if !errors.Is(got.err, identity.ErrUnauthorized) || len(got.page.Items) != 0 || got.page.NextCursor != nil {
				t.Fatal("identity from before a real wait returned private history", change, got.err)
			}
			var count int
			if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, f.conversation).Scan(&count); err != nil || count != 1 {
				t.Fatal("read caused a write or replay", count, err)
			}
		})
	}
}

func TestChatPageNativeFinalProjectionAfterSessionWait(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	peer := ea.Access{Actor: f.b.actor, SessionDigest: f.b.digest}
	if _, err := b.store.ShareHumanEntity(b.ctx, f.access(), f.conversation, nativeShareInput(t, f)); err != nil {
		t.Fatal("owned explicit entity share", err)
	}
	page, err := b.store.ListMessagesPage(b.ctx, peer, f.conversation, connection.PageRequest{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Entity == nil || !page.Items[0].Entity.Available {
		t.Fatal("previously encoded available source", err)
	}
	if _, err = json.Marshal(page); err != nil {
		t.Fatal(err)
	}
	lock, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	var sessionID string
	if err = lock.QueryRow(b.ctx, `SELECT id FROM sessions WHERE account_id=$1 AND token_sha256=$2 FOR UPDATE`, peer.Actor.ID, peer.SessionDigest[:]).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 8*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- b.store.ValidateChatPageResponse(ctx, peer, []string{f.conversation}, page) }()
	// Real Session row wait, not a mocked callback. The source withdrawal is
	// committed while the final gate is queued behind that exact captured row.
	placeMemoryWaitForBlock(t, f.f, lock.Conn().PgConn().PID())
	if err = b.store.WithdrawHumanMoment(b.ctx, f.a.digest, f.a.actor, f.moment, f.revision); err != nil {
		t.Fatal("source withdrawal during recipient Session wait", err)
	}
	if err = lock.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, connection.ErrConflict) {
		t.Fatal("source checked before Session wait allowed the old visible title", err)
	}
}

func TestChatPageNativePersistedPrivateNameAfterAgentInactive(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	protectedID := b.person.ID
	conversation := chatPageNativeConversation(t, f, protectedID, time.Now().UTC())
	public, err := b.store.ConversationCurrent(b.ctx, a, conversation)
	if err != nil || public.OtherName != "合成公开资料" {
		t.Fatal("fixture must start with the actual public human name", err)
	}
	visibility, err := b.store.ReadOwnAgentProfileVisibility(b.ctx, f.f.private.owner)
	if err != nil {
		t.Fatal(err)
	}
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
	if _, err = b.store.ReplaceOwnAgentProfileVisibility(b.ctx, f.f.private.owner, agentprofile.ReplaceVisibilityInput{ExpectedVersion: visibility.Profile.ProfileVersion, Rules: rules}); err != nil {
		t.Fatal(err)
	}
	private, err := b.store.ConversationCurrent(b.ctx, a, conversation)
	if err != nil || private.OtherName != "Birdtie 成员" {
		t.Fatal("active Agent's explicit field restriction", err)
	}
	for _, status := range []string{"suspended", "retired"} {
		b.exec(`UPDATE agents SET status=$2 WHERE id=$1`, b.personID, status)
		now, err := b.store.ConversationCurrent(b.ctx, a, conversation)
		if err != nil || now.OtherName != "Birdtie 成员" {
			t.Fatal("stopping Agent must not republish a persisted PRIVATE name", status, err)
		}
		page, err := b.store.ListConversationsPage(b.ctx, a, connection.PageRequest{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range page.Items {
			if item.ID == conversation {
				found = true
				if item.OtherName != "Birdtie 成员" {
					t.Fatal("paged projection bypassed persistent privacy", status)
				}
			}
		}
		if !found {
			t.Fatal("do not remove an otherwise authorized original conversation")
		}
		if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{conversation}, private); err != nil {
			t.Fatal("safe masked final projection should stay safe", status, err)
		}
		if err = b.store.ValidateChatPageResponse(b.ctx, a, []string{conversation}, public); !errors.Is(err, connection.ErrConflict) {
			t.Fatal("old public name must not be returned by final gate", status, err)
		}
	}
	ordinary, err := b.store.ConversationCurrent(b.ctx, a, f.conversation)
	if err != nil || ordinary.OtherName != "本地合成分享好友" {
		t.Fatal("an ordinary public person with no Agent/rules remains readable", err)
	}
}
