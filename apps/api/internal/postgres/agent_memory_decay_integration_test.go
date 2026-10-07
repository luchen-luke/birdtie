package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentdecay"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func decayFixture(t *testing.T) (*agentPrivateFixture, string) {
	t.Helper()
	f := agentMemoryTestFixture(t)
	b := f.base
	mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("decay-explicit-bootstrap"))
	var id string
	// Trusted local fixture only. No inference producer/consent/model is enabled.
	err := b.pool.QueryRow(b.ctx, `INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,confidence,source_type,visibility,status,created_at,updated_at,valid_from,valid_until)
 VALUES(gen_random_uuid(),$1,$2,'PREFERENCE','decay-inferred','PRIVATE_DECAY_SUMMARY','{"private":"PRIVATE_DECAY_VALUE"}',0.8,'INFERRED','AGENT_ONLY','PENDING_REVIEW',clock_timestamp()-interval '120 days',clock_timestamp()-interval '1 day',clock_timestamp()-interval '180 days',clock_timestamp()+interval '30 days') RETURNING id`, b.personID, b.person.ID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return f, id
}
func decaySnapshot(t *testing.T, f *agentPrivateFixture) string {
	t.Helper()
	var rows string
	if e := f.base.pool.QueryRow(f.base.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]')::text FROM agent_memories m WHERE owner_id=ANY($1::uuid[])`, f.base.accounts).Scan(&rows); e != nil {
		t.Fatal(e)
	}
	return agentMemoryOwnedSourceSnapshot(t, f) + rows
}
func decayDenied(t *testing.T, v agentdecay.View, e, want error) {
	t.Helper()
	if !errors.Is(e, want) || !reflect.DeepEqual(v, agentdecay.View{}) {
		t.Fatalf("decay denied got=%v want=%v payload=%+v", e, want, v)
	}
}
func TestDecayNativeBaselineRealAnchorReconnectAndNoWrites(t *testing.T) {
	f, id := decayFixture(t)
	b := f.base
	before := decaySnapshot(t, f)
	first, e := b.store.ReadOwnMemoryDecay(b.ctx, f.owner, id, 1)
	if e != nil || first.MemoryID != id || first.MemoryVersion != 1 || first.SourceType != agentmemory.SourceInferred || first.Status != agentmemory.StatusPendingReview || first.Assessment.Semantics != agentconfidence.UncalibratedScore || *first.Assessment.Value >= .4 || *first.Assessment.Value < .399 || !first.Decayed || first.ModelAccess != "UNAVAILABLE" {
		t.Fatalf("native decay %+v %v", first, e)
	}
	again, e := b.store.ReadOwnMemoryDecay(b.ctx, f.owner, id, 1)
	if e != nil || *again.Assessment.Value > *first.Assessment.Value || *again.Assessment.Value < .399 {
		t.Fatal("read compounded or increased confidence")
	}
	config, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `SET TIME ZONE 'Pacific/Honolulu'; SET default_transaction_isolation='repeatable read'`)
		return err
	}
	pool, e := pgxpool.NewWithConfig(b.ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	reconnected, e := New(pool, false).ReadOwnMemoryDecay(b.ctx, f.owner, id, 1)
	if e != nil || reconnected.MemoryVersion != 1 || reconnected.AgentID != b.personID || *reconnected.Assessment.Value < .399 {
		t.Fatal("reconnect lost baseline/native identity")
	}
	if reconnected.ReadAt.Location() != time.UTC || reconnected.AnchorAt.Location() != time.UTC {
		t.Fatal("native timezone changed UTC projection")
	}
	raw, e := json.Marshal(first)
	if e != nil || strings.Contains(string(raw), "PRIVATE_DECAY") || strings.Contains(string(raw), "summary") || strings.Contains(string(raw), "structuredValue") {
		t.Fatal("metadata leaked native private content")
	}
	if before != decaySnapshot(t, f) {
		t.Fatal("read mutated native Memory/Profile/session/domain fields")
	}
	b.exec(`UPDATE agent_memories SET updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, id)
	updated, e := b.store.ReadOwnMemoryDecay(b.ctx, f.owner, id, 2)
	if e != nil || !updated.AnchorAt.Equal(first.AnchorAt) || *updated.Assessment.Value >= .4 {
		t.Fatal("updated_at invented reinforcement")
	}
	b.exec(`UPDATE agent_memories SET last_reinforced_at=clock_timestamp()-interval '20 days',version=version+1 WHERE id=$1`, id)
	reinforcedBefore := decaySnapshot(t, f)
	reinforced, e := b.store.ReadOwnMemoryDecay(b.ctx, f.owner, id, 3)
	if e != nil || reinforced.Decayed || *reinforced.Assessment.Value != .8 || reinforced.Status != agentmemory.StatusPendingReview {
		t.Fatal("real persisted last_reinforced_at ignored")
	}
	if reinforcedBefore != decaySnapshot(t, f) {
		t.Fatal("projection rewrote real reinforcement")
	}
	v, e := (agentcognitive.UnavailableCognitivePorts{}).ReadMemory(b.ctx, agentcognitive.ReadRequest{})
	if !errors.Is(e, agentcognitive.ErrUnavailable) || !reflect.ValueOf(v).IsZero() {
		t.Fatal("projection enabled model Memory read")
	}
}
func TestDecayNativeOldExplicitAndInvalidAccess(t *testing.T) {
	f, id := decayFixture(t)
	b := f.base
	var explicit string
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,visibility,created_at,updated_at,valid_from,valid_until)
 VALUES(gen_random_uuid(),$1,$2,'PREFERENCE','decay-old-explicit','Synthetic explicit','{}','PRIVATE',clock_timestamp()-interval '120 days',clock_timestamp(),clock_timestamp()-interval '180 days',clock_timestamp()+interval '30 days') RETURNING id`, b.personID, b.person.ID).Scan(&explicit); e != nil {
		t.Fatal(e)
	}
	before := decaySnapshot(t, f)
	v, e := b.store.ReadOwnMemoryDecay(b.ctx, f.owner, explicit, 1)
	if e != nil || v.Decayed || v.Assessment.Semantics != agentconfidence.DirectDeclaration || *v.Assessment.Value != 1 || v.ReadAt.Sub(v.AnchorAt) < 119*24*time.Hour {
		t.Fatal("old explicit declaration decayed")
	}
	for _, tc := range []struct {
		name   string
		access agentprofile.PrivateAccess
		want   error
	}{{"peer", f.peer, agentdecay.ErrNotFound}, {"organization", f.org, agentdecay.ErrForbidden}, {"business", f.biz, agentdecay.ErrForbidden}, {"anonymous", agentprofile.PrivateAccess{}, agentdecay.ErrForbidden}, {"forged_owner", agentprofile.PrivateAccess{SessionDigest: f.peer.SessionDigest, WorkspacePrincipal: f.owner.WorkspacePrincipal}, agentdecay.ErrForbidden}} {
		t.Run(tc.name, func(t *testing.T) {
			v, e := b.store.ReadOwnMemoryDecay(b.ctx, tc.access, id, 1)
			decayDenied(t, v, e, tc.want)
		})
	}
	v, e = b.store.ReadOwnMemoryDecay(b.ctx, f.owner, id, 2)
	decayDenied(t, v, e, agentdecay.ErrConflict)
	v, e = b.store.ReadOwnMemoryDecay(b.ctx, f.owner, "invalid", 1)
	decayDenied(t, v, e, agentdecay.ErrInvalid)
	if before != decaySnapshot(t, f) {
		t.Fatal("invalid reads changed original data")
	}
}
func TestDecayNativeExpiredDeletedAndFutureRejected(t *testing.T) {
	for _, name := range []string{"expired", "deleted", "future", "revoked_session", "suspended_person", "suspended_agent", "missing_metadata", "dev_phone", "context_cancelled"} {
		t.Run(name, func(t *testing.T) {
			f, id := decayFixture(t)
			b := f.base
			version := int64(1)
			want := agentdecay.ErrNotFound
			ctx := b.ctx
			switch name {
			case "expired":
				b.exec(`UPDATE agent_memories SET status='EXPIRED',version=version+1 WHERE id=$1`, id)
				version = 2
			case "deleted":
				b.exec(`UPDATE agent_memories SET status='DELETED',summary='',structured_value='{}',last_reinforced_at=NULL,version=version+1 WHERE id=$1`, id)
				version = 2
			case "future":
				b.exec(`UPDATE agent_memories SET valid_from=clock_timestamp()+interval '1 day',version=version+1 WHERE id=$1`, id)
				version = 2
			case "revoked_session":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession)
				want = agentdecay.ErrForbidden
			case "suspended_person":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				want = agentdecay.ErrForbidden
			case "suspended_agent":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
				want = agentdecay.ErrForbidden
			case "missing_metadata":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			case "dev_phone":
				b.exec(`UPDATE sessions SET authentication_method='dev_phone' WHERE id=$1`, f.ownerSession)
				want = agentdecay.ErrForbidden
			case "context_cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			}
			before := decaySnapshot(t, f)
			v, e := b.store.ReadOwnMemoryDecay(ctx, f.owner, id, version)
			decayDenied(t, v, e, want)
			if before != decaySnapshot(t, f) {
				t.Fatal("rejected read changed rows")
			}
		})
	}
}

