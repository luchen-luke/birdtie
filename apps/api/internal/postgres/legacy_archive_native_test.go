package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

type legacyReaderNativeFixture struct {
	*legacyNativeFixture
	store       *Store
	actor       identity.Actor
	digest      [32]byte
	session, id string
}

func legacyReaderNative(t *testing.T) *legacyReaderNativeFixture {
	t.Helper()
	f := legacyMigrationNativeFixture(t)
	raw, manifest := legacySyntheticInput(t, func(tables map[string]any) {
		notes := tables["notes"].([]any)
		template := notes[0].(map[string]any)
		for i := 0; i < 21; i++ {
			n := map[string]any{}
			for k, v := range template {
				n[k] = v
			}
			n["id"] = fmt.Sprintf("14600000-0000-4000-8000-%012x", 100+i)
			n["caption"] = fmt.Sprintf("ROOT_SYNTHETIC_PRIVATE_NOTE_%d\n保留原文", i)
			notes = append(notes, n)
		}
		tables["notes"] = notes
		makeRow := func(table string) map[string]any {
			row := map[string]any{}
			for _, key := range legacymigration.ColumnNames(table) {
				row[key] = nil
			}
			return row
		}
		places := []any{}
		for i, name := range []string{"原输入地点甲", "原输入地点乙"} {
			r := makeRow("note_places")
			r["note_id"] = template["id"]
			r["raw_name"] = name
			r["position"] = i
			places = append(places, r)
		}
		tables["note_places"] = places
		asset := makeRow("media_assets")
		asset["id"] = "14600000-0000-4000-8000-000000000030"
		asset["owner_id"] = template["author_id"]
		asset["public_url"] = "https://synthetic.invalid/NOT_A_MEDIA_PERMISSION"
		tables["media_assets"] = []any{asset}
		media := makeRow("note_media")
		media["note_id"] = template["id"]
		media["media_id"] = asset["id"]
		media["position"] = 0
		tables["note_media"] = []any{media}
	})
	if result, e := f.Importer.Apply(f.Context, legacyTestRunID, raw, manifest); e != nil || result.Status != "COMMITTED" {
		t.Fatal("actual owned archive import failed", e)
	}
	out := &legacyReaderNativeFixture{legacyNativeFixture: f, store: New(f.Pool, false)}
	out.actor.AccountType = "person"
	if e := f.Pool.QueryRow(f.Context, `SELECT target_id::text FROM legacy_entity_mappings WHERE entity_kind='user' AND source_id='14600000-0000-4000-8000-000000000010'`).Scan(&out.actor.ID); e != nil {
		t.Fatal("owned user mapping", e)
	}
	if e := f.Pool.QueryRow(f.Context, `SELECT target_id::text FROM legacy_entity_mappings WHERE entity_kind='note' AND source_id='14600000-0000-4000-8000-000000000020'`).Scan(&out.id); e != nil {
		t.Fatal("owned note mapping", e)
	}
	// Explicit synthetic fixture activation; this is NOT a real Civu claim or a
	// production activation. The mapped PersonalAgent stays suspended throughout.
	if _, e := f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, out.actor.ID); e != nil {
		t.Fatal(e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	out.digest = digest
	if e = f.Pool.QueryRow(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')RETURNING id::text`, out.actor.ID, digest[:]).Scan(&out.session); e != nil {
		t.Fatal(e)
	}
	return out
}

func TestLegacyArchiveNativeOwnerPaginationAndLiteralPreservation(t *testing.T) {
	f := legacyReaderNative(t)
	detail, e := f.store.ReadHumanLegacyArchive(f.Context, f.digest, f.actor, f.id)
	if e != nil || detail.Title != "" || detail.Body != "仅测试旧正文" || detail.RecordedStartDate != nil || detail.RecordedEndDate != nil || detail.Relationships == nil || len(detail.Relationships.Places) != 2 || detail.Relationships.Places[0].RawName != "原输入地点甲" || detail.Relationships.Media[0].State != "UNAVAILABLE" {
		t.Fatal("original empty/date/multiple-place/media archive changed", e)
	}
	if e = f.store.FinalizeHumanLegacyArchiveRead(f.Context, f.digest, f.actor, detail); e != nil {
		t.Fatal("actual encoded detail tail", e)
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "NOT_A_MEDIA_PERMISSION") || strings.Contains(string(encoded), "password_hash") {
		t.Fatal("raw media/credentials escaped private source")
	}
	cursor := ""
	seen := map[string]bool{}
	deleted := 0
	pages := 0
	for {
		page, e := f.store.ListHumanLegacyArchives(f.Context, f.digest, f.actor, cursor)
		if e != nil || page.Items == nil || len(page.Items) > 20 {
			t.Fatal("actual bounded page", e)
		}
		if e = f.store.FinalizeHumanLegacyArchivePage(f.Context, f.digest, f.actor, page); e != nil {
			t.Fatal("actual encoded page tail", e)
		}
		pages++
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate stable archive ID")
			}
			seen[item.ID] = true
			if item.Deleted {
				deleted++
				d, e := f.store.ReadHumanLegacyArchive(f.Context, f.digest, f.actor, item.ID)
				if e != nil || d.Body != "" || d.Title != "" || d.Relationships != nil {
					t.Fatal("deleted original body or media resurrected", e)
				}
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if pages != 2 || len(seen) != 23 || deleted != 1 {
		t.Fatal("all original pages/tombstone were not preserved")
	}
	// A real native current different PERSON cannot read the old public archive.
	var peer string
	if e = f.Pool.QueryRow(f.Context, `INSERT INTO accounts(id,account_type)VALUES(gen_random_uuid(),'person')RETURNING id::text`).Scan(&peer); e != nil {
		t.Fatal(e)
	}
	_, pd, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, peer, pd[:]); e != nil {
		t.Fatal(e)
	}
	a := identity.Actor{ID: peer, AccountType: "person"}
	foreign, e := f.store.ReadHumanLegacyArchive(f.Context, pd, a, f.id)
	if !errors.Is(e, legacyarchive.ErrNotFound) || foreign.Body != "" {
		t.Fatal("old public flag leaked another owner's body", e)
	}
	page, e := f.store.ListHumanLegacyArchives(f.Context, pd, a, "")
	if e != nil || len(page.Items) != 0 || page.ResponseProof == nil {
		t.Fatal("real empty archive did not have a current proof", e)
	}
	if e = f.store.FinalizeHumanLegacyArchivePage(f.Context, pd, a, page); e != nil {
		t.Fatal("empty tail", e)
	}
}

func legacyReaderOuter(t *testing.T, f *legacyReaderNativeFixture, query string, args ...any) (pgx.Tx, int) {
	t.Helper()
	tx, e := f.Pool.Begin(f.Context)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var pid int
	if e = tx.QueryRow(f.Context, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(f.Context, query, args...); e != nil {
		t.Fatal(e)
	}
	return tx, pid
}

func legacyReaderWait(t *testing.T, f *legacyReaderNativeFixture, pid int) {
	t.Helper()
	hcNativeWaitPID(t, &savedPageNativeFixture{ctx: f.Context, pool: f.Pool}, pid)
}

func TestLegacyArchiveNativeWaitFencesAccountABAAndExpiredOriginalSession(t *testing.T) {
	for _, mode := range []string{"account-aba", "expired-session"} {
		t.Run(mode, func(t *testing.T) {
			f := legacyReaderNative(t)
			query := `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`
			target := f.actor.ID
			if mode == "expired-session" {
				query = `SELECT id FROM legacy_note_archives WHERE id=$1 FOR UPDATE`
				target = f.id
			}
			outer, pid := legacyReaderOuter(t, f, query, target)
			type result struct {
				v legacyarchive.Detail
				e error
			}
			done := make(chan result, 1)
			go func() {
				v, e := f.store.ReadHumanLegacyArchive(f.Context, f.digest, f.actor, f.id)
				done <- result{v, e}
			}()
			legacyReaderWait(t, f, pid)
			if mode == "account-aba" {
				if _, e := outer.Exec(f.Context, `UPDATE accounts SET status='suspended' WHERE id=$1;`, f.actor.ID); e != nil {
					t.Fatal(e)
				}
				if _, e := outer.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1;`, f.actor.ID); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := f.Pool.Exec(f.Context, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',expires_at=clock_timestamp()-interval '1 second',idle_expires_at=clock_timestamp()-interval '2 seconds' WHERE id=$1`, f.session); e != nil {
					t.Fatal(e)
				}
			}
			if e := outer.Commit(f.Context); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				if got.e == nil || got.v.Body != "" {
					t.Fatal("lock-wait original private result survived retired authority", got.e)
				}
			case <-f.Context.Done():
				t.Fatal("actual native wait did not finish")
			}
		})
	}
}

func TestLegacyArchiveNativeEncodedProofRejectsBodyChangeAndRevocation(t *testing.T) {
	f := legacyReaderNative(t)
	detail, e := f.store.ReadHumanLegacyArchive(f.Context, f.digest, f.actor, f.id)
	if e != nil {
		t.Fatal(e)
	}
	changed := detail
	changed.Body = "caller-mutated synthetic body"
	if e = f.store.FinalizeHumanLegacyArchiveRead(f.Context, f.digest, f.actor, changed); e == nil {
		t.Fatal("encoded body changed without original proof")
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.actor.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, f.actor.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.store.FinalizeHumanLegacyArchiveRead(f.Context, f.digest, f.actor, detail); e == nil {
		t.Fatal("encoded old source survived native principal ABA")
	}
	fresh, e := f.store.ReadHumanLegacyArchive(f.Context, f.digest, f.actor, f.id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session); e != nil {
		t.Fatal(e)
	}
	if e = f.store.FinalizeHumanLegacyArchiveRead(f.Context, f.digest, f.actor, fresh); e == nil {
		t.Fatal("encoded private body survived original session revoke")
	}
}
