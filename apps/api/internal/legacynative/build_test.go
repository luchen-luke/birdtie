package legacynative

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
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

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func TestLegacyNativeVerifiedBuildIsPrivateStableAndSourceBound(t *testing.T) {
	source, manifest := fixtureDocuments(t, fixtureTables())
	before := append([]byte(nil), source...)
	plan, err := Build(testRun, source, manifest)
	if err != nil {
		t.Fatal("verified synthetic native projection rejected", err)
	}
	if plan.Counts.SourceNotes != 2 || plan.Counts.NoteTombstones != 1 || plan.Counts.NativeNotes != 1 || plan.Counts.NativeThemeMaps != 1 || plan.Counts.NativeSavedItems != 1 || plan.Counts.GeneratedTitles != 0 {
		t.Fatal("private subset or tombstone accounting changed")
	}
	note := plan.Notes[0]
	if note.Title != "" || note.GeneratedTitle || note.Visibility != "private" || note.Status != "draft" || note.TimePrecision != "unknown" || note.SourceCreatedAt != nil || note.SourceUpdatedAt != nil || note.RecordedEndDate == nil || !containsReason(note.ResidualReasons, "legacy_media_unavailable") {
		t.Fatal("unknown time, neutral title or honest unavailable media lost")
	}
	base, err := legacymigration.BuildPlan(testRun, source, manifest)
	if err != nil || note.SourceSHA256 != base.Notes[0].SourceSHA256 {
		t.Fatal("note mapping no longer binds exact immutable 146 source hash")
	}
	archives, err := legacycollections.BuildPlan(testRun, source, manifest)
	if err != nil {
		t.Fatal("archive projection")
	}
	for _, archive := range archives.Records {
		if archive.Kind == legacycollections.SavedPlace && plan.SavedItems[0].SourceSHA256 != archive.SourceSHA256 || archive.Kind == legacycollections.ThemeMap && plan.Collections[0].SourceSHA256 != archive.SourceSHA256 {
			t.Fatal("personal mapping no longer binds exact immutable 149 hash")
		}
	}
	collection := plan.Collections[0]
	if collection.Visibility != "private" || collection.Status != "draft" || collection.Entries[0].LocationVisibility != "hidden" || collection.EntrySources[0].SourcePosition != 7 || !strings.Contains(collection.Entries[0].Note, "原分组：原分组") || !strings.Contains(collection.Entries[0].Note, "原说明：私人说明") || !strings.Contains(collection.Entries[0].Note, "原协作披露：是") {
		t.Fatal("section, creator text, disclosure, order or private precision lost")
	}
	again, err := Build("15610000-0000-4000-8000-000000000099", source, manifest)
	if err != nil || again.Notes[0].TargetID != note.TargetID || again.Collections[0].TargetID != collection.TargetID || again.SavedItems[0].TargetID != plan.SavedItems[0].TargetID || again.ProjectionSHA256 != plan.ProjectionSHA256 || !bytes.Equal(source, before) {
		t.Fatal("same source identity duplicated or original bytes mutated")
	}
	for _, residual := range plan.Residuals {
		if residual.SourceKey == testDeleted && (residual.TargetID != "" || !containsReason(residual.Reasons, "source_deleted_tombstone")) {
			t.Fatal("deleted source produced native content")
		}
	}
	wire, _ := json.Marshal(plan)
	for _, secret := range []string{testOwner, testNote, "原文保留", "私人说明", "SYNTHETIC_PRIVATE", "DELETED_PRIVATE_BODY"} {
		if bytes.Contains(wire, []byte(secret)) || strings.Contains(fmt.Sprint(plan), secret) || strings.Contains(fmt.Sprint(note), secret) {
			t.Fatal("ordinary diagnostics expose private projection")
		}
	}
	if _, err := Build(testRun, bytes.Replace(source, []byte("原文保留"), []byte("变更原文"), 1), manifest); err == nil {
		t.Fatal("source changed outside manifest accepted")
	}
}

func noteWithPlaces(t *testing.T, destination *string, selectors []legacyarchive.PlaceSelector) (legacymigration.NotePlan, map[string]any) {
	t.Helper()
	rel := legacyarchive.Relationships{Places: selectors, Media: []legacyarchive.MediaSelector{}, MediaLocations: []legacyarchive.MediaLocation{}, ThreadLegs: []legacyarchive.ThreadLeg{}}
	raw, err := json.Marshal(rel)
	if err != nil {
		t.Fatal("relationships")
	}
	row := map[string]any{"destination_id": nil, "destination_source": "", "created_at": nil, "updated_at": nil}
	if destination != nil {
		row["destination_id"] = *destination
	}
	return legacymigration.NotePlan{SourceID: testNote, OwnerSourceID: testOwner, SourceSHA256: strings.Repeat("a", 64), SourceVersion: 1, Title: "原动态", Body: "原文", Payload: raw}, row
}

