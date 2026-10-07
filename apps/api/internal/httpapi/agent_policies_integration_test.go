package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func policyHTTPNative(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	var installed bool
	if f.pool.QueryRow(f.ctx, `SELECT to_regclass('public.agent_policy_settings') IS NOT NULL`).Scan(&installed) != nil || !installed {
		t.Fatal("requires actual migration065")
	}
	return f
}
func policyHTTPRecord(t *testing.T, raw []byte) agentpolicysettings.Bundle {
	t.Helper()
	var v struct {
		Data agentpolicysettings.Bundle `json:"data"`
	}
	if json.Unmarshal(raw, &v) != nil || agentpolicysettings.ValidateBundle(v.Data) != nil {
		t.Fatal("native Policy response invalid")
	}
	return v.Data
}
func policyHTTPRows(t *testing.T, f *privateProfileHTTPDBFixture) string {
	t.Helper()
	var raw string
	if f.pool.QueryRow(f.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY agent_id,family),'[]'::jsonb)::text FROM agent_policy_settings p WHERE owner_id=ANY($1::uuid[])`, f.accountIDs).Scan(&raw) != nil {
		t.Fatal("cannot snapshot own native policy rows")
	}
	return raw
}
func TestPolicyAPIsNativeAllFourOperations(t *testing.T) {
	f := policyHTTPNative(t)
	before := profileAPIsSnapshot(t, f, true)
	ordinary := profileAPIsSnapshot(t, f, false)
	var expected []profileAPIsAuditSpec
	t.Run("MissingGETNoPersistence", func(t *testing.T) {
		got := policyHTTPRecord(t, f.request(t, f.handler, "GET", policyAPIPath, "", f.tokens[0], 200, nil).Body.Bytes())
		if got.OwnerID != f.accountIDs[0] || got.AgentID != f.agentIDs[0] || got.OwnerType != actorref.Person || got.Attention.Configured || got.Social.Configured || got.Autonomy.Configured || policyHTTPRows(t, f) != "[]" {
			t.Fatal("GET invented or persisted default")
		}
		profileAPIsAssertAuditDelta(t, f, ordinary)
	})
	for _, family := range agentpolicysettings.Families() {
		t.Run(string(family), func(t *testing.T) {
			body := policyAPIInput(family, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour))
			w := f.request(t, f.handler, "PUT", policyAPIPath+"/"+strings.ToLower(string(family)), body, f.tokens[0], 200, nil)
			expected = append(expected, profileAPIsAuditSpec{"replace", "agent_policy", f.agentIDs[0], "human_policy_edit", w.Header().Get("X-Request-ID")})
			got := policyHTTPRecord(t, w.Body.Bytes())
			var r agentpolicysettings.Record
			switch family {
			case agentpolicysettings.Attention:
				r = got.Attention
			case agentpolicysettings.Social:
				r = got.Social
			case agentpolicysettings.Autonomy:
				r = got.Autonomy
			}
			if r.NativeRevision != 1 || !r.Configured || r.Status != "ACTIVE" {
				t.Fatal("save result not authoritative")
			}
			afterSave := profileAPIsSnapshot(t, f, false)
			f.request(t, f.handler, "PUT", policyAPIPath+"/"+strings.ToLower(string(family)), body, f.tokens[0], 409, nil)
			profileAPIsAssertAuditDelta(t, f, afterSave)
		})
	}
	t.Run("FreshPoolServerRestart", func(t *testing.T) {
		ordinary := profileAPIsSnapshot(t, f, false)
		pool, e := pgxpool.New(f.ctx, f.pool.Config().ConnString())
		if e != nil {
			t.Fatal(e)
		}
		defer pool.Close()
		store := postgres.New(pool, false)
		handler := privateProfileHTTPNew(store, store)
		got := policyHTTPRecord(t, f.request(t, handler, "GET", policyAPIPath, "", f.tokens[0], 200, nil).Body.Bytes())
		if got.Attention.NativeRevision != 1 || got.Social.NativeRevision != 1 || got.Autonomy.NativeRevision != 1 {
			t.Fatal("actual new pool/server lost settings")
		}
		profileAPIsAssertAuditDelta(t, f, ordinary)
	})
	if before != profileAPIsSnapshot(t, f, true) {
		t.Fatal("policy API modified existing profile/Memory/purpose sources")
	}
	profileAPIsAssertAuditDelta(t, f, ordinary, expected...)
}
func TestPolicyAPIsNativeIdentityBoundaries(t *testing.T) {
	f := policyHTTPNative(t)
	body := policyAPIInput(agentpolicysettings.Social, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour))
	f.request(t, f.handler, "PUT", policyAPIPath+"/social", body, f.tokens[0], 200, nil)
	// A real organization admin role cannot select the Person owner's policy.
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status) SELECT id,$1,'admin','active' FROM organizations WHERE account_id=$2`, f.accountIDs[1], f.accountIDs[2])
	var bizAgent string
	if f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES('business',$1,'suspended') RETURNING id`, f.accountIDs[3]).Scan(&bizAgent) != nil {
		t.Fatal("cannot insert dormant Business Agent")
	}
	before := policyHTTPRows(t, f)
	t.Run("PeerAdminOwnUnconfigured", func(t *testing.T) {
		got := policyHTTPRecord(t, f.request(t, f.handler, "GET", policyAPIPath, "", f.tokens[1], 200, nil).Body.Bytes())
		if got.OwnerID != f.accountIDs[1] || got.AgentID != f.agentIDs[1] || got.Social.Configured {
			t.Fatal("Org role/peer inherited owner's policy")
		}
	})
	for _, idx := range []int{2, 3} {
		name := "Org"
		if idx == 3 {
			name = "DormantBiz"
		}
		t.Run(name, func(t *testing.T) {
			f.request(t, f.handler, "GET", policyAPIPath, "", f.tokens[idx], 403, nil)
			f.request(t, f.handler, "PUT", policyAPIPath+"/social", body, f.tokens[idx], 403, nil)
		})
	}
	for _, path := range []string{policyAPIPath + "?ownerId=" + f.accountIDs[0], policyAPIPath + "?agentId=" + f.agentIDs[0]} {
		t.Run(path[len(policyAPIPath):], func(t *testing.T) { f.request(t, f.handler, "GET", path, "", f.tokens[1], 400, nil) })
	}
	t.Run("OrgWorkspaceDenied", func(t *testing.T) {
		workspace := f.accountIDs[2]
		f.request(t, f.handler, "GET", policyAPIPath, "", f.tokens[1], 403, &workspace)
		f.request(t, f.handler, "PUT", policyAPIPath+"/social", body, f.tokens[1], 403, &workspace)
	})
	t.Run("Anonymous", func(t *testing.T) { f.request(t, f.handler, "GET", policyAPIPath, "", "", 401, nil) })
	for _, name := range []string{"expired", "revoked"} {
		t.Run(name, func(t *testing.T) {
			token := f.newSession(f.accountIDs[0], name == "expired", name == "revoked")
			f.request(t, f.handler, "GET", policyAPIPath, "", token, 401, nil)
		})
	}
	if before != policyHTTPRows(t, f) {
		t.Fatal("rejected subjects wrote policies")
	}
}
func TestPolicyAPIsNativeCurrentSessionWaiting(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		for _, mode := range []string{"expiry", "revoke", "cancel", "late_suspension_RR"} {
			t.Run(method+"_"+mode, func(t *testing.T) {
				f := policyHTTPNative(t)
				token := f.tokens[0]
				digest, _ := identity.ParseBearer("Bearer " + token)
				if mode == "expiry" {
					f.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1300 milliseconds',idle_expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE token_sha256=$1`, digest[:])
				}
				handler := f.handler
				if mode == "late_suspension_RR" {
					cfg := f.pool.Config().Copy()
					cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
					pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
					if e != nil {
						t.Fatal(e)
					}
					defer pool.Close()
					var isolation string
					if pool.QueryRow(f.ctx, `SHOW default_transaction_isolation`).Scan(&isolation) != nil || isolation != "repeatable read" {
						t.Fatal("real default RR pool not established")
					}
					store := postgres.New(pool, false)
					handler = privateProfileHTTPNew(store, store)
				}
				before := policyHTTPRows(t, f)
				source := profileAPIsSnapshot(t, f, true)
				blocker, e := f.pool.Begin(f.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer blocker.Rollback(context.Background())
				var pid int
				if blocker.QueryRow(f.ctx, `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, f.accountIDs[0]).Scan(&pid) != nil {
					t.Fatal("cannot lock real Account")
				}
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				done := make(chan *httptest.ResponseRecorder, 1)
				path, body := policyAPIPath, ""
				if method == "PUT" {
					path += "/autonomy"
					body = policyAPIInput(agentpolicysettings.Autonomy, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour))
				}
				go func() {
					w := httptest.NewRecorder()
					r := privateProfileHTTPRequest(method, path, body, token, "application/json").WithContext(ctx)
					r.Header.Set("X-Request-ID", "policy_current_"+mode)
					handler.ServeHTTP(w, r)
					done <- w
				}()
				profileAPIsWaitBlocked(t, f, pid)
				want := 403
				switch mode {
				case "revoke":
					if f.store.RevokeSession(f.ctx, digest) != nil {
						t.Fatal("real revoke blocked or failed")
					}
				case "cancel":
					cancel()
					want = 503
				case "late_suspension_RR":
					if _, e = blocker.Exec(f.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0]); e != nil {
						t.Fatal(e)
					}
					defer f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.accountIDs[0])
				case "expiry":
					expired := false
					deadline := time.Now().Add(4 * time.Second)
					for !expired && time.Now().Before(deadline) {
						if f.pool.QueryRow(f.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired) != nil {
							t.Fatal("cannot read native PG clock")
						}
						if !expired {
							time.Sleep(10 * time.Millisecond)
						}
					}
					if !expired {
						t.Fatal("real expiry not reached")
					}
				}
				if blocker.Commit(f.ctx) != nil {
					t.Fatal("cannot release native wait")
				}
				select {
				case w := <-done:
					if w.Code != want || before != policyHTTPRows(t, f) || source != profileAPIsSnapshot(t, f, true) || strings.Contains(w.Body.String(), "settings") {
						t.Fatalf("actual current session wait %s status=%d expected=%d", mode, w.Code, want)
					}
					t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY %s firstAuthenticate -> actual Account wait -> %s -> %d/no policy writes", method, mode, w.Code)
				case <-time.After(6 * time.Second):
					t.Fatal("real current native request did not finish")
				}
			})
		}
	}
}
func TestPolicyAPIsNativeAfterWriteExpiryRollback(t *testing.T) {
	f := policyHTTPNative(t)
	digest, _ := identity.ParseBearer("Bearer " + f.tokens[0])
	f.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1700 milliseconds',idle_expires_at=clock_timestamp()+interval '1600 milliseconds' WHERE token_sha256=$1`, digest[:])
	blocker, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var pid int
	if blocker.QueryRow(f.ctx, `SELECT pg_backend_pid() FROM (SELECT pg_advisory_xact_lock(hashtextextended('policy-after-insert:'||$1::text,0))) s`, f.agentIDs[0]).Scan(&pid) != nil {
		t.Fatal("cannot hold owned after-write barrier")
	}
	// Owned disposable DB trigger exists solely to delay a real write statement.
	// It provides no fake current session/purpose or allow result.
	f.exec(`CREATE FUNCTION age070_test_after_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(hashtextextended('policy-after-insert:'||NEW.agent_id::text,0));RETURN NEW;END $$;CREATE TRIGGER age070_test_after_insert AFTER INSERT ON agent_policy_settings FOR EACH ROW EXECUTE FUNCTION age070_test_after_insert()`)
	defer f.exec(`DROP TRIGGER age070_test_after_insert ON agent_policy_settings;DROP FUNCTION age070_test_after_insert()`)
	before := policyHTTPRows(t, f)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, privateProfileHTTPRequest("PUT", policyAPIPath+"/autonomy", policyAPIInput(agentpolicysettings.Autonomy, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)), f.tokens[0], "application/json"))
		done <- w
	}()
	deadline := time.Now().Add(4 * time.Second)
	blocked := false
	for time.Now().Before(deadline) && !blocked {
		if f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked) != nil {
			t.Fatal("cannot observe actual post-write wait")
		}
		if !blocked {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !blocked {
		t.Fatal("native settings write never reached barrier")
	}
	expired := false
	for time.Now().Before(deadline) && !expired {
		if f.pool.QueryRow(f.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired) != nil {
			t.Fatal("cannot read post-write clock")
		}
		if !expired {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !expired {
		t.Fatal("post-write real expiry not reached")
	}
	if blocker.Commit(f.ctx) != nil {
		t.Fatal("cannot release post-write barrier")
	}
	select {
	case w := <-done:
		if w.Code != 403 || before != policyHTTPRows(t, f) {
			t.Fatal("expired-after-write native tx did not rollback", w.Code)
		}
		t.Log("actual SQL insert -> owned AFTER INSERT lock -> session expiry -> final PG clock rejects403/whole row rolled back")
	case <-time.After(6 * time.Second):
		t.Fatal("post-write request did not finish")
	}
}
func TestPolicyAPIsNativeMetadataAbsentNoRepair(t *testing.T) {
	f := policyHTTPNative(t)
	f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0])
	before := policyHTTPRows(t, f)
	f.request(t, f.handler, "GET", policyAPIPath, "", f.tokens[0], 404, nil)
	f.request(t, f.handler, "PUT", policyAPIPath+"/autonomy", policyAPIInput(agentpolicysettings.Autonomy, 0, time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)), f.tokens[0], 404, nil)
	var n int
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0]).Scan(&n) != nil || n != 0 || before != policyHTTPRows(t, f) {
		t.Fatal("read or write repaired missing metadata")
	}
}
func TestPolicyAPIsNativeExpiredGETKeepsSource(t *testing.T) {
	f := policyHTTPNative(t)
	body := policyAPIInput(agentpolicysettings.Autonomy, 0, time.Now().UTC().Truncate(time.Microsecond).Add(250*time.Millisecond))
	first := policyHTTPRecord(t, f.request(t, f.handler, "PUT", policyAPIPath+"/autonomy", body, f.tokens[0], 200, nil).Body.Bytes())
	before := policyHTTPRows(t, f)
	for time.Now().Before(*first.Autonomy.ExpiresAt) {
		time.Sleep(10 * time.Millisecond)
	}
	current := policyHTTPRecord(t, f.request(t, f.handler, "GET", policyAPIPath, "", f.tokens[0], 200, nil).Body.Bytes())
	a, b := first.Autonomy, current.Autonomy
	a.Status, b.Status = "", ""
	if current.Autonomy.Status != "EXPIRED" || !reflect.DeepEqual(a, b) || before != policyHTTPRows(t, f) {
		t.Fatal("expired GET revived/revised settings")
	}
}
