package legacymigration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
)

const testRun = "a1000000-0000-4000-8000-000000000001"
const testOwner = "a2000000-0000-4000-8000-000000000001"
const testNote = "a3000000-0000-4000-8000-000000000001"
const testDeleted = "a3000000-0000-4000-8000-000000000002"
const testPlace = "a4000000-0000-4000-8000-000000000001"
const testMedia = "a5000000-0000-4000-8000-000000000001"
const testThread = "a6000000-0000-4000-8000-000000000001"

func baseRow(table string, values map[string]any) map[string]any {
	r := map[string]any{}
	for _, k := range sourceColumns[table] {
		r[k] = nil
	}
	for k, v := range values {
		r[k] = v
	}
	return r
}
func syntheticTables() map[string]any {
	ts := map[string]any{}
	for _, k := range Tables() {
		ts[k] = []any{}
	}
	ts["users"] = []any{baseRow("users", map[string]any{"id": testOwner, "version": json.Number("7"), "status": "limited", "data_origin": "user", "display_name": "", "bio": "原个人简介", "handle": "原handle不可占新unique", "email": "synthetic@example.invalid", "password_hash": "SYNTHETIC_PASSWORD_HASH_SECRET", "avatar_url": "SYNTHETIC_PRIVATE_AVATAR_URL", "profile_visibility": "public"})}
	ts["notes"] = []any{baseRow("notes", map[string]any{"id": testNote, "author_id": testOwner, "version": json.Number("19"), "title": "", "caption": "<script>只是原文</script>\u2028未知时刻", "deleted_at": nil, "visibility": "public", "moderation_status": "ok", "cover_url": "SYNTHETIC_PRIVATE_COVER_URL", "recorded_start_date": nil, "recorded_end_date": "2021-03-02", "thread_id": testThread, "thread_position": json.Number("0")}), baseRow("notes", map[string]any{"id": testDeleted, "author_id": testOwner, "version": json.Number("4"), "title": "不得恢复标题", "caption": "不得恢复删除正文", "deleted_at": "2022-05-01T00:00:00+00:00", "recorded_start_date": "2019-01-01", "visibility": "private", "moderation_status": "deleted"})}
	ts["note_places"] = []any{baseRow("note_places", map[string]any{"note_id": testNote, "place_id": nil, "raw_name": "原未匹配地点", "position": json.Number("9")}), baseRow("note_places", map[string]any{"note_id": testNote, "place_id": testPlace, "raw_name": "原地点", "position": json.Number("2")}), baseRow("note_places", map[string]any{"note_id": testDeleted, "place_id": testPlace, "raw_name": "删除关系", "position": json.Number("1")})}
	ts["note_media"] = []any{baseRow("note_media", map[string]any{"note_id": testNote, "media_id": testMedia, "position": json.Number("7")}), baseRow("note_media", map[string]any{"note_id": testDeleted, "media_id": testMedia, "position": json.Number("1")})}
	ts["note_media_locations"] = []any{baseRow("note_media_locations", map[string]any{"note_id": testNote, "media_id": testMedia, "latitude": json.Number("25.123456789123456789"), "longitude": nil, "map_enabled": true, "place_ids": []any{testPlace}, "captured_date": nil})}
	ts["moment_threads"] = []any{baseRow("moment_threads", map[string]any{"id": testThread, "author_id": testOwner, "entry_count": json.Number("2"), "created_at": "2021-03-01T00:00:00+00:00", "route_revision": json.Number("5")})}
	ts["moment_thread_legs"] = []any{baseRow("moment_thread_legs", map[string]any{"thread_id": testThread, "from_note_id": testNote, "to_note_id": testDeleted, "from_position": json.Number("0"), "to_position": json.Number("1"), "mode": "旧模式未核实", "from_anchor_place_id": nil, "to_anchor_place_id": testPlace, "created_at": "原记录时间", "updated_at": "原更新时间"})}
	ts["media_assets"] = []any{baseRow("media_assets", map[string]any{"id": testMedia, "public_url": "SYNTHETIC_PRIVATE_MEDIA_URL", "storage_key": "SYNTHETIC_PRIVATE_MEDIA_PATH"})}
	ts["journeys"] = []any{baseRow("journeys", map[string]any{"id": testDeleted, "deleted_at": "原删除时间"})}
	return ts
}
func documents(t *testing.T, ts map[string]any) ([]byte, []byte) {
	t.Helper()
	h := map[string]any{"instanceTag": SourceSystem, "container": "birdtie-civu-restore-20261014", "database": "civu_restore", "backupCipherSha256": BackupCipherSHA256, "schemaSha256": SourceSchemaSHA256}
	snap, e := Digest(map[string]any{"source": h, "tables": ts})
	if e != nil {
		t.Fatal(e)
	}
	h["snapshotSha256"] = snap
	source, e := Canonical(map[string]any{"schemaVersion": SourceProtocol, "source": h, "capture": map[string]any{"observedAt": "2026-10-14T00:00:00Z", "transactionSnapshot": "100:100:"}, "tables": ts})
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, '\n')
	counts, hashes, fam := map[string]any{}, map[string]any{}, map[string]any{}
	for k, v := range ts {
		counts[k] = len(v.([]any))
		hashes[k], e = Digest(v)
		if e != nil {
			t.Fatal(e)
		}
	}
	for k, v := range families {
		a := []any{}
		for _, s := range v {
			a = append(a, s)
		}
		fam[k] = a
	}
	manifest, e := Canonical(map[string]any{"schemaVersion": ManifestProtocol, "source": h, "sourceFileSha256": fileHash(source), "auditedSchemaOutputSha256": AuditedOutputSHA256, "counts": counts, "families": fam, "tableSha256": hashes, "execution": map[string]any{"preservation": "EXTRACTED_ONLY", "targetImport": "NOT_RUN", "identityVerification": "NOT_RUN", "sourceRights": "NOT_RUN", "mediaBytesMatched": "NOT_RUN", "mediaAuthorization": "NOT_RUN", "sessionMigration": "NOT_RUN", "relationshipEffects": 0, "sourceWrites": 0, "runtimeStarted": false}})
	if e != nil {
		t.Fatal(e)
	}
	return source, append(manifest, '\n')
}

