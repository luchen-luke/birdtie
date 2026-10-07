package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"

	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5/pgxpool"
)

func reinforcementFlags(t *testing.T) *agentfeature.Controller {
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
func reinforcementFixture(t *testing.T) (*memoryEvidenceFixture, *MemoryReinforcementService) {
	t.Helper()
	f := memoryEvidenceNativeFixture(t)
	s := NewMemoryReinforcementService(f.native.base.private.base.store, reinforcementFlags(t))
	var installed bool
	if e := s.store.pool.QueryRow(context.Background(), `SELECT to_regclass('agent_memory_reinforcement') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("requires actual migration060")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := s.store.pool.Exec(ctx, `DELETE FROM moment_activity_links WHERE moment_id IN (SELECT id FROM moments WHERE author_account_id=ANY($1::uuid[]))`, f.native.base.private.base.accounts); e != nil {
			t.Error("owned link cleanup", e)
		}
	})
	return f, s
}
func reinforcementPreview(t *testing.T, f *memoryEvidenceFixture, s *MemoryReinforcementService, version int64, ids ...string) *MemoryReinforcementPreview {
	t.Helper()
	p, e := s.PreviewOwnReinforcement(context.Background(), f.native.base.private.owner, f.memory.ID, f.memory.Version, version, ids)
	if e != nil {
		t.Fatal("native preview", e)
	}
	return p
}
func reinforcementApprove(t *testing.T, f *memoryEvidenceFixture, s *MemoryReinforcementService, p *MemoryReinforcementPreview) agentreinforcement.View {
	t.Helper()
	v, e := s.ApproveOwnReinforcement(context.Background(), f.native.base.private.owner, p)
	if e != nil || v.MemoryID != f.memory.ID || v.MemoryVersion != f.memory.Version || (v.Assessment.Value == nil || *v.Assessment.Value != 1) || v.ModelAccess != "UNAVAILABLE" {
		t.Fatalf("native approve %+v %v", v, e)
	}
	return v
}
func TestMemoryReinforcementNativePersistenceDedupAndIndependentCAS(t *testing.T) {
	f, s := reinforcementFixture(t)
	b := f.native.base.private.base
	before := agentMemoryOwnedSourceSnapshot(t, f.native.base.private)
	moment := f.attach(t, agentevent.MomentSource)
	rsvp := f.attach(t, agentevent.ParticipationSource)
	saved := f.attach(t, agentevent.SavedPlaceSource)
	var activity string
	if e := b.pool.QueryRow(b.ctx, `SELECT activity_id FROM activity_participations WHERE id=$1`, rsvp.Source.ID).Scan(&activity); e != nil {
		t.Fatal(e)
	}
	if _, e := b.pool.Exec(b.ctx, `INSERT INTO moment_activity_links(moment_id,activity_id,city_id,author_confirmed_at) VALUES($1,$2,'aberdeen-gb',clock_timestamp())`, moment.Source.ID, activity); e != nil {
		t.Fatal(e)
	}
	p := reinforcementPreview(t, f, s, 0, moment.ID)
	v := reinforcementApprove(t, f, s, p)
	if v.Version != 1 || v.SupportClusters != 1 || v.EvidenceCount != 1 {
		t.Fatal(v)
	}
	retry := reinforcementApprove(t, f, s, p)
	if !reflect.DeepEqual(v, retry) {
		t.Fatal("retry mutated ledger")
	}
	t.Run("same-event", func(t *testing.T) {
		p = reinforcementPreview(t, f, s, 1, rsvp.ID)
		v = reinforcementApprove(t, f, s, p)
		if v.Version != 2 || v.SupportClusters != 1 || v.EvidenceCount != 2 {
			t.Fatal(v)
		}
	})
	t.Run("independent-source", func(t *testing.T) {
		p = reinforcementPreview(t, f, s, 2, saved.ID)
		v = reinforcementApprove(t, f, s, p)
		if v.Version != 3 || v.SupportClusters != 2 || v.EvidenceCount != 3 {
			t.Fatal(v)
		}
	})
	t.Run("reopened-native-store", func(t *testing.T) {
		reopened := NewMemoryReinforcementService(New(b.pool, true), reinforcementFlags(t))
		loaded, e := reopened.ReadOwnReinforcement(b.ctx, f.native.base.private.owner, f.memory.ID, 1)
		if e != nil || !reflect.DeepEqual(v, loaded) {
			t.Fatal("durable support lost", e)
		}
	})
	t.Run("memory-unchanged", func(t *testing.T) {
		var raw []byte
		if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m) FROM agent_memories m WHERE id=$1`, f.memory.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		var row struct {
			Version    int64      `json:"version"`
			Confidence float64    `json:"confidence"`
			Last       *time.Time `json:"last_reinforced_at"`
		}
		if json.Unmarshal(raw, &row) != nil || row.Version != 1 || row.Confidence != 1 || row.Last != nil {
			t.Fatal("altered declaration")
		}
		var count int
		if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, f.memory.AgentID).Scan(&count) != nil || count != 1 {
			t.Fatal("duplicate Memory")
		}
	})
	t.Run("detach-conservative-clear", func(t *testing.T) {
		if _, e := b.store.RemoveOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, moment.ID, 1); e != nil {
			t.Fatal(e)
		}
		loaded, e := s.ReadOwnReinforcement(b.ctx, f.native.base.private.owner, f.memory.ID, 1)
		if e != nil || loaded.Version != 4 || loaded.EvidenceCount != 0 || loaded.LastSupportAt != nil {
			t.Fatal("detach retained approval", e, loaded)
		}
	})
	if before != agentMemoryOwnedSourceSnapshot(t, f.native.base.private) {
		t.Fatal("changed source identity/Profile/grants")
	}
}
func TestMemoryReinforcementNativePreviewBoundaries(t *testing.T) {
	changes := []string{"wrong-session", "wrong-person", "organization-principal", "missing-session", "wrong-memory-version", "wrong-support-version", "empty", "unknown-evidence", "duplicate-evidence", "model-unavailable", "json-preview", "another-service", "flag-off", "flag-off-on", "ctx-cancel", "metadata-version", "expired-preview", "future-preview", "memory-change", "memory-delete", "evidence-detach", "source-edit", "source-withdraw", "source-rebuild", "link-change", "session-revoked", "session-expired", "idle-expired", "account-disabled", "agent-disabled", "metadata-deleted", "memory-expired"}
	for _, name := range changes {
		t.Run(name, func(t *testing.T) {
			f, s := reinforcementFixture(t)
			b := f.native.base.private.base
			evidence := f.attach(t, agentevent.MomentSource)
			p := reinforcementPreview(t, f, s, 0, evidence.ID)
			access := f.native.base.private.owner
			ctx := b.ctx
			switch name {
			case "wrong-session":
				access.SessionDigest = [32]byte{1}
			case "wrong-person":
				access.WorkspacePrincipal.ID = b.other.ID
			case "organization-principal":
				access.WorkspacePrincipal.Type = actorref.Organization
			case "missing-session":
				access.SessionDigest = [32]byte{}
			case "wrong-memory-version":
				_, e := s.PreviewOwnReinforcement(ctx, access, f.memory.ID, 2, 0, []string{evidence.ID})
				if e == nil {
					t.Fatal("stale Memory accepted")
				}
				return
			case "wrong-support-version":
				_, e := s.PreviewOwnReinforcement(ctx, access, f.memory.ID, 1, 1, []string{evidence.ID})
				if e == nil {
					t.Fatal("stale support accepted")
				}
				return
			case "empty":
				_, e := s.PreviewOwnReinforcement(ctx, access, f.memory.ID, 1, 0, nil)
				if e == nil {
					t.Fatal("empty accepted")
				}
				return
			case "unknown-evidence":
				_, e := s.PreviewOwnReinforcement(ctx, access, f.memory.ID, 1, 0, []string{agentMemoryID(t, f.native.base.private)})
				if e == nil {
					t.Fatal("unknown accepted")
				}
				return
			case "duplicate-evidence":
				_, e := s.PreviewOwnReinforcement(ctx, access, f.memory.ID, 1, 0, []string{evidence.ID, evidence.ID})
				if e == nil {
					t.Fatal("duplicate accepted")
				}
				return
			case "model-unavailable":
				if !errors.Is(s.ReinforceForCognition(ctx), agentcognitive.ErrUnavailable) {
					t.Fatal("model port open")
				}
				return
			case "json-preview":
				if _, e := json.Marshal(p); e == nil {
					t.Fatal("preview exported")
				}
				var imported MemoryReinforcementPreview
				if json.Unmarshal([]byte(`{"confirmed":true,"verified":true}`), &imported) == nil || imported.service != nil {
					t.Fatal("JSON conferred authority")
				}
				return
			case "another-service":
				s = NewMemoryReinforcementService(b.store, s.flags)
			case "flag-off":
				if e := s.flags.Disable(agentfeature.Memory); e != nil {
					t.Fatal(e)
				}
			case "flag-off-on":
				if e := s.flags.Disable(agentfeature.Memory); e != nil {
					t.Fatal(e)
				}

				config, _ := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
				if e := s.flags.Replace(s.flags.Revision(), config); e != nil {
					t.Fatal(e)
				}

			case "ctx-cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "metadata-version":
				if _, e := b.pool.Exec(ctx, `UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, f.memory.AgentID); e != nil {
					t.Fatal(e)
				}
			case "session-revoked":
				if _, e := b.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.native.base.private.ownerSession); e != nil {
					t.Fatal(e)
				}
			case "session-expired":
				if _, e := b.pool.Exec(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp()-interval '1 millisecond' AS instant) UPDATE sessions SET idle_expires_at=n.instant,expires_at=n.instant FROM n WHERE id=$1`, f.native.base.private.ownerSession); e != nil {
					t.Fatal(e)
				}
			case "idle-expired":
				if _, e := b.pool.Exec(ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()-interval '1 millisecond' WHERE id=$1`, f.native.base.private.ownerSession); e != nil {
					t.Fatal(e)
				}
			case "account-disabled":
				if _, e := b.pool.Exec(ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID); e != nil {
					t.Fatal(e)
				}
			case "agent-disabled":
				if _, e := b.pool.Exec(ctx, `UPDATE agents SET status='retired' WHERE id=$1`, b.personID); e != nil {
					t.Fatal(e)
				}
			case "metadata-deleted":
				if _, e := b.pool.Exec(ctx, `DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID); e != nil {
					t.Fatal(e)
				}
			case "memory-expired":
				if _, e := b.pool.Exec(ctx, `UPDATE agent_memories SET version=version+1,valid_until=clock_timestamp()-interval '1 microsecond' WHERE id=$1`, f.memory.ID); e != nil {
					t.Fatal(e)
				}
			case "expired-preview":
				if e := b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.expires); e != nil {
					t.Fatal(e)
				}
			case "future-preview":
				if e := b.pool.QueryRow(ctx, `SELECT clock_timestamp()+interval '1 hour'`).Scan(&p.created); e != nil {
					t.Fatal(e)
				}
			case "memory-change":
				in := agentMemoryInput("evidence-005")
				in.ExpectedVersion = 1
				in.Summary = "合成新的声明"
				mustPutAgentMemory(t, f.native.base.private, f.memory.ID, in)
			case "memory-delete":
				if _, e := b.store.DeleteOwnMemory(ctx, access, f.memory.ID, 1); e != nil {
					t.Fatal(e)
				}
			case "evidence-detach":
				if _, e := b.store.RemoveOwnMemoryEvidence(ctx, access, f.memory.ID, evidence.ID, 1); e != nil {
					t.Fatal(e)
				}
			case "source-edit":
				if _, e := b.pool.Exec(ctx, `UPDATE moments SET revision=revision+1,updated_at=clock_timestamp(),body='合成编辑' WHERE id=$1`, evidence.Source.ID); e != nil {
					t.Fatal(e)
				}
			case "source-withdraw":
				if e := b.store.WithdrawMoment(ctx, b.person.ID, evidence.Source.ID, evidence.Source.Version.Revision); e != nil {
					t.Fatal(e)
				}
			case "source-rebuild": // Rebuild identical retained fields; only native row epoch changes.
				tx, e := b.pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(ctx)
				if _, e = tx.Exec(ctx, `CREATE TEMP TABLE reinforcement_moment_copy AS SELECT * FROM moments WHERE id=$1`, evidence.Source.ID); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(ctx, `DELETE FROM moments WHERE id=$1`, evidence.Source.ID); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(ctx, `INSERT INTO moments SELECT * FROM reinforcement_moment_copy`); e != nil {
					t.Fatal(e)
				}
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
			case "link-change":
				if _, e := b.pool.Exec(ctx, `INSERT INTO moment_activity_links(moment_id,activity_id,city_id,author_confirmed_at) VALUES($1,$2,'aberdeen-gb',clock_timestamp())`, evidence.Source.ID, f.native.activities[0]); e != nil {
					t.Fatal(e)
				}
			}
			v, e := s.ApproveOwnReinforcement(ctx, access, p)
			if e == nil || !reflect.DeepEqual(v, agentreinforcement.View{}) {
				t.Fatalf("boundary returned data %s %v", name, e)
			}
		})
	}
}
func TestMemoryReinforcementNativeCurrentSourceRecovery(t *testing.T) {
	for _, name := range []string{"moment-edit", "rsvp-cancel", "activity-hidden", "activity-expired", "block-host", "save-remove", "place-hidden", "place-expired", "memory-change"} {
		t.Run(name, func(t *testing.T) {
			f, s := reinforcementFixture(t)
			b := f.native.base.private.base
			kind := agentevent.MomentSource
			if name == "rsvp-cancel" || name == "activity-hidden" || name == "activity-expired" || name == "block-host" {
				kind = agentevent.ParticipationSource
			}
			if name == "save-remove" || name == "place-hidden" || name == "place-expired" {
				kind = agentevent.SavedPlaceSource
			}
			ev := f.attach(t, kind)
			p := reinforcementPreview(t, f, s, 0, ev.ID)
			reinforcementApprove(t, f, s, p)
			var q string
			var args []any
			switch name {
			case "moment-edit":
				q = `UPDATE moments SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`
				args = []any{ev.Source.ID}
			case "rsvp-cancel":
				q = `UPDATE activity_participations SET status='cancelled',cancelled_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`
				args = []any{ev.Source.ID}
			case "activity-hidden":
				q = `UPDATE activities SET publication_status='hidden' WHERE id=(SELECT activity_id FROM activity_participations WHERE id=$1)`
				args = []any{ev.Source.ID}
			case "activity-expired":
				q = `UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=(SELECT activity_id FROM activity_participations WHERE id=$1)`
				args = []any{ev.Source.ID}
			case "block-host":
				q = `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`
				args = []any{b.person.ID, b.other.ID}
			case "save-remove":
				q = `DELETE FROM saved_items WHERE id=$1`
				args = []any{ev.Source.ID}
			case "place-hidden":
				q = `UPDATE places SET publication_status='hidden' WHERE id=(SELECT place_id FROM saved_items WHERE id=$1)`
				args = []any{ev.Source.ID}
			case "place-expired":
				q = `UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=(SELECT place_id FROM saved_items WHERE id=$1)`
				args = []any{ev.Source.ID}
			case "memory-change":
				in := agentMemoryInput("evidence-005")
				in.ExpectedVersion = 1
				in.Summary = "修改后人工声明"
				f.memory = mustPutAgentMemory(t, f.native.base.private, f.memory.ID, in)
			}
			if q != "" {
				if _, e := b.pool.Exec(b.ctx, q, args...); e != nil {
					t.Fatal(e)
				}
			}
			v, e := s.ReadOwnReinforcement(b.ctx, f.native.base.private.owner, f.memory.ID, f.memory.Version)
			if e != nil || v.EvidenceCount != 0 || v.SupportClusters != 0 || v.LastSupportAt != nil || v.Version <= 1 {
				t.Fatal("stale support retained", e, v)
			}
			var raw string
			if e := b.pool.QueryRow(b.ctx, `SELECT entries::text FROM agent_memory_reinforcement WHERE memory_id=$1`, f.memory.ID).Scan(&raw); e != nil || raw != "[]" {
				t.Fatal("did not scrub", e)
			}
		})
	}
}
func TestMemoryReinforcementNativeConcurrentRetry(t *testing.T) {
	f, s := reinforcementFixture(t)
	evidence := f.attach(t, agentevent.MomentSource)
	p := reinforcementPreview(t, f, s, 0, evidence.ID)
	var wg sync.WaitGroup
	results := make(chan agentreinforcement.View, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.ApproveOwnReinforcement(context.Background(), f.native.base.private.owner, p)
			results <- v
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for v := range results {
		if v.Version != 1 || v.SupportClusters != 1 {
			t.Fatal("concurrent duplicate", v)
		}
	}
}
func TestMemoryReinforcementNativeDistinctPlansCAS(t *testing.T) {
	f, s := reinforcementFixture(t)
	ev1 := f.attach(t, agentevent.MomentSource)
	ev2 := f.attach(t, agentevent.SavedPlaceSource)
	p1 := reinforcementPreview(t, f, s, 0, ev1.ID)
	p2 := reinforcementPreview(t, f, s, 0, ev2.ID)
	reinforcementApprove(t, f, s, p1)
	if v, e := s.ApproveOwnReinforcement(context.Background(), f.native.base.private.owner, p2); !errors.Is(e, agentmemory.ErrConflict) || !reflect.DeepEqual(v, agentreinforcement.View{}) {
		t.Fatal("lost update allowed", e)
	}
}
func TestMemoryReinforcementNativeAdditionalSameActivityEvidence(t *testing.T) {
	f, s := reinforcementFixture(t)
	b := f.native.base.private.base
	rsvp := f.attach(t, agentevent.ParticipationSource)
	var activity string
	if e := b.pool.QueryRow(b.ctx, `SELECT activity_id FROM activity_participations WHERE id=$1`, rsvp.Source.ID).Scan(&activity); e != nil {
		t.Fatal(e)
	}
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: "aberdeen-gb", Title: "同一合成活动的另一条记录", Body: "合成本人记录", TimePrecision: "unknown", LocationPrecision: "city", ActivityID: &activity})
	if e != nil {
		t.Fatal(e)
	}
	ev, e := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, agentMemoryID(t, f.native.base.private), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentevent.MomentSource, SourceID: m.ID})
	if e != nil {
		t.Fatal(e)
	}
	p := reinforcementPreview(t, f, s, 0, rsvp.ID, ev.ID)
	v := reinforcementApprove(t, f, s, p)
	if v.EvidenceCount != 2 || v.SupportClusters != 1 {
		t.Fatal("same event double count", v)
	}
}
func TestMemoryReinforcementNativeOffAndNil(t *testing.T) {
	f, _ := reinforcementFixture(t)
	off, _ := agentfeature.NewController(agentfeature.DefaultConfig())
	for _, s := range []*MemoryReinforcementService{nil, NewMemoryReinforcementService(nil, nil), NewMemoryReinforcementService(f.native.base.private.base.store, off)} {
		if _, e := s.ReadOwnReinforcement(context.Background(), f.native.base.private.owner, f.memory.ID, 1); !errors.Is(e, agentmemory.ErrUnavailable) {
			t.Fatal("OFF available", e)
		}
	}
}

