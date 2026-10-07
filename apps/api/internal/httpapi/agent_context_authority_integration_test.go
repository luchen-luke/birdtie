package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

// Real registered HTTP and a real PostgreSQL relation wait. Every identity,
// organization and task is synthetic in a separately owned migrated database.
func TestContextAuthorityHTTPOrganizationRevokedDuringCityRead(t *testing.T) {
	modelEgressHTTPOwnedDatabase(t)
	f := contextBuilderHTTPNative(t)
	var org, membership string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&org); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner')`, org, f.accountIDs[0])
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'admin') RETURNING id`, org, f.accountIDs[1]).Scan(&membership); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Error(err)
		}
	})
	task, err := f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "organization", PrincipalID: f.accountIDs[2], ActingUserID: f.accountIDs[1], CityID: f.city, Query: "组织公开活动", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskCompleted, Filters: map[string]string{"currentQuery": "组织公开活动"}, Conversation: []agentworkspace.Message{}})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	var lockPID int
	if err = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&lockPID); err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(f.ctx, `LOCK TABLE cities IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	r := privateProfileHTTPRequest("GET", "/v1/me/agent-tasks/"+task.ID, "", f.tokens[1], "application/json")
	r.Header.Set("X-Birdtie-Organization-Workspace", org)
	rw := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); f.handler.ServeHTTP(rw, r) }()
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND (query LIKE '%FROM cities WHERE id =%' OR (query LIKE '%LOCK TABLE organizations%' AND query LIKE '%cities%')))`, lockPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("registered HTTP did not reach the actual GetCity relation wait")
	}
	if err = f.store.RevokeMember(f.ctx, f.accountIDs[0], org, membership); err != nil {
		t.Fatal(err)
	}
	if err = lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("registered HTTP did not finish")
	}
	if rw.Code != 403 {
		t.Fatalf("revoked organization response status=%d want403", rw.Code)
	}
	if strings.Contains(rw.Body.String(), task.ID) || strings.Contains(rw.Body.String(), "组织公开活动") {
		t.Fatal("denied response returned old organization task")
	}
}

func TestContextAuthorityNativeCurrentTaskSourceAndAuthorityABA(t *testing.T) {
	for _, name := range []string{"sourceABA", "taskABA", "orgABA", "agentABA", "sessionRevoked", "seenShortIdleCannotRenew"} {
		t.Run(name, func(t *testing.T) {
			f, org, task := contextAuthorityHTTPFixture(t)
			digest := sha256.Sum256([]byte(f.tokens[1]))
			actor, err := f.store.Authenticate(f.ctx, digest)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := f.store.CaptureOrganizationTaskAuthority(f.ctx, digest, actor, org, f.accountIDs[2], "admin")
			if err != nil {
				t.Fatal(err)
			}
			if name == "seenShortIdleCannotRenew" {
				f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '4 seconds' WHERE token_sha256=$1`, digest[:])
			}
			current, receipt, err := f.store.ReadOrganizationTaskContext(f.ctx, initial, task.ID)
			if err != nil {
				t.Fatal("native current capture", err)
			}
			want := error(arp.ErrChanged)
			switch name {
			case "sourceABA":
				f.exec(`UPDATE places SET summary='changed' WHERE id=$1`, f.place)
				f.exec(`UPDATE places SET summary='' WHERE id=$1`, f.place)
			case "taskABA":
				changed := current
				changed.Status = agentworkspace.TaskActive
				if _, err = f.store.UpdateTask(f.ctx, changed); err != nil {
					t.Fatal(err)
				}
				if _, err = f.store.UpdateTask(f.ctx, current); err != nil {
					t.Fatal(err)
				}
			case "orgABA":
				f.exec(`UPDATE organizations SET name=name||'changed' WHERE id=$1`, org)
				f.exec(`UPDATE organizations SET name='Synthetic private HTTP boundary organization' WHERE id=$1`, org)
				want = organization.ErrForbidden
			case "agentABA":
				f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[2])
				f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[2])
				want = organization.ErrForbidden
			case "sessionRevoked":
				if err = f.store.RevokeSession(f.ctx, digest); err != nil {
					t.Fatal(err)
				}
				want = identity.ErrUnauthorized
			case "seenShortIdleCannotRenew":
				if _, err = f.store.Authenticate(f.ctx, digest); err != nil {
					t.Fatal(err)
				}
				time.Sleep(4200 * time.Millisecond)
				want = organization.ErrForbidden
			}
			if err = f.store.RevalidateOrganizationTaskContext(f.ctx, receipt, []agentworkspace.Task{current}); !errors.Is(err, want) {
				t.Fatalf("%s stale native receipt err=%v want %v", name, err, want)
			}
		})
	}
}

