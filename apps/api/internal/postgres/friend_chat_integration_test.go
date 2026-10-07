package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFriendChatIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.conversation_member_states') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("035_conversation_read_state not applied")
	}
	ids := make([]string, 2)
	for i := range ids {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'Synthetic friend chat','public')`, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_member_states WHERE conversation_id IN (SELECT id FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_messages WHERE conversation_id IN (SELECT id FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	store := New(pool, false)
	request, err := store.CreateFriendRequest(ctx, ids[0], ids[1], "一起聊天吗？")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideRequest(ctx, ids[1], request.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	ties, err := store.ListTies(ctx, ids[0])
	if err != nil || len(ties) != 1 {
		t.Fatalf("accepted tie: %+v %v", ties, err)
	}
	if _, err := store.StartFriendConversation(ctx, ids[0], ids[1]); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("account id used as tie: %v", err)
	}
	chat, err := store.StartFriendConversation(ctx, ids[0], ties[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.StartFriendConversation(ctx, ids[1], ties[0].ID)
	if err != nil || again.ID != chat.ID {
		t.Fatalf("duplicate chat: %+v %v", again, err)
	}
	message, err := store.SendMessage(ctx, ids[0], chat.ID, "持久消息")
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.ListConversations(ctx, ids[1])
	if err != nil || len(list) != 1 || list[0].UnreadCount != 1 {
		t.Fatalf("receiver list: %+v %v", list, err)
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	history, err := New(fresh, false).ListMessages(ctx, ids[1], chat.ID)
	if err != nil || len(history) != 1 || history[0].ID != message.ID || history[0].Body != "持久消息" {
		t.Fatalf("restart history: %+v %v", history, err)
	}
	if err := store.BlockAccount(ctx, ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SendMessage(ctx, ids[0], chat.ID, "blocked"); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("blocked send: %v", err)
	}
	if err := store.UnblockAccount(ctx, ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartFriendConversation(ctx, ids[0], ties[0].ID); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("block removed Tie but chat resumed: %v", err)
	}
	if _, err := store.SendMessage(ctx, ids[0], chat.ID, "removed tie"); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("removed Tie allowed friend chat: %v", err)
	}
	if list, err := store.ListConversations(ctx, ids[0]); err != nil || len(list) != 0 {
		t.Fatalf("removed Tie exposed chat: %+v %v", list, err)
	}
}