func TestMemoryReinforcementNativeConnectionTimeZone(t *testing.T) {
	for _, zone := range []string{"Asia/Tokyo", "Pacific/Honolulu"} {
		t.Run(zone, func(t *testing.T) {
			f, s := reinforcementFixture(t)
			moment := f.attach(t, agentevent.MomentSource)
			rsvp := f.attach(t, agentevent.ParticipationSource)
			saved := f.attach(t, agentevent.SavedPlaceSource)
			p := reinforcementPreview(t, f, s, 0, moment.ID, rsvp.ID, saved.ID)
			initial := reinforcementApprove(t, f, s, p)
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.ConnConfig.RuntimeParams["timezone"] = zone
			pool, e := pgxpool.NewWithConfig(context.Background(), cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			zoned := NewMemoryReinforcementService(New(pool, true), reinforcementFlags(t))
			read, e := zoned.ReadOwnReinforcement(context.Background(), f.native.base.private.owner, f.memory.ID, 1)
			if e != nil || !reflect.DeepEqual(initial, read) {
				t.Fatal("connection timezone changed source token", e)
			}
			for i := 0; i < 20; i++ {
				next := reinforcementPreview(t, f, zoned, read.Version, moment.ID, rsvp.ID, saved.ID)
				read = reinforcementApprove(t, f, zoned, next)
			}
			if read.EvidenceCount != 3 || read.SupportClusters != 3 {
				t.Fatal("UTC clock read drift", read)
			}
		})
	}
}
func TestMemoryReinforcementNativeFinalACLAfterInsertWait(t *testing.T) {
	f, s := reinforcementFixture(t)
	b := f.native.base.private.base
	ev := f.attach(t, agentevent.ParticipationSource)
	p := reinforcementPreview(t, f, s, 0, ev.ID)
	// An owned disposable DB trigger pauses the actual INSERT after its guards.
	// Activity ACL can change without waiting on the native Participation lock.
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_lock(910009)`); e != nil {
		t.Fatal(e)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(910009)`)
	if _, e = b.pool.Exec(b.ctx, `CREATE FUNCTION reinforcement_test_insert_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(910009); RETURN NEW; END$$; CREATE TRIGGER zz_reinforcement_test_pause BEFORE INSERT ON agent_memory_reinforcement FOR EACH ROW EXECUTE FUNCTION reinforcement_test_insert_pause()`); e != nil {
		t.Fatal(e)
	}
	defer b.pool.Exec(context.Background(), `DROP TRIGGER zz_reinforcement_test_pause ON agent_memory_reinforcement;DROP FUNCTION reinforcement_test_insert_pause()`)
	result := make(chan error, 1)
	go func() {
		v, e := s.ApproveOwnReinforcement(b.ctx, f.native.base.private.owner, p)
		if !reflect.DeepEqual(v, agentreinforcement.View{}) {
			result <- errors.New("late ACL returned payload")
			return
		}
		result <- e
	}()
	deadline := time.Now().Add(8 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=910009 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("real INSERT did not reach controlled wait")
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE activities SET publication_status='hidden' WHERE id=(SELECT activity_id FROM activity_participations WHERE id=$1)`, ev.Source.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_unlock(910009)`); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
		if !errors.Is(e, agentmemory.ErrConflict) {
			t.Fatal("late final source gate", e)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("native operation did not complete")
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_reinforcement WHERE memory_id=$1`, f.memory.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("late rejection committed support", e)
	}
}
func TestMemoryReinforcementNativeMigrationCurrentDataDownReapply(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s := reinforcementFixture(t)
	b := f.native.base.private.base
	ev := f.attach(t, agentevent.MomentSource)
	p := reinforcementPreview(t, f, s, 0, ev.ID)
	reinforcementApprove(t, f, s, p)
	snapshot := func() string {
		var raw string
		if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('memory',(SELECT to_jsonb(m) FROM agent_memories m WHERE id=$1),'evidence',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agent_memory_evidence e WHERE memory_id=$1))::text`, f.memory.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	old := snapshot()
	native := agentMemoryOwnedSourceSnapshot(t, f.native.base.private)
	for _, name := range []string{"060_agent_memory_reinforcement.down.sql", "060_agent_memory_reinforcement.sql"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.pool.Exec(b.ctx, string(raw)); e != nil {
			t.Fatal("actual schema roundtrip", e)
		}
		if snapshot() != old || agentMemoryOwnedSourceSnapshot(t, f.native.base.private) != native {
			t.Fatal("schema changed old retained data")
		}
	}
	reopened := NewMemoryReinforcementService(b.store, reinforcementFlags(t))
	v, e := reopened.ReadOwnReinforcement(b.ctx, f.native.base.private.owner, f.memory.ID, 1)
	if e != nil || v.Version != 0 || v.SupportClusters != 0 {
		t.Fatal("down reapply retained deleted support", e)
	}
	next := reinforcementPreview(t, f, reopened, 0, ev.ID)
	v = reinforcementApprove(t, f, reopened, next)
	if v.SupportClusters != 1 || v.MemoryID != f.memory.ID || snapshot() != old {
		t.Fatal("reapply could not reinforce same stable Memory")
	}
}

func TestMemoryReinforcementNativeCrossMemoryEvidenceAndPreviewCopy(t *testing.T) {
	f, s := reinforcementFixture(t)
	b := f.native.base.private.base
	ev := f.attach(t, agentevent.MomentSource)
	other := mustPutAgentMemory(t, f.native.base.private, agentMemoryID(t, f.native.base.private), agentMemoryInput("another-explicit-memory"))
	if _, e := s.PreviewOwnReinforcement(b.ctx, f.native.base.private.owner, other.ID, 1, 0, []string{ev.ID}); !errors.Is(e, agentmemory.ErrForbidden) {
		t.Fatal("cross Memory Evidence accepted", e)
	}
	p := reinforcementPreview(t, f, s, 0, ev.ID)
	view := p.View()
	review := p.Review()
	if review.Memory.MemoryID != f.memory.ID || review.Memory.MemoryVersion != 1 || len(review.Evidence) != 1 || review.Evidence[0].EvidenceID != ev.ID || review.PlanDigest == "" || review.Purpose != "HUMAN_EXPLICIT_REINFORCEMENT" || !review.ExpiresAt.After(review.PreviewedAt) {
		t.Fatal("specific review missing")
	}
	review.Evidence[0].EvidenceID = "fake"
	if p.Review().Evidence[0].EvidenceID != ev.ID {
		t.Fatal("review aliased capability")
	}
	*view.Assessment.Value = 0
	if p.View().Assessment.Value == nil || *p.View().Assessment.Value != 1 {
		t.Fatal("preview view aliased internal assessment")
	}
	p2 := reinforcementPreview(t, f, s, 0, ev.ID)
	var broken MemoryReinforcementPreview = *p2
	if json.Unmarshal([]byte(`{"confirmed":true}`), &broken) == nil || broken.service != nil {
		t.Fatal("JSON failed to wipe capability")
	}
	if _, e := s.ApproveOwnReinforcement(b.ctx, f.native.base.private.owner, &broken); e == nil {
		t.Fatal("wiped capability accepted")
	}
	reinforcementApprove(t, f, s, p)
}
func TestMemoryReinforcementNativeTimeExpiryReadScrubs(t *testing.T) {
	f, s := reinforcementFixture(t)
	b := f.native.base.private.base
	if _, e := b.pool.Exec(b.ctx, `UPDATE agent_memories SET version=version+1,valid_until=clock_timestamp()+interval '1 second' WHERE id=$1`, f.memory.ID); e != nil {
		t.Fatal(e)
	}
	f.memory.Version = 2
	ev := f.attach(t, agentevent.MomentSource)
	p := reinforcementPreview(t, f, s, 0, ev.ID)
	reinforcementApprove(t, f, s, p)
	// The Memory remains ACTIVE/version2 in native rows: only DB time advances.
	if _, e := b.pool.Exec(b.ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (valid_until-clock_timestamp())))+0.01) FROM agent_memories WHERE id=$1`, f.memory.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.ReadOwnReinforcement(b.ctx, f.native.base.private.owner, f.memory.ID, 2)
	if !errors.Is(e, agentmemory.ErrNotFound) || !reflect.DeepEqual(v, agentreinforcement.View{}) {
		t.Fatal("expired Memory returned support", e)
	}
	var empty bool
	if e = b.pool.QueryRow(b.ctx, `SELECT entries='[]'::jsonb AND last_plan_digest IS NULL AND last_support_at IS NULL FROM agent_memory_reinforcement WHERE memory_id=$1`, f.memory.ID).Scan(&empty); e != nil || !empty {
		t.Fatal("time expiry did not commit authorized scrub", e)
	}
}
func TestMemoryReinforcementNativeLateSessionExpiry(t *testing.T) {
	f, s := reinforcementFixture(t)
	b := f.native.base.private.base
	ev := f.attach(t, agentevent.MomentSource)
	if _, e := b.pool.Exec(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1`, f.native.base.private.ownerSession); e != nil {
		t.Fatal(e)
	}
	p := reinforcementPreview(t, f, s, 0, ev.ID)
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_lock(910010)`); e != nil {
		t.Fatal(e)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(910010)`)
	if _, e = b.pool.Exec(b.ctx, `CREATE FUNCTION reinforcement_test_expiry_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(910010); RETURN NEW; END$$; CREATE TRIGGER zz_reinforcement_expiry_pause BEFORE INSERT ON agent_memory_reinforcement FOR EACH ROW EXECUTE FUNCTION reinforcement_test_expiry_pause()`); e != nil {
		t.Fatal(e)
	}
	defer b.pool.Exec(context.Background(), `DROP TRIGGER zz_reinforcement_expiry_pause ON agent_memory_reinforcement;DROP FUNCTION reinforcement_test_expiry_pause()`)
	results := make(chan error, 1)
	go func() {
		v, e := s.ApproveOwnReinforcement(b.ctx, f.native.base.private.owner, p)
		if !reflect.DeepEqual(v, agentreinforcement.View{}) {
			results <- errors.New("late expiry returned payload")
			return
		}
		results <- e
	}()
	waiting := false
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=910010 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting) != nil {
			t.Fatal("cannot observe native wait")
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("native INSERT did not wait")
	}
	// Observe the real DB session deadline, not a caller-supplied/fake clock.
	if _, e = conn.Exec(b.ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM(idle_expires_at-clock_timestamp())))+0.01) FROM sessions WHERE id=$1`, f.native.base.private.ownerSession); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(b.ctx, `SELECT pg_advisory_unlock(910010)`); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-results:
		if e == nil {
			t.Fatal("late session accepted")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("late operation did not finish")
	}
	var count int
	if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_reinforcement WHERE memory_id=$1`, f.memory.ID).Scan(&count) != nil || count != 0 {
		t.Fatal("late expiry committed support")
	}
}