func contextAuthorityHTTPFixture(t *testing.T) (*contextBuilderHTTPFixture, string, agentworkspace.Task) {
	t.Helper()
	modelEgressHTTPOwnedDatabase(t)
	f := contextBuilderHTTPNative(t)
	var org string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&org); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, org, f.accountIDs[0], f.accountIDs[1])
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Error(err)
		}
	})
	task, err := f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "organization", PrincipalID: f.accountIDs[2], ActingUserID: f.accountIDs[1], CityID: f.city, Query: "组织公开活动", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskCompleted, Filters: map[string]string{"currentQuery": "组织公开活动"}, Conversation: []agentworkspace.Message{}})
	if err != nil {
		t.Fatal(err)
	}
	return f, org, task
}

func TestContextAuthorityHTTPRegisteredOrganizationCurrentAndIdentity(t *testing.T) {
	f, org, task := contextAuthorityHTTPFixture(t)
	path := "/v1/me/agent-tasks/" + task.ID
	w := f.request(t, f.handler, "GET", path, "", f.tokens[1], 200, &org)
	if !strings.Contains(w.Body.String(), task.ID) || strings.Contains(w.Body.String(), "contextSnapshot") {
		t.Fatal("original Task lost or snapshot exposed")
	}
	f.request(t, f.handler, "GET", "/v1/me/agent-tasks", "", f.tokens[1], 200, &org)
	f.request(t, f.handler, "GET", path, "", f.tokens[3], 403, &org)
	f.request(t, f.handler, "GET", path, "", "", 401, &org)
	f.request(t, f.handler, "GET", path, "", "forged-local-session", 401, &org)
	otherOrg := task.ID
	f.request(t, f.handler, "GET", path, "", f.tokens[1], 403, &otherOrg)
	f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找羽毛球活动"}`, f.tokens[1], 200, &org)
}