type decayFinalGate struct {
	armed            atomic.Bool
	started, release chan struct{}
}

func (g *decayFinalGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "/* agentdecay final boundary */") && g.armed.CompareAndSwap(true, false) {
		close(g.started)
		select {
		case <-g.release:
		case <-ctx.Done():
		}
	}
	return ctx
}
func (*decayFinalGate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestDecayNativeFinalCurrentBoundary(t *testing.T) {
	for _, name := range []string{"session_revoke", "session_expiry", "natural_session_deadline", "natural_memory_deadline", "memory_cas", "memory_deleted", "agent_suspended", "metadata_removed", "context_cancelled"} {
		t.Run(name, func(t *testing.T) {
			f, id := decayFixture(t)
			b := f.base
			config, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			expectedVersion := int64(1)
			var deadline time.Time
			if name == "natural_session_deadline" {
				if e := b.pool.QueryRow(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING idle_expires_at`, f.ownerSession).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			if name == "natural_memory_deadline" {
				if e := b.pool.QueryRow(b.ctx, `UPDATE agent_memories SET valid_until=clock_timestamp()+interval '1 second',version=version+1 WHERE id=$1 RETURNING valid_until`, id).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
				expectedVersion = 2
			}
			if e != nil {
				t.Fatal(e)
			}
			gate := &decayFinalGate{started: make(chan struct{}), release: make(chan struct{})}
			gate.armed.Store(true)
			config.ConnConfig.Tracer = gate
			pool, e := pgxpool.NewWithConfig(b.ctx, config)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			var once sync.Once
			release := func() { once.Do(func() { close(gate.release) }) }
			defer release()
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			type result struct {
				v agentdecay.View
				e error
			}
			done := make(chan result, 1)
			go func() {
				v, e := New(pool, false).ReadOwnMemoryDecay(ctx, f.owner, id, expectedVersion)
				done <- result{v, e}
			}()
			select {
			case <-gate.started:
			case <-b.ctx.Done():
				t.Fatal("final boundary not reached")
			}
			want := agentdecay.ErrForbidden
			switch name {
			case "natural_session_deadline", "natural_memory_deadline":
				for {
					var now time.Time
					if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
						t.Fatal(e)
					}
					if !now.Before(deadline) {
						break
					}
					if _, e := b.pool.Exec(b.ctx, `SELECT pg_sleep(least(0.025,greatest(extract(epoch FROM ($1::timestamptz-clock_timestamp())),0)))`, deadline); e != nil {
						t.Fatal(e)
					}
				}
				if name == "natural_memory_deadline" {
					want = agentdecay.ErrNotFound
				}
			case "session_revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession)
			case "session_expiry":
				b.exec(`UPDATE sessions SET idle_expires_at=created_at+interval '1 microsecond' WHERE id=$1`, f.ownerSession)
			case "memory_cas":
				b.exec(`UPDATE agent_memories SET confidence=0.3,version=version+1 WHERE id=$1`, id)
				want = agentdecay.ErrConflict
			case "memory_deleted":
				b.exec(`UPDATE agent_memories SET status='DELETED',summary='',structured_value='{}',last_reinforced_at=NULL,version=version+1 WHERE id=$1`, id)
				want = agentdecay.ErrConflict
			case "agent_suspended":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
			case "metadata_removed":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
				want = agentdecay.ErrNotFound
			case "context_cancelled":
				cancel()
				want = context.Canceled
			}
			before := decaySnapshot(t, f)
			release()
			select {
			case got := <-done:
				decayDenied(t, got.v, got.e, want)
			case <-b.ctx.Done():
				t.Fatal("read did not return")
			}
			if before != decaySnapshot(t, f) {
				t.Fatal("late response wrote native rows")
			}
		})
	}
}
