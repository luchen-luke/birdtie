package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/jackc/pgx/v5/pgxpool"
)

type memoryEvidenceFixture struct {
	native  *enrichmentFixture
	memory  agentmemory.Record
	sources map[agentevent.SourceType]string
}

func memoryEvidenceNativeFixture(t *testing.T) *memoryEvidenceFixture {
	t.Helper()
	f := &memoryEvidenceFixture{native: enrichmentNativeFixture(t), sources: map[agentevent.SourceType]string{}}
	b := f.native.base.private.base
	var installed bool
	if err := b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.agent_memory_evidence') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("Evidence tests require actual migration057")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := b.pool.Exec(ctx, `DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`, b.accounts); err != nil {
			t.Errorf("owned Evidence native parent cleanup: %v", err)
		}
	})
	id := agentMemoryID(t, f.native.base.private)
	f.memory = mustPutAgentMemory(t, f.native.base.private, id, agentMemoryInput("evidence-005"))
	f.sources[agentevent.MomentSource] = f.native.sources[agentevent.MomentCreated]
	f.sources[agentevent.ParticipationSource] = f.native.sources[agentevent.ActivityJoined]
	f.sources[agentevent.SavedPlaceSource] = f.native.sources[agentevent.PlaceSaved]
	return f
}
func (f *memoryEvidenceFixture) attach(t *testing.T, kind agentevent.SourceType) agentmemory.Evidence {
	t.Helper()
	b := f.native.base.private.base
	e, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, agentMemoryID(t, f.native.base.private), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: f.memory.Version, SourceType: kind, SourceID: f.sources[kind]})
	if err != nil || agentmemory.ValidateEvidence(e) != nil || e.OwnerID != b.person.ID || e.AgentID != b.personID || e.MemoryID != f.memory.ID || e.MemoryVersion != f.memory.Version || e.Status != agentmemory.EvidenceCurrent {
		t.Fatalf("native Evidence creation: %v", err)
	}
	return e
}
func evidenceReject(t *testing.T, e agentmemory.Evidence, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || !reflect.DeepEqual(e, agentmemory.Evidence{}) {
		t.Fatalf("Evidence must reject without payload: got %v want %v", err, want)
	}
}
func (f *memoryEvidenceFixture) provenance(t *testing.T) agentmemory.Provenance {
	t.Helper()
	b := f.native.base.private.base
	p, err := b.store.ReadOwnMemoryProvenance(b.ctx, f.native.base.private.owner, f.memory.ID)
	if err != nil || p.MemoryID != f.memory.ID || p.MemoryVersion != f.memory.Version || p.Declaration != "本人明确填写" {
		t.Fatalf("native provenance: %v", err)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "合成064私密Moment") || strings.Contains(string(raw), "合成064不能泄漏正文") || strings.Contains(string(raw), "出席") || strings.Contains(string(raw), "已到访") {
		t.Fatal("provenance copied private body or invented experience")
	}
	return p
}
func TestAgentMemoryEvidenceNativePersistenceCASAndProvenance(t *testing.T) {
	f := memoryEvidenceNativeFixture(t)
	b := f.native.base.private.base
	before := agentMemoryOwnedSourceSnapshot(t, f.native.base.private)
	all := []agentmemory.Evidence{}
	for _, kind := range []agentevent.SourceType{agentevent.MomentSource, agentevent.ParticipationSource, agentevent.SavedPlaceSource} {
		t.Run(string(kind), func(t *testing.T) {
			e := f.attach(t, kind)
			all = append(all, e)
			retried, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, e.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: kind, SourceID: f.sources[kind]})
			if err != nil || !reflect.DeepEqual(retried, e) {
				t.Fatalf("exact Evidence retry changed data: %v", err)
			}
		})
	}
	p := f.provenance(t)
	if len(p.Evidence) != 3 {
		t.Fatalf("expected three real distinct sources: %d", len(p.Evidence))
	}
	if !strings.Contains(p.Explanation, "私人记录") || !strings.Contains(p.Explanation, "报名") || !strings.Contains(p.Explanation, "收藏") {
		t.Fatal("missing concise Chinese provenance")
	}
	reopened := New(b.pool, true)
	loaded, err := reopened.ReadOwnMemoryProvenance(b.ctx, f.native.base.private.owner, f.memory.ID)
	if err != nil || !reflect.DeepEqual(loaded, p) {
		t.Fatal("new native Store did not load committed provenance")
	}
	t.Run("duplicate-source", func(t *testing.T) {
		e, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, agentMemoryID(t, f.native.base.private), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentevent.MomentSource, SourceID: f.sources[agentevent.MomentSource]})
		evidenceReject(t, e, err, agentmemory.ErrConflict)
	})
	t.Run("wrong-memory-version", func(t *testing.T) {
		e, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, agentMemoryID(t, f.native.base.private), agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 2, SourceType: agentevent.MomentSource, SourceID: f.sources[agentevent.MomentSource]})
		evidenceReject(t, e, err, agentmemory.ErrConflict)
	})
	if before != agentMemoryOwnedSourceSnapshot(t, f.native.base.private) {
		t.Fatal("Evidence changed native identity/Profile/source versions or grants")
	}
	removed, err := b.store.RemoveOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, all[0].ID, 1)
	if err != nil || removed.Status != agentmemory.EvidenceRemoved || removed.Version != 2 || removed.Source != nil || removed.EventTime != nil || removed.Weight != 0 {
		t.Fatalf("remove must scrub source: %v", err)
	}
	retry, err := b.store.RemoveOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, all[0].ID, 1)
	if err != nil || !reflect.DeepEqual(retry, removed) {
		t.Fatal("remove retry changed tombstone")
	}
	if len(f.provenance(t).Evidence) != 2 {
		t.Fatal("removed Evidence leaked into provenance")
	}
	t.Run("removed-id-cannot-return", func(t *testing.T) {
		e, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, all[0].ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentevent.MomentSource, SourceID: f.sources[agentevent.MomentSource]})
		evidenceReject(t, e, err, agentmemory.ErrConflict)
	})
	in := agentMemoryInput("evidence-005")
	in.ExpectedVersion = 1
	in.Summary = "本人修改声明005"
	f.memory = mustPutAgentMemory(t, f.native.base.private, f.memory.ID, in)
	if len(f.provenance(t).Evidence) != 0 {
		t.Fatal("Memory change retained previous version evidence")
	}
	var stale int
	if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1 AND (status<>'REMOVED' OR source_id IS NOT NULL OR source_token IS NOT NULL OR signal_type IS NOT NULL OR weight<>0 OR event_time IS NOT NULL)`, f.memory.ID).Scan(&stale); err != nil || stale != 0 {
		t.Fatal("Memory change not atomically scrubbed")
	}
	newEvidence := f.attach(t, agentevent.MomentSource)
	if _, err = b.store.DeleteOwnMemory(b.ctx, f.native.base.private.owner, f.memory.ID, f.memory.Version); err != nil {
		t.Fatal(err)
	}
	if p, err = b.store.ReadOwnMemoryProvenance(b.ctx, f.native.base.private.owner, f.memory.ID); !errors.Is(err, agentmemory.ErrNotFound) || !reflect.DeepEqual(p, agentmemory.Provenance{}) {
		t.Fatal("deleted Memory released provenance")
	}
	if e, err := b.store.RemoveOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, newEvidence.ID, 1); err != nil || e.Status != agentmemory.EvidenceRemoved {
		t.Fatal("owner cannot inspect already scrubbed control after delete")
	}
}

func TestAgentMemoryEvidenceNativeSourceInvalidation(t *testing.T) {
	for _, change := range []string{"moment-edit", "moment-withdraw", "participation-cancel", "activity-hidden", "activity-expired", "block-host", "save-remove", "place-hidden", "place-expired"} {
		t.Run(change, func(t *testing.T) {
			f := memoryEvidenceNativeFixture(t)
			b := f.native.base.private.base
			kind := agentevent.MomentSource
			if strings.HasPrefix(change, "participation") || strings.HasPrefix(change, "activity") || change == "block-host" {
				kind = agentevent.ParticipationSource
			}
			if strings.HasPrefix(change, "save") || strings.HasPrefix(change, "place") {
				kind = agentevent.SavedPlaceSource
			}
			e := f.attach(t, kind)
			if len(f.provenance(t).Evidence) != 1 {
				t.Fatal("positive source missing")
			}
			switch change {
			case "moment-edit":
				m := f.native.base.moment
				_, err := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, content.MomentInput{CityID: "aberdeen-gb", Title: "合成005修改", Body: "不能复用旧版本", TimePrecision: "unknown", LocationPrecision: "city"})
				if err != nil {
					t.Fatal(err)
				}
			case "moment-withdraw":
				if err := b.store.WithdrawMoment(b.ctx, b.person.ID, f.native.base.moment.ID, f.native.base.moment.Revision); err != nil {
					t.Fatal(err)
				}
			case "participation-cancel":
				var target string
				if err := b.pool.QueryRow(b.ctx, `SELECT activity_id FROM activity_participations WHERE id=$1`, f.sources[kind]).Scan(&target); err != nil {
					t.Fatal(err)
				}
				if _, err := b.store.CancelParticipation(b.ctx, b.person.ID, target); err != nil {
					t.Fatal(err)
				}
			case "activity-hidden":
				if _, err := b.pool.Exec(b.ctx, `UPDATE activities SET publication_status='hidden' WHERE id=(SELECT activity_id FROM activity_participations WHERE id=$1)`, f.sources[kind]); err != nil {
					t.Fatal(err)
				}
			case "activity-expired":
				if _, err := b.pool.Exec(b.ctx, `UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=(SELECT activity_id FROM activity_participations WHERE id=$1)`, f.sources[kind]); err != nil {
					t.Fatal(err)
				}
			case "block-host":
				if _, err := b.pool.Exec(b.ctx, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, b.person.ID, b.other.ID); err != nil {
					t.Fatal(err)
				}
			case "save-remove":
				if err := b.store.RemoveSaved(b.ctx, b.person.ID, f.sources[kind]); err != nil {
					t.Fatal(err)
				}
			case "place-hidden":
				if _, err := b.pool.Exec(b.ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, f.native.place); err != nil {
					t.Fatal(err)
				}
			case "place-expired":
				if _, err := b.pool.Exec(b.ctx, `UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.native.place); err != nil {
					t.Fatal(err)
				}
			}
			if len(f.provenance(t).Evidence) != 0 {
				t.Fatal("stale/withdrawn/inaccessible source survived current resolver")
			}
			got, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, e.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: kind, SourceID: f.sources[kind]})
			if change == "moment-edit" {
				evidenceReject(t, got, err, agentmemory.ErrConflict)
			} else {
				evidenceReject(t, got, err, agentmemory.ErrForbidden)
			}
			removed, err := b.store.RemoveOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, e.ID, 1)
			if err != nil || removed.Source != nil {
				t.Fatal("removed source prevents owner control cleanup")
			}
		})
	}
}

