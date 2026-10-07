package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Companion adapter only for the existing offline transport fixtures. These
// synthetic versions/authority are not a native permission or production grant.
func (httpActionSafetyCatalog) ResolveOwnContextAgent(_ context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	return agentcognitive.AgentReference{AgentID: "33000000-0000-4000-8000-000000000091", Principal: a.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, nil
}
func (httpActionSafetyCatalog) BuildOwnAgentContext(_ context.Context, r acb.Request) (acb.BuiltContext, error) {
	now := time.Now().UTC()
	out := acb.BuiltContext{Authority: strings.Repeat("a", 64), Bundle: acb.Bundle{SchemaVersion: acb.SchemaVersion, Agent: r.Agent, Mode: r.Mode, RequestID: r.RequestID, TaskID: r.TaskID, CityID: r.CityID, CurrentQuery: r.CurrentQuery, ObservedAt: now, ExpiresAt: r.DeadlineAt, ModelAccess: "UNAVAILABLE", Sections: acb.Sections{Profile: "NOT_REQUESTED", Memories: "NOT_REQUESTED", Policies: "NOT_REQUESTED", Relationships: "UNAVAILABLE", Activities: "AVAILABLE", Places: "NOT_REQUESTED"}}}
	for _, x := range []struct{ kind, id string }{{"PUBLIC_CITY", r.CityID}, {"CURRENT_TASK_REQUEST", r.TaskID}} {
		v, _ := acb.PublicVersion(x.kind, r.TaskUpdatedAt, []byte(`{"offlineTransportFixture":true}`))
		out.Bundle.Sources = append(out.Bundle.Sources, acb.Source{Kind: x.kind, ID: x.id, Version: v, NativeTime: r.TaskUpdatedAt})
	}
	for _, id := range r.ActivityIDs {
		v, _ := acb.PublicVersion("PUBLIC_ACTIVITY", r.TaskUpdatedAt, []byte(`{"offlineTransportFixture":true}`))
		out.Bundle.Sources = append(out.Bundle.Sources, acb.Source{Kind: "PUBLIC_ACTIVITY", ID: id, Version: v, NativeTime: r.TaskUpdatedAt})
		out.Activities = append(out.Activities, foundation.Activity{ID: id, CityID: r.CityID, Title: "Synthetic public activity", Visibility: "public"})
		out.Bundle.Activities = append(out.Bundle.Activities, acb.PublicActivity{ID: id, Title: "Synthetic public activity"})
	}
	return out, nil
}

type contextBuilderHTTPCatalog struct {
	*postgres.Store
	before        func(acb.Request)
	beforeResolve func(agentprofile.PrivateAccess) error
	calls         int
	modes         []acb.Mode
}

func (c *contextBuilderHTTPCatalog) ResolveOwnContextAgent(ctx context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	if c.beforeResolve != nil {
		if e := c.beforeResolve(a); e != nil {
			return agentcognitive.AgentReference{}, e
		}
	}
	return c.Store.ResolveOwnContextAgent(ctx, a)
}

func (c *contextBuilderHTTPCatalog) BuildOwnAgentContext(ctx context.Context, r acb.Request) (acb.BuiltContext, error) {
	c.calls++
	c.modes = append(c.modes, r.Mode)
	if c.before != nil {
		c.before(r)
	}
	return c.Store.BuildOwnAgentContext(ctx, r)
}
func contextBuilderHTTPNew(c foundation.PublicCatalog, a identity.AccessStore, s *postgres.Store) http.Handler {
	return New(c, a, nil, s, s, s, s, s, s, s, s, nil, false, nil, nil, nil)
}

type contextBuilderHTTPFixture struct {
	*privateProfileHTTPDBFixture
	city, place, public, private string
	catalog                      *contextBuilderHTTPCatalog
}

func contextBuilderHTTPNative(t *testing.T) *contextBuilderHTTPFixture {
	f := &contextBuilderHTTPFixture{privateProfileHTTPDBFixture: privateProfileHTTPDBNew(t)}
	if e := f.pool.QueryRow(f.ctx, `SELECT 'builder033-'||gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&f.city, &f.place); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`, `DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(context.Background(), q, f.accountIDs); e != nil {
				t.Error("ContextHTTP owned cleanup", e)
			}
		}
		for _, q := range []string{`DELETE FROM places WHERE city_id=$1`, `DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
			if _, e := f.pool.Exec(context.Background(), q, f.city); e != nil {
				t.Error("ContextHTTP owned city cleanup", e)
			}
		}
	})
	f.exec(`INSERT INTO cities(id,name,country_code,region,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'本地合成 Context 城市','GB','LOCAL_SYNTHETIC','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:AGE033','合成维护者',$2)`, f.city, f.accountIDs[1])
	f.exec(`INSERT INTO city_contexts(city_id) VALUES($1) ON CONFLICT(city_id) DO NOTHING`, f.city)
	f.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'合成 Context 地点','sports',57.15,-2.1,'point','published','LOCAL_SYNTHETIC_FIXTURE','local:AGE033','合成维护者',$3)`, f.place, f.city, f.accountIDs[1])
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	for _, x := range []struct {
		vis    string
		target *string
	}{{"public", &f.public}, {"invite_only", &f.private}} {
		a, e := f.store.CreateSocialDraft(f.ctx, f.accountIDs[1], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[1]}, CityID: f.city, PlaceID: f.place, Title: "合成羽毛球 " + x.vis, Summary: "UNREQUESTED_CONTEXT_SUMMARY_CANARY", Description: "UNREQUESTED_CONTEXT_BODY_CANARY", CategoryCode: "badminton", Visibility: x.vis, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), TimeZone: "Europe/London"})
		if e != nil {
			t.Fatal(e)
		}
		*x.target = a.ID
		if _, e = f.store.PublishSocialActivity(f.ctx, f.accountIDs[1], a.ID); e != nil {
			t.Fatal(e)
		}
		if x.vis == "invite_only" {
			if e = f.store.InviteActivityPerson(f.ctx, f.accountIDs[1], a.ID, f.accountIDs[0]); e != nil {
				t.Fatal(e)
			}
		}
	}
	f.catalog = &contextBuilderHTTPCatalog{Store: f.store}
	f.handler = contextBuilderHTTPNew(f.catalog, f.store, f.store)
	return f
}
func TestContextBuilderHTTPRegisteredRuntimeConsumesCurrentNativeBundle(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	f.catalog.before = func(r acb.Request) {
		if r.Mode != acb.RulesPublicQuery || len(r.ProfileFields)+len(r.MemoryIDs)+len(r.PolicyFamilies) > 0 {
			t.Fatal("runtime read private selectors")
		}
		for _, id := range r.ActivityIDs {
			if id == f.private {
				t.Fatal("private lawful result entered public bundle")
			}
		}
		if len(r.ActivityIDs) > 0 {
			f.exec(`UPDATE activities SET title='Context 当前重新读取的标题',updated_at=clock_timestamp() WHERE id=$1`, f.public)
		}
	}
	w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"帮我找羽毛球活动"}`, f.tokens[0], 200, nil)
	if !strings.Contains(w.Body.String(), "Context 当前重新读取的标题") || !strings.Contains(w.Body.String(), f.private) || f.catalog.calls != 1 {
		t.Fatalf("builder not used or lawful private supply lost calls=%d response=%s", f.catalog.calls, w.Body.String())
	}
	var reply struct {
		Data struct {
			TaskID string `json:"taskId"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Data.TaskID == "" {
		t.Fatal("task id missing")
	}
	f.catalog.before = nil
	f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", fmt.Sprintf(`{"query":"近一点的呢？","taskId":%q}`, reply.Data.TaskID), f.tokens[0], 200, nil)
	f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找附近的地点"}`, f.tokens[0], 200, nil)
	if f.catalog.calls != 3 {
		t.Fatalf("runtime follow-up/place did not consume builder calls=%d", f.catalog.calls)
	}
	var raw string
	if e := f.pool.QueryRow(f.ctx, `SELECT conversation::text FROM agent_tasks WHERE id=$1`, reply.Data.TaskID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(raw, "CANARY") {
		t.Fatal("task copied entity body into history")
	}
}
func TestContextBuilderHTTPNativeBetweenSearchAndBuildCurrentGuards(t *testing.T) {
	for _, name := range []string{"Hide", "Cancel", "Block", "Revoke", "Suspend", "RemoveMetadata"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderHTTPNative(t)
			f.catalog.before = func(r acb.Request) {
				switch name {
				case "Hide":
					f.exec(`UPDATE activities SET publication_status='hidden' WHERE id=$1`, f.public)
				case "Cancel":
					f.exec(`UPDATE activities SET cancelled_at=clock_timestamp() WHERE id=$1`, f.public)
				case "Block":
					f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[0], f.accountIDs[1])
				case "Revoke":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
				case "Suspend":
					f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0])
				case "RemoveMetadata":
					f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0])
				}
			}
			w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"帮我找羽毛球活动"}`, f.tokens[0], 403, nil)
			if strings.Contains(w.Body.String(), f.public) || strings.Contains(w.Body.String(), "CANARY") || strings.Contains(w.Body.String(), "SELECT") {
				t.Fatal("failed closed response leaked source")
			}
		})
	}
}
func TestContextBuilderHTTPMissingPortAndAnonymousDoNotBorrowPersonContext(t *testing.T) {
	s, mux, _, token := httpActionSafetyServer(t, nil)
	s.catalog = struct{ foundation.PublicCatalog }{httpActionSafetyCatalog{}}
	status, body := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"找羽毛球活动"}`)
	if status != 503 || strings.Contains(string(body), httpActionSafetyActivity) {
		t.Fatal("missing native capability fell back", status, string(body))
	}
	status, _ = httpActionSafetyCall(t, mux, "", "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"找羽毛球活动"}`)
	if status != 200 {
		t.Fatal("anonymous existing public path broke", status)
	}
}

