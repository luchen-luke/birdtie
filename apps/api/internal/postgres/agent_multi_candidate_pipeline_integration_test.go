package postgres

import (
	"context"
	"encoding/json"
	"errors"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentlocalcandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	amc "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMultiCandidateNativeSameTxRealCandidateAndExplicitHumanAccept(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	off := NewMultiCandidatePipeline(b.store, pipelineFlags(t, false))
	r, e := off.StageOwnMultiCandidate(b.ctx, a, g.ID)
	if !errors.Is(e, amc.ErrUnavailable) || r.Committed {
		t.Fatal("default OFF", e)
	}
	flags := pipelineFlags(t, true)
	svc := amc.NewService(&multiCandidateExecutor{store: b.store, Store: b.store, errorHook: func(e error) {
		var p *pgconn.PgError
		if errors.As(e, &p) {
			t.Log("SQL state", p.Code, "safe native message", p.Message)
		}
	}, hook: func(stage string) error { t.Log("native phase", stage); return nil }}, flags)
	r, e = svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil {
		t.Fatal("native multi stage", e)
	}
	if !r.Committed || len(r.Candidate.Sources) != 2 || r.Candidate.Status != agentmemorycandidate.Candidate || r.EventID != p.Review.AnchorEventID {
		t.Fatal(r)
	}
	repeat, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil || repeat.EffectKey != r.EffectKey || repeat.CandidateID != r.CandidateID || repeat.Candidate != nil {
		t.Fatal("repeat", e)
	}
	beforeRepeat := multiState(t, f)
	for i := 0; i < 100; i++ {
		next, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
		if e != nil || next.EffectKey != r.EffectKey || next.CandidateID != r.CandidateID || next.Candidate != nil {
			t.Fatal("100 original ID retries", i, e)
		}
	}
	if multiState(t, f) != beforeRepeat {
		t.Fatal("100 retries changed business/effect/lease")
	}
	human := NewMemoryCandidateService(b.store, flags)
	var mid string
	if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&mid); e != nil {
		t.Fatal(e)
	}
	check, e := human.PreviewOwnAcceptance(b.ctx, a, r.CandidateID, 1, mid, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour))
	if e != nil {
		t.Fatal("actual two clusters human preview", e)
	}
	accepted, e := human.AcceptOwnCandidate(b.ctx, a, check)
	if e != nil || accepted.Status != agentmemorycandidate.Active || accepted.MemoryID == nil {
		t.Fatal("human explicit accept", e)
	}
	var typ, status string
	var n int
	if e = b.pool.QueryRow(b.ctx, `SELECT source_type FROM agent_memories WHERE id=$1`, *accepted.MemoryID).Scan(&typ); e != nil || typ != "EXPLICIT" {
		t.Fatal("automatic inference", typ, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1`, *accepted.MemoryID).Scan(&n); e != nil || n != 2 {
		t.Fatal("real independent source evidence", n, e)
	}
	if _, e = b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, f.grants[1].ID, 1); e != nil {
		t.Fatal(e)
	}
	if row, e := human.ReadOwnCandidate(b.ctx, a, r.CandidateID); e != nil || row.Status != agentmemorycandidate.Expired {
		t.Fatal("independent support not invalidated", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT source_type,status FROM agent_memories WHERE id=$1`, *accepted.MemoryID).Scan(&typ, &status); e != nil || typ != "EXPLICIT" || status != "ACTIVE" {
		t.Fatal("revoked inference deleted human statement", e)
	}
}

func multiState(t *testing.T, f *multiNativeFixture) string {
	t.Helper()
	return pipelineCount(t, &retentionFixture{f: f.f})
}

