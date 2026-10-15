package legacycatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

const testRun = "15600000-0000-4000-8000-000000000001"
const countryID = "15600000-0000-4000-8000-000000000010"
const cityID = "15600000-0000-4000-8000-000000000011"
const districtID = "15600000-0000-4000-8000-000000000012"
const oldCityID = "15600000-0000-4000-8000-000000000013"
const mergedCityID = "15600000-0000-4000-8000-000000000014"
const placeID = "15600000-0000-4000-8000-000000000020"
const unknownPlaceID = "15600000-0000-4000-8000-000000000021"
const mergedPlaceID = "15600000-0000-4000-8000-000000000022"

func baseRow(table string, values row) row {
	r := row{}
	for _, key := range legacymigration.ColumnNames(table) {
		r[key] = nil
	}
	for key, value := range values {
		r[key] = value
	}
	return r
}

func syntheticTables() map[string]any {
	tables := map[string]any{}
	for _, key := range legacymigration.Tables() {
		tables[key] = []any{}
	}
	tables["geo_areas"] = []any{
		baseRow("geo_areas", row{"id": countryID, "kind": "country", "status": "active", "country_code": "CN", "name_en": "Synthetic country"}),
		baseRow("geo_areas", row{"id": cityID, "kind": "city", "status": "active", "parent_id": countryID, "country_code": "CN", "admin_code": "156001", "name_local": "合成城市原名", "artwork_url": "PRIVATE_ARTWORK_MUST_NOT_PROJECT"}),
		baseRow("geo_areas", row{"id": districtID, "kind": "district", "status": "active", "parent_id": cityID, "country_code": "CN"}),
		baseRow("geo_areas", row{"id": mergedCityID, "kind": "city", "status": "merged", "canonical_id": cityID, "country_code": "CN"}),
	}
	tables["cities"] = []any{baseRow("cities", row{"id": oldCityID, "adcode": "156001", "name": "不同旧别名", "level": "prefecture"})}
	tables["places"] = []any{
		baseRow("places", row{"id": placeID, "name": "原合成地点", "category_code": "museum", "description": "原地点说明", "source": "official", "source_url": "https://example.invalid/catalog", "admin_area_id": districtID, "status": "active", "admin_status": "active", "lifecycle_status": "active", "lat": json.Number("30.1"), "lng": json.Number("120.2"), "cover_url": "PRIVATE_COVER_MUST_NOT_PROJECT", "phone": "PRIVATE_PHONE_MUST_NOT_PROJECT"}),
		baseRow("places", row{"id": unknownPlaceID, "name": "不能根据城市名猜归属", "category_code": "museum", "city": "合成城市原名", "status": "active", "admin_status": "active", "lifecycle_status": "active", "lat": json.Number("30.1"), "lng": json.Number("120.2")}),
		baseRow("places", row{"id": mergedPlaceID, "status": "active", "admin_status": "active", "lifecycle_status": "merged", "canonical_place_id": unknownPlaceID}),
	}
	tables["place_provider_refs"] = []any{baseRow("place_provider_refs", row{"provider": "amap", "external_id": "SYNTHETIC_EXACT_PROVIDER", "place_id": placeID, "source_payload": row{"coordinateNormalization": row{"sourceCoordinateSystem": "GCJ-02", "storedCoordinateSystem": "WGS-84", "wgs84Lat": json.Number("30.1"), "wgs84Lng": json.Number("120.2")}}})}
	return tables
}

