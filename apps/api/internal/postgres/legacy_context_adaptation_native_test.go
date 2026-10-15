package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacycontext"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"reflect"
	"strings"
	"testing"
)

func legacyContextNativeInput(t *testing.T) (LegacyPrivateMediaInput, []byte) {
	input, raw := legacyMediaNativeInput(t)
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(input.Source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		t.Fatal("synthetic context source")
	}
	input.Source, input.Manifest = legacySyntheticInput(t, func(tables map[string]any) {
		for key, rs := range doc.Tables {
			out := []any{}
			for _, r := range rs {
				out = append(out, r)
			}
			tables[key] = out
		}
		row := func(table string) map[string]any {
			r := map[string]any{}
			for _, k := range legacymigration.ColumnNames(table) {
				r[k] = nil
			}
			return r
		}
		owner := "14600000-0000-4000-8000-000000000010"
		note := "14600000-0000-4000-8000-000000000020"
		second := "16200000-0000-4000-8000-000000000026"
		thread := "16200000-0000-4000-8000-000000000030"
		deletedThread := "16200000-0000-4000-8000-000000000031"
		p1 := "16200000-0000-4000-8000-000000000040"
		p2 := "16200000-0000-4000-8000-000000000041"
		places := []any{}
		links := []any{}
		for i, id := range []string{p1, p2} {
			p := row("places")
			p["id"], p["name"], p["category_code"], p["status"], p["admin_status"], p["lifecycle_status"] = id, "合成无城市地点", "park", "active", "active", "active"
			places = append(places, p)
			links = append(links, map[string]any{"note_id": note, "place_id": id, "raw_name": "合成原关系", "position": i})
		}
		tables["places"], tables["note_places"] = places, links
		tables["user_saved_places"] = []any{map[string]any{"user_id": owner, "place_id": p1, "created_at": "2021-01-01T00:00:00Z"}}
		tables["note_media_locations"] = []any{map[string]any{"note_id": note, "media_id": "15700000-0000-4000-8000-000000000011", "latitude": json.Number("30.123456789123456789"), "longitude": json.Number("120.123"), "map_enabled": true, "place_ids": []any{p1, p2}, "captured_date": "2021-02-03"}}
		notes := tables["notes"].([]any)
		first := notes[0].(map[string]any)
		first["thread_id"], first["thread_position"], first["inbound_mode"] = thread, 0, "walking"
		clone := row("notes")
		for k, v := range first {
			clone[k] = v
		}
		clone["id"], clone["thread_position"], clone["caption"] = second, 1, "第二条合成原正文"
		notes = append(notes, clone)
		notes[1].(map[string]any)["thread_id"], notes[1].(map[string]any)["thread_position"] = deletedThread, 0
		tables["notes"] = notes
		tables["moment_threads"] = []any{map[string]any{"id": thread, "author_id": owner, "entry_count": 2, "route_revision": 1, "created_at": "2021-01-01T00:00:00Z"}, map[string]any{"id": deletedThread, "author_id": owner, "entry_count": 1, "route_revision": 1, "created_at": "2021-01-01T00:00:00Z"}}
		tables["moment_thread_legs"] = []any{map[string]any{"thread_id": thread, "from_note_id": note, "to_note_id": second, "from_position": 0, "to_position": 1, "mode": "walking", "from_anchor_place_id": p1, "to_anchor_place_id": p2, "created_at": "2021-01-01T00:00:00Z", "updated_at": "2021-01-01T00:00:00Z"}}
	})
	var sourceDoc map[string]json.RawMessage
	if json.Unmarshal(input.Source, &sourceDoc) != nil {
		t.Fatal("synthetic header")
	}
	var matched map[string]any
	if json.Unmarshal(input.Matched, &matched) != nil {
		t.Fatal("synthetic matcher")
	}
	matched["source"], matched["sourceFileSha256"] = sourceDoc["source"], legacymedia.Hash(input.Source)
	input.Matched, _ = json.Marshal(matched)
	var seal map[string]any
	if json.Unmarshal(input.AuthenticatedSeal, &seal) != nil {
		t.Fatal("synthetic seal")
	}
	seal["matcherFileSHA256"] = legacymedia.Hash(input.Matched)
	input.AuthenticatedSeal, _ = json.Marshal(seal)
	input.ExpectedSealSHA256 = legacymedia.Hash(input.AuthenticatedSeal)
	return input, raw
}

