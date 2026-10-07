package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	si "github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func creationNative(t *testing.T) (*placeMemoryFixture, si.CreationAccess) {
	t.Helper()
	ownedMigrationDatabase(t)
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a, e := b.store.Authenticate(b.ctx, f.private.owner.SessionDigest)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM social_intent_creation_receipts WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, `DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error(e)
			}
		}
		for _, q := range []string{`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`} {
			if _, e := b.pool.Exec(ctx, q, f.city); e != nil {
				t.Error(e)
			}
		}
	})
	return f, si.CreationAccess{Actor: a, SessionDigest: f.private.owner.SessionDigest}
}
func creationNativeInput(t *testing.T, f *placeMemoryFixture) si.DraftInput {
	t.Helper()
	return si.DraftInput{OperationID: agentMemoryID(t, f.private), Type: "FIND_ACTIVITY", Title: "本地合成同一次保存", Audience: "PRIVATE", Modality: "ONLINE", Constraints: json.RawMessage(`{"onlinePlatform":"Zoom"}`), ExpiresAt: time.Now().UTC().Add(time.Hour)}
}
func creationNativeCounts(t *testing.T, f *placeMemoryFixture) (int, int, int) {
	t.Helper()
	var a, b, c int
	x := f.private.base
	if e := x.pool.QueryRow(x.ctx, `SELECT (SELECT count(*) FROM social_intents WHERE creator_account_id=$1),(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='social_intent' AND action='create'),(SELECT count(*) FROM social_intent_creation_receipts WHERE owner_account_id=$1)`, x.person.ID).Scan(&a, &b, &c); e != nil {
		t.Fatal(e)
	}
	return a, b, c
}
func TestSocialIntentCreationNativeConcurrencyReplayAndContentBoundary(t *testing.T) {
	f, a := creationNative(t)
	b := f.private.base
	in := creationNativeInput(t, f)
	var wg sync.WaitGroup
	results := make(chan si.CreationReceipt, 24)
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", in)
			if e != nil {
				errs <- e
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	var id string
	for r := range results {
		if id == "" {
			id = r.IntentID
		}
		if r.IntentID != id || r.Status != "COMMITTED" {
			t.Fatal("native retry changed authority", r)
		}
	}
	if id == "" {
		t.Fatal("no native receipt")
	}
	i, u, r := creationNativeCounts(t, f)
	if i != 1 || u != 1 || r != 1 {
		t.Fatal("duplicate native effect", i, u, r)
	}
	changed := in
	changed.Title = "不同具体内容"
	if _, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", changed); !errors.Is(e, si.ErrCreationConflict) {
		t.Fatal("same key changed body", e)
	}
	changed = in
	changed.OperationID = agentMemoryID(t, f.private)
	other, created, e := b.store.CreatePrivateDraft(b.ctx, a, "", changed)
	if e != nil || !created || other.IntentID == id {
		t.Fatal("separate explicit operation", e)
	}
	changed = in
	changed.ExpiresAt = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", changed); !errors.Is(e, si.ErrCreationConflict) {
		t.Fatal("retry hid changed expiry", e)
	}
	// The original committed receipt is stable after the actual object changes.
	b.exec(`UPDATE social_intents SET title='实际后续编辑标题',status='CANCELLED',updated_at=clock_timestamp() WHERE id=$1`, id)
	got, e := b.store.ReadCreation(b.ctx, a, in.OperationID)
	if e != nil || got.IntentID != id || got.RequestDigest == "" || got.Intent.Title != "实际后续编辑标题" || got.Intent.Status != "CANCELLED" {
		t.Fatal("original receipt/current object", got, e)
	}
	again, created, e := b.store.CreatePrivateDraft(b.ctx, a, "", in)
	if e != nil || created || again.IntentID != id || again.RecordedAt != got.RecordedAt {
		t.Fatal("committed replay after edit", e)
	}
}
func TestSocialIntentCreationNativeSourceOriginalLegacyAndNoEffect(t *testing.T) {
	f, a := creationNative(t)
	b := f.private.base
	in := creationNativeInput(t, f)
	b.exec(`INSERT INTO city_contexts(city_id) VALUES($1) ON CONFLICT(city_id) DO NOTHING`, f.city)
	task, e := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: f.city, Query: "合成羽毛球", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskCompleted, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if e != nil {
		t.Fatal(e)
	}
	old := in
	old.OperationID = ""
	original, e := b.store.CreateSocialIntentDraftFromTask(b.ctx, a.Actor.ID, task.ID, old)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.CreateSocialIntentDraftFromTask(b.ctx, a.Actor.ID, task.ID, old); !errors.Is(e, si.ErrConflict) {
		t.Fatal("legacy source duplicate compatibility", e)
	}
	in.Title = "本次未保存的修改"
	r, created, e := b.store.CreatePrivateDraft(b.ctx, a, task.ID, in)
	if e != nil || created || r.Status != "NO_EFFECT" || r.Reason != "SOURCE_ALREADY_EXISTS" || r.PriorIntentID != original.ID || r.Intent.Title == in.Title {
		t.Fatal("source conflict falsely saved", r, e)
	}
	source, e := b.store.ReadTaskDraft(b.ctx, a, task.ID)
	if e != nil || source.IntentID != original.ID || source.SourceTaskID != task.ID || source.Intent.CreatorID != a.Actor.ID {
		t.Fatal("actual source/original ID", e)
	}
	replay, _, e := b.store.CreatePrivateDraft(b.ctx, a, task.ID, in)
	if e != nil || replay.PriorIntentID != original.ID || replay.RecordedAt != r.RecordedAt {
		t.Fatal("source immutable no-effect", e)
	}
	if _, _, e = b.store.CreatePrivateDraft(b.ctx, a, "", in); !errors.Is(e, si.ErrCreationConflict) {
		t.Fatal("same key changed source", e)
	}
	i, u, rr := creationNativeCounts(t, f)
	if i != 1 || u != 1 || rr != 1 {
		t.Fatal("source duplicated effect", i, u, rr)
	}
	missing := creationNativeInput(t, f)
	r, created, e = b.store.CreatePrivateDraft(b.ctx, a, agentMemoryID(t, f.private), missing)
	if e != nil || created || r.Status != "NO_EFFECT" || r.Reason != "SOURCE_UNAVAILABLE" || r.Intent != nil {
		t.Fatal("missing source got effect", r, e)
	}
}
func TestSocialIntentCreationNativeCurrentIdentityRestartAndExpired(t *testing.T) {
	f, a := creationNative(t)
	b := f.private.base
	in := creationNativeInput(t, f)
	first, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", in)
	if e != nil {
		t.Fatal(e)
	}
	// A new actual connection pool and Store cannot depend on process memory.
	pool, e := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	other := New(pool, false)
	receipt, e := other.ReadCreation(b.ctx, a, in.OperationID)
	if e != nil || receipt.IntentID != first.IntentID {
		t.Fatal("new native pool receipt", e)
	}
	receipt, created, e := other.CreatePrivateDraft(b.ctx, a, "", in)
	if e != nil || created || receipt.IntentID != first.IntentID {
		t.Fatal("new native pool retry", e)
	}
	outsider := a
	outsider.Actor.ID = b.other.ID
	if _, e = other.ReadCreation(b.ctx, outsider, in.OperationID); !errors.Is(e, si.ErrCreationDenied) {
		t.Fatal("forged identity/session", e)
	}
	org := a
	org.Actor.AccountType = "organization"
	if _, _, e = other.CreatePrivateDraft(b.ctx, org, "", creationNativeInput(t, f)); !errors.Is(e, si.ErrCreationDenied) {
		t.Fatal("organization gained create", e)
	}
	expired := creationNativeInput(t, f)
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	no, created, e := other.CreatePrivateDraft(b.ctx, a, "", expired)
	if e != nil || created || no.Status != "NO_EFFECT" || no.Reason != "EXPIRED" {
		t.Fatal("native expired no-effect", e)
	}
	expired.ExpiresAt = time.Now().Add(time.Hour)
	if _, _, e = other.CreatePrivateDraft(b.ctx, a, "", expired); !errors.Is(e, si.ErrCreationConflict) {
		t.Fatal("closed operation changed later", e)
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
	if _, e = other.ReadCreation(b.ctx, a, in.OperationID); !errors.Is(e, si.ErrCreationDenied) {
		t.Fatal("revoked read", e)
	}
	if _, _, e = other.CreatePrivateDraft(b.ctx, a, "", creationNativeInput(t, f)); !errors.Is(e, si.ErrCreationDenied) {
		t.Fatal("revoked write", e)
	}
}
func TestSocialIntentCreationNativeMigrationNoRewriteAndUsedDown(t *testing.T) {
	f, a := creationNative(t)
	b := f.private.base
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "096_social_intent_creation_receipts.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "096_social_intent_creation_receipts.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
		t.Fatal("unused down", e)
	}
	if _, e = b.pool.Exec(b.ctx, string(up)); e != nil {
		t.Fatal("reapply", e)
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != before {
		t.Fatal("migration roundtrip changed old domain rows/xmin")
	}
	r, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", creationNativeInput(t, f))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = conn.Exec(b.ctx, string(down))
	if e == nil {
		t.Fatal("used down erased durable receipt")
	}
	if _, e = conn.Exec(b.ctx, `ROLLBACK`); e != nil {
		t.Fatal(e)
	}
	conn.Release()
	if _, e = b.pool.Exec(b.ctx, `UPDATE social_intent_creation_receipts SET status='NO_EFFECT' WHERE owner_account_id=$1`, a.Actor.ID); e == nil {
		t.Fatal("immutable receipt modified")
	}
	if got, e := b.store.ReadCreation(b.ctx, a, r.OperationID); e != nil || got.IntentID != r.IntentID {
		t.Fatal("down failure corrupted original receipt", e)
	}
}

func TestSocialIntentCreationNativeFailureAtomicAndSessionWait(t *testing.T) {
	f, a := creationNative(t)
	b := f.private.base
	b.exec(`CREATE FUNCTION creation_fail_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'OWNED_SYNTHETIC_RECEIPT_FAILURE'; END $$;CREATE TRIGGER creation_fail_receipt BEFORE INSERT ON social_intent_creation_receipts FOR EACH ROW EXECUTE FUNCTION creation_fail_receipt()`)
	if _, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", creationNativeInput(t, f)); !errors.Is(e, si.ErrCreationUnavailable) {
		t.Fatal("injected receipt write failure", e)
	}
	if i, u, r := creationNativeCounts(t, f); i != 0 || u != 0 || r != 0 {
		t.Fatal("receipt failure left partial native effect", i, u, r)
	}
	b.exec(`DROP TRIGGER creation_fail_receipt ON social_intent_creation_receipts;DROP FUNCTION creation_fail_receipt()`)
	in := creationNativeInput(t, f)
	if _, e := b.store.CreateSocialIntentDraft(b.ctx, a.Actor.ID, in); !errors.Is(e, si.ErrCreationInvalid) {
		t.Fatal("caller key bypassed session-aware gateway", e)
	}
	hold, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer hold.Rollback(context.Background())
	if _, e = hold.Exec(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.SessionDigest[:]); e != nil {
		t.Fatal(e)
	}
	completed := make(chan error, 1)
	go func() { _, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", in); completed <- e }()
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query ILIKE '%sessions%' AND query ILIKE '%FOR%SHARE%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("native create did not reach real session row-lock wait")
	}
	if _, e = hold.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:]); e != nil {
		t.Fatal(e)
	}
	if e = hold.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-completed:
		if !errors.Is(e, si.ErrCreationDenied) {
			t.Fatal("revoked during real write wait", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native write wait never resolved")
	}
	if i, u, r := creationNativeCounts(t, f); i != 0 || u != 0 || r != 0 {
		t.Fatal("late revoked commit", i, u, r)
	}
}
func TestSocialIntentCreationNativeSourceCurrentAfterLastReceiptWait(t *testing.T) {
	for _, mode := range []string{"changed", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f, a := creationNative(t)
			b := f.private.base
			in := creationNativeInput(t, f)
			in.Modality = "IN_PERSON"
			in.Constraints = json.RawMessage(`{"placeId":"` + f.place + `"}`)
			if mode == "expired" {
				b.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '1500 milliseconds' WHERE id=$1`, f.place)
			}
			const key = int64(967406096)
			hold, e := b.pool.Acquire(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Release()
			if _, e = hold.Exec(b.ctx, `SELECT pg_advisory_lock($1)`, key); e != nil {
				t.Fatal(e)
			}
			defer hold.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
			b.exec(`CREATE FUNCTION creation_block_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='COMMITTED' THEN PERFORM pg_advisory_xact_lock(967406096); END IF;RETURN NEW;END $$;CREATE TRIGGER creation_block_receipt BEFORE INSERT ON social_intent_creation_receipts FOR EACH ROW EXECUTE FUNCTION creation_block_receipt()`)
			done := make(chan struct {
				r si.CreationReceipt
				e error
			}, 1)
			go func() {
				r, _, e := b.store.CreatePrivateDraft(b.ctx, a, "", in)
				done <- struct {
					r si.CreationReceipt
					e error
				}{r, e}
			}()
			waiting := false
			until := time.Now().Add(5 * time.Second)
			for time.Now().Before(until) {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query ILIKE '%INSERT INTO social_intent_creation_receipts%')`).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("actual last durable receipt insert wait not reached")
			}
			if mode == "changed" {
				b.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, f.place)
			} else {
				for {
					var expired bool
					if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM places WHERE id=$1`, f.place).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			if _, e = hold.Exec(b.ctx, `SELECT pg_advisory_unlock($1)`, key); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				if got.e != nil || got.r.Status != "NO_EFFECT" || got.r.Reason != "SOURCE_CHANGED" || got.r.Intent != nil {
					t.Fatal("native source/deadline after actual receipt wait", got.r, got.e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("receipt wait did not resolve")
			}
			if i, u, r := creationNativeCounts(t, f); i != 0 || u != 0 || r != 1 {
				t.Fatal("source change left partial effect", i, u, r)
			}
			b.exec(`DROP TRIGGER creation_block_receipt ON social_intent_creation_receipts;DROP FUNCTION creation_block_receipt()`)
			t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY last insert wait observed then", strings.ToUpper(mode), "source blocked effect")
		})
	}
}
