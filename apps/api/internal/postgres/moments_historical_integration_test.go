package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestHistoricalMomentNativePersistence(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agent_domain_outbox WHERE source_type='MOMENT' AND source_id IN (SELECT id FROM moments WHERE author_account_id=ANY($1::uuid[]))`,
			`DELETE FROM audit_events WHERE resource_type='moment' AND actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`,
		} {
			if _, err := b.pool.Exec(ctx, q, b.accounts); err != nil {
				t.Errorf("owned historical native cleanup: %v", err)
			}
		}
	})
	for _, precision := range []string{"unknown", "year", "month", "day", "instant"} {
		t.Run(precision, func(t *testing.T) {
			occurrence := time.Date(2025, 9, 12, 9, 30, 12, 123456000, time.UTC)
			in := content.MomentInput{CityID: "aberdeen-gb", Title: "2025年的阿伯丁旅行", Body: "合成本人声明，非到访证明", TimePrecision: precision, LocationPrecision: "city"}
			if precision != "unknown" {
				in.OccurredAt = &occurrence
			}
			m, err := b.store.CreateMomentDraft(b.ctx, b.person.ID, in)
			if err != nil {
				t.Fatal(err)
			}
			if m.TimePrecision != precision || m.CreatedAt.IsZero() || m.Status != "draft" || m.Visibility != "private" {
				t.Fatal("native historical shape/privacy lost")
			}
			var eventTime time.Time
			if err = b.pool.QueryRow(b.ctx, `SELECT occurred_at FROM agent_domain_outbox WHERE source_type='MOMENT' AND source_id=$1 AND event_type='MOMENT_CREATED'`, m.ID).Scan(&eventTime); err != nil || !eventTime.Equal(m.CreatedAt) {
				t.Fatalf("native event clock must remain creation time, not historical occurrence: %v", err)
			}
			if precision == "unknown" {
				if m.OccurredAt != nil {
					t.Fatal("unknown fabricated occurrence")
				}
			} else if m.OccurredAt == nil || !m.OccurredAt.Equal(occurrence) || m.CreatedAt.Equal(occurrence) {
				t.Fatal("occurrence conflated with create clock")
			}
			conn, err := b.pool.Acquire(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = conn.Exec(b.ctx, `SET TIME ZONE 'Pacific/Honolulu'`); err != nil {
				conn.Release()
				t.Fatal(err)
			}
			loaded, err := scanMoment(conn.QueryRow(b.ctx, `SELECT `+momentColumns+` FROM moments m WHERE id=$1`, m.ID))
			if _, e := conn.Exec(b.ctx, `SET TIME ZONE 'UTC'`); e != nil {
				conn.Release()
				t.Fatal(e)
			}
			conn.Release()
			if err != nil || loaded.TimePrecision != precision || !loaded.CreatedAt.Equal(m.CreatedAt) {
				t.Fatal("new connection/timezone altered occurrence/creation")
			}
			if m.OccurredAt != nil && (loaded.OccurredAt == nil || !loaded.OccurredAt.Equal(*m.OccurredAt)) {
				t.Fatal("timezone changed historical instant")
			}
			in.Title = "只修改标题"
			updated, err := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, in)
			if err != nil || updated.Revision != m.Revision+1 || !updated.CreatedAt.Equal(m.CreatedAt) || updated.TimePrecision != precision {
				t.Fatalf("historical edit persistence: %v", err)
			}
			if _, err = b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, in); !errors.Is(err, content.ErrConflict) {
				t.Fatal("stale revision accepted")
			}
			if _, err = b.store.GetOwnMoment(b.ctx, b.other.ID, m.ID); !errors.Is(err, content.ErrNotFound) {
				t.Fatal("historical private read leaked")
			}
			if _, err = b.store.UpdateMomentDraft(b.ctx, b.other.ID, m.ID, updated.Revision, in); !errors.Is(err, content.ErrConflict) {
				t.Fatal("other account edited historical private Moment")
			}
			in.OccurredAt = nil
			in.TimePrecision = "unknown"
			cleared, err := b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, updated.Revision, in)
			if err != nil || cleared.OccurredAt != nil || cleared.TimePrecision != "unknown" || !cleared.CreatedAt.Equal(m.CreatedAt) {
				t.Fatal("explicit unknown substituted creation")
			}
			if err = b.store.WithdrawMoment(b.ctx, b.person.ID, m.ID, cleared.Revision); err != nil {
				t.Fatal(err)
			}
			list, err := b.store.ListOwnMoments(b.ctx, b.person.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range list {
				if item.ID == m.ID {
					t.Fatal("withdrawn historical Moment remained in own list")
				}
			}
		})
	}
}

func TestHistoricalMomentCandidateLockOrder(t *testing.T) {
	f, candidates := memoryCandidateFixture(t)
	b := f.base.private.base
	var deadlocksBefore int64
	if err := b.pool.QueryRow(b.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocksBefore); err != nil {
		t.Fatal(err)
	}
	actor, err := b.store.Authenticate(b.ctx, f.base.private.owner.SessionDigest)
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	application := "historical_human_lock_" + b.personID
	config.ConnConfig.RuntimeParams["application_name"] = application
	humanPool, err := pgxpool.NewWithConfig(b.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer humanPool.Close()
	humanStore := New(humanPool, b.store.devPhoneEnabled)
	momentID := f.sources[agentevent.MomentCreated]
	m, err := b.store.GetOwnMoment(b.ctx, actor.ID, momentID)
	if err != nil {
		t.Fatal(err)
	}
	momentConn, err := b.pool.Acquire(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer momentConn.Release()
	momentBlock, err := momentConn.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer momentBlock.Rollback(context.Background())
	if _, err = momentBlock.Exec(b.ctx, `SELECT id FROM moments WHERE id=$1 FOR UPDATE`, momentID); err != nil {
		t.Fatal(err)
	}
	metaConn, err := b.pool.Acquire(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer metaConn.Release()
	metaBlock, err := metaConn.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer metaBlock.Rollback(context.Background())
	if _, err = metaBlock.Exec(b.ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR SHARE`, b.personID); err != nil {
		t.Fatal(err)
	}
	wait := func(pid uint32, pattern string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			var reached bool
			if e := b.pool.QueryRow(b.ctx, `WITH RECURSIVE waiters AS (
 SELECT pid FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid))
 UNION SELECT a.pid FROM pg_stat_activity a JOIN waiters w ON w.pid=ANY(pg_blocking_pids(a.pid)))
 SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN waiters w ON w.pid=a.pid WHERE a.query LIKE $2)`, int(pid), pattern).Scan(&reached); e != nil {
				t.Fatal(e)
			}
			if reached {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("real candidate/human path did not reach expected native lock barrier")
	}
	candidateResult := make(chan error, 1)
	draft := memoryCandidateDraft(t, f)
	target := agentMemoryID(t, f.base.private)
	go func() {
		_, e := candidates.SaveOwnCandidate(b.ctx, f.base.private.owner, target, draft, "", 0)
		candidateResult <- e
	}()
	wait(metaConn.Conn().PgConn().PID(), "%FROM agent_profiles%FOR UPDATE%")
	var candidatePID int
	if err = b.pool.QueryRow(b.ctx, `SELECT pid FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM agent_profiles%FOR UPDATE%'`, int(metaConn.Conn().PgConn().PID())).Scan(&candidatePID); err != nil {
		t.Fatal(err)
	}
	humanResult := make(chan error, 1)
	go func() {
		_, e := humanStore.UpdateHumanMomentDraft(b.ctx, f.base.private.owner.SessionDigest, actor, m.ID, m.Revision, content.MomentInput{CityID: m.CityID, PlaceID: m.PlaceID, Title: "本人修改历史记录；锁序合成测试", Body: m.Body, OccurredAt: m.OccurredAt, TimePrecision: m.TimePrecision, LocationPrecision: m.LocationPrecision})
		humanResult <- e
	}()
	// Before the repair Human waits on Moment; after the repair it waits on
	// optional metadata SHARE. Either actual path must be observed, no sleeps
	// standing in for a transaction reaching its dependency.
	deadline := time.Now().Add(8 * time.Second)
	humanReached := false
	var humanPID int
	for time.Now().Before(deadline) {
		var momentWait, metaWait bool
		if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%UPDATE moments%'),EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%agent_profiles%' AND query LIKE '%FOR SHARE%')`, application).Scan(&momentWait, &metaWait); e != nil {
			t.Fatal(e)
		}
		if momentWait || metaWait {
			t.Logf("LOCAL_SYNTHETIC_ONLY Human waitingMoment=%t waitingOptionalMetadata=%t", momentWait, metaWait)
			humanReached = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !humanReached {
		t.Fatal("actual Human path did not reach metadata/Moment barrier")
	}
	if err = b.pool.QueryRow(b.ctx, `SELECT pid FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock'`, application).Scan(&humanPID); err != nil {
		t.Fatal(err)
	}
	if err = metaBlock.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}
	// With the repaired compatible metadata SHARE, Candidate can remain queued
	// on Human metadata instead of acquiring metadata and queueing on Moment.
	// Confirm one of those actual edges before releasing the final barrier.
	deadline = time.Now().Add(8 * time.Second)
	candidateReached := false
	for time.Now().Before(deadline) {
		var oldMomentEdge, newMetadataEdge bool
		if err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock' AND query LIKE '%FROM moments%FOR SHARE%'),EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock' AND query LIKE '%FROM agent_profiles%FOR UPDATE%' AND $2::integer=ANY(pg_blocking_pids(pid)))`, candidatePID, humanPID).Scan(&oldMomentEdge, &newMetadataEdge); err != nil {
			t.Fatal(err)
		}
		if oldMomentEdge || newMetadataEdge {
			candidateReached = true
			t.Logf("LOCAL_SYNTHETIC_ONLY Candidate waitingMoment=%t waitingHumanMetadata=%t", oldMomentEdge, newMetadataEdge)
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !candidateReached {
		t.Fatal("actual Candidate did not reach source or Human metadata lock")
	}
	if err = momentBlock.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}
	var candidateErr, humanErr error
	select {
	case candidateErr = <-candidateResult:
	case <-time.After(8 * time.Second):
		t.Fatal("candidate path did not finish")
	}
	select {
	case humanErr = <-humanResult:
	case <-time.After(8 * time.Second):
		t.Fatal("human path did not finish")
	}
	t.Logf("LOCAL_SYNTHETIC_RED_OR_GREEN actualCandidateError=%v actualHumanError=%v", candidateErr, humanErr)
	var deadlocksAfter int64
	// PG statistics can flush shortly after the victim rolls back. This bounded
	// observation records the real database counter, never infers a deadlock
	// merely from a mapped conflict or elapsed time.
	until := time.Now().Add(2 * time.Second)
	for {
		if _, err = b.pool.Exec(b.ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
			t.Fatal(err)
		}
		if err = b.pool.QueryRow(b.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocksAfter); err != nil {
			t.Fatal(err)
		}
		if deadlocksAfter > deadlocksBefore || candidateErr == nil && humanErr == nil || !time.Now().Before(until) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("LOCAL_SYNTHETIC_ONLY databaseDeadlocksBefore=%d after=%d", deadlocksBefore, deadlocksAfter)
	if candidateErr != nil || humanErr != nil || deadlocksAfter != deadlocksBefore {
		t.Fatalf("actual candidate/human lock graph failed: candidate=%v human=%v", candidateErr, humanErr)
	}
}

func TestHistoricalMomentHumanGatewayWithoutAgent(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agent_domain_outbox WHERE source_type='MOMENT' AND source_id IN(SELECT id FROM moments WHERE author_account_id=ANY($1::uuid[]))`,
			`DELETE FROM audit_events WHERE resource_type='moment' AND actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`,
		} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Error(e)
			}
		}
	})
	actor, err := b.store.Authenticate(b.ctx, f.owner.SessionDigest)
	if err != nil {
		t.Fatal(err)
	}
	b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
	b.exec(`DELETE FROM agents WHERE id=$1`, b.personID)
	input := content.MomentInput{CityID: "aberdeen-gb", Title: "历史声明无需Agent", TimePrecision: "unknown", LocationPrecision: "city"}
	m, err := b.store.CreateHumanMomentDraft(b.ctx, f.owner.SessionDigest, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_domain_outbox WHERE source_type='MOMENT' AND source_id=$1`, m.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("human create invented Agent/capture")
	}
	loaded, err := b.store.GetHumanMoment(b.ctx, f.owner.SessionDigest, actor, m.ID)
	if err != nil || loaded.ID != m.ID {
		t.Fatal("current human read requires Agent")
	}
	list, err := b.store.ListHumanMoments(b.ctx, f.owner.SessionDigest, actor)
	if err != nil || len(list) != 1 {
		t.Fatal("current human list requires Agent")
	}
	for _, access := range []struct {
		digest [32]byte
		actor  identity.Actor
	}{{f.peer.SessionDigest, actor}, {[32]byte{}, actor}, {f.owner.SessionDigest, identity.Actor{ID: b.org.ID, AccountType: "organization"}}, {f.owner.SessionDigest, identity.Actor{ID: b.business.ID, AccountType: "business"}}} {
		if _, e := b.store.CreateHumanMomentDraft(b.ctx, access.digest, access.actor, input); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("client/mismatched/non-person identity authorized", e)
		}
	}
	future := time.Now().UTC().Add(48 * time.Hour)
	input.TimePrecision = "instant"
	input.OccurredAt = &future
	if _, e := b.store.CreateHumanMomentDraft(b.ctx, f.owner.SessionDigest, actor, input); !errors.Is(e, content.ErrInvalid) {
		t.Fatal("future bound bypassed database clock", e)
	}
	input.TimePrecision = "year"
	past := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	input.OccurredAt = &past
	updated, err := b.store.UpdateHumanMomentDraft(b.ctx, f.owner.SessionDigest, actor, m.ID, m.Revision, input)
	if err != nil || updated.Revision != 2 || updated.TimePrecision != "year" || !updated.CreatedAt.Equal(m.CreatedAt) {
		t.Fatal("native Human CAS/time edit failed", err)
	}
	if _, e := b.store.UpdateHumanMomentDraft(b.ctx, f.owner.SessionDigest, actor, m.ID, m.Revision, input); !errors.Is(e, content.ErrConflict) {
		t.Fatal("old concrete version accepted", e)
	}
	canceled, cancel := context.WithCancel(b.ctx)
	cancel()
	if _, e := b.store.GetHumanMoment(canceled, f.owner.SessionDigest, actor, m.ID); !errors.Is(e, content.ErrUnavailable) {
		t.Fatal("canceled context read produced payload", e)
	}
	b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, actor.ID)
	if _, e := b.store.GetHumanMoment(b.ctx, f.owner.SessionDigest, actor, m.ID); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("cached actor read suspended account", e)
	}
	if e := b.store.WithdrawHumanMoment(b.ctx, f.owner.SessionDigest, actor, m.ID, updated.Revision); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("cached actor withdrew under suspended account", e)
	}
	b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, actor.ID)
	if err = b.store.WithdrawHumanMoment(b.ctx, f.owner.SessionDigest, actor, m.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}
	list, err = b.store.ListHumanMoments(b.ctx, f.owner.SessionDigest, actor)
	if err != nil || len(list) != 0 {
		t.Fatal("withdrawn historical data remained listed")
	}
}
