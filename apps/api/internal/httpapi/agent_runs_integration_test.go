package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"strings"
	"testing"
	"time"
)

func TestAgentRunHTTPNativeRegisteredWaitingGrantCancelIdentityBoundaries(t *testing.T) {
	fixture := retentionHTTPNative(t)
	f := fixture.f
	runs := postgres.NewAgentRuns(f.store, pipelineHTTPFlags(t, false))
	handler := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithAgentRuns(runs))
	base := "/v1/me/agent-runs"
	body := `{"momentId":"` + f.moment.ID + `","retentionGrantId":""}`
	w := f.request(t, handler, "POST", base, body, f.tokens[0], 200, nil)
	rec := enrichmentHTTPData[ar.Record](t, w.Body.Bytes())
	if ar.ValidateRecord(rec) != nil || rec.State != ar.WaitingConfirmation || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "UNSELECTED_HTTP_TITLE") {
		t.Fatal(w.Body.String())
	}
	f.request(t, handler, "GET", base+"/"+rec.ID, "", "", 401, nil)
	f.request(t, handler, "GET", base+"/"+rec.ID, "", f.tokens[1], 403, nil)
	f.request(t, handler, "GET", base+"/"+rec.ID+"?confirmed=true", "", f.tokens[0], 400, nil)
	emptyWorkspace := ""
	f.request(t, handler, "GET", base+"/"+rec.ID, "", f.tokens[0], 403, &emptyWorkspace)
	f.request(t, handler, "POST", base, `{"momentId":"`+f.moment.ID+`","retentionGrantId":"","workerId":"x"}`, f.tokens[0], 400, nil)
	a := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}, SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0])}
	p, e := f.store.PreviewOwnCandidateRetention(f.ctx, a, fixture.selection)
	if e != nil {
		t.Fatal(e)
	}
	g, e := f.store.ApproveOwnCandidateRetention(f.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"expectedVersion": rec.Version, "retentionGrantId": g.ID})
	w = f.request(t, handler, "POST", base+"/"+rec.ID+"/retention-grant", string(raw), f.tokens[0], 200, nil)
	queued := enrichmentHTTPData[ar.Record](t, w.Body.Bytes())
	if ar.ValidateRecord(queued) != nil || queued.State != ar.Queued || queued.ID != rec.ID || !queued.Deadline.Equal(rec.Deadline) {
		t.Fatal(queued)
	}
	// Missing flags is genuine unavailability; a human may still inspect/cancel
	// the same existing Run using authenticated native metadata operations.
	f.request(t, handler, "POST", base+"/"+rec.ID+"/retention-grant", string(raw), f.tokens[0], 409, nil)
	cancel, _ := json.Marshal(map[string]any{"expectedVersion": queued.Version})
	w = f.request(t, handler, "POST", base+"/"+rec.ID+"/cancel", string(cancel), f.tokens[0], 200, nil)
	out := enrichmentHTTPData[ar.Record](t, w.Body.Bytes())
	if out.State != ar.Cancelled || out.Committed || out.CandidateID != "" {
		t.Fatal(out)
	}
	f.request(t, handler, "POST", base+"/"+rec.ID+"/cancel", string(cancel), f.tokens[0], 409, nil)
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memory_candidates WHERE owner_id=$1`, f.accountIDs[0]).Scan(&n); e != nil || n != 0 {
		t.Fatal("metadata wrote a candidate", n, e)
	}
	// Real native authentication invalidation, not a mocked principal response.
	if _, e = f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0]); e != nil {
		t.Fatal(e)
	}
	f.request(t, handler, "GET", base+"/"+rec.ID, "", f.tokens[0], 401, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e = f.pool.Exec(ctx, `DELETE FROM agent_enrichment_runs WHERE owner_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
	})
}

