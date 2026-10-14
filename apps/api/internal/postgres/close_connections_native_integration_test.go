package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

type closePGFixture struct {
	t                     *testing.T
	ctx                   context.Context
	pool                  *pgxpool.Pool
	store                 *Store
	owner, peer, outsider ea.Access
	tie, request          string
	accountIDs            []string
}

// Hash real rows, including native xmin, rather than reporting synthetic
// equality of counters. No seed or private contents are emitted in evidence.
func closeNativeOlderRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, e := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename<>'person_close_connections' ORDER BY tablename`)
	if e != nil {
		t.Fatal(e)
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			t.Fatal(e)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	out := map[string]string{}
	for _, name := range tables {
		var raw string
		e = pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(v ORDER BY v::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(r)||jsonb_build_object('xmin',r.xmin::text) v FROM `+pgx.Identifier{"public", name}.Sanitize()+` r) q`).Scan(&raw)
		if e != nil {
			t.Fatal("older row snapshot", name, e)
		}
		h := sha256.Sum256([]byte(raw))
		out[name] = hex.EncodeToString(h[:])
	}
	return out
}
func closeNativeOlderFunctions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var raw string
	e := pool.QueryRow(ctx, `SELECT COALESCE(string_agg(pg_get_functiondef(p.oid),E'\n' ORDER BY p.oid),'') FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind='f' AND p.proname NOT IN('birdtie_connection_visible','birdtie_close_connection_visible','birdtie_guard_close_connection','birdtie_retire_close_pair','birdtie_retire_close_tie_source','birdtie_retire_close_request_source','birdtie_retire_close_account_source','birdtie_retire_close_block_source')`).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
func closeNativeMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, refusal bool) {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if e != nil {
		t.Fatal(e)
	}
	c, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Release()
	_, outcome := c.Exec(ctx, string(raw))
	if outcome != nil {
		if _, e = c.Exec(ctx, `ROLLBACK`); e != nil {
			t.Fatal("migration same-connection rollback", e)
		}
	}
	if !refusal && outcome != nil {
		t.Fatal("actual owned original migration", name, outcome)
	}
	if refusal {
		var pe *pgconn.PgError
		if !errors.As(outcome, &pe) || pe.Code != "23514" || !strings.Contains(pe.Message, "118 down refused: Close choices or retired source fences remain") {
			t.Fatal("118 used history requires its exact refusal", outcome)
		}
	}
}
func TestCloseNative118EmptyRoundtripAndUsedDownRetention(t *testing.T) {
	parent := closePG(t) // Strict exact owned parent/loopback guard first.
	parentBefore := closeNativeOlderRows(t, parent.ctx, parent.pool)
	var name, owner, marker string
	var oid uint32
	// After the existing helper's exact successful-create-only child DROP.
	t.Cleanup(func() {
		if name == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var remaining int
		if e := parent.pool.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname=$1 OR oid=$2`, name, oid).Scan(&remaining); e != nil || remaining != 0 {
			t.Error("owned Close child not removed", name, oid, remaining, e)
		}
		preserved := reflect.DeepEqual(parentBefore, closeNativeOlderRows(t, ctx, parent.pool))
		if !preserved {
			t.Error("independent118 migration changed older parent rows/xmin")
		}
		t.Logf("OWNED_118_CHILD_CLEANUP name=%s oid=%d owner=%s marker=%s remaining=%d parent_rows_preserved=%t", name, oid, owner, marker, remaining, preserved)
	})
	ownedMigrationDatabase(t) // Unmodified helper, actual001..119, fresh child.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	var role string
	if e = pool.QueryRow(ctx, `SELECT current_database(),d.oid,pg_get_userbyid(d.datdba),current_user FROM pg_database d WHERE d.datname=current_database()`).Scan(&name, &oid, &owner, &role); e != nil || !regexp.MustCompile(`^birdtie_owned_migration_[0-9a-f]{24}$`).MatchString(name) || name == os.Getenv("BIRDTIE_VISIBILITY_DATABASE") || owner != role {
		t.Fatal("independent Close child ownership", name, oid, owner, e)
	}
	if e = pool.QueryRow(ctx, `SELECT 'SAF002-owned-close-'||gen_random_uuid()::text`).Scan(&marker); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `COMMENT ON DATABASE `+pgx.Identifier{name}.Sanitize()+` IS '`+marker+`'`); e != nil {
		t.Fatal(e)
	}
	// Check exact identity immediately before the original helper's cleanup.
	// No schema override or DROP targets a shared/phone/previous database.
	t.Cleanup(func() {
		var actualOwner, actualMarker string
		if e := parent.pool.QueryRow(context.Background(), `SELECT pg_get_userbyid(datdba),shobj_description(oid,'pg_database') FROM pg_database WHERE datname=$1 AND oid=$2`, name, oid).Scan(&actualOwner, &actualMarker); e != nil || actualOwner != owner || actualMarker != marker {
			t.Error("owned Close child exact pre-drop identity mismatch", e)
		}
	})
	t.Logf("OWNED_118_CHILD name=%s oid=%d owner=%s marker=%s parent=%s", name, oid, owner, marker, os.Getenv("BIRDTIE_VISIBILITY_DATABASE"))
	// Only the independently created child is downgraded. Remove dependent119
	// first; its seeded older audiences require no coercion or deletion.
	closeNativeMigration(t, ctx, pool, "119_visibility_matrix.down.sql", false)
	olderRows, olderFunctions := closeNativeOlderRows(t, ctx, pool), closeNativeOlderFunctions(t, ctx, pool)
	closeNativeMigration(t, ctx, pool, "118_close_connections.down.sql", false)
	var absent bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('public.person_close_connections') IS NULL AND to_regprocedure('public.birdtie_connection_visible(uuid,uuid)') IS NULL`).Scan(&absent); e != nil || !absent || !reflect.DeepEqual(olderRows, closeNativeOlderRows(t, ctx, pool)) || olderFunctions != closeNativeOlderFunctions(t, ctx, pool) {
		t.Fatal("empty118 down changed older rows/xmin/functions", e)
	}
	closeNativeMigration(t, ctx, pool, "118_close_connections.sql", false)
	if !reflect.DeepEqual(olderRows, closeNativeOlderRows(t, ctx, pool)) || olderFunctions != closeNativeOlderFunctions(t, ctx, pool) {
		t.Fatal("118 reapply changed older rows/xmin/functions")
	}
	f := &closePGFixture{t: t, ctx: ctx, pool: pool, store: New(pool, false)}
	t.Cleanup(f.cleanup)
	f.owner, f.peer, f.outsider = f.person(), f.person(), f.person()
	f.friend()
	selected := f.choose() // Formal native writer,118-only generic name branch.
	if selected.Connection.DisplayName != "Birdtie 成员" {
		t.Fatal("118-only must not manufacture missing119 name ACL")
	}
	for _, state := range []string{"active", "retired"} {
		t.Run(state+"-down-refused", func(t *testing.T) {
			if state == "retired" {
				_, e := f.store.DeleteOwnCloseConnectionCurrent(ctx, f.owner, f.peer.Actor.ID, connection.CloseDelete{TieID: f.tie, RequestID: f.request, ExpectedRevision: selected.Connection.NativeRevision, Confirmed: true})
				if e != nil {
					t.Fatal(e)
				}
			}
			before := f.snapshot()
			olderBefore := closeNativeOlderRows(t, ctx, pool)
			closeNativeMigration(t, ctx, pool, "118_close_connections.down.sql", true)
			if before != f.snapshot() || !reflect.DeepEqual(olderBefore, closeNativeOlderRows(t, ctx, pool)) || olderFunctions != closeNativeOlderFunctions(t, ctx, pool) {
				t.Fatal("refused118 down changed immutable native history/older functions")
			}
		})
	}
}

func closePG(t *testing.T) *closePGFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_VISIBILITY_DATABASE") == "" {
		t.Skip("requires explicitly owned SAF002 visibility runner")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_VISIBILITY_DATABASE") || !regexp.MustCompile(`^birdtie_saf002_visibility_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
		t.Fatal("refuse unowned/remote Close database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	var db string
	var ready bool
	if e = pool.QueryRow(ctx, `SELECT current_database(),to_regclass('public.person_close_connections') IS NOT NULL AND to_regprocedure('public.birdtie_profile_visible_to(uuid,uuid)') IS NOT NULL`).Scan(&db, &ready); e != nil || db != cfg.ConnConfig.Database || !ready {
		t.Fatal("owned actual118+119 required", e)
	}
	f := &closePGFixture{t: t, ctx: ctx, pool: pool, store: New(pool, false)}
	// Register before the first insert and append each parent immediately. A
	// later seed failure must not leave an untracked synthetic identity.
	t.Cleanup(f.cleanup)
	f.owner = f.person()
	f.peer = f.person()
	f.outsider = f.person()
	f.friend()
	return f
}
func (f *closePGFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, e := f.pool.Exec(f.ctx, sql, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f *closePGFixture) person() ea.Access {
	f.t.Helper()
	var id, agent string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&id); e != nil {
		f.t.Fatal(e)
	}
	f.accountIDs = append(f.accountIDs, id)
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) RETURNING id`, id).Scan(&agent); e != nil {
		f.t.Fatal(e)
	}
	f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility) VALUES($1,'Close隔离合成好友','PRIVATE_CLOSE_BIO_CANARY','public')`, id)
	_, digest, e := identity.NewToken()
	if e != nil {
		f.t.Fatal(e)
	}
	f.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() AS now) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) SELECT $1,$2,'test',n.now+interval '3 hours',n.now+interval '1 hour' FROM n`, id, digest[:])
	return ea.Access{Actor: identity.Actor{ID: id, AccountType: "person"}, SessionDigest: digest}
}
func (f *closePGFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if len(f.accountIDs) == 0 {
		return
	}
	// Only parents created by this fixture are removed. Tie deletion is the
	// native FK cascade that may erase Close fences; direct fence deletion is
	// deliberately forbidden. Original Friend acceptance creates no chat.
	for i, sql := range []string{
		`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
		`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
		`DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
		`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
		`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
		`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
		`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
	} {
		if _, e := f.pool.Exec(ctx, sql, f.accountIDs); e != nil {
			f.t.Errorf("owned Close fixture cleanup step %d failed: %v", i, e)
		}
	}
	var residue int
	e := f.pool.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM user_profiles WHERE account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM sessions WHERE account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM person_close_connections WHERE owner_account_id=ANY($1::uuid[]) OR person_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM follows WHERE follower_account_id=ANY($1::uuid[]) OR person_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM audit_events WHERE actor_account_id=ANY($1::uuid[]))+
	 (SELECT count(*) FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[]))`, f.accountIDs).Scan(&residue)
	if e != nil || residue != 0 {
		f.t.Errorf("owned Close fixture residue=%d: %v", residue, e)
	} else {
		f.t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY owned Close fixture residue=0")
	}
}
func (f *closePGFixture) friend() {
	f.t.Helper()
	r, e := f.store.CreateFriendRequestCurrent(f.ctx, f.owner, f.peer.Actor.ID, "明确好友夹具", "", nil)
	if e != nil {
		f.t.Fatal("original request", e)
	}
	if _, e = f.store.DecideRequestCurrent(f.ctx, f.peer, r.ID, "accept"); e != nil {
		f.t.Fatal("original accept", e)
	}
	f.request = r.ID
	if e = f.pool.QueryRow(f.ctx, `SELECT id FROM person_ties WHERE request_id=$1`, r.ID).Scan(&f.tie); e != nil {
		f.t.Fatal(e)
	}
}
func (f *closePGFixture) view() connection.CloseView {
	f.t.Helper()
	v, e := f.store.ListOwnCloseConnectionsCurrent(f.ctx, f.owner)
	if e != nil || len(v.Connections) != 1 {
		f.t.Fatal("original native current eligible Friend", v, e)
	}
	return v
}
func (f *closePGFixture) input(v connection.CloseView) connection.ClosePut {
	p := v.Connections[0]
	return connection.ClosePut{TieID: p.TieID, RequestID: p.RequestID, SourceVersion: p.SourceVersion, ValidUntil: v.ValidUntil, Confirmed: true}
}
func (f *closePGFixture) choose() connection.CloseResult {
	f.t.Helper()
	v, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, f.input(f.view()))
	if e != nil || !v.Connection.IsClose {
		f.t.Fatal("native confirmed Close", v, e)
	}
	return v
}
func (f *closePGFixture) visible(owner, viewer string, close bool) bool {
	f.t.Helper()
	fn := "birdtie_connection_visible"
	if close {
		fn = "birdtie_close_connection_visible"
	}
	var v bool
	if e := f.pool.QueryRow(f.ctx, `SELECT `+fn+`($1::uuid,$2::uuid)`, owner, viewer).Scan(&v); e != nil {
		f.t.Fatal(e)
	}
	return v
}
func (f *closePGFixture) snapshot() string {
	f.t.Helper()
	var v string
	if e := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object('close',COALESCE((SELECT jsonb_agg(to_jsonb(c)||jsonb_build_object('xmin',c.xmin::text) ORDER BY c.person_account_id) FROM person_close_connections c WHERE c.owner_account_id=$1),'[]'::jsonb),'audit',COALESCE((SELECT jsonb_agg(to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text) ORDER BY a.id) FROM audit_events a WHERE a.actor_account_id=$1 AND a.purpose='human_close'),'[]'::jsonb))::text`, f.owner.Actor.ID).Scan(&v); e != nil {
		f.t.Fatal(e)
	}
	return v
}
func TestCloseNativeDirectionalSelectionRevokeAndPersistence(t *testing.T) {
	f := closePG(t)
	v := f.view()
	if v.Connections[0].IsClose || !f.visible(f.owner.Actor.ID, f.peer.Actor.ID, false) || f.visible(f.owner.Actor.ID, f.outsider.Actor.ID, false) || f.visible(f.owner.Actor.ID, f.owner.Actor.ID, false) {
		t.Fatal("Friend fact is neither stranger nor Close")
	}
	result := f.choose()
	if !f.visible(f.owner.Actor.ID, f.peer.Actor.ID, true) || f.visible(f.peer.Actor.ID, f.owner.Actor.ID, true) || f.store.ValidateCloseConnectionResponse(f.ctx, f.owner, f.peer.Actor.ID, result) != nil {
		t.Fatal("explicit direction and current native response")
	}
	fresh := New(f.pool, false)
	view, e := fresh.ListOwnCloseConnectionsCurrent(f.ctx, f.owner)
	if e != nil || !view.Connections[0].IsClose || fresh.ValidateCloseConnectionsResponse(f.ctx, f.owner, view) != nil {
		t.Fatal("persisted native data after Store restart", e)
	}
	in := connection.CloseDelete{TieID: result.Connection.TieID, RequestID: result.Connection.RequestID, ExpectedRevision: result.Connection.NativeRevision, Confirmed: true}
	r, e := f.store.DeleteOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, in)
	if e != nil || f.store.ValidateCloseDeleteResponse(f.ctx, f.owner, r) != nil || f.visible(f.owner.Actor.ID, f.peer.Actor.ID, true) {
		t.Fatal("owned revoke and private native receipt", e)
	}
	before := f.snapshot()
	if _, e = f.store.DeleteOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, in); e != nil || before != f.snapshot() {
		t.Fatal("same retired choice is idempotent and immutable", e)
	}
	next := f.choose()
	before = f.snapshot()
	if _, e = f.store.DeleteOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, in); !errors.Is(e, connection.ErrCloseChanged) || f.snapshot() != before || next.Connection.NativeRevision <= in.ExpectedRevision {
		t.Fatal("late old revocation must not clear new choice", e)
	}
}
func TestCloseNativeStrictCurrentProofAndCAS(t *testing.T) {
	f := closePG(t)
	v := f.view()
	input := f.input(v)
	t.Run("deadline-bound", func(t *testing.T) {
		bad := input
		bad.ValidUntil = bad.ValidUntil.Add(time.Microsecond)
		before := f.snapshot()
		if _, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, bad); !errors.Is(e, connection.ErrCloseChanged) || f.snapshot() != before {
			t.Fatal("deadline cannot extend original proposal", e)
		}
	})
	t.Run("unconfirmed", func(t *testing.T) {
		bad := input
		bad.Confirmed = false
		if _, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, bad); !errors.Is(e, connection.ErrCloseInvalid) {
			t.Fatal(e)
		}
	})
	t.Run("two-concurrent-original-sources", func(t *testing.T) {
		start := make(chan struct{})
		out := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				_, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, input)
				out <- e
			}()
		}
		close(start)
		wins, conflicts := 0, 0
		for i := 0; i < 2; i++ {
			select {
			case e := <-out:
				if e == nil {
					wins++
				} else if errors.Is(e, connection.ErrCloseChanged) {
					conflicts++
				} else {
					t.Fatal(e)
				}
			case <-f.ctx.Done():
				t.Fatal("native CAS timed out")
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatal("exactly one current source wins", wins, conflicts)
		}
	})
	current := f.view()
	t.Run("decoded-wire-has-no-proof", func(t *testing.T) {
		raw, _ := json.Marshal(current)
		var decoded connection.CloseView
		json.Unmarshal(raw, &decoded)
		if !errors.Is(f.store.ValidateCloseConnectionsResponse(f.ctx, f.owner, decoded), connection.ErrCloseUnavailable) {
			t.Fatal("DTO is not native authority")
		}
	})
	t.Run("proof-other-store", func(t *testing.T) {
		if !errors.Is(New(f.pool, false).ValidateCloseConnectionsResponse(f.ctx, f.owner, current), connection.ErrCloseUnavailable) {
			t.Fatal("proof must originate in this Store")
		}
	})
	t.Run("tampered-wire", func(t *testing.T) {
		bad := current
		bad.Connections = append([]connection.CloseEntry{}, current.Connections...)
		bad.Connections[0].DisplayName = "伪造姓名"
		if !errors.Is(f.store.ValidateCloseConnectionsResponse(f.ctx, f.owner, bad), connection.ErrCloseUnavailable) {
			t.Fatal("native proof binds original wire")
		}
	})
	t.Run("foreign-digest", func(t *testing.T) {
		a := f.owner
		a.SessionDigest = f.outsider.SessionDigest
		if _, e := f.store.ListOwnCloseConnectionsCurrent(f.ctx, a); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("owner path cannot replace own Session", e)
		}
	})
}
func TestCloseNativeSourceRetirementAndABA(t *testing.T) {
	for _, kind := range []string{"remove-reaccept", "block-unblock", "reverse-block-unblock", "native-block-before-ever-selected", "account-restored", "request-restored", "session-revoke-restored", "idle-live-renewal", "profile-name-changed"} {
		t.Run(kind, func(t *testing.T) {
			f := closePG(t)
			v := f.view()
			if kind != "native-block-before-ever-selected" && kind != "session-revoke-restored" && kind != "idle-live-renewal" && kind != "profile-name-changed" {
				f.choose()
				v = f.view()
			}
			input := f.input(v)
			oldTie := f.tie
			switch kind {
			case "remove-reaccept":
				if e := f.store.RemoveTie(f.ctx, f.owner.Actor.ID, f.tie); e != nil {
					t.Fatal(e)
				}
				f.friend()
				if f.tie != oldTie || f.request == input.RequestID {
					t.Fatal("original reaccept must reuse Tie and change request")
				}
			case "block-unblock":
				if e := f.store.BlockAccount(f.ctx, f.owner.Actor.ID, f.peer.Actor.ID); e != nil {
					t.Fatal(e)
				}
				if e := f.store.UnblockAccount(f.ctx, f.owner.Actor.ID, f.peer.Actor.ID); e != nil {
					t.Fatal(e)
				}
				f.friend()
			case "reverse-block-unblock":
				if e := f.store.BlockAccount(f.ctx, f.peer.Actor.ID, f.owner.Actor.ID); e != nil {
					t.Fatal(e)
				}
				if e := f.store.UnblockAccount(f.ctx, f.peer.Actor.ID, f.owner.Actor.ID); e != nil {
					t.Fatal(e)
				}
				f.friend()
			case "native-block-before-ever-selected":
				f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.peer.Actor.ID, f.owner.Actor.ID)
				f.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.peer.Actor.ID, f.owner.Actor.ID)
			case "account-restored":
				f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.peer.Actor.ID)
				f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.peer.Actor.ID)
			case "request-restored":
				f.exec(`UPDATE connection_requests SET state='declined' WHERE id=$1`, f.request)
				f.exec(`UPDATE connection_requests SET state='accepted' WHERE id=$1`, f.request)
			case "session-revoke-restored":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.owner.SessionDigest[:])
				f.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, f.owner.SessionDigest[:])
			case "idle-live-renewal":
				f.exec(`UPDATE sessions SET idle_expires_at=idle_expires_at+interval '1 hour' WHERE token_sha256=$1`, f.owner.SessionDigest[:])
			case "profile-name-changed":
				f.exec(`UPDATE user_profiles SET display_name='新的当前姓名' WHERE account_id=$1`, f.peer.Actor.ID)
			}
			before := f.snapshot()
			_, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, input)
			if kind == "idle-live-renewal" {
				if e != nil {
					t.Fatal("still-live original idle renewal does not change authorization epoch", e)
				}
				return
			}
			if !errors.Is(e, connection.ErrCloseChanged) && !errors.Is(e, connection.ErrNotFound) {
				t.Fatal("old reviewed source must never revive after native boundary", e)
			}
			if before != f.snapshot() {
				t.Fatal("stale source changed Close or audit")
			}
			if kind != "session-revoke-restored" && kind != "profile-name-changed" && f.visible(f.owner.Actor.ID, f.peer.Actor.ID, true) {
				t.Fatal("old selection revived")
			}
			if e = f.store.ValidateCloseConnectionsResponse(f.ctx, f.owner, v); e == nil {
				t.Fatal("old native response proof remained current")
			}
		})
	}
}
func TestCloseNativeAuditRollbackAndFinalClock(t *testing.T) {
	for _, mode := range []string{"fault", "expiry-after-audit-wait"} {
		t.Run(mode, func(t *testing.T) {
			f := closePG(t)
			suffix := strings.ReplaceAll(f.owner.Actor.ID, "-", "")
			fn := "close_audit_" + suffix
			trigger := "close_audit_tr_" + suffix
			if mode == "expiry-after-audit-wait" {
				f.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp()+interval '350 milliseconds' AS until) UPDATE sessions SET expires_at=n.until,idle_expires_at=n.until FROM n WHERE token_sha256=$1`, f.owner.SessionDigest[:])
			}
			body := "RAISE EXCEPTION 'owned Close audit fault' USING ERRCODE='P0001';"
			if mode == "expiry-after-audit-wait" {
				body = "PERFORM pg_sleep(0.45);"
			}
			t.Cleanup(func() {
				_, e := f.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+trigger+` ON audit_events;DROP FUNCTION IF EXISTS `+fn+`() `)
				if e != nil {
					t.Error("owned audit guard cleanup", e)
				}
			})
			f.exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.actor_account_id='%s'::uuid AND NEW.purpose='human_close' THEN %s END IF;RETURN NEW;END $$`, fn, f.owner.Actor.ID, body))
			f.exec(`CREATE TRIGGER ` + trigger + ` BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION ` + fn + `()`)
			in := f.input(f.view())
			before := f.snapshot()
			_, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, in)
			if e == nil || f.snapshot() != before {
				t.Fatal("audit fault/current clock failure must rollback actual choice and audit", e)
			}
		})
	}
}
func TestCloseNativeNameGrantDeadline(t *testing.T) {
	f := closePG(t)
	f.exec(`UPDATE user_profiles SET visibility='private',display_name='仅限当前本人授权姓名' WHERE account_id=$1`, f.peer.Actor.ID)
	// Original specific Profile grant is only a name resource permission. It
	// is neither Close nor model access, and its expiry changes no source row.
	grant, e := f.store.GrantProfileRead(f.ctx, f.peer.Actor.ID, f.owner.Actor.ID, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	// The original writer requires >=1 minute. Set only this owned fixture's
	// exact grant to a short native-clock boundary so no real-time minute wait
	// or production grant validation change is needed.
	var expires time.Time
	if e = f.pool.QueryRow(f.ctx, `UPDATE consent_grants SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, grant.ID).Scan(&expires); e != nil {
		t.Fatal(e)
	}
	v := f.view()
	if v.Connections[0].DisplayName != "仅限当前本人授权姓名" || v.ValidUntil.After(expires) {
		t.Fatal("native name grant expiry must cap original response deadline", v.ValidUntil, expires)
	}
	var delay float64
	if e := f.pool.QueryRow(f.ctx, `SELECT GREATEST(0,extract(epoch FROM $1::timestamptz-clock_timestamp()))+0.03`, expires).Scan(&delay); e != nil {
		t.Fatal(e)
	}
	f.exec(`SELECT pg_sleep($1)`, delay)
	if e := f.store.ValidateCloseConnectionsResponse(f.ctx, f.owner, v); !errors.Is(e, connection.ErrCloseChanged) {
		t.Fatal("unchanged native rows cannot keep an expired name response current", e)
	}
	before := f.snapshot()
	if _, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, f.input(v)); !errors.Is(e, connection.ErrCloseChanged) || f.snapshot() != before {
		t.Fatal("expired original proposal must not create Close or audit", e)
	}
	fresh := f.view()
	if fresh.Connections[0].DisplayName != "Birdtie 成员" {
		t.Fatal("fresh eligible Friend view hides the now-unpermitted name")
	}
}
func TestCloseNativeWaitingBlockAndRemoveRejectOldSource(t *testing.T) {
	for _, kind := range []string{"block", "remove"} {
		t.Run(kind, func(t *testing.T) {
			f := closePG(t)
			in := f.input(f.view())
			tx, e := f.pool.BeginTx(f.ctx, pgx.TxOptions{})
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			if kind == "block" {
				_, e = tx.Exec(f.ctx, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.peer.Actor.ID, f.owner.Actor.ID)
			} else {
				_, e = tx.Exec(f.ctx, `UPDATE person_ties SET status='removed' WHERE id=$1`, f.tie)
			}
			if e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { _, e := f.store.PutOwnCloseConnectionCurrent(f.ctx, f.owner, f.peer.Actor.ID, in); done <- e }()
			select {
			case e := <-done:
				t.Fatal("writer must await actual prior native boundary", e)
			case <-time.After(30 * time.Millisecond):
			}
			if e = tx.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				if !errors.Is(e, connection.ErrNotFound) && !errors.Is(e, connection.ErrCloseChanged) {
					t.Fatal("late original source accepted", e)
				}
			case <-f.ctx.Done():
				t.Fatal("native source wait never resolved")
			}
			if f.visible(f.owner.Actor.ID, f.peer.Actor.ID, true) {
				t.Fatal("waiting source created Close")
			}
		})
	}
}
func TestCloseNativeGuardAndSafeParentCascade(t *testing.T) {
	f := closePG(t)
	for _, tc := range []string{"non-friend", "wrong-request", "wrong-tie", "self", "foreign-revision"} {
		t.Run(tc, func(t *testing.T) {
			peer, tie, request, rev := f.peer.Actor.ID, f.tie, f.request, int64(1)
			switch tc {
			case "non-friend":
				peer = f.outsider.Actor.ID
			case "wrong-request":
				request = f.outsider.Actor.ID
			case "wrong-tie":
				tie = f.outsider.Actor.ID
			case "self":
				peer = f.owner.Actor.ID
			case "foreign-revision":
				rev = 3
			}
			_, e := f.pool.Exec(f.ctx, `INSERT INTO person_close_connections(owner_account_id,person_account_id,tie_id,request_id,is_active,native_revision,selected_at) VALUES($1,$2,$3,$4,true,$5,clock_timestamp())`, f.owner.Actor.ID, peer, tie, request, rev)
			if e == nil {
				t.Fatal("native guard accepted incompatible source")
			}
		})
	}
	f.choose()
	t.Run("cannot-erase-live-fence", func(t *testing.T) {
		if _, e := f.pool.Exec(f.ctx, `DELETE FROM person_close_connections WHERE owner_account_id=$1`, f.owner.Actor.ID); e == nil {
			t.Fatal("native source fence erased")
		}
	})
	t.Run("parent-tie-cascade-compatible", func(t *testing.T) {
		f.exec(`DELETE FROM person_ties WHERE id=$1`, f.tie)
		var n int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM person_close_connections WHERE owner_account_id=$1 OR person_account_id=$1`, f.owner.Actor.ID).Scan(&n); e != nil || n != 0 {
			t.Fatal("old fixture may safely delete parent Tie", e)
		}
	})
}
