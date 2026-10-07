package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSocialReportTargetsIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ids := make([]string, 4) // sender, recipient, outsider, Business principal
	for i := range ids {
		kind := "person"
		if i == 3 {
			kind = "business"
		}
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)
			VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	var requestID, conversationID, messageID, communityID, businessID string
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM incident_reports WHERE reporter_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		if messageID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM conversation_messages WHERE id=$1`, messageID)
		}
		if conversationID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM conversation_member_states WHERE conversation_id=$1`, conversationID)
			_, _ = pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
		}
		if requestID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM connection_requests WHERE id=$1`, requestID)
		}
		if communityID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM community_memberships WHERE community_id=$1`, communityID)
			_, _ = pool.Exec(ctx, `DELETE FROM communities WHERE id=$1`, communityID)
		}
		if businessID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM businesses WHERE id=$1`, businessID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	if err = pool.QueryRow(ctx, `INSERT INTO connection_requests
		(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
		VALUES($1,$2,'aberdeen-gb','Synthetic safety report','conversation','accepted',now()+interval '1 day')
		RETURNING id`, ids[0], ids[1]).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO conversations
		(request_id,member_a_account_id,member_b_account_id) VALUES($1,$2,$3) RETURNING id`,
		requestID, ids[0], ids[1]).Scan(&conversationID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body)
		VALUES($1,$2,'Synthetic offensive message') RETURNING id`, conversationID, ids[0]).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	store := New(pool, false)
	report := func(reporter, kind, id string) error {
		_, e := store.CreateReport(ctx, reporter, safety.ReportInput{
			TargetType: kind, TargetID: id, Reason: "harassment", Details: "Synthetic safety report detail",
		})
		return e
	}
	if err = report(ids[2], "message", messageID); !errors.Is(err, safety.ErrTargetNotFound) {
		t.Fatalf("outsider message report: %v", err)
	}
	if err = report(ids[0], "message", messageID); !errors.Is(err, safety.ErrTargetNotFound) {
		t.Fatalf("self-message report: %v", err)
	}
	if err = report(ids[1], "message", messageID); err != nil {
		t.Fatalf("recipient message report: %v", err)
	}
	if err = store.BlockAccount(ctx, ids[1], ids[0]); err != nil {
		t.Fatal(err)
	}
	if err = report(ids[1], "message", messageID); err != nil {
		t.Fatalf("blocked sender message report: %v", err)
	}
	comm, err := store.CreateSocialCommunity(ctx, ids[0], community.SocialInput{
		Name: "Synthetic report Community", Visibility: "public", JoinPolicy: "request", CityID: "aberdeen-gb",
	})
	if err != nil {
		t.Fatal(err)
	}
	communityID = comm.ID
	if err = report(ids[1], "community", communityID); err != nil {
		t.Fatalf("public Community report: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE communities SET visibility='hidden' WHERE id=$1`, communityID); err != nil {
		t.Fatal(err)
	}
	if err = report(ids[2], "community", communityID); !errors.Is(err, safety.ErrTargetNotFound) {
		t.Fatalf("hidden Community outsider report: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO community_memberships(community_id,user_account_id,role,status)
		VALUES($1,$2,'member','active')`, communityID, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err = report(ids[1], "community", communityID); err != nil {
		t.Fatalf("hidden Community member report: %v", err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO businesses(account_id,name,claim_status,claim_source_url,claim_reviewed_by,claim_reviewed_at)
		VALUES($1,'Synthetic report Business','pending',NULL,NULL,NULL) RETURNING id`, ids[3]).Scan(&businessID); err != nil {
		t.Fatal(err)
	}
	if err = report(ids[1], "business", businessID); !errors.Is(err, safety.ErrTargetNotFound) {
		t.Fatalf("pending Business report: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE businesses SET claim_status='verified',claim_source_url='https://example.org/test',
		claim_reviewed_by=$2,claim_reviewed_at=now() WHERE id=$1`, businessID, ids[2]); err != nil {
		t.Fatal(err)
	}
	if err = report(ids[1], "business", businessID); err != nil {
		t.Fatalf("verified Business report: %v", err)
	}
	if err = report(ids[1], "business", businessID); !errors.Is(err, safety.ErrRateLimited) {
		t.Fatalf("report rate limit: %v", err)
	}
	if receipts, e := store.ListOwnReports(ctx, ids[1]); e != nil || len(receipts) != 5 {
		t.Fatalf("report receipts: %d %v", len(receipts), e)
	}
}