func TestContextAuthorityNativeCapturedRoleABANormalIdleAndOpaque(t *testing.T) {
	f, org, task := contextAuthorityHTTPFixture(t)
	digest := sha256.Sum256([]byte(f.tokens[1]))
	actor, err := f.store.Authenticate(f.ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := f.store.CaptureOrganizationTaskAuthority(f.ctx, digest, actor, org, f.accountIDs[2], "admin")
	if err != nil {
		t.Fatal(err)
	}
	current, receipt, err := f.store.ReadOrganizationTaskContext(f.ctx, initial, task.ID)
	if err != nil {
		t.Fatal("native original Task capture", err)
	}
	if _, err = json.Marshal(receipt); err == nil {
		t.Fatal("snapshot serialized")
	}
	if _, err = f.store.Authenticate(f.ctx, digest); err != nil {
		t.Fatal(err)
	}
	if err = f.store.RevalidateOrganizationTaskContext(f.ctx, receipt, []agentworkspace.Task{current}); err != nil {
		t.Fatal("normal idle refresh must preserve current snapshot", err)
	}
	var member string
	if err = f.pool.QueryRow(f.ctx, `SELECT id FROM organization_memberships WHERE organization_id=$1 AND user_account_id=$2`, org, actor.ID).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if err = f.store.RevokeMember(f.ctx, f.accountIDs[0], org, member); err != nil {
		t.Fatal(err)
	}
	invite, err := f.store.InviteMember(f.ctx, f.accountIDs[0], org, actor.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.AcceptInvitation(f.ctx, actor.ID, invite.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.store.RevalidateOrganizationTaskContext(f.ctx, receipt, []agentworkspace.Task{current}); !errors.Is(err, organization.ErrForbidden) {
		t.Fatalf("original role ABA receipt must deny: %v", err)
	}
	f.request(t, f.handler, "GET", "/v1/me/agent-tasks/"+task.ID, "", f.tokens[1], 200, &org)
}

func TestContextAuthorityHTTPOrganizationPostSourceABAAfterSearch(t *testing.T) {
	f, org, _ := contextAuthorityHTTPFixture(t)
	// The hook is confined to this owned database and blocks the real final
	// Task UPDATE after SearchActivities has materialized its response.
	f.exec(`CREATE FUNCTION context_authority_post_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.principal_type='organization' AND NEW.status='COMPLETED' AND NEW.filters ? 'resultIDs' THEN PERFORM pg_advisory_xact_lock(823014); END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER context_authority_post_wait BEFORE UPDATE ON agent_tasks FOR EACH ROW EXECUTE FUNCTION context_authority_post_wait()`)
	t.Cleanup(func() {
		f.exec(`DROP TRIGGER context_authority_post_wait ON agent_tasks`)
		f.exec(`DROP FUNCTION context_authority_post_wait()`)
	})
	lock, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	var pid int
	if err = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(f.ctx, `SELECT pg_advisory_xact_lock(823014)`); err != nil {
		t.Fatal(err)
	}
	r := privateProfileHTTPRequest("POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找羽毛球活动"}`, f.tokens[1], "application/json")
	r.Header.Set("X-Birdtie-Organization-Workspace", org)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); f.handler.ServeHTTP(w, r) }()
	waitUntil := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(waitUntil) {
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%UPDATE agent_tasks SET intent=%')`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("POST did not reach real post-search Task UPDATE advisory wait")
	}
	f.exec(`UPDATE activities SET title=title||' changed' WHERE id=$1`, f.public)
	f.exec(`UPDATE activities SET title='合成羽毛球 public' WHERE id=$1`, f.public)
	if err = lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("POST did not finish")
	}
	if w.Code != 409 {
		t.Fatalf("post-search source ABA status=%d want409", w.Code)
	}
	if strings.Contains(w.Body.String(), f.public) {
		t.Fatal("old source escaped rejected POST")
	}
}

type contextAuthorityReadBarrier struct {
	*postgres.Store
	once      sync.Once
	afterRead func()
}

func (b *contextAuthorityReadBarrier) GetTask(ctx context.Context, owner, id string) (agentworkspace.Task, error) {
	task, err := b.Store.GetTask(ctx, owner, id)
	if err == nil {
		b.once.Do(b.afterRead)
	}
	return task, err
}
func (b *contextAuthorityReadBarrier) ReadOrganizationTaskContext(ctx context.Context, snapshot agentruntime.ContextSnapshot, id string) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	task, next, err := b.Store.ReadOrganizationTaskContext(ctx, snapshot, id)
	if err == nil {
		b.once.Do(b.afterRead)
	}
	return task, next, err
}

func TestContextAuthorityHTTPOrganizationFollowupTaskABAWhileUpdateWaits(t *testing.T) {
	f, org, task := contextAuthorityHTTPFixture(t)
	lock, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	var pid int
	if err = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	read := make(chan struct{})
	barrier := &contextAuthorityReadBarrier{Store: f.store, afterRead: func() {
		if _, e := lock.Exec(f.ctx, `SELECT id FROM agent_tasks WHERE id=$1 FOR UPDATE`, task.ID); e != nil {
			panic(e)
		}
		close(read)
	}}
	f.handler = New(f.catalog, f.store, nil, f.store, barrier, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
	r := privateProfileHTTPRequest("POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找羽毛球活动","taskId":"`+task.ID+`"}`, f.tokens[1], "application/json")
	r.Header.Set("X-Birdtie-Organization-Workspace", org)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); f.handler.ServeHTTP(w, r) }()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("actual native Task read did not complete before update barrier")
	}
	blocked := false
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM agent_tasks WHERE id=%' AND query LIKE '%FOR UPDATE%')`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("followup did not reach original Task FOR UPDATE wait")
	}
	if _, err = lock.Exec(f.ctx, `UPDATE agent_tasks SET status='ACTIVE' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(f.ctx, `UPDATE agent_tasks SET status='COMPLETED' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if err = lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("followup did not finish")
	}
	if w.Code != 409 {
		t.Fatalf("Task ABA during original UPDATE wait status=%d want409", w.Code)
	}
}