func TestAgentMemoryEvidenceNativeIdentityAndConcurrentRetry(t *testing.T) {
	f := memoryEvidenceNativeFixture(t)
	b := f.native.base.private.base
	e := f.attach(t, agentevent.MomentSource)
	for _, access := range []agentprofile.PrivateAccess{f.native.base.private.peer, f.native.base.private.org, f.native.base.private.biz, {}} {
		t.Run(string(access.WorkspacePrincipal.Type)+access.WorkspacePrincipal.ID, func(t *testing.T) {
			p, err := b.store.ReadOwnMemoryProvenance(b.ctx, access, f.memory.ID)
			if err == nil || !reflect.DeepEqual(p, agentmemory.Provenance{}) {
				t.Fatal("cross principal released provenance")
			}
			got, err := b.store.PutOwnMemoryEvidence(b.ctx, access, f.memory.ID, e.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentevent.MomentSource, SourceID: f.sources[agentevent.MomentSource]})
			if err == nil || !reflect.DeepEqual(got, agentmemory.Evidence{}) {
				t.Fatal("cross principal wrote source")
			}
		})
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, e.ID, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: agentevent.MomentSource, SourceID: f.sources[agentevent.MomentSource]})
			if err != nil {
				errs <- err
			} else if !reflect.DeepEqual(got, e) {
				errs <- errors.New("retry mismatch")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(f.provenance(t).Evidence) != 1 {
		t.Fatal("parallel retries duplicated evidence")
	}
	if _, err := b.pool.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.native.base.private.owner.SessionDigest[:]); err != nil {
		t.Fatal(err)
	}
	p, err := b.store.ReadOwnMemoryProvenance(b.ctx, f.native.base.private.owner, f.memory.ID)
	if !errors.Is(err, agentmemory.ErrForbidden) || !reflect.DeepEqual(p, agentmemory.Provenance{}) {
		t.Fatal("revoked session released provenance")
	}
}