func TestMultiCandidateNativeHikingV2OrdinalOriginalTaskAndExplicitAccept(t *testing.T) {
	f := multiNative(t, "本人所选徒步记录", "Hiking after class")
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	var taskBefore string
	if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(t)::text FROM agent_tasks t WHERE id=$1`, f.f.f.task.ID).Scan(&taskBefore); e != nil {
		t.Fatal(e)
	}
	p, g := f.approve(t)
	if p.Review.Proposal.Category != "hiking" || p.Review.Proposal.AlgorithmVersion != agentlocalcandidate.Version || p.Review.Proposal.Assessment.Level != "LOW" || p.Review.Proposal.Assessment.Value != nil {
		t.Fatal("hiking is only ordinal", p.Review)
	}
	svc := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true))
	r, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil || r.Candidate == nil || r.Candidate.Category != "hiking" || r.Candidate.Status != agentmemorycandidate.Candidate {
		t.Fatal(r, e)
	}
	var taskAfter, mid string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(t)::text FROM agent_tasks t WHERE id=$1`, f.f.f.task.ID).Scan(&taskAfter); e != nil || taskAfter != taskBefore {
		t.Fatal("hiking requires no Task parser or implicit Task mutation", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&mid); e != nil {
		t.Fatal(e)
	}
	h := NewMemoryCandidateService(b.store, pipelineFlags(t, true))
	preview, e := h.PreviewOwnAcceptance(b.ctx, a, r.CandidateID, 1, mid, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour))
	if e != nil || preview.Review().Statement != "我偏好徒步活动" {
		t.Fatal("specific human statement", e)
	}
	accepted, e := h.AcceptOwnCandidate(b.ctx, a, preview)
	if e != nil || accepted.Status != agentmemorycandidate.Active {
		t.Fatal(e)
	}
	var source, summary string
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT source_type,summary FROM agent_memories WHERE id=$1`, mid).Scan(&source, &summary); e != nil || source != "EXPLICIT" || summary != "我偏好徒步活动" {
		t.Fatal("not automatic inferred promotion", source, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1`, mid).Scan(&count); e != nil || count != 2 {
		t.Fatal("two actual source references", count, e)
	}
	if _, e = b.store.RevokeOwnEnrichmentPurpose(b.ctx, a, f.grants[1].ID, 1); e != nil {
		t.Fatal(e)
	}
	row, e := h.ReadOwnCandidate(b.ctx, a, r.CandidateID)
	if e != nil || row.Status != agentmemorycandidate.Expired {
		t.Fatal("revoked support must expire", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT source_type FROM agent_memories WHERE id=$1 AND status='ACTIVE'`, mid).Scan(&source); e != nil || source != "EXPLICIT" {
		t.Fatal("independent declaration was deleted", e)
	}
}

func TestMultiCandidateNativeHikingPinnedLegacyReviewAndUsed092Down(t *testing.T) {
	f := multiNative(t, "culture hiking", "culture hiking")
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	// Create a real v1 approval on actual schema091, then upgrade without
	// changing any old row or xmin. Old approved culture must still replay v1.
	if !candidateVocabularyFixture(t, b.pool, b.ctx, false) {
		t.Fatal("expected current092 fixture")
	}
	missingBefore := multiState(t, f)
	if _, e := b.store.PreviewOwnMultiCandidate(b.ctx, a, f.selection); !errors.Is(e, amc.ErrUnavailable) {
		t.Fatal("new v2 without092 must be unavailable", e)
	}
	if multiState(t, f) != missingBefore {
		t.Fatal("missing092 changed effect or candidate")
	}
	p, e := b.store.previewOwnMultiCandidateVersion(b.ctx, a, f.selection, agentlocalcandidate.LegacyVersion)
	if e != nil || p.Review.Proposal.AlgorithmVersion != agentlocalcandidate.LegacyVersion || p.Review.Proposal.Category != "culture" {
		t.Fatal("original algorithm", e)
	}
	g, e := b.store.ApproveOwnMultiCandidate(b.ctx, a, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	oldRows := enrichmentAllPublic(t, b.pool, b.ctx)
	oldXmin := hikingAllRowXmin(t, b.pool, b.ctx)
	candidateVocabularyFixture(t, b.pool, b.ctx, true)
	if enrichmentAllPublic(t, b.pool, b.ctx) != oldRows || hikingAllRowXmin(t, b.pool, b.ctx) != oldXmin {
		t.Fatal("092 upgraded or modified legacy approval rows/xmin")
	}
	r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil || r.Candidate == nil || r.Candidate.Category != "culture" {
		t.Fatal("old approved review silently upgraded", e)
	}
	if _, e = b.store.PreviewOwnMultiCandidate(b.ctx, a, f.selection); !errors.Is(e, amc.ErrUnavailable) {
		t.Fatal("new v2 must see ambiguity", e)
	}
	// v2 history alone is enough to protect down, even before a candidate is
	// committed. Never delete a used/new-version preview merely to downgrade.
	f2 := multiNative(t, "徒步", "hiking")
	b2 := f2.f.f.place.private.base
	a2 := f2.f.f.place.private.owner
	p2, e := b2.store.PreviewOwnMultiCandidate(b2.ctx, a2, f2.selection)
	if e != nil || p2.Review.Proposal.Category != "hiking" {
		t.Fatal(e)
	}
	before := enrichmentAllPublic(t, b2.pool, b2.ctx)
	raw, e := os.ReadFile("../../migrations/092_agent_candidate_hiking_vocabulary.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b2.pool.Acquire(b2.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	defer conn.Exec(context.Background(), `ROLLBACK`)
	_, e = conn.Exec(b2.ctx, string(raw))
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "55000" || !strings.Contains(pe.Message, "092 v2") {
		t.Fatal("used092 down did not refuse", e)
	}
	if _, e = conn.Exec(b2.ctx, `ROLLBACK`); e != nil {
		t.Fatal(e)
	}
	if enrichmentAllPublic(t, b2.pool, b2.ctx) != before {
		t.Fatal("used092 down changed any old row")
	}
}
func TestMultiCandidateNativeHikingLostMetadataStillProtectsUsedDown(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil || r.Candidate == nil || p.Review.Proposal.AlgorithmVersion != agentlocalcandidate.Version {
		t.Fatal(e)
	}
	if _, e = b.store.RevokeOwnMultiCandidate(b.ctx, a, g.ID, 1); e != nil {
		t.Fatal(e)
	}
	// Privileged fixture metadata loss is not a supported application action.
	// Persistently NULL transient proof and a cleared category cannot prove v1.
	b.exec(`DELETE FROM agent_multi_candidate_bindings WHERE grant_id=$1`, g.ID)
	b.exec(`DELETE FROM agent_multi_candidate_previews WHERE id=$1`, p.ID)
	var category *string
	var status string
	if e = b.pool.QueryRow(b.ctx, `SELECT status,category FROM agent_memory_candidates WHERE id=$1`, r.CandidateID).Scan(&status, &category); e != nil || status != "EXPIRED" || category != nil {
		t.Fatal("expired metadata", status, e)
	}
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	raw, e := os.ReadFile("../../migrations/092_agent_candidate_hiking_vocabulary.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	defer conn.Exec(context.Background(), `ROLLBACK`)
	_, e = conn.Exec(b.ctx, string(raw))
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "55000" {
		t.Fatal("unknown effect history downgraded", e)
	}
	if _, e = conn.Exec(b.ctx, `ROLLBACK`); e != nil {
		t.Fatal(e)
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != before {
		t.Fatal("refused down changed original rows")
	}
}

func TestMultiCandidateNativeAllSourceAndIdentityClosure(t *testing.T) {
	for _, mode := range []string{"extra_analysis_revoke", "retention_revoke", "task_ABA", "city_ABA", "extra_source_ABA", "account_ABA", "agent_ABA", "metadata_ABA", "wrong_owner", "wrong_session"} {
		t.Run(mode, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			_, g := f.approve(t)
			switch mode {
			case "extra_analysis_revoke":
				b.exec(`UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, f.grants[1].ID)
			case "retention_revoke":
				if _, e := b.store.RevokeOwnMultiCandidate(b.ctx, a, g.ID, 1); e != nil {
					t.Fatal(e)
				}
			case "task_ABA":
				for _, q := range []string{"changed", f.f.f.task.Query} {
					b.exec(`UPDATE agent_tasks SET query=$2,filters=jsonb_set(filters,'{currentQuery}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1`, f.f.f.task.ID, q)
				}
			case "city_ABA":
				for _, s := range []string{"hidden", "published"} {
					b.exec(`UPDATE cities SET publication_status=$2 WHERE id=$1`, f.f.f.place.city, s)
				}
			case "extra_source_ABA":
				for _, body := range []string{"badminton source changed", "另一条独立羽毛球记录_MULTI_PRIVATE_BODY_CANARY"} {
					b.exec(`UPDATE moments SET body=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.grants[1].Selection.MomentID, body)
				}
			case "account_ABA":
				for _, s := range []string{"suspended", "active"} {
					b.exec(`UPDATE accounts SET status=$2 WHERE id=$1`, b.person.ID, s)
				}
			case "agent_ABA":
				for _, s := range []string{"suspended", "active"} {
					b.exec(`UPDATE agents SET status=$2 WHERE id=$1`, g.AgentID, s)
				}
			case "metadata_ABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, g.AgentID)
			case "wrong_owner":
				a = f.f.f.place.private.peer
			case "wrong_session":
				a.SessionDigest = [32]byte{78}
			}
			before := multiState(t, f)
			r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
			multiZero(t, r, e)
			if multiState(t, f) != before {
				t.Fatal("denied stage changed candidate/effect/inbox/outbox/memory")
			}
		})
	}
}
func TestMultiCandidateNativeHumanCurrentSupportAndExplicitIndependence(t *testing.T) {
	for _, mode := range []string{"missing_binding", "extra_analysis_revoke", "task_ABA", "extra_source_ABA", "disabled_guard"} {
		t.Run(mode, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			_, g := f.approve(t)
			flags := pipelineFlags(t, true)
			r, e := NewMultiCandidatePipeline(b.store, flags).StageOwnMultiCandidate(b.ctx, a, g.ID)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "missing_binding":
				b.exec(`DELETE FROM agent_multi_candidate_bindings WHERE grant_id=$1`, g.ID)
			case "extra_analysis_revoke":
				b.exec(`UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, f.grants[1].ID)
			case "task_ABA":
				b.exec(`UPDATE agent_tasks SET updated_at=clock_timestamp() WHERE id=$1`, f.f.f.task.ID)
			case "extra_source_ABA":
				b.exec(`UPDATE moments SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.grants[1].Selection.MomentID)
			case "disabled_guard":
				b.exec(`ALTER TABLE agent_effect_ledger DISABLE TRIGGER candidate_pipeline_atomic_final`)
			}
			human := NewMemoryCandidateService(b.store, flags)
			row, e := human.ReadOwnCandidate(b.ctx, a, r.CandidateID)
			if mode == "disabled_guard" {
				if !errors.Is(e, agentmemory.ErrUnavailable) {
					t.Fatal("missing safety guard accepted", e)
				}
				return
			}
			if e != nil || row.Status != agentmemorycandidate.Expired || row.Predicate != "" || len(row.Sources) != 0 || row.Assessment != nil {
				t.Fatal("stale human support", row, e)
			}
			list, e := human.ListOwnCandidates(b.ctx, a)
			if e != nil || len(list) != 1 || list[0].Status != agentmemorycandidate.Expired || len(list[0].Sources) != 0 {
				t.Fatal("list retained stale support", e)
			}
			var id string
			if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
				t.Fatal(e)
			}
			if _, e = human.PreviewOwnAcceptance(b.ctx, a, r.CandidateID, 1, id, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)); e == nil {
				t.Fatal("stale human approval preview")
			}
		})
	}
}
func TestMultiCandidateNativeAtomicRollbackFaultMatrix(t *testing.T) {
	for _, point := range []string{"after_claim", "after_candidate", "after_effect", "after_inbox", "after_checkpoint", "before_commit"} {
		t.Run(point, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			_, g := f.approve(t)
			before := multiState(t, f)
			svc := amc.NewService(&multiCandidateExecutor{store: b.store, Store: b.store, hook: func(s string) error {
				if s == point {
					return amc.ErrUnavailable
				}
				return nil
			}}, pipelineFlags(t, true))
			r, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
			multiZero(t, r, e)
			if before != multiState(t, f) {
				t.Fatal("fault committed partial business effect", point)
			}
		})
	}
}
func TestMultiCandidateNativeStableAnchorOrderAndSingleMutualExclusion(t *testing.T) {
	for _, first := range []string{"multi", "single"} {
		t.Run(first, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			p, g := f.approve(t)
			var anchor string
			for _, s := range p.Review.SourceSelections {
				if s.Source.Selector.ID == p.Review.AnchorSource.Selector.ID {
					anchor = s.AnalysisGrantID
				}
			}
			sp, e := b.store.PreviewOwnCandidateRetention(b.ctx, a, acr.Selection{AnalysisGrantID: anchor, RetainUntil: f.selection.RetainUntil})
			if e != nil {
				t.Fatal(e)
			}
			sg, e := b.store.ApproveOwnCandidateRetention(b.ctx, a, sp.ID)
			if e != nil {
				t.Fatal(e)
			}
			if first == "multi" {
				r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
				if e != nil {
					t.Fatal(e)
				}
				before := multiState(t, f)
				if _, e = NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, sg.ID); e == nil {
					t.Fatal("single duplicate effect")
				}
				reversed := f.selection
				reversed.AnalysisGrantIDs = []string{f.selection.AnalysisGrantIDs[1], f.selection.AnalysisGrantIDs[0]}
				rp, e := b.store.PreviewOwnMultiCandidate(b.ctx, a, reversed)
				if e != nil || rp.Review.AnchorEventID != r.EventID {
					t.Fatal("anchor changed", e)
				}
				rg, e := b.store.ApproveOwnMultiCandidate(b.ctx, a, rp.ID)
				if e != nil {
					t.Fatal(e)
				}
				rr, err := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, rg.ID)
				multiZero(t, rr, err)
				if before != multiState(t, f) {
					t.Fatal("reordered selection wrote different effect")
				}
			} else {
				if _, e = NewCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMomentCandidate(b.ctx, a, sg.ID); e != nil {
					t.Fatal(e)
				}
				before := multiState(t, f)
				r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
				multiZero(t, r, e)
				if before != multiState(t, f) {
					t.Fatal("multi bypassed original single effect")
				}
			}
			var n int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1`, b.person.ID).Scan(&n); e != nil || n != 1 {
				t.Fatal("effect multiplicity", n, e)
			}
		})
	}
}
func TestMultiCandidateNativeConcurrentOriginalKeyOneEffect(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	_, g := f.approve(t)
	svc := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true))
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
			if e == nil {
				if !r.Committed {
					errs <- errors.New("success no native commit")
				}
			} else if !errors.Is(e, amc.ErrBusy) {
				errs <- e
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	var n int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1`, b.person.ID).Scan(&n); e != nil || n != 1 {
		t.Fatal("concurrent duplicate", n, e)
	}
}
func TestMultiCandidateNativeUsedDownAndOriginalDeadlineCleanup(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	var at time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		t.Fatal(e)
	}
	f.selection.RetainUntil = at.UTC().Truncate(time.Microsecond).Add(5 * time.Second)
	_, g := f.approve(t)
	r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil {
		t.Fatal(e)
	}
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "091_agent_multi_candidate_pipeline.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	before := multiState(t, f)
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = conn.Exec(b.ctx, string(down))
	if e == nil {
		t.Fatal("used091 down erased history")
	}
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "55000" {
		t.Fatal("unexpected down denial", e)
	}
	conn.Exec(b.ctx, `ROLLBACK`)
	conn.Release()
	if before != multiState(t, f) {
		t.Fatal("used down was not atomic")
	}
	until := time.Now().Add(8 * time.Second)
	for {
		if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
			t.Fatal(e)
		}
		if !at.Before(g.ExpiresAt) {
			break
		}
		if time.Now().After(until) {
			t.Fatal("bounded native expiry wait")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var n int
	if e = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&n); e != nil || n != 1 {
		t.Fatal("multi cleanup", n, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT birdtie_expire_candidate_pipeline(100)`).Scan(&n); e != nil || n != 0 {
		t.Fatal("repeat cleanup", n, e)
	}
	var raw string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(c)::text FROM agent_memory_candidates c WHERE id=$1`, r.CandidateID).Scan(&raw); e != nil || strings.Contains(raw, "badminton") || !strings.Contains(raw, "EXPIRED") {
		t.Fatal("expired support not erased", e)
	}
}

func TestMultiCandidateNativeNewSessionMetadataOnlyAndHumanNoInheritedSupport(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	_, g := f.approve(t)
	flags := pipelineFlags(t, true)
	svc := NewMultiCandidatePipeline(b.store, flags)
	r, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '2 hours',now()+interval '1 hour')`, b.person.ID, digest[:])
	a.SessionDigest = digest
	read, e := svc.ReadOwnMultiCandidateReceipt(b.ctx, a, g.ID)
	if e != nil || !read.Committed || read.Candidate != nil || read.CandidateID != r.CandidateID {
		t.Fatal("new session inherited machine body", e)
	}
	row, e := NewMemoryCandidateService(b.store, flags).ReadOwnCandidate(b.ctx, a, r.CandidateID)
	if e != nil || row.Status != agentmemorycandidate.Expired || row.Category != "" {
		t.Fatal("new session inherited candidate support", e)
	}
	r, e = svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
	multiZero(t, r, e)
}
func TestMultiCandidateNativeRealSourceTableWaitAndFinalDeadline(t *testing.T) {
	for _, mode := range []string{"extra_analysis_revoke", "extra_source_ABA", "task_ABA", "natural_expiry", "session_expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			if mode == "natural_expiry" {
				var at time.Time
				if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
					t.Fatal(e)
				}
				f.selection.RetainUntil = at.UTC().Truncate(time.Microsecond).Add(5 * time.Second)
			}
			_, g := f.approve(t)
			if mode == "session_expiry" {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '2 seconds' WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			before := multiState(t, f)
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			done := make(chan error, 1)
			consumed := false
			defer func() {
				cancel()
				held.Rollback(context.Background())
				if !consumed {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("bounded stage drain failed")
					}
				}
			}()
			if _, e = held.Exec(ctx, `LOCK TABLE moments IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			go func() {
				r, e := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true)).StageOwnMultiCandidate(ctx, a, g.ID)
				if e == nil || r.Committed || r.Candidate != nil {
					done <- errors.New("wait released private candidate")
				} else {
					done <- nil
				}
			}()
			until := time.Now().Add(4 * time.Second)
			for {
				var waiting bool
				if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%LOCK TABLE%moments%')`, int(held.Conn().PgConn().PID())).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					t.Log("actual member source table wait", mode)
					break
				}
				select {
				case e := <-done:
					consumed = true
					t.Fatal("stage ended before selected-source wait", e)
				default:
				}
				if time.Now().After(until) {
					t.Fatal("source wait not observed")
				}
				time.Sleep(10 * time.Millisecond)
			}
			switch mode {
			case "extra_analysis_revoke":
				_, e = held.Exec(ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, f.grants[1].ID)
			case "extra_source_ABA":
				for _, s := range []string{"source changed", "另一条独立羽毛球记录_MULTI_PRIVATE_BODY_CANARY"} {
					if _, e = held.Exec(ctx, `UPDATE moments SET body=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, f.grants[1].Selection.MomentID, s); e != nil {
						break
					}
				}
			case "task_ABA":
				for _, q := range []string{"changed", f.f.f.task.Query} {
					if _, e = held.Exec(ctx, `UPDATE agent_tasks SET query=$2,filters=jsonb_set(filters,'{currentQuery}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1`, f.f.f.task.ID, q); e != nil {
						break
					}
				}
			default:
				deadline := g.ExpiresAt
				if mode == "session_expiry" {
					if e = held.QueryRow(ctx, `SELECT idle_expires_at FROM sessions WHERE token_sha256=$1`, a.SessionDigest[:]).Scan(&deadline); e != nil {
						t.Fatal(e)
					}
				}
				until := time.Now().Add(8 * time.Second)
				for {
					var at time.Time
					if e = held.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
						t.Fatal(e)
					}
					if !at.Before(deadline) {
						t.Log("original native deadline elapsed", mode, at.UTC(), deadline.UTC())
						break
					}
					if time.Now().After(until) {
						t.Fatal("native clock deadline wait bound")
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = held.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				consumed = true
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("selected-source wait did not finish")
			}
			if before != multiState(t, f) {
				t.Fatal("wait-time revocation/expiry committed effects")
			}
		})
	}
}

