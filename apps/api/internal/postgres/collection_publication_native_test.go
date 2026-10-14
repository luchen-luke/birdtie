package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type collectionPublicationNativeFixture struct {
	t                    *testing.T
	ctx                  context.Context
	pool                 *pgxpool.Pool
	s                    *Store
	owner, peer          identity.Actor
	digest, peerDigest   [32]byte
	session, peerSession string
	places               []string
}

// Narrow diagnosis only: execute the same publication SQL stages in this
// fixture's random owned child, then roll back. It never touches parent data.
func TestCollectionPublicationNativeSQLDiagnosis(t *testing.T) {
	f := collectionPublicationFixture(t)
	v, in := f.draft("journey")
	tx, e := f.s.beginHumanMomentTx(f.ctx, f.digest, f.owner)
	if e != nil {
		t.Fatal("begin", e)
	}
	defer tx.Rollback(context.Background())
	raw := func(stage, q string, args ...any) {
		t.Helper()
		_, e := tx.Exec(f.ctx, q, args...)
		if e != nil {
			var native *pgconn.PgError
			if errors.As(e, &native) {
				t.Fatalf("owned SQL stage=%s SQLSTATE=%s message=%s", stage, native.Code, native.Message)
			}
			t.Fatalf("owned SQL stage=%s error=%v", stage, e)
		}
		t.Log("owned SQL stage passed:", stage)
	}
	if _, e = tx.Exec(f.ctx, `SELECT id FROM journeys WHERE id=$1 AND owner_account_id=$2 FOR UPDATE`, v.ID, f.owner.ID); e != nil {
		t.Fatal("target", e)
	}
	if _, e = collectionReadTx(f.ctx, tx, f.owner.ID, "journey", v.ID, false); e != nil {
		t.Fatal("private-read", e)
	}
	if e = collectionSourcesTx(f.ctx, tx, v.CollectionInput); e != nil {
		t.Fatal("source-lock", e)
	}
	ids := []string{f.places[0], f.places[1], f.places[2]}
	rows, e := tx.Query(f.ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM cities c CROSS JOIN cl WHERE c.id=$1 AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>cl.at))
 AND (SELECT count(*) FROM places p CROSS JOIN cl WHERE p.id=ANY($2::uuid[]) AND p.city_id=$1 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>cl.at) AND `+currentRetainedPlaceVisibilitySQL("p", "cl.at")+`)=$3`, v.CityID, ids, len(ids))
	if e != nil {
		t.Fatal("native source-current SQL", e)
	}
	rows.Close()
	for i, entry := range in.Entries {
		raw("entry-precision", `UPDATE journey_stops SET location_visibility=$3 WHERE journey_id=$1 AND sequence_no=$2 AND place_id=$4`, v.ID, i+1, entry.LocationVisibility, entry.PlaceID)
	}
	raw("original-entity-publish", `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE journeys SET visibility='public',status='published',revision=revision+1,owner_confirmed_at=cl.at,published_at=cl.at,updated_at=cl.at FROM cl WHERE id=$1`, v.ID)
	digest := content.CollectionPublicationDigest("journey", v.ID, "PUBLISH", in.Revision, in.Entries)
	raw("immutable-receipt-insert", `INSERT INTO personal_collection_publication_operations(owner_account_id,operation_id,kind,target_id,journey_id,theme_map_id,action,expected_revision,result_revision,input_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8::bigint,$8::bigint+1::bigint,$9)`, f.owner.ID, in.OperationID, "journey", v.ID, v.ID, nil, "PUBLISH", in.Revision, digest[:])
	if e = collectionAuditTx(f.ctx, tx, f.owner.ID, "journey", v.ID, "publish"); e != nil {
		t.Fatal("audit", e)
	}
	if _, e = collectionReadTx(f.ctx, tx, f.owner.ID, "journey", v.ID, false); e != nil {
		t.Fatal("published-owner-read", e)
	}
	// Parse the new public native statement too, without exporting source data.
	_, e = tx.Prepare(f.ctx, "owned_collection_public_projection", collectionPublicReadSQL("journey"))
	if e != nil {
		t.Fatal("public-read-prepare", e)
	}
}

func collectionPublicationFixture(t *testing.T) *collectionPublicationNativeFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &collectionPublicationNativeFixture{t: t, ctx: ctx, pool: pool, s: New(pool, false)}
	account := func() (identity.Actor, [32]byte, string) {
		id := f.uuid()
		f.exec(`INSERT INTO accounts(id,account_type)VALUES($1,'person')`, id)
		_, d, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		sid := f.uuid()
		f.exec(`INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',now()+interval '1 hour',now()+interval '30 minutes')`, sid, id, d[:])
		return identity.Actor{ID: id, AccountType: "person"}, d, sid
	}
	f.owner, f.digest, f.session = account()
	f.peer, f.peerDigest, f.peerSession = account()
	for i := 0; i < 3; i++ {
		id := f.uuid()
		f.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'aberdeen-gb',$2,'park',57.15,-2.1,'point','published','合成测试','disposable://collection-publication','合成')`, id, "公开合成站"+string(rune('一'+i)))
		f.places = append(f.places, id)
	}
	return f
}
func (f *collectionPublicationNativeFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f *collectionPublicationNativeFixture) uuid() string {
	f.t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		f.t.Fatal(e)
	}
	return id
}
func (f *collectionPublicationNativeFixture) draft(kind string) (content.PersonalCollection, content.CollectionPublicationInput) {
	f.t.Helper()
	entries := []content.CollectionEntry{}
	choices := []content.CollectionPublicationEntry{}
	for i, precision := range []string{"hidden", "city", "place"} {
		entries = append(entries, content.CollectionEntry{PlaceID: f.places[i], Note: "本人批准的文字", LocationVisibility: "hidden"})
		choices = append(choices, content.CollectionPublicationEntry{PlaceID: f.places[i], LocationVisibility: precision})
	}
	v, e := f.s.CreateHumanCollection(f.ctx, f.digest, f.owner, content.CollectionCreate{OperationID: f.uuid(), CollectionInput: content.CollectionInput{Kind: kind, CityID: "aberdeen-gb", Title: "原集合明示发布", Summary: "摘要也将公开", Entries: entries}})
	if e != nil {
		f.t.Fatal(e)
	}
	return v, content.CollectionPublicationInput{OperationID: f.uuid(), Revision: v.Revision, Entries: choices}
}
func (f *collectionPublicationNativeFixture) publish(kind string) (content.PersonalCollection, content.CollectionPublicationInput) {
	f.t.Helper()
	draft, in := f.draft(kind)
	v, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, in)
	if e != nil {
		f.t.Fatal(e)
	}
	return v, in
}
func TestCollectionPublicationNativeLifecyclePermissionsAndRecovery(t *testing.T) {
	f := collectionPublicationFixture(t)
	for _, kind := range []string{"journey", "theme_map"} {
		t.Run(kind, func(t *testing.T) {
			draft, in := f.draft(kind)
			if _, e := f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{}, kind, draft.ID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("private draft was public", e)
			}
			if _, e := f.s.PublishHumanCollection(f.ctx, f.peerDigest, f.peer, kind, draft.ID, in); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("peer approved someone else's collection", e)
			}
			swapped := in
			swapped.OperationID = f.uuid()
			swapped.Entries = append([]content.CollectionPublicationEntry(nil), in.Entries...)
			swapped.Entries[0], swapped.Entries[1] = swapped.Entries[1], swapped.Entries[0]
			if _, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, swapped); !errors.Is(e, content.ErrConflict) {
				t.Fatal("client changed saved order", e)
			}
			published, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, in)
			if e != nil || published.ID != draft.ID || published.Revision != 2 || published.Status != "published" || published.Visibility != "public" || published.PublicationOperation == nil || published.PublicationOperation.ResultRevision != 2 {
				t.Fatal(published, e)
			}
			again, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, in)
			if e != nil || again.ID != draft.ID || again.Revision != 2 {
				t.Fatal("duplicate created/published twice", again, e)
			}
			changed := in
			changed.Entries = append([]content.CollectionPublicationEntry(nil), in.Entries...)
			changed.Entries[0].LocationVisibility = "place"
			if _, e = f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, changed); !errors.Is(e, content.ErrConflict) {
				t.Fatal("changed approval reused same operation", e)
			}
			changedRevision := in
			changedRevision.Revision = 2
			if _, e = f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, changedRevision); !errors.Is(e, content.ErrConflict) {
				t.Fatal("operation changed expected revision", e)
			}
			if _, e = f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, f.uuid(), in); !errors.Is(e, content.ErrConflict) {
				t.Fatal("operation changed target", e)
			}
			otherKind := "theme_map"
			if kind == "theme_map" {
				otherKind = "journey"
			}
			if _, e = f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, otherKind, draft.ID, in); !errors.Is(e, content.ErrConflict) {
				t.Fatal("operation changed original collection kind", e)
			}
			if e = f.s.WithdrawHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, 2); !errors.Is(e, content.ErrConflict) {
				t.Fatal("private deletion withdrew published entity without explicit unpublish", e)
			}
			if _, e = f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, content.CollectionUnpublishInput{OperationID: in.OperationID, Revision: 1}); !errors.Is(e, content.ErrConflict) {
				t.Fatal("operation changed action", e)
			}
			page, e := f.s.ListHumanCollections(f.ctx, f.digest, f.owner, kind, "")
			if e != nil || len(page.Items) != 1 || page.Items[0].Status != "published" {
				t.Fatal("owner list lost published original", page, e)
			}
			for _, access := range []content.PublicMomentAccess{{}, {Actor: f.peer, SessionDigest: f.peerDigest}} {
				pub, e := f.s.GetPublicCollection(f.ctx, access, kind, draft.ID)
				if e != nil || pub.ID != draft.ID || len(pub.Entries) != 3 || pub.Revision != 2 {
					t.Fatal(pub, e)
				}
				if e = f.s.RevalidatePublicCollection(f.ctx, access, pub); e != nil {
					t.Fatal("stable read/normal fixed TTL was rejected", e)
				}
				raw, e := json.Marshal(pub)
				if e != nil {
					t.Fatal(e)
				}
				var dto map[string]any
				if e = json.Unmarshal(raw, &dto); e != nil {
					t.Fatal(e)
				}
				if _, ok := dto["ownerAccountId"]; ok {
					t.Fatal("private owner profile selector")
				}
				if strings.Contains(string(raw), pub.SourceBinding) {
					t.Fatal("private read fence")
				}
				entries := dto["entries"].([]any)
				for _, entry := range entries[:2] {
					m := entry.(map[string]any)
					for _, key := range []string{"placeId", "placeName", "latitude", "longitude"} {
						if _, ok := m[key]; ok {
							t.Fatal("hidden/city leaked", key)
						}
					}
				}
				if pub.Entries[2].PlaceID == nil || *pub.Entries[2].PlaceID != f.places[2] || pub.Entries[2].PlaceName == "" || pub.Entries[2].Latitude == nil {
					t.Fatal("explicit exact Place projection missing")
				}
				var bounded bool
				if e = f.pool.QueryRow(f.ctx, `SELECT $1::timestamptz>clock_timestamp() AND $1::timestamptz<=clock_timestamp()+interval '60 seconds'`, pub.SourceExpiresAt).Scan(&bounded); e != nil || !bounded {
					t.Fatal("unbounded native public lease", e)
				}
				tampered := pub
				tampered.Entries = append([]content.PublicCollectionEntry(nil), pub.Entries...)
				tampered.Entries[0].Note = "rewritten"
				if e = f.s.RevalidatePublicCollection(f.ctx, access, tampered); e == nil {
					t.Fatal("proof authorized rewritten payload")
				}
			}
			// A source disappearing must block public delivery but never owner retraction.
			f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.places[0])
			if _, e = f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{}, kind, draft.ID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("withdrawn referenced source public", e)
			}
			unpublished, e := f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, content.CollectionUnpublishInput{OperationID: f.uuid(), Revision: 2})
			if e != nil || unpublished.ID != draft.ID || unpublished.Revision != 3 || unpublished.Status != "draft" || unpublished.Visibility != "private" || len(unpublished.Entries) != 3 {
				t.Fatal("unpublish erased content or required source", unpublished, e)
			}
			if _, e = f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{}, kind, draft.ID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("unpublish still public", e)
			}
			f.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.places[0])
			input := draft.CollectionInput
			for i := range input.Entries {
				input.Entries[i].PlaceName = ""
				input.Entries[i].Available = false
				input.Entries[i].Latitude = nil
				input.Entries[i].Longitude = nil
				input.Entries[i].SourceExpiresAt = nil
			}
			input.Title = "本人修改后的私有文本"
			edited, e := f.s.UpdateHumanCollection(f.ctx, f.digest, f.owner, draft.ID, 3, input)
			if e != nil || edited.Revision != 4 {
				t.Fatal("private edit after retraction", e)
			}
			recovered, e := f.s.GetHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID)
			if e != nil || recovered.Revision != 4 || recovered.Status != "draft" || recovered.PublicationOperation.ResultRevision != 2 || recovered.PublicationOperation.ExpectedRevision != 1 {
				t.Fatal("receipt mistook current state for old commit", recovered, e)
			}
			replay, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, in)
			if e != nil || replay.Revision != 4 || replay.Status != "draft" {
				t.Fatal("old UNKNOWN retry republished", replay, e)
			}
			if _, e = f.s.GetHumanCollectionPublicationOperation(f.ctx, f.peerDigest, f.peer, in.OperationID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("peer recovered receipt", e)
			}
			if _, e = f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{}, kind, draft.ID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("old operation revived public content", e)
			}
			for _, q := range []string{`UPDATE personal_collection_publication_operations SET result_revision=9 WHERE owner_account_id=$1 AND operation_id=$2`, `DELETE FROM personal_collection_publication_operations WHERE owner_account_id=$1 AND operation_id=$2`} {
				_, e = f.pool.Exec(f.ctx, q, f.owner.ID, in.OperationID)
				var native *pgconn.PgError
				if !errors.As(e, &native) || native.Code != "23514" {
					t.Fatal("causal ledger mutated", e)
				}
			}
			_, e = f.pool.Exec(f.ctx, `INSERT INTO personal_collection_publication_operations(owner_account_id,operation_id,kind,target_id,action,expected_revision,result_revision,input_sha256) VALUES($1,$2,$3,$4,'PUBLISH',1,2,decode(repeat('00',32),'hex'))`, f.owner.ID, f.uuid(), kind, draft.ID)
			var invalidSelector *pgconn.PgError
			if !errors.As(e, &invalidSelector) || invalidSelector.Code != "23514" {
				t.Fatal("NULL typed FK selector bypassed original target check", e)
			}
		})
	}
	// Actual rollback refuses history, preserves the owned DB, and ends aborted tx.
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", "128_collection_publication.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	_, e = conn.Exec(f.ctx, string(raw))
	var native *pgconn.PgError
	if !errors.As(e, &native) || native.Code != "23514" {
		t.Fatal("rollback discarded UNKNOWN history", e)
	}
	if _, e = conn.Exec(f.ctx, "ROLLBACK"); e != nil {
		t.Fatal(e)
	}
}
func TestCollectionPublicationNativeCurrentSourceSessionAndSafetyFences(t *testing.T) {
	f := collectionPublicationFixture(t)
	v, _ := f.publish("theme_map")
	access := content.PublicMomentAccess{Actor: f.peer, SessionDigest: f.peerDigest}
	read := func() content.PublicCollection {
		t.Helper()
		out, e := f.s.GetPublicCollection(f.ctx, access, "theme_map", v.ID)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	assertRetired := func(old content.PublicCollection) {
		t.Helper()
		if e := f.s.RevalidatePublicCollection(f.ctx, access, old); e == nil {
			t.Fatal("retired projection/fence accepted")
		}
	}
	old := read()
	if e := f.s.BlockAccount(f.ctx, f.peer.ID, f.owner.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.GetPublicCollection(f.ctx, access, "theme_map", v.ID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("blocked viewer retained read", e)
	}
	if e := f.s.UnblockAccount(f.ctx, f.peer.ID, f.owner.ID); e != nil {
		t.Fatal(e)
	}
	assertRetired(old)
	old = read()
	past := time.Now().Add(-time.Second)
	if _, e := f.s.readPublicCollection(f.ctx, access, "theme_map", v.ID, &past); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("final native read extended an expired original deadline", e)
	}
	f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.places[0])
	f.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.places[0])
	assertRetired(old)
	old = read()
	f.exec(`UPDATE sessions SET authentication_method='test_changed' WHERE id=$1`, f.peerSession)
	f.exec(`UPDATE sessions SET authentication_method='test' WHERE id=$1`, f.peerSession)
	assertRetired(old)
	old = read()
	f.exec(`UPDATE sessions SET idle_expires_at=now()+interval '31 minutes' WHERE id=$1`, f.peerSession)
	if e := f.s.RevalidatePublicCollection(f.ctx, access, old); e != nil {
		t.Fatal("normal idle renewal invalidated source proof", e)
	}
	old = read()
	f.exec(`UPDATE cities SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id='aberdeen-gb'`)
	assertRetired(old)
	current := read()
	var bounded bool
	if e := f.pool.QueryRow(f.ctx, `SELECT $1::timestamptz<=expires_at FROM cities WHERE id='aberdeen-gb'`, current.SourceExpiresAt).Scan(&bounded); e != nil || !bounded {
		t.Fatal("city deadline not bounded", e)
	}
	f.exec(`UPDATE cities SET expires_at=NULL WHERE id='aberdeen-gb'`)
	old = read()
	f.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '20 seconds' WHERE id=$1`, f.places[0])
	assertRetired(old)
	f.exec(`UPDATE places SET expires_at=NULL WHERE id=$1`, f.places[0])
	old = read()
	f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.owner.ID)
	assertRetired(old)
	f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.owner.ID)
	old = read()
	f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.peerSession)
	if e := f.s.RevalidatePublicCollection(f.ctx, access, old); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("invalid session anonymously downgraded", e)
	}
	if _, e := f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{Actor: identity.Actor{ID: f.owner.ID, AccountType: "organization"}, SessionDigest: f.digest}, "theme_map", v.ID); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("organization public access", e)
	}
}
func TestCollectionPublicationNativePublishWaitRechecksOriginalSession(t *testing.T) {
	f := collectionPublicationFixture(t)
	draft, in := f.draft("journey")
	blocker, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(f.ctx, `SELECT id FROM journeys WHERE id=$1 FOR UPDATE`, draft.ID); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		_, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, "journey", draft.ID, in)
		result <- e
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM journeys WHERE id=%FOR UPDATE%')`).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual publication resource wait not observed")
		}
		time.Sleep(15 * time.Millisecond)
	}
	// Native generation changes by a real authority field, then returns to A.
	f.exec(`UPDATE sessions SET authentication_method='test_changed' WHERE id=$1`, f.session)
	f.exec(`UPDATE sessions SET authentication_method='test' WHERE id=$1`, f.session)
	if e = blocker.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
		if !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("publication reused session ABA", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not retire")
	}
	v, e := f.s.GetHumanCollection(f.ctx, f.digest, f.owner, "journey", draft.ID)
	if e != nil || v.Revision != 1 || v.Status != "draft" {
		t.Fatal("failed final gate committed mutation", v, e)
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM personal_collection_publication_operations WHERE owner_account_id=$1 AND operation_id=$2`, f.owner.ID, in.OperationID).Scan(&n); e != nil || n != 0 {
		t.Fatal("failed publish produced receipt", n, e)
	}
}
func TestCollectionPublicationNativeEmptyMigrationRoundtrip(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	for _, name := range []string{"129_collection_publication_close.down.sql", "128_collection_publication.down.sql", "128_collection_publication.sql", "129_collection_publication_close.sql"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(name, e)
		}
	}
	var present bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('personal_collection_publication_operations') IS NOT NULL`).Scan(&present); e != nil || !present {
		t.Fatal("roundtrip missing original receipt ledger", e)
	}
}

