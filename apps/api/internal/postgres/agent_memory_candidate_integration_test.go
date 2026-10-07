package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func memoryCandidateFixture(t *testing.T) (*enrichmentFixture, *MemoryCandidateService) {
	t.Helper()
	f := enrichmentNativeFixture(t)
	s := NewMemoryCandidateService(f.base.private.base.store, reinforcementFlags(t))
	cleanupPool := f.base.private.base.pool
	var installed bool
	if s.store.pool.QueryRow(context.Background(), `SELECT to_regclass('agent_memory_candidates') IS NOT NULL`).Scan(&installed) != nil || !installed {
		t.Fatal("requires native063")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := cleanupPool.Exec(ctx, `DELETE FROM moment_activity_links WHERE moment_id IN(SELECT id FROM moments WHERE author_account_id=ANY($1::uuid[]))`, f.base.private.base.accounts); e != nil {
			t.Error(e)
		}
	})
	return f, s
}
func memoryCandidateDraft(t *testing.T, f *enrichmentFixture) agentmemorycandidate.Draft {
	t.Helper()
	var until time.Time
	if e := f.base.private.base.pool.QueryRow(context.Background(), `SELECT clock_timestamp()+interval '1 hour'`).Scan(&until); e != nil {
		t.Fatal(e)
	}
	a, _ := agentconfidence.NewUncalibratedScore(0.25)
	return agentmemorycandidate.Draft{Predicate: "ACTIVITY_CATEGORY", Category: "sports", Assessment: a, ValidUntil: until, Sources: []agentmemorycandidate.Selector{{Type: agentevent.MomentSource, ID: f.sources[agentevent.MomentCreated]}, {Type: agentevent.SavedPlaceSource, ID: f.sources[agentevent.PlaceSaved]}}}
}
func memoryCandidateSave(t *testing.T, f *enrichmentFixture, s *MemoryCandidateService) agentmemorycandidate.Record {
	t.Helper()
	r, e := s.SaveOwnCandidate(context.Background(), f.base.private.owner, agentMemoryID(t, f.base.private), memoryCandidateDraft(t, f), "", 0)
	if e != nil {
		t.Fatal("native save", e)
	}
	return r
}
func memoryCandidatePreview(t *testing.T, f *enrichmentFixture, s *MemoryCandidateService, r agentmemorycandidate.Record) *MemoryCandidatePreview {
	t.Helper()
	var until time.Time
	if s.store.pool.QueryRow(context.Background(), `SELECT clock_timestamp()+interval '1 day'`).Scan(&until) != nil {
		t.Fatal("db clock")
	}
	p, e := s.PreviewOwnAcceptance(context.Background(), f.base.private.owner, r.ID, r.Version, agentMemoryID(t, f.base.private), 0, until)
	if e != nil {
		t.Fatal("native preview", e)
	}
	return p
}
func TestMemoryCandidateNativeAtomicAcceptReplay100(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	before := agentMemoryOwnedSourceSnapshot(t, f.base.private)
	r := memoryCandidateSave(t, f, s)
	if r.Status != agentmemorycandidate.Candidate || r.Version != 1 {
		t.Fatal(r)
	}
	p := memoryCandidatePreview(t, f, s, r)
	review := p.Review()
	if review.Clusters != 2 || review.Purpose != "HUMAN_EXPLICIT_DECLARATION" || review.Statement != "我偏好运动活动" {
		t.Fatal(review)
	}
	review.Candidate.Category = "health"
	review.Candidate.Sources[0].Anchors[0] = "forged"
	v, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p)
	if e != nil || v.Status != agentmemorycandidate.Active || v.Version != 2 {
		t.Fatal(v, e)
	}
	for i := 0; i < 100; i++ {
		retry, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p)
		if e != nil || !reflect.DeepEqual(v, retry) {
			t.Fatalf("replay %d %v", i, e)
		}
	}
	t.Run("single-authority-effect", func(t *testing.T) {
		var memory, evidence int
		var nature string
		var confidence float64
		var last *time.Time
		if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, b.personID).Scan(&memory) != nil || memory != 1 {
			t.Fatal("duplicate memory", memory)
		}
		if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1 AND status='CURRENT'`, v.MemoryID).Scan(&evidence) != nil || evidence != 2 {
			t.Fatal("wrong evidence", evidence)
		}
		if b.pool.QueryRow(b.ctx, `SELECT source_type,confidence,last_reinforced_at FROM agent_memories WHERE id=$1`, v.MemoryID).Scan(&nature, &confidence, &last) != nil || nature != "EXPLICIT" || confidence != 1 || last != nil {
			t.Fatal("score promoted")
		}
	})
	t.Run("reopened", func(t *testing.T) {
		other := NewMemoryCandidateService(New(b.pool, true), reinforcementFlags(t))
		got, e := other.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
		if e != nil || !reflect.DeepEqual(got, v) {
			t.Fatal(e)
		}
	})
	t.Run("old-memory-005-060", func(t *testing.T) {
		in := candidateMemoryInput(r, 1, p.review.MemoryValidUntil)
		in.Summary = "本人修订后的声明"
		if _, e := b.store.PutOwnMemory(b.ctx, f.base.private.owner, *v.MemoryID, in); e != nil {
			t.Fatal(e)
		}
		got, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
		if e != nil || got.Status != agentmemorycandidate.Superseded || got.Category != "" || len(got.Sources) != 0 {
			t.Fatal(got, e)
		}
		var n int
		if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1 AND status='CURRENT'`, v.MemoryID).Scan(&n) != nil || n != 0 {
			t.Fatal("005 not invalidated")
		}
	})
	if before != agentMemoryOwnedSourceSnapshot(t, f.base.private) {
		t.Fatal("source identity mutated")
	}
}
func TestMemoryCandidateNativeRejectReplacementAndWeak(t *testing.T) {
	for _, name := range []string{"reject-retry-suppression", "replacement", "one-source-high-score", "same-activity", "source-expired"} {
		t.Run(name, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			d := memoryCandidateDraft(t, f)
			if name == "one-source-high-score" {
				d.Sources = d.Sources[:1]
				d.Assessment, _ = agentconfidence.NewUncalibratedScore(1)
			}
			if name == "same-activity" {
				d.Sources[1] = agentmemorycandidate.Selector{Type: agentevent.ParticipationSource, ID: f.sources[agentevent.ActivityJoined]}
				var activity string
				if b.pool.QueryRow(b.ctx, `SELECT activity_id FROM activity_participations WHERE id=$1`, d.Sources[1].ID).Scan(&activity) != nil {
					t.Fatal("activity")
				}
				if _, e := b.pool.Exec(b.ctx, `INSERT INTO moment_activity_links(moment_id,activity_id,city_id,author_confirmed_at) VALUES($1,$2,'aberdeen-gb',clock_timestamp())`, d.Sources[0].ID, activity); e != nil {
					t.Fatal(e)
				}
			}
			r, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), d, "", 0)
			if e != nil {
				t.Fatal(e)
			}
			switch name {
			case "reject-retry-suppression":
				v, e := s.RejectOwnCandidate(b.ctx, f.base.private.owner, r.ID, 1)
				if e != nil || v.Status != agentmemorycandidate.Rejected || v.Category != "" || len(v.Sources) != 0 {
					t.Fatal(v, e)
				}
				retry, e := s.RejectOwnCandidate(b.ctx, f.base.private.owner, r.ID, 1)
				if e != nil || !reflect.DeepEqual(v, retry) {
					t.Fatal(e)
				}
				d.Assessment, _ = agentconfidence.NewUncalibratedScore(0.82)
				suppressed, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), d, "", 0)
				if e != nil || !reflect.DeepEqual(v, suppressed) {
					t.Fatal("same source revived", e)
				}
			case "replacement":
				p := memoryCandidatePreview(t, f, s, r)
				d.Category = "culture"
				newR, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), d, r.ID, 1)
				if e != nil || newR.Status != agentmemorycandidate.Candidate {
					t.Fatal(e)
				}
				old, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
				if e != nil || old.Status != agentmemorycandidate.Superseded || old.Category != "" {
					t.Fatal(old, e)
				}
				if _, e = s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); e == nil {
					t.Fatal("stale approval")
				}
			case "one-source-high-score", "same-activity":
				if _, e = s.PreviewOwnAcceptance(b.ctx, f.base.private.owner, r.ID, 1, agentMemoryID(t, f.base.private), 0, d.ValidUntil); !errors.Is(e, agentmemory.ErrForbidden) {
					t.Fatal("weak made stable", e)
				}
			case "source-expired":
				if _, e = b.pool.Exec(b.ctx, `UPDATE moments SET revision=revision+1,body='合成修改',updated_at=clock_timestamp() WHERE id=$1`, d.Sources[0].ID); e != nil {
					t.Fatal(e)
				}
				got, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
				if e != nil || got.Status != agentmemorycandidate.Expired || got.Category != "" || len(got.Sources) != 0 {
					t.Fatal(got, e)
				}
			}
			var n int
			if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, b.personID).Scan(&n) != nil || n != 0 {
				t.Fatal("unauthorized stable write")
			}
		})
	}
}
func TestMemoryCandidateNativeAcceptanceBoundaries(t *testing.T) {
	names := []string{"wrong-session", "wrong-person", "organization", "missing-session", "stale-version", "json-preview", "other-service", "flag-off", "flag-off-on", "ctx-cancel", "metadata-version", "expired-preview", "source-edit", "source-withdraw", "source-rebuild", "link-change", "session-revoked", "session-expired", "idle-expired", "account-disabled", "agent-disabled", "metadata-deleted", "target-other-owner", "target-stale", "target-inferred"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			r := memoryCandidateSave(t, f, s)
			p := memoryCandidatePreview(t, f, s, r)
			access := f.base.private.owner
			ctx := b.ctx
			switch name {
			case "wrong-session":
				access.SessionDigest = [32]byte{1}
			case "wrong-person":
				access.WorkspacePrincipal.ID = b.other.ID
			case "organization":
				access.WorkspacePrincipal.Type = actorref.Organization
			case "missing-session":
				access.SessionDigest = [32]byte{}
			case "stale-version":
				p.review.Candidate.Version++
			case "json-preview":
				if _, e := json.Marshal(p); e == nil {
					t.Fatal("export authority")
				}
				var imported MemoryCandidatePreview
				if json.Unmarshal([]byte(`{"confirmed":true,"verified":true}`), &imported) == nil {
					t.Fatal("import authority")
				}
				p = &imported
			case "other-service":
				s = NewMemoryCandidateService(b.store, s.flags)
			case "flag-off":
				_ = s.flags.Disable(agentfeature.Memory)
			case "flag-off-on":
				_ = s.flags.Disable(agentfeature.Memory)
				config, _ := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
				_ = s.flags.Replace(s.flags.Revision(), config)
			case "ctx-cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "metadata-version":
				_, e := b.pool.Exec(ctx, `UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
				if e != nil {
					t.Fatal(e)
				}
			case "expired-preview":
				if b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.review.ExpiresAt) != nil {
					t.Fatal("clock")
				}
			case "source-edit":
				if _, e := b.pool.Exec(ctx, `UPDATE moments SET revision=revision+1,updated_at=clock_timestamp(),body='合成修改' WHERE id=$1`, f.sources[agentevent.MomentCreated]); e != nil {
					t.Fatal(e)
				}
			case "source-withdraw":
				if e := b.store.WithdrawMoment(ctx, b.person.ID, f.sources[agentevent.MomentCreated], r.Sources[0].Version.Revision); e != nil {
					t.Fatal(e)
				}
			case "source-rebuild":
				tx, e := b.pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(ctx)
				for _, q := range []string{`CREATE TEMP TABLE candidate_moment_copy AS SELECT * FROM moments WHERE id=$1`, `DELETE FROM moments WHERE id=$1`, `INSERT INTO moments SELECT * FROM candidate_moment_copy`} {
					if q == `INSERT INTO moments SELECT * FROM candidate_moment_copy` {
						_, e = tx.Exec(ctx, q)
					} else {
						_, e = tx.Exec(ctx, q, f.sources[agentevent.MomentCreated])
					}
					if e != nil {
						t.Fatal(e)
					}
				}
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
			case "link-change":
				if _, e := b.pool.Exec(ctx, `INSERT INTO moment_activity_links(moment_id,activity_id,city_id,author_confirmed_at) VALUES($1,$2,'aberdeen-gb',clock_timestamp())`, f.sources[agentevent.MomentCreated], f.activities[0]); e != nil {
					t.Fatal(e)
				}
			case "session-revoked":
				_, e := b.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.base.private.ownerSession)
				if e != nil {
					t.Fatal(e)
				}
			case "session-expired":
				_, e := b.pool.Exec(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp()-interval '1 millisecond' instant) UPDATE sessions SET expires_at=n.instant,idle_expires_at=n.instant FROM n WHERE id=$1`, f.base.private.ownerSession)
				if e != nil {
					t.Fatal(e)
				}
			case "idle-expired":
				_, e := b.pool.Exec(ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()-interval '1 millisecond' WHERE id=$1`, f.base.private.ownerSession)
				if e != nil {
					t.Fatal(e)
				}
			case "account-disabled":
				_, e := b.pool.Exec(ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				if e != nil {
					t.Fatal(e)
				}
			case "agent-disabled":
				_, e := b.pool.Exec(ctx, `UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
				if e != nil {
					t.Fatal(e)
				}
			case "metadata-deleted":
				_, e := b.pool.Exec(ctx, `DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
				if e != nil {
					t.Fatal(e)
				}
			case "target-other-owner":
				p.review.TargetMemoryID = agentMemoryID(t, f.base.private)
				p.review.Candidate.Category = "forged"
			case "target-stale":
				p.review.ExpectedMemoryVersion = 2
			case "target-inferred":
				p.review.Candidate.Assessment = new(agentconfidence.Assessment)
			}
			if _, e := s.AcceptOwnCandidate(ctx, access, p); e == nil {
				t.Fatal("boundary accepted")
			}
			var n int
			if b.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, b.personID).Scan(&n) != nil || n != 0 {
				t.Fatal("partial Memory effect")
			}
		})
	}
}
func TestMemoryCandidateNativeConcurrentAcceptance(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	r := memoryCandidateSave(t, f, s)
	p := memoryCandidatePreview(t, f, s, r)
	start := make(chan struct{})
	var wg sync.WaitGroup
	errorsC := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := s.AcceptOwnCandidate(context.Background(), f.base.private.owner, p)
			errorsC <- e
		}()
	}
	close(start)
	wg.Wait()
	close(errorsC)
	for e := range errorsC {
		if e != nil {
			t.Fatal(e)
		}
	}
	var n int
	if s.store.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, r.AgentID).Scan(&n) != nil || n != 1 {
		t.Fatal("concurrent duplicate")
	}
}
func TestMemoryCandidateNativeModelUnavailable(t *testing.T) {
	if _, e := (*MemoryCandidateService)(nil).SubmitInferredCandidate(context.Background()); !errors.Is(e, agentcognitive.ErrUnavailable) {
		t.Fatal(e)
	}
}
func TestMemoryCandidateNativeDraftAndTargetBoundaries(t *testing.T) {
	for _, name := range []string{"sensitive", "unknown-category", "sensitive-predicate", "declaration-score", "calibrated", "empty", "duplicate", "other-owner-source", "foreign-candidate", "stale-candidate-cas", "target-other-owner", "target-stale", "target-inferred", "nil-flags"} {
		t.Run(name, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			d := memoryCandidateDraft(t, f)
			access := f.base.private.owner
			id := agentMemoryID(t, f.base.private)
			switch name {
			case "sensitive":
				d.Category = "health"
			case "unknown-category":
				d.Category = "unknown_category"
			case "sensitive-predicate":
				d.Predicate = "religion"
			case "declaration-score":
				d.Assessment = agentconfidence.NewDirectDeclaration()
			case "calibrated":
				v := 0.82
				d.Assessment = agentconfidence.Assessment{Semantics: agentconfidence.CalibratedProbability, Value: &v}
			case "empty":
				d.Sources = nil
			case "duplicate":
				d.Sources = append(d.Sources, d.Sources[0])
			case "other-owner-source":
				access = f.base.private.peer
			case "nil-flags":
				s = NewMemoryCandidateService(b.store, nil)
			case "foreign-candidate", "stale-candidate-cas", "target-other-owner", "target-stale", "target-inferred":
				r := memoryCandidateSave(t, f, s)
				switch name {
				case "foreign-candidate":
					if _, e := s.ReadOwnCandidate(b.ctx, f.base.private.peer, r.ID); e == nil {
						t.Fatal("cross owner read")
					}
					return
				case "stale-candidate-cas":
					if _, e := s.RejectOwnCandidate(b.ctx, access, r.ID, 2); e == nil {
						t.Fatal("stale CAS")
					}
					return
				case "target-other-owner":
					in := agentMemoryInput("other-owner")
					if _, e := b.store.PutOwnMemory(b.ctx, f.base.private.peer, id, in); e != nil {
						t.Fatal(e)
					}
				case "target-stale":
					mustPutAgentMemory(t, f.base.private, id, agentMemoryInput("stale-target"))
				case "target-inferred":
					if _, e := b.pool.Exec(b.ctx, `INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,confidence,source_type,status,visibility,valid_until) VALUES($1,$2,$3,'PREFERENCE','candidate.reserved','合成形状非授权','{}',0.82,'INFERRED','PENDING_REVIEW','AGENT_ONLY',clock_timestamp()+interval '1 day')`, id, b.personID, b.person.ID); e != nil {
						t.Fatal(e)
					}
				}
				expected := int64(0)
				if name == "target-inferred" {
					expected = 1
				}
				if _, e := s.PreviewOwnAcceptance(b.ctx, access, r.ID, 1, id, expected, d.ValidUntil); e == nil {
					t.Fatal("target authority/CAS bypass")
				}
				return
			}
			if _, e := s.SaveOwnCandidate(b.ctx, access, id, d, "", 0); e == nil {
				t.Fatal("draft boundary accepted")
			}
		})
	}
}
func TestMemoryCandidateNativeHumanHikingDraftRequiresSpecificExplicitReview(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	d := memoryCandidateDraft(t, f)
	d.Category = "hiking"
	d.Assessment, _ = agentconfidence.NewOrdinal(agentconfidence.Low)
	assertNoMemory := func() {
		t.Helper()
		var memories, evidence int
		if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM agent_memories WHERE agent_id=$1),(SELECT count(*) FROM agent_memory_evidence WHERE agent_id=$1)`, b.personID).Scan(&memories, &evidence); e != nil || memories != 0 || evidence != 0 {
			t.Fatal("manual candidate implicitly wrote Memory/evidence", memories, evidence, e)
		}
	}
	assertNoMemory()
	single := d
	single.Sources = single.Sources[:1]
	one, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), single, "", 0)
	if e != nil || one.Status != agentmemorycandidate.Candidate || one.Category != "hiking" || one.MemoryID != nil {
		t.Fatal("single human hiking draft was not pending", one, e)
	}
	var memoryUntil time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '1 day'`).Scan(&memoryUntil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PreviewOwnAcceptance(b.ctx, f.base.private.owner, one.ID, one.Version, agentMemoryID(t, f.base.private), 0, memoryUntil); !errors.Is(e, agentmemory.ErrForbidden) {
		t.Fatal("one independent source bypassed human acceptance cluster gate", e)
	}
	assertNoMemory()
	r, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), d, "", 0)
	if e != nil || r.Status != agentmemorycandidate.Candidate || r.Category != "hiking" || r.MemoryID != nil || r.Assessment == nil || r.Assessment.Semantics != agentconfidence.Ordinal || r.Assessment.Level != agentconfidence.Low || r.Assessment.Value != nil {
		t.Fatal("human hiking draft did not remain ordinal/pending", r, e)
	}
	assertNoMemory()
	p := memoryCandidatePreview(t, f, s, r)
	review := p.Review()
	if review.Clusters != 2 || review.Purpose != "HUMAN_EXPLICIT_DECLARATION" || review.Statement != "我偏好徒步活动" || review.Candidate.ID != r.ID {
		t.Fatal("specific human hiking review mismatch", review)
	}
	assertNoMemory()
	accepted, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p)
	if e != nil || accepted.Status != agentmemorycandidate.Active || accepted.MemoryID == nil || accepted.Version != r.Version+1 {
		t.Fatal("specific human hiking acceptance failed", accepted, e)
	}
	var nature, statement, key string
	var confidence float64
	var memories, evidence int
	if e = b.pool.QueryRow(b.ctx, `SELECT source_type,summary,memory_key,confidence,(SELECT count(*) FROM agent_memories WHERE agent_id=$2),(SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1) FROM agent_memories WHERE id=$1 AND agent_id=$2`, *accepted.MemoryID, b.personID).Scan(&nature, &statement, &key, &confidence, &memories, &evidence); e != nil || nature != "EXPLICIT" || confidence != 1 || statement != "我偏好徒步活动" || key != "activity_category:hiking" || memories != 1 || evidence != 2 {
		t.Fatal("manual acceptance changed inferred/ordinal semantics or duplicated Memory", nature, statement, key, confidence, memories, evidence, e)
	}
	stillPending, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, one.ID)
	if e != nil || stillPending.Status != agentmemorycandidate.Candidate || stillPending.MemoryID != nil {
		t.Fatal("manual acceptance promoted another candidate", stillPending, e)
	}
}
func TestMemoryCandidateNativeSourceReclamation(t *testing.T) {
	for _, name := range []string{"rsvp-cancel", "activity-hidden", "city-hidden", "block", "saved-remove", "place-hidden", "source-future", "source-infinity", "metadata-revision"} {
		t.Run(name, func(t *testing.T) {
			if name == "city-hidden" {
				// The hide/restore commits must stay in an owned seed database.
				ownedMigrationDatabase(t)
			}
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			d := memoryCandidateDraft(t, f)
			if name == "rsvp-cancel" || name == "activity-hidden" || name == "block" {
				d.Sources[0] = agentmemorycandidate.Selector{Type: agentevent.ParticipationSource, ID: f.sources[agentevent.ActivityJoined]}
			}
			r, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), d, "", 0)
			if e != nil {
				t.Fatal(e)
			}
			var q string
			var args []any
			switch name {
			case "rsvp-cancel":
				q = `UPDATE activity_participations SET status='cancelled',cancelled_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`
				args = []any{d.Sources[0].ID}
			case "activity-hidden":
				q = `UPDATE activities SET publication_status='hidden' WHERE id=(SELECT activity_id FROM activity_participations WHERE id=$1)`
				args = []any{d.Sources[0].ID}
			case "city-hidden":
				q = `UPDATE cities SET publication_status='hidden' WHERE id='aberdeen-gb'`
				defer b.pool.Exec(context.Background(), `UPDATE cities SET publication_status='published' WHERE id='aberdeen-gb'`)
			case "block":
				q = `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`
				args = []any{b.person.ID, b.other.ID}
			case "saved-remove":
				q = `DELETE FROM saved_items WHERE id=$1`
				args = []any{d.Sources[1].ID}
			case "place-hidden":
				q = `UPDATE places SET publication_status='hidden' WHERE id=$1`
				args = []any{f.place}
			case "source-future":
				q = `UPDATE moments SET revision=revision+1,updated_at=clock_timestamp()+interval '1 hour' WHERE id=$1`
				args = []any{d.Sources[0].ID}
			case "source-infinity":
				q = `UPDATE moments SET revision=revision+1,updated_at='infinity' WHERE id=$1`
				args = []any{d.Sources[0].ID}
			case "metadata-revision":
				q = `UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`
				args = []any{b.personID}
			}
			if _, e = b.pool.Exec(b.ctx, q, args...); e != nil {
				t.Fatal(e)
			}
			got, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
			if name == "source-infinity" {
				if e == nil {
					t.Fatal("infinite source released payload")
				}
				return
			}
			if e != nil || got.Status != agentmemorycandidate.Expired || got.Category != "" || len(got.Sources) != 0 {
				t.Fatal("stale source retained", got, e)
			}
		})
	}
}
func TestMemoryCandidateNativeRealExpiryScrubs(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	d := memoryCandidateDraft(t, f)
	if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '500 milliseconds'`).Scan(&d.ValidUntil) != nil {
		t.Fatal("clock")
	}
	r, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), d, "", 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM(valid_until-clock_timestamp())))+0.02) FROM agent_memory_candidates WHERE id=$1`, r.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
	if e != nil || v.Status != agentmemorycandidate.Expired || v.Category != "" {
		t.Fatal(v, e)
	}
}
func TestMemoryCandidateNativeRollbackAndLateBoundary(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, name := range []string{"sql-failure", "late-ACL", "late-cancel", "late-switch"} {
		t.Run(name, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			d := memoryCandidateDraft(t, f)
			d.Sources[0] = agentmemorycandidate.Selector{Type: agentevent.ParticipationSource, ID: f.sources[agentevent.ActivityJoined]}
			r, e := s.SaveOwnCandidate(b.ctx, f.base.private.owner, agentMemoryID(t, f.base.private), d, "", 0)
			if e != nil {
				t.Fatal(e)
			}
			p := memoryCandidatePreview(t, f, s, r)
			if name == "sql-failure" {
				if _, e = b.pool.Exec(b.ctx, `CREATE FUNCTION candidate_test_failure() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.status='ACTIVE' THEN RAISE EXCEPTION 'synthetic private candidate failure'; END IF;RETURN NEW;END$$;CREATE TRIGGER zz_candidate_test_failure BEFORE UPDATE ON agent_memory_candidates FOR EACH ROW EXECUTE FUNCTION candidate_test_failure()`); e != nil {
					t.Fatal(e)
				}
				defer b.pool.Exec(context.Background(), `DROP TRIGGER zz_candidate_test_failure ON agent_memory_candidates;DROP FUNCTION candidate_test_failure()`)
				if _, e = s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); !errors.Is(e, agentmemory.ErrUnavailable) {
					t.Fatal("SQL failure redaction", e)
				}
			} else {
				conn, e := b.pool.Acquire(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer conn.Release()
				if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_lock(910007)`); e != nil {
					t.Fatal(e)
				}
				defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(910007)`)
				if _, e = b.pool.Exec(b.ctx, `CREATE FUNCTION candidate_test_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.status='ACTIVE' THEN PERFORM pg_advisory_xact_lock(910007); END IF;RETURN NEW;END$$;CREATE TRIGGER zz_candidate_test_pause BEFORE UPDATE ON agent_memory_candidates FOR EACH ROW EXECUTE FUNCTION candidate_test_pause()`); e != nil {
					t.Fatal(e)
				}
				defer b.pool.Exec(context.Background(), `DROP TRIGGER zz_candidate_test_pause ON agent_memory_candidates;DROP FUNCTION candidate_test_pause()`)
				ctx, cancel := context.WithCancel(b.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					v, e := s.AcceptOwnCandidate(ctx, f.base.private.owner, p)
					if v.ID != "" {
						done <- errors.New("late response leaked payload")
						return
					}
					done <- e
				}()
				waiting := false
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					if b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=910007 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting) != nil {
						t.Fatal("barrier")
					}
					if waiting {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !waiting {
					t.Fatal("native UPDATE not waiting")
				}
				switch name {
				case "late-ACL":
					if _, e = b.pool.Exec(b.ctx, `UPDATE activities SET publication_status='hidden' WHERE id=(SELECT activity_id FROM activity_participations WHERE id=$1)`, d.Sources[0].ID); e != nil {
						t.Fatal(e)
					}
				case "late-cancel":
					cancel()
				case "late-switch":
					_ = s.flags.Disable(agentfeature.Memory)
				}
				if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_unlock(910007)`); e != nil {
					t.Fatal(e)
				}
				select {
				case e = <-done:
					if e == nil {
						t.Fatal("late boundary allowed")
					}
				case <-time.After(8 * time.Second):
					t.Fatal("native wait timeout")
				}
			}
			var memories, evidences int
			if b.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM agent_memories WHERE agent_id=$1),(SELECT count(*) FROM agent_memory_evidence WHERE agent_id=$1)`, r.AgentID).Scan(&memories, &evidences) != nil || memories != 0 || evidences != 0 {
				t.Fatal("transaction partially committed", memories, evidences)
			}
			var status string
			if b.pool.QueryRow(context.Background(), `SELECT status FROM agent_memory_candidates WHERE id=$1`, r.ID).Scan(&status) != nil || status != "CANDIDATE" {
				t.Fatal("rollback lost candidate")
			}
		})
	}
}
func TestMemoryCandidateNativeCurrentDataMigrationRoundtrip(t *testing.T) {
	// The whole-suite runtime is also used by other packages. Exercise the
	// destructive 063 roundtrip in the established independently owned DB.
	ownedMigrationDatabase(t)
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	r := memoryCandidateSave(t, f, s)
	p := memoryCandidatePreview(t, f, s, r)
	if _, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); e != nil {
		t.Fatal(e)
	}
	snapshot := func() string {
		var raw string
		if b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('memory',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agent_memories m WHERE agent_id=$1),'evidence',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agent_memory_evidence e WHERE agent_id=$1),'support',(SELECT jsonb_agg(to_jsonb(r) ORDER BY memory_id) FROM agent_memory_reinforcement r WHERE memory_id IN(SELECT id FROM agent_memories WHERE agent_id=$1)))::text`, r.AgentID).Scan(&raw) != nil {
			t.Fatal("snapshot")
		}
		return raw
	}
	before := snapshot()
	// 082 has a real FK to this table. Its unused dependent schema must be
	// downgraded first; never CASCADE, erase effect history or skip 063 down.
	var dependent082 bool
	if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.agent_effect_ledger') AND attname='candidate_id' AND NOT attisdropped)`).Scan(&dependent082); e != nil {
		t.Fatal(e)
	}
	publicRows := func() map[string]json.RawMessage {
		var rows map[string]json.RawMessage
		if e := json.Unmarshal([]byte(enrichmentAllPublic(t, b.pool, b.ctx)), &rows); e != nil {
			t.Fatal(e)
		}
		// Only the proposal rows are intentionally removed by the original 063
		// contract. Every other public table must retain all original columns.
		delete(rows, "agent_memory_candidates")
		return rows
	}
	participationXmin := func() string {
		var raw string
		if e := b.pool.QueryRow(b.ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'xmin',xmin::text) ORDER BY id),'[]')::text FROM activity_participations`).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	catalog := func() string {
		var raw string
		if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
 'functions',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',p.proname,'args',pg_get_function_identity_arguments(p.oid),'definition',pg_get_functiondef(p.oid)) ORDER BY p.proname,pg_get_function_identity_arguments(p.oid)),'[]') FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind='f'),
 'triggers',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',t.tgname,'definition',pg_get_triggerdef(t.oid),'enabled',t.tgenabled) ORDER BY c.relname,t.tgname),'[]') FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND NOT t.tgisinternal),
 'constraints',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',r.relname,'name',c.conname,'definition',pg_get_constraintdef(c.oid)) ORDER BY r.relname,c.conname),'[]') FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid JOIN pg_namespace n ON n.oid=r.relnamespace WHERE n.nspname='public'),
 'columns',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'name',a.attname,'ordinal',CASE WHEN c.relname='agent_effect_ledger' AND a.attname IN('candidate_id','retention_grant_id','event_id','handler_version','fence','attempt','proof_review') THEN NULL ELSE a.attnum END,'type',format_type(a.atttypid,a.atttypmod),'notNull',a.attnotnull,'default',pg_get_expr(d.adbin,d.adrelid),'identity',a.attidentity,'generated',a.attgenerated) ORDER BY c.relname,a.attname),'[]') FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname='public' AND c.relkind IN('r','p','v','m') AND a.attnum>0 AND NOT a.attisdropped),
 'indexes',(SELECT coalesce(jsonb_agg(jsonb_build_object('table',t.relname,'name',i.relname,'definition',pg_get_indexdef(x.indexrelid),'valid',x.indisvalid,'ready',x.indisready) ORDER BY t.relname,i.relname),'[]') FROM pg_index x JOIN pg_class t ON t.oid=x.indrelid JOIN pg_class i ON i.oid=x.indexrelid JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname='public'),
 'tables',(SELECT coalesce(jsonb_agg(jsonb_build_object('name',c.relname,'kind',c.relkind,'owner',pg_get_userbyid(c.relowner),'rls',c.relrowsecurity,'forceRLS',c.relforcerowsecurity,'privileges',c.relacl) ORDER BY c.relname),'[]') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p','v','m','S')),
 'policies',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY tablename,policyname),'[]') FROM pg_policies p WHERE schemaname='public'))::text`).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	// DROP/re-add assigns new physical attnums to the seven new 082 columns.
	// Their full definitions remain compared; all older column ordinals are
	// also exact. OIDs and expected dropped-column holes are not permissions.
	allOldXmin := func() string {
		conn, e := b.pool.Acquire(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer conn.Release()
		if _, e = conn.Exec(b.ctx, `CREATE OR REPLACE FUNCTION pg_temp.candidate_roundtrip_xmin() RETURNS jsonb LANGUAGE plpgsql AS $$ DECLARE item record; rows jsonb; result jsonb:='{}'; BEGIN FOR item IN SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename<>'agent_memory_candidates' ORDER BY tablename LOOP EXECUTE format('SELECT coalesce(jsonb_agg(jsonb_build_object(''row'',to_jsonb(t),''xmin'',t.xmin::text) ORDER BY to_jsonb(t)::text),''[]''::jsonb) FROM public.%I t',item.tablename) INTO rows; result:=result||jsonb_build_object(item.tablename,rows); END LOOP; RETURN result; END $$`); e != nil {
			t.Fatal("full old xmin helper", e)
		}
		var raw string
		if e = conn.QueryRow(b.ctx, `SELECT pg_temp.candidate_roundtrip_xmin()::text`).Scan(&raw); e != nil {
			t.Fatal("full old xmin", e)
		}
		return raw
	}
	// Preserve only the installed trigger that DROP 063 naturally removes.
	// No 094 migration is replayed over existing correction history.
	var correctionDefinition, correctionEnabled, correctionFunction string
	if e := b.pool.QueryRow(b.ctx, `SELECT coalesce((SELECT pg_get_triggerdef(t.oid) FROM pg_trigger t WHERE t.tgrelid='agent_memory_candidates'::regclass AND t.tgname='memory_candidate_correction_guard'),''),coalesce((SELECT t.tgenabled::text FROM pg_trigger t WHERE t.tgrelid='agent_memory_candidates'::regclass AND t.tgname='memory_candidate_correction_guard'),''),coalesce((SELECT pg_get_functiondef(t.tgfoid) FROM pg_trigger t WHERE t.tgrelid='agent_memory_candidates'::regclass AND t.tgname='memory_candidate_correction_guard'),'')`).Scan(&correctionDefinition, &correctionEnabled, &correctionFunction); e != nil {
		t.Fatal(e)
	}
	// Older 082/091 replace these objects. Preserve the already installed
	// 101/102 definitions without replaying migrations over existing history.
	var currentOutboxGuards []string
	for _, name := range []string{"birdtie_guard_agent_outbox", "birdtie_guard_agent_consumer_inbox"} {
		var definition string
		if e := b.pool.QueryRow(b.ctx, `SELECT pg_get_functiondef(to_regprocedure($1))`, "public."+name+"()").Scan(&definition); e != nil {
			t.Fatal("current dependent guard", e)
		}
		currentOutboxGuards = append(currentOutboxGuards, definition)
	}
	type installedConstraint struct{ table, name, definition string }
	var currentOutboxConstraints []installedConstraint
	for _, item := range []installedConstraint{
		{table: "agent_domain_outbox", name: "agent_domain_outbox_delivery_state_check"},
		{table: "agent_consumer_inbox", name: "agent_consumer_inbox_handler_version_check"},
		{table: "agent_consumer_inbox", name: "agent_consumer_inbox_control_state_check"},
		{table: "agent_consumer_inbox", name: "agent_consumer_inbox_check1"},
	} {
		if e := b.pool.QueryRow(b.ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2`, "public."+item.table, item.name).Scan(&item.definition); e != nil {
			t.Fatal("current dependent constraint", e)
		}
		currentOutboxConstraints = append(currentOutboxConstraints, item)
	}
	beforePublic, beforeCatalog, beforeXmin := publicRows(), catalog(), participationXmin()
	beforeAllXmin := allOldXmin()
	// 095 replaces the maintenance function installed by 091. Restore its
	// predecessor before the original dependent roundtrip, then reapply it
	// after all older schemas/guards are restored. Every original comparison
	// below still covers the full current catalog and all unrelated rows/xmin.
	var dependent095 bool
	if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.proname='birdtie_expire_candidate_pipeline' AND p.prosrc LIKE '%batch_size IS NULL%' AND p.prosrc LIKE '%mom-candidate-local-v2%')`).Scan(&dependent095); e != nil {
		t.Fatal(e)
	}
	var up095 []byte
	if dependent095 {
		down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "095_agent_candidate_invalidation_cleanup.down.sql"))
		if e != nil {
			t.Fatal(e)
		}
		up095, e = os.ReadFile(filepath.Join("..", "..", "migrations", "095_agent_candidate_invalidation_cleanup.sql"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
			t.Fatal("dependent095 down", e)
		}
	}
	dependent092 := candidateVocabularyFixture(t, b.pool, b.ctx, false)
	// 091 replaces installed 082 functions and adds a trigger on 063. Its
	// unused native schema must be removed first and restored last, otherwise
	// down082/up082 silently replaces the multi current/revoke/effect closures.
	var dependent091 bool
	if e := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_multi_candidate_bindings') IS NOT NULL`).Scan(&dependent091); e != nil {
		t.Fatal(e)
	}
	var up091 []byte
	if dependent091 {
		down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "091_agent_multi_candidate_pipeline.down.sql"))
		if e != nil {
			t.Fatal(e)
		}
		up091, e = os.ReadFile(filepath.Join("..", "..", "migrations", "091_agent_multi_candidate_pipeline.sql"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
			t.Fatal("unused dependent091 down", e)
		}
	}
	var up082 []byte
	if dependent082 {
		down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "082_agent_candidate_atomic_consumer.down.sql"))
		if e != nil {
			t.Fatal(e)
		}
		up082, e = os.ReadFile(filepath.Join("..", "..", "migrations", "082_agent_candidate_atomic_consumer.sql"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
			t.Fatal("unused dependent082 down", e)
		}
	}
	for _, name := range []string{"063_agent_memory_candidates.down.sql", "063_agent_memory_candidates.sql"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.pool.Exec(b.ctx, string(raw)); e != nil {
			t.Fatal("schema roundtrip", e)
		}
	}
	if dependent082 {
		if _, e := b.pool.Exec(b.ctx, string(up082)); e != nil {
			t.Fatal("dependent082 reapply", e)
		}
	}
	if dependent091 {
		if _, e := b.pool.Exec(b.ctx, string(up091)); e != nil {
			t.Fatal("dependent091 reapply", e)
		}
	}
	if dependent092 {
		candidateVocabularyFixture(t, b.pool, b.ctx, true)
	}
	// 063 DROP naturally removes its dependent 094 trigger. Restore the
	// already installed function's exact trigger, without dropping correction
	// history or relaxing any original catalog/row/xmin assertion.
	if correctionDefinition != "" {
		var actualFunction string
		if e := b.pool.QueryRow(b.ctx, `SELECT pg_get_functiondef('public.birdtie_memory_candidate_correction_guard()'::regprocedure)`).Scan(&actualFunction); e != nil || actualFunction != correctionFunction {
			t.Fatal("original094 trigger function changed", e)
		}
		if _, e := b.pool.Exec(b.ctx, correctionDefinition); e != nil {
			t.Fatal("restore exact094 dependent trigger", e)
		}
		mode := map[string]string{"O": "ENABLE", "D": "DISABLE", "R": "ENABLE REPLICA", "A": "ENABLE ALWAYS"}[correctionEnabled]
		if mode == "" {
			t.Fatal("unknown original094 trigger mode")
		}
		if _, e := b.pool.Exec(b.ctx, `ALTER TABLE agent_memory_candidates `+mode+` TRIGGER memory_candidate_correction_guard`); e != nil {
			t.Fatal("restore original094 trigger mode", e)
		}
		var actualDefinition, actualEnabled string
		if e := b.pool.QueryRow(b.ctx, `SELECT pg_get_triggerdef(t.oid),t.tgenabled::text FROM pg_trigger t WHERE t.tgrelid='agent_memory_candidates'::regclass AND t.tgname='memory_candidate_correction_guard'`).Scan(&actualDefinition, &actualEnabled); e != nil || actualDefinition != correctionDefinition || actualEnabled != correctionEnabled {
			t.Fatal("original094 trigger was not restored exactly", e)
		}
	}
	if dependent095 {
		if _, e := b.pool.Exec(b.ctx, string(up095)); e != nil {
			t.Fatal("dependent095 reapply", e)
		}
	}
	for _, definition := range currentOutboxGuards {
		if _, e := b.pool.Exec(b.ctx, definition); e != nil { t.Fatal("restore exact current dependent guard", e) }
	}
	for _, item := range currentOutboxConstraints {
		if _, e := b.pool.Exec(b.ctx, `ALTER TABLE public.`+item.table+` DROP CONSTRAINT `+item.name+`, ADD CONSTRAINT `+item.name+` `+item.definition); e != nil { t.Fatal("restore exact current dependent constraint", e) }
	}
	if !reflect.DeepEqual(publicRows(), beforePublic) || catalog() != beforeCatalog || participationXmin() != beforeXmin || allOldXmin() != beforeAllXmin {
		t.Fatal("dependent091/082/063 roundtrip changed unrelated public rows, catalog or old xmin")
	}
	if snapshot() != before {
		t.Fatal("old Memory/Evidence/060 changed")
	}
	if _, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID); !errors.Is(e, agentmemory.ErrNotFound) {
		t.Fatal("down proposal retained", e)
	}
}
func TestMemoryCandidateNativeAcceptedEvidenceDetach(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	r := memoryCandidateSave(t, f, s)
	p := memoryCandidatePreview(t, f, s, r)
	v, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := b.pool.Query(b.ctx, `SELECT id FROM agent_memory_evidence WHERE memory_id=$1 AND status='CURRENT' ORDER BY id`, v.MemoryID)
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			t.Fatal("id")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil || len(ids) != 2 {
		t.Fatal("native refs")
	}
	reinforcement := NewMemoryReinforcementService(b.store, reinforcementFlags(t))
	preview, e := reinforcement.PreviewOwnReinforcement(b.ctx, f.base.private.owner, *v.MemoryID, 1, 0, ids)
	if e != nil {
		t.Fatal(e)
	}
	support, e := reinforcement.ApproveOwnReinforcement(b.ctx, f.base.private.owner, preview)
	if e != nil || support.SupportClusters != 2 {
		t.Fatal(e)
	}
	if _, e = b.store.RemoveOwnMemoryEvidence(b.ctx, f.base.private.owner, *v.MemoryID, ids[0], 1); e != nil {
		t.Fatal(e)
	}
	got, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
	if e != nil || got.Status != agentmemorycandidate.Superseded || len(got.Sources) != 0 || got.MemoryID != nil {
		t.Fatal("detach retained candidate", got, e)
	}
	cleared, e := reinforcement.ReadOwnReinforcement(b.ctx, f.base.private.owner, *v.MemoryID, 1)
	if e != nil || cleared.EvidenceCount != 0 || cleared.LastSupportAt != nil {
		t.Fatal("060 retained stale support", e)
	}
	if _, e = s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); e == nil {
		t.Fatal("detach reused approval")
	}
}
func TestMemoryCandidateNativeConnectionTimeZone(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	makePool := func(zone string) *pgxpool.Pool {
		config, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		config.MaxConns = 1
		config.ConnConfig.RuntimeParams["timezone"] = zone
		p, e := pgxpool.NewWithConfig(b.ctx, config)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	tokyo := makePool("Asia/Tokyo")
	honolulu := makePool("Pacific/Honolulu")
	s.store = New(tokyo, true)
	r := memoryCandidateSave(t, f, s)
	s.store = New(honolulu, true)
	got, e := s.ReadOwnCandidate(b.ctx, f.base.private.owner, r.ID)
	if e != nil || !reflect.DeepEqual(r, got) {
		t.Fatal("timezone changed native snapshots", e)
	}
	p := memoryCandidatePreview(t, f, s, r)
	s.store = New(tokyo, true)
	accepted, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p)
	if e != nil || accepted.Status != agentmemorycandidate.Active {
		t.Fatal("cross connection revalidate", e)
	}
	for _, x := range []struct {
		pool *pgxpool.Pool
		zone string
	}{{tokyo, "Asia/Tokyo"}, {honolulu, "Pacific/Honolulu"}} {
		var setting string
		if x.pool.QueryRow(b.ctx, `SELECT current_setting('TimeZone')`).Scan(&setting) != nil || setting != x.zone {
			t.Fatal("global timezone changed", setting)
		}
	}
}
func TestMemoryCandidateNativeLateSessionExpiry(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	r := memoryCandidateSave(t, f, s)
	p := memoryCandidatePreview(t, f, s, r)
	if _, e := b.pool.Exec(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1`, f.base.private.ownerSession); e != nil {
		t.Fatal(e)
	}
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_lock(910017)`); e != nil {
		t.Fatal(e)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(910017)`)
	if _, e = b.pool.Exec(b.ctx, `CREATE FUNCTION candidate_test_session_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.status='ACTIVE' THEN PERFORM pg_advisory_xact_lock(910017);END IF;RETURN NEW;END$$;CREATE TRIGGER zz_candidate_test_session_pause BEFORE UPDATE ON agent_memory_candidates FOR EACH ROW EXECUTE FUNCTION candidate_test_session_pause()`); e != nil {
		t.Fatal(e)
	}
	defer b.pool.Exec(context.Background(), `DROP TRIGGER zz_candidate_test_session_pause ON agent_memory_candidates;DROP FUNCTION candidate_test_session_pause()`)
	done := make(chan error, 1)
	go func() { _, e := s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); done <- e }()
	waiting := false
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=910017 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting) != nil {
			t.Fatal("wait")
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("actual UPDATE not waiting")
	}
	if _, e = conn.Exec(b.ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM(idle_expires_at-clock_timestamp())))+0.02) FROM sessions WHERE id=$1`, f.base.private.ownerSession); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_unlock(910017)`); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("expired session committed")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timeout")
	}
	var n int
	if b.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, r.AgentID).Scan(&n) != nil || n != 0 {
		t.Fatal("session late partial effect")
	}
}
func TestMemoryCandidateNativeSameContentTargetChangeInvalidatesApproval(t *testing.T) {
	for _, name := range []string{"create-after-preview", "update-after-preview"} {
		t.Run(name, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			r := memoryCandidateSave(t, f, s)
			var until time.Time
			if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '1 day'`).Scan(&until) != nil {
				t.Fatal("clock")
			}
			mid := agentMemoryID(t, f.base.private)
			expected := int64(0)
			if name == "update-after-preview" {
				initial := candidateMemoryInput(r, 0, until)
				initial.Summary = "本人先前声明"
				if _, e := b.store.PutOwnMemory(b.ctx, f.base.private.owner, mid, initial); e != nil {
					t.Fatal(e)
				}
				expected = 1
			}
			p, e := s.PreviewOwnAcceptance(b.ctx, f.base.private.owner, r.ID, 1, mid, expected, until)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = b.store.PutOwnMemory(b.ctx, f.base.private.owner, mid, candidateMemoryInput(r, expected, until)); e != nil {
				t.Fatal(e)
			}
			var before string
			if b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, mid).Scan(&before) != nil {
				t.Fatal("target")
			}
			if _, e = s.AcceptOwnCandidate(b.ctx, f.base.private.owner, p); !errors.Is(e, agentmemory.ErrConflict) {
				t.Fatal("other effect treated as candidate retry", e)
			}
			var after, status string
			var count int
			if b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, mid).Scan(&after) != nil || before != after {
				t.Fatal("target mutated")
			}
			if b.pool.QueryRow(b.ctx, `SELECT status FROM agent_memory_candidates WHERE id=$1`, r.ID).Scan(&status) != nil || status != "CANDIDATE" {
				t.Fatal("pending changed")
			}
			if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1`, mid).Scan(&count) != nil || count != 0 {
				t.Fatal("stale approval attached evidence")
			}
		})
	}
}