func TestContextAuthorityHTTPOrganizationQueryDoesNotInheritPersonalInvitation(t *testing.T) {
	f, org, _ := contextAuthorityHTTPFixture(t)
	w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找羽毛球活动"}`, f.tokens[1], 200, &org)
	if !strings.Contains(w.Body.String(), f.public) {
		t.Fatal("public Activity missing")
	}
	if strings.Contains(w.Body.String(), f.private) {
		t.Fatal("Organization City-context query inherited actor's Personal private Activity")
	}
	var response struct {
		Data agentworkspace.Results `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	get := f.request(t, f.handler, "GET", "/v1/me/agent-tasks/"+response.Data.TaskID, "", f.tokens[1], 200, &org)
	if strings.Contains(get.Body.String(), f.private) {
		t.Fatal("restored Organization Task inherited Personal private Activity")
	}
}

func TestContextAuthorityHTTPOrganizationAuditWaitExpiryRollsBackTask(t *testing.T) {
	f, org, _ := contextAuthorityHTTPFixture(t)
	f.exec(`CREATE FUNCTION context_authority_audit_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='agent_task' AND NEW.action='create' THEN PERFORM pg_advisory_xact_lock(823015); END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER context_authority_audit_wait BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION context_authority_audit_wait()`)
	t.Cleanup(func() {
		f.exec(`DROP TRIGGER context_authority_audit_wait ON audit_events`)
		f.exec(`DROP FUNCTION context_authority_audit_wait()`)
	})
	var before string
	query := `SELECT jsonb_build_object('tasks',(SELECT coalesce(jsonb_agg(jsonb_build_object('row',to_jsonb(t),'xmin',t.xmin::text) ORDER BY id),'[]') FROM agent_tasks t WHERE owner_account_id=$1),'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]') FROM audit_events a WHERE actor_account_id=ANY($2::uuid[])))::text`
	if err := f.pool.QueryRow(f.ctx, query, f.accountIDs[2], f.accountIDs).Scan(&before); err != nil {
		t.Fatal(err)
	}
	lock, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	var pid int
	if err = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(f.ctx, `SELECT pg_advisory_xact_lock(823015)`); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE cities SET expires_at=clock_timestamp()+interval '4 seconds' WHERE id=$1`, f.city)
	r := privateProfileHTTPRequest("POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找羽毛球活动"}`, f.tokens[1], "application/json")
	r.Header.Set("X-Birdtie-Organization-Workspace", org)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); f.handler.ServeHTTP(w, r) }()
	blocked := false
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%INSERT INTO audit_events%')`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("Task create did not reach real audit INSERT wait")
	}
	time.Sleep(4200 * time.Millisecond)
	if err = lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("audit wait did not finish")
	}
	if w.Code != 403 {
		t.Fatalf("expired source after audit wait status=%d want403", w.Code)
	}
	var after string
	if err = f.pool.QueryRow(f.ctx, query, f.accountIDs[2], f.accountIDs).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("rejected Task create changed original Task/audit rows or xmin")
	}
}

