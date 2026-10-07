package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"strings"
	"testing"
	"time"
)

const candidateHTTPPath = "/v1/me/agent-memory-candidates"

func candidateHTTPFlags(t *testing.T) *agentfeature.Controller {
	t.Helper()
	c, e := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
	if e != nil {
		t.Fatal(e)
	}
	f, e := agentfeature.NewController(c)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func candidateHTTPRead(t *testing.T, raw []byte) agentmemorycandidate.Record {
	t.Helper()
	var v struct {
		Data agentmemorycandidate.Record `json:"data"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Data.ID == "" {
		t.Fatal("candidate native DTO")
	}
	return v.Data
}
func TestMemoryCandidateHTTPNativeRegisteredLifecycleAndBoundaries(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	flags := candidateHTTPFlags(t)
	g := postgres.NewMemoryCandidateHumanGateway(f.store, flags)
	f.handler = New(f.store, f.store, nil, f.store, nil, nil, nil, nil, f.store, nil, nil, nil, false, nil, nil, nil, WithMemoryCandidates(g))
	// Fixtures create exclusively owned ordinary-domain rows; registered APIs
	// perform the manual candidate lifecycle with actual authenticated sessions.
	m, e := f.store.CreateMomentDraft(f.ctx, f.accountIDs[0], content.MomentInput{CityID: "aberdeen-gb", Title: "人工候选合成动态", Body: "合成测试，无生产身份", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	var place string
	if e = f.pool.QueryRow(f.ctx, `SELECT id FROM places WHERE publication_status='published' AND city_id='aberdeen-gb' AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1`).Scan(&place); e != nil {
		t.Fatal(e)
	}
	saved, e := f.store.Save(f.ctx, f.accountIDs[0], "place", place)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM saved_items WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM moment_activity_links WHERE moment_id IN(SELECT id FROM moments WHERE author_account_id=ANY($1::uuid[]))`, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(context.Background(), q, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	d := agentmemorycandidate.HumanDraft{Category: "sports", Sources: []agentmemorycandidate.Selector{{Type: agentevent.MomentSource, ID: m.ID}, {Type: agentevent.SavedPlaceSource, ID: saved}}, ValidUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)}
	raw, _ := json.Marshal(d)
	for _, name := range []string{"anonymous", "organization", "workspace", "unknownKey", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			token := f.tokens[0]
			body := string(raw)
			var workspace *string
			want := 400
			switch name {
			case "anonymous":
				token = ""
				want = 401
			case "organization":
				token = f.tokens[2]
				want = 403
			case "workspace":
				v := ""
				workspace = &v
				want = 403
			case "unknownKey":
				body = strings.TrimSuffix(body, "}") + `,"confirmed":true}`
			case "duplicate":
				body = `{"category":"sports","category":"culture"}`
			}
			f.request(t, f.handler, "POST", candidateHTTPPath, body, token, want, workspace)
		})
	}
	r := candidateHTTPRead(t, f.request(t, f.handler, "POST", candidateHTTPPath, string(raw), f.tokens[0], 200, nil).Body.Bytes())
	path := candidateHTTPPath + "/" + r.ID
	if r.Assessment == nil || r.Assessment.Value != nil || r.Assessment.Semantics != "ORDINAL" {
		t.Fatal("numeric confidence exposed")
	}
	f.request(t, f.handler, "GET", path, "", f.tokens[1], 404, nil)
	f.request(t, f.handler, "GET", path+"?owner=all", "", f.tokens[0], 400, nil)
	in := agentmemorycandidate.HumanPreviewInput{PreviewID: strings.Repeat("c", 64), ExpectedVersion: r.Version, MemoryValidUntil: d.ValidUntil}
	raw, _ = json.Marshal(in)
	pwire := f.request(t, f.handler, "POST", path+"/preview", string(raw), f.tokens[0], 200, nil)
	var p struct {
		Data agentmemorycandidate.HumanPreview `json:"data"`
	}
	if json.Unmarshal(pwire.Body.Bytes(), &p) != nil || p.Data.Review.Clusters != 2 {
		t.Fatal("concrete native preview")
	}
	f.request(t, f.handler, "POST", path+"/accept", `{"previewId":"`+in.PreviewID+`","confirmed":true}`, f.tokens[0], 400, nil)
	var n int
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, f.agentIDs[0]).Scan(&n) != nil || n != 0 {
		t.Fatal("preview/invalid approved wrote")
	}
	accepted := candidateHTTPRead(t, f.request(t, f.handler, "POST", path+"/accept", `{"previewId":"`+in.PreviewID+`"}`, f.tokens[0], 200, nil).Body.Bytes())
	if accepted.Status != agentmemorycandidate.Active || accepted.MemoryID == nil || *accepted.MemoryID != p.Data.Review.TargetMemoryID {
		t.Fatal("accept native receipt")
	}
	f.request(t, f.handler, "POST", path+"/accept", `{"previewId":"`+in.PreviewID+`"}`, f.tokens[0], 200, nil)
	restarted := New(f.store, f.store, nil, f.store, nil, nil, nil, nil, f.store, nil, nil, nil, false, nil, nil, nil, WithMemoryCandidates(postgres.NewMemoryCandidateHumanGateway(f.store, flags)))
	f.request(t, restarted, "POST", path+"/accept", `{"previewId":"`+in.PreviewID+`"}`, f.tokens[0], 409, nil)
	f.request(t, restarted, "GET", path, "", f.tokens[0], 200, nil)
	f.request(t, restarted, "GET", candidateHTTPPath, "", f.tokens[0], 200, nil)
	d.Category = "culture"
	raw, _ = json.Marshal(d)
	rejectedCandidate := candidateHTTPRead(t, f.request(t, f.handler, "POST", candidateHTTPPath, string(raw), f.tokens[0], 200, nil).Body.Bytes())
	rejectPath := candidateHTTPPath + "/" + rejectedCandidate.ID + "/reject"
	f.request(t, f.handler, "POST", rejectPath, `{"expectedVersion":0}`, f.tokens[0], 400, nil)
	f.request(t, f.handler, "POST", rejectPath, `{"expectedVersion":1}`, f.tokens[1], 404, nil)
	rejected := candidateHTTPRead(t, f.request(t, f.handler, "POST", rejectPath, `{"expectedVersion":1}`, f.tokens[0], 200, nil).Body.Bytes())
	if rejected.Status != agentmemorycandidate.Rejected || len(rejected.Sources) != 0 {
		t.Fatal("rejection failed to clear body")
	}
	f.request(t, f.handler, "POST", rejectPath, `{"expectedVersion":1}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "GET", candidateHTTPPath, `{}`, f.tokens[0], 400, nil)
	off, _ := agentfeature.NewController(agentfeature.DefaultConfig())
	disabled := New(f.store, f.store, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, WithMemoryCandidates(postgres.NewMemoryCandidateHumanGateway(f.store, off)))
	raw, _ = json.Marshal(d)
	f.request(t, disabled, "POST", candidateHTTPPath, string(raw), f.tokens[0], 503, nil)
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1 AND source_type='EXPLICIT'`, f.agentIDs[0]).Scan(&n) != nil || n != 1 {
		t.Fatal("duplicate/automatic Memory")
	}
}
