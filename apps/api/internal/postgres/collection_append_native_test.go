package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func batchAAppendFixture(t *testing.T, f *savedPageNativeFixture, kind string) (content.PersonalCollection, string) {
	t.Helper()
	_, first := f.bookmark(t)
	_, second := f.bookmark(t)
	c, e := f.store.CreateHumanCollection(f.ctx, f.digest, f.actor, content.CollectionCreate{OperationID: f.id(t), CollectionInput: content.CollectionInput{Kind: kind, CityID: "aberdeen-gb", Title: "合成原集合", Summary: "保留原概述", Entries: []content.CollectionEntry{{PlaceID: first, Note: "原集合私人备注保持", LocationVisibility: "hidden"}}}})
	if e != nil {
		t.Fatal(e)
	}
	return c, second
}
func TestBatchACollectionAppendCASOriginalReceiptsStopAndPrivateIsolation(t *testing.T) {
	f := savedPageNative(t)
	for _, name := range []string{"136_personal_collection_append_operations.down.sql", "136_personal_collection_append_operations.sql"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.pool.Exec(f.ctx, string(raw)); e != nil {
			t.Fatal("empty owned 136 roundtrip", e)
		}
	}
	otherID := f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, otherID)
	_, otherDigest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.id(t), otherID, otherDigest[:])
	other := identity.Actor{ID: otherID, AccountType: "person"}
	for _, kind := range []string{"journey", "theme_map"} {
		t.Run(kind, func(t *testing.T) {
			c, place := batchAAppendFixture(t, f, kind)
			in := content.CollectionAppendInput{CityID: c.CityID, ExpectedRevision: c.Revision, OperationID: f.id(t), Entries: []content.CollectionAppendEntry{{PlaceID: place, Note: "", LocationVisibility: "hidden"}}}
			r, e := f.store.AppendHumanCollectionPlaces(f.ctx, f.digest, f.actor, kind, c.ID, in)
			if e != nil || r.AppliedRevision != 2 || r.Status != "SUCCEEDED" {
				t.Fatal("actual append", e)
			}
			if again, e := f.store.AppendHumanCollectionPlaces(f.ctx, f.digest, f.actor, kind, c.ID, in); e != nil || again != r {
				t.Fatal("lost reply duplicated effect", e)
			}
			current, e := f.store.GetHumanCollection(f.ctx, f.digest, f.actor, kind, c.ID)
			if e != nil || current.Revision != 2 || current.Title != c.Title || current.Summary != c.Summary || len(current.Entries) != 2 || current.Entries[0].Note != c.Entries[0].Note || current.Entries[1].Note != "" || current.Entries[1].LocationVisibility != "hidden" {
				t.Fatal("append replaced or copied private data", e)
			}
			input := content.CollectionInput{Kind: kind, CityID: c.CityID, Title: "后来编辑", Entries: []content.CollectionEntry{}}
			for _, i := range current.Entries {
				input.Entries = append(input.Entries, content.CollectionEntry{PlaceID: i.PlaceID, Note: i.Note, LocationVisibility: i.LocationVisibility})
			}
			if _, e = f.store.UpdateHumanCollection(f.ctx, f.digest, f.actor, c.ID, 2, input); e != nil {
				t.Fatal(e)
			}
			if receipt, e := f.store.GetHumanCollectionAppendOperation(f.ctx, f.digest, f.actor, kind, c.ID, in.OperationID); e != nil || receipt != r {
				t.Fatal("later edit rewrote original receipt", e)
			}
			if stopped, e := f.store.StopHumanCollectionAppendOperation(f.ctx, f.digest, f.actor, kind, c.ID, in.OperationID, content.CollectionAppendStopInput{CityID: c.CityID, ExpectedRevision: 1}); e != nil || stopped != r {
				t.Fatal("success wrongly called no-effect", e)
			}
			bad := in
			bad.OperationID = f.id(t)
			bad.ExpectedRevision = 3
			if _, e = f.store.AppendHumanCollectionPlaces(f.ctx, f.digest, f.actor, kind, c.ID, bad); !errors.Is(e, content.ErrConflict) {
				t.Fatal("existing Place duplicated", e)
			}
			_, third := f.bookmark(t)
			late := content.CollectionAppendInput{CityID: c.CityID, ExpectedRevision: 3, OperationID: f.id(t), Entries: []content.CollectionAppendEntry{{PlaceID: third, Note: "", LocationVisibility: "hidden"}}}
			stop, e := f.store.StopHumanCollectionAppendOperation(f.ctx, f.digest, f.actor, kind, c.ID, late.OperationID, content.CollectionAppendStopInput{CityID: c.CityID, ExpectedRevision: 3})
			if e != nil || stop.Status != "STOPPED" || stop.AppliedRevision != 0 {
				t.Fatal("durable no-effect tombstone", e)
			}
			if result, e := f.store.AppendHumanCollectionPlaces(f.ctx, f.digest, f.actor, kind, c.ID, late); e != nil || result != stop {
				t.Fatal("late original POST passed stop", e)
			}
			final, e := f.store.GetHumanCollection(f.ctx, f.digest, f.actor, kind, c.ID)
			if e != nil || final.Revision != 3 || len(final.Entries) != 2 {
				t.Fatal("STOPPED had effects", e)
			}
			if _, e = f.store.GetHumanCollectionAppendOperation(f.ctx, otherDigest, other, kind, c.ID, in.OperationID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("foreign caller recovered receipt", e)
			}
			foreign := late
			foreign.OperationID = f.id(t)
			if _, e = f.store.AppendHumanCollectionPlaces(f.ctx, otherDigest, other, kind, c.ID, foreign); !errors.Is(e, content.ErrConflict) {
				t.Fatal("foreign caller appended", e)
			}
			pub := content.CollectionPublicationInput{OperationID: f.id(t), Revision: 3, Entries: []content.CollectionPublicationEntry{}}
			for _, entry := range final.Entries {
				pub.Entries = append(pub.Entries, content.CollectionPublicationEntry{PlaceID: entry.PlaceID, LocationVisibility: "hidden"})
			}
			if _, e = f.store.PublishHumanCollection(f.ctx, f.digest, f.actor, kind, c.ID, pub); e != nil {
				t.Fatal("actual public lifecycle", e)
			}
			blocked := late
			blocked.OperationID = f.id(t)
			blocked.ExpectedRevision = 4
			if _, e = f.store.AppendHumanCollectionPlaces(f.ctx, f.digest, f.actor, kind, c.ID, blocked); !errors.Is(e, content.ErrConflict) {
				t.Fatal("published object edited through append", e)
			}
			if _, e = f.store.GetHumanCollectionAppendOperation(f.ctx, f.digest, f.actor, kind, c.ID, f.id(t)); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("missing receipt manufactured no-effect", e)
			}
		})
	}
	downgrade, e := os.ReadFile(filepath.Join("..", "..", "migrations", "136_personal_collection_append_operations.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(f.ctx, string(downgrade)); e == nil {
		conn.Release()
		t.Fatal("used append history lost on down")
	}
	_, _ = conn.Exec(f.ctx, "ROLLBACK")
	conn.Release()
}
func TestBatchACollectionAppendWaitSessionABARollsBack(t *testing.T) {
	f := savedPageNative(t)
	c, place := batchAAppendFixture(t, f, "journey")
	in := content.CollectionAppendInput{CityID: c.CityID, ExpectedRevision: 1, OperationID: f.id(t), Entries: []content.CollectionAppendEntry{{PlaceID: place, Note: "", LocationVisibility: "hidden"}}}
	blocker, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(f.ctx, `SELECT id FROM journeys WHERE id=$1 FOR UPDATE`, c.ID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := f.store.AppendHumanCollectionPlaces(f.ctx, f.digest, f.actor, "journey", c.ID, in)
		done <- e
	}()
	until := time.Now().Add(4 * time.Second)
	waiting := false
	for time.Now().Before(until) {
		if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%FROM journeys%' AND query LIKE '%FOR UPDATE%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("append did not reach original resource wait")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = blocker.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, content.ErrConflict) {
			t.Fatal("original Session ABA accepted", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("append wait did not finish")
	}
	current, e := f.store.GetHumanCollection(f.ctx, f.digest, f.actor, "journey", c.ID)
	if e != nil || current.Revision != 1 || len(current.Entries) != 1 {
		t.Fatal("stale Session append wrote state", e)
	}
	if _, e = f.store.GetHumanCollectionAppendOperation(f.ctx, f.digest, f.actor, "journey", c.ID, in.OperationID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("rolled-back append manufactured receipt", e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if _, e = f.store.AppendHumanCollectionPlaces(f.ctx, f.digest, f.actor, "journey", c.ID, in); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked Session accepted", e)
	}
}
