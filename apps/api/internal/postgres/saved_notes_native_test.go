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

type privateSavedNoteFixture struct {
	ctx                     context.Context
	pool                    *pgxpool.Pool
	store                   *Store
	owner, peer             identity.Actor
	digest, peerDigest      [32]byte
	session, place, savedID string
	row, sourceToken, epoch string
	invalidations           int64
}

func privateSavedNotesNativeFixture(t *testing.T) *privateSavedNoteFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("NOT_RUN: native PostgreSQL not configured")
	}
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &privateSavedNoteFixture{ctx: ctx, pool: pool, store: New(pool, false)}
	account := func() (identity.Actor, [32]byte, string) {
		id, session := f.uuid(t), f.uuid(t)
		f.exec(t, `INSERT INTO accounts(id,account_type) VALUES($1,'person')`, id)
		_, d, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, session, id, d[:])
		return identity.Actor{ID: id, AccountType: "person"}, d, session
	}
	f.owner, f.digest, f.session = account()
	f.peer, f.peerDigest, _ = account()
	f.place = f.uuid(t)
	f.exec(t, `INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'aberdeen-gb','合成私人收藏','park',57.15,-2.1,'point','published','合成测试','disposable://saved-note','合成')`, f.place)
	f.savedID, e = f.store.Save(ctx, f.owner.ID, "place", f.place)
	if e != nil {
		t.Fatal(e)
	}
	f.row, f.sourceToken, f.epoch, f.invalidations = f.snapshot(t)
	return f
}

