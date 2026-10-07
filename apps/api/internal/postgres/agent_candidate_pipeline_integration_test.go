package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentlocalcandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func pipelineNative(t *testing.T) *retentionFixture {
	t.Helper()
	f := retentionNative(t)
	b := f.f.f.place.private.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := b.pool.Exec(ctx, `DELETE FROM agent_effect_ledger WHERE subject_id=ANY($1::uuid[])`, b.accounts); e != nil {
			t.Error(e)
		}
	})
	return f
}
func pipelineFlags(t *testing.T, on bool) *agentfeature.Controller {
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
func pipelineCount(t *testing.T, f *retentionFixture) string {
	t.Helper()
	b := f.f.f.place.private.base
	var raw string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('candidates',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY c.id),'[]') FROM agent_memory_candidates c WHERE owner_id=$1),'effects',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY effect_key),'[]') FROM agent_effect_ledger e WHERE subject_id=$1),'inbox',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY event_id,handler_version),'[]') FROM agent_consumer_inbox i WHERE subject_id=$1),'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY event_id),'[]') FROM agent_domain_outbox o WHERE subject_id=$1),'memory',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]') FROM agent_memories m WHERE owner_id=$1))::text`, b.personID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestCandidatePipelineNativeHikingV2SingleSourceRemainsPending(t *testing.T) {
	ownedMigrationDatabase(t)
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	m, e := b.store.UpdateMomentDraft(b.ctx, b.person.ID, f.f.moment.ID, f.f.moment.Revision, content.MomentInput{CityID: f.f.f.place.city, Title: f.f.moment.Title, Body: "本人所选徒步记录", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	f.f.moment = m
	f.f.selection.MomentRevision = m.Revision
	_, ag := f.f.approve(t)
	f.selection.AnalysisGrantID = ag.ID
	p, g := f.approve(t)
	if p.Review.Proposal.Category != "hiking" || p.Review.Proposal.AlgorithmVersion != agentlocalcandidate.Version || p.Review.Clusters != 1 {
		t.Fatal("specific single source review", p.Review)
	}
	r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
	if e != nil || r.Candidate == nil || r.Candidate.Category != "hiking" || r.Candidate.Status != "CANDIDATE" || r.Candidate.MemoryID != nil || r.Candidate.Assessment.Value != nil {
		t.Fatal("single source must remain pending", e)
	}
	var mid string
	if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&mid); e != nil {
		t.Fatal(e)
	}
	if _, e = NewMemoryCandidateService(b.store, pipelineFlags(t, true)).PreviewOwnAcceptance(b.ctx, a, r.CandidateID, 1, mid, 0, time.Now().UTC().Add(time.Hour)); e == nil {
		t.Fatal("one source cannot bypass two independent clusters")
	}
}

func TestCandidatePipelineNativeExactlyOnce100AndHandlerUpgrade(t *testing.T) {
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	_, g := f.approve(t)
	off := NewCandidatePipeline(b.store, pipelineFlags(t, false))
	before := pipelineCount(t, f)
	if _, e := off.StageOwnMomentCandidate(b.ctx, a, g.ID); !errors.Is(e, acp.ErrUnavailable) {
		t.Fatal(e)
	}
	if pipelineCount(t, f) != before {
		t.Fatal("OFF wrote domain")
	}
	flags := pipelineFlags(t, true)
	service := NewCandidatePipeline(b.store, flags)
	r, e := service.StageOwnMomentCandidate(b.ctx, a, g.ID)
	if e != nil {
		t.Fatal("stage", e)
	}
	if acp.ValidateReceipt(r) != nil || r.Candidate == nil || r.Candidate.Status != "CANDIDATE" || r.Candidate.Assessment.Level != "LOW" || r.Candidate.MemoryID != nil {
		t.Fatal(r)
	}
	stable := pipelineCount(t, f)
	for i := 0; i < 100; i++ {
		retry, e := service.StageOwnMomentCandidate(b.ctx, a, g.ID)
		if e != nil || retry.CandidateID != r.CandidateID || retry.EffectKey != r.EffectKey {
			t.Fatal(i, retry, e)
		}
	}
	v2 := acp.NewServiceWithHandler(&candidatePipelineExecutor{store: b.store}, flags, acp.LocalV2)
	retry, e := v2.StageOwnMomentCandidate(b.ctx, a, g.ID)
	if e != nil || retry.HandlerVersion != r.HandlerVersion || retry.EffectKey != r.EffectKey {
		t.Fatal(retry, e)
	}
	if pipelineCount(t, f) != stable {
		t.Fatal("retry/upgrade renewed or duplicated")
	}
	read, e := off.ReadOwnMomentCandidateReceipt(b.ctx, a, g.ID)
	if e != nil || read.CandidateID != r.CandidateID {
		t.Fatal(read, e)
	}
	if pipelineCount(t, f) != stable {
		t.Fatal("receipt GET wrote")
	}
	var proof *string
	var state string
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT proof_review FROM agent_effect_ledger WHERE subject_id=$1`, a.WorkspacePrincipal.ID).Scan(&proof); e != nil || proof != nil {
		t.Fatal("persisted proof", proof, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT delivery_state FROM agent_domain_outbox WHERE event_id=$1`, r.EventID).Scan(&state); e != nil || state != "CANDIDATE_STAGED" {
		t.Fatal(state, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE owner_id=$1`, a.WorkspacePrincipal.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("automatic Memory", count, e)
	}
	if _, e = b.store.RevokeOwnCandidateRetention(b.ctx, a, g.ID, 1); e != nil {
		t.Fatal(e)
	}
	var candidateRaw string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(c)::text FROM agent_memory_candidates c WHERE id=$1`, r.CandidateID).Scan(&candidateRaw); e != nil || strings.Contains(candidateRaw, "badminton") || !strings.Contains(candidateRaw, "EXPIRED") {
		t.Fatal("revoked payload retained", candidateRaw, e)
	}
	read, e = off.ReadOwnMomentCandidateReceipt(b.ctx, a, g.ID)
	if e != nil || read.Candidate != nil || !read.Committed {
		t.Fatal("metadata reconciliation", read, e)
	}
}
func TestCandidatePipelineNativeNewGrantCannotRenewOldLogicalEffect(t *testing.T) {
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	_, g := f.approve(t)
	svc := NewCandidatePipeline(b.store, pipelineFlags(t, true))
	r, e := svc.StageOwnMomentCandidate(b.ctx, a, g.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, second := f.approve(t)
	if second.ID == g.ID {
		t.Fatal("must use actual new explicit preview/grant")
	}
	before := pipelineCount(t, f)
	if next, e := svc.StageOwnMomentCandidate(b.ctx, a, second.ID); e == nil || !reflectReceiptZero(next) {
		t.Fatal("new grant attached existing effect as new commit", next, e)
	}
	if rec, e := svc.ReadOwnMomentCandidateReceipt(b.ctx, a, second.ID); e != nil || rec.Committed || rec.State != "NOT_STAGED" {
		t.Fatal("new grant inherited original receipt", rec, e)
	}
	if pipelineCount(t, f) != before {
		t.Fatal("renewed/duplicated original logical effect")
	}
	if old, e := svc.ReadOwnMomentCandidateReceipt(b.ctx, a, g.ID); e != nil || old.CandidateID != r.CandidateID {
		t.Fatal(old, e)
	}
}

func TestCandidatePipelineNativeFaultAtomicityAndControllerLate(t *testing.T) {
	for _, point := range []string{"after_claim", "after_candidate", "after_effect", "after_inbox", "after_checkpoint", "before_commit", "controller_ABA", "expiry"} {
		t.Run(point, func(t *testing.T) {
			f := pipelineNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			if point == "expiry" {
				f.selection.RetainUntil = time.Now().UTC().Truncate(time.Microsecond).Add(350 * time.Millisecond)
			}
			_, g := f.approve(t)
			flags := pipelineFlags(t, true)
			before := pipelineCount(t, f)
			sentinel := errors.New("actual native fault")
			x := &candidatePipelineExecutor{store: b.store, hook: func(p string) error {
				if point == "controller_ABA" && p == "before_commit" {
					flags.Disable(agentfeature.Memory)
					cfg, e := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
					if e != nil {
						return e
					}
					return flags.Replace(flags.Revision(), cfg)
				}
				if point == "expiry" && p == "before_commit" {
					time.Sleep(450 * time.Millisecond)
					return nil
				}
				if p == point {
					return sentinel
				}
				return nil
			}}
			out, e := acp.NewService(x, flags).StageOwnMomentCandidate(b.ctx, a, g.ID)
			if e == nil || !reflectReceiptZero(out) {
				t.Fatal("fault released receipt", out, e)
			}
			if pipelineCount(t, f) != before {
				t.Fatal("partial candidate/effect/control commit", point)
			}
			if point != "expiry" {
				r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
				if e != nil || !r.Committed {
					t.Fatal("recover original event", r, e)
				}
			}
		})
	}
}
func reflectReceiptZero(r acp.Receipt) bool {
	raw, _ := json.Marshal(r)
	zero, _ := json.Marshal(acp.Receipt{})
	return string(raw) == string(zero)
}
func TestCandidatePipelineNativeTwoWorkersAndOriginalPurpose(t *testing.T) {
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	_, g := f.approve(t)
	flags := pipelineFlags(t, true)
	var wg sync.WaitGroup
	results := make(chan acp.Receipt, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := NewCandidatePipeline(b.store, flags).StageOwnMomentCandidate(b.ctx, a, g.ID)
			if e != nil {
				failures <- e
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	var id string
	for r := range results {
		if id != "" && id != r.CandidateID {
			t.Fatal("two candidates")
		}
		id = r.CandidateID
	}
	if id == "" {
		t.Fatal("neither worker committed")
	}
	for e := range failures {
		if !errors.Is(e, acp.ErrBusy) {
			t.Fatal(e)
		}
	}
	before := pipelineCount(t, f)
	for _, wrong := range []string{f.selection.AnalysisGrantID, f.f.f.task.ID, f.f.moment.ID} {
		if r, e := NewCandidatePipeline(b.store, flags).StageOwnMomentCandidate(b.ctx, a, wrong); e == nil || !reflectReceiptZero(r) {
			t.Fatal("cross-purpose", r, e)
		}
	}
	if pipelineCount(t, f) != before {
		t.Fatal("denied wrote")
	}
}
func TestCandidatePipelineNativeSubmitterIsActualAndRefsCannotGrant(t *testing.T) {
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	flags := pipelineFlags(t, true)
	svc := NewCandidatePipeline(b.store, flags)
	ref := agentcognitive.SourceReference{Type: agentcognitive.SourceType("MOMENT"), ID: p.Review.Source.Selector.ID, Owner: a.WorkspacePrincipal, Version: p.Review.Source.Version.Revision}
	req := agentcognitive.CandidateSubmission{Request: agentcognitive.ReadRequest{Version: agentcognitive.ContractVersion, RequestID: g.ID, TaskID: p.Review.TaskID, Agent: agentcognitive.AgentReference{AgentID: g.AgentID, Principal: a.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, Purpose: agentcognitive.SubmitMemoryCandidate, Scope: agentruntime.Private, Source: ref, ExpiresAt: g.ExpiresAt}, Sources: []agentcognitive.SourceReference{ref}}
	req.PayloadDigest, _ = acr.DigestReview(*p.Review)
	bad := req
	bad.PayloadDigest = strings.Repeat("f", 64)
	if r, e := svc.BindCandidateSubmitter(a, g.ID).SubmitMemoryCandidate(b.ctx, bad); e == nil || r.Status == agentcognitive.Available {
		t.Fatal("digest as authority", r, e)
	}
	r, e := svc.BindCandidateSubmitter(a, g.ID).SubmitMemoryCandidate(b.ctx, req)
	if e != nil || r.Status != agentcognitive.Available {
		t.Fatal(r, e)
	}
}

func TestCandidatePipelineNativeSQLCannotSubstituteCandidateOrSkipCheckpoint(t *testing.T) {
	for _, mode := range []string{"category", "assessment", "source", "task", "expiry", "unknown_review", "duplicate_review", "effect_address", "old_checkpoint", "missing_atomic_checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			f := pipelineNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			p, g := f.approve(t)
			flags := pipelineFlags(t, true)
			before := pipelineCount(t, f)
			caught := false
			x := &candidatePipelineExecutor{store: b.store, txHook: func(ctx context.Context, tx pgx.Tx, point string) error {
				if mode == "missing_atomic_checkpoint" {
					if point != "after_effect" {
						return nil
					}
					_, e := tx.Exec(ctx, `SET CONSTRAINTS candidate_pipeline_atomic_final IMMEDIATE`)
					var pe *pgconn.PgError
					if !errors.As(e, &pe) || pe.Code != "55000" {
						t.Fatalf("missing checkpoint SQL should reject: %v", e)
					}
					caught = true
					return errors.New("expected final guard rejection")
				}
				if point != "after_candidate" {
					return nil
				}
				var currentID string
				if e := tx.QueryRow(ctx, `SELECT id FROM agent_memory_candidates WHERE owner_id=$1 AND status='CANDIDATE'`, a.WorkspacePrincipal.ID).Scan(&currentID); e != nil {
					return e
				}
				proof, _ := json.Marshal(p.Review)
				if mode == "task" {
					proof = []byte(strings.Replace(string(proof), p.Review.TaskID, g.ID, 1))
				}
				if mode == "unknown_review" {
					proof = []byte(strings.TrimSuffix(string(proof), "}") + `,"confirmed":true}`)
				}
				if mode == "duplicate_review" {
					proof = []byte(strings.TrimSuffix(string(proof), "}") + `,"clusters":1}`)
				}
				var chosen string
				switch mode {
				case "category", "assessment", "source", "expiry":
					category := p.Review.Proposal.Category
					ass, _ := json.Marshal(p.Review.Proposal.Assessment)
					source, _ := json.Marshal([]any{p.Review.Source})
					end := g.ExpiresAt
					if mode == "category" {
						category = "culture"
					}
					if mode == "assessment" {
						ass = []byte(`{"semantics":"ORDINAL","level":"HIGH"}`)
					}
					if mode == "source" {
						source = []byte(strings.Replace(string(source), p.Review.Source.Selector.ID, g.ID, -1))
					}
					if mode == "expiry" {
						end = end.Add(time.Second)
					}
					e := tx.QueryRow(ctx, `INSERT INTO agent_memory_candidates(id,agent_id,owner_id,version,metadata_version,status,predicate,category,assessment,sources,intent_digest,valid_until,created_at,updated_at) SELECT gen_random_uuid(),agent_id,owner_id,1,metadata_version,'CANDIDATE',predicate,$2,$3,$4,$5,$6,created_at,created_at FROM agent_memory_candidates WHERE id=$1 RETURNING id`, currentID, category, ass, source, strings.Repeat("c", 64), end).Scan(&chosen)
					if e != nil {
						return e
					}
				default:
					chosen = currentID
				}
				var event, op string
				var fence, attempt int64
				if e := tx.QueryRow(ctx, `SELECT rp.event_id,rp.logical_operation_id,d.fence,d.attempt FROM agent_candidate_retention_previews rp JOIN agent_domain_outbox d ON d.event_id=rp.event_id WHERE rp.id=$1`, p.ID).Scan(&event, &op, &fence, &attempt); e != nil {
					return e
				}
				owner := a.WorkspacePrincipal
				key, e := agentoutbox.EffectKey(agentoutbox.EffectAddress{Tenant: owner, Subject: owner, AgentID: g.AgentID, LogicalOperationID: op, ActionID: acp.StageActionID, Kind: agentoutbox.MemoryCandidateEffect})
				if e != nil {
					return e
				}
				if mode == "effect_address" {
					key = strings.Repeat("d", 64)
				}
				if mode == "old_checkpoint" {
					_, e := tx.Exec(ctx, `UPDATE agent_domain_outbox SET delivery_state='CANDIDATE_STAGED',lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE event_id=$1`, event)
					var pe *pgconn.PgError
					if !errors.As(e, &pe) || pe.Code != "55000" {
						t.Fatalf("fake checkpoint accepted: %v", e)
					}
					caught = true
					return errors.New("expected checkpoint rejection")
				}
				digest, _ := acr.DigestReview(*p.Review)
				_, e = tx.Exec(ctx, `INSERT INTO agent_effect_ledger(subject_id,effect_key,agent_id,logical_operation_id,action_id,effect_kind,action_digest,source_id,source_revision,candidate_id,retention_grant_id,event_id,handler_version,fence,attempt,proof_review) VALUES($1,$2,$3,$4,$5,'MEMORY_CANDIDATE',$6,$7,$8,$9,$10,$11,'mom-candidate-local-v1',$12,$13,$14)`, owner.ID, key, g.AgentID, op, acp.StageActionID, digest, p.Review.Source.Selector.ID, p.Review.Source.Version.Revision, chosen, g.ID, event, fence, attempt, string(proof))
				var pe *pgconn.PgError
				if !errors.As(e, &pe) || pe.Code != "55000" {
					t.Fatalf("%s SQL proof wrongly accepted: %v", mode, e)
				}
				caught = true
				return errors.New("expected native SQL rejection")
			}}
			if r, e := acp.NewService(x, flags).StageOwnMomentCandidate(b.ctx, a, g.ID); e == nil || !reflectReceiptZero(r) || !caught {
				t.Fatal("failed strict SQL negative", mode, r, e, caught)
			}
			if pipelineCount(t, f) != before {
				t.Fatal("negative had partial writes")
			}
		})
	}
}
func TestCandidatePipelineNativeCurrentSourcesIdentityAndLease(t *testing.T) {
	for _, mode := range []string{"retention_revoke", "analysis_revoke", "moment_ABA", "task_ABA", "city_ABA", "account_ABA", "agent_ABA", "metadata_ABA", "session_revoke", "wrong_owner", "wrong_session", "expired", "links", "source_revision"} {
		t.Run(mode, func(t *testing.T) {
			f := pipelineNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			if mode == "expired" {
				f.selection.RetainUntil = time.Now().UTC().Truncate(time.Microsecond).Add(250 * time.Millisecond)
			}
			_, g := f.approve(t)
			switch mode {
			case "retention_revoke":
				if _, e := b.store.RevokeOwnCandidateRetention(b.ctx, a, g.ID, 1); e != nil {
					t.Fatal(e)
				}
			case "analysis_revoke":
				if _, e := b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, f.selection.AnalysisGrantID, 1); e != nil {
					t.Fatal(e)
				}
			case "moment_ABA":
				b.exec(`UPDATE moments SET body=body||'X' WHERE id=$1`, f.f.moment.ID)
				b.exec(`UPDATE moments SET body=left(body,length(body)-1) WHERE id=$1`, f.f.moment.ID)
			case "task_ABA":
				b.exec(`UPDATE agent_tasks SET query=query||'X' WHERE id=$1`, f.f.f.task.ID)
				b.exec(`UPDATE agent_tasks SET query=left(query,length(query)-1) WHERE id=$1`, f.f.f.task.ID)
			case "city_ABA":
				b.exec(`UPDATE cities SET name=name||'X' WHERE id=$1`, f.f.f.place.city)
				b.exec(`UPDATE cities SET name=left(name,length(name)-1) WHERE id=$1`, f.f.f.place.city)
			case "account_ABA":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "agent_ABA":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, g.AgentID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, g.AgentID)
			case "metadata_ABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, g.AgentID)
			case "session_revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			case "wrong_owner":
				a = f.f.f.place.private.peer
			case "wrong_session":
				var digest []byte
				if e := b.pool.QueryRow(b.ctx, `SELECT token_sha256 FROM sessions WHERE account_id=$1 AND id<>(SELECT id FROM sessions WHERE token_sha256=$2) LIMIT 1`, b.person.ID, a.SessionDigest[:]).Scan(&digest); e == nil {
					copy(a.SessionDigest[:], digest)
				} else {
					a.SessionDigest = [32]byte{97}
				}
			case "expired":
				time.Sleep(350 * time.Millisecond)
			case "links":
				b.exec(`INSERT INTO moment_activity_links(moment_id,activity_id,city_id,author_confirmed_at) SELECT $1,$2,city_id,clock_timestamp() FROM moments WHERE id=$1`, f.f.moment.ID, f.f.f.public)
			case "source_revision":
				b.exec(`UPDATE moments SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.f.moment.ID)
			}
			before := pipelineCount(t, f)
			if r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID); e == nil || !reflectReceiptZero(r) {
				t.Fatal(mode, r, e)
			}
			if pipelineCount(t, f) != before {
				t.Fatal("denied writes")
			}
		})
	}
}
func TestCandidatePipelineNativeUsedDownAndTTLFullPayloadCleanup(t *testing.T) {
	ownedMigrationDatabase(t)
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	pgAt := func(phase string) time.Time {
		var at time.Time
		if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
			t.Fatal(e)
		}
		t.Logf("used TTL phase=%s pg_at=%s original_selected_until=%s", phase, at.UTC().Format(time.RFC3339Nano), f.selection.RetainUntil.UTC().Format(time.RFC3339Nano))
		return at
	}
	// Pick the user's finite deadline once before Preview. Approval and Stage
	// must consume this original bound; never update/renew an issued grant.
	f.selection.RetainUntil = pgAt("before_selection").UTC().Truncate(time.Microsecond).Add(5 * time.Second)
	pgAt("before_preview")
	_, g := f.approve(t)
	afterApproval := pgAt("after_approve")
	if !g.ExpiresAt.After(afterApproval) || g.ExpiresAt.After(f.selection.RetainUntil) {
		t.Fatal("approval missed original finite selection", g.ExpiresAt, f.selection.RetainUntil)
	}
	r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
	afterStage := pgAt("after_stage")
	if e != nil {
		t.Fatal("Stage failed before native expiry check", e)
	}
	if r.Candidate == nil || !r.Candidate.ValidUntil.After(afterStage) || r.Candidate.ValidUntil.After(g.ExpiresAt) || r.Candidate.ValidUntil.After(f.selection.RetainUntil) {
		t.Fatal("candidate did not preserve original grant/selection deadline")
	}
	t.Logf("used TTL original_grant_until=%s original_candidate_until=%s", g.ExpiresAt.UTC().Format(time.RFC3339Nano), r.Candidate.ValidUntil.UTC().Format(time.RFC3339Nano))
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "082_agent_candidate_atomic_consumer.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	before := pipelineCount(t, f)
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	func() {
		// Even an assertion failure must release the actual aborted migration
		// transaction before the owned fixture attempts any cleanup queries.
		defer conn.Release()
		defer conn.Exec(context.Background(), "ROLLBACK")
		_, e = conn.Exec(b.ctx, string(down))
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "P0001" || pe.Message != "082 down refuses candidate/effect/handler history" {
			t.Fatal("original082 used history guard", e)
		}
	}()
	if pipelineCount(t, f) != before {
		t.Fatal("down was not atomic")
	}
	// Cleanup's purpose is to observe actual expiry, not assume that a fixed
	// wall-clock sleep also covered approval/commit and native clock costs.
	waitLimit := time.Now().Add(10 * time.Second)
	for {
		at := pgAt("await_original_candidate_expiry")
		if !at.Before(r.Candidate.ValidUntil) {
			break
		}
		if time.Now().After(waitLimit) {
			t.Fatal("bounded PG expiry wait exceeded")
		}
		remaining := r.Candidate.ValidUntil.Sub(at)
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		time.Sleep(remaining)
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&count); e != nil || count != 1 {
		t.Fatal("cleanup", count, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&count); e != nil || count != 0 {
		t.Fatal("cleanup not idempotent", count, e)
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(c)::text FROM agent_memory_candidates c WHERE id=$1`, r.CandidateID).Scan(&raw); e != nil || strings.Contains(raw, "badminton") || !strings.Contains(raw, "EXPIRED") {
		t.Fatal(raw, e)
	}
}

func TestCandidatePipelineNativeRealWaitRejectsCurrentGrantSourceAndDeadline(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"retention_revoke", "analysis_revoke", "source_ABA", "task_ABA", "city_ABA", "natural_expiry", "session_expiry", "outbox_busy"} {
		t.Run(mode, func(t *testing.T) {
			f := pipelineNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			if mode == "natural_expiry" {
				var pgAt time.Time
				if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&pgAt); e != nil {
					t.Fatal(e)
				}
				// Approval must finish before this test can exercise a real wait.
				// Choose once from PG time; do not renew or rewrite the grant.
				f.selection.RetainUntil = pgAt.UTC().Truncate(time.Microsecond).Add(5 * time.Second)
				t.Logf("natural expiry phase=before_preview pg_at=%s selected_until=%s", pgAt.UTC().Format(time.RFC3339Nano), f.selection.RetainUntil.Format(time.RFC3339Nano))
			}
			_, g := f.approve(t)
			if mode == "natural_expiry" {
				var pgAt time.Time
				if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&pgAt); e != nil {
					t.Fatal(e)
				}
				if !g.ExpiresAt.After(pgAt) || g.ExpiresAt.After(f.selection.RetainUntil) {
					t.Fatal("natural expiry must reach wait with original current grant")
				}
				t.Logf("natural expiry phase=after_approve pg_at=%s original_grant_until=%s", pgAt.UTC().Format(time.RFC3339Nano), g.ExpiresAt.UTC().Format(time.RFC3339Nano))
			}
			if mode == "session_expiry" {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '450 milliseconds' WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			before := pipelineCount(t, f)
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			if mode == "outbox_busy" {
				var event string
				if e = held.QueryRow(b.ctx, `SELECT event_id FROM agent_domain_outbox WHERE subject_id=$1 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, b.person.ID).Scan(&event); e != nil {
					t.Fatal(e)
				}
				r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
				if !errors.Is(e, acp.ErrBusy) || !reflectReceiptZero(r) {
					t.Fatal("NOWAIT must fail closed", r, e)
				}
				held.Rollback(context.Background())
				if pipelineCount(t, f) != before {
					t.Fatal("busy wrote")
				}
				return
			}
			if _, e = held.Exec(b.ctx, `LOCK TABLE moments IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
				if e == nil || !reflectReceiptZero(r) {
					done <- errors.New("real wait released candidate")
				} else {
					done <- nil
				}
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				var waiting bool
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%LOCK TABLE%moments%')`, int(held.Conn().PgConn().PID())).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("real native wait not observed")
				}
				time.Sleep(10 * time.Millisecond)
			}
			switch mode {
			case "retention_revoke":
				_, e = held.Exec(b.ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, g.ID)
			case "analysis_revoke":
				_, e = held.Exec(b.ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, f.selection.AnalysisGrantID)
			case "source_ABA":
				for _, body := range []string{"changed-while-waiting", f.f.moment.Body} {
					if _, e = held.Exec(b.ctx, `UPDATE moments SET body=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.f.moment.ID, body); e != nil {
						break
					}
				}
			case "task_ABA":
				for _, query := range []string{"changed-while-waiting", f.f.f.task.Query} {
					if _, e = held.Exec(b.ctx, `UPDATE agent_tasks SET query=$2,filters=jsonb_set(filters,'{currentQuery}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1`, f.f.f.task.ID, query); e != nil {
						break
					}
				}
			case "city_ABA":
				for _, status := range []string{"hidden", "published"} {
					if _, e = held.Exec(b.ctx, `UPDATE cities SET publication_status=$2 WHERE id=$1`, f.f.f.place.city, status); e != nil {
						break
					}
				}
			case "natural_expiry":
				waitCtx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
				defer cancel()
				for {
					var pgAt time.Time
					if e = held.QueryRow(waitCtx, `SELECT clock_timestamp()`).Scan(&pgAt); e != nil {
						t.Fatal("original grant deadline wait", e)
					}
					if !pgAt.Before(g.ExpiresAt) {
						t.Logf("natural expiry phase=actual_locked_deadline pg_at=%s original_grant_until=%s", pgAt.UTC().Format(time.RFC3339Nano), g.ExpiresAt.UTC().Format(time.RFC3339Nano))
						break
					}
					select {
					case <-waitCtx.Done():
						t.Fatal("original grant expiry wait exceeded bound", waitCtx.Err())
					case <-time.After(20 * time.Millisecond):
					}
				}
			default:
				time.Sleep(550 * time.Millisecond)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native wait stuck")
			}
			var count int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1`, b.person.ID).Scan(&count); e != nil || count != 0 {
				t.Fatal("late authority wrote", count, e)
			}
			if mode == "natural_expiry" {
				if e = b.pool.QueryRow(b.ctx, `SELECT
 (SELECT count(*) FROM agent_memory_candidates WHERE owner_id=$1)+
 (SELECT count(*) FROM agent_consumer_inbox WHERE subject_id=$1)`, b.person.ID).Scan(&count); e != nil || count != 0 {
					t.Fatal("expired original grant retained candidate or checkpoint", count, e)
				}
			}
		})
	}
}
func TestCandidatePipelineNativeMissingGuardAndOriginalIntentConflict(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"effect_guard", "final_guard", "revoke_guard", "outbox_guard", "inbox_guard", "schema082", "existing_manual"} {
		t.Run(mode, func(t *testing.T) {
			f := pipelineNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			p, g := f.approve(t)
			before := pipelineCount(t, f)
			if mode == "existing_manual" {
				raw, _ := json.Marshal([]any{p.Review.Source})
				assessment, _ := json.Marshal(p.Review.Proposal.Assessment)
				intent, _ := agentreinforcement.Digest(struct {
					Predicate, Category string
					Sources             []agentmemorycandidate.Source
				}{p.Review.Proposal.Predicate, p.Review.Proposal.Category, []agentmemorycandidate.Source{p.Review.Source}})
				b.exec(`INSERT INTO agent_memory_candidates(id,agent_id,owner_id,version,metadata_version,status,predicate,category,assessment,sources,intent_digest,valid_until,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,1,profile_version,'CANDIDATE',$3,$4,$5,$6,$7,$8,clock_timestamp(),clock_timestamp() FROM agent_profiles WHERE agent_id=$1`, g.AgentID, a.WorkspacePrincipal.ID, p.Review.Proposal.Predicate, p.Review.Proposal.Category, assessment, raw, intent, g.ExpiresAt)
				before = pipelineCount(t, f)
			} else {
				table := "agent_effect_ledger"
				trigger := "agent_effect_writer_unavailable"
				if mode == "final_guard" {
					trigger = "candidate_pipeline_atomic_final"
				}
				if mode == "outbox_guard" {
					table = "agent_domain_outbox"
					trigger = "agent_outbox_guard"
				}
				if mode == "inbox_guard" {
					table = "agent_consumer_inbox"
					trigger = "agent_consumer_inbox_guard"
				}
				if mode == "revoke_guard" {
					table = "consent_grants"
					trigger = "candidate_pipeline_revoke"
				}
				if mode == "schema082" {
					down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "082_agent_candidate_atomic_consumer.down.sql"))
					if e != nil {
						t.Fatal(e)
					}
					if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
						t.Fatal(e)
					}
					t.Cleanup(func() {
						up, _ := os.ReadFile(filepath.Join("..", "..", "migrations", "082_agent_candidate_atomic_consumer.sql"))
						if _, e := b.pool.Exec(context.Background(), string(up)); e != nil {
							t.Error(e)
						}
						candidateVocabularyFixture(t, b.pool, b.ctx, true)
					})
				} else {
					b.exec(`ALTER TABLE ` + table + ` DISABLE TRIGGER ` + trigger)
					t.Cleanup(func() { b.pool.Exec(context.Background(), `ALTER TABLE `+table+` ENABLE TRIGGER `+trigger) })
				}
			}
			r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
			if e == nil || !reflectReceiptZero(r) {
				t.Fatal(mode, r, e)
			}
			if mode == "existing_manual" && !errors.Is(e, acp.ErrConflict) {
				t.Fatal("must not revive manual candidate", e)
			}
			if mode != "schema082" && pipelineCount(t, f) != before {
				t.Fatal("guard missing but wrote")
			}
		})
	}
}
func TestCandidatePipelineProcessCrashHelper(t *testing.T) {
	if os.Getenv("BIRDTIE_PIPELINE_TEST_CHILD") != "1" {
		return
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("only owned disposable fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var digest [32]byte
	raw, e := hex.DecodeString(os.Getenv("BIRDTIE_PIPELINE_TEST_DIGEST"))
	if e != nil || len(raw) != 32 {
		t.Fatal("fixture digest")
	}
	copy(digest[:], raw)
	a := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: os.Getenv("BIRDTIE_PIPELINE_TEST_OWNER")}, SessionDigest: digest}
	x := &candidatePipelineExecutor{store: New(pool, false), hook: func(point string) error {
		if point == os.Getenv("BIRDTIE_PIPELINE_TEST_POINT") {
			os.Exit(86)
		}
		return nil
	}}
	_, e = acp.NewService(x, pipelineFlags(t, true)).StageOwnMomentCandidate(ctx, a, os.Getenv("BIRDTIE_PIPELINE_TEST_GRANT"))
	t.Fatal("expected process exit before commit", e)
}
func TestCandidatePipelineNativeProcessCrashRollsBackAndRestartReconciles(t *testing.T) {
	for _, point := range []string{"after_candidate", "after_effect", "after_inbox", "after_checkpoint", "before_commit"} {
		t.Run(point, func(t *testing.T) {
			f := pipelineNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			_, g := f.approve(t)
			before := pipelineCount(t, f)
			cmd := exec.CommandContext(b.ctx, os.Args[0], "-test.run=^TestCandidatePipelineProcessCrashHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(), "BIRDTIE_PIPELINE_TEST_CHILD=1", "BIRDTIE_PIPELINE_TEST_POINT="+point, "BIRDTIE_PIPELINE_TEST_OWNER="+a.WorkspacePrincipal.ID, "BIRDTIE_PIPELINE_TEST_DIGEST="+hex.EncodeToString(a.SessionDigest[:]), "BIRDTIE_PIPELINE_TEST_GRANT="+g.ID)
			output, e := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(e, &exit) || exit.ExitCode() != 86 {
				t.Fatalf("actual child did not die at %s: %v %s", point, e, output)
			}
			if pipelineCount(t, f) != before {
				t.Fatal("process death partially committed")
			}
			receipt, e := NewCandidatePipeline(b.store, pipelineFlags(t, false)).ReadOwnMomentCandidateReceipt(b.ctx, a, g.ID)
			if e != nil || receipt.State != "NOT_STAGED" {
				t.Fatal("unknown outcome", receipt, e)
			}
			r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
			if e != nil || !r.Committed {
				t.Fatal("restart original key", r, e)
			}
		})
	}
}
func TestCandidatePipelineNativeCleanupActualCommandRepeatedConcurrentRestart(t *testing.T) {
	// Cleanup scans the whole database: use an owned database rather than
	// expiring candidates belonging to another concurrently running package.
	ownedMigrationDatabase(t)
	binary := filepath.Join(t.TempDir(), "candidate-cleanup.exe")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer buildCancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, "./cmd/agent-candidate-cleanup")
	build.Dir = filepath.Join("..", "..")
	output, e := build.CombinedOutput()
	if e != nil {
		t.Fatalf("actual cleanup compile %v %s", e, output)
	}
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	pgNow := func(stage string) time.Time {
		t.Helper()
		var at time.Time
		if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
			t.Fatal("cleanup phase PostgreSQL clock", stage, e)
		}
		t.Logf("cleanup phase=%s pg_at=%s", stage, at.UTC().Format(time.RFC3339Nano))
		return at
	}
	// Select a concrete deadline after compilation. Approval and submission
	// consume that original window; neither retries nor extends its authority.
	f.selection.RetainUntil = pgNow("before_preview").UTC().Truncate(time.Microsecond).Add(10 * time.Second)
	_, g := f.approve(t)
	pgNow("after_approve")
	r, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, g.ID)
	pgNow("after_stage")
	if e != nil {
		t.Fatal(e)
	}
	if !r.Committed || r.Candidate == nil || r.RetentionGrantID != g.ID ||
		r.Candidate.ValidUntil.After(f.selection.RetainUntil) || r.Candidate.ValidUntil.After(g.ExpiresAt) {
		t.Fatal("candidate exceeded original selection or grant deadline")
	}
	t.Logf("cleanup selection_until=%s grant_until=%s candidate_until=%s", f.selection.RetainUntil.UTC().Format(time.RFC3339Nano), g.ExpiresAt.UTC().Format(time.RFC3339Nano), r.Candidate.ValidUntil.UTC().Format(time.RFC3339Nano))
	// Wait for the actual committed deadline using PostgreSQL time. The host
	// timer only bounds the test, never establishes expiry or rewrites the row.
	waitCtx, waitCancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer waitCancel()
	for {
		var expired bool
		if e := b.pool.QueryRow(waitCtx, `SELECT clock_timestamp() >= $1::timestamptz`, r.Candidate.ValidUntil).Scan(&expired); e != nil {
			t.Fatal("waiting for original candidate deadline", e)
		}
		if expired {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("original candidate deadline wait exceeded bound", waitCtx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	pgNow("expired_before_cleanup")
	run := func() (int, error) {
		cmd := exec.CommandContext(b.ctx, binary, "-batch", "100", "-deadline", "10s")
		cmd.Env = append(os.Environ(), "BIRDTIE_ENV=development", "BIRDTIE_DISPOSABLE_DB=1")
		raw, e := cmd.CombinedOutput()
		if e != nil {
			return 0, fmt.Errorf("cleanup command failed %w: %s", e, raw)
		}
		var v struct {
			Expired             int    `json:"expired"`
			Scope               string `json:"scope"`
			ModelAccess         bool   `json:"modelAccess"`
			ProductionScheduler bool   `json:"productionScheduler"`
		}
		if json.Unmarshal(raw, &v) != nil || v.Scope != "LOCAL_DISPOSABLE_ONLY" || v.ModelAccess || v.ProductionScheduler {
			return 0, errors.New("invalid secret-free cleanup result")
		}
		return v.Expired, nil
	}
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); n, e := run(); counts <- n; errs <- e }()
	}
	wg.Wait()
	close(counts)
	close(errs)
	total := 0
	for n := range counts {
		total += n
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if total != 1 {
		t.Fatal("cleanup race lost or doubled", total)
	}
	for i := 0; i < 2; i++ {
		if n, e := run(); e != nil || n != 0 {
			t.Fatal("restart/repeat not idempotent", n, e)
		}
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(c)::text FROM agent_memory_candidates c WHERE id=$1`, r.CandidateID).Scan(&raw); e != nil || strings.Contains(raw, "badminton") || !strings.Contains(raw, "EXPIRED") {
		t.Fatal("payload retained", raw, e)
	}
}