func TestContextAuthorityHTTPMissingNativePortIsUnavailable(t *testing.T) {
	s, mux, tasks, token := httpActionSafetyServer(t, []organization.Organization{{ID: httpActionSafetyOrgID, AccountID: httpActionSafetyOrgAccount, Role: "admin"}})
	s.agent = tasks // Deliberately remove ONLY the synthetic native port.
	code, body := httpActionSafetyCall(t, mux, token, httpActionSafetyOrgID, "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"帮我发布活动"}`)
	if code != 503 || !strings.Contains(string(body), "organization_context_unavailable") || tasks.task.ID != "" {
		t.Fatalf("missing native proof fallback code=%d TaskWritten=%t", code, tasks.task.ID != "")
	}
}

func TestContextAuthorityHTTPOrganizationFollowupDifferentCurrentMember(t *testing.T) {
	f, org, task := contextAuthorityHTTPFixture(t)
	f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找羽毛球活动","taskId":"`+task.ID+`"}`, f.tokens[0], 200, &org)
}

func TestContextAuthorityNativeFinalSessionWaitRechecksSourceAndClock(t *testing.T) {
	for _, name := range []string{"sourceABA", "cityNaturalExpiry", "sessionNaturalExpiry"} {
		t.Run(name, func(t *testing.T) {
			f, org, task := contextAuthorityHTTPFixture(t)
			digest := sha256.Sum256([]byte(f.tokens[1]))
			actor, err := f.store.Authenticate(f.ctx, digest)
			if err != nil {
				t.Fatal(err)
			}
			if name == "cityNaturalExpiry" {
				f.exec(`UPDATE cities SET expires_at=clock_timestamp()+interval '4 seconds' WHERE id=$1`, f.city)
			}
			if name == "sessionNaturalExpiry" {
				f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '4 seconds' WHERE token_sha256=$1`, digest[:])
			}
			initial, err := f.store.CaptureOrganizationTaskAuthority(f.ctx, digest, actor, org, f.accountIDs[2], "admin")
			if err != nil {
				t.Fatal(err)
			}
			current, receipt, err := f.store.ReadOrganizationTaskContext(f.ctx, initial, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			lock, err := f.pool.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback(context.Background())
			var pid int
			if err = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			if _, err = lock.Exec(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				done <- f.store.RevalidateOrganizationTaskContext(f.ctx, receipt, []agentworkspace.Task{current})
			}()
			blocked := false
			until := time.Now().Add(5 * time.Second)
			for time.Now().Before(until) {
				if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%FROM sessions WHERE token_sha256=%' AND query LIKE '%FOR SHARE%')`, pid).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("final native revalidation did not reach actual Session row lock")
			}
			want := error(arp.ErrChanged)
			if name == "sourceABA" {
				f.exec(`UPDATE places SET name=name||' changed' WHERE id=$1`, f.place)
				f.exec(`UPDATE places SET name='合成 Context 地点' WHERE id=$1`, f.place)
			} else {
				time.Sleep(4200 * time.Millisecond)
				want = organization.ErrForbidden
				if name == "sessionNaturalExpiry" {
					want = identity.ErrUnauthorized
				}
			}
			if err = lock.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
			case <-time.After(8 * time.Second):
				t.Fatal("final native check did not finish")
			}
			if !errors.Is(err, want) {
				t.Fatalf("%s final wait err=%v want%v", name, err, want)
			}
		})
	}
}

func TestContextAuthorityHTTPCurrentOperatorAuditUsesCurrentMemberAndKeepsCreator(t *testing.T) {
	f, org, task := contextAuthorityHTTPFixture(t)
	creatorA, operatorB := f.accountIDs[1], f.accountIDs[0]
	if task.ActingUserID != creatorA || creatorA == operatorB {
		t.Fatal("original native fixture did not retain distinct creator A and current operator B")
	}
	var creates, originalCreatorCreates int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)::integer,
 count(*) FILTER(WHERE actor_account_id=$2::uuid)::integer
 FROM audit_events WHERE resource_type='agent_task' AND resource_id=$1 AND action='create'`, task.ID, creatorA).Scan(&creates, &originalCreatorCreates); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || originalCreatorCreates != 1 {
		t.Fatal("original task create audit does not identify creator A exactly once")
	}

	// Existing native15 test already runs this registered request as operator B
	// and asserts 200. The additional native assertions inspect its REAL audit.
	w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks",
		`{"query":"找羽毛球活动","taskId":"`+task.ID+`"}`, f.tokens[0], 200, &org)
	trace := w.Header().Get("X-Request-ID")
	if trace == "" {
		t.Fatal("registered request did not expose its original correlation ID")
	}
	var total, attributedToOperatorB, attributedToCreatorA int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)::integer,
 count(*) FILTER(WHERE actor_account_id=$3::uuid)::integer,
 count(*) FILTER(WHERE actor_account_id=$4::uuid)::integer
 FROM audit_events WHERE resource_type='agent_task' AND resource_id=$1
 AND action='update' AND request_id=$2`, task.ID, trace, operatorB, creatorA).Scan(&total, &attributedToOperatorB, &attributedToCreatorA); err != nil {
		t.Fatal(err)
	}
	// Do not prescribe the number of existing internal updates. Every actual
	// update committed for this request must identify its current human actor.
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY current_operator_audit total=%d operatorB=%d creatorA=%d request_id=%s", total, attributedToOperatorB, attributedToCreatorA, trace)
	if total < 1 || attributedToOperatorB != total || attributedToCreatorA != 0 {
		t.Errorf("registered member B update audit was attributed to another actor: total=%d B=%d A=%d", total, attributedToOperatorB, attributedToCreatorA)
	}
	var retainedCreator string
	if err := f.pool.QueryRow(f.ctx, `SELECT acting_user_account_id::text FROM agent_tasks WHERE id=$1`, task.ID).Scan(&retainedCreator); err != nil {
		t.Fatal(err)
	}
	if retainedCreator != creatorA {
		t.Error("updating audit attribution rewrote original task creator")
	}
	var currentCreates, retainedCreatorCreates int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)::integer,
 count(*) FILTER(WHERE actor_account_id=$2::uuid)::integer
 FROM audit_events WHERE resource_type='agent_task' AND resource_id=$1 AND action='create'`, task.ID, creatorA).Scan(&currentCreates, &retainedCreatorCreates); err != nil {
		t.Fatal(err)
	}
	if currentCreates != creates || retainedCreatorCreates != originalCreatorCreates {
		t.Error("current-operator update changed original creator audit history")
	}

	// The same creator A can still make a later authorized invocation; this new
	// request must record A, without globally replacing all audit actors by B.
	w = f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks",
		`{"query":"找羽毛球活动","taskId":"`+task.ID+`"}`, f.tokens[1], 200, &org)
	trace = w.Header().Get("X-Request-ID")
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)::integer,
 count(*) FILTER(WHERE actor_account_id=$3::uuid)::integer,
 count(*) FILTER(WHERE actor_account_id=$4::uuid)::integer
 FROM audit_events WHERE resource_type='agent_task' AND resource_id=$1
 AND action='update' AND request_id=$2`, task.ID, trace, creatorA, operatorB).Scan(&total, &attributedToCreatorA, &attributedToOperatorB); err != nil {
		t.Fatal(err)
	}
	if total < 1 || attributedToCreatorA != total || attributedToOperatorB != 0 {
		t.Error("later creator A invocation did not retain its own current-operator audit")
	}
}