func TestLegacyNativeAnchorsDoNotGuessOrCollapsePlaces(t *testing.T) {
	index := catalogIndex{cities: map[string]string{testCity: "known-city"}, places: map[string]legacycatalog.Place{
		testPlace: {TargetID: testPlace, CityID: "known-city"}, testPlace2: {TargetID: testPlace2, CityID: "known-city"},
	}}
	selectors := []legacyarchive.PlaceSelector{{SourcePlaceID: ptr(testPlace), Position: 3}, {SourcePlaceID: ptr(testPlace2), Position: 8}}
	source, row := noteWithPlaces(t, nil, selectors)
	note, reasons, err := buildNote(source, row, index)
	if err != nil || note.TargetID == "" || note.CityID != "known-city" || note.PlaceID != "" || !containsReason(reasons, "legacy_multiple_places_archive_only") {
		t.Fatal("multiple places collapsed or proven common city lost")
	}
	index.places[testPlace2] = legacycatalog.Place{TargetID: testPlace2, CityID: "other-city"}
	note, reasons, err = buildNote(source, row, index)
	if err != nil || note.TargetID == "" || note.CityID != "" || note.LocationPrecision != "none" || !containsReason(reasons, "city_unresolved") {
		t.Fatal("cross-city places guessed a destination")
	}
	row["destination_id"] = testCity
	note, reasons, err = buildNote(source, row, index)
	if err != nil || note.TargetID == "" || note.PlaceID != "" || !containsReason(reasons, "legacy_cross_city_places_archive_only") {
		t.Fatal("explicit destination should retain cross-city residual without choosing a place")
	}
	source, row = noteWithPlaces(t, nil, []legacyarchive.PlaceSelector{{RawName: "不能按自由文本猜城市", Position: 0}})
	note, reasons, err = buildNote(source, row, index)
	if err != nil || note.TargetID == "" || note.CityID != "" || note.LocationPrecision != "none" || !containsReason(reasons, "city_unresolved") {
		t.Fatal("raw place label guessed a city")
	}
}

func ptr(value string) *string { return &value }

func TestLegacyNativeCollectionsDeferComplexRoutesWithoutTruncation(t *testing.T) {
	index := catalogIndex{cities: map[string]string{testCity: "known-city"}, places: map[string]legacycatalog.Place{
		testPlace: {TargetID: testPlace, CityID: "known-city", Name: "原地点"},
	}}
	arrangement := map[string]any{"sourceType": "place", "sourcePlaceId": testPlace, "sourceName": "原地点", "sourcePrivateNote": "原备注", "position": json.Number("8")}
	payload := map[string]any{"stops": []any{}, "arrangements": []any{arrangement}, "notes": []any{}}
	meta := map[string]any{"sourceCityId": testCity, "sourceDays": json.Number("0")}
	entries, sources, city, _, reasons := journeyEntries(payload, meta, index)
	if len(entries) != 1 || sources[0].SourcePosition != 8 || city != "known-city" || len(reasons) != 0 {
		t.Fatal("strict place-only journey subset unexpectedly lost")
	}
	arrangement["sourceTransportMode"] = "rail"
	entries, _, _, _, reasons = journeyEntries(payload, meta, index)
	if len(entries) != 0 || !containsReason(reasons, "legacy_journey_arrangement_requires_schedule_model") {
		t.Fatal("transport arrangement flattened into a place-only route")
	}
	delete(arrangement, "sourceTransportMode")
	payload["arrangements"] = []any{arrangement, map[string]any{"sourceType": "place", "sourcePlaceId": testPlace, "position": json.Number("9")}}
	entries, _, _, _, reasons = journeyEntries(payload, meta, index)
	if len(entries) != 0 || !containsReason(reasons, "legacy_repeated_place_collection") {
		t.Fatal("repeated route visit silently deduplicated")
	}
	theme := map[string]any{"entries": []any{map[string]any{"sourcePlaceId": testPlace, "position": json.Number("0"), "sourceSection": "原组", "sourceCreatorNote": strings.Repeat("文", 700)}}}
	entries, _, _, reasons = themeEntries(theme, index)
	if len(entries) != 0 || !containsReason(reasons, "native_collection_note_limit_exceeded") {
		t.Fatal("overlong section/creator text truncated")
	}
}