type context162FaultDatabase struct {
	legacyMigrationDatabase
	t    *testing.T
	fail bool
}

func (d context162FaultDatabase) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, e := d.legacyMigrationDatabase.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return &context162Tx{Tx: tx, t: d.t, fail: d.fail}, nil
}

type context162Tx struct {
	pgx.Tx
	t       *testing.T
	fail    bool
	members int
}

func (tx *context162Tx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if tx.fail && strings.Contains(q, "INSERT INTO moment_thread_members") {
		tx.members++
		if tx.members == 2 {
			return pgconn.CommandTag{}, errors.New("synthetic_second_original_member_failure")
		}
	}
	tag, e := tx.Tx.Exec(ctx, q, args...)
	var p *pgconn.PgError
	if errors.As(e, &p) {
		tx.t.Logf("context SQL stage=%s code=%s constraint=%s", strings.Fields(q)[0], p.Code, p.ConstraintName)
	}
	return tag, e
}
func (tx *context162Tx) Commit(ctx context.Context) error {
	e := tx.Tx.Commit(ctx)
	var p *pgconn.PgError
	if errors.As(e, &p) {
		tx.t.Logf("context SQL stage=COMMIT code=%s constraint=%s", p.Code, p.ConstraintName)
	}
	return e
}
func TestLegacyContextNativeAtomicOriginalOwnerAndExistingThreadPublish(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	input, raw := legacyContextNativeInput(t)
	plan, e := legacycontext.Build(legacyTestRunID, input.Source, input.Manifest)
	if e != nil || plan.Counts.Places != 2 || plan.Counts.NoteLocations != 2 || plan.Counts.MediaContexts != 1 || plan.Counts.Threads != 1 || plan.Counts.Members != 2 || plan.Counts.Legs != 1 {
		t.Fatal("typed synthetic plan", e, plan.Counts)
	}
	for _, apply := range []func() error{func() error {
		_, e := f.Importer.Apply(f.Context, legacyTestRunID, input.Source, input.Manifest)
		return e
	}, func() error {
		_, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, input.Source, input.Manifest)
		return e
	}, func() error {
		_, e := f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, input.Source, input.Manifest)
		return e
	}, func() error {
		_, e := f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, input, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(raw)), nil
		})
		return e
	}} {
		if e = apply(); e != nil {
			t.Fatal("independent original phase", e)
		}
	}
	fault := *f.Importer
	fault.database = context162FaultDatabase{f.Importer.database, t, true}
	if _, e = fault.ApplyContextAdaptation(f.Context, legacyTestRunID, input.Source, input.Manifest); e == nil {
		t.Fatal("synthetic second original child failure committed")
	}
	var clean bool
	if e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM legacy_native_context_runs) AND NOT EXISTS(SELECT 1 FROM legacy_native_context_entities) AND NOT EXISTS(SELECT 1 FROM private_moment_media_contexts) AND NOT EXISTS(SELECT 1 FROM moment_threads) AND NOT EXISTS(SELECT 1 FROM places WHERE city_id IS NULL)`).Scan(&clean); e != nil || !clean {
		t.Fatal("context transaction partially survived", e)
	}
	actual := *f.Importer
	actual.database = context162FaultDatabase{f.Importer.database, t, false}
	first, e := actual.ApplyContextAdaptation(f.Context, legacyTestRunID, input.Source, input.Manifest)
	if e != nil || first.Status != "COMMITTED" || first.Counts != plan.Counts {
		t.Fatal("context import", e, first.Counts)
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT(SELECT count(*) FROM sessions)=0 AND(SELECT count(*) FROM moments WHERE status='draft' AND visibility='private')=2 AND NOT EXISTS(SELECT 1 FROM moments WHERE status='published') AND(SELECT count(*) FROM moment_thread_operations)=0 AND(SELECT bool_and(status='suspended') FROM accounts WHERE id IN(SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user'))`).Scan(&clean); e != nil || !clean {
		t.Fatal("context import fabricated publication/session/activation", e)
	}
	var owner string
	if e = f.Pool.QueryRow(f.Context, `SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user'`).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	actor := identity.Actor{ID: owner, AccountType: "person"}
	digest := sha256.Sum256([]byte("context162-synthetic-fresh-owner-session"))
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `WITH cl AS(SELECT clock_timestamp() at)INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	mid := plan.Threads[0].Members[0].MomentID
	contexts, e := store.ReadOwnedMomentMediaContexts(f.Context, digest, actor, mid)
	if e != nil || len(contexts.Items) != 1 || contexts.Items[0].CoordinateStatus != "UNSPECIFIED" || contexts.Items[0].SourceLatitude.String() != "30.123456789123456789" || len(contexts.Items[0].Locations) != 2 || contexts.Items[0].Locations[0].LegacyReference == nil {
		t.Fatal("owner current typed media metadata", e)
	}
	if e = contexts.ResponseProof.RevalidateHumanControl(f.Context); e != nil {
		t.Fatal("context response proof", e)
	}
	ref := lpr.Reference{Kind: "moment", RecordID: mid, PlaceID: plan.Places[0].TargetID}
	detail, e := store.ReadHumanLegacyPlaceReference(f.Context, digest, actor, ref)
	if e != nil || detail.CityID != "" || detail.Latitude != nil || detail.LocationPrecision != "none" {
		t.Fatal("private no-city detail or invented coordinate", e)
	}
	owned, e := store.ReadOwnedMomentThread(f.Context, digest, actor, mid, true)
	if e != nil || owned.Audience != "PRIVATE" || len(owned.Members) != 2 || len(owned.EntryContexts) != 2 || len(owned.Legs) != 1 || owned.RootMomentID == nil {
		t.Fatal("original owner thread metadata", e)
	}
	if e = owned.ResponseProof.RevalidateHumanControl(f.Context); e != nil {
		t.Fatal("owned thread proof", e)
	}
	peer := "16200000-0000-4000-8000-000000000090"
	peerDigest := sha256.Sum256([]byte("context162-synthetic-foreign-owner-session"))
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, peer); e != nil {
		t.Fatal("independent synthetic person", e)
	}
	if _, e = f.Pool.Exec(f.Context, `WITH cl AS(SELECT clock_timestamp() at)INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl`, peer, peerDigest[:]); e != nil {
		t.Fatal("independent synthetic Session", e)
	}
	peerActor := identity.Actor{ID: peer, AccountType: "person"}
	if _, e = store.ReadOwnedMomentMediaContexts(f.Context, peerDigest, peerActor, mid); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("foreign owner read private context", e)
	}
	if _, e = store.ReadOwnedMomentThread(f.Context, peerDigest, peerActor, owned.ThreadID, false); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("foreign owner read original private thread", e)
	}
	if _, e = store.ReadOwnedMomentThread(f.Context, [32]byte{}, identity.Actor{}, owned.ThreadID, false); e == nil {
		t.Fatal("anonymous private thread readable")
	}
	saved, e := store.ListHumanSavedPage(f.Context, digest, actor, "")
	if e != nil || len(saved.Items) != 1 || saved.Items[0].Available || saved.Items[0].LegacyReference == nil {
		t.Fatal("native unplaced saved reference", e)
	}
	// One original child edit remains the same imported object and is carried by
	// the subsequent whole-thread preview, never copied into a replacement child.
	current, e := store.GetHumanMoment(f.Context, digest, actor, mid)
	if e != nil {
		t.Fatal(e)
	}
	draftInput := content.MomentInput{Body: current.Body + " 本人继续编辑", TimePrecision: "unknown", LocationPrecision: current.LocationPrecision, CityID: current.CityID, PlaceID: current.PlaceID}
	op := "16200000-0000-4000-8000-000000000071"
	update := content.MomentControlInput{OperationID: op, Action: "UPDATE", MomentID: mid, ExpectedRevision: current.Revision, Input: &draftInput}
	preview, e := store.PreviewMomentControl(f.Context, digest, actor, update)
	if e != nil {
		t.Fatal("ordinary original child edit preview", e)
	}
	update.SourceVersion, update.Approval = preview.SourceVersion, preview.Approval
	if _, e = store.CommitMomentControl(f.Context, digest, actor, update); e != nil {
		t.Fatal("ordinary original child edit", e)
	}
	imagePlan, e := legacymedia.Build(legacyTestRunID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.DeleteHumanPrivateImage(f.Context, digest, actor, mid, imagePlan.Records[0].ImageID, 1); e != nil {
		t.Fatal("owner original image delete", e)
	}
	fresh, e := store.ReadOwnedMomentMediaContexts(f.Context, digest, actor, mid)
	if e != nil || len(fresh.Items) != 0 {
		t.Fatal("deleted asset context still readable", e)
	}
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM saved_items WHERE owner_account_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	thread := content.MomentThreadInput{OperationID: "16200000-0000-4000-8000-000000000072", Action: "PUBLISH", ThreadID: owned.ThreadID, ExpectedVersion: owned.Version, LocalScope: "16200000-0000-4000-8000-000000000073", Audience: "PUBLIC", Items: []content.MomentThreadItem{}}
	for _, m := range owned.Members {
		current, e := store.GetHumanMoment(f.Context, digest, actor, *m.MomentID)
		if e != nil {
			t.Fatal(e)
		}
		thread.Items = append(thread.Items, content.MomentThreadItem{MomentID: current.ID, ExpectedRevision: current.Revision, Input: content.MomentInput{Body: current.Body, CityID: current.CityID, PlaceID: current.PlaceID, TimePrecision: "unknown", LocationPrecision: current.LocationPrecision}, Media: []momentmedia.AttachmentInput{}})
	}
	tp, e := store.PreviewMomentThread(f.Context, digest, actor, thread)
	if e != nil || len(tp.Members) != 0 || len(tp.Locations) != 2 {
		t.Fatal("existing private thread preview", e)
	}
	thread.SourceVersion, thread.Approval = tp.SourceVersion, tp.Approval
	fact, e := store.CommitMomentThread(f.Context, digest, actor, thread)
	if e != nil || fact.Action != "PUBLISH" || fact.AppliedVersion != 2 || fact.Members[0].MomentID != mid || fact.Members[0].AppliedRevision != 3 {
		t.Fatal("atomic existing thread publication", e)
	}
	for _, m := range fact.Members {
		if _, e = store.GetPublicMoment(f.Context, content.PublicMomentAccess{}, m.MomentID); e != nil {
			t.Fatal("original child public", e)
		}
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT count(*)=2 FROM moments WHERE author_account_id=$1`, owner).Scan(&clean); e != nil || !clean {
		t.Fatal("whole publication cloned original Moments", e)
	}
	again, e := store.CommitMomentThread(f.Context, digest, actor, thread)
	if e != nil || !reflect.DeepEqual(fact, again) {
		t.Fatal("original PUBLISH receipt changed", e)
	}
	if recovered, e := store.ReadMomentThreadOperation(f.Context, digest, actor, thread.OperationID, false); e != nil || !reflect.DeepEqual(fact, recovered) {
		t.Fatal("original PUBLISH GET fact changed", e)
	}
	for _, repeat := range []bool{false, true} {
		var got LegacyContextAdaptationResult
		if repeat {
			got, e = actual.ApplyContextAdaptation(f.Context, legacyTestRunID, input.Source, input.Manifest)
		} else {
			got, e = actual.InspectContextAdaptation(f.Context, legacyTestRunID)
		}
		if e != nil || !reflect.DeepEqual(first, got) {
			t.Fatal("immutable context fact changed after user actions", e)
		}
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM saved_items WHERE owner_account_id=$1)AND EXISTS(SELECT 1 FROM private_moment_images WHERE id=$2 AND status='deleted' AND derivative IS NULL)`, owner, imagePlan.Records[0].ImageID).Scan(&clean); e != nil || !clean {
		t.Fatal("import replay resurrected user's removed objects", e)
	}
}
