package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"net/http"
	"net/http/httptest"
	"testing"
)

func legacyPlaceReferenceFixtureSource(t *testing.T) ([]byte, []byte) {
	t.Helper()
	base, _ := nativeAdaptationFixtureInput(t)
	var envelope struct {
		Tables map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(base))
	d.UseNumber()
	if d.Decode(&envelope) != nil {
		t.Fatal("synthetic reference fixture")
	}
	return legacySyntheticInput(t, func(tables map[string]any) {
		for key, value := range envelope.Tables {
			tables[key] = value
		}
		row := func(table string, values map[string]any) map[string]any {
			v := map[string]any{}
			for _, key := range legacymigration.ColumnNames(table) {
				v[key] = nil
			}
			for key, value := range values {
				v[key] = value
			}
			return v
		}
		places := tables["places"].([]any)
		for _, id := range []string{"15600000-0000-4000-8000-000000000004", "15600000-0000-4000-8000-000000000005"} {
			copy := map[string]any{}
			for k, v := range places[0].(map[string]any) {
				copy[k] = v
			}
			copy["id"], copy["name"] = id, "Second synthetic retained place"
			places = append(places, copy)
		}
		tables["places"] = places
		const theme = "15600000-0000-4000-8000-000000000040"
		const stamp = "2026-10-13T00:00:00Z"
		tables["theme_maps"] = []any{row("theme_maps", map[string]any{"id": theme, "owner_id": "14600000-0000-4000-8000-000000000010", "title": "Synthetic retained theme", "summary": "Own original summary", "visibility": "private", "status": "draft", "audit_status": "pending", "revision": json.Number("1"), "created_at": stamp, "updated_at": stamp, "moderation_status": "pending", "commercial_disclosure": "", "topic": "", "review_reason": "", "decision_reason": "", "operator_created": false})}
		entries := []any{}
		for i, id := range []string{"15600000-0000-4000-8000-000000000003", "15600000-0000-4000-8000-000000000004"} {
			entries = append(entries, row("theme_map_entries", map[string]any{"map_id": theme, "place_id": id, "position": json.Number(string(rune('1' + i))), "section": "", "creator_note": "Own retained note", "collaboration_disclosure": false, "created_at": stamp, "updated_at": stamp}))
		}
		tables["theme_map_entries"] = entries
	})
}

