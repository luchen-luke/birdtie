package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Each social test owns its people. Selecting the first accounts in a shared
// disposable database borrows another package's short-lived fixture identities.
func ownedSocialPeopleFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int) ([]string, func()) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	ids := make([]string, count)
	for i := range ids {
		if err := tx.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name) VALUES($1,'Owned social integration fixture')`, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return ids, func() {
		for _, sql := range []string{
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, err := pool.Exec(ctx, sql, ids); err != nil {
				t.Errorf("owned social Person cleanup: %v", err)
			}
		}
		var residue int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+(SELECT count(*) FROM audit_events WHERE actor_account_id=ANY($1::uuid[]))`, ids).Scan(&residue); err != nil || residue != 0 {
			t.Errorf("owned social Person/audit residue=%d error=%v", residue, err)
		}
	}
}

// Cleanup errors are test failures; never mask fixture leakage or delete another
// test's dependent rows to make account deletion succeed.
func ownedSocialFixtureCleanup(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, query, args...); err != nil {
		t.Errorf("owned social fixture cleanup: %v", err)
	}
}
