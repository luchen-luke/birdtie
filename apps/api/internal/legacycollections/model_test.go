package legacycollections

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

const (
	testRun         = "c1000000-0000-4000-8000-000000000001"
	testOwner       = "c2000000-0000-4000-8000-000000000001"
	testJourney     = "c3000000-0000-4000-8000-000000000001"
	testDeleted     = "c3000000-0000-4000-8000-000000000002"
	testTheme       = "c4000000-0000-4000-8000-000000000001"
	testPlace       = "c5000000-0000-4000-8000-000000000001"
	testStop        = "c6000000-0000-4000-8000-000000000001"
	testPublication = "c7000000-0000-4000-8000-000000000001"
	testTimestamp   = "2021-03-01T02:03:04+08:00"
)

func rawRow(table string, values map[string]any) map[string]any {
	r := map[string]any{}
	for _, k := range legacymigration.ColumnNames(table) {
		r[k] = nil
	}
	for k, v := range values {
		r[k] = v
	}
	return r
}
func projectionRow(table string, spec []field, values map[string]any) map[string]any {
	r := rawRow(table, nil)
	for _, f := range spec {
		if f.source == "" {
			continue
		}
		if f.nullable {
			r[f.source] = nil
			continue
		}
		switch f.typ {
		case "s":
			r[f.source] = "原文<标签>\u2028"
		case "id":
			r[f.source] = testPlace
		case "i":
			r[f.source] = json.Number("7")
		case "n":
			r[f.source] = json.Number("25.123456789123456789")
		case "b":
			r[f.source] = false
		case "t":
			r[f.source] = testTimestamp
		case "date":
			r[f.source] = "2021-03-01"
		case "snapshot":
			r[f.source] = map[string]any{"privateUrl": "SYNTHETIC_PRIVATE_SNAPSHOT_URL", "nested": map[string]any{"exact": json.Number("9007199254740993")}}
		}
	}
	for k, v := range values {
		r[k] = v
	}
	return r
}
func fixtureTables() map[string]any {
	ts := map[string]any{}
	for _, t := range legacymigration.Tables() {
		ts[t] = []any{}
	}
	ts["users"] = []any{rawRow("users", map[string]any{"id": testOwner, "version": json.Number("4"), "status": "active", "data_origin": "user", "display_name": "", "bio": "", "handle": nil, "password_hash": "SYNTHETIC_PASSWORD_SECRET"})}
	j := projectionRow("journeys", journeyMeta, map[string]any{"id": testJourney, "owner_id": testOwner, "working_revision": json.Number("9"), "title": "私人旅程<原文>", "created_at": testTimestamp, "updated_at": testTimestamp, "deleted_at": nil, "city": "旧自由城市", "start_date": nil, "allow_copy": true, "cover_url": "SYNTHETIC_PRIVATE_COVER_URL"})
	d := rawRow("journeys", map[string]any{"id": testDeleted, "owner_id": testOwner, "working_revision": json.Number("11"), "title": "DELETED_TITLE_PRIVATE", "deleted_at": testTimestamp, "created_at": testTimestamp, "updated_at": testTimestamp})
	ts["journeys"] = []any{j, d}
	tm := projectionRow("theme_maps", themeMeta, map[string]any{"id": testTheme, "owner_id": testOwner, "revision": json.Number("5"), "title": "旧主题地图", "created_at": testTimestamp, "updated_at": testTimestamp, "deleted_at": nil, "cover_url": "SYNTHETIC_PRIVATE_THEME_URL"})
	ts["theme_maps"] = []any{tm}
	for _, spec := range journeyRelations {
		vals := map[string]any{"journey_id": testJourney}
		switch spec.payload {
		case "stops":
			vals["id"] = testStop
		case "publications":
			vals["id"] = testPublication
		case "publicationPlaces":
			vals["publication_id"] = testPublication
		case "members":
			vals["user_id"] = testOwner
		}
		ts[spec.table] = []any{projectionRow(spec.table, spec.fields, vals)}
	}
	// A deleted parent keeps an exact family hash, not readable child text.
	deadChild := rawRow("journey_arrangements", map[string]any{"id": testDeleted, "journey_id": testDeleted, "private_note": "DELETED_CHILD_PRIVATE"})
	ts["journey_arrangements"] = append(ts["journey_arrangements"].([]any), deadChild)
	for _, spec := range themeRelations {
		if spec.payload == "entrySources" {
			continue
		}
		ts[spec.table] = []any{projectionRow(spec.table, spec.fields, map[string]any{spec.key: testTheme, "place_id": testPlace})}
	}
	ts["user_saved_places"] = []any{rawRow("user_saved_places", map[string]any{"user_id": testOwner, "place_id": testPlace, "created_at": testTimestamp})}
	ts["place_save_sources"] = []any{projectionRow(savedSources.table, savedSources.fields, map[string]any{"user_id": testOwner, "place_id": testPlace, "source_type": "journey", "source_key": testJourney})}
	return ts
}
func seal(t *testing.T, ts map[string]any) ([]byte, []byte) {
	t.Helper()
	h := map[string]any{"instanceTag": legacymigration.SourceSystem, "container": "birdtie-civu-restore-20261014", "database": "civu_restore", "backupCipherSha256": legacymigration.BackupCipherSHA256, "schemaSha256": legacymigration.SourceSchemaSHA256}
	snap, e := legacymigration.Digest(map[string]any{"source": h, "tables": ts})
	if e != nil {
		t.Fatal(e)
	}
	h["snapshotSha256"] = snap
	source, e := legacymigration.Canonical(map[string]any{"schemaVersion": legacymigration.SourceProtocol, "source": h, "capture": map[string]any{"observedAt": "2026-10-14T00:00:00Z", "transactionSnapshot": "100:100:"}, "tables": ts})
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, '\n')
	counts, hashes := map[string]any{}, map[string]any{}
	for name, rs := range ts {
		counts[name] = len(rs.([]any))
		hashes[name], e = legacymigration.Digest(rs)
		if e != nil {
			t.Fatal(e)
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
	hash := sha256.Sum256(source)
	manifest, e := legacymigration.Canonical(map[string]any{"schemaVersion": legacymigration.ManifestProtocol, "source": h, "sourceFileSha256": hex.EncodeToString(hash[:]), "auditedSchemaOutputSha256": legacymigration.AuditedOutputSHA256, "counts": counts, "families": families, "tableSha256": hashes, "execution": map[string]any{"preservation": "EXTRACTED_ONLY", "targetImport": "NOT_RUN", "identityVerification": "NOT_RUN", "sourceRights": "NOT_RUN", "mediaBytesMatched": "NOT_RUN", "mediaAuthorization": "NOT_RUN", "sessionMigration": "NOT_RUN", "relationshipEffects": 0, "sourceWrites": 0, "runtimeStarted": false}})
	if e != nil {
		t.Fatal(e)
	}
	return source, append(manifest, '\n')
}
func build(t *testing.T, ts map[string]any) Plan {
	t.Helper()
	s, m := seal(t, ts)
	p, e := BuildPlan(testRun, s, m)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func record(t *testing.T, p Plan, kind, key string) Archive {
	t.Helper()
	for _, a := range p.Records {
		if a.Kind == kind && a.SourceKey == key {
			return a
		}
	}
	t.Fatal("record not found")
	return Archive{}
}

func TestPlanPreservesClosedPrivateCollectionsWithoutNativePermission(t *testing.T) {
	ts := fixtureTables()
	s, m := seal(t, ts)
	old := append([]byte(nil), s...)
	p, e := BuildPlan(testRun, s, m)
	if e != nil {
		t.Fatal(e)
	}
	if p.Counts != (Counts{Journeys: 2, JourneyTombstones: 1, ThemeMaps: 1, SavedPlaces: 1}) || len(p.Records) != 4 || !bytes.Equal(old, s) || ValidatePlan(p) != nil {
		t.Fatal("counts/source/plan changed")
	}
	a := record(t, p, Journey, testJourney)
	if *a.SourceRevision != 9 || a.SourceOwnerID != testOwner || a.Title != "私人旅程<原文>" {
		t.Fatal("owner/version/text changed")
	}
	v, e := decode(a.Payload, MaxPayloadBytes)
	if e != nil {
		t.Fatal(e)
	}
	payload := v.(map[string]any)
	meta := payload["metadata"].(map[string]any)
	if meta["sourceStartDate"] != nil || meta["sourceCityLabel"] != "旧自由城市" || meta["sourceAllowCopy"] != true {
		t.Fatal("unknown dates or literal old metadata changed")
	}
	if payload["stops"].([]any)[0].(map[string]any)["sourceConfidence"].(json.Number).String() != "25.123456789123456789" {
		t.Fatal("binary float changed source numeric fact")
	}
	if payload["media"].([]any)[0].(map[string]any)["state"] != "UNAVAILABLE" {
		t.Fatal("media became usable")
	}
	d := record(t, p, Journey, testDeleted)
	if !d.Deleted || d.Title != "" || len(d.Payload) != 0 || d.SourceDeletedAt == nil {
		t.Fatal("tombstone resurrected")
	}
	save := record(t, p, SavedPlace, testOwner+":"+testPlace)
	if save.SourceRevision != nil || save.SourceUpdatedAt != nil || save.Title != "" {
		t.Fatal("saved source invents revision/content")
	}
	raw, _ := json.Marshal(p)
	for _, secret := range []string{testOwner, "私人旅程", "SYNTHETIC_PRIVATE", "SYNTHETIC_PASSWORD", "DELETED_CHILD_PRIVATE"} {
		if bytes.Contains(raw, []byte(secret)) || strings.Contains(fmt.Sprint(p), secret) {
			t.Fatal("ordinary plan output leaks private data")
		}
	}
	for _, a := range p.Records {
		for _, secret := range []string{"SYNTHETIC_PRIVATE", "SYNTHETIC_PASSWORD", "DELETED_CHILD_PRIVATE", "DELETED_TITLE_PRIVATE", "storage_key", "currentPlaceId"} {
			if bytes.Contains(a.Payload, []byte(secret)) {
				t.Fatal("opaque content/current authority entered projection")
			}
		}
	}
	// A valid empty source remains valid; real source counts are not hardcoded.
	empty := map[string]any{}
	for _, name := range legacymigration.Tables() {
		empty[name] = []any{}
	}
	zero := build(t, empty)
	if zero.Counts.Total() != 0 || zero.Records == nil {
		t.Fatal("empty verified snapshot rejected")
	}
}

func TestOriginalVerificationAndRelationshipBindingFailClosed(t *testing.T) {
	tests := map[string]func(map[string]any){
		"unknown original field": func(ts map[string]any) { ts["journeys"].([]any)[0].(map[string]any)["extra"] = true },
		"duplicate saved pair": func(ts map[string]any) {
			ts["user_saved_places"] = append(ts["user_saved_places"].([]any), ts["user_saved_places"].([]any)[0])
		},
		"unmapped owner": func(ts map[string]any) { ts["journeys"].([]any)[0].(map[string]any)["owner_id"] = testPlace },
		"orphan child":   func(ts map[string]any) { ts["journey_stops"].([]any)[0].(map[string]any)["journey_id"] = testPlace },
		"duplicate child identity": func(ts map[string]any) {
			ts["journey_stops"] = append(ts["journey_stops"].([]any), ts["journey_stops"].([]any)[0])
		},
		"publication of other family": func(ts map[string]any) {
			ts["journey_publication_places"].([]any)[0].(map[string]any)["publication_id"] = testPlace
		},
		"source without saved pair": func(ts map[string]any) {
			ts["place_save_sources"].([]any)[0].(map[string]any)["place_id"] = testDeleted
		},
		"lossy source revision": func(ts map[string]any) { ts["theme_maps"].([]any)[0].(map[string]any)["revision"] = json.Number("1.5") },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			ts := fixtureTables()
			mutate(ts)
			s, m := seal(t, ts)
			if _, e := BuildPlan(testRun, s, m); e == nil {
				t.Fatal("invalid verified-family candidate accepted")
			}
		})
	}
	s, m := seal(t, fixtureTables())
	s = bytes.Replace(s, []byte("私人旅程"), []byte("擅改旅程"), 1)
	if _, e := BuildPlan(testRun, s, m); e == nil {
		t.Fatal("tampered source/manifest accepted")
	}
}