func TestAgentMemoryEvidenceTimeZoneAndLazyReassociation(t *testing.T) {
	f := memoryEvidenceNativeFixture(t)
	b := f.native.base.private.base
	config, err := pgxpool.ParseConfig(b.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["timezone"] = "Asia/Shanghai"
	pool, err := pgxpool.NewWithConfig(b.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	localStore := New(pool, true)
	for _, kind := range []agentevent.SourceType{agentevent.ParticipationSource, agentevent.SavedPlaceSource} {
		t.Run(string(kind), func(t *testing.T) {
			id := agentMemoryID(t, f.native.base.private)
			in := agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: kind, SourceID: f.sources[kind]}
			first, err := localStore.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, id, in)
			if err != nil {
				t.Fatal(err)
			}
			second, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, id, in)
			if err != nil || !reflect.DeepEqual(first, second) {
				t.Fatalf("actual timezone-dependent retry: %v", err)
			}
		})
	}
	if len(f.provenance(t).Evidence) != 2 {
		t.Fatal("timezone reconnect invalidated current references")
	}
	e := f.attach(t, agentevent.MomentSource)
	m := f.native.base.moment
	if _, err = b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, content.MomentInput{CityID: "aberdeen-gb", Title: "合成005新源版本", Body: "新引用需要当前版本", TimePrecision: "unknown", LocationPrecision: "city"}); err != nil {
		t.Fatal(err)
	}
	if len(f.provenance(t).Evidence) != 2 {
		t.Fatal("lazy retirement included stale Moment")
	}
	var scrubbed bool
	if err = b.pool.QueryRow(b.ctx, `SELECT status='REMOVED' AND version=2 AND source_id IS NULL AND source_token IS NULL AND signal_type IS NULL AND weight=0 AND event_time IS NULL FROM agent_memory_evidence WHERE id=$1`, e.ID).Scan(&scrubbed); err != nil || !scrubbed {
		t.Fatal("lazy retirement did not scrub stored source metadata")
	}
	newE := f.attach(t, agentevent.MomentSource)
	if newE.Source.Version.Revision != m.Revision+1 {
		t.Fatal("new reference reused old source version")
	}
	if len(f.provenance(t).Evidence) != 3 {
		t.Fatal("stale slot prevented a newly explicit current reference")
	}
}

