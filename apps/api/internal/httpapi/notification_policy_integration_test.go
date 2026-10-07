package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real PostgreSQL and actual registered HTTP routes. All actors, Sessions and
// policy values are owned disposable synthetic fixtures, not IdP/push/provider,
// source-consumer or production readiness evidence. Env-disabled integration
// remains a Skip in unconfigured developer runs; our acceptance runner must
// provide an actual fresh migration061 DB and record zero skips.
func notificationPolicyHTTPDBNew(t *testing.T) *privateProfileHTTPDBFixture {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	var installed bool
	if err := f.pool.QueryRow(f.ctx, `SELECT to_regclass('public.native_notification_policies') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("notification HTTP integration requires actual migration061")
	}
	// Explicitly remove only this fixture's owned Agent parents, then prove
	// native policy cascade before the shared identity cleanup runs.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := f.pool.Exec(ctx, `DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Error("owned notification Agent cleanup failed")
			return
		}
		var residue int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM native_notification_policies WHERE owner_id=ANY($1::uuid[])`, f.accountIDs).Scan(&residue); err != nil || residue != 0 {
			t.Errorf("owned notification policy residue=%d", residue)
		} else {
			t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY notification policy cascade residue=0")
		}
	})
	return f
}

func notificationPolicyHTTPDBRecord(t *testing.T, raw []byte) agentnotification.Policy {
	t.Helper()
	var envelope struct {
		Data agentnotification.Policy `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || agentnotification.ValidatePolicy(envelope.Data) != nil {
		t.Fatal("actual notification policy response schema invalid")
	}
	return envelope.Data
}
func notificationPolicyHTTPDBBody(t *testing.T, input agentnotification.PutInput) string {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func notificationPolicyHTTPDBSourceSnapshot(t *testing.T, f *privateProfileHTTPDBFixture) string {
	t.Helper()
	var result string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
	 'profiles',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY account_id) FROM user_profiles p WHERE account_id=ANY($1::uuid[])),'[]'::jsonb),
	 'metadata',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_profiles p WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb),
	 'private',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_private_profiles p WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb),
	 'visibility',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_profile_field_visibility p WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb),
	 'memory',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agent_memories p WHERE owner_id=ANY($1::uuid[])),'[]'::jsonb)
	)::text`, f.accountIDs).Scan(&result); err != nil {
		t.Fatal("cannot inspect owned notification source baseline")
	}
	return result
}

func TestNotificationPolicyHTTPNativeLifecycleIntegration(t *testing.T) {
	f := notificationPolicyHTTPDBNew(t)
	baseline := f.effects(t)
	sourceBefore := notificationPolicyHTTPDBSourceSnapshot(t, f)
	rules := []agentnotification.Rule{{Category: agentnotification.CategorySocial, Route: agentnotification.Block}, {Category: agentnotification.CategoryMessage, Route: agentnotification.Immediate}, {Category: agentnotification.CategoryActivity, Route: agentnotification.Digest}}
	input := agentnotification.PutInput{Enabled: true, DefaultRoute: agentnotification.Normal, Rules: rules, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	var current agentnotification.Policy
	t.Run("unconfigured_get_no_insert", func(t *testing.T) {
		w := f.request(t, f.handler, "GET", notificationHTTPPath, "", f.tokens[0], 200, nil)
		p := notificationPolicyHTTPDBRecord(t, w.Body.Bytes())
		if p.Version != 0 || p.Enabled || p.AgentID != f.agentIDs[0] || p.Rules == nil {
			t.Fatal("default policy is not exact current Agent")
		}
		var count int
		if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_policies WHERE owner_id=ANY($1::uuid[])`, f.accountIDs).Scan(&count) != nil || count != 0 {
			t.Fatal("GET persisted default policy")
		}
	})
	t.Run("save_exact_own_first_version", func(t *testing.T) {
		w := f.request(t, f.handler, "PUT", notificationHTTPPath, notificationPolicyHTTPDBBody(t, input), f.tokens[0], 200, nil)
		current = notificationPolicyHTTPDBRecord(t, w.Body.Bytes())
		if current.AgentID != f.agentIDs[0] || current.Version != 1 || !current.Enabled || current.Rules[0].Category != agentnotification.CategoryActivity {
			t.Fatal("real first version/binding/normalization")
		}
	})
	t.Run("stale_cas_conflict_without_change", func(t *testing.T) {
		f.request(t, f.handler, "PUT", notificationHTTPPath, notificationPolicyHTTPDBBody(t, input), f.tokens[0], 409, nil)
		w := f.request(t, f.handler, "GET", notificationHTTPPath, "", f.tokens[0], 200, nil)
		p := notificationPolicyHTTPDBRecord(t, w.Body.Bytes())
		if !reflect.DeepEqual(p, current) {
			t.Fatal("stale CAS modified saved policy")
		}
	})
	t.Run("other_person_isolated_namespace", func(t *testing.T) {
		w := f.request(t, f.handler, "GET", notificationHTTPPath, "", f.tokens[1], 200, nil)
		p := notificationPolicyHTTPDBRecord(t, w.Body.Bytes())
		if p.AgentID != f.agentIDs[1] || p.Version != 0 {
			t.Fatal("peer read another namespace")
		}
		foreign := input
		foreign.ExpectedVersion = current.Version
		f.request(t, f.handler, "PUT", notificationHTTPPath, notificationPolicyHTTPDBBody(t, foreign), f.tokens[1], 409, nil)
		f.request(t, f.handler, "GET", notificationHTTPPath+"?ownerId="+f.accountIDs[0], "", f.tokens[1], 400, nil)
	})
	t.Run("disable_retains_cas_record", func(t *testing.T) {
		next := input
		next.ExpectedVersion = current.Version
		next.Enabled = false
		next.DefaultRoute = agentnotification.Block
		w := f.request(t, f.handler, "PUT", notificationHTTPPath, notificationPolicyHTTPDBBody(t, next), f.tokens[0], 200, nil)
		current = notificationPolicyHTTPDBRecord(t, w.Body.Bytes())
		if current.Version != 2 || current.Enabled {
			t.Fatal("disabled record version")
		}
		decision, err := agentnotification.ChooseRoute(current, agentnotification.CategoryActivity, time.Now())
		if err != nil || decision.Route != agentnotification.Normal || decision.Reason != "disabled" {
			t.Fatal("disabled was claimed as silent")
		}
	})
	t.Run("enable_requires_new_revision", func(t *testing.T) {
		next := input
		next.ExpectedVersion = current.Version
		pause := time.Now().UTC().Add(time.Minute)
		next.PauseUntil = &pause
		w := f.request(t, f.handler, "PUT", notificationHTTPPath, notificationPolicyHTTPDBBody(t, next), f.tokens[0], 200, nil)
		current = notificationPolicyHTTPDBRecord(t, w.Body.Bytes())
		if current.Version != 3 || !current.Enabled || current.PauseUntil == nil {
			t.Fatal("enable/pause revision")
		}
	})
	t.Run("restart_native_store_persistence", func(t *testing.T) {
		pool, err := pgxpool.New(f.ctx, f.pool.Config().ConnString())
		if err != nil {
			t.Fatal("new native pool")
		}
		defer pool.Close()
		store := postgres.New(pool, false)
		handler := privateProfileHTTPNew(store, store)
		w := f.request(t, handler, "GET", notificationHTTPPath, "", f.tokens[0], 200, nil)
		if p := notificationPolicyHTTPDBRecord(t, w.Body.Bytes()); !reflect.DeepEqual(p, current) {
			t.Fatal("policy changed across reconnect")
		}
	})
	t.Run("real_clock_expiry_bounds", func(t *testing.T) {
		for _, until := range []time.Time{time.Now().Add(-time.Hour), time.Now(), time.Now().Add(31 * 24 * time.Hour)} {
			next := input
			next.ExpectedVersion = current.Version
			next.ExpiresAt = until
			f.request(t, f.handler, "PUT", notificationHTTPPath, notificationPolicyHTTPDBBody(t, next), f.tokens[0], 400, nil)
		}
		w := f.request(t, f.handler, "GET", notificationHTTPPath, "", f.tokens[0], 200, nil)
		if p := notificationPolicyHTTPDBRecord(t, w.Body.Bytes()); !reflect.DeepEqual(p, current) {
			t.Fatal("invalid deadline modified policy")
		}
	})
	f.assertEffectsUnchanged(t, baseline)
	if notificationPolicyHTTPDBSourceSnapshot(t, f) != sourceBefore {
		t.Fatal("notification policy changed native Profile/Private/visibility/Memory/source versions")
	}
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual policy HTTP requests=%d", f.requestCount)
}

