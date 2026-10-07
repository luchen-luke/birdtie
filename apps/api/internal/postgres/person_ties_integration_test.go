package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersonTieLifecycleIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for PostgreSQL Tie integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.person_ties') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("034_person_ties migration has not been applied")
	}
	var chatSchema bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.conversation_member_states') IS NOT NULL`).Scan(&chatSchema); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 3)
	for i := range ids {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,$2,'public')`, ids[i], "Synthetic Tie participant"); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_messages WHERE conversation_id IN (SELECT id FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	store := New(pool, false)
	request, err := store.CreateFriendRequest(ctx, ids[0], ids[1], "一起参加活动吗？")
	if err != nil || request.Scope != "friend" || request.CityID != "" {
		t.Fatalf("friend request: %+v %v", request, err)
	}
	if _, err := store.CreateFriendRequest(ctx, ids[1], ids[0], "重复申请"); !errors.Is(err, connection.ErrConflict) {
		t.Fatalf("reverse pending request: %v", err)
	}
	if ties, err := store.ListTies(ctx, ids[0]); err != nil || len(ties) != 0 {
		t.Fatalf("pending cannot create tie: %+v %v", ties, err)
	}
	if _, err := store.DecideRequest(ctx, ids[2], request.ID, "accept"); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("outsider decision: %v", err)
	}
	if _, err := store.DecideRequest(ctx, ids[1], request.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{ids[0], ids[1]}, {ids[1], ids[0]}} {
		ties, err := store.ListTies(ctx, pair[0])
		if err != nil || len(ties) != 1 || ties[0].OtherAccountID != pair[1] {
			t.Fatalf("bilateral tie: %+v %v", ties, err)
		}
	}
	var cityID any
	if err := pool.QueryRow(ctx, `SELECT city_id FROM connection_requests WHERE id=$1`, request.ID).Scan(&cityID); err != nil || cityID != nil {
		t.Fatalf("friend request is city-bound: %v %v", cityID, err)
	}
	if _, err := store.CreateFriendRequest(ctx, ids[1], ids[0], "已是好友"); err == nil {
		t.Fatal("duplicate active tie accepted")
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	ties, err := New(fresh, false).ListTies(ctx, ids[0])
	if err != nil || len(ties) != 1 {
		t.Fatalf("tie lost after new pool: %+v %v", ties, err)
	}
	if _, err := store.CreateFriendRequest(ctx, ids[0], ids[2], "另一个人"); err != nil {
		t.Fatal(err)
	}
	var conversationID string
	err = pool.QueryRow(ctx, `WITH request AS (
		INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
		VALUES($1,$2,'aberdeen-gb','Synthetic legacy chat','conversation','accepted',now()+interval '1 day') RETURNING id)
		INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id)
		SELECT id,$1,$2 FROM request RETURNING id`, ids[0], ids[1]).Scan(&conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BlockAccount(ctx, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if ties, err := store.ListTies(ctx, ids[1]); err != nil || len(ties) != 0 {
		t.Fatalf("block leaked tie: %+v %v", ties, err)
	}
	if _, err := store.ReadProfile(ctx, ids[0], ids[1]); err == nil {
		t.Fatal("block leaked profile")
	}
	if _, err := store.CreateFriendRequest(ctx, ids[1], ids[0], "blocked"); err == nil {
		t.Fatal("block accepted friend request")
	}
	if chatSchema {
		if conversations, err := store.ListConversations(ctx, ids[0]); err != nil || len(conversations) != 0 {
			t.Fatalf("block leaked chat: %+v %v", conversations, err)
		}
	}
	if _, err := store.SendMessage(ctx, ids[0], conversationID, "blocked message"); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("block allowed message: %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT status FROM person_ties WHERE id=$1`, ties[0].ID).Scan(&state); err != nil || state != "removed" {
		t.Fatalf("block did not remove Tie: %s %v", state, err)
	}
	if err := store.UnblockAccount(ctx, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if ties, err := store.ListTies(ctx, ids[0]); err != nil || len(ties) != 0 {
		t.Fatalf("unblock restored Tie: %+v %v", ties, err)
	}
	if err := store.RemoveTie(ctx, ids[2], ties[0].ID); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("outsider removed Tie: %v", err)
	}
	second, err := store.CreateFriendRequest(ctx, ids[1], ids[0], "重新加好友")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideRequest(ctx, ids[0], second.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	active, err := store.ListTies(ctx, ids[0])
	if err != nil || len(active) != 1 || active[0].ID != ties[0].ID {
		t.Fatalf("reactivated Tie: %+v %v", active, err)
	}
	if err := store.RemoveTie(ctx, ids[0], active[0].ID); err != nil {
		t.Fatal(err)
	}
	if active, err := store.ListTies(ctx, ids[1]); err != nil || len(active) != 0 {
		t.Fatalf("removed Tie visible: %+v %v", active, err)
	}
	if err := store.RemoveTie(ctx, ids[0], ties[0].ID); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("duplicate remove: %v", err)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='person_tie' AND action='remove'`, ids[0]).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("Tie removal audit: %d %v", auditCount, err)
	}
	if _, err := store.ReadProfile(ctx, ids[0], ids[1]); err != nil {
		t.Fatalf("explicit unblock did not restore public profile: %v", err)
	}
	if chatSchema {
		if conversations, err := store.ListConversations(ctx, ids[0]); err != nil || len(conversations) != 1 {
			t.Fatalf("legacy chat after unblock: %+v %v", conversations, err)
		}
	}
	if _, err := store.SendMessage(ctx, ids[0], conversationID, "unblocked message"); err != nil {
		t.Fatalf("legacy chat after unblock: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM person_ties WHERE id=$1`, ties[0].ID).Scan(&state); err != nil || state != "removed" {
		t.Fatalf("removed Tie not persistent: %s %v", state, err)
	}
	if _, err := store.CreateFriendRequest(ctx, ids[0], ids[1], "可重新申请"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFriendRequest(ctx, ids[0], ids[1], "重复待处理"); !errors.Is(err, connection.ErrConflict) {
		t.Fatalf("duplicate after remove: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND action='unblock'`, ids[0]).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("unblock audit: %d %v", auditCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND action='block'`, ids[0]).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("block audit: %d %v", auditCount, err)
	}
}