func TestDeletedFamilyAndOpaqueSnapshotStillAffectSourceHash(t *testing.T) {
	a := build(t, fixtureTables())
	ts := fixtureTables()
	ts["journey_arrangements"].([]any)[1].(map[string]any)["private_note"] = "CHANGED_DELETED_CHILD_PRIVATE"
	b := build(t, ts)
	before, after := record(t, a, Journey, testDeleted), record(t, b, Journey, testDeleted)
	if before.SourceSHA256 == after.SourceSHA256 || len(after.Payload) != 0 || after.Title != "" {
		t.Fatal("discarded deleted family lost evidence or resurrected")
	}
	ts = fixtureTables()
	ts["journey_publications"].([]any)[0].(map[string]any)["snapshot"] = map[string]any{"privateUrl": "CHANGED_PRIVATE_URL"}
	c := build(t, ts)
	x, y := record(t, a, Journey, testJourney), record(t, c, Journey, testJourney)
	if x.SourceSHA256 == y.SourceSHA256 || bytes.Equal(x.Payload, y.Payload) || bytes.Contains(y.Payload, []byte("CHANGED_PRIVATE_URL")) {
		t.Fatal("snapshot digest did not retain opaque evidence only")
	}
}

func TestWriterAndResponseValidatorsRejectMutatedOrExpandedProjection(t *testing.T) {
	p := build(t, fixtureTables())
	changed := p
	changed.Records = append([]Archive(nil), p.Records...)
	changed.Records[0].Title += "changed"
	if ValidatePlan(changed) == nil {
		t.Fatal("writer mutated plan without original hash")
	}
	changed = p
	changed.Counts.SavedPlaces++
	if ValidatePlan(changed) == nil {
		t.Fatal("writer changed counts")
	}
	a := record(t, p, Journey, testJourney)
	v, _ := decode(a.Payload, MaxPayloadBytes)
	m := v.(map[string]any)
	m["metadata"].(map[string]any)["currentPlaceId"] = testPlace
	a.Payload, _ = legacymigration.Canonical(m)
	if Validate(a) == nil {
		t.Fatal("current authority/unknown field accepted")
	}
	a = record(t, p, Journey, testJourney)
	a.Payload = bytes.Replace(a.Payload, []byte(`"metadata":{`), []byte(`"metadata":{"sourceCityLabel":"duplicate",`), 1)
	if Validate(a) == nil {
		t.Fatal("duplicate JSON key accepted")
	}
	a = record(t, p, Journey, testJourney)
	a.Payload = bytes.Replace(a.Payload, []byte("旧自由城市"), []byte(`\ud800`), 1)
	if Validate(a) == nil {
		t.Fatal("invalid surrogate repaired into historical text")
	}
	a = record(t, p, Journey, testDeleted)
	a.Payload = json.RawMessage(`null`)
	if Validate(a) == nil {
		t.Fatal("deleted payload accepted")
	}
	for _, a := range p.Records {
		s := Summary{ID: testStop, Kind: a.Kind, SourceKey: a.SourceKey, SourceRevision: a.SourceRevision, SourceSHA256: a.SourceSHA256, Deleted: a.Deleted, SourceDeletedAt: a.SourceDeletedAt, Title: a.Title, ImportedAt: time.Now()}
		if ValidateSummary(s) != nil || ValidateDetail(Detail{s, a.Payload}) != nil {
			t.Fatal("valid response shape rejected")
		}
	}
}
