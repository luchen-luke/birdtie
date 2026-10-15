package legacycontext

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"testing"
)

const (
	testRun     = "15610000-0000-4000-8000-000000000001"
	testOwner   = "15610000-0000-4000-8000-000000000002"
	testCountry = "15610000-0000-4000-8000-000000000010"
	testCity    = "15610000-0000-4000-8000-000000000011"
	testPlace   = "15610000-0000-4000-8000-000000000020"
	testPlace2  = "15610000-0000-4000-8000-000000000021"
	testNote    = "15610000-0000-4000-8000-000000000030"
	testDeleted = "15610000-0000-4000-8000-000000000031"
	testMedia   = "15610000-0000-4000-8000-000000000032"
	testTheme   = "15610000-0000-4000-8000-000000000040"
	testStamp   = "2021-02-03T04:05:06+08:00"
)

func fixtureRow(table string, values map[string]any) map[string]any {
	row := map[string]any{}
	for _, key := range legacymigration.ColumnNames(table) {
		row[key] = nil
	}
	for key, value := range values {
		row[key] = value
	}
	return row
}

func fixtureTables() map[string]any {
	tables := map[string]any{}
	for _, table := range legacymigration.Tables() {
		tables[table] = []any{}
	}
	tables["users"] = []any{fixtureRow("users", map[string]any{
		"id": testOwner, "version": json.Number("1"), "status": "active", "data_origin": "user", "display_name": "", "bio": "", "handle": nil,
	})}
	tables["geo_areas"] = []any{
		fixtureRow("geo_areas", map[string]any{"id": testCountry, "kind": "country", "status": "active", "country_code": "CN", "name_en": "Synthetic country"}),
		fixtureRow("geo_areas", map[string]any{"id": testCity, "kind": "city", "status": "active", "parent_id": testCountry, "country_code": "CN", "admin_code": "156001", "name_local": "合成城"}),
	}
	tables["places"] = []any{fixtureRow("places", map[string]any{
		"id": testPlace, "name": "合成地点", "category_code": "museum", "source": "official", "source_url": "https://example.invalid/catalog",
		"admin_area_id": testCity, "status": "active", "admin_status": "active", "lifecycle_status": "active", "lat": json.Number("30.1"), "lng": json.Number("120.2"),
	})}
	tables["notes"] = []any{
		fixtureRow("notes", map[string]any{"id": testNote, "author_id": testOwner, "version": json.Number("8"), "title": "", "caption": "原文保留<标签>", "deleted_at": nil,
			"visibility": "public", "moderation_status": "ok", "destination_id": testCity, "destination_source": "manual", "recorded_start_date": nil,
			"recorded_end_date": "2021-02-03", "cover_url": "SYNTHETIC_PRIVATE_MEDIA_URL"}),
		fixtureRow("notes", map[string]any{"id": testDeleted, "author_id": testOwner, "version": json.Number("2"), "deleted_at": testStamp, "title": "DELETED_MUST_NOT_RESTORE", "caption": "DELETED_PRIVATE_BODY"}),
	}
	tables["note_places"] = []any{fixtureRow("note_places", map[string]any{"note_id": testNote, "place_id": testPlace, "raw_name": "原地点名", "position": json.Number("9")})}
	tables["note_media"] = []any{fixtureRow("note_media", map[string]any{"note_id": testNote, "media_id": testMedia, "position": json.Number("4")})}
	tables["theme_maps"] = []any{fixtureRow("theme_maps", map[string]any{
		"id": testTheme, "owner_id": testOwner, "title": "合成主题", "summary": "原摘要", "visibility": "public", "status": "published", "audit_status": "approved",
		"revision": json.Number("3"), "published_at": nil, "created_at": testStamp, "updated_at": testStamp, "deleted_at": nil, "purge_after": nil,
		"moderation_status": "ok", "commercial_disclosure": "", "topic": "", "review_reason": "", "decision_reason": "", "moderated_by": nil, "moderated_at": nil,
		"operator_created": false, "operator_created_by": nil,
	})}
	tables["theme_map_entries"] = []any{fixtureRow("theme_map_entries", map[string]any{
		"map_id": testTheme, "place_id": testPlace, "position": json.Number("7"), "section": "原分组", "creator_note": "私人说明", "collaboration_disclosure": true,
		"created_at": testStamp, "updated_at": testStamp, "primary_moment_id": testNote,
	})}
	tables["user_saved_places"] = []any{fixtureRow("user_saved_places", map[string]any{"user_id": testOwner, "place_id": testPlace, "created_at": testStamp})}
	return tables
}

