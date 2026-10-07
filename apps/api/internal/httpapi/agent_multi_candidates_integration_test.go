package httpapi

import (
	"context"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	amc "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const multiHTTPBase = "/v1/me/agent-multi-candidates"

func TestMultiCandidateHTTPNativeHikingV2RegisteredWireAndNoImplicitPromotion(t *testing.T) {
	f, s, h := multiHTTPNative(t, "周末徒步记录", "Hiking after class")
	w := f.request(t, h, "POST", multiHTTPBase+"/previews", enrichmentHTTPJSON(t, s), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[amc.Preview](t, w.Body.Bytes())
	if p.Review == nil || p.Review.Proposal.Category != "hiking" || p.Review.Proposal.AlgorithmVersion != "moment-lexical-category-v2" || p.Review.Proposal.Assessment.Value != nil || p.Review.Proposal.Assessment.Level != "LOW" {
		t.Fatal("transparent hiking wire", w.Body.String())
	}
	t.Log("HIKING_REGISTERED_PREVIEW_RAW_WIRE", w.Body.String())
	w = f.request(t, h, "POST", multiHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	g := enrichmentHTTPData[amc.Grant](t, w.Body.Bytes())
	w = f.request(t, h, "POST", multiHTTPBase+"/stage", `{"retentionGrantId":"`+g.ID+`"}`, f.tokens[0], 200, nil)
	r := enrichmentHTTPData[amc.Receipt](t, w.Body.Bytes())
	if r.Candidate == nil || r.Candidate.Status != "CANDIDATE" || r.Candidate.Category != "hiking" || r.Candidate.MemoryID != nil {
		t.Fatal("no automatic ACTIVE", w.Body.String())
	}
	t.Log("HIKING_REGISTERED_RECEIPT_RAW_WIRE", w.Body.String())
	f.request(t, h, "GET", multiHTTPBase+"/grants/"+g.ID+"/receipt", "", f.tokens[1], 403, nil)
}

func multiHTTPNative(t *testing.T, bodies ...string) (*enrichmentHTTPFixture, amc.Selection, http.Handler) {
	t.Helper()
	v4PrivacyHTTPDatabase(t)
	single := retentionHTTPNative(t)
	f := single.f
	var database string
	if e := f.pool.QueryRow(f.ctx, `SELECT current_database()`).Scan(&database); e != nil {
		t.Fatal(e)
	}
	t.Log("MULTI owned registered HTTP database", database)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM agent_effect_ledger WHERE subject_id=ANY($1::uuid[])`, `DELETE FROM agent_memory_candidates WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_multi_candidate_bindings WHERE grant_id IN(SELECT id FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`, `DELETE FROM agent_multi_candidate_previews WHERE owner_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	if len(bodies) != 0 && len(bodies) != 2 {
		t.Fatal("need two explicitly selected private texts")
	}
	firstGrant := single.selection.AnalysisGrantID
	secondBody := "另一条羽毛球原生记录_MULTI_HTTP_PRIVATE_CANARY"
	if len(bodies) == 2 {
		m, e := f.store.UpdateMomentDraft(f.ctx, f.accountIDs[0], f.selection.MomentID, f.selection.MomentRevision, content.MomentInput{CityID: f.city, Title: f.moment.Title, Body: bodies[0], TimePrecision: "unknown", LocationPrecision: "city"})
		if e != nil {
			t.Fatal(e)
		}
		f.moment = m
		f.selection.MomentRevision = m.Revision
		p, e := f.store.PreviewOwnEnrichmentPurpose(f.ctx, a, f.selection)
		if e != nil {
			t.Fatal(e)
		}
		g, e := f.store.ApproveOwnEnrichmentPurpose(f.ctx, a, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		firstGrant = g.ID
		secondBody = bodies[1]
	}
	m, e := f.store.CreateMomentDraft(f.ctx, f.accountIDs[0], content.MomentInput{CityID: f.city, Title: "MULTI_HTTP_UNSELECTED_TITLE_CANARY", Body: secondBody, TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	s := f.selection
	s.MomentID = m.ID
	s.MomentRevision = m.Revision
	p, e := f.store.PreviewOwnEnrichmentPurpose(f.ctx, a, s)
	if e != nil {
		t.Fatal(e)
	}
	g, e := f.store.ApproveOwnEnrichmentPurpose(f.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	selection := amc.Selection{AnalysisGrantIDs: []string{firstGrant, g.ID}, RetainUntil: single.selection.RetainUntil}
	h := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithMultiCandidates(postgres.NewMultiCandidatePipeline(f.store, pipelineHTTPFlags(t, true))))
	return f, selection, h
}
func TestMultiCandidateHTTPNativeRegisteredSevenRoutesAndClosedTransport(t *testing.T) {
	f, s, h := multiHTTPNative(t)
	body := enrichmentHTTPJSON(t, s)
	f.request(t, h, "POST", multiHTTPBase+"/previews", body, "", 401, nil)
	f.request(t, h, "POST", multiHTTPBase+"/previews", body, f.tokens[0], 403, &f.accountIDs[1])
	f.request(t, h, "POST", multiHTTPBase+"/previews?confirmed=true", body, f.tokens[0], 400, nil)
	for _, bad := range []string{`{"confirmed":true}`, strings.TrimSuffix(body, "}") + `,"category":"badminton"}`, strings.TrimSuffix(body, "}") + `,"purpose":"STAGE_MEMORY_CANDIDATE"}`} {
		f.request(t, h, "POST", multiHTTPBase+"/previews", bad, f.tokens[0], 400, nil)
	}
	w := f.request(t, h, "POST", multiHTTPBase+"/previews", body, f.tokens[0], 200, nil)
	p := enrichmentHTTPData[amc.Preview](t, w.Body.Bytes())
	if amc.ValidatePreview(p) != nil || p.Review.Clusters != 2 || strings.Contains(w.Body.String(), "PRIVATE_CANARY") {
		t.Fatal(w.Body.String())
	}
	t.Log("MULTI_REGISTERED_PREVIEW_RAW_WIRE", w.Body.String())
	f.request(t, h, "GET", multiHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil)
	f.request(t, h, "POST", multiHTTPBase+"/previews/"+p.ID+"/approve", `{"confirmed":true}`, f.tokens[0], 400, nil)
	w = f.request(t, h, "POST", multiHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	g := enrichmentHTTPData[amc.Grant](t, w.Body.Bytes())
	if amc.ValidateGrant(g) != nil {
		t.Fatal(w.Body.String())
	}
	again := enrichmentHTTPData[amc.Grant](t, f.request(t, h, "POST", multiHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil).Body.Bytes())
	if again.ID != g.ID || !again.ExpiresAt.Equal(g.ExpiresAt) {
		t.Fatal("retry renewed grant")
	}
	f.request(t, h, "GET", multiHTTPBase+"/grants/"+g.ID, "", f.tokens[1], 403, nil)
	f.request(t, h, "GET", multiHTTPBase+"/grants/"+g.ID, "", f.tokens[0], 200, nil)
	stage := `{"retentionGrantId":"` + g.ID + `"}`
	f.request(t, h, "POST", multiHTTPBase+"/stage", `{"retentionGrantId":"`+s.AnalysisGrantIDs[0]+`"}`, f.tokens[0], 403, nil)
	off := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithMultiCandidates(postgres.NewMultiCandidatePipeline(f.store, pipelineHTTPFlags(t, false))))
	f.request(t, off, "POST", multiHTTPBase+"/stage", stage, f.tokens[0], 503, nil)
	w = f.request(t, h, "POST", multiHTTPBase+"/stage", stage, f.tokens[0], 200, nil)
	r := enrichmentHTTPData[amc.Receipt](t, w.Body.Bytes())
	if amc.ValidateReceipt(r) != nil || r.Candidate == nil || len(r.Candidate.Sources) != 2 || r.MemoryPromotionAllowed || r.ModelAccess {
		t.Fatal(w.Body.String())
	}
	t.Log("MULTI_REGISTERED_RECEIPT_RAW_WIRE", w.Body.String())
	f.request(t, h, "GET", multiHTTPBase+"/grants/"+g.ID+"/receipt", "", f.tokens[0], 200, nil)
	w = f.request(t, h, "DELETE", multiHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	rev := enrichmentHTTPData[amc.Grant](t, w.Body.Bytes())
	if rev.RevokedAt == nil || rev.Revision != 2 {
		t.Fatal(w.Body.String())
	}
	w = f.request(t, h, "GET", multiHTTPBase+"/grants/"+g.ID+"/receipt", "", f.tokens[0], 200, nil)
	rr := enrichmentHTTPData[amc.Receipt](t, w.Body.Bytes())
	if rr.Candidate != nil || !rr.Committed || rr.CandidateID != r.CandidateID {
		t.Fatal("revoked support leaked", w.Body.String())
	}
	var status string
	var raw string
	if e := f.pool.QueryRow(f.ctx, `SELECT status,to_jsonb(c)::text FROM agent_memory_candidates c WHERE id=$1`, r.CandidateID).Scan(&status, &raw); e != nil || status != "EXPIRED" || strings.Contains(raw, "badminton") {
		t.Fatal("revoke did not clear support", status, e)
	}
}
func TestMultiCandidateHTTPNativeUnknownOriginalGrantReconciliationNoReplay(t *testing.T) {
	f, s, h := multiHTTPNative(t)
	p := enrichmentHTTPData[amc.Preview](t, f.request(t, h, "POST", multiHTTPBase+"/previews", enrichmentHTTPJSON(t, s), f.tokens[0], 200, nil).Body.Bytes())
	g := enrichmentHTTPData[amc.Grant](t, f.request(t, h, "POST", multiHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil).Body.Bytes())
	var dropped atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == multiHTTPBase+"/stage" && !dropped.Swap(true) {
			record := httptest.NewRecorder()
			h.ServeHTTP(record, r)
			if record.Code != 200 {
				w.WriteHeader(record.Code)
				w.Write(record.Body.Bytes())
				return
			}
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			conn.Close()
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 12 * time.Second
	call := func(method, path, body string) (int, []byte, error) {
		req, e := http.NewRequestWithContext(f.ctx, method, server.URL+path, strings.NewReader(body))
		if e != nil {
			return 0, nil, e
		}
		req.Header.Set("Authorization", "Bearer "+f.tokens[0])
		req.Header.Set("Content-Type", "application/json")
		response, e := client.Do(req)
		if e != nil {
			return 0, nil, e
		}
		defer response.Body.Close()
		raw, e := io.ReadAll(response.Body)
		return response.StatusCode, raw, e
	}
	if _, _, e := call("POST", multiHTTPBase+"/stage", `{"retentionGrantId":"`+g.ID+`"}`); e == nil {
		t.Fatal("expected actual lost response")
	}
	code, raw, e := call("GET", multiHTTPBase+"/grants/"+g.ID+"/receipt", "")
	if e != nil || code != 200 {
		t.Fatal(code, e, string(raw))
	}
	r := enrichmentHTTPData[amc.Receipt](t, raw)
	if !r.Committed || r.Candidate == nil {
		t.Fatal(string(raw))
	}
	// A new service object is not an OS restart. It proves receipt is native
	// persistent metadata rather than an in-process reply cache.
	h = New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithMultiCandidates(postgres.NewMultiCandidatePipeline(f.store, pipelineHTTPFlags(t, true))))
	recovered := enrichmentHTTPData[amc.Receipt](t, f.request(t, h, "GET", multiHTTPBase+"/grants/"+g.ID+"/receipt", "", f.tokens[0], 200, nil).Body.Bytes())
	if recovered.CandidateID != r.CandidateID || recovered.EffectKey != r.EffectKey {
		t.Fatal("native receipt changed")
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1`, f.accountIDs[0]).Scan(&n); e != nil || n != 1 {
		t.Fatal("unknown created duplicate", n, e)
	}
	var before string
	query := `SELECT jsonb_build_object('candidate',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]') FROM agent_memory_candidates c WHERE owner_id=$1),'effect',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY effect_key),'[]') FROM agent_effect_ledger e WHERE subject_id=$1),'inbox',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY event_id,handler_version),'[]') FROM agent_consumer_inbox i WHERE subject_id=$1),'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(d) ORDER BY event_id),'[]') FROM agent_domain_outbox d WHERE subject_id=$1))::text`
	if e = f.pool.QueryRow(f.ctx, query, f.accountIDs[0]).Scan(&before); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") || cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("owned loopback process boundary", e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for round := 1; round <= 2; round++ {
		cmd := exec.CommandContext(f.ctx, exe, "-test.run=^TestMultiCandidateHTTPNativeReceiptRestartChild$", "-test.v")
		cmd.Env = append(os.Environ(), "BIRDTIE_MULTI_CHILD=owned-native-http", "BIRDTIE_MULTI_DB="+cfg.ConnConfig.Database, "BIRDTIE_MULTI_TOKEN="+f.tokens[0], "BIRDTIE_MULTI_GRANT="+g.ID, "BIRDTIE_MULTI_CANDIDATE="+r.CandidateID, "BIRDTIE_MULTI_EFFECT="+r.EffectKey)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal("actual OS metadata recovery", round, e, string(out))
		}
		t.Logf("multi original receipt independent OS round=%d %s", round, strings.TrimSpace(string(out)))
		var after string
		if e = f.pool.QueryRow(f.ctx, query, f.accountIDs[0]).Scan(&after); e != nil || after != before {
			t.Fatal("OS receipt recovery changed domain", e)
		}
	}
}

func TestMultiCandidateHTTPNativeReceiptRestartChild(t *testing.T) {
	if os.Getenv("BIRDTIE_MULTI_CHILD") == "" {
		return
	}
	if os.Getenv("BIRDTIE_MULTI_CHILD") != "owned-native-http" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("child ownership")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_MULTI_DB") || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") || cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("owned fresh child only", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := postgres.New(pool, false)
	h := New(s, s, nil, s, s, s, s, s, s, s, s, nil, false, nil, nil, nil, WithMultiCandidates(postgres.NewMultiCandidatePipeline(s, pipelineHTTPFlags(t, false))))
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, e := http.NewRequestWithContext(ctx, "GET", srv.URL+multiHTTPBase+"/grants/"+os.Getenv("BIRDTIE_MULTI_GRANT")+"/receipt", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("BIRDTIE_MULTI_TOKEN"))
	req.Header.Set("X-Request-ID", fmt.Sprintf("multi_restart_%d", os.Getpid()))
	response, e := srv.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(response.Body)
	if e != nil || response.StatusCode != 200 {
		t.Fatal("registered original receipt", response.StatusCode, e)
	}
	receipt := enrichmentHTTPData[amc.Receipt](t, raw)
	if amc.ValidateReceipt(receipt) != nil || !receipt.Committed || receipt.CandidateID != os.Getenv("BIRDTIE_MULTI_CANDIDATE") || receipt.EffectKey != os.Getenv("BIRDTIE_MULTI_EFFECT") {
		t.Fatal("OS original receipt mismatch")
	}
	t.Logf("actual OS process pid=%d HTTP200 originalGrant originalCandidate originalEffect defaultFeatureOFF noDispatch", os.Getpid())
}

func TestMultiPreviewReceiptHTTPNativeRegisteredBodylessApproval(t *testing.T) {
	f, sel, h := multiHTTPNative(t)
	w := f.request(t, h, "POST", multiHTTPBase+"/previews", enrichmentHTTPJSON(t, sel), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[amc.Preview](t, w.Body.Bytes())
	path := multiHTTPBase + "/previews/" + p.ID + "/receipt"
	w = f.request(t, h, "GET", path, "", f.tokens[0], 200, nil)
	r := enrichmentHTTPData[amc.PreviewReceipt](t, w.Body.Bytes())
	if amc.ValidatePreviewReceipt(r) != nil || r.State != "OPEN_UNCONSUMED" {
		t.Fatal(w.Body.String())
	}
	f.request(t, h, "POST", multiHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	if _, e := f.pool.Exec(f.ctx, `UPDATE moments SET body='MULTI_RECEIPT_HIDDEN_HTTP',revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.moment.ID); e != nil {
		t.Fatal(e)
	}
	w = f.request(t, h, "GET", path, "", f.tokens[0], 200, nil)
	r = enrichmentHTTPData[amc.PreviewReceipt](t, w.Body.Bytes())
	if amc.ValidatePreviewReceipt(r) != nil || r.State != "APPROVAL_RECORDED" || r.ConsumedGrantID == "" {
		t.Fatal(w.Body.String())
	}
	for _, bad := range []string{`"selection"`, `"review"`, `"sourceSelections"`, f.moment.Body, "MULTI_RECEIPT_HIDDEN_HTTP"} {
		if strings.Contains(w.Body.String(), bad) {
			t.Fatal("multi history wire leaked")
		}
	}
	t.Log("HISTORICAL_MULTI_PREVIEW_RECEIPT_RAW_WIRE", w.Body.String())
	f.request(t, h, "GET", path, "", f.tokens[1], 403, nil)
	f.request(t, h, "GET", path+"?confirm=true", "", f.tokens[0], 400, nil)
	org := f.accountIDs[0]
	f.request(t, h, "GET", path, "", f.tokens[0], 403, &org)
}