func documents(t *testing.T, tables map[string]any) ([]byte, []byte) {
	t.Helper()
	header := row{"instanceTag": legacymigration.SourceSystem, "container": "birdtie-civu-restore-20261014", "database": "civu_restore", "backupCipherSha256": legacymigration.BackupCipherSHA256, "schemaSha256": legacymigration.SourceSchemaSHA256}
	snapshot, e := legacymigration.Digest(row{"source": header, "tables": tables})
	if e != nil {
		t.Fatal("synthetic snapshot")
	}
	header["snapshotSha256"] = snapshot
	source, e := legacymigration.Canonical(row{"schemaVersion": legacymigration.SourceProtocol, "source": header, "capture": row{"observedAt": "2026-10-14T00:00:00Z", "transactionSnapshot": "100:100:"}, "tables": tables})
	if e != nil {
		t.Fatal("synthetic source")
	}
	source = append(source, '\n')
	counts, hashes := row{}, row{}
	for key, value := range tables {
		counts[key] = len(value.([]any))
		hashes[key], e = legacymigration.Digest(value)
		if e != nil {
			t.Fatal("synthetic table digest")
		}
	}
	families := row{
		"accounts":    []any{"users", "auth_identities"},
		"places":      []any{"cities", "regions", "geo_areas", "places", "place_area_relations", "place_provider_refs"},
		"saved":       []any{"user_saved_places", "place_save_sources"},
		"collections": []any{"journeys", "journey_stops", "journey_arrangements", "journey_notes", "journey_members", "journey_media", "journey_publications", "journey_publication_places", "theme_maps", "theme_map_entries", "theme_map_entry_sources"},
		"notes":       []any{"notes", "note_places", "note_media", "note_media_locations", "moment_threads", "moment_thread_legs"},
		"media":       []any{"media_assets", "place_images"},
	}
	sha := sha256.Sum256(source)
	manifest, e := legacymigration.Canonical(row{"schemaVersion": legacymigration.ManifestProtocol, "source": header, "sourceFileSha256": hex.EncodeToString(sha[:]), "auditedSchemaOutputSha256": legacymigration.AuditedOutputSHA256, "counts": counts, "families": families, "tableSha256": hashes, "execution": row{"preservation": "EXTRACTED_ONLY", "targetImport": "NOT_RUN", "identityVerification": "NOT_RUN", "sourceRights": "NOT_RUN", "mediaBytesMatched": "NOT_RUN", "mediaAuthorization": "NOT_RUN", "sessionMigration": "NOT_RUN", "relationshipEffects": 0, "sourceWrites": 0, "runtimeStarted": false}})
	if e != nil {
		t.Fatal("synthetic manifest")
	}
	return source, append(manifest, '\n')
}