func TestAgentMemoryEvidenceSourceLockWaitPastDeadlineRollsBack(t *testing.T) {
	for _, boundary := range []string{"memory-deadline", "session-deadline"} {
		t.Run(boundary, func(t *testing.T) {
			f := memoryEvidenceNativeFixture(t)
			b := f.native.base.private.base
			deadline := time.Now().UTC().Truncate(time.Microsecond).Add(1200 * time.Millisecond)
			if boundary == "memory-deadline" {
				in := agentMemoryInput("evidence-005")
				in.ExpectedVersion = 1
				in.ValidUntil = deadline
				f.memory = mustPutAgentMemory(t, f.native.base.private, f.memory.ID, in)
			} else {
				if _, err := b.pool.Exec(b.ctx, `UPDATE sessions SET expires_at=$2,idle_expires_at=$2 WHERE token_sha256=$1`, f.native.base.private.owner.SessionDigest[:], deadline); err != nil {
					t.Fatal(err)
				}
			}
			locker, err := b.pool.Begin(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer locker.Rollback(b.ctx)
			var sourceID string
			if err = locker.QueryRow(b.ctx, `SELECT id FROM moments WHERE id=$1 AND author_account_id=$2 FOR UPDATE`, f.sources[agentevent.MomentSource], b.person.ID).Scan(&sourceID); err != nil {
				t.Fatal(err)
			}
			var xid string
			if err = locker.QueryRow(b.ctx, `SELECT pg_current_xact_id()::text`).Scan(&xid); err != nil {
				t.Fatal(err)
			}
			id := agentMemoryID(t, f.native.base.private)
			type result struct {
				e   agentmemory.Evidence
				err error
			}
			done := make(chan result, 1)
			go func() {
				e, err := b.store.PutOwnMemoryEvidence(b.ctx, f.native.base.private.owner, f.memory.ID, id, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: f.memory.Version, SourceType: agentevent.MomentSource, SourceID: sourceID})
				done <- result{e, err}
			}()
			reached := false
			limit := time.Now().Add(3 * time.Second)
			for time.Now().Before(limit) {
				if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='transactionid' AND transactionid::text=$1 AND NOT granted)`, xid).Scan(&reached); err != nil {
					t.Fatal(err)
				}
				if reached {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !reached {
				t.Fatal("actual source-row wait was not observed")
			}
			expired := false
			for !expired {
				if err = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=$1::timestamptz`, deadline).Scan(&expired); err != nil {
					t.Fatal(err)
				}
				if !expired {
					time.Sleep(5 * time.Millisecond)
				}
			}
			if err = locker.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case r := <-done:
				evidenceReject(t, r.e, r.err, agentmemory.ErrForbidden)
			case <-time.After(5 * time.Second):
				t.Fatal("source wait did not recover after releasing owned lock")
			}
			var count int
			if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1`, f.memory.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("expired source wait committed Evidence")
			}
			t.Logf("ACTUAL_SOURCE_ROW_WAIT=%s reached=true PGclockPastDeadline=true evidenceRows=0", boundary)
		})
	}
}
