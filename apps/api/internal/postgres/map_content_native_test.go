package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacycontext"
	"github.com/birdtie/birdtie/apps/api/internal/legacymapanchor"
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

func map170SyntheticInput(t *testing.T, anchors ...bool) (LegacyPrivateMediaInput, []byte) {
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
		if len(anchors) > 0 && anchors[0] {
			for _, v := range tables["notes"].([]any) {
				r := v.(map[string]any)
				if r["id"] == "14600000-0000-4000-8000-000000000020" {
					r["media_locations_set"] = true
				}
				if r["id"] == onlyID {
					r["map_anchor_lat"] = json.Number("57.18")
					r["map_anchor_lng"] = json.Number("-2.2")
					r["map_anchor_precision"] = "exact"
					r["map_anchor_source"] = "manual"
					r["media_locations_set"] = false
				}
			}
			kept := []any{}
			for _, v := range tables["note_places"].([]any) {
				if v.(map[string]any)["note_id"] != onlyID {
					kept = append(kept, v)
				}
			}
			tables["note_places"] = kept
			for _, v := range tables["note_media_locations"].([]any) {
				r := v.(map[string]any)
				if r["note_id"] == "14600000-0000-4000-8000-000000000020" {
					r["map_enabled"] = true
					r["latitude"] = json.Number("57.19")
					r["longitude"] = json.Number("-2.21")
					r["place_ids"] = []any{}
				}
			}
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
	q := content.MapContentQuery{CityID: "aberdeen-gb", Zoom: 16, Bounds: content.MapContentBounds{West: -2.4, South: 57, East: -1.9, North: 57.3}}
	spatialRows, spatialErr := f.Pool.Query(ctx, mapContentSpatialSQL(), viewer.ID, q.Bounds.West, q.Bounds.South, q.Bounds.East, q.Bounds.North)
	if spatialErr != nil {
		t.Fatal("synthetic spatial SQL", spatialErr)
	}
	spatialRows.Close()
	started := time.Now()
	page, e := store.ReadHumanMapContent(ctx, digest, viewer, q)
	if e != nil || !page.Valid(time.Now()) || len(page.Pins) != 2 {
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
		if p.CityID != nil || p.MomentCount != 2 || p.Cover == nil || !containsMapString(p.VisibilitySources, "CIVU_PUBLIC_MOMENT") {
			t.Fatal("NULL city, grouped count or original-public image absent", p.CityID, p.MomentCount, p.Cover != nil, p.VisibilitySources)
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
	// Eager reading details carry a display selector, not an original/empty
	// selector. Follow that declared path while retaining exact actor/source
	// binding and the original-variant negative below.
	mediaVariant := func(asset content.MapContentMedia) string {
		t.Helper()
		u, err := url.Parse(asset.ContentPath)
		if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" ||
			u.Path != fmt.Sprintf("/v1/me/map-content/moments/%s/media/%s/content", mid, asset.ID) ||
			len(u.Query()) != 2 || u.Query().Get("sourceVersion") != asset.SourceVersion ||
			u.Query().Get("variant") != "display" {
			t.Fatal("current display path/source contract")
		}
		return u.Query().Get("variant")
	}
	asset := detail.Media[0]
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, asset.ID, asset.SourceVersion, ""); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("display source accepted as original/empty variant", e)
	}
	binary, e := store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, asset.ID, asset.SourceVersion, mediaVariant(asset))
	if e != nil || len(binary.Bytes) != asset.ByteCount || legacymedia.Hash(binary.Bytes) != asset.SHA256 {
		t.Fatal("source-public declared display bytes", e)
	}
	if e = binary.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("binary metadata-only output proof", e)
	}
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
	binary, e = store.ReadHumanMapContentBinary(ctx, digest, viewer, "places", coverPin.PlaceID, cover.ID, cover.SourceVersion, "marker")
	if e != nil || legacymedia.Hash(binary.Bytes) != cover.SHA256 {
		t.Fatal("current cover real bytes", e)
	}
	clear(binary.Bytes)
	other, otherDigest, otherSession := newReader("map170-other-viewer")
	if _, e = store.ReadHumanMapContentBinary(ctx, otherDigest, other, "moments", mid, asset.ID, asset.SourceVersion, mediaVariant(asset)); !errors.Is(e, content.ErrNotFound) {
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
	// Establish this selector after the intentional location mutation above,
	// so the assertion isolates the unrelated attachment deletion only.
	unchangedAsset := beforeDelete.Items[0].Media[0]
	unchangedBinary, e := store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, unchangedAsset.ID, unchangedAsset.SourceVersion, mediaVariant(unchangedAsset))
	if e != nil {
		t.Fatal("current exact media selector before deletion", e)
	}
	originalBinaryProof := unchangedBinary.ResponseProof
	clear(unchangedBinary.Bytes)
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
	if e = originalBinaryProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("unchanged exact media selector invalidated by a different attachment deletion", e)
	}
	afterDelete, e := store.ReadHumanMapContentPlace(ctx, digest, viewer, plan.Places[0].TargetID, "")
	if e != nil || len(afterDelete.Items) != 2 || len(afterDelete.Items[0].Media) != 1 || afterDelete.SourceVersion == beforeDelete.SourceVersion {
		t.Fatal("media source fact absent from current page", e)
	}
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, deletedImage.ID, deletedImage.SourceVersion, mediaVariant(deletedImage)); !errors.Is(e, content.ErrNotFound) {
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
	if _, e = store.ReadHumanMapContentBinary(ctx, digest, viewer, "moments", mid, asset.ID, asset.SourceVersion, mediaVariant(asset)); !errors.Is(e, content.ErrNotFound) {
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
	if found {
		t.Fatal("unselected collection-only place polluted the default content layer")
	}
	if _, e = store.UnpublishHumanCollection(ctx, otherDigest, other, "theme_map", source.ID, content.CollectionUnpublishInput{OperationID: cf.uuid(), Revision: source.Revision}); e != nil {
		t.Fatal("real borrowed source withdrawal", e)
	}
	if e = borrowed.ResponseProof.RevalidateHumanControl(ctx); e != nil {
		t.Fatal("unselected collection withdrawal changed unrelated default content", e)
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

func TestMapContent171SpatialBudgetCompleteAndStable(t *testing.T) {
	// Distinct source coordinates remain distinct at every zoom. The wire
	// response pages groups rather than inventing a shared location.
	city := []mapSpatialLeaf{
		{pin: content.MapContentPin{Anchor: content.MapContentAnchor{Latitude: 57.15, Longitude: -2.14}}},
		{pin: content.MapContentPin{Anchor: content.MapContentAnchor{Latitude: 57.15, Longitude: -2.07}}},
	}
	for _, zoom := range []float64{0, 5, 12, 13, 19} {
		if len(mapSpatialGroups(city, zoom)) != 2 {
			t.Fatal("zoom collapsed distinct source positions", zoom)
		}
	}
	for _, n := range []int{100, 1000, 10000} {
		leaves := make([]mapSpatialLeaf, n)
		for i := range leaves {
			id := fmt.Sprintf("17100000-0000-4000-8000-%012d", i+1)
			leaves[i] = mapSpatialLeaf{pin: content.MapContentPin{MarkerID: "anchor:" + id, Anchor: content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: -70 + float64(i%97)*1.4, Longitude: -179 + float64(i%233)*1.53}}, moments: map[string]content.MapContentMomentSelector{id: {ID: id}}}
		}
		start := time.Now()
		groups := mapSpatialGroups(leaves, 19)
		elapsed := time.Since(start)
		if len(groups) != n {
			t.Fatalf("%d distinct positions merged into %d groups", n, len(groups))
		}
		seen := map[int]bool{}
		for _, g := range groups {
			for _, i := range g {
				if seen[i] {
					t.Fatal("candidate repeated")
				}
				seen[i] = true
			}
			pages := 0
			for start := 0; start < len(g); start += 40 {
				pages++
				if len(g[start:min(start+40, len(g))]) > 40 {
					t.Fatal("unbounded member page")
				}
			}
			if pages < 1 {
				t.Fatal("empty cluster")
			}
		}
		if len(seen) != n {
			t.Fatal("budget silently discarded candidates", n, len(seen))
		}
		if !reflect.DeepEqual(groups, mapSpatialGroups(leaves, 19)) {
			t.Fatal("unstable grouping")
		}
		t.Logf("synthetic candidates=%d markers=%d aggregationMicros=%d completeLeaves=%d", n, len(groups), elapsed.Microseconds(), len(seen))
	}
	q := content.MapContentQuery{Bounds: content.MapContentBounds{West: 179, South: -5, East: -179, North: 5}, Zoom: 5}
	if !q.Valid() {
		t.Fatal("date line rejected")
	}
	path := mapMembersPath(q, "cluster:"+strings.Repeat("a", 64), strings.Repeat("b", 64), "next")
	u, e := url.Parse(path)
	if e != nil || u.Query().Get("west") != "179" || u.Query().Get("east") != "-179" || u.Query().Get("cursor") != "next" {
		t.Fatal("member selector lost crossing scope")
	}
	sql := mapContentSpatialSQL()
	for _, required := range []string{"p.latitude BETWEEN $3 AND $5", "f.latitude BETWEEN $3 AND $5", "m.id IN(SELECT mid FROM raw_ids)", "vm.source<>'CIVU_PUBLIC_MOMENT' OR relations.original_relation", "NOT relations.private_context AND relations.native_public_relation", "birdtie_legacy_map_moment_binding(m)=a.native_binding", "ctx.original_map_enabled", "l.precision='place'", "$2::float8>$4::float8"} {
		if !strings.Contains(sql, required) {
			t.Fatal("missing spatial/current authority clause", required)
		}
	}
	if strings.Contains(sql, "LIMIT 10001") {
		t.Fatal("global prefix truncation remains")
	}
	if (content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: math.Inf(1)}).Valid() {
		t.Fatal("infinite location accepted")
	}
}

func TestMapContent171AnchorPlanUsesOnlyExplicitInputs(t *testing.T) {
	input, _ := map170SyntheticInput(t, true)
	plan, e := legacymapanchor.Build(legacyTestRunID, input.Source, input.Manifest)
	if e != nil || plan.Counts.NoteAnchors != 1 || plan.Counts.MediaAnchors < 1 {
		t.Fatal("explicit note/media anchor plan", e, plan.Counts)
	}
	ids := map[string]bool{}
	for _, a := range plan.Anchors {
		if ids[a.ID] || a.MomentID == "" || a.Latitude == 0 || a.Longitude == 0 {
			t.Fatal("anchor identity or location inferred")
		}
		ids[a.ID] = true
		if a.ContextID != "" && a.SourceMediaID == "" {
			t.Fatal("media correspondence lost")
		}
	}
	again, e := legacymapanchor.Build(legacyTestRunID, input.Source, input.Manifest)
	if e != nil || !reflect.DeepEqual(again, plan) {
		t.Fatal("unstable import replay")
	}
	altered := append([]byte(nil), input.Source...)
	altered[len(altered)/2] ^= 1
	if _, e = legacymapanchor.Build(legacyTestRunID, altered, input.Manifest); e == nil {
		t.Fatal("unverified backup mutation accepted")
	}
}

// Root executes this against a disposable marked PostgreSQL child. All inputs
// are synthetic; no live user's geographic facts or account are involved.
func TestMapContent171NativeCoordinateAnchorsAndRetirement(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	input, raw := map170SyntheticInput(t, true)
	ctx := f.Context
	for _, apply := range []func() error{
		func() error { _, e := f.Importer.Apply(ctx, legacyTestRunID, input.Source, input.Manifest); return e },
		func() error {
			_, e := f.Importer.ApplyPersonalArchives(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyNativeAdaptation(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyPrivateMedia(ctx, legacyTestRunID, input, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(raw)), nil
			})
			return e
		},
		func() error {
			_, e := f.Importer.ApplyContextAdaptation(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyMapRestore(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
		func() error {
			_, e := f.Importer.ApplyLegacyMapAnchorRestore(ctx, legacyTestRunID, input.Source, input.Manifest)
			return e
		},
	} {
		if e := apply(); e != nil {
			t.Fatal("synthetic anchor adaptation", e)
		}
	}
	again, e := f.Importer.ApplyLegacyMapAnchorRestore(ctx, legacyTestRunID, input.Source, input.Manifest)
	if e != nil || again.Counts.NoteAnchors != 1 {
		t.Fatal("anchor replay", e)
	}
	var viewer, session string
	viewer, _ = legacyMigrationUUID()
	digest := sha256.Sum256([]byte("map171-independent-reader"))
	if _, e = f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, viewer); e != nil {
		t.Fatal(e)
	}
	if e = f.Pool.QueryRow(ctx, `WITH cl AS(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl RETURNING id`, viewer, digest[:]).Scan(&session); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: viewer, AccountType: "person"}
	q := content.MapContentQuery{CityID: "aberdeen-gb", Bounds: content.MapContentBounds{West: -2.3, South: 57.17, East: -2.19, North: 57.21}, Zoom: 17}
	spatialRows, spatialErr := f.Pool.Query(ctx, mapContentSpatialSQL(), viewer, q.Bounds.West, q.Bounds.South, q.Bounds.East, q.Bounds.North)
	if spatialErr != nil {
		t.Fatal("synthetic spatial SQL", spatialErr)
	}
	spatialRows.Close()
	page, e := store.ReadHumanMapContent(ctx, digest, actor, q)
	if e != nil || len(page.Pins) < 2 {
		t.Fatal("unplaced coordinates not visible", e, len(page.Pins))
	}
	var mediaPin content.MapContentPin
	for _, p := range page.Pins {
		if p.Kind != "MOMENT_ANCHOR" || p.PlaceID != "" || p.EntryID != p.MomentID || p.AnchorID == "" || p.MomentCount != 1 {
			t.Fatal("anchor became POI or wrong entry")
		}
		if p.Cover != nil {
			mediaPin = p
			u, _ := url.Parse(p.Cover.ContentPath)
			b, e := store.ReadHumanMapContentBinary(ctx, digest, actor, "moments", p.MomentID, p.Cover.ID, p.Cover.SourceVersion, u.Query().Get("variant"))
			if e != nil || len(b.Bytes) != p.Cover.ByteCount || p.Cover.Width > 160 || p.Cover.Height > 160 {
				t.Fatal("real bounded marker thumbnail", e)
			}
			clear(b.Bytes)
		}
		detail, e := store.ReadHumanMapContentMoment(ctx, digest, actor, p.MomentID)
		if e != nil || detail.ID != p.EntryID {
			t.Fatal("anchor opened wrong child", e)
		}
		if detail.Author == nil || detail.Author.DisplayName != "Birdtie 成员" || detail.Author.Avatar != nil || detail.PublishedAt != nil {
			t.Fatal("legacy visible content disclosed private author or invented publication")
		}
		if detail.CreatedAt.Year() == 2020 {
			if detail.CreatedAtSource != "CIVU_ORIGINAL" {
				t.Fatal("original legacy creation time not identified")
			}
		} else if detail.CreatedAtSource != "UNKNOWN" {
			t.Fatal("import fallback presented as original creation", detail.CreatedAtSource)
		}

	}
	if mediaPin.Cover == nil {
		t.Fatal("matching anchor media cover absent")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE private_moment_media_contexts SET status='REMOVED',revision=revision+1 WHERE id=(SELECT context_id FROM legacy_map_anchor_facts WHERE id=$1)`, mediaPin.AnchorID); e != nil {
		t.Fatal(e)
	}
	if e = page.ResponseProof.RevalidateHumanControl(ctx); e == nil {
		t.Fatal("revoked coordinate survived old map proof")
	}
	fresh, e := store.ReadHumanMapContent(ctx, digest, actor, q)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range fresh.Pins {
		if p.AnchorID == mediaPin.AnchorID {
			t.Fatal("retired original coordinate reappeared")
		}
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ReadHumanMapContent(ctx, digest, actor, q); e == nil {
		t.Fatal("logged out anchor cache readable")
	}
}

func TestMapContent171MediaVersionsIgnoreViewportAndCoverBudget(t *testing.T) {
	makeLeaf := func(mid string) mapSpatialLeaf {
		return mapSpatialLeaf{pin: content.MapContentPin{SourceVersion: "base"}, moments: map[string]content.MapContentMomentSelector{mid: {ID: mid}}}
	}
	a, b := []mapSpatialLeaf{makeLeaf("one")}, []mapSpatialLeaf{makeLeaf("one"), makeLeaf("two")}
	if e := mapSpatialMediaVersions(a, `[["moment","one","image",1]]`); e != nil {
		t.Fatal(e)
	}
	if e := mapSpatialMediaVersions(b, `[["moment","one","image",1],["moment","two","image",2]]`); e != nil {
		t.Fatal(e)
	}
	if a[0].pin.SourceVersion != b[0].pin.SourceVersion {
		t.Fatal("unrelated viewport media changed a stable leaf")
	}
	c := []mapSpatialLeaf{makeLeaf("one")}
	if e := mapSpatialMediaVersions(c, `[["moment","one","image",2]]`); e != nil {
		t.Fatal(e)
	}
	if a[0].pin.SourceVersion == c[0].pin.SourceVersion {
		t.Fatal("own media mutation did not retire leaf")
	}
}

func TestMapContent171NativeContinuationReauthorizes(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx := f.Context
	owner, _ := legacyMigrationUUID()
	digest := sha256.Sum256([]byte("synthetic-map171-continuation"))
	if _, e := f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `WITH cl AS(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO places(id,name,category_code,latitude,longitude,coordinate_system,location_precision,publication_status,source_label,source_ref,maintainer_label)
SELECT md5('map171-cont-place:'||i)::uuid,'Synthetic continuation point','park',10,20,'wgs84','point','draft','LOCAL_SYNTHETIC_FIXTURE','disposable://map171-continuation','synthetic' FROM generate_series(1,81)i`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO moments(id,author_account_id,title,body,status,visibility,location_precision)
SELECT md5('map171-cont-moment:'||i)::uuid,$1,'','Synthetic continuation content','draft','private','none' FROM generate_series(1,81)i`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(ctx, `INSERT INTO moment_location_links(id,moment_id,kind,place_id,precision,position)
SELECT md5('map171-cont-link:'||i)::uuid,md5('map171-cont-moment:'||i)::uuid,'PLACE',md5('map171-cont-place:'||i)::uuid,'place',0 FROM generate_series(1,81)i`); e != nil {
		t.Fatal(e)
	}
	s := New(f.Pool, true)
	a := identity.Actor{ID: owner, AccountType: "person"}
	q := content.MapContentQuery{Bounds: content.MapContentBounds{West: 19, South: 9, East: 21, North: 11}, Zoom: 0}
	page, e := s.ReadHumanMapContent(ctx, digest, a, q)
	if e != nil || len(page.Pins) != 40 || page.TotalAnchorCount != 81 || page.TotalPointCount != 81 {
		t.Fatal("independent coincident points keep bounded transport", e)
	}
	for _, pin := range page.Pins {
		if pin.Kind != "PLACE_CONTENT" {
			t.Fatal("independent Place replaced by cluster")
		}
	}
	q.SourceVersion = page.SourceVersion
	first, e := s.ReadHumanMapContent(ctx, digest, a, q)
	if e != nil || len(first.Pins) != 40 || first.NextCursor == nil {
		t.Fatal("first", e)
	}
	position, e := decodeJourneyHistoryCursor(*first.NextCursor)
	if e != nil || position.Until.Sub(first.ObservedAt) < 29*time.Minute {
		t.Fatal("position lifetime still content lease", e)
	}
	// A genuinely signed near-expiry position cannot shorten a fresh ACL read.
	position.Until = time.Now().Add(2 * time.Second)
	q.Cursor, e = encodeJourneyHistoryCursor(*position)
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.ReadHumanMapContent(ctx, digest, a, q)
	if e != nil || len(second.Pins) != 40 || second.NextCursor == nil || second.ValidUntil.Sub(second.ObservedAt) < 25*time.Second || second.ValidUntil.Sub(second.ObservedAt) > 30*time.Second {
		t.Fatal("new page not independently authorized", e)
	}
	q.SelectedMarkerID = second.Pins[39].MarkerID
	selected, e := s.ReadHumanMapContent(ctx, digest, a, q)
	if e != nil || selected.SourceVersion != second.SourceVersion {
		t.Fatal("cover selection changed complete source", e)
	}
	for i := range selected.Pins {
		if selected.Pins[i].SourceVersion != second.Pins[i].SourceVersion {
			t.Fatal("cover budget changed leaf source")
		}
	}
	q.Cursor = *second.NextCursor
	last, e := s.ReadHumanMapContent(ctx, digest, a, q)
	if e != nil || len(last.Pins) != 1 || last.NextCursor != nil {
		t.Fatal("terminal position", e)
	}
	position.Until = time.Now().Add(-time.Second)
	q.Cursor, _ = encodeJourneyHistoryCursor(*position)
	if _, e = s.ReadHumanMapContent(ctx, digest, a, q); e == nil {
		t.Fatal("expired position accepted")
	}
	q.Cursor = *second.NextCursor
	if _, e = f.Pool.Exec(ctx, `UPDATE moments SET body=body||' changed',revision=revision+1 WHERE id=md5('map171-cont-moment:1')::uuid`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadHumanMapContent(ctx, digest, a, q); e == nil {
		t.Fatal("changed source resumed")
	}
	if _, e = f.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadHumanMapContent(ctx, digest, a, q); e == nil {
		t.Fatal("revoked session resumed")
	}
}