func TestMultiCandidateNativeSQLNonnullProofNotGoOnly(t *testing.T) {
	for _, field := range []string{"anchorEventId", "logicalOperationId", "taskId", "analysisPreviewId"} {
		t.Run(field, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			p, g := f.approve(t)
			before := multiState(t, f)
			raw, _ := json.Marshal(p.Review)
			var review map[string]any
			if e := json.Unmarshal(raw, &review); e != nil {
				t.Fatal(e)
			}
			if field == "analysisPreviewId" {
				review["sourceSelections"].([]any)[0].(map[string]any)[field] = nil
			} else {
				review[field] = nil
			}
			bad, _ := json.Marshal(review)
			key, e := multiPipelineKey(contextBuilderBinding{owner: g.Owner.ID, agent: g.AgentID}, multiRetentionStored{operation: p.Review.LogicalOperationID})
			if e != nil {
				t.Fatal(e)
			}
			seen := false
			svc := amc.NewService(&multiCandidateExecutor{store: b.store, Store: b.store, txHook: func(ctx context.Context, tx pgx.Tx, stage string) error {
				if stage != "after_candidate" {
					return nil
				}
				// A forged metadata row is inserted only inside this rollback-only negative
				// transaction. Its native fields/grants are valid; NULL proof must be rejected
				// by the database itself, not solely the HTTP/Go validator or old digest.
				var pid, gid string
				e := tx.QueryRow(ctx, `INSERT INTO agent_multi_candidate_previews(id,owner_id,agent_id,owner_type,session_id,selection,authority,source_frame,review_digest,event_id,logical_operation_id,algorithm_version,observed_at,expires_at) SELECT gen_random_uuid(),owner_id,agent_id,owner_type,session_id,selection,authority,source_frame,encode(sha256(convert_to($2::text,'UTF8')),'hex'),event_id,logical_operation_id,algorithm_version,observed_at,expires_at FROM agent_multi_candidate_previews WHERE id=$1 RETURNING id`, p.ID, string(bad)).Scan(&pid)
				if e != nil {
					return e
				}
				e = tx.QueryRow(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,actions,purpose,created_at,expires_at,revision) SELECT gen_random_uuid(),owner_account_id,recipient_account_id,resource_type,$2,actions,purpose,created_at,expires_at,1 FROM consent_grants WHERE id=$1 RETURNING id`, g.ID, pid).Scan(&gid)
				if e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO agent_multi_candidate_bindings(grant_id,preview_id) VALUES($1,$2)`, gid, pid); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `SAVEPOINT null_proof`); e != nil {
					return e
				}
				_, e = tx.Exec(ctx, `SELECT birdtie_multi_candidate_effect_validate(jsonb_populate_record(NULL::agent_effect_ledger,
 jsonb_build_object('subject_id',c.owner_id,'effect_key',$1::text,'agent_id',c.agent_id,'logical_operation_id',d.logical_operation_id,'action_id','519fde3b-cc12-4abc-8c4f-f3227290a815','effect_kind','MEMORY_CANDIDATE','action_digest',encode(sha256(convert_to($2::text,'UTF8')),'hex'),'source_id',d.source_id,'source_revision',d.source_revision,'candidate_id',c.id,'retention_grant_id',$3::uuid,'event_id',d.event_id,'handler_version','mom-candidate-multi-v1','fence',d.fence,'attempt',d.attempt,'proof_review',$2::text))) FROM agent_memory_candidates c JOIN agent_domain_outbox d ON d.event_id=$4 WHERE c.owner_id=$5 AND c.status='CANDIDATE'`, key, string(bad), gid, p.Review.AnchorEventID, g.Owner.ID)
				var pe *pgconn.PgError
				if !errors.As(e, &pe) || pe.Code != "55000" || !strings.Contains(pe.Message, "nonnull") {
					return errors.New("database did not reject exact NULL proof type")
				}
				if _, e = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT null_proof`); e != nil {
					return e
				}
				seen = true
				t.Log("native closed proof null rejected", field, pe.Code)
				return amc.ErrUnavailable
			}}, pipelineFlags(t, true))
			r, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
			multiZero(t, r, e)
			if !seen {
				t.Fatal("negative did not reach database typed proof check", e)
			}
			if before != multiState(t, f) {
				t.Fatal("malformed proof changed original effects")
			}
		})
	}
}

func TestMultiCandidateNativeTrustedBoundSubmitterExactSources(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	svc := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true))
	refs := []agentcognitive.SourceReference{}
	for _, s := range p.Review.Sources {
		refs = append(refs, agentcognitive.SourceReference{Type: "MOMENT", ID: s.Selector.ID, Version: s.Version.Revision, Owner: a.WorkspacePrincipal})
	}
	req := agentcognitive.CandidateSubmission{Request: agentcognitive.ReadRequest{Version: agentcognitive.ContractVersion, RequestID: g.ID, TaskID: p.Review.TaskID, Agent: agentcognitive.AgentReference{AgentID: g.AgentID, Principal: a.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, Purpose: agentcognitive.SubmitMemoryCandidate, Scope: agentruntime.Private, Source: refs[0], ExpiresAt: g.ExpiresAt}, Sources: refs}
	req.PayloadDigest, _ = amc.DigestReview(*p.Review)
	before := multiState(t, f)
	for _, mode := range []string{"digest", "subset", "wrong_task", "wrong_anchor"} {
		bad := req
		switch mode {
		case "digest":
			bad.PayloadDigest = strings.Repeat("f", 64)
		case "subset":
			bad.Sources = refs[:1]
		case "wrong_task":
			bad.Request.TaskID = g.ID
		case "wrong_anchor":
			bad.Request.Source = refs[1]
		}
		r, e := svc.BindCandidateSubmitter(a, g.ID).SubmitMemoryCandidate(b.ctx, bad)
		if e == nil || r.Status == agentcognitive.Available {
			t.Fatal("submission consistency became permission", mode, e)
		}
		if before != multiState(t, f) {
			t.Fatal("invalid typed port wrote", mode)
		}
	}
	out, e := svc.BindCandidateSubmitter(a, g.ID).SubmitMemoryCandidate(b.ctx, req)
	if e != nil || out.Status != agentcognitive.Available {
		t.Fatal("actual trusted candidate producer port", out, e)
	}
	receipt, e := svc.ReadOwnMultiCandidateReceipt(b.ctx, a, g.ID)
	if e != nil || receipt.Candidate == nil || len(receipt.Candidate.Sources) != 2 {
		t.Fatal("real producer did not commit candidate", e)
	}
}
func TestMultiCandidateNativeFinalControllerABAAndMissingCheckpoint(t *testing.T) {
	for _, mode := range []string{"controller_ABA", "missing_checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			f := multiNative(t)
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			_, g := f.approve(t)
			flags := pipelineFlags(t, true)
			before := multiState(t, f)
			seen := false
			svc := amc.NewService(&multiCandidateExecutor{store: b.store, Store: b.store, txHook: func(ctx context.Context, tx pgx.Tx, stage string) error {
				if mode == "missing_checkpoint" && stage == "after_effect" {
					_, e := tx.Exec(ctx, `SET CONSTRAINTS candidate_pipeline_atomic_final IMMEDIATE`)
					var pe *pgconn.PgError
					if !errors.As(e, &pe) || pe.Code != "55000" {
						return errors.New("missing checkpoint was not independently rejected")
					}
					seen = true
					return amc.ErrUnavailable
				}
				if mode == "controller_ABA" && stage == "before_commit" {
					flags.Disable(agentfeature.Memory)
					cfg, e := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
					if e != nil {
						return e
					}
					e = flags.Replace(flags.Revision(), cfg)
					seen = e == nil
					return e
				}
				return nil
			}}, flags)
			r, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
			multiZero(t, r, e)
			if !seen || before != multiState(t, f) {
				t.Fatal("final brake/checkpoint guard missing or wrote partial state", mode, e)
			}
		})
	}
}

func TestMultiCandidateNativeCurrentDataSingleMemoryAuditAndFullXminRoundtrip(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	dependent092 := candidateVocabularyFixture(t, b.pool, b.ctx, false)
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "091_agent_multi_candidate_pipeline.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "091_agent_multi_candidate_pipeline.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
		t.Fatal("initial unused down to actual090", e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var hasMulti, hasVocabulary bool
		if e := b.pool.QueryRow(ctx, `SELECT to_regclass('public.agent_multi_candidate_bindings') IS NOT NULL,to_regprocedure('public.birdtie_candidate_algorithm_vocabulary(text,text)') IS NOT NULL`).Scan(&hasMulti, &hasVocabulary); e != nil {
			t.Error("restore schema before owned fixture cleanup", e)
			return
		}
		if !hasMulti {
			if _, e := b.pool.Exec(ctx, string(up)); e != nil {
				t.Error("restore original091 on failed positive", e)
				return
			}
		}
		if dependent092 && !hasVocabulary {
			raw, e := os.ReadFile("../../migrations/092_agent_candidate_hiking_vocabulary.sql")
			if e != nil {
				t.Error(e)
				return
			}
			if _, e = b.pool.Exec(ctx, string(raw)); e != nil {
				t.Error("restore current092 on failed positive", e)
			}
		}
	})
	sp, e := b.store.previewOwnCandidateRetentionVersion(b.ctx, a, acr.Selection{AnalysisGrantID: f.grants[0].ID, RetainUntil: f.selection.RetainUntil}, agentlocalcandidate.LegacyVersion)
	if e != nil {
		t.Fatal(e)
	}
	sg, e := b.store.ApproveOwnCandidateRetention(b.ctx, a, sp.ID)
	if e != nil {
		t.Fatal(e)
	}
	// Use the original 083/090 runtime and actual 082 producer, rather than
	// inserting synthetic Run/Step/Dispatch rows or relabelling a direct Stage.
	runs := NewAgentRuns(b.store, pipelineFlags(t, true))
	run, e := runs.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID, RetentionGrantID: sg.ID})
	requireRun(t, run, e, ar.Queued)
	claim, e := runs.ClaimAgentRun(b.ctx, runWorker1)
	if e != nil || claim.RunID != run.ID {
		t.Fatal("original090 actual claim", e)
	}
	completed, e := runs.ExecuteAgentRun(b.ctx, claim)
	requireRun(t, completed, e, ar.Succeeded)
	original, e := NewCandidatePipeline(b.store, pipelineFlags(t, true)).ReadOwnMomentCandidateReceipt(b.ctx, a, sg.ID)
	if e != nil || !original.Committed || len(original.Candidate.Sources) != 1 || completed.CandidateID != original.CandidateID {
		t.Fatal("actual original single-source Run and effect before091", e)
	}
	var runtimeCounts []int64
	if e = b.pool.QueryRow(b.ctx, `SELECT ARRAY[(SELECT count(*) FROM agent_enrichment_runs WHERE id=$1),(SELECT count(*) FROM agent_run_steps WHERE run_id=$1),(SELECT count(*) FROM agent_run_dispatches WHERE run_id=$1 AND state='COMMITTED'),(SELECT count(*) FROM agent_run_audit WHERE run_id=$1)]`, run.ID).Scan(&runtimeCounts); e != nil {
		t.Fatal(e)
	}
	for _, n := range runtimeCounts {
		if n < 1 {
			t.Fatal("nonempty original090 native runtime history missing", runtimeCounts)
		}
	}
	t.Log("actual090 positive original083 STAGE_CANDIDATE Run/Step/COMMITTED dispatch/Run audit counts", runtimeCounts)
	var mid string
	if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&mid); e != nil {
		t.Fatal(e)
	}
	memory, e := b.store.PutOwnMemory(b.ctx, a, mid, agentMemoryInput("old.before091.statement"))
	if e != nil || memory.SourceType != agentmemory.SourceExplicit {
		t.Fatal("actual old explicit Memory", e)
	}
	var counts []int64
	if e = b.pool.QueryRow(b.ctx, `SELECT ARRAY[(SELECT count(*) FROM agent_memories WHERE owner_id=$1),(SELECT count(*) FROM agent_memory_candidates WHERE owner_id=$1),(SELECT count(*) FROM agent_effect_ledger WHERE subject_id=$1),(SELECT count(*) FROM audit_events WHERE actor_account_id=$1)]`, b.person.ID).Scan(&counts); e != nil {
		t.Fatal(e)
	}
	for _, n := range counts {
		if n < 1 {
			t.Fatal("positive retained domain not exercised", counts)
		}
	}
	t.Log("actual090 positive old Memory/Candidate/single effect/audit counts", counts)
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Exec(b.ctx, `SET TIME ZONE 'UTC';CREATE FUNCTION pg_temp.multi_old_snapshot() RETURNS jsonb LANGUAGE plpgsql AS $$ DECLARE item record; r jsonb; result jsonb:='{}';BEGIN FOR item IN SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LOOP EXECUTE format('SELECT coalesce(jsonb_agg(jsonb_build_object(''row'',to_jsonb(t),''xmin'',t.xmin::text) ORDER BY to_jsonb(t)::text),''[]''::jsonb) FROM public.%I t',item.tablename) INTO r;result:=result||jsonb_build_object(item.tablename,r);END LOOP;RETURN result;END $$`); e != nil {
		t.Fatal(e)
	}
	snapshot := func() string {
		var out string
		if e := conn.QueryRow(b.ctx, `SELECT pg_temp.multi_old_snapshot()::text`).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	catalog := func() string {
		var out string
		if e := conn.QueryRow(b.ctx, `SELECT jsonb_build_object(
 'functions',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',p.proname,'args',pg_get_function_identity_arguments(p.oid),'definition',pg_get_functiondef(p.oid)) ORDER BY p.proname,pg_get_function_identity_arguments(p.oid)),'[]') FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind='f'),
 'triggers',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',t.tgname,'definition',pg_get_triggerdef(t.oid),'enabled',t.tgenabled) ORDER BY c.relname,t.tgname),'[]') FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND NOT t.tgisinternal),
 'constraints',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',r.relname,'name',c.conname,'definition',pg_get_constraintdef(c.oid)) ORDER BY r.relname,c.conname),'[]') FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid JOIN pg_namespace n ON n.oid=r.relnamespace WHERE n.nspname='public'),
 'columns',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',a.attname,'ordinal',a.attnum,'type',format_type(a.atttypid,a.atttypmod),'notNull',a.attnotnull,'default',pg_get_expr(d.adbin,d.adrelid),'identity',a.attidentity,'generated',a.attgenerated) ORDER BY c.relname,a.attnum),'[]') FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m') AND a.attnum>0 AND NOT a.attisdropped),
 'indexes',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',t.relname,'name',i.relname,'definition',pg_get_indexdef(x.indexrelid),'valid',x.indisvalid,'ready',x.indisready) ORDER BY t.relname,i.relname),'[]') FROM pg_index x JOIN pg_class t ON t.oid=x.indrelid JOIN pg_class i ON i.oid=x.indexrelid JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname='public'),
 'tables',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',c.relname,'kind',c.relkind,'owner',pg_get_userbyid(c.relowner),'rls',c.relrowsecurity,'forceRLS',c.relforcerowsecurity,'privileges',c.relacl) ORDER BY c.relname),'[]') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m','S')),
 'policies',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY tablename,policyname),'[]') FROM pg_policies p WHERE schemaname='public'));
 `).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	before, cat := snapshot(), catalog()
	if _, e = conn.Exec(b.ctx, string(up)); e != nil {
		t.Fatal("upgrade positive090", e)
	}
	installedCat := catalog()
	strip := func(raw string) string {
		var m map[string]json.RawMessage
		if e := json.Unmarshal([]byte(raw), &m); e != nil {
			t.Fatal(e)
		}
		for _, name := range []string{"agent_multi_candidate_previews", "agent_multi_candidate_bindings"} {
			if string(m[name]) != "[]" {
				t.Fatal("migration produced unapproved metadata")
			}
			delete(m, name)
		}
		v, _ := json.Marshal(m)
		return string(v)
	}
	compact := func(raw string) string {
		var m map[string]json.RawMessage
		json.Unmarshal([]byte(raw), &m)
		v, _ := json.Marshal(m)
		return string(v)
	}
	if strip(snapshot()) != compact(before) {
		t.Fatal("091 upgrade changed old rows/IDs/xmin")
	}
	if _, e = conn.Exec(b.ctx, string(down)); e != nil {
		t.Fatal("unused down with true old single history", e)
	}
	if compact(snapshot()) != compact(before) || catalog() != cat {
		t.Fatal("unused091 down changed full old rows/xmin/seven-kind semantic catalog")
	}
	if _, e = conn.Exec(b.ctx, string(up)); e != nil {
		t.Fatal("reapply positive old090", e)
	}
	if strip(snapshot()) != compact(before) || catalog() != installedCat {
		t.Fatal("091 reapply changed old row/xmin or new catalog")
	}
	if dependent092 {
		candidateVocabularyFixture(t, b.pool, b.ctx, true)
	}
	t.Log("091 true old single+explicit Memory+Run/Step/Dispatch/RunAudit+audit complete public rows/all xmin/seven catalog up/unuseddown/reapply preserved")
}

func TestMultiCandidateNativeHikingSQLVersionProofNotGoOnly(t *testing.T) {
	for _, field := range []string{"v1_hiking", "wrong_preview_version"} {
		t.Run(field, func(t *testing.T) {
			f := multiNative(t, "徒步记录", "hiking")
			b := f.f.f.place.private.base
			a := f.f.f.place.private.owner
			p, g := f.approve(t)
			before := multiState(t, f)
			raw, _ := json.Marshal(p.Review)
			var review map[string]any
			if e := json.Unmarshal(raw, &review); e != nil {
				t.Fatal(e)
			}
			review["proposal"].(map[string]any)["algorithmVersion"] = agentlocalcandidate.LegacyVersion
			bad, _ := json.Marshal(review)
			key, e := multiPipelineKey(contextBuilderBinding{owner: g.Owner.ID, agent: g.AgentID}, multiRetentionStored{operation: p.Review.LogicalOperationID})
			if e != nil {
				t.Fatal(e)
			}
			seen := false
			svc := amc.NewService(&multiCandidateExecutor{store: b.store, Store: b.store, txHook: func(ctx context.Context, tx pgx.Tx, stage string) error {
				if stage != "after_candidate" {
					return nil
				}
				// A forged metadata row is inserted only inside this rollback-only negative
				// transaction. Its native fields/grants are valid; NULL proof must be rejected
				// by the database itself, not solely the HTTP/Go validator or old digest.
				var pid, gid string
				e := tx.QueryRow(ctx, `INSERT INTO agent_multi_candidate_previews(id,owner_id,agent_id,owner_type,session_id,selection,authority,source_frame,review_digest,event_id,logical_operation_id,algorithm_version,observed_at,expires_at) SELECT gen_random_uuid(),owner_id,agent_id,owner_type,session_id,selection,authority,source_frame,encode(sha256(convert_to($2::text,'UTF8')),'hex'),event_id,logical_operation_id,CASE WHEN $3::boolean THEN 'moment-lexical-category-v1' ELSE algorithm_version END,observed_at,expires_at FROM agent_multi_candidate_previews WHERE id=$1 RETURNING id`, p.ID, string(bad), field == "v1_hiking").Scan(&pid)
				if e != nil {
					return e
				}
				e = tx.QueryRow(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,actions,purpose,created_at,expires_at,revision) SELECT gen_random_uuid(),owner_account_id,recipient_account_id,resource_type,$2,actions,purpose,created_at,expires_at,1 FROM consent_grants WHERE id=$1 RETURNING id`, g.ID, pid).Scan(&gid)
				if e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO agent_multi_candidate_bindings(grant_id,preview_id) VALUES($1,$2)`, gid, pid); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `SAVEPOINT null_proof`); e != nil {
					return e
				}
				_, e = tx.Exec(ctx, `SELECT birdtie_multi_candidate_effect_validate(jsonb_populate_record(NULL::agent_effect_ledger,
 jsonb_build_object('subject_id',c.owner_id,'effect_key',$1::text,'agent_id',c.agent_id,'logical_operation_id',d.logical_operation_id,'action_id','519fde3b-cc12-4abc-8c4f-f3227290a815','effect_kind','MEMORY_CANDIDATE','action_digest',encode(sha256(convert_to($2::text,'UTF8')),'hex'),'source_id',d.source_id,'source_revision',d.source_revision,'candidate_id',c.id,'retention_grant_id',$3::uuid,'event_id',d.event_id,'handler_version','mom-candidate-multi-v1','fence',d.fence,'attempt',d.attempt,'proof_review',$2::text))) FROM agent_memory_candidates c JOIN agent_domain_outbox d ON d.event_id=$4 WHERE c.owner_id=$5 AND c.status='CANDIDATE'`, key, string(bad), gid, p.Review.AnchorEventID, g.Owner.ID)
				var pe *pgconn.PgError
				if !errors.As(e, &pe) || pe.Code != "55000" {
					return errors.New("database did not reject algorithm/category or original preview mismatch")
				}
				if _, e = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT null_proof`); e != nil {
					return e
				}
				seen = true
				t.Log("native closed hiking algorithm proof rejected", field, pe.Code)
				return amc.ErrUnavailable
			}}, pipelineFlags(t, true))
			r, e := svc.StageOwnMultiCandidate(b.ctx, a, g.ID)
			multiZero(t, r, e)
			if !seen {
				t.Fatal("negative did not reach database typed proof check", e)
			}
			if before != multiState(t, f) {
				t.Fatal("malformed proof changed original effects")
			}
		})
	}
}
