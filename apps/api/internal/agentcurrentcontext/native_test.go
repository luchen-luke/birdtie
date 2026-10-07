package agentcurrentcontext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type nativeFixture struct {
	ctx                                          context.Context
	pool                                         *pgxpool.Pool
	store                                        *postgres.Store
	service                                      *Service
	controller                                   *agentfeature.Controller
	owner, agent, city, contextID, taskID, other string
	access                                       agentprofile.PrivateAccess
	config                                       agentfeature.Config
}

func enabledConfig(t *testing.T) agentfeature.Config {
	t.Helper()
	cfg, e := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":false,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
	if e != nil {
		t.Fatal(e)
	}
	return cfg
}
func nativeFixtureFor(t *testing.T) *nativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("current native context requires explicitly disposable PostgreSQL")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal("cannot create native fixture pool")
	}
	t.Cleanup(pool.Close)
	f := &nativeFixture{ctx: ctx, pool: pool, store: postgres.New(pool, false), config: enabledConfig(t)}
	var installed bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('public.agent_profiles') IS NOT NULL AND to_regclass('public.agent_memories') IS NOT NULL AND to_regclass('public.person_contexts') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("native context proof requires 001-056 and current identity metadata")
	}
	var suffix string
	if e = pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&suffix); e != nil {
		t.Fatal(e)
	}
	f.city = "native-current-" + suffix
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		owners := []string{}
		if f.owner != "" {
			owners = append(owners, f.owner)
		}
		if f.other != "" {
			owners = append(owners, f.other)
		}
		for _, sql := range []string{`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`, `DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, `DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`} {
			if _, e := pool.Exec(cleanupCtx, sql, owners); e != nil {
				t.Errorf("owned account cleanup failed: %v", e)
			}
		}
		for _, sql := range []string{`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
			if _, e := pool.Exec(cleanupCtx, sql, f.city); e != nil {
				t.Errorf("owned city cleanup failed: %v", e)
			}
		}
		var count int
		if e := pool.QueryRow(cleanupCtx, `SELECT (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+(SELECT count(*) FROM cities WHERE id=$2)`, owners, f.city).Scan(&count); e != nil || count != 0 {
			t.Errorf("owned native context residue: %d %v", count, e)
		}
	})
	for _, dest := range []*string{&f.owner, &f.other} {
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id::text`).Scan(dest); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) ON CONFLICT DO NOTHING`, f.owner); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE agent_type='personal' AND principal_account_id=$1`, f.owner).Scan(&f.agent); e != nil {
		t.Fatal(e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal("fixture credential allocation failed")
	}
	if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	f.access = agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.owner}}
	if _, e = pool.Exec(ctx, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'合成临时城市','Fixture','GB','Europe/London','published','native fixture',$1,'synthetic test')`, f.city); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO city_contexts(city_id) VALUES($1)`, f.city); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT id::text FROM contexts WHERE context_type='CITY' AND city_id=$1`, f.city).Scan(&f.contextID); e != nil {
		t.Fatal(e)
	}
	d, e := f.store.DeclareContext(ctx, f.owner, contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.city, Relation: "current"})
	if e != nil || d.ContextID != f.contextID {
		t.Fatal("native declaration failed", e)
	}
	task, e := f.store.SaveTask(ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.owner, ActingUserID: f.owner, CityID: f.city, Query: "今晚找活动", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskActive, Filters: map[string]string{"timePreference": "tonight"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "今晚找活动"}}})
	if e != nil {
		t.Fatal("native task failed", e)
	}
	f.taskID = task.ID
	f.controller, e = agentfeature.NewController(f.config)
	if e != nil {
		t.Fatal(e)
	}
	f.service, e = NewService(pool, f.controller, false)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *nativeFixture) request(kind Selection) Request {
	r := Request{Access: f.access, Agent: agentcognitive.AgentReference{AgentID: f.agent, Principal: f.access.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, Selection: kind, Query: "今晚有什么活动", DeadlineAt: time.Now().UTC().Add(10 * time.Minute)}
	if kind == CurrentTask {
		r.Query = ""
		r.TaskID = f.taskID
	}
	if kind == SelectedCity {
		r.CityID = f.city
	}
	return r
}
func requireEmpty(t *testing.T, out Snapshot, e error) {
	t.Helper()
	if e == nil || out.Query != "" || out.SchemaVersion != "" || out.Sources != nil || out.Agent.AgentID != "" {
		t.Fatalf("denial released native data: %v", e)
	}
	if strings.Contains(e.Error(), "SELECT") || strings.Contains(e.Error(), "合成") {
		t.Fatal("private SQL/body leaked")
	}
}
func fixtureSourceRows(t *testing.T, f *nativeFixture) string {
	t.Helper()
	var raw string
	e := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'memory',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]') FROM agent_memories m WHERE owner_id=$1),
 'task',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY t.id),'[]') FROM agent_tasks t WHERE owner_account_id=$1),
 'declaration',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.context_id,p.relation),'[]') FROM person_contexts p WHERE person_account_id=$1),
 'metadata',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.agent_id),'[]') FROM agent_profiles p WHERE owner_id=$1),
 'city',(SELECT to_jsonb(c) FROM cities c WHERE id=$2),
 'context',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]') FROM contexts c WHERE city_id=$2),
 'account',(SELECT to_jsonb(a) FROM accounts a WHERE id=$1),'agent',(SELECT to_jsonb(a) FROM agents a WHERE principal_account_id=$1))::text`, f.owner, f.city).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestNativeCurrentContextReadsActualSourcesAndKeepsMemory(t *testing.T) {
	f := nativeFixtureFor(t)
	var memoryID string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&memoryID); e != nil {
		t.Fatal(e)
	}
	memory, e := f.store.PutOwnMemory(f.ctx, f.access, memoryID, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "explicit.long-term", Summary: "合成明确长期偏好", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(24 * time.Hour)})
	if e != nil || memory.Version != 1 {
		t.Fatal("actual long-term comparison fixture", e)
	}
	before := fixtureSourceRows(t, f)
	for _, kind := range []Selection{CurrentDeclaration, CurrentTask, SelectedCity} {
		t.Run(string(kind), func(t *testing.T) {
			out, e := f.service.ReadOwn(f.ctx, f.request(kind))
			if e != nil || out.City.ID != f.city || out.Agent.AgentID != f.agent || out.ModelAccess != "UNAVAILABLE" || out.MemoryPromotionAllowed || out.City.SourceFreshness != "unverified" || out.Purpose != "HUMAN_SELF_REVIEW" || out.TimePreference != "tonight" || out.ExpiresAt.Sub(out.ObservedAt) > MaxLease {
				t.Fatal("actual native current context", e)
			}
			renewed, e := f.service.RevalidateOwn(f.ctx, f.access, out)
			if e != nil || !renewed.ExpiresAt.Equal(out.ExpiresAt) || renewed.nativeDigest != out.nativeDigest {
				t.Fatal("current revalidation changed source/lease", e)
			}
			wire, e := json.Marshal(out)
			if e != nil || strings.Contains(string(wire), "session") || strings.Contains(string(wire), "authority") || strings.Contains(string(wire), "长期偏好") {
				t.Fatal("snapshot wire leaked authority or Memory")
			}
			if _, e = f.service.ReadForCognition(f.ctx, agentcognitive.ReadRequest{}); !errors.Is(e, agentcognitive.ErrUnavailable) {
				t.Fatal("native self read opened model")
			}
		})
	}
	if after := fixtureSourceRows(t, f); after != before {
		t.Fatal("temporary read modified native source or durable Memory")
	}
}
func TestNativeCurrentContextCurrentGuardsReject(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		args func(*nativeFixture) []any
	}{
		{"revoked_session", `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, func(f *nativeFixture) []any { return []any{f.owner} }},
		{"expired_session", `UPDATE sessions SET expires_at=created_at+interval '1 microsecond',idle_expires_at=created_at+interval '1 microsecond' WHERE account_id=$1`, func(f *nativeFixture) []any { return []any{f.owner} }},
		{"suspended_person", `UPDATE accounts SET status='suspended' WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.owner} }},
		{"retired_exact_agent", `UPDATE agents SET status='retired' WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.agent} }},
		{"missing_metadata", `DELETE FROM agent_profiles WHERE agent_id=$1`, func(f *nativeFixture) []any { return []any{f.agent} }},
		{"hidden_city", `UPDATE cities SET publication_status='hidden' WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.city} }},
		{"expired_city", `UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.city} }},
		{"paused_city_context", `UPDATE city_contexts SET status='paused' WHERE city_id=$1`, func(f *nativeFixture) []any { return []any{f.city} }},
		{"future_verification", `UPDATE cities SET verified_at=clock_timestamp()+interval '1 day' WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.city} }},
		{"infinite_verification", `UPDATE cities SET verified_at='infinity'::timestamptz WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.city} }},
		{"invalid_timezone", `UPDATE cities SET time_zone='unknown-city-time-zone' WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.city} }},
		{"server_local_timezone", `UPDATE cities SET time_zone='Local' WHERE id=$1`, func(f *nativeFixture) []any { return []any{f.city} }},
		{"deleted_declaration", `DELETE FROM person_contexts WHERE person_account_id=$1`, func(f *nativeFixture) []any { return []any{f.owner} }},
		{"public_declaration", `UPDATE person_contexts SET visibility='public' WHERE person_account_id=$1`, func(f *nativeFixture) []any { return []any{f.owner} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeFixtureFor(t)
			if _, e := f.pool.Exec(f.ctx, tc.sql, tc.args(f)...); e != nil {
				t.Fatal(e)
			}
			out, e := f.service.ReadOwn(f.ctx, f.request(CurrentDeclaration))
			requireEmpty(t, out, e)
		})
	}
	for _, name := range []string{"wrong_session_owner", "wrong_exact_agent", "organization", "business", "community"} {
		t.Run(name, func(t *testing.T) {
			f := nativeFixtureFor(t)
			r := f.request(CurrentDeclaration)
			switch name {
			case "wrong_session_owner":
				r.Access.WorkspacePrincipal.ID = f.other
				r.Agent.Principal = r.Access.WorkspacePrincipal
			case "wrong_exact_agent":
				r.Agent.AgentID = f.other
			case "organization":
				r.Agent.Principal.Type = actorref.Organization
				r.Access.WorkspacePrincipal.Type = actorref.Organization
				r.Agent.Role = agentruntime.OrganizationAgent
			case "business":
				r.Agent.Principal.Type = actorref.Business
				r.Access.WorkspacePrincipal.Type = actorref.Business
				r.Agent.Role = agentruntime.BusinessAgent
			case "community":
				r.Agent.Principal.Type = actorref.Community
				r.Access.WorkspacePrincipal.Type = actorref.Community
			}
			out, e := f.service.ReadOwn(f.ctx, r)
			requireEmpty(t, out, e)
		})
	}
}
func TestNativeCurrentContextTaskAndSnapshotInvalidate(t *testing.T) {
	for _, name := range []string{"task_other_owner", "task_completed", "task_deleted", "task_stale_tonight", "task_unknown_time"} {
		t.Run(name, func(t *testing.T) {
			f := nativeFixtureFor(t)
			switch name {
			case "task_other_owner":
				_, e := f.pool.Exec(f.ctx, `UPDATE agent_tasks SET owner_account_id=$2,acting_user_account_id=$2 WHERE id=$1`, f.taskID, f.other)
				if e != nil {
					t.Fatal(e)
				}
			case "task_completed":
				_, e := f.pool.Exec(f.ctx, `UPDATE agent_tasks SET status='COMPLETED' WHERE id=$1`, f.taskID)
				if e != nil {
					t.Fatal(e)
				}
			case "task_deleted":
				_, e := f.pool.Exec(f.ctx, `DELETE FROM agent_tasks WHERE id=$1`, f.taskID)
				if e != nil {
					t.Fatal(e)
				}
			case "task_stale_tonight":
				_, e := f.pool.Exec(f.ctx, `UPDATE agent_tasks SET created_at=clock_timestamp()-interval '3 days',updated_at=clock_timestamp()-interval '2 days' WHERE id=$1`, f.taskID)
				if e != nil {
					t.Fatal(e)
				}
			case "task_unknown_time":
				_, e := f.pool.Exec(f.ctx, `UPDATE agent_tasks SET filters='{"timePreference":"forever"}'::jsonb WHERE id=$1`, f.taskID)
				if e != nil {
					t.Fatal(e)
				}
			}
			out, e := f.service.ReadOwn(f.ctx, f.request(CurrentTask))
			requireEmpty(t, out, e)
		})
	}
	for _, name := range []string{"changed_task_same_time", "recreated_declaration_same_time", "changed_metadata", "off_then_on", "other_service", "tampered_query", "tampered_expiry", "tampered_source", "tampered_agent", "tampered_model_access"} {
		t.Run(name, func(t *testing.T) {
			f := nativeFixtureFor(t)
			kind := CurrentDeclaration
			if name == "changed_task_same_time" {
				kind = CurrentTask
			}
			out, e := f.service.ReadOwn(f.ctx, f.request(kind))
			if e != nil {
				t.Fatal(e)
			}
			svc := f.service
			switch name {
			case "changed_task_same_time":
				_, e = f.pool.Exec(f.ctx, `UPDATE agent_tasks SET query=query||'修订',conversation='[]'::jsonb,updated_at=updated_at WHERE id=$1`, f.taskID)
			case "recreated_declaration_same_time":
				var original time.Time
				e = f.pool.QueryRow(f.ctx, `SELECT created_at FROM person_contexts WHERE person_account_id=$1 AND context_id=$2 AND relation='current'`, f.owner, f.contextID).Scan(&original)
				if e != nil {
					t.Fatal(e)
				}
				tx, err := f.pool.Begin(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(f.ctx)
				if _, e = tx.Exec(f.ctx, `DELETE FROM person_contexts WHERE person_account_id=$1 AND context_id=$2 AND relation='current'`, f.owner, f.contextID); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(f.ctx, `INSERT INTO person_contexts(person_account_id,context_id,relation,visibility,created_at) VALUES($1,$2,'current','private',$3)`, f.owner, f.contextID, original); e != nil {
					t.Fatal(e)
				}
				e = tx.Commit(f.ctx)
			case "changed_metadata":
				_, e = f.pool.Exec(f.ctx, `UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, f.agent)
			case "off_then_on":
				e = f.controller.Disable(agentfeature.Enrichment)
				if e == nil {
					e = f.controller.Replace(f.controller.Revision(), f.config)
				}
			case "other_service":
				svc, e = NewService(f.pool, f.controller, false)
			case "tampered_query":
				out.Query = "已替换问句"
			case "tampered_expiry":
				out.ExpiresAt = out.ExpiresAt.Add(time.Minute)
			case "tampered_source":
				out.Sources[0].Version.Token = strings.Repeat("a", 64)
			case "tampered_agent":
				out.Agent.AgentID = f.other
			case "tampered_model_access":
				out.ModelAccess = "ALLOW"
			}
			if e != nil {
				t.Fatal(e)
			}
			renewed, e := svc.RevalidateOwn(f.ctx, f.access, out)
			requireEmpty(t, renewed, e)
		})
	}
}
func TestNativeCurrentContextConcurrentRevocationBeforeFinalPayload(t *testing.T) {
	for _, name := range []string{"session", "agent", "declaration", "task", "flag", "expired_request", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			f := nativeFixtureFor(t)
			r := f.request(CurrentDeclaration)
			if name == "task" {
				r = f.request(CurrentTask)
			}
			if name == "expired_request" {
				r.DeadlineAt = time.Now().UTC().Add(200 * time.Millisecond)
			}
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			reached := make(chan struct{})
			release := make(chan struct{})
			f.service.beforeFinal = func() { close(reached); <-release }
			type outcome struct {
				out Snapshot
				err error
			}
			done := make(chan outcome, 1)
			go func() { out, e := f.service.ReadOwn(ctx, r); done <- outcome{out, e} }()
			select {
			case <-reached:
			case got := <-done:
				t.Fatalf("failed before native race point: %v", got.err)
			case <-time.After(5 * time.Second):
				t.Fatal("native first read timeout")
			}
			var e error
			switch name {
			case "session":
				_, e = f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.owner)
			case "agent":
				_, e = f.pool.Exec(f.ctx, `UPDATE agents SET status='retired' WHERE id=$1`, f.agent)
			case "declaration":
				_, e = f.pool.Exec(f.ctx, `DELETE FROM person_contexts WHERE person_account_id=$1`, f.owner)
			case "task":
				_, e = f.pool.Exec(f.ctx, `UPDATE agent_tasks SET filters='{"timePreference":"anytime"}'::jsonb,updated_at=clock_timestamp() WHERE id=$1`, f.taskID)
			case "flag":
				e = f.controller.Disable(agentfeature.Enrichment)
			case "expired_request":
				time.Sleep(250 * time.Millisecond)
			case "cancelled":
				cancel()
			}
			close(release)
			if e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				requireEmpty(t, got.out, got.err)
			case <-time.After(5 * time.Second):
				t.Fatal("native final payload did not return")
			}
		})
	}
}
func TestNativeCurrentContextConnectionTimeZoneCanonicalization(t *testing.T) {
	f := nativeFixtureFor(t)
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.MaxConns = 2
	pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	a, e := pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Release()
	b, e := pool.Acquire(f.ctx)
	if e != nil {
		a.Release()
		t.Fatal(e)
	}
	defer b.Release()
	if _, e = a.Exec(f.ctx, `SET TIME ZONE 'Asia/Shanghai'`); e != nil {
		t.Fatal(e)
	}
	if _, e = b.Exec(f.ctx, `SET TIME ZONE 'America/New_York'`); e != nil {
		t.Fatal(e)
	}
	var zoneA, zoneB string
	if e = a.QueryRow(f.ctx, `SHOW TIME ZONE`).Scan(&zoneA); e != nil {
		t.Fatal(e)
	}
	if e = b.QueryRow(f.ctx, `SHOW TIME ZONE`).Scan(&zoneB); e != nil {
		t.Fatal(e)
	}
	if zoneA == zoneB {
		t.Fatal("distinct actual connection zones required")
	}
	svc, e := NewService(pool, f.controller, false)
	if e != nil {
		t.Fatal(e)
	}
	b.Release()
	var held *pgxpool.Conn
	// The first source read uses B. Hold B and release A before the final read,
	// forcing distinct configured connections without modifying global timezone.
	svc.beforeFinal = func() {
		var err error
		held, err = pool.Acquire(f.ctx)
		if err != nil {
			panic("cannot hold fixture connection")
		}
		a.Release()
	}
	out, e := svc.ReadOwn(f.ctx, f.request(CurrentTask))
	if held != nil {
		held.Release()
	}
	if e != nil {
		t.Fatal("different connection zones caused false source conflict", e)
	}
	svc.beforeFinal = nil
	again, e := svc.RevalidateOwn(f.ctx, f.access, out)
	if e != nil || again.nativeDigest != out.nativeDigest || !again.ExpiresAt.Equal(out.ExpiresAt) {
		t.Fatal("timezone revalidation unstable", e)
	}
	var restored string
	conn, e := pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = conn.QueryRow(f.ctx, `SHOW TIME ZONE`).Scan(&restored); e != nil {
		t.Fatal(e)
	}
	conn.Release()
	if restored != "Asia/Shanghai" && restored != "America/New_York" {
		t.Fatal("SET LOCAL changed connection-global configuration", fmt.Sprintf("%q", restored))
	}
}