func TestLegacyMigrationPlanPreservesOriginalFactsWithoutNativeAuthorization(t *testing.T) {
	source, manifest := documents(t, syntheticTables())
	old := append([]byte(nil), source...)
	p, e := BuildPlan(testRun, source, manifest)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Owners) != 1 || len(p.Notes) != 2 || p.Owners[0].SourceVersion != 7 || p.Owners[0].SourceStatus != "limited" || p.Owners[0].DisplayName != "" || p.Notes[0].SourceVersion != 19 || p.Notes[0].Title != "" || p.Notes[0].RecordedStartDate != nil || *p.Notes[0].RecordedEndDate != "2021-03-02" {
		t.Fatal("original source versions/unknown fields were replaced")
	}
	if p.Notes[0].Body != "<script>只是原文</script>\u2028未知时刻" || !bytes.Equal(old, source) {
		t.Fatal("literal source altered")
	}
	if p.Deferred["media_assets"] != 1 || p.Deferred["journeys"] != 1 || p.Run.SourceFileSHA256 != fileHash(source) || p.Run.ManifestFileSHA256 != fileHash(manifest) {
		t.Fatal("unsupported history not deferred")
	}
	rel, e := legacyarchive.DecodeRelationships(p.Notes[0].Payload)
	if e != nil {
		t.Fatal(e)
	}
	if len(rel.Places) != 2 || rel.Places[0].Position != 2 || rel.Places[1].SourcePlaceID != nil || rel.Media[0].State != "UNAVAILABLE" || rel.MediaLocations[0].Latitude.String() != "25.123456789123456789" || !rel.MediaLocations[0].MapEnabled || rel.Thread.RouteRevision != 5 || rel.ThreadLegs[0].SourceToNoteID != testDeleted {
		t.Fatal("ordered private relationships lost or granted current media")
	}
	deleted := p.Notes[1]
	if !deleted.Deleted || deleted.Title != "" || deleted.Body != "" || deleted.RecordedStartDate != nil || deleted.SourceVisibility != "" || string(deleted.Payload) != "{}" || deleted.SourceSHA256 == "" {
		t.Fatal("deleted source resurrected")
	}
	raw, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"SYNTHETIC_PASSWORD", "SYNTHETIC_PRIVATE", "原个人简介", "不得恢复删除正文", testOwner} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("plan JSON exposes private proposals")
		}
	}
	for _, n := range p.Notes {
		if strings.Contains(string(n.Payload), "SYNTHETIC_PRIVATE") || strings.Contains(string(n.Payload), "public_url") {
			t.Fatal("old URL/credentials entered native projection")
		}
	}
	if strings.Contains(fmt.Sprint(p), testOwner) || strings.Contains(fmt.Sprint(p.Owners[0]), "原个人简介") {
		t.Fatal("ordinary diagnostic exposes source")
	}
}
func TestLegacyMigrationCanonicalMatchesPythonWithoutFloatOrHTMLChanges(t *testing.T) {
	v, e := decode([]byte("{\"z\":25.123456789123456789,\"text\":\"<中文>\u2028\u2029\",\"n\":9007199254740993,\"exponent\":1E-7,\"fraction\":1.200}"))
	if e != nil {
		t.Fatal(e)
	}
	got, e := Canonical(v)
	want := "{\"exponent\":1E-7,\"fraction\":1.200,\"n\":9007199254740993,\"text\":\"<中文>\u2028\u2029\",\"z\":25.123456789123456789}"
	if e != nil || string(got) != want {
		t.Fatal("Python canonical source digest changed")
	}
	if _, e = Canonical(0.25); e == nil {
		t.Fatal("binary float accepted into source hash")
	}
	for _, raw := range []string{`{"a":1,"a":2}`, `{"s":"\ud800"}`, `{"s":"\udfff"}`, `{"s":"\ud800\u1234"}`, `{"a":NaN}`, `{} {}`} {
		if _, e = decode([]byte(raw)); e == nil {
			t.Fatal("malformed or lossy source accepted")
		}
	}
	if _, e = decode([]byte(`{"s":"\ud83d\ude00"}`)); e != nil {
		t.Fatal("valid paired Unicode rejected")
	}
}
func TestLegacyMigrationHashesClosedSchemaAndOriginalVersionsFailClosed(t *testing.T) {
	source, manifest := documents(t, syntheticTables())
	for _, run := range []string{"", testRun + "x"} {
		if _, e := BuildPlan(run, source, manifest); e == nil {
			t.Fatal("invalid run accepted")
		}
	}
	mutations := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missingTable", func(ts map[string]any) { delete(ts, "note_media_locations") }},
		{"unknownTable", func(ts map[string]any) { ts["sessions"] = []any{} }},
		{"unknownColumn", func(ts map[string]any) { ts["notes"].([]any)[0].(map[string]any)["nativePermission"] = true }},
		{"missingUserVersion", func(ts map[string]any) { delete(ts["users"].([]any)[0].(map[string]any), "version") }},
		{"inventedUserVersion", func(ts map[string]any) { ts["users"].([]any)[0].(map[string]any)["version"] = true }},
		{"fractionalNoteVersion", func(ts map[string]any) { ts["notes"].([]any)[0].(map[string]any)["version"] = json.Number("1.0") }},
		{"orphanOwner", func(ts map[string]any) { ts["users"] = []any{} }},
		{"duplicateOwner", func(ts map[string]any) { ts["users"] = append(ts["users"].([]any), ts["users"].([]any)[0]) }},
		{"duplicateNote", func(ts map[string]any) { ts["notes"] = append(ts["notes"].([]any), ts["notes"].([]any)[0]) }},
		{"crossOwnerThread", func(ts map[string]any) { ts["moment_threads"].([]any)[0].(map[string]any)["author_id"] = testDeleted }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			ts := syntheticTables()
			tt.mutate(ts)
			s, m := documents(t, ts)
			if _, e := BuildPlan(testRun, s, m); e == nil {
				t.Fatal("ambiguous source accepted")
			}
		})
	}
	changed := append([]byte(nil), source...)
	changed = bytes.Replace(changed, []byte("只是原文"), []byte("变更原文"), 1)
	if _, e := BuildPlan(testRun, changed, manifest); e == nil {
		t.Fatal("file hash mismatch accepted")
	}
	for _, pair := range [][2][]byte{{source, bytes.Replace(manifest, []byte(`"targetImport":"NOT_RUN"`), []byte(`"targetImport":"IMPORTED"`), 1)}, {source, bytes.Replace(manifest, []byte(`"sourceWrites":0`), []byte(`"sourceWrites":1`), 1)}} {
		if _, e := BuildPlan(testRun, pair[0], pair[1]); e == nil {
			t.Fatal("false execution assertion accepted")
		}
	}
}