func TestAgentRunHTTPNativeRegisteredRecoveryClosedPermissionAndHistory(t *testing.T) {
	fixture := retentionHTTPNative(t)
	f := fixture.f
	runs := postgres.NewAgentRuns(f.store, pipelineHTTPFlags(t, true))
	handler := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithAgentRuns(runs))
	a := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}, SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0])}
	p, e := f.store.PreviewOwnCandidateRetention(f.ctx, a, fixture.selection)
	if e != nil {
		t.Fatal(e)
	}
	g, e := f.store.ApproveOwnCandidateRetention(f.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	r, e := runs.ScheduleOwn(f.ctx, a, ar.Input{MomentID: f.moment.ID, RetentionGrantID: g.ID})
	if e != nil {
		t.Fatal(e)
	}
	// Five actual native claims; trusted fixture SQL supplies the temporary
	// failure transition. Real lease exhaustion is separately tested in PG.
	for i := int64(1); i <= ar.MaxAttempts; i++ {
		c, e := runs.ClaimAgentRun(f.ctx, "89d59b97-dabe-4345-ac34-dfc73bcbe374")
		if e != nil || c.RunID != r.ID || c.Attempt != i {
			t.Fatal(c, e)
		}
		if _, e = f.pool.Exec(f.ctx, `UPDATE agent_enrichment_runs SET state='RETRY_WAIT',reason='RETRY_UNAVAILABLE',checkpoint='RECONCILE_EFFECT',worker_id=NULL,lease_until=NULL,version=version+1,updated_at=clock_timestamp(),next_attempt_at=clock_timestamp() WHERE id=$1`, r.ID); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = runs.ClaimAgentRun(f.ctx, "89d59b97-dabe-4345-ac34-dfc73bcbe374"); !errors.Is(e, ar.ErrClaimExhausted) {
		t.Fatal(e)
	}
	r, e = runs.ReadOwn(f.ctx, a, r.ID)
	if e != nil || r.State != ar.Failed {
		t.Fatal(r, e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e = f.pool.Exec(ctx, `DELETE FROM agent_enrichment_runs WHERE owner_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
	})
	base := "/v1/me/agent-runs/" + r.ID
	var original string
	snapshot := `SELECT jsonb_build_object('row',to_jsonb(r),'xmin',r.xmin::text,'audit',(SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM agent_run_audit x WHERE x.run_id=r.id))::text FROM agent_enrichment_runs r WHERE r.id=$1`
	if e = f.pool.QueryRow(f.ctx, snapshot, r.ID).Scan(&original); e != nil {
		t.Fatal(e)
	}
	w := f.request(t, handler, "GET", base+"/failure", "", f.tokens[0], 200, nil)
	v := enrichmentHTTPData[ar.FailureView](t, w.Body.Bytes())
	if ar.ValidateFailureView(v) != nil || !v.Recoverable || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	for _, marker := range []string{"UNSELECTED_HTTP_TITLE", "authority_binding", "token_sha256"} {
		if strings.Contains(w.Body.String(), marker) {
			t.Fatal("private raw DLQ", marker)
		}
	}
	w = f.request(t, handler, "GET", "/v1/me/agent-runs/failures", "", f.tokens[0], 200, nil)
	list := enrichmentHTTPData[[]ar.FailureView](t, w.Body.Bytes())
	if len(list) != 1 || list[0].Recoverable {
		t.Fatal(w.Body.String())
	}
	body, _ := json.Marshal(ar.RecoveryInput{ExpectedVersion: r.Version, Reason: ar.RecoveryReason})
	f.request(t, handler, "POST", base+"/recover", string(body), "", 401, nil)
	f.request(t, handler, "POST", base+"/recover", string(body), f.tokens[1], 403, nil)
	emptyOrg := ""
	f.request(t, handler, "POST", base+"/recover", string(body), f.tokens[0], 403, &emptyOrg)
	f.request(t, handler, "GET", base+"/failure?confirmed=true", "", f.tokens[0], 400, nil)
	for _, bad := range []string{`{}`, `null`, `{"expectedVersion":null,"reason":"RETRY_TRANSIENT_NO_EFFECT"}`, `{"expectedVersion":1,"reason":"retry_all"}`, `{"expectedVersion":1,"reason":"RETRY_TRANSIENT_NO_EFFECT","confirmed":true}`, `{"expectedVersion":1,"expectedVersion":1,"reason":"RETRY_TRANSIENT_NO_EFFECT"}`} {
		f.request(t, handler, "POST", base+"/recover", bad, f.tokens[0], 400, nil)
	}
	stale, _ := json.Marshal(ar.RecoveryInput{ExpectedVersion: r.Version + 1, Reason: ar.RecoveryReason})
	f.request(t, handler, "POST", base+"/recover", string(stale), f.tokens[0], 409, nil)
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_enrichment_runs WHERE recovery_root_id=$1`, r.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal("failed requests wrote child", n, e)
	}
	w = f.request(t, handler, "POST", base+"/recover", string(body), f.tokens[0], 200, nil)
	child := enrichmentHTTPData[ar.Record](t, w.Body.Bytes())
	if ar.ValidateRecord(child) != nil || child.Generation != 1 || child.RecoveryRootID != r.ID || child.EventID != r.EventID || child.LogicalOperationID != r.LogicalOperationID || child.Deadline.After(r.Deadline) {
		t.Fatal(w.Body.String())
	}
	f.request(t, handler, "POST", base+"/recover", string(body), f.tokens[0], 409, nil)
	var after string
	if e = f.pool.QueryRow(f.ctx, snapshot, r.ID).Scan(&after); e != nil || after != original {
		t.Fatal("FAILED root history changed", e)
	}
	if _, e = f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0]); e != nil {
		t.Fatal(e)
	}
	f.request(t, handler, "GET", base+"/failure", "", f.tokens[0], 401, nil)
	f.request(t, handler, "POST", base+"/recover", string(body), f.tokens[0], 401, nil)
}
