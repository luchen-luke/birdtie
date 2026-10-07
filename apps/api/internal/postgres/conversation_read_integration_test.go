package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConversationReadStateIntegration(t *testing.T) {
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
	ids := make([]string, 3)
	for i := range ids {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'Synthetic reader','public')`, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	var requestID, conversationID string
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_member_states WHERE conversation_id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM conversation_messages WHERE conversation_id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM connection_requests WHERE id=$1`, requestID)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	err = pool.QueryRow(ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
		VALUES($1,$2,'aberdeen-gb','Synthetic chat','conversation','accepted',now()+interval '1 day') RETURNING id`, ids[0], ids[1]).Scan(&requestID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id)
		VALUES($1,$2,$3) RETURNING id`, requestID, ids[0], ids[1]).Scan(&conversationID)
	if err != nil {
		t.Fatal(err)
	}
	var memberCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conversation_member_states WHERE conversation_id=$1`, conversationID).Scan(&memberCount); err != nil || memberCount != 2 {
		t.Fatalf("member state: %d %v", memberCount, err)
	}
	store := New(pool, false)
	first, err := store.SendMessage(ctx, ids[0], conversationID, "first")
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.ListConversations(ctx, ids[1])
	if err != nil || len(list) != 1 || list[0].UnreadCount != 1 || list[0].LastReadAt != nil {
		t.Fatalf("first unread: %+v %v", list, err)
	}
	if _, err := store.MarkRead(ctx, ids[2], conversationID, first.ID); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("outsider marked read: %v", err)
	}
	if _, err := store.MarkRead(ctx, ids[1], conversationID, ids[2]); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("foreign cursor accepted: %v", err)
	}
	read, err := store.MarkRead(ctx, ids[1], conversationID, first.ID)
	if err != nil || read.LastReadMessageID != first.ID {
		t.Fatalf("mark read: %+v %v", read, err)
	}
	list, err = store.ListConversations(ctx, ids[1])
	if err != nil || list[0].UnreadCount != 0 || list[0].LastReadAt == nil {
		t.Fatalf("read cursor: %+v %v", list, err)
	}
	second, err := store.SendMessage(ctx, ids[0], conversationID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRead(ctx, ids[1], conversationID, first.ID); err != nil {
		t.Fatal(err)
	}
	list, err = store.ListConversations(ctx, ids[1])
	if err != nil || list[0].UnreadCount != 1 {
		t.Fatalf("backward cursor lost unread: %+v %v", list, err)
	}
	if _, err := store.MarkRead(ctx, ids[1], conversationID, second.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	list, err = New(fresh, false).ListConversations(ctx, ids[1])
	if err != nil || len(list) != 1 || list[0].UnreadCount != 0 || list[0].LastReadAt == nil {
		t.Fatalf("read state lost after reconnect: %+v %v", list, err)
	}
	var entityCardsInstalled bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_name='conversation_messages' AND column_name='entity_type')`).Scan(&entityCardsInstalled); err != nil {
		t.Fatal(err)
	}
	if entityCardsInstalled {
		personCard, err := store.SendEntityMessage(ctx, ids[0], conversationID, "分享用户", "person", ids[2])
		if err != nil || personCard.Entity == nil || personCard.Entity.ID != ids[2] {
			t.Fatalf("public person card: %+v %v", personCard, err)
		}
		for _, fixture := range []struct{ kind, id string }{
			{"place", "b1700000-0000-4000-8000-000000000004"},
			{"activity", "b1700000-0000-4000-8000-000000000017"},
			{"community", "b1700000-0000-4000-8000-000000000030"},
		} {
			card, err := store.SendEntityMessage(ctx, ids[0], conversationID, "分享卡片", fixture.kind, fixture.id)
			if err != nil || card.Entity == nil || !card.Entity.Available {
				t.Fatalf("%s card: %+v %v", fixture.kind, card, err)
			}
		}
		var orgAccount, orgID, businessAccount, businessID string
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)
			VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&orgAccount); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, orgAccount)
		if err := pool.QueryRow(ctx, `INSERT INTO organizations(account_id,organization_type,name)
			VALUES($1,'club','Synthetic card organization') RETURNING id`, orgAccount).Scan(&orgID); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID)
		if _, err := store.SendEntityMessage(ctx, ids[0], conversationID, "unverified", "organization", orgID); !errors.Is(err, connection.ErrNotFound) {
			t.Fatalf("unverified organization shared: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE organizations SET verification_status='verified' WHERE id=$1`, orgID); err != nil {
			t.Fatal(err)
		}
		if card, err := store.SendEntityMessage(ctx, ids[0], conversationID, "organization", "organization", orgID); err != nil || card.Entity == nil || !card.Entity.Available {
			t.Fatalf("organization card: %+v %v", card, err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)
			VALUES(gen_random_uuid(),'business') RETURNING id`).Scan(&businessAccount); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, businessAccount)
		if err := pool.QueryRow(ctx, `INSERT INTO businesses(account_id,name)
			VALUES($1,'Synthetic card business') RETURNING id`, businessAccount).Scan(&businessID); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DELETE FROM businesses WHERE id=$1`, businessID)
		if _, err := store.SendEntityMessage(ctx, ids[0], conversationID, "pending", "business", businessID); !errors.Is(err, connection.ErrNotFound) {
			t.Fatalf("pending business shared: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE businesses SET claim_status='verified',
			claim_source_url='https://example.org/synthetic-card',claim_reviewed_by=$2,claim_reviewed_at=now()
			WHERE id=$1`, businessID, ids[2]); err != nil {
			t.Fatal(err)
		}
		if card, err := store.SendEntityMessage(ctx, ids[0], conversationID, "business", "business", businessID); err != nil || card.Entity == nil || !card.Entity.Available {
			t.Fatalf("business card: %+v %v", card, err)
		}
		var momentID string
		if err := pool.QueryRow(ctx, `INSERT INTO moments(author_account_id,city_id,title,body)
			VALUES($1,'aberdeen-gb','Synthetic private card','private') RETURNING id`, ids[0]).Scan(&momentID); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DELETE FROM moments WHERE id=$1`, momentID)
		if _, err := store.SendEntityMessage(ctx, ids[0], conversationID, "private", "moment", momentID); !errors.Is(err, connection.ErrNotFound) {
			t.Fatalf("private moment shared: %v", err)
		}
		// The old fixture flipped flags without an explicit public Place. Use
		// the actual draft/preview/confirmed-publication domain flow instead.
		publicFixture := placeMemoryNativeFixture(t)
		publicAuthor := momentPublicationOrdinary(t, publicFixture)
		publicDraft := momentPublicationDraft(t, publicFixture, publicAuthor)
		publicMoment := momentPublicationPublish(t, publicFixture, publicAuthor, publicDraft)
		momentCard, err := store.SendEntityMessage(ctx, ids[0], conversationID, "moment", "moment", publicDraft.ID)
		if err != nil || momentCard.Entity == nil || !momentCard.Entity.Available {
			t.Fatalf("public moment card: %+v %v", momentCard, err)
		}
		if err := publicFixture.private.base.store.WithdrawHumanMoment(ctx, publicAuthor.digest, publicAuthor.actor, publicDraft.ID, publicMoment.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SendEntityMessage(ctx, ids[2], conversationID, "outsider", "place", "b1700000-0000-4000-8000-000000000004"); !errors.Is(err, connection.ErrNotFound) {
			t.Fatalf("outsider shared to conversation: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, ids[2]); err != nil {
			t.Fatal(err)
		}
		messages, err := New(fresh, false).ListMessages(ctx, ids[1], conversationID)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if (message.ID == personCard.ID || message.ID == momentCard.ID) && (message.Entity == nil || message.Entity.Available || message.Entity.ID != "" || message.Entity.Title != "") {
				t.Fatalf("revoked person details leaked: %+v", message.Entity)
			}
		}
	}
	if err := store.BlockAccount(ctx, ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRead(ctx, ids[1], conversationID, second.ID); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("blocked read: %v", err)
	}
	list, err = store.ListConversations(ctx, ids[1])
	if err != nil || len(list) != 0 {
		t.Fatalf("blocked list: %+v %v", list, err)
	}
}
