package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func seedHTTPRecord(t *testing.T, w *httptest.ResponseRecorder) agentseed.Record {
	t.Helper()
	var out struct {
		Data agentseed.Record `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("invalid actual seed response")
	}
	return out.Data
}
func TestAgentSeedHTTPNativeRegisteredLifecycle(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	r := seedHTTPRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", f.tokens[0], 200, nil))
	if r.CurrentCity != nil || r.Intent.BasicIntent != "" || !r.NeedsPrompt || len(r.Cities) == 0 {
		t.Fatal(r)
	}
	input := agentseed.Input{ExpectedSnapshot: r.Snapshot, Action: "SAVE", DisplayName: "中文初始昵称", CurrentCityID: r.Cities[0].ID, CurrentCitySnapshot: r.Cities[0].Snapshot, LanguagePreferences: []string{"zh-CN"}, BasicIntent: "FIND_ACTIVITIES", InterestChoice: "SET", Interests: []string{"羽毛球"}}
	raw, _ := json.Marshal(input)
	saved := seedHTTPRecord(t, f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), f.tokens[0], 200, nil))
	if saved.Intent.Version != 1 || saved.Intent.Progress != "COMPLETED" || saved.ProfileVisibility != r.ProfileVisibility || saved.CurrentCity == nil || saved.NeedsPrompt {
		t.Fatal(saved)
	}
	f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), f.tokens[0], 409, nil)
	for _, index := range []int{1, 2, 3} {
		code := 409
		if index > 1 {
			code = 403
		}
		f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), f.tokens[index], code, nil)
	}
	f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", "", 401, nil)
	replacement := f.newSession(f.accountIDs[0], false, false)
	input.ExpectedSnapshot = saved.Snapshot
	raw, _ = json.Marshal(input)
	f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), replacement, 409, nil)
	// Registered human owner path is independent of model/projection grants.
	profile := f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[0], 200, nil)
	if strings.Contains(profile.Body.String(), "FIND_ACTIVITIES") || strings.Contains(profile.Body.String(), "personalPreferences") {
		t.Fatal("private seed leak")
	}
	t.Log("LOCAL_SYNTHETIC_ONLY registered current self seed; no real identity/model access")
}
func TestAgentSeedHTTPNativeLateRevocation(t *testing.T) {
	for _, mode := range []string{"revoke", "naturalExpiry", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := privateProfileHTTPDBNew(t)
			r := seedHTTPRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", f.tokens[0], 200, nil))
			input := agentseed.Input{ExpectedSnapshot: r.Snapshot, Action: "SAVE", DisplayName: "不得保存", CurrentCityID: r.Cities[0].ID, CurrentCitySnapshot: r.Cities[0].Snapshot, LanguagePreferences: []string{"zh-CN"}, BasicIntent: "JUST_EXPLORE", InterestChoice: "SKIP", Interests: []string{}}
			raw, _ := json.Marshal(input)
			lock, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(f.ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR UPDATE`, r.AgentID); e != nil {
				t.Fatal(e)
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/v1/me/agent-seed", strings.NewReader(string(raw)))
			req.Header.Set("Authorization", "Bearer "+f.tokens[0])
			req.Header.Set("Content-Type", "application/json")
			requestCtx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			req = req.WithContext(requestCtx)
			if mode == "naturalExpiry" {
				if _, e = f.pool.Exec(f.ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() t) UPDATE sessions SET expires_at=n.t+interval '1 second',idle_expires_at=n.t+interval '1 second' FROM n WHERE account_id=$1 AND token_sha256=$2`, f.accountIDs[0], func() []byte { d, _ := identity.ParseBearer("Bearer " + f.tokens[0]); return d[:] }()); e != nil {
					t.Fatal(e)
				}
			}
			done := make(chan struct{})
			go func() { f.handler.ServeHTTP(w, req); close(done) }()
			until := time.Now().Add(5 * time.Second)
			for {
				var wait bool
				// Another package may wait on a different fixture's metadata in the
				// same database. Only this owned locker proves our HTTP has reached
				// its resource wait; cancelling earlier can still be authentication.
				if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM agent_profiles%' AND query LIKE '%FOR UPDATE%' AND $1::integer=ANY(pg_blocking_pids(pid)))`, lock.Conn().PgConn().PID()).Scan(&wait); e != nil {
					t.Fatal(e)
				}
				if wait {
					break
				}
				if time.Now().After(until) {
					t.Fatal("real HTTP metadata wait missing")
				}
				time.Sleep(10 * time.Millisecond)
			}
			d, e := identity.ParseBearer("Bearer " + f.tokens[0])
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "revoke":
				if e = f.store.RevokeSession(f.ctx, d); e != nil {
					t.Fatal(e)
				}
			case "naturalExpiry":
				for {
					var expired bool
					if e = f.pool.QueryRow(f.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, d[:]).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			case "cancel":
				cancel()
			}
			if e = lock.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case <-done:
				want := 403
				if mode == "cancel" {
					want = 503
				}
				if w.Code != want {
					t.Fatal(w.Code, w.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("HTTP stuck")
			}
			var effect int
			if e = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM agent_seed_user_intents WHERE owner_id=$1)+(SELECT count(*) FROM agent_private_profiles WHERE owner_id=$1)+(SELECT count(*) FROM person_contexts WHERE person_account_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$1)`, f.accountIDs[0]).Scan(&effect); e != nil || effect != 0 {
				t.Fatal("denied effects", effect, e)
			}
		})
	}
}

func TestAgentSeedHTTPNativeOrdinaryContentWithoutAgent(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	if _, e := f.pool.Exec(f.ctx, `DELETE FROM agents WHERE principal_account_id=$1`, f.accountIDs[0]); e != nil {
		t.Fatal(e)
	}
	f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", f.tokens[0], 403, nil)
	f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[0], 200, nil)
	f.request(t, f.handler, "PUT", "/v1/me/profile", `{"displayName":"普通个人资料继续可用","bio":"","visibility":"private"}`, f.tokens[0], 200, nil)
}