type contextBuilderHTTPObservedAuth struct {
	*postgres.Store
	authenticated chan bool
}

func (a *contextBuilderHTTPObservedAuth) Authenticate(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	actor, e := a.Store.Authenticate(ctx, digest)
	if e == nil {
		select {
		case a.authenticated <- true:
		default:
		}
	}
	return actor, e
}
func TestContextBuilderHTTPCurrentSessionAfterFirstAuthenticateAndAccountWait(t *testing.T) {
	for _, name := range []string{"Revoke", "AbsoluteExpiry", "IdleExpiry", "Suspend"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderHTTPNative(t)
			cfg := f.pool.Config().Copy()
			app := "builder033-http-" + f.accountIDs[0]
			cfg.ConnConfig.RuntimeParams["application_name"] = app
			pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := postgres.New(pool, false)
			auth := &contextBuilderHTTPObservedAuth{Store: store, authenticated: make(chan bool, 1)}
			var lock pgx.Tx
			entered := make(chan error, 1)
			catalog := &contextBuilderHTTPCatalog{Store: store, beforeResolve: func(a agentprofile.PrivateAccess) error {
				var e error
				lock, e = f.pool.Begin(f.ctx)
				if e == nil {
					_, e = lock.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.accountIDs[0])
				}
				entered <- e
				return e
			}}
			handler := contextBuilderHTTPNew(catalog, auth, store)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				r := privateProfileHTTPRequest("POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"帮我找羽毛球活动"}`, f.tokens[0], "application/json")
				r.Header.Set("X-Request-ID", "LOCAL_NATIVE_AUTH_WAIT")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				done <- w
			}()
			select {
			case <-auth.authenticated:
			case <-time.After(4 * time.Second):
				t.Fatal("actual first Authenticate never succeeded")
			}
			select {
			case e = <-entered:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("Runtime did not reach native resolver")
			}
			defer lock.Rollback(context.Background())
			waiting := false
			until := time.Now().Add(4 * time.Second)
			for time.Now().Before(until) {
				if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, app).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("no native Account wait after first auth")
			}
			switch name {
			case "Revoke":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
			case "AbsoluteExpiry":
				f.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',expires_at=clock_timestamp()-interval '1 second',idle_expires_at=clock_timestamp()-interval '2 seconds' WHERE account_id=$1`, f.accountIDs[0])
			case "IdleExpiry":
				f.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',idle_expires_at=clock_timestamp()-interval '1 second' WHERE account_id=$1`, f.accountIDs[0])
			case "Suspend":
				if _, e = lock.Exec(f.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0]); e != nil {
					t.Fatal(e)
				}
			}
			if e = lock.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 403 || strings.Contains(w.Body.String(), f.public) || strings.Contains(w.Body.String(), "CANARY") {
					t.Fatalf("late native auth leaked status=%d body=%s", w.Code, w.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("HTTP native late auth stuck")
			}
		})
	}
}
