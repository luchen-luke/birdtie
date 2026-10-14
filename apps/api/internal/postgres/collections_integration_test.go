package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Uses an independently owned disposable database, never the running API DB.
func TestPersonalCollectionNativeCRUDPermissionsAndRecovery(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("NOT_RUN: BIRDTIE_DATABASE_URL not configured")
	}
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	uuid := func() string {
		t.Helper()
		var id string
		if e := pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	account := func() (identity.Actor, [32]byte, string) {
		t.Helper()
		id := uuid()
		exec(`INSERT INTO accounts(id,account_type) VALUES($1,'person')`, id)
		_, d, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		session := uuid()
		exec(`INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, session, id, d[:])
		return identity.Actor{ID: id, AccountType: "person"}, d, session
	}
	a, d, session := account()
	peer, pd, _ := account()
	place, place2 := uuid(), uuid()
	for _, id := range []string{place, place2} {
		exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'aberdeen-gb','合成集合地点','park',57.15,-2.1,'point','published','合成测试','disposable://collection','合成')`, id)
	}
	for _, kind := range []string{"journey", "theme_map"} {
		in := content.CollectionCreate{OperationID: uuid(), CollectionInput: content.CollectionInput{Kind: kind, CityID: "aberdeen-gb", Title: "合成个人收藏", Entries: []content.CollectionEntry{{PlaceID: place, Note: "第一站", LocationVisibility: "hidden"}, {PlaceID: place2, Note: "第二站", LocationVisibility: "city"}}}}
		v, e := s.CreateHumanCollection(ctx, d, a, in)
		if e != nil || v.OwnerID != a.ID || len(v.Entries) != 2 || v.Revision != 1 || v.Visibility != "private" || v.Entries[0].Latitude == nil || v.Entries[0].SourceExpiresAt == nil {
			t.Fatal(kind, v, e)
		}
		again, e := s.CreateHumanCollection(ctx, d, a, in)
		if e != nil || again.ID != v.ID || again.Revision != 1 {
			t.Fatal("lost receipt retry duplicated", again, e)
		}
		op, e := s.GetHumanCollectionOperation(ctx, d, a, in.OperationID)
		if e != nil || op.ID != v.ID {
			t.Fatal("operation recovery", op, e)
		}
		changed := in
		changed.Title = "different"
		if _, e = s.CreateHumanCollection(ctx, d, a, changed); !errors.Is(e, content.ErrConflict) {
			t.Fatal("operation accepted different draft", e)
		}
		if _, e = s.GetHumanCollection(ctx, pd, peer, kind, v.ID); !errors.Is(e, content.ErrNotFound) {
			t.Fatal("peer read private collection", e)
		}
		if _, e = s.GetHumanCollectionOperation(ctx, pd, peer, in.OperationID); !errors.Is(e, content.ErrNotFound) {
			t.Fatal("peer recovered owner operation", e)
		}
		update := in.CollectionInput
		update.Title = "修改后本人草稿"
		update.Entries = []content.CollectionEntry{in.Entries[1], in.Entries[0]}
		v, e = s.UpdateHumanCollection(ctx, d, a, v.ID, 1, update)
		if e != nil || v.Revision != 2 || v.Entries[0].PlaceID != place2 {
			t.Fatal("order or CAS update", v, e)
		}
		if _, e = s.UpdateHumanCollection(ctx, d, a, v.ID, 1, update); !errors.Is(e, content.ErrConflict) {
			t.Fatal("stale revision reused", e)
		}
		exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, place)
		read, e := s.GetHumanCollection(ctx, d, a, kind, v.ID)
		if e != nil || read.Entries[1].Available || read.Entries[1].PlaceName != "" || read.Entries[1].Latitude != nil {
			t.Fatal("withdrawn source leaked", read, e)
		}
		if _, e = s.UpdateHumanCollection(ctx, d, a, v.ID, 2, update); !errors.Is(e, content.ErrConflict) {
			t.Fatal("withdrawn place accepted", e)
		}
		exec(`UPDATE places SET publication_status='published' WHERE id=$1`, place)
		if e = s.WithdrawHumanCollection(ctx, d, a, kind, v.ID, 2); e != nil {
			t.Fatal(e)
		}
		read, e = s.GetHumanCollection(ctx, d, a, kind, v.ID)
		if e != nil || read.Status != "withdrawn" || read.Revision != 3 {
			t.Fatal(read, e)
		}
		page, e := s.ListHumanCollections(ctx, d, a, kind, "")
		if e != nil || len(page.Items) != 0 {
			t.Fatal("withdrawn remained visible", page, e)
		}
	}
	for i := 0; i < 25; i++ {
		_, e := s.CreateHumanCollection(ctx, d, a, content.CollectionCreate{OperationID: uuid(), CollectionInput: content.CollectionInput{Kind: "theme_map", CityID: "aberdeen-gb", Title: "分页合成", Entries: []content.CollectionEntry{{PlaceID: place, LocationVisibility: "hidden"}}}})
		if e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.ListHumanCollections(ctx, d, a, "theme_map", "")
	if e != nil || len(page.Items) != 20 || page.NextCursor == "" {
		t.Fatal("first page", page, e)
	}
	next, e := s.ListHumanCollections(ctx, d, a, "theme_map", page.NextCursor)
	if e != nil || len(next.Items) != 5 || next.NextCursor != "" {
		t.Fatal("second page", next, e)
	}
	seen := map[string]bool{}
	for _, v := range page.Items {
		seen[v.ID] = true
	}
	for _, v := range next.Items {
		if seen[v.ID] {
			t.Fatal("page duplicated ID")
		}
	}
	if _, e = s.ListHumanCollections(ctx, pd, peer, "theme_map", page.NextCursor); !errors.Is(e, content.ErrConflict) {
		t.Fatal("peer cursor borrowed authority", e)
	}
	exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session)
	if _, e = s.GetHumanCollection(ctx, d, a, "theme_map", page.Items[0].ID); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked session read", e)
	}
	if e = s.WithdrawHumanCollection(ctx, d, a, "theme_map", page.Items[0].ID, 1); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked session wrote", e)
	}
}