func fixtureDocuments(t *testing.T, tables map[string]any) ([]byte, []byte) {
	t.Helper()
	header := map[string]any{"instanceTag": legacymigration.SourceSystem, "container": "birdtie-civu-restore-20261014", "database": "civu_restore", "backupCipherSha256": legacymigration.BackupCipherSHA256, "schemaSha256": legacymigration.SourceSchemaSHA256}
	snapshot, err := legacymigration.Digest(map[string]any{"source": header, "tables": tables})
	if err != nil {
		t.Fatal("synthetic snapshot")
	}
	header["snapshotSha256"] = snapshot
	source, err := legacymigration.Canonical(map[string]any{"schemaVersion": legacymigration.SourceProtocol, "source": header, "capture": map[string]any{"observedAt": "2026-10-14T00:00:00Z", "transactionSnapshot": "100:100:"}, "tables": tables})
	if err != nil {
		t.Fatal("synthetic source")
	}
	source = append(source, '\n')
	counts, hashes := map[string]any{}, map[string]any{}
	for table, rows := range tables {
		counts[table] = len(rows.([]any))
		hashes[table], err = legacymigration.Digest(rows)
		if err != nil {
			t.Fatal("synthetic table hash")
		}
	}
	families := map[string]any{
		"accounts":    []any{"users", "auth_identities"},
		"places":      []any{"cities", "regions", "geo_areas", "places", "place_area_relations", "place_provider_refs"},
		"saved":       []any{"user_saved_places", "place_save_sources"},
		"collections": []any{"journeys", "journey_stops", "journey_arrangements", "journey_notes", "journey_members", "journey_media", "journey_publications", "journey_publication_places", "theme_maps", "theme_map_entries", "theme_map_entry_sources"},
		"notes":       []any{"notes", "note_places", "note_media", "note_media_locations", "moment_threads", "moment_thread_legs"},
		"media":       []any{"media_assets", "place_images"},
	}
	sha := sha256.Sum256(source)
	manifest, err := legacymigration.Canonical(map[string]any{"schemaVersion": legacymigration.ManifestProtocol, "source": header,
		"sourceFileSha256": hex.EncodeToString(sha[:]), "auditedSchemaOutputSha256": legacymigration.AuditedOutputSHA256, "counts": counts, "families": families, "tableSha256": hashes,
		"execution": map[string]any{"preservation": "EXTRACTED_ONLY", "targetImport": "NOT_RUN", "identityVerification": "NOT_RUN", "sourceRights": "NOT_RUN", "mediaBytesMatched": "NOT_RUN", "mediaAuthorization": "NOT_RUN", "sessionMigration": "NOT_RUN", "relationshipEffects": 0, "sourceWrites": 0, "runtimeStarted": false}})
	if err != nil {
		t.Fatal("synthetic manifest")
	}
	return source, append(manifest, '\n')
}

func TestLegacyContextStablePrivateTypedProjection(t *testing.T) {
	tables := fixtureTables()
	places := tables["places"].([]any)
	places[0].(map[string]any)["admin_area_id"] = testCountry
	second := fixtureRow("places", map[string]any{"id": testPlace2, "name": "第二合成地点", "category_code": "park", "admin_area_id": testCity, "status": "active", "admin_status": "active", "lifecycle_status": "active"})
	tables["places"] = append(places, second)
	tables["note_places"] = []any{fixtureRow("note_places", map[string]any{"note_id": testNote, "place_id": testPlace, "raw_name": "源一", "position": json.Number("0")}), fixtureRow("note_places", map[string]any{"note_id": testNote, "place_id": testPlace2, "raw_name": "源二", "position": json.Number("1")})}
	tables["media_assets"] = []any{fixtureRow("media_assets", map[string]any{"id": testMedia, "owner_id": testOwner, "kind": "image", "status": "ready", "content_type": "image/jpeg", "size_bytes": json.Number("100")})}
	location := fixtureRow("note_media_locations", map[string]any{"note_id": testNote, "media_id": testMedia, "latitude": json.Number("30.100000000000000001"), "longitude": json.Number("120.2"), "map_enabled": true, "place_ids": []any{testPlace, testPlace2}, "captured_date": "2021-02-03"})
	tables["note_media_locations"] = []any{location}
	thread := "16210000-0000-4000-8000-000000000001"
	deletedThread := "16210000-0000-4000-8000-000000000002"
	notes := tables["notes"].([]any)
	notes[0].(map[string]any)["thread_id"] = thread
	notes[0].(map[string]any)["thread_position"] = json.Number("0")
	notes[0].(map[string]any)["inbound_mode"] = "walking"
	notes[1].(map[string]any)["thread_id"] = deletedThread
	notes[1].(map[string]any)["thread_position"] = json.Number("0")
	tables["moment_threads"] = []any{fixtureRow("moment_threads", map[string]any{"id": thread, "author_id": testOwner, "entry_count": json.Number("1"), "route_revision": json.Number("1"), "created_at": testStamp}), fixtureRow("moment_threads", map[string]any{"id": deletedThread, "author_id": testOwner, "entry_count": json.Number("1"), "route_revision": json.Number("1"), "created_at": testStamp})}
	source, manifest := fixtureDocuments(t, tables)
	before := append([]byte{}, source...)
	p, e := Build(testRun, source, manifest)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.ProjectionSHA256) != 64 {
		t.Fatal("projection digest missing")
	}
	if p.Counts.Places != 1 || p.Counts.SavedItems != 1 || p.Counts.NoteLocations != 2 || p.Counts.MediaContexts != 1 || p.Counts.MediaPlaces != 2 || p.Counts.Threads != 1 || p.Counts.Members != 1 || p.Counts.ThreadTombstones != 1 {
		t.Fatal("typed synthetic counts", p.Counts)
	}
	if p.Places[0].CityID != "" || p.Places[0].Latitude != nil || p.Places[0].Longitude != nil || p.MediaContexts[0].Latitude.String() != "30.100000000000000001" || p.MediaContexts[0].SourceSHA256 != hash(location) {
		t.Fatal("invented geography or changed original numeric/source frame")
	}
	again, e := Build(testRun, source, manifest)
	if e != nil || again.ProjectionSHA256 != p.ProjectionSHA256 || !bytes.Equal(before, source) {
		t.Fatal("identity/projection/source changed", e)
	}
	raw, _ := json.Marshal(p)
	if bytes.Contains(raw, []byte("第二合成地点")) || bytes.Contains(raw, []byte("30.1000")) {
		t.Fatal("diagnostic serialized private records")
	}
}