func (f *privateSavedNoteFixture) uuid(t *testing.T) string {
	t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *privateSavedNoteFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *privateSavedNoteFixture) snapshot(t *testing.T) (string, string, string, int64) {
	t.Helper()
	var row, token, epoch string
	var n int64
	e := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(s)::text,encode(sha256(convert_to(to_jsonb(s)::text,'UTF8')),'hex'),s.xmin::text,(SELECT count(*) FROM agent_memory_source_invalidations WHERE owner_id=s.owner_account_id AND source_id=s.id) FROM saved_items s WHERE s.id=$1`, f.savedID).Scan(&row, &token, &epoch, &n)
	if e != nil {
		t.Fatal(e)
	}
	return row, token, epoch, n
}
func (f *privateSavedNoteFixture) assertSourceIntact(t *testing.T) {
	t.Helper()
	row, token, epoch, n := f.snapshot(t)
	if row != f.row || token != f.sourceToken || epoch != f.epoch || n != f.invalidations {
		t.Fatal("private annotation changed original bookmark source or Memory invalidations")
	}
}

func TestPrivateSavedNotesNativeCASRecoveryAndSourceIsolation(t *testing.T) {
	f := privateSavedNotesNativeFixture(t)
	initial, e := f.store.GetHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID)
	if e != nil || initial.Note != "" || initial.Revision != 0 {
		t.Fatal("initial private note", initial, e)
	}
	op := f.uuid(t)
	in := saved.NoteInput{Note: "  只给本人看的合成备注  ", OperationID: op}
	first, e := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, in)
	if e != nil || first.Revision != 1 || first.Status != "SUCCEEDED" {
		t.Fatal("first CAS", first, e)
	}
	f.assertSourceIntact(t)
	again, e := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, in)
	if e != nil || again != first {
		t.Fatal("same intent duplicated", again, e)
	}
	changed := in
	changed.Note = "different"
	if _, e = f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, changed); !errors.Is(e, saved.ErrNoteChanged) {
		t.Fatal("operation ID rebound", e)
	}
	stale := in
	stale.OperationID = f.uuid(t)
	if _, e = f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, stale); !errors.Is(e, saved.ErrNoteChanged) {
		t.Fatal("stale CAS accepted", e)
	}
	if _, e = f.store.GetHumanSavedNote(f.ctx, f.peerDigest, f.peer, f.savedID); !errors.Is(e, saved.ErrNotFound) {
		t.Fatal("peer read private annotation", e)
	}
	if _, e = f.store.GetHumanSavedNoteOperation(f.ctx, f.peerDigest, f.peer, f.savedID, op); !errors.Is(e, saved.ErrNotFound) {
		t.Fatal("peer recovered private receipt", e)
	}
	if _, e = f.store.UpdateHumanSavedNote(f.ctx, f.peerDigest, f.peer, f.savedID, in); !errors.Is(e, saved.ErrNotFound) {
		t.Fatal("peer modified private annotation", e)
	}
	clear, e := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, saved.NoteInput{ExpectedRevision: 1, OperationID: f.uuid(t)})
	if e != nil || clear.Revision != 2 {
		t.Fatal("clear erased version", clear, e)
	}
	read, e := f.store.GetHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID)
	if e != nil || read.Note != "" || read.Revision != 2 {
		t.Fatal("clear ABA", read, e)
	}
	recovered, e := f.store.GetHumanSavedNoteOperation(f.ctx, f.digest, f.owner, f.savedID, op)
	if e != nil || recovered != first {
		t.Fatal("later edit erased original receipt", recovered, e)
	}
	again, e = f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, in)
	if e != nil || again != first {
		t.Fatal("old receipt replay rewrote current annotation", again, e)
	}
	read, e = f.store.GetHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID)
	if e != nil || read.Revision != 2 || read.Note != "" {
		t.Fatal("replay rewrote note", read, e)
	}
	if _, e = f.store.GetHumanSavedNoteOperation(f.ctx, f.digest, f.owner, f.savedID, f.uuid(t)); !errors.Is(e, saved.ErrNotFound) {
		t.Fatal("missing operation invented receipt", e)
	}
	f.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
	hidden, e := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, saved.NoteInput{Note: "已下架地点的本人备注", ExpectedRevision: 2, OperationID: f.uuid(t)})
	if e != nil || hidden.Revision != 3 {
		t.Fatal("private note incorrectly required public target", hidden, e)
	}
	legacy, e := f.store.ListSaved(f.ctx, f.owner.ID)
	if e != nil || len(legacy) != 1 || legacy[0].Available || legacy[0].Title != "" || legacy[0].Note != "" || legacy[0].NoteRevision != 0 {
		t.Fatal("legacy source/public projection acquired private note", e)
	}
	// Two real writers race on one concrete version; only one can commit.
	results := make(chan struct {
		v saved.NoteOperation
		e error
	}, 2)
	for _, text := range []string{"并发本人备注一", "并发本人备注二"} {
		input := saved.NoteInput{Note: text, ExpectedRevision: 3, OperationID: f.uuid(t)}
		go func() {
			v, e := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, input)
			results <- struct {
				v saved.NoteOperation
				e error
			}{v, e}
		}()
	}
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.e == nil && r.v.Revision == 4 {
			success++
		} else if errors.Is(r.e, saved.ErrNoteChanged) {
			conflict++
		} else {
			t.Fatal("race returned unknown result", r.e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("race committed more than exact one CAS", success, conflict)
	}
	f.assertSourceIntact(t)
	var notes, receipts int64
	if e = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM private_saved_notes WHERE saved_item_id=$1),(SELECT count(*) FROM private_saved_note_operations WHERE saved_item_id=$1)`, f.savedID).Scan(&notes, &receipts); e != nil || notes != 1 || receipts != 4 {
		t.Fatal("note/operation cardinality", notes, receipts, e)
	}
	if e = f.store.RemoveSaved(f.ctx, f.owner.ID, f.savedID); e != nil {
		t.Fatal(e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM private_saved_notes WHERE saved_item_id=$1),(SELECT count(*) FROM private_saved_note_operations WHERE saved_item_id=$1)`, f.savedID).Scan(&notes, &receipts); e != nil || notes != 0 || receipts != 0 {
		t.Fatal("deleted bookmark retained private annotations or receipts", notes, receipts, e)
	}
	if _, e = f.store.GetHumanSavedNoteOperation(f.ctx, f.digest, f.owner, f.savedID, op); !errors.Is(e, saved.ErrNotFound) {
		t.Fatal("deleted bookmark resurrected receipt", e)
	}
}

func TestPrivateSavedNotesNativeResourceWaitRevocationRollsBack(t *testing.T) {
	f := privateSavedNotesNativeFixture(t)
	held, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	var pid int
	if e = held.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = held.Exec(f.ctx, `SELECT id FROM saved_items WHERE id=$1 FOR UPDATE`, f.savedID); e != nil {
		t.Fatal(e)
	}
	op := f.uuid(t)
	done := make(chan error, 1)
	go func() {
		_, e := f.store.UpdateHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID, saved.NoteInput{Note: "不得提交的迟到备注", OperationID: op})
		done <- e
	}()
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE 'SELECT id FROM saved_items WHERE id=%' AND query LIKE '%FOR UPDATE%')`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("write did not wait on actual bookmark", e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("native bookmark wait not observed")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if e = held.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("committed revoke ignored after resource wait", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waited write did not finish")
	}
	var notes, receipts int
	if e = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM private_saved_notes WHERE saved_item_id=$1),(SELECT count(*) FROM private_saved_note_operations WHERE saved_item_id=$1 AND operation_id=$2)`, f.savedID, op).Scan(&notes, &receipts); e != nil || notes != 0 || receipts != 0 {
		t.Fatal("revoked intent retained note or success receipt", notes, receipts, e)
	}
	f.assertSourceIntact(t)
	if _, e = f.store.GetHumanSavedNote(f.ctx, f.digest, f.owner, f.savedID); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("revoked private read accepted", e)
	}
}
