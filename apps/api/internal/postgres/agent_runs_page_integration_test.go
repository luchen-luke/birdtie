package postgres

import (
	"context"
	"errors"
	"fmt"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
	"time"
)

func TestAgentRunPageNativeBeyondTwentyAndSessionABA(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s, a := runFixture(t, false)
	b := f.f.f.place.private.base
	for i := 0; i < 23; i++ {
		m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.f.f.place.city, Title: fmt.Sprintf("LOCAL_SYNTHETIC_RUN_%d", i), Body: "仅本地运行元数据测试", TimePrecision: "unknown", LocationPrecision: "city"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: m.ID}); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		_, e := b.pool.Exec(context.Background(), `DELETE FROM agent_enrichment_runs WHERE owner_id=$1`, b.person.ID)
		if e != nil {
			t.Error(e)
		}
	})
	first, e := s.ReadOwnRunPage(b.ctx, a, "")
	if e != nil || len(first.Items) != 20 || first.NextCursor == nil {
		t.Fatal(first, e)
	}
	if e = s.RevalidateOwnRunPage(b.ctx, a, first); e != nil {
		t.Fatal("fresh final fence", e)
	}
	var sessionBefore, sessionAfter string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(s)::text FROM sessions s WHERE token_sha256=$1`, a.SessionDigest[:]).Scan(&sessionBefore); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthenticateRunPage(b.ctx, a.SessionDigest); e != nil {
		t.Fatal(e)
	}
	if e = s.RevalidateOwnRunPage(b.ctx, a, first); e != nil {
		t.Fatal(e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(s)::text FROM sessions s WHERE token_sha256=$1`, a.SessionDigest[:]).Scan(&sessionAfter); e != nil || sessionBefore != sessionAfter {
		t.Fatal("metadata page renewed Session", e)
	}
	next, e := s.ReadOwnRunPage(b.ctx, a, *first.NextCursor)
	if e != nil || len(next.Items) != 3 || next.NextCursor != nil || !next.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatal(next, e)
	}
	seen := map[string]bool{}
	for _, r := range first.Items {
		seen[r.ID] = true
	}
	for _, r := range next.Items {
		if seen[r.ID] {
			t.Fatal("duplicate row")
		}
	}
	if _, e = s.ReadOwnRunPage(b.ctx, f.f.f.place.private.peer, *first.NextCursor); e == nil {
		t.Fatal("cross-owner cursor accepted")
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, a.SessionDigest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadOwnRunPage(b.ctx, a, *first.NextCursor); e == nil {
		t.Fatal("Session ABA revived cursor")
	}
	if _, e = s.ReadOwnRunPage(b.ctx, a, ""); e != nil {
		t.Fatal("new first page rejected", e)
	}
	current, e := s.ReadOwnRunPage(b.ctx, a, "")
	if e != nil {
		t.Fatal(e)
	}
	chosen := current.Items[0]
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, e := s.CancelOwn(b.ctx, a, chosen.ID, chosen.Version); results <- e }()
	}
	good, bad := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			good++
		} else if errors.Is(e, ar.ErrConflict) {
			bad++
		} else {
			t.Fatal(e)
		}
	}
	if good != 1 || bad != 1 {
		t.Fatal("concurrent version cancellation", good, bad)
	}
	if e = s.RevalidateOwnRunPage(b.ctx, a, current); e == nil {
		t.Fatal("old encoded page survived Run version change")
	}
}
func TestAgentRunPageNativeCancelAfterTriggerWaitExpiresSession(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s, a := runFixture(t, false)
	b := f.f.f.place.private.base
	r, e := s.ScheduleOwn(b.ctx, a, ar.Input{MomentID: f.f.moment.ID})
	requireRun(t, r, e, ar.WaitingConfirmation)
	t.Cleanup(func() {
		_, e := b.pool.Exec(context.Background(), `DELETE FROM agent_enrichment_runs WHERE owner_id=$1`, b.person.ID)
		if e != nil {
			t.Error(e)
		}
	})
	var until time.Time
	if e = b.pool.QueryRow(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '4 seconds' WHERE token_sha256=$1 RETURNING idle_expires_at`, a.SessionDigest[:]).Scan(&until); e != nil {
		t.Fatal(e)
	}
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var step string
	if e = blocker.QueryRow(b.ctx, `SELECT step_name FROM agent_run_steps WHERE run_id=$1 FOR UPDATE`, r.ID).Scan(&step); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := s.CancelOwn(b.ctx, a, r.ID, r.Version); done <- e }()
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%UPDATE agent_enrichment_runs r SET state=%CANCELLED%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("cancel finished without AFTER wait", e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("actual cancel checkpoint wait absent")
	}
	time.Sleep(time.Until(until) + 50*time.Millisecond)
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	e = <-done
	if e == nil || !errors.Is(e, ar.ErrDenied) {
		t.Fatal("expired Session committed cancel", e)
	}
	var state string
	var version int64
	if e = b.pool.QueryRow(b.ctx, `SELECT state,version FROM agent_enrichment_runs WHERE id=$1`, r.ID).Scan(&state, &version); e != nil || state != string(ar.WaitingConfirmation) || version != r.Version {
		t.Fatal("late cancel not rolled back", state, version, e)
	}
	t.Cleanup(func() {
		_, e := b.pool.Exec(context.Background(), `DELETE FROM agent_enrichment_runs WHERE owner_id=$1`, b.person.ID)
		if e != nil {
			t.Error(e)
		}
	})
}
