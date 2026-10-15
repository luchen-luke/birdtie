package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	rm "github.com/birdtie/birdtie/apps/api/internal/legacyreferencemedia"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func referenceMediaFixtureInput(t *testing.T) (LegacyPrivateMediaInput, []byte) {
	t.Helper()
	input, raw := legacyMediaNativeInput(t)
	base, _ := journeyDetailsFixtureSource(t)
	decode := func(raw []byte) map[string]any {
		var v map[string]any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if d.Decode(&v) != nil {
			t.Fatal("synthetic envelope")
		}
		return v
	}
	table := decode(base)["tables"].(map[string]any)
	old := decode(input.Source)["tables"].(map[string]any)
	for _, name := range []string{"media_assets", "note_media"} {
		table[name] = old[name]
	}
	const owner = "14600000-0000-4000-8000-000000000010"
	const note = "14600000-0000-4000-8000-000000000020"
	const deleted = "14600000-0000-4000-8000-000000000021"
	const place = "15600000-0000-4000-8000-000000000003"
	aid := func(n string) string { return "16700000-0000-4000-8000-00000000000" + n }
	row := func(name string, values map[string]any) map[string]any {
		r := map[string]any{}
		for _, k := range legacymigration.ColumnNames(name) {
			r[k] = nil
		}
		for k, v := range values {
			r[k] = v
		}
		return r
	}
	for _, id := range []string{aid("3"), aid("4"), aid("5")} {
		o := any(owner)
		if id == aid("5") {
			o = nil
		}
		table["media_assets"] = append(table["media_assets"].([]any), row("media_assets", map[string]any{"id": id, "owner_id": o, "status": "ready", "kind": "image", "content_type": "image/jpeg", "size_bytes": len(raw), "storage_key": "synthetic-" + id, "public_url": "/source/" + id, "width": json.Number("3"), "height": json.Number("2")}))
	}
	table["note_media"] = append(table["note_media"].([]any), map[string]any{"note_id": deleted, "media_id": aid("4"), "position": json.Number("0")})
	for _, v := range table["notes"].([]any) {
		n := v.(map[string]any)
		if n["id"] == note {
			n["cover_url"] = "/source/" + aid("3")
		}
	}
	table["geo_areas"].([]any)[1].(map[string]any)["artwork_media_id"] = aid("5")
	table["geo_areas"].([]any)[1].(map[string]any)["artwork_version"] = strings.Repeat("a", 64)
	links := []any{}
	for n, s := range []struct{ asset, status string }{{"15700000-0000-4000-8000-000000000011", "approved"}, {aid("3"), "rejected"}, {aid("4"), "approved"}, {"15700000-0000-4000-8000-000000000011", "removed"}} {
		sourceNote := any(nil)
		if n == 2 {
			sourceNote = deleted
		}
		links = append(links, row("place_images", map[string]any{"id": "16710000-0000-4000-8000-00000000000" + string(rune('1'+n)), "place_id": place, "media_id": s.asset, "contributor_id": owner, "image_url": "", "source_type": "user", "rights_basis": "user_owned", "attribution": "Synthetic original attribution", "status": s.status, "is_cover": n == 0, "sort_order": json.Number(string(rune('0' + n))), "source_note_id": sourceNote, "cover_eligible": true, "suggest_cover": false, "review_reason": ""}))
	}
	table["place_images"] = links
	table["journey_media"] = []any{row("journey_media", map[string]any{"id": "16720000-0000-4000-8000-000000000001", "journey_id": "15800000-0000-4000-8000-000000000010", "media_id": aid("3"), "source": "manual", "hidden": false, "publishable": false, "position": json.Number("0")})}
	// The deleted Note's independent approved Place use remains active.
	input.Source, input.Manifest = legacySyntheticInput(t, func(target map[string]any) {
		for k, v := range table {
			target[k] = v
		}
	})
	matcher := decode(input.Matched)
	fresh := decode(input.Source)
	matcher["source"], matcher["sourceFileSha256"] = fresh["source"], legacymedia.Hash(input.Source)
	for _, id := range []string{aid("3"), aid("4"), aid("5")} {
		o := owner
		if id == aid("5") {
			o = ""
		}
		matcher["selectors"] = append(matcher["selectors"].([]any), map[string]any{"sourceMediaId": id, "sourceOwnerId": o, "role": "original", "sourceStatus": "ready", "sourceStorageKey": "synthetic-" + id, "archiveMember": "private/synthetic-" + id, "sourceMIME": "image/jpeg", "observedMIME": "image/jpeg", "sourceBytes": len(raw), "bytes": len(raw), "sha256": legacymedia.Hash(raw), "matched": true, "reasons": []string{"NOT_SCANNED", "RIGHTS_NOT_REVIEWED"}})
	}
	input.Matched, _ = json.Marshal(matcher)
	seal := decode(input.AuthenticatedSeal)
	seal["matcherFileSHA256"] = legacymedia.Hash(input.Matched)
	input.AuthenticatedSeal, _ = json.Marshal(seal)
	input.ExpectedSealSHA256 = legacymedia.Hash(input.AuthenticatedSeal)
	return input, raw
}
func TestReferenceMediaSyntheticSourceKeepsIndependentPurposes(t *testing.T) {
	in, _ := referenceMediaFixtureInput(t)
	p, e := rm.Build(legacyTestRunID, in.Source, in.Manifest, in.Matched, in.AuthenticatedSeal, in.ExpectedSealSHA256)
	if e != nil || p.Counts.PrivateImages != 2 || p.Counts.ReusedImages != 1 || p.Counts.PlaceRelations != 4 || p.Counts.PlaceTombstones != 1 || p.Counts.MomentCovers != 1 || p.Counts.JourneyMedia != 1 || p.Counts.SystemArtworkArchives != 1 {
		t.Fatal("purpose fixture", e, p.Counts)
	}
	for _, r := range p.References {
		if r.SourceNoteID == "14600000-0000-4000-8000-000000000021" && r.Tombstone {
			t.Fatal("deleted Note erased independent approved Place purpose")
		}
		if r.Kind == "JOURNEY_MEDIA" && (r.Publishable == nil || *r.Publishable) {
			t.Fatal("old false publication changed")
		}
	}
}