func collectionCloseSelector(v content.PersonalCollection, in content.CollectionPublicationInput) content.CollectionPublicationCloseInput {
	return content.CollectionPublicationCloseInput{Kind: v.Kind, ID: v.ID, Action: "PUBLISH", Revision: in.Revision}
}
func (f *collectionPublicationNativeFixture) operationCount(op string) int {
	f.t.Helper()
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM personal_collection_publication_operations WHERE owner_account_id=$1 AND operation_id=$2`, f.owner.ID, op).Scan(&n); e != nil {
		f.t.Fatal(e)
	}
	return n
}
func (f *collectionPublicationNativeFixture) expireSession() {
	f.t.Helper()
	// The session was created before this test's real resource operations.
	// Keep its expiry strictly after created_at as required by the native
	// constraint, and prove it is expired at the actual database clock.
	f.exec(`UPDATE sessions SET idle_expires_at=created_at+interval '1 microsecond' WHERE id=$1`, f.session)
	var expired bool
	if e := f.pool.QueryRow(f.ctx, `SELECT idle_expires_at>created_at AND idle_expires_at<=expires_at AND idle_expires_at<clock_timestamp() FROM sessions WHERE id=$1`, f.session).Scan(&expired); e != nil || !expired {
		f.t.Fatal("fixture did not produce a real constraint-valid Session expiry", e)
	}
}
func (f *collectionPublicationNativeFixture) waitForQueryLock(query string) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if e := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, query).Scan(&waiting); e != nil {
			f.t.Fatal(e)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatal("actual owned database lock wait not observed", query)
		}
		time.Sleep(15 * time.Millisecond)
	}
}
func TestCollectionPublicationNativeCloseNoEffectAndLateRequests(t *testing.T) {
	f := collectionPublicationFixture(t)
	for _, kind := range []string{"journey", "theme_map"} {
		t.Run(kind, func(t *testing.T) {
			draft, in := f.draft(kind)
			selector := collectionCloseSelector(draft, in)
			if _, e := f.s.GetHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("never-sent selector already had effect", e)
			}
			v, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector)
			if e != nil || v.ID != draft.ID || v.Revision != draft.Revision || v.Title != draft.Title || v.Summary != draft.Summary || v.Status != "draft" || v.Visibility != "private" || v.PublicationOperation == nil || v.PublicationOperation.Outcome != "NO_EFFECT" || v.PublicationOperation.ResultRevision != 0 {
				t.Fatal("close altered original private draft", v, e)
			}
			for i, entry := range v.Entries {
				if entry.PlaceID != draft.Entries[i].PlaceID || entry.Note != draft.Entries[i].Note || entry.LocationVisibility != draft.Entries[i].LocationVisibility {
					t.Fatal("close changed retained draft", entry)
				}
			}
			var nulls bool
			if e = f.pool.QueryRow(f.ctx, `SELECT outcome='NO_EFFECT' AND result_revision IS NULL AND input_sha256 IS NULL FROM personal_collection_publication_operations WHERE owner_account_id=$1 AND operation_id=$2`, f.owner.ID, in.OperationID).Scan(&nulls); e != nil || !nulls {
				t.Fatal("NO_EFFECT retained approval or invented result", e)
			}
			if _, e = f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, in); !errors.Is(e, content.ErrConflict) {
				t.Fatal("late original publish escaped terminal selector", e)
			}
			if _, e = f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, kind, draft.ID, content.CollectionUnpublishInput{OperationID: in.OperationID, Revision: in.Revision}); !errors.Is(e, content.ErrConflict) {
				t.Fatal("late changed action escaped terminal selector", e)
			}
			for _, change := range []func(*content.CollectionPublicationCloseInput){func(s *content.CollectionPublicationCloseInput) { s.Revision++ }, func(s *content.CollectionPublicationCloseInput) { s.Action = "UNPUBLISH" }, func(s *content.CollectionPublicationCloseInput) { s.ID = f.uuid() }, func(s *content.CollectionPublicationCloseInput) {
				if s.Kind == "journey" {
					s.Kind = "theme_map"
				} else {
					s.Kind = "journey"
				}
			}} {
				changed := selector
				change(&changed)
				if _, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, changed); !errors.Is(e, content.ErrConflict) {
					t.Fatal("terminal selector rebound", changed, e)
				}
			}
			if replay, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector); e != nil || replay.PublicationOperation.Outcome != "NO_EFFECT" || f.operationCount(in.OperationID) != 1 {
				t.Fatal("close replay rewrote terminal receipt", replay, e)
			}
			// Native CAS editing can continue under a fresh operation; the old
			// receipt remains bound to the old selector and reads current truth.
			// Submit editable fields only; the native read projection includes
			// availability/coordinates/source leases that are not write grants.
			changed := content.CollectionInput{Kind: draft.Kind, CityID: draft.CityID, Title: "结束未知操作后保留修改稿", Summary: draft.Summary, Entries: []content.CollectionEntry{}}
			for _, entry := range draft.Entries {
				changed.Entries = append(changed.Entries, content.CollectionEntry{PlaceID: entry.PlaceID, Note: entry.Note, LocationVisibility: entry.LocationVisibility})
			}
			updated, e := f.s.UpdateHumanCollection(f.ctx, f.digest, f.owner, draft.ID, draft.Revision, changed)
			if e != nil {
				t.Fatal(e)
			}
			recovered, e := f.s.GetHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID)
			if e != nil || recovered.ID != draft.ID || recovered.Title != updated.Title || recovered.Revision != updated.Revision || recovered.PublicationOperation.ExpectedRevision != in.Revision || recovered.PublicationOperation.Outcome != "NO_EFFECT" {
				t.Fatal("NO_EFFECT revived an old draft", recovered, e)
			}
			if _, e = f.pool.Exec(f.ctx, `UPDATE personal_collection_publication_operations SET outcome='COMMITTED' WHERE owner_account_id=$1 AND operation_id=$2`, f.owner.ID, in.OperationID); e == nil {
				t.Fatal("terminal NO_EFFECT was mutable")
			}
			if _, e = f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{}, kind, draft.ID); !errors.Is(e, content.ErrNotFound) {
				t.Fatal("ending uncertainty made draft public", e)
			}
			// An ended retraction means that retraction had no effect: it must
			// not silently make a currently published collection private.
			published, _ := f.publish(kind)
			unpublishOp := f.uuid()
			unpublishSelector := content.CollectionPublicationCloseInput{Kind: kind, ID: published.ID, Action: "UNPUBLISH", Revision: published.Revision}
			unchanged, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, unpublishOp, unpublishSelector)
			if e != nil || unchanged.ID != published.ID || unchanged.Status != "published" || unchanged.Visibility != "public" || unchanged.Revision != published.Revision || unchanged.PublicationOperation.Outcome != "NO_EFFECT" {
				t.Fatal("ending an unsent retraction performed a retraction", unchanged, e)
			}
			if _, e = f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, kind, published.ID, content.CollectionUnpublishInput{OperationID: unpublishOp, Revision: published.Revision}); !errors.Is(e, content.ErrConflict) {
				t.Fatal("late original retraction escaped NO_EFFECT", e)
			}
			if _, e = f.s.GetPublicCollection(f.ctx, content.PublicMomentAccess{}, kind, published.ID); e != nil {
				t.Fatal("NO_EFFECT incorrectly withdrew the original public entity", e)
			}
		})
	}
}

func TestCollectionPublicationNativeCloseCommittedRereadsOriginalEntity(t *testing.T) {
	f := collectionPublicationFixture(t)
	for _, kind := range []string{"journey", "theme_map"} {
		t.Run(kind, func(t *testing.T) {
			published, in := f.publish(kind)
			selector := collectionCloseSelector(published, in)
			v, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector)
			if e != nil || v.ID != published.ID || v.Status != "published" || v.Revision != 2 || v.PublicationOperation.Outcome != "COMMITTED" || v.PublicationOperation.ResultRevision != 2 {
				t.Fatal("close cancelled a committed publication", v, e)
			}
			unpub := content.CollectionUnpublishInput{OperationID: f.uuid(), Revision: v.Revision}
			private, e := f.s.UnpublishHumanCollection(f.ctx, f.digest, f.owner, kind, v.ID, unpub)
			if e != nil {
				t.Fatal(e)
			}
			selector.Action, selector.Revision = "UNPUBLISH", unpub.Revision
			v, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, unpub.OperationID, selector)
			if e != nil || v.ID != private.ID || v.Status != "draft" || v.Revision != 3 || v.PublicationOperation.Outcome != "COMMITTED" || v.PublicationOperation.Action != "UNPUBLISH" || v.PublicationOperation.ResultRevision != 3 {
				t.Fatal("original retraction commit was replaced", v, e)
			}
			selector.Action, selector.Revision = "PUBLISH", in.Revision
			v, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector)
			if e != nil || v.Status != "draft" || v.Visibility != "private" || v.Revision != 3 || v.PublicationOperation.ResultRevision != 2 || v.PublicationOperation.Outcome != "COMMITTED" {
				t.Fatal("close replay revived original publication", v, e)
			}
			if e = f.s.WithdrawHumanCollection(f.ctx, f.digest, f.owner, kind, v.ID, v.Revision); e != nil {
				t.Fatal(e)
			}
			v, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector)
			if e != nil || v.Status != "withdrawn" || v.Revision != 4 || v.PublicationOperation.Outcome != "COMMITTED" || v.PublicationOperation.ResultRevision != 2 {
				t.Fatal("soft withdrawal fabricated a replacement entity", v, e)
			}
		})
	}
}

type collectionPublicationNativeResult struct {
	value content.PersonalCollection
	err   error
}

func TestCollectionPublicationNativeCloseBothLockOrders(t *testing.T) {
	f := collectionPublicationFixture(t)
	for _, first := range []string{"publish", "close"} {
		t.Run(first, func(t *testing.T) {
			draft, in := f.draft("journey")
			selector := collectionCloseSelector(draft, in)
			blocker, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			if _, e = blocker.Exec(f.ctx, `SELECT id FROM journeys WHERE id=$1 FOR UPDATE`, draft.ID); e != nil {
				t.Fatal(e)
			}
			publish, closeResult := make(chan collectionPublicationNativeResult, 1), make(chan collectionPublicationNativeResult, 1)
			startPublish := func() {
				go func() {
					v, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, "journey", draft.ID, in)
					publish <- collectionPublicationNativeResult{v, e}
				}()
			}
			startClose := func() {
				go func() {
					v, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector)
					closeResult <- collectionPublicationNativeResult{v, e}
				}()
			}
			if first == "publish" {
				startPublish()
			} else {
				startClose()
			}
			f.waitForQueryLock("SELECT id FROM journeys WHERE id=%FOR UPDATE%")
			if first == "publish" {
				startClose()
			} else {
				startPublish()
			}
			f.waitForQueryLock("SELECT pg_advisory_xact_lock%")
			if e = blocker.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			var p, c collectionPublicationNativeResult
			select {
			case p = <-publish:
			case <-time.After(5 * time.Second):
				t.Fatal("publish did not leave real lock wait")
			}
			select {
			case c = <-closeResult:
			case <-time.After(5 * time.Second):
				t.Fatal("close did not leave real lock wait")
			}
			if c.err != nil || c.value.ID != draft.ID || c.value.PublicationOperation == nil || f.operationCount(in.OperationID) != 1 {
				t.Fatal("close lost original entity or single receipt", c)
			}
			if first == "publish" {
				if p.err != nil || p.value.PublicationOperation.Outcome != "COMMITTED" || c.value.PublicationOperation.Outcome != "COMMITTED" || c.value.Revision != 2 {
					t.Fatal("publish-first did not retain committed effect", p, c)
				}
			} else if !errors.Is(p.err, content.ErrConflict) || c.value.PublicationOperation.Outcome != "NO_EFFECT" || c.value.Revision != 1 {
				t.Fatal("close-first allowed late publish", p, c)
			}
		})
	}
}

func TestCollectionPublicationNativeCloseOwnerSessionAndRetiredSource(t *testing.T) {
	f := collectionPublicationFixture(t)
	draft, in := f.draft("theme_map")
	selector := collectionCloseSelector(draft, in)
	for _, c := range []struct {
		actor  identity.Actor
		digest [32]byte
		want   error
	}{
		{f.peer, f.peerDigest, content.ErrNotFound},
		{f.owner, f.peerDigest, identity.ErrUnauthorized},
		{f.owner, [32]byte{}, identity.ErrUnauthorized},
		{identity.Actor{ID: f.owner.ID, AccountType: "organization"}, f.digest, identity.ErrUnauthorized},
	} {
		if _, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, c.digest, c.actor, in.OperationID, selector); !errors.Is(e, c.want) {
			t.Fatal("close bypassed owner/Session boundary", e, c.want)
		}
	}
	if f.operationCount(in.OperationID) != 0 {
		t.Fatal("unauthorized close sealed someone else's operation")
	}
	// City expiry and Place withdrawal cannot grant publishing and do not
	// obstruct the current owner ending an original uncertain operation.
	f.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, draft.CityID)
	f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.places[0])
	if _, e := f.s.PublishHumanCollection(f.ctx, f.digest, f.owner, draft.Kind, draft.ID, in); !errors.Is(e, content.ErrConflict) {
		t.Fatal("retired sources authorized publication", e)
	}
	v, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector)
	if e != nil || v.PublicationOperation.Outcome != "NO_EFFECT" || v.Revision != 1 || v.Entries[0].Available || v.Entries[0].PlaceName != "" || v.Entries[0].Latitude != nil || v.Entries[0].Longitude != nil {
		t.Fatal("retired source blocked close or revived old approval", v, e)
	}
	if e = f.s.WithdrawHumanCollection(f.ctx, f.digest, f.owner, draft.Kind, draft.ID, v.Revision); e != nil {
		t.Fatal(e)
	}
	unknown := f.uuid()
	v, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, unknown, selector)
	if e != nil || v.Status != "withdrawn" || v.ID != draft.ID || v.Revision != 2 || v.PublicationOperation.Outcome != "NO_EFFECT" {
		t.Fatal("withdrawn original entity was fabricated or mutated", v, e)
	}
	missing := selector
	missing.ID = f.uuid()
	missingOp := f.uuid()
	if _, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, missingOp, missing); !errors.Is(e, content.ErrNotFound) || f.operationCount(missingOp) != 0 {
		t.Fatal("missing original entity invented NO_EFFECT", e)
	}
	f.expireSession()
	if _, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("expired Session recovered terminal authority", e)
	}
}

func TestCollectionPublicationNativeCloseWaitRechecksOriginalSession(t *testing.T) {
	for _, retirement := range []string{"ABA", "expired"} {
		t.Run(retirement, func(t *testing.T) {
			f := collectionPublicationFixture(t)
			draft, in := f.draft("journey")
			selector := collectionCloseSelector(draft, in)
			blocker, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			if _, e = blocker.Exec(f.ctx, `SELECT id FROM journeys WHERE id=$1 FOR UPDATE`, draft.ID); e != nil {
				t.Fatal(e)
			}
			var generation int64
			if e = f.pool.QueryRow(f.ctx, `SELECT authorization_generation FROM sessions WHERE id=$1`, f.session).Scan(&generation); e != nil {
				t.Fatal(e)
			}
			result := make(chan error, 1)
			go func() {
				_, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, selector)
				result <- e
			}()
			f.waitForQueryLock("SELECT id FROM journeys WHERE id=%FOR UPDATE%")
			if retirement == "ABA" {
				f.exec(`UPDATE sessions SET authentication_method='changed' WHERE id=$1`, f.session)
				f.exec(`UPDATE sessions SET authentication_method='test' WHERE id=$1`, f.session)
				var current int64
				if e = f.pool.QueryRow(f.ctx, `SELECT authorization_generation FROM sessions WHERE id=$1`, f.session).Scan(&current); e != nil || current <= generation {
					t.Fatal("fixture did not actually advance native Session generation", e)
				}
			} else {
				f.expireSession()
			}
			if e = blocker.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-result:
				if !errors.Is(e, identity.ErrUnauthorized) {
					t.Fatal("late close retained original Session authority", e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("close did not retire after resource wait")
			}
			if f.operationCount(in.OperationID) != 0 {
				t.Fatal("retired close committed NO_EFFECT")
			}
			var revision int64
			if e = f.pool.QueryRow(f.ctx, `SELECT revision FROM journeys WHERE id=$1`, draft.ID).Scan(&revision); e != nil || revision != 1 {
				t.Fatal("retired close altered draft", revision, e)
			}
		})
	}
}

func TestCollectionPublicationNativeCloseMigrationRoundtripAndHistory(t *testing.T) {
	for _, history := range []string{"empty", "COMMITTED", "NO_EFFECT"} {
		t.Run(history, func(t *testing.T) {
			f := collectionPublicationFixture(t)
			var draft content.PersonalCollection
			var in content.CollectionPublicationInput
			if history == "COMMITTED" {
				draft, in = f.publish("journey")
			}
			if history == "NO_EFFECT" {
				draft, in = f.draft("journey")
				if _, e := f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, collectionCloseSelector(draft, in)); e != nil {
					t.Fatal(e)
				}
			}
			down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "129_collection_publication_close.down.sql"))
			if e != nil {
				t.Fatal(e)
			}
			conn, e := f.pool.Acquire(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Release()
			_, e = conn.Exec(f.ctx, string(down))
			if history == "NO_EFFECT" {
				var native *pgconn.PgError
				if !errors.As(e, &native) || native.Code != "23514" {
					t.Fatal("rollback discarded terminal NO_EFFECT history", e)
				}
				if _, e = conn.Exec(f.ctx, "ROLLBACK"); e != nil {
					t.Fatal(e)
				}
				if receipt, e := f.s.GetHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID); e != nil || receipt.PublicationOperation.Outcome != "NO_EFFECT" {
					t.Fatal("failed rollback lost terminal receipt", receipt, e)
				}
				// Real database constraints reject terminal shapes that could
				// confuse a client into treating unknown effects as committed.
				for _, shape := range []struct {
					outcome string
					result  any
					digest  any
				}{
					{"NO_EFFECT", int64(2), nil}, {"NO_EFFECT", nil, make([]byte, 32)},
					{"COMMITTED", nil, make([]byte, 32)}, {"COMMITTED", int64(2), nil},
					{"UNKNOWN", nil, nil},
				} {
					_, e = conn.Exec(f.ctx, `INSERT INTO personal_collection_publication_operations(owner_account_id,operation_id,kind,target_id,journey_id,theme_map_id,action,expected_revision,outcome,result_revision,input_sha256) SELECT owner_account_id,$3,kind,target_id,journey_id,theme_map_id,action,expected_revision,$4,$5,$6 FROM personal_collection_publication_operations WHERE owner_account_id=$1 AND operation_id=$2`, f.owner.ID, in.OperationID, f.uuid(), shape.outcome, shape.result, shape.digest)
					if !errors.As(e, &native) || native.Code != "23514" {
						t.Fatal("invalid terminal receipt shape accepted", shape.outcome, e)
					}
				}
				return
			}
			if e != nil {
				t.Fatal("empty/committed down failed", e)
			}
			up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "129_collection_publication_close.sql"))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = conn.Exec(f.ctx, string(up)); e != nil {
				t.Fatal("129 reapply failed", e)
			}
			if history == "COMMITTED" {
				v, e := f.s.GetHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID)
				if e != nil || v.ID != draft.ID || v.PublicationOperation.Outcome != "COMMITTED" || v.PublicationOperation.ResultRevision != 2 {
					t.Fatal("upgrade changed original commit causality", v, e)
				}
			} else {
				draft, in = f.draft("theme_map")
				if _, e = f.s.CloseHumanCollectionPublicationOperation(f.ctx, f.digest, f.owner, in.OperationID, collectionCloseSelector(draft, in)); e != nil {
					t.Fatal("reapplied NO_EFFECT contract unusable", e)
				}
			}
		})
	}
}
