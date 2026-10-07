package agentconfidence_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type confidenceNativeFixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	store    *postgres.Store
	reader   *agentconfidence.HumanReader
	accounts []string
	access   []agentprofile.PrivateAccess
	sessions []string
	agents   []string
	memory   agentmemory.Record
}

func confidenceFixture(t *testing.T) *confidenceNativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("confidence native verification requires disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal("cannot connect confidence scope")
	}
	t.Cleanup(pool.Close)
	var installed bool
	if pool.QueryRow(ctx, `SELECT to_regclass('public.agent_memories') IS NOT NULL AND to_regclass('public.agent_profiles') IS NOT NULL`).Scan(&installed) != nil || !installed {
		t.Fatal("confidence verification requires native056 schema")
	}
	f := &confidenceNativeFixture{ctx: ctx, pool: pool, store: postgres.New(pool, false), reader: agentconfidence.NewHumanReader(pool, false)}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, sql := range []string{
			`DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, err := pool.Exec(cleanupCtx, sql, f.accounts); err != nil {
				t.Errorf("owned confidence cleanup: %v", err)
			}
		}
		var remaining int
		if pool.QueryRow(cleanupCtx, `SELECT (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+(SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[]))+(SELECT count(*) FROM agent_memories WHERE owner_id=ANY($1::uuid[]))+(SELECT count(*) FROM sessions WHERE account_id=ANY($1::uuid[]))`, f.accounts).Scan(&remaining) != nil || remaining != 0 {
			t.Errorf("confidence own fixture residue: %d", remaining)
		}
	})
	for _, kind := range []string{"person", "person", "organization", "business"} {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)VALUES(gen_random_uuid(),$1)RETURNING id`, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.accounts = append(f.accounts, id)
		principal, err := actorref.ParsePrincipal(kind, id)
		if err != nil {
			t.Fatal(err)
		}
		_, digest, err := identity.NewToken()
		if err != nil {
			t.Fatal("cannot create disposable digest")
		}
		var session string
		if err := pool.QueryRow(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')RETURNING id`, id, digest[:]).Scan(&session); err != nil {
			t.Fatal(err)
		}
		f.sessions = append(f.sessions, session)
		f.access = append(f.access, agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal})
		var agent string
		if kind == "person" {
			if err := pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id)VALUES('personal',$1) RETURNING id`, id).Scan(&agent); err != nil {
				t.Fatal("cannot create isolated native personal Agent")
			}
			if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,bio,visibility)VALUES($1,'合成置信度本人','PRIVATE_CONFIDENCE_SOURCE_BODY','private')`, id); err != nil {
				t.Fatal(err)
			}
		}
		f.agents = append(f.agents, agent)
	}
	var memoryID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&memoryID); err != nil {
		t.Fatal(err)
	}
	f.memory, err = f.store.PutOwnMemory(ctx, f.access[0], memoryID, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "confidence-native", Summary: "PRIVATE_CONFIDENCE_MEMORY_CANARY", StructuredValue: json.RawMessage(`{"private":"PRIVATE_CONFIDENCE_VALUE_CANARY"}`), Visibility: agentmemory.VisibilityAgentOnly, ValidUntil: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatalf("native manual Memory save: %v", err)
	}
	return f
}
func confidenceRejected(t *testing.T, v agentconfidence.OwnMemoryView, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || !reflect.DeepEqual(v, agentconfidence.OwnMemoryView{}) {
		t.Fatalf("confidence denied: got=%v want=%v", err, want)
	}
}
func confidenceSQL(t *testing.T, f *confidenceNativeFixture, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}
func confidenceOwnedSnapshot(t *testing.T, f *confidenceNativeFixture) string {
	t.Helper()
	var raw string
	err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'accounts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM accounts a WHERE id=ANY($1::uuid[])),
 'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s WHERE account_id=ANY($1::uuid[])),
 'agents',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agents a WHERE principal_account_id=ANY($1::uuid[])),
 'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_profiles p WHERE owner_id=ANY($1::uuid[])),
 'userProfiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY account_id) FROM user_profiles p WHERE account_id=ANY($1::uuid[])),
 'memory',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agent_memories m WHERE owner_id=ANY($1::uuid[])),
 'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY id) FROM consent_grants g WHERE owner_account_id=ANY($1::uuid[])))::text`, f.accounts).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestConfidenceNativeCurrentOwnerMetadataAndReconnect(t *testing.T) {
	f := confidenceFixture(t)
	before := confidenceOwnedSnapshot(t, f)
	for _, name := range []string{"native", "uppercase_memory_id", "reconnect"} {
		t.Run(name, func(t *testing.T) {
			reader := f.reader
			id := f.memory.ID
			if name == "uppercase_memory_id" {
				id = strings.ToUpper(id)
			}
			if name == "reconnect" {
				pool, err := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
				if err != nil {
					t.Fatal(err)
				}
				defer pool.Close()
				reader = agentconfidence.NewHumanReader(pool, false)
			}
			v, err := reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], id, 1)
			if err != nil || agentconfidence.ValidateOwnMemoryView(v) != nil || v.AgentID != f.agents[0] || v.OwnerID != f.accounts[0] || v.MemoryID != f.memory.ID || v.MemoryVersion != f.memory.Version || v.Assessment.Semantics != agentconfidence.DirectDeclaration || *v.Assessment.Value != 1 {
				t.Fatalf("current exact native confidence: %v", err)
			}
			encoded, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			for _, canary := range []string{"PRIVATE_CONFIDENCE", `"summary"`, `"structuredValue"`, `"evidence"`, `"confidence":true`, `"consent"`} {
				if strings.Contains(string(encoded), canary) {
					t.Fatal("current confidence view copied content/authority")
				}
			}
		})
	}
	if before != confidenceOwnedSnapshot(t, f) {
		t.Fatal("human confidence read changed complete native own source rows")
	}
	for i := 0; i < 8; i++ {
		_, err := f.store.CreateMomentDraft(f.ctx, f.accounts[0], content.MomentInput{CityID: "aberdeen-gb", Title: "合成独立文字草稿", Body: "非媒体或亲历证明", TimePrecision: "unknown", LocationPrecision: "city"})
		if err != nil {
			t.Fatal(err)
		}
	}
	v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], f.memory.ID, 1)
	if err != nil || *v.Assessment.Value != 1 {
		t.Fatal("native text records changed explicit declaration confidence")
	}
}
func TestConfidenceNativeIdentityAndSessionBoundaries(t *testing.T) {
	for _, item := range []struct {
		name   string
		want   error
		change func(*testing.T, *confidenceNativeFixture) *agentprofile.PrivateAccess
	}{
		{"peer", agentconfidence.ErrNotFound, func(_ *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess { return &f.access[1] }},
		{"organization", agentconfidence.ErrForbidden, func(_ *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess { return &f.access[2] }},
		{"business", agentconfidence.ErrForbidden, func(_ *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess { return &f.access[3] }},
		{"revoked", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `UPDATE sessions SET revoked_at=clock_timestamp()WHERE id=$1`, f.sessions[0])
			return &f.access[0]
		}},
		{"expired", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second',idle_expires_at=clock_timestamp()-interval '2 seconds'WHERE id=$1`, f.sessions[0])
			return &f.access[0]
		}},
		{"idle_expired", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',idle_expires_at=clock_timestamp()-interval '1 second'WHERE id=$1`, f.sessions[0])
			return &f.access[0]
		}},
		{"account_suspended", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `UPDATE accounts SET status='suspended'WHERE id=$1`, f.accounts[0])
			return &f.access[0]
		}},
		{"agent_suspended", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `UPDATE agents SET status='suspended'WHERE id=$1`, f.agents[0])
			return &f.access[0]
		}},
		{"agent_retired", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `UPDATE agents SET status='retired'WHERE id=$1`, f.agents[0])
			return &f.access[0]
		}},
		{"agent_deleted", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `DELETE FROM agents WHERE id=$1`, f.agents[0])
			return &f.access[0]
		}},
		{"metadata_missing", agentconfidence.ErrNotFound, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `DELETE FROM agent_profiles WHERE agent_id=$1`, f.agents[0])
			return &f.access[0]
		}},
		{"dev_phone_not_production", agentconfidence.ErrForbidden, func(t *testing.T, f *confidenceNativeFixture) *agentprofile.PrivateAccess {
			confidenceSQL(t, f, `UPDATE sessions SET authentication_method='dev_phone'WHERE id=$1`, f.sessions[0])
			return &f.access[0]
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			f := confidenceFixture(t)
			access := item.change(t, f)
			before := confidenceOwnedSnapshot(t, f)
			v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, *access, f.memory.ID, 1)
			confidenceRejected(t, v, err, item.want)
			if before != confidenceOwnedSnapshot(t, f) {
				t.Fatal("denied confidence read changed source/control rows")
			}
		})
	}
	t.Run("forged_principal_does_not_authorize", func(t *testing.T) {
		f := confidenceFixture(t)
		access := f.access[0]
		access.WorkspacePrincipal = f.access[1].WorkspacePrincipal
		v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, access, f.memory.ID, 1)
		confidenceRejected(t, v, err, agentconfidence.ErrForbidden)
	})
}
func TestConfidenceNativeMemoryLifecycleAndReservedInference(t *testing.T) {
	f := confidenceFixture(t)
	t.Run("wrong_expected", func(t *testing.T) {
		v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], f.memory.ID, 2)
		confidenceRejected(t, v, err, agentconfidence.ErrConflict)
	})
	in := agentmemory.PutInput{ExpectedVersion: 1, MemoryType: agentmemory.TypePreference, MemoryKey: f.memory.MemoryKey, Summary: "合成修改声明", StructuredValue: json.RawMessage(`{}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour)}
	updated, err := f.store.PutOwnMemory(f.ctx, f.access[0], f.memory.ID, in)
	if err != nil || updated.Version != 2 {
		t.Fatal("native Memory CAS failed")
	}
	t.Run("old_version_after_native_update", func(t *testing.T) {
		v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], f.memory.ID, 1)
		confidenceRejected(t, v, err, agentconfidence.ErrConflict)
	})
	v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], f.memory.ID, 2)
	if err != nil || v.MemoryVersion != 2 || *v.Assessment.Value != 1 {
		t.Fatal("reader did not consume current native Memory metadata")
	}
	if _, err := f.store.DeleteOwnMemory(f.ctx, f.access[0], f.memory.ID, 2); err != nil {
		t.Fatal(err)
	}
	t.Run("deleted", func(t *testing.T) {
		v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], f.memory.ID, 3)
		confidenceRejected(t, v, err, agentconfidence.ErrNotFound)
	})
	t.Run("inferred_shape_not_service", func(t *testing.T) {
		var id string
		if err := f.pool.QueryRow(f.ctx, `INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,confidence,source_type,visibility,status,valid_until)VALUES(gen_random_uuid(),$1,$2,'PREFERENCE','reserved-inferred','Synthetic reserved shape','{}',0.86,'INFERRED','PRIVATE','PENDING_REVIEW',clock_timestamp()+interval '1 hour')RETURNING id`, f.agents[0], f.accounts[0]).Scan(&id); err != nil {
			t.Fatal(err)
		}
		v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], id, 1)
		confidenceRejected(t, v, err, agentconfidence.ErrUnavailable)
	})
	t.Run("logical_expiry_without_version_write", func(t *testing.T) {
		var id string
		var deadline time.Time
		if err := f.pool.QueryRow(f.ctx, `INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,visibility,valid_until)VALUES(gen_random_uuid(),$1,$2,'PREFERENCE','expires-soon','Synthetic time shape','{}','PRIVATE',clock_timestamp()+interval '250 milliseconds')RETURNING id,valid_until`, f.agents[0], f.accounts[0]).Scan(&id, &deadline); err != nil {
			t.Fatal(err)
		}
		for {
			var now time.Time
			if f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
				t.Fatal("real clock failed")
			}
			if !now.Before(deadline) {
				break
			}
		}
		before := confidenceOwnedSnapshot(t, f)
		v, err := f.reader.ReadOwnMemoryConfidence(f.ctx, f.access[0], id, 1)
		confidenceRejected(t, v, err, agentconfidence.ErrNotFound)
		if before != confidenceOwnedSnapshot(t, f) {
			t.Fatal("expiry read wrote version/lifecycle")
		}
	})
}

type confidenceQueryGate struct {
	armed            atomic.Bool
	started, release chan struct{}
	match            string
}

func (g *confidenceQueryGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	match := g.match
	if match == "" {
		match = "WITH boundary AS MATERIALIZED"
	}
	if strings.Contains(data.SQL, match) && g.armed.CompareAndSwap(true, false) {
		close(g.started)
		select {
		case <-g.release:
		case <-ctx.Done():
		}
	}
	return ctx
}

func confidenceWaitPast(t *testing.T, f *confidenceNativeFixture, deadline time.Time) {
	t.Helper()
	for {
		var now time.Time
		if err := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal("cannot inspect real PostgreSQL deadline")
		}
		if !now.Before(deadline) {
			return
		}
		// Wait only the measured remaining database interval, with a short cap.
		if _, err := f.pool.Exec(f.ctx, `SELECT pg_sleep(least(0.025,greatest(extract(epoch FROM ($1::timestamptz-clock_timestamp())),0)))`, deadline); err != nil {
			t.Fatal("cannot wait for measured PostgreSQL deadline")
		}
	}
}

func TestConfidenceNativeFinalCurrentBoundaryAfterDelay(t *testing.T) {
	for _, name := range []string{"session_revoke", "session_deadline", "memory_deadline", "memory_cas", "agent_suspended", "metadata_removed", "context_cancelled"} {
		t.Run(name, func(t *testing.T) {
			f := confidenceFixture(t)
			expected := int64(1)
			var deadline time.Time
			if name == "session_deadline" {
				if err := f.pool.QueryRow(f.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1 RETURNING idle_expires_at`, f.sessions[0]).Scan(&deadline); err != nil {
					t.Fatal(err)
				}
			}
			if name == "memory_deadline" {
				updated, err := f.store.PutOwnMemory(f.ctx, f.access[0], f.memory.ID, agentmemory.PutInput{ExpectedVersion: 1, MemoryType: f.memory.MemoryType, MemoryKey: f.memory.MemoryKey, Summary: f.memory.Summary, StructuredValue: f.memory.StructuredValue, Visibility: f.memory.Visibility, ValidUntil: time.Now().UTC().Add(3 * time.Second)})
				if err != nil {
					t.Fatal("cannot create native short validity")
				}
				expected, deadline = updated.Version, updated.ValidUntil
			}
			config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			gate := &confidenceQueryGate{started: make(chan struct{}), release: make(chan struct{}), match: "/* agentconfidence final boundary */"}
			gate.armed.Store(true)
			config.ConnConfig.Tracer = gate
			pool, err := pgxpool.NewWithConfig(f.ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(gate.release) }) }
			defer release()
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			result := make(chan struct {
				view agentconfidence.OwnMemoryView
				err  error
			}, 1)
			go func() {
				view, err := agentconfidence.NewHumanReader(pool, false).ReadOwnMemoryConfidence(ctx, f.access[0], f.memory.ID, expected)
				result <- struct {
					view agentconfidence.OwnMemoryView
					err  error
				}{view, err}
			}()
			select {
			case <-gate.started:
			case <-f.ctx.Done():
				t.Fatal("final current boundary gate was not reached")
			}
			want := agentconfidence.ErrForbidden
			switch name {
			case "session_revoke":
				confidenceSQL(t, f, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.sessions[0])
			case "session_deadline":
				confidenceWaitPast(t, f, deadline)
			case "memory_deadline":
				confidenceWaitPast(t, f, deadline)
				want = agentconfidence.ErrNotFound
			case "memory_cas":
				if _, err := f.store.PutOwnMemory(f.ctx, f.access[0], f.memory.ID, agentmemory.PutInput{ExpectedVersion: 1, MemoryType: f.memory.MemoryType, MemoryKey: f.memory.MemoryKey, Summary: "合成当前版本变更", StructuredValue: json.RawMessage(`{}`), Visibility: f.memory.Visibility, ValidUntil: time.Now().UTC().Add(time.Hour)}); err != nil {
					t.Fatal("native concurrent CAS failed")
				}
				want = agentconfidence.ErrConflict
			case "agent_suspended":
				confidenceSQL(t, f, `UPDATE agents SET status='suspended' WHERE id=$1`, f.agents[0])
			case "metadata_removed":
				confidenceSQL(t, f, `DELETE FROM agent_profiles WHERE agent_id=$1`, f.agents[0])
				want = agentconfidence.ErrNotFound
			case "context_cancelled":
				cancel()
				want = context.Canceled
			}
			before := confidenceOwnedSnapshot(t, f)
			release()
			select {
			case got := <-result:
				confidenceRejected(t, got.view, got.err, want)
			case <-f.ctx.Done():
				t.Fatal("final current boundary read did not complete")
			}
			if before != confidenceOwnedSnapshot(t, f) {
				t.Fatal("delayed confidence read changed native source rows")
			}
		})
	}
}
func (*confidenceQueryGate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestConfidenceNativeLateSessionRevokeBeforeSnapshot(t *testing.T) {
	f := confidenceFixture(t)
	config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	gate := &confidenceQueryGate{started: make(chan struct{}), release: make(chan struct{})}
	gate.armed.Store(true)
	config.ConnConfig.Tracer = gate
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	result := make(chan error, 1)
	go func() {
		v, err := agentconfidence.NewHumanReader(pool, false).ReadOwnMemoryConfidence(f.ctx, f.access[0], f.memory.ID, 1)
		if !reflect.DeepEqual(v, agentconfidence.OwnMemoryView{}) {
			result <- errors.New("late revoked view not empty")
			return
		}
		result <- err
	}()
	select {
	case <-gate.started:
	case <-f.ctx.Done():
		close(gate.release)
		t.Fatal("query boundary gate not reached")
	}
	confidenceSQL(t, f, `UPDATE sessions SET revoked_at=clock_timestamp()WHERE id=$1`, f.sessions[0])
	close(gate.release)
	select {
	case err := <-result:
		if !errors.Is(err, agentconfidence.ErrForbidden) {
			t.Fatalf("late revoke: %v", err)
		}
	case <-f.ctx.Done():
		t.Fatal("late snapshot read did not complete")
	}
}

type confidenceFinalContextKey struct{}
type confidenceCancelAfterQuery struct {
	cancel    context.CancelFunc
	succeeded atomic.Bool
}

func (*confidenceCancelAfterQuery) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, confidenceFinalContextKey{}, strings.Contains(data.SQL, "/* agentconfidence final boundary */"))
}
func (tracer *confidenceCancelAfterQuery) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(confidenceFinalContextKey{}) == true && data.Err == nil && data.CommandTag.RowsAffected() == 1 {
		tracer.succeeded.Store(true)
		tracer.cancel()
	}
}
func TestConfidenceNativeCancelAfterFinalSuccessfulStatement(t *testing.T) {
	f := confidenceFixture(t)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	tracer := &confidenceCancelAfterQuery{cancel: cancel}
	config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	before := confidenceOwnedSnapshot(t, f)
	v, err := agentconfidence.NewHumanReader(pool, false).ReadOwnMemoryConfidence(ctx, f.access[0], f.memory.ID, 1)
	confidenceRejected(t, v, err, context.Canceled)
	if !tracer.succeeded.Load() {
		t.Fatal("cancel-after-return-boundary did not execute a successful actual final statement")
	}
	if before != confidenceOwnedSnapshot(t, f) {
		t.Fatal("cancelled completed statement changed source rows")
	}
}