// Exactly one isolated native chain; no original owner or real private image.
func TestLegacyReferenceMediaNativeOwnedPurposesDeletionAndRepeat(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	in, raw := referenceMediaFixtureInput(t)
	p, e := rm.Build(legacyTestRunID, in.Source, in.Manifest, in.Matched, in.AuthenticatedSeal, in.ExpectedSealSHA256)
	if e != nil {
		t.Fatal("fixture", e)
	}
	if _, e = f.Importer.Apply(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyJourneyDetails(f.Context, legacyTestRunID, in.Source, in.Manifest); e != nil {
		t.Fatal(e)
	}
	original, e := f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, in, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	// Existing test-only SQL diagnostics expose stage/code, never query values.
	f.Importer.database = recoveryDiagnosticDatabase{legacyMigrationDatabase: f.Importer.database, t: t}
	bad := func(context.Context, rm.Asset) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("wrong")), nil
	}
	if _, e = f.Importer.ApplyLegacyReferenceMedia(f.Context, legacyTestRunID, in, bad); !errors.Is(e, ErrLegacyReferenceMediaDerivative) {
		t.Fatal("wrong input accepted", e)
	}
	var clean bool
	if f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM legacy_reference_media_runs) AND NOT EXISTS(SELECT 1 FROM private_reference_image_materials)`).Scan(&clean) != nil || !clean {
		t.Fatal("partial reference media survived")
	}
	first, e := f.Importer.ApplyLegacyReferenceMedia(f.Context, legacyTestRunID, in, func(context.Context, rm.Asset) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil })
	if e != nil || first.Counts != p.Counts || first.Status != "COMMITTED" {
		t.Fatal("reference apply", e)
	}
	var owner, mid string
	if f.Pool.QueryRow(f.Context, `SELECT owner_account_id::text,moment_id::text FROM legacy_private_media_import_items LIMIT 1`).Scan(&owner, &mid) != nil {
		t.Fatal("original owner")
	}
	if f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM sessions) AND(SELECT status='suspended' FROM accounts WHERE id=$1)AND(SELECT bool_and(status='suspended') FROM agents WHERE principal_account_id=$1)AND(SELECT count(*)=1 FROM legacy_system_artwork_archives)AND NOT EXISTS(SELECT 1 FROM legacy_reference_media_assets WHERE system_archive AND(owner_account_id IS NOT NULL OR material_id IS NOT NULL))`, owner).Scan(&clean) != nil || !clean {
		t.Fatal("source ownership/activation altered")
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	token, digest, tokenErr := identity.NewToken()
	if tokenErr != nil {
		t.Fatal("synthetic token")
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 hour')`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	api := httpapi.New(store, store, nil, store, nil, nil, nil, nil, store, nil, nil, nil, false, nil, nil, nil)
	call := func(method, path string, body any, auth bool) *httptest.ResponseRecorder {
		var b io.Reader
		if body != nil {
			encoded, _ := json.Marshal(body)
			b = bytes.NewReader(encoded)
		}
		r := httptest.NewRequest(method, path, b)
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		return w
	}
	path := "/v1/me/reference-images?targetType=MOMENT&targetId=" + mid
	if w := call("GET", path, nil, false); w.Code != 401 {
		t.Fatal("anonymous media", w.Code)
	}
	w := call("GET", path, nil, true)
	if w.Code != 200 {
		t.Fatal("registered list", w.Code, w.Body.String())
	}
	var page struct {
		Data content.ReferenceImagePage `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Data.Items) != 1 {
		t.Fatal("cover role lost")
	}
	item := page.Data.Items[0]
	w = call("GET", item.ContentPath, nil, true)
	if w.Code != 200 || legacymedia.Hash(w.Body.Bytes()) != item.SHA256 || bytes.Contains(w.Body.Bytes(), []byte("SYNTHETIC_RAW_METADATA")) {
		t.Fatal("private full content", w.Code)
	}
	// Approved Place media whose old Note is deleted is an independent use.
	var place string
	if f.Pool.QueryRow(f.Context, `SELECT target_id::text FROM legacy_reference_media_relations WHERE kind='PLACE_IMAGE' LIMIT 1`).Scan(&place) != nil {
		t.Fatal("place target")
	}
	pp, e := store.ReadHumanReferenceImages(f.Context, digest, actor, content.ReferenceImageQuery{TargetType: "PLACE", TargetID: place})
	if e != nil || len(pp.Items) != 3 {
		t.Fatal("place roles", e, len(pp.Items))
	}
	rejected := 0
	for _, i := range pp.Items {
		if i.SourceReviewStatus == "rejected" {
			rejected++
		}
		if i.CanPublish || i.CanCopy {
			t.Fatal("old review became publication")
		}
	}
	if rejected != 1 {
		t.Fatal("rejected source fact hidden or reapproved")
	}
	op, _ := legacyMigrationUUID()
	request := content.ReferenceImageDelete{OperationID: op, ExpectedRevision: item.Revision, ExpectedSourceVersion: item.SourceVersion}
	w = call("DELETE", "/v1/me/reference-images/"+item.ID, request, true)
	if w.Code != 200 {
		t.Fatal("delete purpose", w.Code, w.Body.String())
	}
	if w = call("GET", item.ContentPath, nil, true); w.Code == 200 {
		t.Fatal("deleted cover readable")
	}
	if w = call("GET", "/v1/me/reference-image-operations/"+op, nil, true); w.Code != 200 {
		t.Fatal("original operation unreachable", w.Code)
	}
	repeated, e := f.Importer.ApplyLegacyReferenceMedia(f.Context, legacyTestRunID, in, func(context.Context, rm.Asset) (io.ReadCloser, error) {
		t.Fatal("replay reopened source")
		return nil, nil
	})
	if e != nil || !reflect.DeepEqual(first, repeated) {
		t.Fatal("replay changed original fact", e)
	}
	inspected, e := f.Importer.InspectLegacyReferenceMedia(f.Context, legacyTestRunID)
	if e != nil || !reflect.DeepEqual(first, inspected) {
		t.Fatal("inspect changed facts", e)
	}
	old, e := f.Importer.InspectLegacyPrivateMedia(f.Context, legacyTestRunID)
	if e != nil || !reflect.DeepEqual(old, original) {
		t.Fatal("original157 changed", e)
	}
	// Other purposes of the same new physical image and both old Note images live.
	if f.Pool.QueryRow(f.Context, `SELECT(SELECT count(*)=2 FROM private_moment_images WHERE status='ready_private')AND(SELECT status='READY_PRIVATE' FROM private_reference_image_materials WHERE id=(SELECT material_id FROM private_reference_image_links WHERE id=$1))`, item.ID).Scan(&clean) != nil || !clean {
		t.Fatal("delete erased shared material")
	}
	stale := pp
	if _, e = f.Pool.Exec(f.Context, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:]); e != nil {
		t.Fatal(e)
	}
	if stale.ResponseProof.RevalidateHumanControl(f.Context) == nil {
		t.Fatal("revoked Session retained source")
	}
	if w = call("GET", path, nil, true); w.Code != 401 {
		t.Fatal("revoked session", w.Code)
	}
}
