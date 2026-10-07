package httpapi

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func TestInvalidationPropagationHTTPNativeRegisteredWithdrawAndOriginalReceipt(t *testing.T) {
	v4PrivacyHTTPDatabase(t)
	fixture := retentionHTTPNative(t)
	f := fixture.f
	var db string
	if err := f.pool.QueryRow(f.ctx, `SELECT current_database()`).Scan(&db); err != nil {
		t.Fatal(err)
	}
	t.Logf("AIR018 registered HTTP owned database %s", db)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := f.pool.Exec(ctx, `DELETE FROM agent_effect_ledger WHERE subject_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Error(err)
		}
	})
	flags := pipelineHTTPFlags(t, true)
	handler := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil,
		WithCandidatePipeline(postgres.NewCandidatePipeline(f.store, flags)), WithMemoryCandidates(postgres.NewMemoryCandidateHumanGateway(f.store, flags)))
	w := f.request(t, handler, "POST", candidateRetentionHTTPBase+"/previews", enrichmentHTTPJSON(t, fixture.selection), f.tokens[0], 200, nil)
	preview := enrichmentHTTPData[acr.Preview](t, w.Body.Bytes())
	w = f.request(t, handler, "POST", candidateRetentionHTTPBase+"/previews/"+preview.ID+"/approve", "", f.tokens[0], 200, nil)
	grant := enrichmentHTTPData[acr.Grant](t, w.Body.Bytes())
	base := "/v1/me/agent-candidate-pipeline"
	w = f.request(t, handler, "POST", base+"/stage", `{"retentionGrantId":"`+grant.ID+`"}`, f.tokens[0], 200, nil)
	receipt := enrichmentHTTPData[acp.Receipt](t, w.Body.Bytes())
	if !receipt.Committed || receipt.Candidate == nil {
		t.Fatal("actual registered stage")
	}
	path := "/v1/me/moments/" + f.moment.ID + "?revision=" + strconv.FormatInt(f.selection.MomentRevision, 10)
	var sourceBefore, sourceAfter string
	query := `SELECT jsonb_build_object('moment',(SELECT jsonb_build_object('row',to_jsonb(m),'xmin',m.xmin::text) FROM moments m WHERE id=$1),'markers',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY i.id),'[]') FROM agent_memory_source_invalidations i WHERE source_id=$1))::text`
	if err := f.pool.QueryRow(f.ctx, query, f.moment.ID).Scan(&sourceBefore); err != nil {
		t.Fatal(err)
	}
	f.request(t, handler, "DELETE", path, "", f.tokens[1], 409, nil)
	if err := f.pool.QueryRow(f.ctx, query, f.moment.ID).Scan(&sourceAfter); err != nil || sourceBefore != sourceAfter {
		t.Fatal("other owner withdrawal changed original source/marker", err)
	}
	f.request(t, handler, "DELETE", path, "", f.tokens[0], 204, nil)
	var marker bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_source_invalidations WHERE owner_id=$1 AND source_type='MOMENT' AND source_id=$2 AND source_epoch=$3::bigint::text)`, f.accountIDs[0], f.moment.ID, f.selection.MomentRevision).Scan(&marker); err != nil || !marker {
		t.Fatal("native source writer omitted invalidation marker", err)
	}
	// Receipt remains an original committed fact, never a renewed grant/payload.
	w = f.request(t, handler, "GET", base+"/grants/"+grant.ID+"/receipt", "", f.tokens[0], 200, nil)
	after := enrichmentHTTPData[acp.Receipt](t, w.Body.Bytes())
	if !after.Committed || after.EffectKey != receipt.EffectKey || after.CandidateID != receipt.CandidateID || after.Candidate != nil || strings.Contains(w.Body.String(), "HTTP_RETENTION") {
		t.Fatal("invalidated source escaped through receipt")
	}
	w = f.request(t, handler, "GET", "/v1/me/agent-memory-candidates/"+receipt.CandidateID, "", f.tokens[0], 200, nil)
	var envelope struct {
		Data agentmemorycandidate.Record `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	current := envelope.Data
	if current.Status != agentmemorycandidate.Expired || len(current.Sources) != 0 || current.Category != "" {
		t.Fatal("current HumanRead exposed stale supporting payload")
	}
	f.request(t, handler, "GET", base+"/grants/"+grant.ID+"/receipt", "", f.tokens[1], 403, nil)
	emptyOrg := ""
	f.request(t, handler, "GET", base+"/grants/"+grant.ID+"/receipt", "", f.tokens[0], 403, &emptyOrg)
	var effects, memories int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1),(SELECT count(*) FROM agent_memories WHERE owner_id=$1)`, f.accountIDs[0]).Scan(&effects, &memories); err != nil || effects != 1 || memories != 0 {
		t.Fatal("source invalidation created duplicate effect or automatic Memory", effects, memories, err)
	}
}

func TestInvalidationPropagationHTTPNativeCancelledOriginalClaimCannotWrite(t *testing.T) {
	v4PrivacyHTTPDatabase(t)
	fixture := retentionHTTPNative(t)
	f := fixture.f
	var db string
	if err := f.pool.QueryRow(f.ctx, `SELECT current_database()`).Scan(&db); err != nil {
		t.Fatal(err)
	}
	t.Logf("AIR018 registered HTTP owned database %s", db)
	flags := pipelineHTTPFlags(t, true)
	runs := postgres.NewAgentRuns(f.store, flags)
	handler := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithAgentRuns(runs))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := f.pool.Exec(ctx, `DELETE FROM agent_enrichment_runs WHERE owner_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Error(err)
		}
	})
	w := f.request(t, handler, "POST", "/v1/me/agent-runs", `{"momentId":"`+f.moment.ID+`","retentionGrantId":""}`, f.tokens[0], 200, nil)
	r := enrichmentHTTPData[ar.Record](t, w.Body.Bytes())
	if r.State != ar.WaitingConfirmation {
		t.Fatal("actual native waiting run")
	}
	f.request(t, handler, "GET", "/v1/me/agent-runs/"+r.ID, "", f.tokens[1], 403, nil)
	w = f.request(t, handler, "POST", "/v1/me/agent-runs/"+r.ID+"/cancel", `{"expectedVersion":`+strconv.FormatInt(r.Version, 10)+`}`, f.tokens[0], 200, nil)
	c := enrichmentHTTPData[ar.Record](t, w.Body.Bytes())
	if c.ID != r.ID || c.State != ar.Cancelled || c.Version != r.Version+1 {
		t.Fatal("cancellation lost original identity")
	}
	f.request(t, handler, "POST", "/v1/me/agent-runs/"+r.ID+"/cancel", `{"expectedVersion":`+strconv.FormatInt(c.Version, 10)+`}`, f.tokens[0], 409, nil)
	if _, err := runs.ClaimAgentRun(f.ctx, "89d59b97-dabe-4345-ac34-dfc73bcbe374"); err == nil {
		t.Fatal("cancelled run returned a dispatch claim")
	}
	var audits, candidates, effects int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM agent_run_audit WHERE run_id=$1 AND to_state='CANCELLED'),(SELECT count(*) FROM agent_memory_candidates WHERE owner_id=$2),(SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$2)`, r.ID, f.accountIDs[0]).Scan(&audits, &candidates, &effects); err != nil || audits != 1 || candidates != 0 || effects != 0 {
		t.Fatal("cancel audit or zero-effect boundary", audits, candidates, effects, err)
	}
}
