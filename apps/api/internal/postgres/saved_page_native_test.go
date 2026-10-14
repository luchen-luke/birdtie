package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/jackc/pgx/v5/pgxpool"
)

type savedPageNativeFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *Store
	actor   identity.Actor
	digest  [32]byte
	session string
}

func savedPageNative(t *testing.T) *savedPageNativeFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &savedPageNativeFixture{ctx: ctx, pool: pool, store: New(pool, false)}
	id := f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, id)
	_, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	f.actor = identity.Actor{ID: id, AccountType: "person"}
	f.digest = digest
	f.session = f.id(t)
	f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.session, id, digest[:])
	return f
}
func (f *savedPageNativeFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, q, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *savedPageNativeFixture) id(t *testing.T) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *savedPageNativeFixture) bookmark(t *testing.T) (string, string) {
	t.Helper()
	place := f.id(t)
	f.exec(t, `INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'aberdeen-gb','合成分页收藏','park',57.15,-2.1,'point','published','合成测试','disposable://saved-page','合成')`, place)
	id, err := f.store.Save(f.ctx, f.actor.ID, "place", place)
	if err != nil {
		t.Fatal(err)
	}
	return id, place
}
func TestSavedPageNativeBeyondHundredStableOrderAndPrivateNote(t *testing.T) {
	f := savedPageNative(t)
	var first, firstPlace string
	for i := 0; i < 101; i++ {
		id, place := f.bookmark(t)
		if i == 0 {
			first = id
			firstPlace = place
		}
	}
	_, err := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.actor, first, saved.NoteInput{Note: "本人原始收藏的私人备注", ExpectedRevision: 0, OperationID: f.id(t)})
	if err != nil {
		t.Fatal("note setup", err)
	}
	initial, err := f.store.ListHumanSavedPage(f.ctx, f.digest, f.actor, "")
	if err != nil || len(initial.Items) != 20 {
		t.Fatal("bounded initial page", err)
	}
	for _, item := range initial.Items {
		if item.ID == first {
			t.Fatal("oldest bookmark unexpectedly on first page")
		}
	}
	member, err := f.store.GetHumanSavedTarget(f.ctx, f.digest, f.actor, "place", firstPlace)
	if err != nil || member == nil || member.ID != first || member.NoteRevision != 1 {
		t.Fatal("membership outside visible first page lost", err)
	}
	seen := map[string]bool{}
	cursor := ""
	found := false
	pages := 0
	for {
		page, err := f.store.ListHumanSavedPage(f.ctx, f.digest, f.actor, cursor)
		if err != nil || saved.ValidatePage(page, cursor) != nil {
			t.Fatal("actual page", err)
		}
		pages++
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate across stable pages")
			}
			seen[item.ID] = true
			if item.ID == first {
				found = item.Note == "本人原始收藏的私人备注" && item.NoteRevision == 1
			}
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
		if pages > 6 {
			t.Fatal("unbounded cursor loop")
		}
	}
	if len(seen) != 101 || pages != 6 || !found {
		t.Fatal("older bookmark hidden", len(seen), pages, found)
	}
	legacy, err := f.store.ListSaved(f.ctx, f.actor.ID)
	if err != nil || len(legacy) != 100 {
		t.Fatal("old list compatibility", err)
	}
	for _, item := range legacy {
		if item.Note != "" || item.NoteRevision != 0 {
			t.Fatal("note entered legacy Agent/source projection")
		}
	}
}
func TestSavedPageNativeForeignDeletedCursorAndHiddenSource(t *testing.T) {
	f := savedPageNative(t)
	id, place := f.bookmark(t)
	if _, err := f.store.ListHumanSavedPage(f.ctx, f.digest, f.actor, f.id(t)); !errors.Is(err, saved.ErrPageChanged) {
		t.Fatal("foreign cursor", err)
	}
	if _, err := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.actor, id, saved.NoteInput{Note: "隐藏来源仍属本人的备注", ExpectedRevision: 0, OperationID: f.id(t)}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE places SET publication_status='draft' WHERE id=$1`, place)
	page, err := f.store.ListHumanSavedPage(f.ctx, f.digest, f.actor, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatal(err)
	}
	item := page.Items[0]
	if item.Available || item.Title != "" || item.Summary != "" || item.CityID != "" || item.Note != "隐藏来源仍属本人的备注" {
		t.Fatal("source redaction/owner note", item)
	}
	member, err := f.store.GetHumanSavedTarget(f.ctx, f.digest, f.actor, "place", place)
	if err != nil || member == nil || member.ID != id || member.Available || member.Title != "" || member.Note != item.Note {
		t.Fatal("hidden target lookup bypassed redaction", err)
	}
	absent, err := f.store.GetHumanSavedTarget(f.ctx, f.digest, f.actor, "place", f.id(t))
	if err != nil || absent != nil {
		t.Fatal("foreign/unsaved target exposed bookmark", err)
	}
	if err := f.store.RemoveSaved(f.ctx, f.actor.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ListHumanSavedPage(f.ctx, f.digest, f.actor, id); !errors.Is(err, saved.ErrPageChanged) {
		t.Fatal("deleted cursor silently rebased", err)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if _, err := f.store.ListHumanSavedPage(f.ctx, f.digest, f.actor, ""); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked session", err)
	}
}
