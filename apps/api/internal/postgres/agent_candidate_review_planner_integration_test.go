package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

// These are actual native human-created candidates and native identity/source
// checks. They do not emulate an automatic inference provider or execute an
// action. The full row payload and xmin are checked without lifecycle refresh.
func plannerCandidateSnapshot(t *testing.T, f *enrichmentFixture) string {
	t.Helper()
	b := f.base.private.base
	var raw string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
	'candidates',(SELECT coalesce(jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY id),'[]') FROM agent_memory_candidates x WHERE owner_id=ANY($1::uuid[])),
	'memories',(SELECT coalesce(jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY id),'[]') FROM agent_memories x WHERE owner_id=ANY($1::uuid[])),
	'evidence',(SELECT coalesce(jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY id),'[]') FROM agent_memory_evidence x WHERE memory_id IN(SELECT id FROM agent_memories WHERE owner_id=ANY($1::uuid[]))),
	'ledger',(SELECT coalesce(jsonb_agg(to_jsonb(x)||jsonb_build_object('xmin',x.xmin::text) ORDER BY effect_key),'[]') FROM agent_effect_ledger x WHERE candidate_id IN(SELECT id FROM agent_memory_candidates WHERE owner_id=ANY($1::uuid[]))),
	'modelRuns',(SELECT count(*) FROM model_request_runs WHERE owner_id=ANY($1::uuid[])),
	'reservations',(SELECT count(*) FROM model_budget_reservations WHERE owner_id=ANY($1::uuid[]))
	)::text`, b.accounts).Scan(&raw)
	if e != nil {
		t.Fatal("native candidate row/xmin snapshot", e)
	}
	return raw
}

func TestBoundedPlannerNativeCandidateReviewExactIDNoEffects(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	r := memoryCandidateSave(t, f, s)
	input := agentplanner.CandidateSelection{ID: r.ID, ExpectedVersion: r.Version, LogicalOperationID: agentMemoryID(t, f.base.private)}
	before := plannerCandidateSnapshot(t, f)
	identityBefore := agentMemoryOwnedSourceSnapshot(t, f.base.private)
	service := agentplanner.NewService(b.store, s)
	first, e := service.Review(b.ctx, f.base.private.owner, input)
	if e != nil || first.Status != agentplanner.Proposed || len(first.Actions) != 1 || first.Actions[0].Tool != agentplanner.CandidateReview || first.Actions[0].Arguments.CandidateID != r.ID || first.Actions[0].Arguments.ExpectedVersion != r.Version || first.ModelAccess != "UNAVAILABLE" {
		t.Fatal("native existing candidate was not projected exactly", e, first)
	}
	if version, e := hex.DecodeString(first.Actions[0].ResourceVersion); e != nil || len(version) != 32 {
		t.Fatal("internal candidate generation was exposed as a source version", e)
	}
	for i := 0; i < 3; i++ {
		next, e := service.Review(b.ctx, f.base.private.owner, input)
		if e != nil || !reflect.DeepEqual(next.Actions, first.Actions) {
			t.Fatal("same logical operation changed proposal identity", e)
		}
	}
	raw, e := json.Marshal(first)
	decoded, de := agentplanner.Decode(raw)
	if e != nil || de != nil || !reflect.DeepEqual(decoded, first) {
		t.Fatal("actual native review wire not closed", e, de)
	}
	outputSaveWire(t, "native-candidate-review", raw)
	if before != plannerCandidateSnapshot(t, f) || identityBefore != agentMemoryOwnedSourceSnapshot(t, f.base.private) {
		t.Fatal("read-only review changed memory, candidate, source, model or effect authority")
	}
	var status string
	if b.pool.QueryRow(b.ctx, `SELECT status FROM agent_memory_candidates WHERE id=$1`, r.ID).Scan(&status) != nil || status != string(agentmemorycandidate.Candidate) {
		t.Fatal("review accepted or rejected candidate", status)
	}
}

func TestBoundedPlannerNativeCandidateClarifiesSelectionWithoutWrite(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	_ = memoryCandidateSave(t, f, s)
	before := plannerCandidateSnapshot(t, f)
	v, e := agentplanner.NewService(b.store, s).Review(b.ctx, f.base.private.owner, agentplanner.CandidateSelection{LogicalOperationID: agentMemoryID(t, f.base.private)})
	if e != nil || v.Status != agentplanner.Clarification || len(v.Clarifications) != 1 || len(v.Actions) != 0 || before != plannerCandidateSnapshot(t, f) {
		t.Fatal("missing candidate selection was inferred or written", e, v)
	}
	raw, _ := json.Marshal(v)
	outputSaveWire(t, "native-candidate-selection-clarification", raw)
}

func TestBoundedPlannerNativeCandidateRejectsInvalidCurrentSourceWithoutRefresh(t *testing.T) {
	for _, kind := range []string{"anonymous", "peer", "wrong-session", "organization", "unknown-id", "stale-version", "rejected", "accepted", "expired", "source-edit", "source-withdraw", "metadata-version", "session-revoked", "session-expired", "account-disabled", "agent-disabled", "flag-off", "default-off", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			var r agentmemorycandidate.Record
			if kind == "expired" {
				draft := memoryCandidateDraft(t, f)
				if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '1 second'`).Scan(&draft.ValidUntil); e != nil {
					t.Fatal(e)
				}
				var e error
				r, e = s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), draft, "", 0)
				if e != nil {
					t.Fatal(e)
				}
			} else {
				r = memoryCandidateSave(t, f, s)
			}
			input := agentplanner.CandidateSelection{ID: r.ID, ExpectedVersion: r.Version, LogicalOperationID: agentMemoryID(t, f.base.private)}
			a := f.base.private.owner
			ctx := b.ctx
			switch kind {
			case "anonymous":
				a = agentprofile.PrivateAccess{}
			case "peer":
				a = f.base.private.peer
			case "wrong-session":
				a.SessionDigest = f.base.private.peer.SessionDigest
			case "organization":
				a = f.base.private.org
			case "unknown-id":
				input.ID = agentMemoryID(t, f.base.private)
			case "stale-version":
				input.ExpectedVersion++
			case "rejected":
				if _, e := s.RejectOwnCandidate(ctx, a, r.ID, r.Version); e != nil {
					t.Fatal(e)
				}
			case "accepted":
				if _, e := s.AcceptOwnCandidate(ctx, a, memoryCandidatePreview(t, f, s, r)); e != nil {
					t.Fatal(e)
				}
			case "expired":
				if _, e := b.pool.Exec(b.ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM(valid_until-clock_timestamp())))+0.02) FROM agent_memory_candidates WHERE id=$1`, r.ID); e != nil {
					t.Fatal(e)
				}
			case "source-edit":
				b.exec(`UPDATE moments SET revision=revision+1,updated_at=clock_timestamp(),body='本地合成变更' WHERE id=$1`, f.sources[agentevent.MomentCreated])
			case "source-withdraw":
				if e := b.store.WithdrawMoment(ctx, b.person.ID, f.sources[agentevent.MomentCreated], r.Sources[0].Version.Revision); e != nil {
					t.Fatal(e)
				}
			case "metadata-version":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
			case "session-revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.base.private.ownerSession)
			case "session-expired":
				b.exec(`UPDATE sessions SET created_at=statement_timestamp()-interval '1 hour',idle_expires_at=statement_timestamp()-interval '1 second' WHERE id=$1`, f.base.private.ownerSession)
			case "account-disabled":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
			case "agent-disabled":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
			case "flag-off":
				if e := s.flags.Disable(agentfeature.Memory); e != nil {
					t.Fatal(e)
				}
			case "default-off":
				flags, e := agentfeature.NewController(agentfeature.DefaultConfig())
				if e != nil {
					t.Fatal(e)
				}
				s = NewMemoryCandidateService(b.store, flags)
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			before := plannerCandidateSnapshot(t, f)
			v, e := agentplanner.NewService(b.store, s).Review(ctx, a, input)
			if e == nil || !reflect.DeepEqual(v, agentplanner.View{}) || before != plannerCandidateSnapshot(t, f) {
				t.Fatal("stale/foreign candidate was released, refreshed or promoted", kind, e, v)
			}
		})
	}
}

func TestBoundedPlannerNativeCandidateDeadlineWaitDoesNotMutate(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	r := memoryCandidateSave(t, f, s)
	before := plannerCandidateSnapshot(t, f)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(b.ctx, `SELECT id FROM agent_memory_candidates WHERE id=$1 FOR UPDATE`, r.ID); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(b.ctx, 80*time.Millisecond)
	defer cancel()
	v, e := s.PlanOwnCandidateReview(ctx, f.base.private.owner, agentplanner.CandidateSelection{ID: r.ID, ExpectedVersion: r.Version, LogicalOperationID: agentMemoryID(t, f.base.private)})
	if e == nil || !reflect.DeepEqual(v, agentplanner.View{}) || before != plannerCandidateSnapshot(t, f) {
		t.Fatal("late locked candidate mutated/released", e)
	}
}

func TestBoundedPlannerNativeCandidateFlagABADuringActualLockWait(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	r := memoryCandidateSave(t, f, s)
	before := plannerCandidateSnapshot(t, f)
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()
	tx, e := b.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT id FROM agent_memory_candidates WHERE id=$1 FOR UPDATE`, r.ID); e != nil {
		t.Fatal(e)
	}
	var holder int
	if e = tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); e != nil {
		t.Fatal(e)
	}
	type answer struct {
		view agentplanner.View
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		v, e := s.PlanOwnCandidateReview(ctx, f.base.private.owner, agentplanner.CandidateSelection{ID: r.ID, ExpectedVersion: r.Version, LogicalOperationID: agentMemoryID(t, f.base.private)})
		done <- answer{v, e}
	}()
	for {
		var waiting bool
		if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM agent_memory_candidates%FOR UPDATE%')`, holder).Scan(&waiting); e != nil {
			t.Fatal("actual review wait not observed", e)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual review never reached native candidate lock", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if e = s.flags.Disable(agentfeature.Memory); e != nil {
		t.Fatal(e)
	}
	config, e := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.flags.Replace(s.flags.Revision(), config); e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	a := <-done
	if a.err == nil || !reflect.DeepEqual(a.view, agentplanner.View{}) || before != plannerCandidateSnapshot(t, f) {
		t.Fatal("old Memory ticket resumed after OFF/ON", a.err)
	}
}
