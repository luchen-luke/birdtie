package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSocialDiscoveryAndReportPrivacyIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for PostgreSQL privacy integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var cityReady bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cities WHERE id='aberdeen-gb' AND publication_status='published')`).Scan(&cityReady); err != nil {
		t.Fatal(err)
	}
	if !cityReady {
		t.Skip("published Aberdeen fixture required")
	}
	ids := make([]string, 2)
	for i := range ids {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'Synthetic privacy participant','private')`, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM incident_reports WHERE reporter_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM intents WHERE owner_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ids)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ids)
	}()
	store := New(pool, false)
	topic := fmt.Sprintf("synthetic-privacy-%d", time.Now().UnixNano())
	var intentID string
	if err := pool.QueryRow(ctx, `INSERT INTO intents(owner_account_id,city_id,topic,details,
		available_from,available_until,time_zone,coarse_area_label,audience,state,expires_at,owner_confirmed_at)
		VALUES($1,'aberdeen-gb',$2,'Synthetic private-context check',now()-interval '1 minute',
		now()+interval '1 day','Europe/London','city-wide','public','active',now()+interval '1 day',now())
		RETURNING id`, ids[0], topic).Scan(&intentID); err != nil {
		t.Fatal(err)
	}
	search := func(viewer string) int {
		t.Helper()
		result, err := store.Search(ctx, "aberdeen-gb", viewer, []string{topic})
		if err != nil {
			t.Fatal(err)
		}
		return len(result.People)
	}
	if search("") != 0 || search(ids[1]) != 0 {
		t.Fatal("private profile exposed public Intent")
	}
	if _, err := store.ReadProfile(ctx, ids[1], ids[0]); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("private profile leaked: %v", err)
	}
	if _, err := store.GrantProfileRead(ctx, ids[0], ids[1], time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadProfile(ctx, ids[1], ids[0]); err != nil {
		t.Fatalf("explicit profile grant denied: %v", err)
	}
	if search(ids[1]) != 0 {
		t.Fatal("profile grant improperly published Intent")
	}
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if search(ids[1]) != 1 {
		t.Fatal("confirmed public Intent missing")
	}
	if err := store.BlockAccount(ctx, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if search(ids[1]) != 0 {
		t.Fatal("blocked viewer discovered Intent")
	}
	if _, err := store.ReadProfile(ctx, ids[1], ids[0]); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("block leaked profile: %v", err)
	}
	if err := store.UnblockAccount(ctx, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadProfile(ctx, ids[1], ids[0]); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("block revocation did not persist after unblock: %v", err)
	}
	if search(ids[1]) != 0 {
		t.Fatal("private Intent leaked after unblock")
	}
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := store.WithdrawIntent(ctx, ids[0], intentID); err != nil {
		t.Fatal(err)
	}
	if search(ids[1]) != 0 {
		t.Fatal("withdrawn Intent discoverable")
	}
	if err := store.BlockAccount(ctx, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	input := safety.ReportInput{TargetType: "account", TargetID: ids[0], Reason: "harassment", Details: "Synthetic harassment report for privacy verification"}
	for i := 0; i < 5; i++ {
		if _, err := store.CreateReport(ctx, ids[1], input); err != nil {
			t.Fatalf("blocked reporter could not report account: %v", err)
		}
	}
	if _, err := store.CreateReport(ctx, ids[1], input); !errors.Is(err, safety.ErrRateLimited) {
		t.Fatalf("report rate limit missing: %v", err)
	}
	if reports, err := store.ListOwnReports(ctx, ids[1]); err != nil || len(reports) != 5 {
		t.Fatalf("reporter receipts: %d %v", len(reports), err)
	}
	if reports, err := store.ListOwnReports(ctx, ids[0]); err != nil || len(reports) != 0 {
		t.Fatalf("report target accessed reporter records: %d %v", len(reports), err)
	}
}
