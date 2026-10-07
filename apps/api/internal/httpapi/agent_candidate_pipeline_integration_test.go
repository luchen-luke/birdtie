package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func pipelineHTTPFlags(t *testing.T, on bool) *agentfeature.Controller {
	t.Helper()
	cfg := agentfeature.DefaultConfig()
	if on {
		var e error
		cfg, e = agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
		if e != nil {
			t.Fatal(e)
		}
	}
	c, e := agentfeature.NewController(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

// The native handler really commits, then the actual loopback connection is
// closed before any response. Recovery reads the original approved grant ID.
func TestCandidatePipelineHTTPNativeLostResponseUsesOriginalReceipt(t *testing.T) {
	fixture := retentionHTTPNative(t)
	f := fixture.f
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := f.pool.Exec(ctx, `DELETE FROM agent_effect_ledger WHERE subject_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
	})
	handler := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithCandidatePipeline(postgres.NewCandidatePipeline(f.store, pipelineHTTPFlags(t, true))))
	a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	p, e := f.store.PreviewOwnCandidateRetention(f.ctx, a, fixture.selection)
	if e != nil {
		t.Fatal(e)
	}
	g, e := f.store.ApproveOwnCandidateRetention(f.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	base := "/v1/me/agent-candidate-pipeline"
	var dropped atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == base+"/stage" && !dropped.Swap(true) {
			record := httptest.NewRecorder()
			handler.ServeHTTP(record, r)
			if record.Code != 200 {
				w.WriteHeader(record.Code)
				w.Write(record.Body.Bytes())
				return
			}
			hijack, ok := w.(http.Hijacker)
			if !ok {
				t.Error("actual transport cannot hijack")
				return
			}
			conn, _, e := hijack.Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			conn.Close()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	client := srv.Client()
	client.Timeout = 10 * time.Second
	request := func(method, path, body string) (*http.Response, error) {
		r, e := http.NewRequestWithContext(f.ctx, method, srv.URL+path, strings.NewReader(body))
		if e != nil {
			return nil, e
		}
		r.Header.Set("Authorization", "Bearer "+f.tokens[0])
		r.Header.Set("Content-Type", "application/json")
		return client.Do(r)
	}
	if response, e := request("POST", base+"/stage", `{"retentionGrantId":"`+g.ID+`"}`); e == nil {
		response.Body.Close()
		t.Fatal("lost response should be actual network error")
	}
	response, e := request("GET", base+"/grants/"+g.ID+"/receipt", "")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil || response.StatusCode != 200 {
		t.Fatal(response.StatusCode, string(raw), e)
	}
	receipt := enrichmentHTTPData[acp.Receipt](t, raw)
	if acp.ValidateReceipt(receipt) != nil || !receipt.Committed || receipt.Candidate == nil {
		t.Fatal(receipt)
	}
	var before string
	query := `SELECT jsonb_build_object('candidate',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY c.id),'[]') FROM agent_memory_candidates c WHERE owner_id=$1),'effect',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY e.effect_key),'[]') FROM agent_effect_ledger e WHERE subject_id=$1),'inbox',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY i.event_id,i.handler_version),'[]') FROM agent_consumer_inbox i WHERE subject_id=$1),'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(d) ORDER BY d.event_id),'[]') FROM agent_domain_outbox d WHERE subject_id=$1))::text`
	if e = f.pool.QueryRow(f.ctx, query, f.accountIDs[0]).Scan(&before); e != nil {
		t.Fatal(e)
	}
	response, e = request("POST", base+"/stage", `{"retentionGrantId":"`+g.ID+`"}`)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil || response.StatusCode != 200 {
		t.Fatal(string(raw), e)
	}
	retry := enrichmentHTTPData[acp.Receipt](t, raw)
	if retry.CandidateID != receipt.CandidateID || retry.EffectKey != receipt.EffectKey {
		t.Fatal("unknown response created another effect", retry)
	}
	var after string
	if e = f.pool.QueryRow(f.ctx, query, f.accountIDs[0]).Scan(&after); e != nil || after != before {
		t.Fatal("retry changed candidate/effect/checkpoints", e)
	}
}
func TestCandidatePipelineHTTPNativeRegisteredStageAndRead(t *testing.T) {
	fixture := retentionHTTPNative(t)
	f := fixture.f
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := f.pool.Exec(ctx, `DELETE FROM agent_effect_ledger WHERE subject_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
	})
	handler := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithCandidatePipeline(postgres.NewCandidatePipeline(f.store, pipelineHTTPFlags(t, true))))
	w := f.request(t, handler, "POST", candidateRetentionHTTPBase+"/previews", enrichmentHTTPJSON(t, fixture.selection), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[acr.Preview](t, w.Body.Bytes())
	w = f.request(t, handler, "POST", candidateRetentionHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	g := enrichmentHTTPData[acr.Grant](t, w.Body.Bytes())
	base := "/v1/me/agent-candidate-pipeline"
	body := `{"retentionGrantId":"` + g.ID + `"}`
	f.request(t, handler, "POST", base+"/stage", body, "", 401, nil)
	f.request(t, handler, "POST", base+"/stage", body, f.tokens[1], 403, nil)
	f.request(t, handler, "POST", base+"/stage", body, f.tokens[0], 403, &f.accountIDs[1])
	f.request(t, handler, "POST", base+"/stage?confirmed=true", body, f.tokens[0], 400, nil)
	for _, bad := range []string{`{"retentionGrantId":"` + g.ID + `","confirmed":true}`, `{"retentionGrantId":"` + g.ID + `","handler":"mom-candidate-local-v2"}`, `{"retentionGrantId":"` + g.ID + `","retentionGrantId":"` + g.ID + `"}`, `{"retentionGrantId":"` + fixture.selection.AnalysisGrantID + `"}`} {
		status := 400
		if bad == `{"retentionGrantId":"`+fixture.selection.AnalysisGrantID+`"}` {
			status = 403
		}
		f.request(t, handler, "POST", base+"/stage", bad, f.tokens[0], status, nil)
	}
	off := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithCandidatePipeline(postgres.NewCandidatePipeline(f.store, pipelineHTTPFlags(t, false))))
	f.request(t, off, "POST", base+"/stage", body, f.tokens[0], 503, nil)
	f.request(t, New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil), "POST", base+"/stage", body, f.tokens[0], 503, nil)
	w = f.request(t, handler, "GET", base+"/grants/"+g.ID+"/receipt", "", f.tokens[0], 200, nil)
	if r := enrichmentHTTPData[acp.Receipt](t, w.Body.Bytes()); r.State != "NOT_STAGED" || r.Committed {
		t.Fatal(r)
	}
	w = f.request(t, handler, "POST", base+"/stage", body, f.tokens[0], 200, nil)
	r := enrichmentHTTPData[acp.Receipt](t, w.Body.Bytes())
	if acp.ValidateReceipt(r) != nil || r.Candidate == nil || r.Candidate.Assessment.Level != "LOW" || r.ModelAccess || r.MemoryPromotionAllowed || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	t.Log("REGISTERED_CANDIDATE_PIPELINE_WIRE", w.Body.String())
	f.request(t, handler, "POST", base+"/stage", body, f.tokens[0], 200, nil)
	f.request(t, off, "GET", base+"/grants/"+g.ID+"/receipt", "", f.tokens[0], 200, nil)
	f.request(t, handler, "DELETE", candidateRetentionHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	w = f.request(t, off, "GET", base+"/grants/"+g.ID+"/receipt", "", f.tokens[0], 200, nil)
	old := enrichmentHTTPData[acp.Receipt](t, w.Body.Bytes())
	if old.Candidate != nil || old.CandidateID != r.CandidateID || !old.Committed {
		t.Fatal(old)
	}
	f.request(t, handler, "POST", base+"/stage", body, f.tokens[0], 403, nil)
}