func TestNotificationPolicyHTTPNativeIdentityIntegration(t *testing.T) {
	f := notificationPolicyHTTPDBNew(t)
	baseline := notificationPolicyHTTPDBSourceSnapshot(t, f)
	input := agentnotification.PutInput{Enabled: true, DefaultRoute: agentnotification.Normal, Rules: []agentnotification.Rule{}, ExpiresAt: time.Now().Add(time.Hour)}
	body := notificationPolicyHTTPDBBody(t, input)
	for _, method := range []string{"GET", "PUT"} {
		requestBody := body
		if method == "GET" {
			requestBody = ""
		}
		for _, test := range []struct {
			name, token string
			status      int
		}{{"anonymous", "", 401}, {"expired", f.newSession(f.accountIDs[0], true, false), 401}, {"revoked", f.newSession(f.accountIDs[0], false, true), 401}, {"organization", f.tokens[2], 403}, {"business", f.tokens[3], 403}} {
			t.Run(method+"/"+test.name, func(t *testing.T) {
				f.request(t, f.handler, method, notificationHTTPPath, requestBody, test.token, test.status, nil)
			})
		}
		for _, selector := range []string{"ownerId=" + f.accountIDs[1], "agentId=" + f.agentIDs[1], "confirmed=true"} {
			t.Run(method+"/"+selector, func(t *testing.T) {
				f.request(t, f.handler, method, notificationHTTPPath+"?"+selector, requestBody, f.tokens[0], 400, nil)
			})
		}
		t.Run(method+"/org_workspace", func(t *testing.T) {
			empty := ""
			f.request(t, f.handler, method, notificationHTTPPath, requestBody, f.tokens[0], 403, &empty)
		})
	}
	t.Run("native_mismatched_workspace_principal", func(t *testing.T) {
		digest, err := identity.ParseBearer("Bearer " + f.tokens[1])
		if err != nil {
			t.Fatal(err)
		}
		access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
		if p, err := f.store.GetOwnNotificationPolicy(f.ctx, access); !errors.Is(err, agentnotification.ErrForbidden) || !reflect.DeepEqual(p, agentnotification.Policy{}) {
			t.Fatal("real current session cross owner read accepted")
		}
		if p, err := f.store.PutOwnNotificationPolicy(f.ctx, access, input); !errors.Is(err, agentnotification.ErrForbidden) || !reflect.DeepEqual(p, agentnotification.Policy{}) {
			t.Fatal("real current session cross owner write accepted")
		}
	})
	t.Run("inactive_person_agent", func(t *testing.T) {
		f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
		for _, method := range []string{"GET", "PUT"} {
			data := body
			if method == "GET" {
				data = ""
			}
			f.request(t, f.handler, method, notificationHTTPPath, data, f.tokens[0], 403, nil)
		}
		f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
	})
	t.Run("inactive_account", func(t *testing.T) {
		f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0])
		for _, method := range []string{"GET", "PUT"} {
			data := body
			if method == "GET" {
				data = ""
			}
			f.request(t, f.handler, method, notificationHTTPPath, data, f.tokens[0], 401, nil)
		}
		f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.accountIDs[0])
	})
	if notificationPolicyHTTPDBSourceSnapshot(t, f) != baseline {
		t.Fatal("denied operations changed authoritative source")
	}
	var rows int
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_policies WHERE owner_id=ANY($1::uuid[])`, f.accountIDs).Scan(&rows) != nil || rows != 0 {
		t.Fatal("denied operation created preference")
	}
	t.Run("missing_metadata_never_recreated", func(t *testing.T) {
		f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0])
		for _, method := range []string{"GET", "PUT"} {
			data := body
			if method == "GET" {
				data = ""
			}
			f.request(t, f.handler, method, notificationHTTPPath, data, f.tokens[0], 404, nil)
		}
		var count int
		if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0]).Scan(&count) != nil || count != 0 {
			t.Fatal("notification policy recreated withdrawn metadata")
		}
	})
}

func TestNotificationPolicyHTTPNativeConcurrentCASIntegration(t *testing.T) {
	f := notificationPolicyHTTPDBNew(t)
	baseline := notificationPolicyHTTPDBSourceSnapshot(t, f)
	bodyFor := func(route agentnotification.Route) string {
		return notificationPolicyHTTPDBBody(t, agentnotification.PutInput{Enabled: true, DefaultRoute: route, Rules: []agentnotification.Rule{}, ExpiresAt: time.Now().Add(time.Hour)})
	}
	inputs := []string{bodyFor(agentnotification.Immediate), bodyFor(agentnotification.Block)}
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, body := range inputs {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			<-start
			rw := newNotificationPolicyRecorder()
			f.handler.ServeHTTP(rw, privateProfileHTTPRequest("PUT", notificationHTTPPath, body, f.tokens[0], "application/json"))
			codes <- rw.Code
		}(body)
	}
	close(start)
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatalf("native concurrent policy HTTP unexpected status=%d", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("same expectedVersion did not have exactly one winner")
	}
	w := f.request(t, f.handler, "GET", notificationHTTPPath, "", f.tokens[0], 200, nil)
	p := notificationPolicyHTTPDBRecord(t, w.Body.Bytes())
	if p.Version != 1 || (p.DefaultRoute != agentnotification.Immediate && p.DefaultRoute != agentnotification.Block) {
		t.Fatal("concurrent saved policy wrong")
	}
	if notificationPolicyHTTPDBSourceSnapshot(t, f) != baseline {
		t.Fatal("CAS changed original source")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY concurrent native HTTP CAS success=1 conflict=1")
}

// Constructor avoids shared fixture counters/logging inside concurrent calls.
func newNotificationPolicyRecorder() *notificationPolicyNativeRecorder {
	return &notificationPolicyNativeRecorder{header: make(http.Header), Code: 200}
}

type notificationPolicyNativeRecorder struct {
	header http.Header
	Code   int
	body   []byte
}

func (w *notificationPolicyNativeRecorder) Header() http.Header  { return w.header }
func (w *notificationPolicyNativeRecorder) WriteHeader(code int) { w.Code = code }
func (w *notificationPolicyNativeRecorder) Write(body []byte) (int, error) {
	w.body = append(w.body, body...)
	return len(body), nil
}
