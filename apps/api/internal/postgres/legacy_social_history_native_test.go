package postgres

import (
	"bytes"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/legacyhistory"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacysocial"
	"net/http/httptest"
	"strings"
	"testing"
)

func historyFixtureCanonical(t *testing.T, v any) []byte {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal("synthetic encoding")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var normalized any
	if d.Decode(&normalized) != nil {
		t.Fatal("synthetic normalization")
	}
	raw, e = legacymigration.Canonical(normalized)
	if e != nil {
		t.Fatal("synthetic canonical form")
	}
	return append(raw, '\n')
}
func historyFixtureHash(t *testing.T, v any) string {
	var n any
	d := json.NewDecoder(bytes.NewReader(historyFixtureCanonical(t, v)))
	d.UseNumber()
	if d.Decode(&n) != nil {
		t.Fatal("synthetic digest")
	}
	h, e := legacymigration.Digest(n)
	if e != nil {
		t.Fatal("synthetic digest")
	}
	return h
}
func historyFixtureSources(t *testing.T) (os, om, ss, sm, hs, hm []byte) {
	base, _ := journeyDetailsFixtureSource(t)
	var source map[string]any
	d := json.NewDecoder(bytes.NewReader(base))
	d.UseNumber()
	if d.Decode(&source) != nil {
		t.Fatal("synthetic original")
	}
	os, om = legacySyntheticInput(t, func(tables map[string]any) {
		for k, v := range source["tables"].(map[string]any) {
			tables[k] = v
		}
		first := tables["users"].([]any)[0].(map[string]any)
		second := map[string]any{}
		for k, v := range first {
			second[k] = v
		}
		second["id"] = "16500000-0000-4000-8000-000000000002"
		second["handle"] = "synthetic-history-other"
		tables["users"] = []any{first, second}
	})
	original, e := legacymigration.BuildPlan(legacyTestRunID, os, om)
	if e != nil {
		t.Fatal("synthetic base guard")
	}
	socialTables := map[string]any{}
	for _, k := range strings.Fields("follows friendships conversations conversation_members messages message_requests message_attachments private_message_media") {
		socialTables[k] = []any{}
	}
	header := map[string]any{"sourceSystem": legacymigration.SourceSystem, "runId": legacyTestRunID, "backupCipherSha256": legacymigration.BackupCipherSHA256, "originalSourceFileSha256": original.Run.SourceFileSHA256, "originalManifestFileSha256": original.Run.ManifestFileSHA256, "originalSnapshotSha256": original.Run.SnapshotSHA256, "socialSchemaSha256": legacysocial.SourceSchemaSHA256}
	deferred := map[string]any{}
	header["snapshotSha256"] = historyFixtureHash(t, map[string]any{"source": header, "tables": socialTables, "deferredCounts": deferred})
	ss = historyFixtureCanonical(t, map[string]any{"schemaVersion": legacysocial.Protocol, "source": header, "capture": map[string]any{}, "tables": socialTables, "deferredCounts": deferred})
	counts, hashes := map[string]any{}, map[string]any{}
	for k, v := range socialTables {
		counts[k] = len(v.([]any))
		hashes[k] = historyFixtureHash(t, v)
	}
	sm = historyFixtureCanonical(t, map[string]any{"schemaVersion": legacysocial.ManifestProtocol, "source": header, "sourceFileSha256": legacyhistory.SHA(ss), "counts": counts, "tableSha256": hashes, "deferredCounts": deferred})
	if _, e = legacysocial.Build(legacyTestRunID, os, om, ss, sm); e != nil {
		t.Fatal("synthetic sealed social")
	}
	row := func(table string, values map[string]any) map[string]any {
		r := map[string]any{}
		for _, k := range legacyhistory.ColumnNames(table) {
			r[k] = nil
		}
		for k, v := range values {
			r[k] = v
		}
		return r
	}
	const first = "14600000-0000-4000-8000-000000000010"
	const second = "16500000-0000-4000-8000-000000000002"
	const tid = "16500000-0000-4000-8000-000000000010"
	stamp := "2026-10-13T00:00:00Z"
	historyTables := map[string]any{"ai_journey_threads": []any{row("ai_journey_threads", map[string]any{"id": tid, "owner_id": second, "journey_id": "15800000-0000-4000-8000-000000000010", "summary": "合成协作者原对话", "status": "active", "context_revision": 2, "created_at": stamp, "updated_at": stamp}), row("ai_journey_threads", map[string]any{"id": "16500000-0000-4000-8000-000000000011", "owner_id": first, "journey_id": "15800000-0000-4000-8000-000000000019", "summary": "合成已删旅程对话", "status": "active", "context_revision": 1, "created_at": stamp, "updated_at": stamp}), row("ai_journey_threads", map[string]any{"id": "16500000-0000-4000-8000-000000000012", "owner_id": first, "draft_context_id": "16500000-0000-4000-8000-000000000099", "summary": "合成草稿对话", "status": "active", "context_revision": 0, "created_at": stamp, "updated_at": stamp})}, "ai_journey_messages": []any{row("ai_journey_messages", map[string]any{"id": "16500000-0000-4000-8000-000000000020", "thread_id": tid, "role": "user", "content": "仅合成原问题", "created_at": stamp, "request_id": "original-synthetic-request"}), row("ai_journey_messages", map[string]any{"id": "16500000-0000-4000-8000-000000000021", "thread_id": tid, "role": "assistant", "content": "仅合成原助手回复", "created_at": stamp, "request_id": "original-synthetic-reply"})}, "notifications": []any{}, "notification_settings": []any{}}
	for i, id := range []string{"16500000-0000-4000-8000-000000000030", "16500000-0000-4000-8000-000000000031"} {
		n := row("notifications", map[string]any{"id": id, "recipient_id": second, "actor_id": first, "type": "follow", "aggregate_type": "user", "aggregate_id": first, "payload": map[string]any{"targetTitle": "不得公开旧缓存标题"}, "created_at": stamp, "event_type": "follow", "category": "follow", "anchor_type": "user", "payload_schema_version": 1, "inbox_version": i + 1})
		if i == 1 {
			n["read_at"] = stamp
			n["seen_at"] = stamp
			n["deleted_at"] = stamp
		}
		historyTables["notifications"] = append(historyTables["notifications"].([]any), n)
	}
	prefs := map[string]any{}
	for _, k := range strings.Fields("chatPush requestPush commentPush followPush praiseSavePush journeyPush systemPush previewEnabled") {
		prefs[k] = false
	}
	prefs["timezone"] = "Etc/UTC"
	historyTables["notification_settings"] = []any{row("notification_settings", map[string]any{"user_id": second, "settings": prefs, "version": 5, "updated_at": stamp})}
	historyHeader := map[string]any{}
	for k, v := range header {
		if k != "snapshotSha256" && k != "socialSchemaSha256" {
			historyHeader[k] = v
		}
	}
	historyHeader["socialSourceFileSha256"] = legacyhistory.SHA(ss)
	historyHeader["socialManifestFileSha256"] = legacyhistory.SHA(sm)
	historyHeader["socialSnapshotSha256"] = header["snapshotSha256"]
	historyHeader["historySchemaSha256"] = legacyhistory.SourceSchemaSHA256
	historyHeader["snapshotSha256"] = historyFixtureHash(t, map[string]any{"source": historyHeader, "tables": historyTables})
	hs = historyFixtureCanonical(t, map[string]any{"schemaVersion": legacyhistory.Protocol, "source": historyHeader, "capture": map[string]any{}, "tables": historyTables})
	counts, hashes = map[string]any{}, map[string]any{}
	for k, v := range historyTables {
		counts[k] = len(v.([]any))
		hashes[k] = historyFixtureHash(t, v)
	}
	hm = historyFixtureCanonical(t, map[string]any{"schemaVersion": legacyhistory.ManifestProtocol, "source": historyHeader, "sourceFileSha256": legacyhistory.SHA(hs), "counts": counts, "tableSha256": hashes, "execution": map[string]any{}})
	return
}
func TestLegacySocialHistorySealedSyntheticProjection(t *testing.T) {
	os, om, ss, sm, hs, hm := historyFixtureSources(t)
	p, e := legacyhistory.Build(legacyTestRunID, os, om, ss, sm, hs, hm)
	if e != nil || p.Counts.Threads != 3 || p.Counts.Messages != 2 || p.Counts.HistoricalUnread != 1 || p.Counts.DeletedNotifications != 1 || p.Counts.Preferences != 1 {
		t.Fatal("closed synthetic history projection", e)
	}
}
func TestLegacySocialHistoryNativePrivateOwnerInertInboxAndRepeat(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	os, om, ss, sm, hs, hm := historyFixtureSources(t)
	if _, e := f.Importer.Apply(f.Context, legacyTestRunID, os, om); e != nil {
		t.Fatal("synthetic original import", e)
	}
	if _, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, os, om); e != nil {
		t.Fatal("synthetic original archives", e)
	}
	if _, e := f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, os, om); e != nil {
		t.Fatal("synthetic native entities", e)
	}
	if _, e := f.Importer.ApplyJourneyDetails(f.Context, legacyTestRunID, os, om); e != nil {
		t.Fatal("synthetic detailed journeys", e)
	}
	if _, e := f.Importer.ApplyLegacySocial(f.Context, legacyTestRunID, os, om, ss, sm, LegacySocialImageInput{}); e != nil {
		t.Fatal("synthetic sealed social", e)
	}
	before := func() (int, int, int, int) {
		var a, b, c, d int
		if e := f.Pool.QueryRow(f.Context, `SELECT(SELECT count(*)FROM model_request_runs),(SELECT count(*)FROM agent_domain_outbox),(SELECT count(*)FROM native_notification_decisions),(SELECT count(*)FROM system_push_events)`).Scan(&a, &b, &c, &d); e != nil {
			t.Fatal("inert baselines")
		}
		return a, b, c, d
	}
	a, b, c, d := before()
	result, e := f.Importer.ApplyLegacySocialHistory(f.Context, legacyTestRunID, os, om, ss, sm, hs, hm)
	if e != nil || result.Status != "COMMITTED" || result.Counts.Messages != 2 {
		t.Fatal("atomic original history import", e)
	}
	aa, bb, cc, dd := before()
	if a != aa || b != bb || c != cc || d != dd {
		t.Fatal("history import called model/outbox/delivery/push")
	}
	again, e := f.Importer.ApplyLegacySocialHistory(f.Context, legacyTestRunID, os, om, ss, sm, hs, hm)
	if e != nil || again != result {
		t.Fatal("immutable replay changed history", e)
	}
	var first, second string
	if e = f.Pool.QueryRow(f.Context, `SELECT target_id::text FROM legacy_entity_mappings WHERE entity_kind='user' AND source_id='14600000-0000-4000-8000-000000000010'`).Scan(&first); e != nil {
		t.Fatal("first synthetic owner")
	}
	if e = f.Pool.QueryRow(f.Context, `SELECT target_id::text FROM legacy_entity_mappings WHERE entity_kind='user' AND source_id='16500000-0000-4000-8000-000000000002'`).Scan(&second); e != nil {
		t.Fatal("second synthetic owner")
	}
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal("synthetic canonical Session token")
	}
	otherToken, otherDigest, e := identity.NewToken()
	if e != nil {
		t.Fatal("synthetic canonical counterpart Session token")
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=ANY($1::uuid[])`, []string{first, second}); e != nil {
		t.Fatal("synthetic owner status")
	}
	for id, hash := range map[string][32]byte{first: otherDigest, second: digest} {
		if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 hour')`, id, hash[:]); e != nil {
			t.Fatal("synthetic fresh session")
		}
	}
	store := New(f.Pool, true)
	router := httpapi.New(store, store, nil, store, nil, nil, nil, nil, nil, nil, nil, store, true, nil, nil, nil)
	request := func(path, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	id := legacyhistory.TargetID("thread", "16500000-0000-4000-8000-000000000010")
	if w := request("/v1/me/journey-history/"+id, token); w.Code != 200 || !strings.Contains(w.Body.String(), "仅合成原助手回复") || strings.Contains(w.Body.String(), `"journeyId":"158`) {
		t.Fatal("original thread owner private read", w.Code)
	}
	if w := request("/v1/me/journey-history/"+id, otherToken); w.Code != 404 {
		t.Fatal("Journey owner read collaborator private history", w.Code)
	}
	if w := request("/v1/me/journey-history", ""); w.Code != 401 {
		t.Fatal("anonymous private history", w.Code)
	}
	var linked int
	if e = f.Pool.QueryRow(f.Context, `SELECT count(*)FROM personal_journey_history_threads WHERE journey_id IS NOT NULL`).Scan(&linked); e != nil || linked != 0 {
		t.Fatal("deleted/foreign/draft contexts created native Journeys")
	}
	actor := identity.Actor{ID: second, AccountType: "person"}
	page, e := store.ListHumanInboxPage(f.Context, digest, actor, inbox.PageRequest{View: "notifications", Origin: "LEGACY_HISTORY"})
	if e != nil || len(page.Items) != 1 || page.UnreadCount != 0 || page.HistoricalUnreadCount != 1 || page.Items[0].Origin != "LEGACY_HISTORY" || strings.Contains(page.Items[0].Detail, "缓存") {
		t.Fatal("historical notification projection/badge", e)
	}
	item := page.Items[0]
	if _, e = store.ReadHumanInboxItem(f.Context, digest, actor, item.ID); e != nil {
		t.Fatal("mark historical item read", e)
	}
	if _, e = f.Importer.ApplyLegacySocialHistory(f.Context, legacyTestRunID, os, om, ss, sm, hs, hm); e != nil {
		t.Fatal("repeat after own read", e)
	}
	page, e = store.ListHumanInboxPage(f.Context, digest, actor, inbox.PageRequest{View: "notifications", Origin: "LEGACY_HISTORY"})
	if e != nil || page.HistoricalUnreadCount != 0 {
		t.Fatal("repeat reset read status", e)
	}
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM inbox_items WHERE id=$1`, item.ID); e != nil {
		t.Fatal("owned notification delete")
	}
	if _, e = f.Importer.ApplyLegacySocialHistory(f.Context, legacyTestRunID, os, om, ss, sm, hs, hm); e != nil {
		t.Fatal("repeat after delete", e)
	}
	page, e = store.ListHumanInboxPage(f.Context, digest, actor, inbox.PageRequest{View: "notifications", Origin: "LEGACY_HISTORY"})
	if e != nil || len(page.Items) != 0 {
		t.Fatal("repeat revived deleted notification", e)
	}
	v, e := store.ReadJourneyHistory(f.Context, digest, actor, content.JourneyHistoryQuery{Preferences: true})
	if e != nil || v.Preferences == nil || v.Preferences.Settings["systemPush"] {
		t.Fatal("original preferences were enabled", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE sessions SET revoked_at=clock_timestamp()WHERE token_sha256=$1`, digest[:]); e != nil {
		t.Fatal("synthetic revocation")
	}
	if v.ResponseProof.RevalidateHumanControl(f.Context) == nil {
		t.Fatal("late history response survived revocation")
	}
	if w := request("/v1/me/journey-history", token); w.Code != 401 {
		t.Fatal("revoked history session", w.Code)
	}
}
