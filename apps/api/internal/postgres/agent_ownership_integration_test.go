package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentOwnershipAndWorkspaceAuthorizationIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for PostgreSQL ownership integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool, false)
	var owner, outsider, orgAccount, orgID string
	defer func() {
		ids := make([]string, 0, 3)
		for _, id := range []string{owner, outsider, orgAccount} {
			if id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id = ANY($1::uuid[])`, ids)
		}
	}()
	for _, out := range []*string{&owner, &outsider} {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts (id,account_type)
            VALUES (gen_random_uuid(),'person') RETURNING id`).Scan(out); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (id,account_type)
        VALUES (gen_random_uuid(),'organization') RETURNING id`).Scan(&orgAccount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (account_id,organization_type,name)
        VALUES ($1,'club','Synthetic Agent ownership check') RETURNING id`, orgAccount).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_memberships
        (organization_id,user_account_id,role) VALUES ($1,$2,'owner')`, orgID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents (agent_type,principal_account_id)
        VALUES ('personal',$1),('organization',$2)`, owner, orgAccount); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, id string
		want     bool
	}{
		{"person", owner, true}, {"organization", orgAccount, true},
		{"person", orgAccount, false}, {"organization", owner, false},
		{"business", orgAccount, false}, {"person", outsider, false},
	} {
		got, err := store.HasActiveAgent(ctx, tc.kind, tc.id)
		if err != nil || got != tc.want {
			t.Fatalf("active Agent %s/%s: got=%v err=%v want=%v", tc.kind, tc.id, got, err, tc.want)
		}
	}
	principalID, role, err := store.ResolveWorkspace(ctx, owner, orgID)
	if err != nil || principalID != orgAccount || role != "owner" {
		t.Fatalf("authorized workspace: %s %s %v", principalID, role, err)
	}
	if _, _, err := store.ResolveWorkspace(ctx, outsider, orgID); !errors.Is(err, organization.ErrForbidden) {
		t.Fatalf("outsider entered workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agents SET status='suspended'
        WHERE agent_type='organization' AND principal_account_id=$1`, orgAccount); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveWorkspace(ctx, owner, orgID); !errors.Is(err, organization.ErrForbidden) {
		t.Fatalf("suspended organization Agent accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agents SET status='active'
        WHERE agent_type='organization' AND principal_account_id=$1`, orgAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE organization_memberships SET status='removed'
        WHERE organization_id=$1 AND user_account_id=$2`, orgID, owner); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolveWorkspace(ctx, owner, orgID); !errors.Is(err, organization.ErrForbidden) {
		t.Fatalf("removed member entered organization Agent workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agents SET status='suspended'
        WHERE agent_type='personal' AND principal_account_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if active, err := store.HasActiveAgent(ctx, "person", owner); err != nil || active {
		t.Fatalf("suspended personal Agent accepted: %v %v", active, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents (agent_type,principal_account_id)
        VALUES ('city',NULL)`); err == nil {
		t.Fatal("city context was accepted as an Agent")
	}
}
