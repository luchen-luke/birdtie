package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacycontext"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type map170TraceDB struct {
	legacyMigrationDatabase
	t *testing.T
}

func (d map170TraceDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, e := d.legacyMigrationDatabase.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return map170TraceTx{Tx: tx, t: d.t}, nil
}

type map170TraceTx struct {
	pgx.Tx
	t *testing.T
}

func (t map170TraceTx) Exec(ctx context.Context, q string, a ...any) (pgconn.CommandTag, error) {
	v, e := t.Tx.Exec(ctx, q, a...)
	if e != nil {
		t.t.Logf("synthetic map170 SQL error: %v", e)
	}
	return v, e
}

func map170SyntheticInput(t *testing.T) (LegacyPrivateMediaInput, []byte) {
	input, raw := legacyContextNativeInput(t)
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(input.Source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		t.Fatal("synthetic source decode")
	}
	input.Source, input.Manifest = legacySyntheticInput(t, func(tables map[string]any) {
		for k, rs := range doc.Tables {
			a := []any{}
			for _, r := range rs {
				a = append(a, r)
			}
			tables[k] = a
		}
		extraPlace := map[string]any{}
		for k, v := range tables["places"].([]any)[0].(map[string]any) {
			extraPlace[k] = v
		}
		extraPlace["id"] = "17000000-0000-4000-8000-000000000043"
		extraPlace["name"] = "原公开目录但无原动态关联"
		tables["places"] = append(tables["places"].([]any), extraPlace)
		refs := []any{}
		for i, p := range tables["places"].([]any) {
			r := p.(map[string]any)
			r["source"] = "osm"
			r["lat"] = json.Number("57.15")
			if i == 0 {
				r["lng"] = json.Number("-2.1")
			} else {
				r["lng"] = json.Number("-2.11")
			}
			refs = append(refs, map[string]any{"provider": "openstreetmap", "external_id": "node/170" + string(rune('1'+i)), "place_id": r["id"], "created_at": nil, "raw_category": nil, "category_code": nil, "mapping_status": nil, "source_payload": map[string]any{"promotedFrom": "local-civu-catalog", "sourcePlaceId": r["id"]}, "domain_code": nil, "subcategory_code": nil, "type_detail_codes": nil})
		}
		tables["place_provider_refs"] = refs
		for _, n := range tables["notes"].([]any) {
			r := n.(map[string]any)
			if r["deleted_at"] == nil {
				r["moderation_status"] = "ok"
				r["visibility"] = "public"
				if r["id"] == "16200000-0000-4000-8000-000000000026" {
					r["visibility"] = "private"
				}
			}
		}
		only := map[string]any{}
		for k, v := range tables["notes"].([]any)[0].(map[string]any) {
			only[k] = v
		}
		onlyID := "17000000-0000-4000-8000-000000000027"
		only["id"] = onlyID
		only["thread_id"] = nil
		only["thread_position"] = nil
		only["created_at"] = "2020-01-01T00:00:00Z"
		only["caption"] = "只有原地点关系没有任何媒体"
		tables["notes"] = append(tables["notes"].([]any), only)
		for _, p := range append([]any{}, tables["note_places"].([]any)...) {
			r := p.(map[string]any)
			copy := map[string]any{}
			for k, v := range r {
				copy[k] = v
			}
			copy["note_id"] = onlyID
			tables["note_places"] = append(tables["note_places"].([]any), copy)
		}
	})
	var header map[string]json.RawMessage
	_ = json.Unmarshal(input.Source, &header)
	var matched map[string]any
	_ = json.Unmarshal(input.Matched, &matched)
	matched["source"] = header["source"]
	matched["sourceFileSha256"] = legacymedia.Hash(input.Source)
	input.Matched, _ = json.Marshal(matched)
	var seal map[string]any
	_ = json.Unmarshal(input.AuthenticatedSeal, &seal)
	seal["matcherFileSHA256"] = legacymedia.Hash(input.Matched)
	input.AuthenticatedSeal, _ = json.Marshal(seal)
	input.ExpectedSealSHA256 = legacymedia.Hash(input.AuthenticatedSeal)
	return input, raw
}

