package httpapi

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Each mutating test owns its Places; a UUID sort cannot identify seed ownership.
// The shared city is read only and missing seed preparation is a test failure.
func ownedSocialPlacesFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int) (string, []string, func()) {
	t.Helper()
	const cityID = "aberdeen-gb"
	var prepared bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cities WHERE id=$1 AND publication_status='published')`, cityID).Scan(&prepared); err != nil || !prepared {
		t.Fatalf("owned Place fixture requires prepared development city: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	ids := make([]string, count)
	for i := range ids {
		if err := tx.QueryRow(ctx, `INSERT INTO places
			(id,city_id,name,category_code,summary,latitude,longitude,coordinate_system,
			location_precision,publication_status,source_label,source_ref,maintainer_label,verified_at)
			VALUES(gen_random_uuid(),$1,'Owned synthetic Place','sports_venue','Development fixture only',
			57.15,-2.1,'wgs84','point','published','development fixture','test-only','Synthetic',now()) RETURNING id`, cityID).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return cityID, ids, func() {
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM places WHERE id=ANY($1::uuid[])`, ids)
	}
}
