package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOrganizationActivityMutationAuthorizationIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	accounts := make([]string, 4) // principal, owner, member, outsider
	for i := range accounts {
		kind := "person"
		if i == 0 {
			kind = "organization"
		}
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)
			VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&accounts[i]); err != nil {
			t.Fatal(err)
		}
	}
	var orgID, activityID string
	defer func() {
		if activityID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_events WHERE resource_type='activity' AND resource_id=$1`, activityID)
			_, _ = pool.Exec(ctx, `DELETE FROM activities WHERE id=$1`, activityID)
		}
		if orgID != "" {
			_, _ = pool.Exec(ctx, `DELETE FROM organization_memberships WHERE organization_id=$1`, orgID)
			_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, accounts)
	}()
	if err = pool.QueryRow(ctx, `INSERT INTO organizations(account_id,organization_type,name)
		VALUES($1,'club','Synthetic ownership club') RETURNING id`, accounts[0]).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO organization_memberships(organization_id,user_account_id,role)
		VALUES($1,$2,'owner'),($1,$3,'member')`, orgID, accounts[1], accounts[2]); err != nil {
		t.Fatal(err)
	}
	store := New(pool, false)
	start := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	in := activitypublish.Input{CityID: "aberdeen-gb", Title: "合成组织权限活动", Summary: "仅测试",
		StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Europe/London",
		Modality: "online", PhysicalPlaceStatus: "not_applicable", Visibility: "public"}
	for _, actor := range accounts[2:] {
		if _, err = store.CreateDraft(ctx, actor, orgID, in); !errors.Is(err, activitypublish.ErrForbidden) {
			t.Fatalf("non-admin created Organization activity: %v", err)
		}
	}
	draft, err := store.CreateDraft(ctx, accounts[1], orgID, in)
	if err != nil {
		t.Fatal(err)
	}
	activityID = draft.ID
	for _, actor := range accounts[2:] {
		if _, err = store.UpdateActivity(ctx, actor, orgID, activityID, in); !errors.Is(err, activitypublish.ErrForbidden) {
			t.Fatalf("non-admin edited Organization activity: %v", err)
		}
		if _, err = store.PublishActivity(ctx, actor, orgID, activityID); !errors.Is(err, activitypublish.ErrForbidden) {
			t.Fatalf("non-admin published Organization activity: %v", err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, accounts[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateDraft(ctx, accounts[1], orgID, in); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("disabled principal allowed create: %v", err)
	}
	if _, err = store.PublishActivity(ctx, accounts[1], orgID, activityID); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("disabled principal allowed publish: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE accounts SET status='active' WHERE id=$1`, accounts[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE organization_memberships SET role='admin'
		WHERE organization_id=$1 AND user_account_id=$2`, orgID, accounts[2]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateActivity(ctx, accounts[2], orgID, activityID, in); err != nil {
		t.Fatalf("admin edit: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE organization_memberships SET role='member'
		WHERE organization_id=$1 AND user_account_id=$2`, orgID, accounts[2]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PublishActivity(ctx, accounts[1], orgID, activityID); err != nil {
		t.Fatalf("owner publish: %v", err)
	}
	for _, actor := range accounts[2:] {
		if _, err = store.CancelActivity(ctx, actor, orgID, activityID); !errors.Is(err, activitypublish.ErrForbidden) {
			t.Fatalf("non-admin cancelled Organization activity: %v", err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, accounts[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelActivity(ctx, accounts[1], orgID, activityID); !errors.Is(err, activitypublish.ErrForbidden) {
		t.Fatalf("disabled principal allowed cancel: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE accounts SET status='active' WHERE id=$1`, accounts[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelActivity(ctx, accounts[1], orgID, activityID); err != nil {
		t.Fatalf("owner cancel: %v", err)
	}
}
