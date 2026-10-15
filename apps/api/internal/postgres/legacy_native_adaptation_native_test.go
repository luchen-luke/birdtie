package postgres

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

func nativeAdaptationFixtureInput(t *testing.T) ([]byte, []byte) {
	return legacySyntheticInput(t, func(tables map[string]any) {
		row := func(table string) map[string]any {
			v := map[string]any{}
			for _, k := range legacymigration.ColumnNames(table) {
				v[k] = nil
			}
			return v
		}
		countryID := "15600000-0000-4000-8000-000000000001"
		cityID := "15600000-0000-4000-8000-000000000002"
		placeID := "15600000-0000-4000-8000-000000000003"
		ownerID := "14600000-0000-4000-8000-000000000010"
		country := row("geo_areas")
		country["id"] = countryID
		country["kind"] = "country"
		country["status"] = "active"
		country["country_code"] = "GB"
		country["name_en"] = "United Kingdom"
		city := row("geo_areas")
		city["id"] = cityID
		city["kind"] = "city"
		city["status"] = "active"
		city["country_code"] = "GB"
		city["name_en"] = "Synthetic City"
		city["parent_id"] = countryID
		tables["geo_areas"] = []any{country, city}
		place := row("places")
		place["id"] = placeID
		place["admin_area_id"] = cityID
		place["name"] = "Synthetic old place"
		place["category_code"] = "park"
		place["description"] = "Old description"
		place["status"] = "active"
		place["admin_status"] = "active"
		place["lifecycle_status"] = "active"
		place["lat"] = json.Number("57.1")
		place["lng"] = json.Number("-2.1")
		tables["places"] = []any{place}
		for _, n := range tables["notes"].([]any) {
			v := n.(map[string]any)
			v["destination_id"] = cityID
			v["destination_source"] = "selected"
			v["created_at"] = "2026-10-13T00:00:00Z"
			v["updated_at"] = "2026-10-14T00:00:00Z"
		}
		unplaced := row("notes")
		for k, v := range tables["notes"].([]any)[0].(map[string]any) {
			unplaced[k] = v
		}
		unplaced["id"] = "14600000-0000-4000-8000-000000000025"
		unplaced["destination_id"] = nil
		tables["notes"] = append(tables["notes"].([]any), unplaced)
		saved := row("user_saved_places")
		saved["user_id"] = ownerID
		saved["place_id"] = placeID
		saved["created_at"] = "2026-10-13T00:00:00Z"
		tables["user_saved_places"] = []any{saved}
	})
}

func TestLegacyNativeAdaptationNativeStablePrivateTargets(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	source, manifest := nativeAdaptationFixtureInput(t)
	ready, e := PrepareLegacyNativeAdaptation(legacyTestRunID, source, manifest)
	if e != nil || ready.Counts.Cities != 1 || ready.Counts.Places != 1 || ready.Counts.Moments != 2 || ready.Counts.SavedPlaces != 1 {
		t.Fatal("synthetic native plan rejected", e, ready.Counts)
	}
	if _, e = f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, source, manifest); !errors.Is(e, ErrLegacyMigrationNotFound) {
		t.Fatal("missing preservation accepted", e)
	}
	if _, e = f.Importer.Apply(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("base preservation", e)
	}
	if _, e = f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("personal preservation", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='deleted' WHERE id IN(SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user')`); e != nil {
		t.Fatal("deleted owner fixture", e)
	}
	if _, e = f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, source, manifest); !errors.Is(e, ErrLegacyMigrationConflict) {
		t.Fatal("deleted owner was imported", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='suspended' WHERE id IN(SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user')`); e != nil {
		t.Fatal("restore synthetic owner", e)
	}
	first, e := f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, source, manifest)
	if e != nil || first.Status != "COMMITTED" || first.Counts != ready.Counts {
		t.Fatal("native conversion", e, first.Counts)
	}
	var safe bool
	e = f.Pool.QueryRow(f.Context, `WITH owners AS(SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user'),
 ms AS(SELECT * FROM moments WHERE author_account_id IN(SELECT target_id FROM owners)),
 ps AS(SELECT * FROM places WHERE id::text IN(SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='place')),
 cs AS(SELECT * FROM cities WHERE id IN(SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='city')) SELECT
 (SELECT count(*) FROM ms)=2 AND (SELECT count(*) FROM ms WHERE city_id IS NULL AND place_id IS NULL AND location_precision='none')=1
 AND (SELECT bool_and(visibility='private' AND status='draft' AND author_confirmed_at IS NULL AND published_at IS NULL) FROM ms)
 AND (SELECT bool_and(publication_status='draft' AND latitude IS NULL AND longitude IS NULL) FROM ps)
 AND (SELECT bool_and(publication_status='published' AND content_status='building' AND verified_at IS NULL) FROM cs)
 AND (SELECT count(*) FROM legacy_note_archives WHERE deleted AND body='')=1
 AND (SELECT count(*) FROM sessions WHERE account_id IN(SELECT target_id FROM owners))=0
 AND (SELECT bool_and(status='suspended') FROM agents WHERE principal_account_id IN(SELECT target_id FROM owners))
 AND (SELECT count(*) FROM saved_items s JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=s.owner_account_id)=1`).Scan(&safe)
	if e != nil || !safe {
		t.Fatal("migration expanded authority, guessed coordinates, or revived deletion", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE moments SET body='',status='published',visibility='public',author_confirmed_at=now(),published_at=now() WHERE city_id IS NULL`); e == nil {
		t.Fatal("empty content entered public state")
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE moments SET body='owner edited after migration',revision=revision+1 WHERE id::text IN(SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='moment')`); e != nil {
		t.Fatal("native draft edit", e)
	}
	repeat, e := f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, source, manifest)
	if e != nil || !reflect.DeepEqual(first, repeat) {
		t.Fatal("original import was not idempotent", e)
	}
	inspected, e := f.Importer.InspectNativeAdaptation(f.Context, legacyTestRunID)
	if e != nil || !reflect.DeepEqual(first, inspected) {
		t.Fatal("original receipt changed", e)
	}
	var edited bool
	if e = f.Pool.QueryRow(f.Context, `SELECT bool_and(body='owner edited after migration' AND revision=2) FROM moments WHERE id::text IN(SELECT target_id FROM legacy_native_adaptation_entities WHERE kind='moment')`).Scan(&edited); e != nil || !edited {
		t.Fatal("replay overwrote native owner edit", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE legacy_native_adaptation_entities SET target_id='replacement'`); e == nil {
		t.Fatal("provenance became mutable")
	}
	wrong := *f.Importer
	wrong.expected.DatabaseOID++
	if _, e = wrong.InspectNativeAdaptation(f.Context, legacyTestRunID); !errors.Is(e, ErrLegacyMigrationGuard) {
		t.Fatal("foreign target accepted", e)
	}
}