// One real PG chain in a marked random child. Source files, people, Session
// credentials and images here are synthetic, never a live imported account.
func TestMapContent170NativeOriginalPublicAndOwnPrivateCurrent(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	input, raw := map170SyntheticInput(t)
	ctx := f.Context
	plan, e := legacycontext.Build(legacyTestRunID, input.Source, input.Manifest)
	if e != nil {
		t.Fatal("synthetic native context", e)
	}
	for _, apply := range []func() error{func() error { _, e := f.Importer.Apply(ctx, legacyTestRunID, input.Source, input.Manifest); return e }, func() error {
		_, e := f.Importer.ApplyPersonalArchives(ctx, legacyTestRunID, input.Source, input.Manifest)
		return e
	}, func() error {
		_, e := f.Importer.ApplyNativeAdaptation(ctx, legacyTestRunID, input.Source, input.Manifest)
		return e
	}, func() error {
		_, e := f.Importer.ApplyLegacyPrivateMedia(ctx, legacyTestRunID, input, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(raw)), nil
		})
		return e
	}, func() error {
		_, e := f.Importer.ApplyContextAdaptation(ctx, legacyTestRunID, input.Source, input.Manifest)
		return e
	}} {
		if e = apply(); e != nil {
			t.Fatal("synthetic preserved phases", e)
		}
	}
	ready, e := PrepareLegacyMapRestore(legacyTestRunID, input.Source, input.Manifest)
	if e != nil || ready.Counts.Places != 3 || ready.Counts.CoordinatePlaces != 3 || ready.Counts.PublicMoments != 2 {
		t.Fatal("source-derived map plan", e, ready.Counts)
	}
	mapImporter := *f.Importer
	mapImporter.database = map170TraceDB{f.Importer.database, t}
	first, e := mapImporter.ApplyLegacyMapRestore(ctx, legacyTestRunID, input.Source, input.Manifest)
	if e != nil || first.Status != "COMMITTED" {
		t.Fatal("map proof import", e)
	}
	repeat, e := f.Importer.ApplyLegacyMapRestore(ctx, legacyTestRunID, input.Source, input.Manifest)
	if e != nil || !reflect.DeepEqual(first, repeat) {
		t.Fatal("original fact replay", e)
	}
	inspected, e := f.Importer.InspectLegacyMapRestore(ctx, legacyTestRunID)
	if e != nil || !reflect.DeepEqual(first, inspected) {
		t.Fatal("original inspect", e)
	}
	var owner, mid, privateMID, onlyMID string
	if e = f.Pool.QueryRow(ctx, `SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user'`).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	if e = f.Pool.QueryRow(ctx, `SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='moment' AND source_key='14600000-0000-4000-8000-000000000020'`).Scan(&mid); e != nil {
		t.Fatal(e)
	}
	if e = f.Pool.QueryRow(ctx, `SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='moment' AND source_key='16200000-0000-4000-8000-000000000026'`).Scan(&privateMID); e != nil {
		t.Fatal(e)
	}
	if e = f.Pool.QueryRow(ctx, `SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='moment' AND source_key='17000000-0000-4000-8000-000000000027'`).Scan(&onlyMID); e != nil {
		t.Fatal(e)
	}
	var unchanged bool
	if e = f.Pool.QueryRow(ctx, `SELECT (SELECT bool_and(status='draft' AND visibility='private' AND published_at IS NULL AND author_confirmed_at IS NULL)FROM moments) AND(SELECT status='suspended' FROM accounts WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM sessions)`, owner).Scan(&unchanged); e != nil || !unchanged {
		t.Fatal("read restoration published/activated original content", e)
	}
	store := New(f.Pool, true)
	newReader := func(seed string) (identity.Actor, [32]byte, string) {
		t.Helper()
		id, e := legacyMigrationUUID()
		if e != nil {
			t.Fatal(e)
		}
		digest := sha256.Sum256([]byte(seed))
		var session string
		if _, e = f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, id); e != nil {
			t.Fatal(e)
		}
		if e = f.Pool.QueryRow(ctx, `WITH cl AS(SELECT clock_timestamp() at)INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl RETURNING id`, id, digest[:]).Scan(&session); e != nil {
			t.Fatal(e)
		}
		return identity.Actor{ID: id, AccountType: "person"}, digest, session
	}
	viewer, digest, session := newReader("map170-viewer")
	q := content.MapContentQuery{CityID: "aberdeen-gb", Bounds: content.MapContentBounds{West: -2.4, South: 57, East: -1.9, North: 57.3}}
	started := time.Now()
	page, e := store.ReadHumanMapContent(ctx, digest, viewer, q)
	if e != nil || !page.Valid(time.Now()) || len(page.Pins) != 3 {
		t.Fatal("NULL-city OSM original-public grouped map", e, len(page.Pins))
	}
	t.Logf("map170 synthetic actual map read durationMs=%d", time.Since(started).Milliseconds())
	if e = page.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("map current proof", e)
	}
	for _, p := range page.Pins {
		if p.MomentCount == 0 {
			continue
		}
		if p.CityID != nil || p.MomentCount != 2 || p.Cover == nil || !containsMapString(p.VisibilitySources, "CIVU_PUBLIC_CATALOG") || !containsMapString(p.VisibilitySources, "CIVU_PUBLIC_MOMENT") {
			t.Fatal("NULL city, grouped count or original-public image absent")
		}
	}
	only, e := store.ReadHumanMapContentMoment(ctx, digest, viewer, onlyMID)
	if e != nil || len(only.Media) != 0 || len(only.Locations) != 2 {
		t.Fatal("precision-none original Note-place relations require exact source restore without media fallback", e)
	}
	detail, e := store.ReadHumanMapContentMoment(ctx, digest, viewer, mid)
	if e != nil || detail.CanEdit || detail.VisibilitySource != "CIVU_PUBLIC_MOMENT" || len(detail.Media) != 2 || len(detail.Locations) != 2 || detail.Status != "draft" || detail.Visibility != "private" {
		t.Fatal("original public detail/actual image", e)
	}
	if _, e = store.ReadHumanMapContentMoment(ctx, digest, viewer, privateMID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("original private Note leaked", e)
	}
	placePage, e := store.ReadHumanMapContentPlace(ctx, digest, viewer, plan.Places[0].TargetID, "")
	if e != nil || len(placePage.Items) != 2 || placePage.Items[0].ID != mid {
		t.Fatal("place-associated visible full body reader", e)
	}
	readerCtx, readerFrame, e := store.beginMapContent(ctx, digest, viewer)
	if e != nil {
		t.Fatal("cursor reader frame", e)
	}
	var jit string
	if e = readerFrame.tx.QueryRow(readerCtx, `SHOW jit`).Scan(&jit); e != nil || jit != "off" {
		t.Fatal("bounded map read must disable JIT only inside its transaction", e)
	}
	reader := historyReader(readerFrame)
	_ = readerFrame.tx.Rollback(readerCtx)
	shortUntil := placePage.ValidUntil.Add(-2 * time.Second)
	cursor, e := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: viewer.ID, Reader: reader, Query: content.MomentControlHash([]any{content.MapContentPlaceSchema, plan.Places[0].TargetID}), Source: placePage.SourceVersion, Before: mid, Until: shortUntil})
	if e != nil {
		t.Fatal("short page cursor", e)
	}
	continued, e := store.ReadHumanMapContentPlace(ctx, digest, viewer, plan.Places[0].TargetID, cursor)
	if e != nil || len(continued.Items) != 1 || !continued.ValidUntil.Equal(shortUntil) || continued.Items[0].ValidUntil.After(continued.ValidUntil) {
		t.Fatal("cursor lease must also cap every delivered item", e)
	}
	if e = continued.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("continued page current proof", e)
	}
	asset := detail.Media[0]
	binary, e := store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, asset.ID, asset.SourceVersion, "")
	if e != nil || len(binary.Bytes) != asset.ByteCount || legacymedia.Hash(binary.Bytes) != asset.SHA256 {
		t.Fatal("source-public canonical bytes", e)
	}
	if e = binary.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("binary metadata-only output proof", e)
	}
	originalBinaryProof := binary.ResponseProof
	clear(binary.Bytes)
	var coverPin content.MapContentPin
	for _, p := range page.Pins {
		if p.Cover != nil {
			coverPin = p
			break
		}
	}
	cover := coverPin.Cover
	if cover == nil {
		t.Fatal("real cover absent")
	}
	binary, e = store.ReadHumanMapContentBinary(ctx, digest, viewer, "places", coverPin.PlaceID, cover.ID, cover.SourceVersion, "")
	if e != nil || legacymedia.Hash(binary.Bytes) != cover.SHA256 {
		t.Fatal("current cover real bytes", e)
	}
	clear(binary.Bytes)
	other, otherDigest, otherSession := newReader("map170-other-viewer")
	if _, e = store.ReadHumanMapContentBinary(ctx, otherDigest, other, "moments", mid, asset.ID, asset.SourceVersion, ""); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("selector was not actor bound", e)
	}
	if _, e = f.Pool.Exec(ctx, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, viewer.ID, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanMapContentMoment(ctx, digest, viewer, mid); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("block ignored", e)
	}
	if e = detail.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("late body survived block")
	}
	if _, e = f.Pool.Exec(ctx, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, viewer.ID, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	ownerDigest := sha256.Sum256([]byte("map170-current-owner"))
	if _, e = f.Pool.Exec(ctx, `WITH cl AS(SELECT clock_timestamp() at)INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl`, owner, ownerDigest[:]); e != nil {
		t.Fatal(e)
	}
	ownerActor := identity.Actor{ID: owner, AccountType: "person"}
	if own, e := store.ReadHumanMapContentMoment(ctx, ownerDigest, ownerActor, privateMID); e != nil || !own.CanEdit || own.VisibilitySource != "SELF_PRIVATE" {
		t.Fatal("real owner private reader", e)
	}
	newOp, _ := legacyMigrationUUID()
	preview, e := store.PreviewHumanPrivateImage(ctx, ownerDigest, ownerActor, mid, media.PrivateImageInput{OperationID: newOp, MomentRevision: 1, MIME: "image/jpeg", ByteSize: int64(len(raw)), SHA256: legacymedia.Hash(raw), PixelRisk: "UNKNOWN", Purpose: media.PrivateImagePurpose})
	if e != nil {
		t.Fatal("later ordinary private image preview", e)
	}
	if _, e = store.SaveHumanPrivateImage(ctx, ownerDigest, ownerActor, mid, preview.ID, "image/jpeg", raw); e != nil {
		t.Fatal("later ordinary private image save", e)
	}
	var extraPID string
	if e = f.Pool.QueryRow(ctx, `SELECT place_id FROM legacy_map_place_facts WHERE source_place_id='17000000-0000-4000-8000-000000000043'`).Scan(&extraPID); e != nil {
		t.Fatal(e)
	}
	newLink, _ := legacyMigrationUUID()
	if _, e = f.Pool.Exec(ctx, `INSERT INTO moment_location_links(id,moment_id,kind,place_id,precision,position)VALUES($1,$2,'PLACE',$3,'place',2)`, newLink, mid, extraPID); e != nil {
		t.Fatal(e)
	}
	foreignUnchanged, e := store.ReadHumanMapContentMoment(ctx, digest, viewer, mid)
	if e != nil || len(foreignUnchanged.Media) != 2 || len(foreignUnchanged.Locations) != 2 {
		t.Fatal("original public grant expanded to later private image/location", e)
	}
	for _, x := range foreignUnchanged.Media {
		if x.ID == preview.ID {
			t.Fatal("later private upload exposed by original public parent")
		}
	}
	beforeDelete, e := store.ReadHumanMapContentPlace(ctx, digest, viewer, plan.Places[0].TargetID, "")
	if e != nil || len(beforeDelete.Items) != 2 || len(beforeDelete.Items[0].Media) != 2 {
		t.Fatal("current original-public page before independent image delete", e)
	}
	deletedImage := beforeDelete.Items[0].Media[1]
	if e = store.DeleteHumanPrivateImage(ctx, ownerDigest, ownerActor, mid, deletedImage.ID, deletedImage.Revision); e != nil {
		t.Fatal("real owner independent imported image deletion", e)
	}
	var unchangedRevision bool
	if e = f.Pool.QueryRow(ctx, `SELECT revision=1 FROM moments WHERE id=$1`, mid).Scan(&unchangedRevision); e != nil || !unchangedRevision {
		t.Fatal("independent image delete changed parent fixture", e)
	}
	if e = beforeDelete.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("old full page proof survived independent image deletion")
	}
	if e = originalBinaryProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("metadata-only binary proof survived independent image deletion")
	}
	afterDelete, e := store.ReadHumanMapContentPlace(ctx, digest, viewer, plan.Places[0].TargetID, "")
	if e != nil || len(afterDelete.Items) != 2 || len(afterDelete.Items[0].Media) != 1 || afterDelete.SourceVersion == beforeDelete.SourceVersion {
		t.Fatal("media source fact absent from current page", e)
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, deletedImage.ID, deletedImage.SourceVersion, ""); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("deleted original-public image selector remained live", e)
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE moments SET body='only current owner text',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanMapContentMoment(ctx, digest, viewer, mid); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("edited draft retained original public visibility", e)
	}
	if own, e := store.ReadHumanMapContentMoment(ctx, ownerDigest, ownerActor, mid); e != nil || own.Body != "only current owner text" || len(own.Media) != 2 {
		t.Fatal("owner current edit/media inaccessible", e)
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE moments SET status='withdrawn',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, asset.ID, asset.SourceVersion, ""); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("withdrawn old bytes resurrected", e)
	}
	// Original Civu none precision is readable under its original source grant,
	// but a later native publication must still honor current hidden precision.
	var originalPlaces []string
	for _, p := range plan.Places {
		if p.TargetID != extraPID {
			originalPlaces = append(originalPlaces, p.TargetID)
		}
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE places SET city_id='aberdeen-gb',publication_status='published',coordinate_system='wgs84',location_precision='point',latitude=57.15,longitude=-2.1 WHERE id=ANY($1::uuid[])`, originalPlaces); e != nil {
		t.Fatal(e)
	}
	linkRows, e := f.Pool.Query(ctx, `SELECT id,place_id FROM moment_location_links WHERE moment_id=$1 AND status='ACTIVE' ORDER BY position`, onlyMID)
	if e != nil {
		t.Fatal(e)
	}
	hiddenLinks := []content.MomentLocationInput{}
	for linkRows.Next() {
		l := content.MomentLocationInput{Kind: "PLACE", Precision: "none"}
		if e = linkRows.Scan(&l.ID, &l.PlaceID); e != nil {
			t.Fatal(e)
		}
		hiddenLinks = append(hiddenLinks, l)
	}
	linkRows.Close()
	if len(hiddenLinks) != 2 {
		t.Fatal("only-note hidden original relations missing")
	}
	ownerControl := &savedPageNativeFixture{ctx: ctx, pool: f.Pool, store: store, actor: ownerActor, digest: ownerDigest}
	hiddenInput := content.MomentInput{Body: only.Body, TimePrecision: "unknown", LocationPrecision: "none", Locations: &hiddenLinks}
	mc144Write(t, ownerControl, content.MomentControlInput{OperationID: func() string { id, _ := legacyMigrationUUID(); return id }(), Action: "PUBLISH", MomentID: onlyMID, ExpectedRevision: 1, Input: &hiddenInput})
	nativeHidden, e := store.ReadHumanMapContentMoment(ctx, digest, viewer, onlyMID)
	if e != nil || nativeHidden.VisibilitySource != "BIRDTIE_PUBLIC_MOMENT" || len(nativeHidden.Locations) != 0 || nativeHidden.Body != only.Body {
		t.Fatal("native publish expanded original precision-none locations", e)
	}
	// Reuse existing native publication/copy actions in this same child to test
	// current City and borrowed-source withdrawal, rather than inventing grants.
	cf := &collectionPublicationNativeFixture{t: t, ctx: ctx, pool: f.Pool, s: store, owner: other, peer: viewer, digest: otherDigest, peerDigest: digest, session: otherSession, peerSession: session}
	for i := 0; i < 3; i++ {
		pid := cf.uuid()
		cf.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,coordinate_system,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,'aberdeen-gb','native city-bound public point','park',57.15,-2.16,'wgs84','point','published','LOCAL_SYNTHETIC_FIXTURE','disposable://map170-current-city','synthetic')`, pid)
		cf.places = append(cf.places, pid)
	}
	control := &savedPageNativeFixture{ctx: ctx, pool: f.Pool, store: store, actor: other, digest: otherDigest, session: otherSession}
	locations := []content.MomentLocationInput{{ID: cf.uuid(), Kind: "PLACE", PlaceID: cf.places[0], Precision: "place"}}
	nativeInput := content.MomentInput{Body: "native public body independent of expired City", TimePrecision: "unknown", LocationPrecision: "none", Locations: &locations}
	nativeDraft, _ := mc144Write(t, control, content.MomentControlInput{OperationID: cf.uuid(), Action: "CREATE", Input: &nativeInput})
	nativePublished, _ := mc144Write(t, control, content.MomentControlInput{OperationID: cf.uuid(), Action: "PUBLISH", MomentID: nativeDraft.MomentID, ExpectedRevision: 1, Input: &nativeInput})
	cityDetail, e := store.ReadHumanMapContentMoment(ctx, digest, viewer, nativePublished.MomentID)
	if e != nil || len(cityDetail.Locations) != 1 {
		t.Fatal("current native public location", e)
	}
	cf.exec(`UPDATE cities SET publication_status='hidden' WHERE id='aberdeen-gb'`)
	if e = cityDetail.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("public location source proof survived City withdrawal")
	}
	cityRetired, e := store.ReadHumanMapContentMoment(ctx, digest, viewer, nativePublished.MomentID)
	if e != nil || cityRetired.Body != nativeInput.Body || len(cityRetired.Locations) != 0 {
		t.Fatal("City withdrawal must retire location without retiring body", e)
	}
	cf.exec(`UPDATE cities SET publication_status='published' WHERE id='aberdeen-gb'`)
	source, publishInput := cf.draft("theme_map")
	publishInput.AllowPrivatePlaceCopy = true
	publishInput.Entries[0].LocationVisibility = "place"
	source, e = store.PublishHumanCollection(ctx, otherDigest, other, "theme_map", source.ID, publishInput)
	if e != nil {
		t.Fatal("real opt-in collection source", e)
	}
	copyIDs := []string{cf.places[0], cf.places[2]}
	copyInput := content.CollectionCopyInput{OperationID: cf.uuid(), PublicationRevision: source.Revision, GrantVersion: 1, OrderedPlaceIDs: copyIDs, OrderedPlaceIDsSHA256: content.CollectionCopyPlacesDigest(copyIDs), Title: "independent private copy"}
	copyInput = collectionCopyNativeApprove(t, cf, source, copyInput)
	copied, e := store.CopyHumanCollection(ctx, digest, viewer, content.CollectionRef{Kind: "theme_map", ID: source.ID}, copyInput)
	if e != nil || copied.DestinationRef == nil {
		t.Fatal("real private borrowed map", e)
	}
	borrowed, e := store.ReadHumanMapContent(ctx, digest, viewer, q)
	if e != nil {
		t.Fatal("borrowed own map", e)
	}
	found := false
	for _, p := range borrowed.Pins {
		if p.PlaceID == cf.places[2] && containsMapString(p.VisibilitySources, "SELF_PRIVATE") {
			found = true
		}
	}
	if !found {
		t.Fatal("current explicit borrowed place not shown")
	}
	if _, e = store.UnpublishHumanCollection(ctx, otherDigest, other, "theme_map", source.ID, content.CollectionUnpublishInput{OperationID: cf.uuid(), Revision: source.Revision}); e != nil {
		t.Fatal("real borrowed source withdrawal", e)
	}
	if e = borrowed.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("old map proof survived borrowed source withdrawal")
	}
	retired, e := store.ReadHumanMapContent(ctx, digest, viewer, q)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range retired.Pins {
		if p.PlaceID == cf.places[2] {
			t.Fatal("withdrawn borrowed Place remained in owner map")
		}
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanMapContent(ctx, digest, viewer, q); e == nil {
		t.Fatal("revoked session map remained readable")
	}
}