// One routed chain, exclusively synthetic and confined to its owned child DB.
func TestLegacyPlaceReferenceNativeOwnedReadEditAndRevocation(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	source, manifest := legacyPlaceReferenceFixtureSource(t)
	if _, e := f.Importer.Apply(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("base preservation", e)
	}
	if _, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("personal preservation", e)
	}
	if _, e := f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("native preserved targets", e)
	}
	var owner, savedID, themeID, place, second, unrelated string
	if e := f.Pool.QueryRow(f.Context, `SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user' AND source_id='14600000-0000-4000-8000-000000000010'`).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	for _, query := range []struct {
		sql  string
		dest *string
	}{{`SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='saved_place'`, &savedID}, {`SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='theme_map'`, &themeID}, {`SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='place' AND source_key='15600000-0000-4000-8000-000000000003'`, &place}, {`SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='place' AND source_key='15600000-0000-4000-8000-000000000004'`, &second}, {`SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='place' AND source_key='15600000-0000-4000-8000-000000000005'`, &unrelated}} {
		if e := f.Pool.QueryRow(f.Context, query.sql).Scan(query.dest); e != nil {
			t.Fatal(e)
		}
	}
	uuid := func() string {
		id, e := legacyMigrationUUID()
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, uuid(), owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	peer := uuid()
	peerToken, peerDigest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, peer); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, uuid(), peer, peerDigest[:]); e != nil {
		t.Fatal(e)
	}
	// A second owner's row prevents correlated saved projection aliases from
	// accidentally becoming self comparisons or multi-row scalar subqueries.
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO saved_items(id,owner_account_id,place_id)VALUES($1,$2,$3)`, uuid(), peer, unrelated); e != nil {
		t.Fatal(e)
	}
	s := New(f.Pool, false)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	router := httpapi.New(s, s, nil, s, nil, nil, nil, nil, s, nil, nil, nil, false, nil, nil, nil)
	request := func(method, path string, input any) *httptest.ResponseRecorder {
		var body bytes.Buffer
		if input != nil {
			if json.NewEncoder(&body).Encode(input) != nil {
				t.Fatal("synthetic request")
			}
		}
		r := httptest.NewRequest(method, path, &body)
		r.Header.Set("Authorization", "Bearer "+token)
		if input != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	ref := lpr.Reference{Kind: "saved_place", RecordID: savedID, PlaceID: place}
	path := func(r lpr.Reference) string {
		return "/v1/me/place-references/" + r.PlaceID + "?kind=" + r.Kind + "&recordId=" + r.RecordID
	}
	w := request(http.MethodGet, path(ref), nil)
	var detail struct{ Data lpr.Detail }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Data.Name == "" || detail.Data.Latitude != nil || detail.Data.Longitude != nil || detail.Data.LocationPrecision != "none" {
		t.Fatal("actual owned retained reference route or invented CRS", w.Code)
	}
	original, e := s.ReadHumanLegacyPlaceReference(f.Context, digest, actor, ref)
	if e != nil {
		t.Fatal(e)
	}
	if e = original.ResponseProof.RevalidateHumanControl(f.Context); e != nil {
		t.Fatal("current owned reference proof", e)
	}
	for _, suffix := range []string{"", "?kind=place", "?q=Synthetic"} {
		w = request(http.MethodGet, "/v1/me/saved/page"+suffix, nil)
		var page struct{ Data saved.Page }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Data.Items) != 1 || page.Data.Items[0].LegacyReference == nil || page.Data.Items[0].Available || page.Data.Items[0].Title == "" || page.Data.Items[0].Latitude != nil || page.Data.Items[0].SourceExpiresAt != nil {
			t.Fatal("owner saved projection/search lost private reference", w.Code)
		}
	}
	if w = request(http.MethodGet, "/v1/places/"+place, nil); w.Code != 404 {
		t.Fatal("private legacy reference changed public Place access", w.Code)
	}
	bad := ref
	bad.PlaceID = unrelated
	if w = request(http.MethodGet, path(bad), nil); w.Code != 404 {
		t.Fatal("same-run unrelated draft Place readable", w.Code)
	}
	themeRef := lpr.Reference{Kind: "theme_map", RecordID: themeID, PlaceID: second}
	if w = request(http.MethodGet, path(themeRef), nil); w.Code != 200 {
		t.Fatal("own original theme entry unreadable", w.Code)
	}
	w = request(http.MethodGet, "/v1/me/personal-collections/theme_map/"+themeID, nil)
	var collection struct{ Data content.PersonalCollection }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &collection) != nil || len(collection.Data.Entries) != 2 || collection.Data.Entries[0].LegacyReference == nil || collection.Data.Entries[0].PlaceName == "" || collection.Data.Entries[0].Available || collection.Data.Entries[0].Latitude != nil {
		t.Fatal("theme current private metadata/proof", w.Code)
	}
	edit := func(entries []content.CollectionControlEntry) content.PersonalCollection {
		p := content.CollectionControlInput{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: uuid(), Action: "UPDATE", Kind: "theme_map", CollectionID: themeID, ExpectedRevision: collection.Data.Revision, Input: &content.CollectionControlBody{Kind: "theme_map", CityID: collection.Data.CityID, Title: "Edited own theme", Summary: "Edited own summary", Entries: entries}}
		w = request(http.MethodPost, "/v1/me/personal-collection-controls/preview", p)
		var preview struct {
			Data content.CollectionControlPreview
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil {
			t.Fatal("private retained theme concrete preview", w.Code)
		}
		w = request(http.MethodPut, "/v1/me/personal-collection-controls/operations/"+p.OperationID, content.CollectionControlSubmission{CollectionControlInput: p, Approval: preview.Data.Approval})
		if w.Code != 200 {
			t.Fatal("private retained theme atomic edit", w.Code)
		}
		w = request(http.MethodGet, "/v1/me/personal-collections/theme_map/"+themeID, nil)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &collection) != nil {
			t.Fatal("edited private retained theme reload", w.Code)
		}
		return collection.Data
	}
	changed := edit([]content.CollectionControlEntry{{PlaceID: second, Note: "Own changed second note", LocationVisibility: "hidden"}, {PlaceID: place, Note: "Own changed first note", LocationVisibility: "hidden"}})
	if changed.Entries[0].PlaceID != second || changed.Title != "Edited own theme" {
		t.Fatal("owner text/order did not persist")
	}
	removedProof, e := s.ReadHumanLegacyPlaceReference(f.Context, digest, actor, ref)
	if e != nil {
		t.Fatal(e)
	}
	edit([]content.CollectionControlEntry{{PlaceID: place, Note: "Kept private note", LocationVisibility: "hidden"}})
	if w = request(http.MethodGet, path(themeRef), nil); w.Code != 404 {
		t.Fatal("removed collection entry resurrected", w.Code)
	}
	invalid := content.CollectionControlInput{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: uuid(), Action: "UPDATE", Kind: "theme_map", CollectionID: themeID, ExpectedRevision: collection.Data.Revision, Input: &content.CollectionControlBody{Kind: "theme_map", CityID: collection.Data.CityID, Title: "Own title", Entries: []content.CollectionControlEntry{{PlaceID: place, LocationVisibility: "hidden"}, {PlaceID: second, LocationVisibility: "hidden"}}}}
	if _, e = s.PreviewHumanCollectionControl(f.Context, digest, actor, invalid); e == nil {
		t.Fatal("removed draft entry re-added without current relation")
	}
	if _, e = s.PublishHumanCollection(f.Context, digest, actor, "theme_map", themeID, content.CollectionPublicationInput{OperationID: uuid(), Revision: collection.Data.Revision, Entries: []content.CollectionPublicationEntry{{PlaceID: place, LocationVisibility: "hidden"}}}); e == nil {
		t.Fatal("private retained reference became a publication grant")
	}
	ownerToken := token
	token = peerToken
	if w = request(http.MethodGet, path(ref), nil); w.Code != 404 {
		t.Fatal("foreign owner accessed retained Place", w.Code)
	}
	token = ownerToken
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM saved_items WHERE id=$1 AND owner_account_id=$2`, savedID, owner); e != nil {
		t.Fatal(e)
	}
	if w = request(http.MethodGet, path(ref), nil); w.Code != 404 {
		t.Fatal("removed saved relationship still granted access", w.Code)
	}
	if e = removedProof.ResponseProof.RevalidateHumanControl(f.Context); e == nil {
		t.Fatal("old retained reference proof survived removal")
	}
	kept := lpr.Reference{Kind: "theme_map", RecordID: themeID, PlaceID: place}
	if _, e = f.Pool.Exec(f.Context, `UPDATE theme_maps SET status='withdrawn',revision=revision+1 WHERE id=$1`, themeID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadHumanLegacyPlaceReference(f.Context, digest, actor, kept); !errors.Is(e, lpr.ErrNotFound) {
		t.Fatal("withdrawn collection still granted private reference", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:]); e != nil {
		t.Fatal(e)
	}
	if w = request(http.MethodGet, path(kept), nil); w.Code != 401 {
		t.Fatal("revoked Session borrowed old private reference", w.Code)
	}
}