func TestContextAuthorityHTTPCurrentOperatorAuditIdentityCannotComeFromBodyOrRevokedMember(t *testing.T) {
	f, org, task := contextAuthorityHTTPFixture(t)
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'task',(SELECT jsonb_build_object('row',to_jsonb(t),'xmin',t.xmin::text) FROM agent_tasks t WHERE t.id=$1),
 'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM audit_events a WHERE a.resource_type='agent_task' AND a.resource_id=$1::uuid::text))::text`, task.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	// The existing public command has no actingUserId input. Adding one cannot
	// repair audit attribution or grant a claimed actor authority.
	f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks",
		`{"query":"找羽毛球活动","taskId":"`+task.ID+`","actingUserId":"`+f.accountIDs[1]+`"}`, f.tokens[0], 400, &org)
	if snapshot() != before {
		t.Error("denied claimed actor changed original Task row/xmin or committed audit")
	}
	var membership string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM organization_memberships WHERE organization_id=$1 AND user_account_id=$2 AND status='active'`, org, f.accountIDs[1]).Scan(&membership); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RevokeMember(f.ctx, f.accountIDs[0], org, membership); err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks",
		`{"query":"找羽毛球活动","taskId":"`+task.ID+`"}`, f.tokens[1], 403, &org)
	if snapshot() != before {
		t.Error("revoked creator changed original Task row/xmin or committed update audit")
	}
}

func TestContextAuthorityHTTPCurrentOperatorNewTaskAuditKeepsActualCreator(t *testing.T) {
	f, org, _ := contextAuthorityHTTPFixture(t)
	w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks",
		`{"query":"找羽毛球活动"}`, f.tokens[0], 200, &org)
	var total, actorCreates int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)::integer,
 count(*) FILTER(WHERE actor_account_id=$2::uuid)::integer
 FROM audit_events WHERE resource_type='agent_task' AND action='create' AND request_id=$1`, w.Header().Get("X-Request-ID"), f.accountIDs[0]).Scan(&total, &actorCreates); err != nil {
		t.Fatal(err)
	}
	if total != 1 || actorCreates != 1 {
		t.Error("new task create audit lost its current original creator")
	}
}