func TestLegacyCatalogDraftProjectionAndSourceGuards(t *testing.T) {
	source, manifest := documents(t, syntheticTables())
	original := append([]byte(nil), source...)
	p, e := Build(testRun, source, manifest)
	if e != nil {
		t.Fatal("synthetic draft build", e)
	}
	if len(p.Cities) != 1 || len(p.Places) != 1 || p.Counts.PointCoordinates != 1 || p.CityMappings[oldCityID] != "civu-geo-"+cityID || p.CityMappings[districtID] != p.CityMappings[cityID] || p.CityMappings[mergedCityID] != p.CityMappings[cityID] || p.CityMappings[countryID] != "" {
		t.Fatal("exact administrative identities or country boundary lost")
	}
	if p.Cities[0].TimeZone != "Asia/Shanghai" || p.MergedAliases[mergedPlaceID] != unknownPlaceID || p.PlaceMappings[mergedPlaceID] != "" || p.PlaceMappings[unknownPlaceID] != "" || p.Deferred["places_without_city_ancestor"] != 1 {
		t.Fatal("deferred place was guessed, omitted, or aliased to absent native data")
	}
	if *p.Places[0].Latitude != 30.1 || *p.Places[0].Longitude != 120.2 || p.Places[0].TargetID[14] != '5' || p.Places[0].SourceSHA256 == "" || p.ProjectionSHA256 == "" {
		t.Fatal("WGS source was reconverted or deterministic identity/hash missing")
	}
	again, e := Build(testRun, source, manifest)
	if e != nil || again.ProjectionSHA256 != p.ProjectionSHA256 || !bytes.Equal(source, original) {
		t.Fatal("replay changed source or projection")
	}
	wire, _ := json.Marshal(p)
	for _, value := range []string{cityID, placeID, "原合成地点", "PRIVATE_", "原地点说明"} {
		if bytes.Contains(wire, []byte(value)) || strings.Contains(p.String(), value) {
			t.Fatal("ordinary diagnostics expose original records")
		}
	}
	if _, e = Build(testRun, bytes.Replace(source, []byte("原地点说明"), []byte("被改变的说明"), 1), manifest); e == nil {
		t.Fatal("mismatched source frame accepted")
	}
	for _, mode := range []string{"unproven", "conflict", "foreign_provider_label"} {
		tables := syntheticTables()
		r := tables["place_provider_refs"].([]any)[0].(row)
		switch mode {
		case "unproven":
			r["source_payload"] = row{"sourceCoordinateSystem": "WGS-84"}
		case "conflict":
			r["source_payload"] = row{"storedCoordinateSystem": "WGS-84", "wgs84Lat": json.Number("31.1"), "wgs84Lng": json.Number("120.2")}
		case "foreign_provider_label":
			r["provider"] = "openstreetmap"
			r["source_payload"] = row{}
		}
		s, m := documents(t, tables)
		out, err := Build(testRun, s, m)
		if err != nil || len(out.Places) != 1 || out.Places[0].Latitude != nil || out.Places[0].Longitude != nil || out.Counts.PointCoordinates != 0 {
			t.Fatal("unknown/conflicting/provider-only CRS granted point coordinates", mode)
		}
	}
	for _, mutate := range []func(map[string]any){
		func(ts map[string]any) { ts["geo_areas"].([]any)[2].(row)["parent_id"] = districtID },
		func(ts map[string]any) { ts["geo_areas"].([]any)[3].(row)["canonical_id"] = mergedCityID },
		func(ts map[string]any) { ts["places"].([]any)[2].(row)["canonical_place_id"] = mergedPlaceID },
		func(ts map[string]any) { ts["places"] = append(ts["places"].([]any), ts["places"].([]any)[0]) },
	} {
		tables := syntheticTables()
		mutate(tables)
		s, m := documents(t, tables)
		if _, err := Build(testRun, s, m); err == nil {
			t.Fatal("cyclic or duplicate source identity accepted")
		}
	}
	// A broken historical merge remains deferred without manufacturing its
	// destination; valid active catalog identities remain usable.
	tables := syntheticTables()
	unresolved := "15600000-0000-4000-8000-000000000099"
	tables["geo_areas"] = append(tables["geo_areas"].([]any), baseRow("geo_areas", row{"id": unresolved, "kind": "city", "status": "merged", "country_code": "CN"}))
	s, m := documents(t, tables)
	unresolvedPlan, err := Build(testRun, s, m)
	if err != nil || len(unresolvedPlan.Cities) != 1 || unresolvedPlan.CityMappings[unresolved] != "" || unresolvedPlan.Deferred["geo_areas_unresolved_merges"] != 1 {
		t.Fatal("unresolved old merge was guessed or blocked all active cities")
	}
	// Exact persisted Aberdeen identity, not a fuzzy same-name reconciliation.
	tables = syntheticTables()
	tables["places"], tables["cities"], tables["place_provider_refs"] = []any{}, []any{}, []any{}
	const aberdeenID = "34611cd6-62bf-41fb-808f-7010b7b7fef6"
	const parentID = "a1cbc7cc-dbf5-4ad7-99bc-601843a78c0e"
	const gbID = "9d5e11f7-d995-408d-aa2b-32484170973c"
	tables["geo_areas"] = []any{
		baseRow("geo_areas", row{"id": gbID, "kind": "country", "status": "active", "country_code": "GB"}),
		baseRow("geo_areas", row{"id": parentID, "kind": "admin_area", "status": "active", "country_code": "GB", "parent_id": gbID, "stable_key": "admin_area:GB:3e2db8254d32003009ac", "name_local": "Aberdeen City"}),
		baseRow("geo_areas", row{"id": aberdeenID, "kind": "city", "status": "active", "country_code": "GB", "parent_id": parentID, "stable_key": "city:GB:03ae5ad3ce26b7a77719", "name_local": "Aberdeen", "name_en": "Aberdeen", "name_zh": "Aberdeen"}),
	}
	s, m = documents(t, tables)
	reuse, err := Build(testRun, s, m)
	if err != nil || len(reuse.Cities) != 1 || reuse.Cities[0].TargetID != "aberdeen-gb" || !reuse.Cities[0].ReuseExisting || reuse.Cities[0].TimeZone != "Europe/London" {
		t.Fatal("exact existing Aberdeen reuse missing")
	}
	tables["geo_areas"].([]any)[2].(row)["stable_key"] = "different-source-identity"
	s, m = documents(t, tables)
	if _, err = Build(testRun, s, m); err == nil {
		t.Fatal("same-name changed Aberdeen identity accepted")
	}
}

// Explicitly selected source/manifest are read once in RAM; this does not write
// the source, contact a database/provider, or log native records and identities.
func TestLegacyCatalogRealPrepare(t *testing.T) {
	sourcePath, manifestPath := os.Getenv("BIRDTIE_LEGACY_CATALOG_SOURCE"), os.Getenv("BIRDTIE_LEGACY_CATALOG_MANIFEST")
	if sourcePath == "" || manifestPath == "" {
		t.Skip("explicit existing restricted snapshot not selected")
	}
	source, e := os.ReadFile(sourcePath)
	if e != nil {
		t.Fatal("selected restricted source unavailable")
	}
	manifest, e := os.ReadFile(manifestPath)
	if e != nil {
		t.Fatal("selected restricted manifest unavailable")
	}
	p, e := Build("c5df0d7c-f361-4275-9838-feb6d1d92352", source, manifest)
	if e != nil {
		t.Fatal("actual source prepare rejected", e)
	}
	wire, e := json.Marshal(p)
	if e != nil {
		t.Fatal("aggregate summary")
	}
	t.Log(string(wire))
	t.Logf("sourceSHA256=%s manifestSHA256=%s projectionSHA256=%s", p.SourceFileSHA256, p.ManifestFileSHA256, p.ProjectionSHA256)
}