func TestNativeCurrentContextSnapshotClockDomains(t *testing.T) {
	f := nativeFixtureFor(t)
	request := f.request(SelectedCity)
	snapshot, err := f.service.ReadOwn(f.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.service.native.load(f.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	// Real current native session, source and sealed snapshot. Only the host
	// argument is controlled; it grants no authority and changes no DB clock.
	if err = validateSnapshotClocks(snapshot, current.now, snapshot.ObservedAt.Add(-time.Second)); err != nil {
		t.Fatal("A database-issued current snapshot was rejected by an earlier host clock", err)
	}
	future := cloneSnapshot(snapshot)
	future.ObservedAt = current.now.Add(time.Second)
	if err = validateSnapshotClocks(future, current.now, snapshot.ObservedAt); !errors.Is(err, ErrExpired) {
		t.Fatal("Future database issuance was accepted", err)
	}
	if _, err = f.service.RevalidateOwn(f.ctx, f.access, snapshot); err != nil {
		t.Fatal("Current native revalidation failed", err)
	}
}

func TestNativeCurrentContextLeaseSourceChangesAndFreshness(t *testing.T) {
	for _, name := range []string{"deadline_expired", "city_changed_same_time", "account_generation_changed", "metadata_recreated", "other_session", "future_city_time", "future_declaration_time", "review_needed", "known_current"} {
		t.Run(name, func(t *testing.T) {
			f := nativeFixtureFor(t)
			r := f.request(CurrentDeclaration)
			if name == "future_city_time" {
				if _, e := f.pool.Exec(f.ctx, `UPDATE cities SET updated_at=clock_timestamp()+interval '1 day' WHERE id=$1`, f.city); e != nil {
					t.Fatal(e)
				}
				out, e := f.service.ReadOwn(f.ctx, r)
				requireEmpty(t, out, e)
				return
			}
			if name == "future_declaration_time" {
				if _, e := f.pool.Exec(f.ctx, `UPDATE person_contexts SET created_at=clock_timestamp()+interval '1 day' WHERE person_account_id=$1`, f.owner); e != nil {
					t.Fatal(e)
				}
				out, e := f.service.ReadOwn(f.ctx, r)
				requireEmpty(t, out, e)
				return
			}
			if name == "review_needed" || name == "known_current" {
				sql := `UPDATE cities SET verified_at=updated_at-interval '1 hour' WHERE id=$1`
				expected := "review_needed"
				if name == "known_current" {
					sql = `UPDATE cities SET verified_at=updated_at WHERE id=$1`
					expected = "current"
				}
				if _, e := f.pool.Exec(f.ctx, sql, f.city); e != nil {
					t.Fatal(e)
				}
				out, e := f.service.ReadOwn(f.ctx, r)
				if e != nil || out.City.SourceFreshness != expected {
					t.Fatal("native freshness classification", e)
				}
				return
			}
			if name == "deadline_expired" {
				r.DeadlineAt = time.Now().UTC().Add(250 * time.Millisecond)
			}
			out, e := f.service.ReadOwn(f.ctx, r)
			if e != nil {
				t.Fatal(e)
			}
			access := f.access
			switch name {
			case "deadline_expired":
				time.Sleep(300 * time.Millisecond)
			case "city_changed_same_time":
				_, e = f.pool.Exec(f.ctx, `UPDATE cities SET name=name||'修改',updated_at=updated_at WHERE id=$1`, f.city)
			case "account_generation_changed":
				_, e = f.pool.Exec(f.ctx, `UPDATE accounts SET status=status WHERE id=$1`, f.owner)
			case "metadata_recreated":
				var createdAt time.Time
				e = f.pool.QueryRow(f.ctx, `SELECT created_at FROM agent_profiles WHERE agent_id=$1`, f.agent).Scan(&createdAt)
				if e != nil {
					t.Fatal(e)
				}
				tx, err := f.pool.Begin(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(f.ctx)
				if _, e = tx.Exec(f.ctx, `DELETE FROM agent_profiles WHERE agent_id=$1`, f.agent); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(f.ctx, `INSERT INTO agent_profiles(agent_id,owner_type,owner_id,created_at,updated_at) VALUES($1,'PERSON',$2,$3,$3)`, f.agent, f.owner, createdAt); e != nil {
					t.Fatal(e)
				}
				e = tx.Commit(f.ctx)
			case "other_session":
				_, digest, err := identity.NewToken()
				if err != nil {
					t.Fatal(err)
				}
				_, e = f.pool.Exec(f.ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.owner, digest[:])
				access.SessionDigest = digest
			}
			if e != nil {
				t.Fatal(e)
			}
			again, e := f.service.RevalidateOwn(f.ctx, access, out)
			requireEmpty(t, again, e)
		})
	}
}
