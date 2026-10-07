package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func evidenceHTTPRecord(t *testing.T, raw []byte) agentmemory.Evidence {
	t.Helper()
	var envelope struct {
		Data agentmemory.Evidence `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || agentmemory.ValidateEvidence(envelope.Data) != nil {
		t.Fatal("invalid real Evidence response")
	}
	return envelope.Data
}
func provenanceHTTPRecord(t *testing.T, raw []byte) agentmemory.Provenance {
	t.Helper()
	var envelope struct {
		Data agentmemory.Provenance `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Data.SchemaVersion != agentmemory.ProvenanceSchemaV1 {
		t.Fatal("invalid real provenance response")
	}
	return envelope.Data
}
func TestAgentMemoryEvidenceHTTPRealRoutesPersistenceIsolationAndScrub(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	var installed bool
	if err := f.pool.QueryRow(f.ctx, `SELECT to_regclass('public.agent_memory_evidence') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("requires actual057")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := f.pool.Exec(ctx, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Errorf("own HTTP Moment cleanup: %v", err)
		}
	})
	var memoryID, evidenceID string
	if err := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid(),gen_random_uuid()`).Scan(&memoryID, &evidenceID); err != nil {
		t.Fatal(err)
	}
	mpath := memoryHTTPList + "/" + memoryID
	ppath := mpath + "/provenance"
	epath := mpath + "/evidence/" + evidenceID
	m := memoryDBHTTPRecord(t, f.request(t, f.handler, "PUT", mpath, memoryDBHTTPBody(t, 0, "source-005", "本人明确声明005"), f.tokens[0], 200, nil).Body.Bytes())
	moment, err := f.store.CreateMomentDraft(f.ctx, f.accountIDs[0], content.MomentInput{CityID: "aberdeen-gb", Title: "HTTP_SOURCE_BODY_MUST_NOT_COPY", Body: "HTTP_SOURCE_PRIVATE_CANARY_005", TimePrecision: "unknown", LocationPrecision: "city"})
	if err != nil {
		t.Fatal(err)
	}
	input := agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentevent.MomentSource, SourceID: moment.ID}
	encoded, _ := json.Marshal(input)
	for _, probe := range []struct{ method, path, body string }{{"GET", ppath, ""}, {"PUT", epath, string(encoded)}, {"DELETE", epath, `{"expectedVersion":1}`}} {
		f.request(t, f.handler, probe.method, probe.path, probe.body, "", 401, nil)
		workspace := ""
		f.request(t, f.handler, probe.method, probe.path, probe.body, f.tokens[0], 403, &workspace)
		f.request(t, f.handler, probe.method, probe.path+"?ownerId="+f.accountIDs[1], probe.body, f.tokens[0], 400, nil)
		f.request(t, f.handler, probe.method, probe.path, probe.body, f.tokens[1], 404, nil)
		for _, token := range f.tokens[2:] {
			f.request(t, f.handler, probe.method, probe.path, probe.body, token, 403, nil)
		}
	}
	f.request(t, f.handler, "PUT", epath, `{"expectedMemoryVersion":1,"sourceType":"MOMENT","sourceId":"`+moment.ID+`","confirmed":true}`, f.tokens[0], 400, nil)
	f.request(t, f.handler, "PUT", epath, `{"expectedMemoryVersion":1,"sourceType":"MOMENT","sourceId":"`+moment.ID+`","sourceId":"`+moment.ID+`"}`, f.tokens[0], 400, nil)
	e := evidenceHTTPRecord(t, f.request(t, f.handler, "PUT", epath, string(encoded), f.tokens[0], 200, nil).Body.Bytes())
	if e.OwnerID != f.accountIDs[0] || e.AgentID != f.agentIDs[0] || e.MemoryID != m.ID || e.MemoryVersion != 1 || e.Source == nil || e.Source.Version.Revision != moment.Revision {
		t.Fatal("real exact binding/sourceVersion mismatch")
	}
	if retry := evidenceHTTPRecord(t, f.request(t, f.handler, "PUT", epath, string(encoded), f.tokens[0], 200, nil).Body.Bytes()); !reflect.DeepEqual(retry, e) {
		t.Fatal("HTTP repeated reference changed version/observed time")
	}
	reopened := postgres.New(f.pool, false)
	newHandler := privateProfileHTTPNew(reopened, reopened)
	p := provenanceHTTPRecord(t, f.request(t, newHandler, "GET", ppath, "", f.tokens[0], 200, nil).Body.Bytes())
	if len(p.Evidence) != 1 || p.Declaration != "本人明确填写" || p.Explanation != "本人关联了1条私人记录" {
		t.Fatal("real human provenance missing")
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "HTTP_SOURCE_") {
		t.Fatal("private source body copied into provenance")
	}
	if _, err = f.store.UpdateMomentDraft(f.ctx, f.accountIDs[0], moment.ID, moment.Revision, content.MomentInput{CityID: "aberdeen-gb", Title: "本人已编辑005", Body: "新源不可当旧版本", TimePrecision: "unknown", LocationPrecision: "city"}); err != nil {
		t.Fatal(err)
	}
	p = provenanceHTTPRecord(t, f.request(t, newHandler, "GET", ppath, "", f.tokens[0], 200, nil).Body.Bytes())
	if len(p.Evidence) != 0 || p.Explanation != "本人明确填写" {
		t.Fatal("edited source survived current read")
	}
	var scrubbed bool
	if err = f.pool.QueryRow(f.ctx, `SELECT status='REMOVED' AND version=2 AND source_id IS NULL AND source_token IS NULL AND signal_type IS NULL AND event_time IS NULL AND weight=0 FROM agent_memory_evidence WHERE id=$1`, e.ID).Scan(&scrubbed); err != nil || !scrubbed {
		t.Fatal("stale read did not scrub native provenance control")
	}
	f.request(t, newHandler, "PUT", epath, string(encoded), f.tokens[0], 409, nil)
	var newID string
	if err = f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	epNew := mpath + "/evidence/" + newID
	newE := evidenceHTTPRecord(t, f.request(t, newHandler, "PUT", epNew, string(encoded), f.tokens[0], 200, nil).Body.Bytes())
	if newE.Source.Version.Revision != moment.Revision+1 {
		t.Fatal("fresh manual reference did not bind new native revision")
	}
	f.request(t, newHandler, "DELETE", mpath, `{"expectedVersion":1}`, f.tokens[0], 200, nil)
	f.request(t, newHandler, "GET", ppath, "", f.tokens[0], 404, nil)
	removed := evidenceHTTPRecord(t, f.request(t, newHandler, "DELETE", epNew, `{"expectedVersion":1}`, f.tokens[0], 200, nil).Body.Bytes())
	if removed.Source != nil || removed.Status != agentmemory.EvidenceRemoved {
		t.Fatal("deleted Memory did not erase source link")
	}
	if _, err = f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0]); err != nil {
		t.Fatal(err)
	}
	f.request(t, newHandler, http.MethodDelete, epNew, `{"expectedVersion":1}`, f.tokens[0], 401, nil)
}
