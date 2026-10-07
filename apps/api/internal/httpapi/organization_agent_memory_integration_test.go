package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func orgMemoryHTTPFixture(t *testing.T) (*privateProfileHTTPDBFixture, string) {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&id); e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, id, f.accountIDs[0], f.accountIDs[1])
	t.Cleanup(func() {
		if _, e := f.pool.Exec(context.Background(), `DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error("owned Org Memory HTTP audit cleanup", e)
		}
	})
	return f, id
}
func orgMemoryHTTPInput() agentorganizationmemory.PutInput {
	return agentorganizationmemory.PutInput{Category: agentmemory.OrgFAQ, Key: "native-http", Summary: "合成管理员内部声明，不是核验FAQ", StructuredValue: json.RawMessage(`{"note":"合成内容","tags":["测试"]}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
}
func orgMemoryHTTPBody(t *testing.T, in agentorganizationmemory.PutInput) string {
	t.Helper()
	raw, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func orgMemoryHTTPID(t *testing.T, f *privateProfileHTTPDBFixture) string {
	t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func orgMemoryHTTPView(t *testing.T, w *httptest.ResponseRecorder) agentorganizationmemory.View {
	t.Helper()
	var envelope struct {
		Data agentorganizationmemory.View `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	return envelope.Data
}

func TestOrganizationMemoryRegisteredHTTPNativeCRUDAndCurrentRoles(t *testing.T) {
	f, org := orgMemoryHTTPFixture(t)
	path := "/v1/me/organizations/" + org + "/agent-memories"
	id := orgMemoryHTTPID(t, f)
	in := orgMemoryHTTPInput()
	body := orgMemoryHTTPBody(t, in)
	w := f.request(t, f.handler, "PUT", path+"/"+id, body, f.tokens[0], 200, nil)
	first := orgMemoryHTTPView(t, w)
	if first.Memory.ID != id || first.Memory.OwnerType != "ORGANIZATION" || first.Memory.OwnerID != f.accountIDs[2] || first.Memory.AgentID != f.agentIDs[2] || first.Memory.Version != 1 || first.Disclaimer != agentorganizationmemory.Disclaimer {
		t.Fatal("registered route did not bind current native Org/Agent")
	}
	retry := orgMemoryHTTPView(t, f.request(t, f.handler, "PUT", path+"/"+id, body, f.tokens[1], 200, nil))
	if !reflect.DeepEqual(first, retry) {
		t.Fatal("exact HTTP retry changed content")
	}
	var audit int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_audit_events WHERE organization_id=$1 AND resource_type='organization_memory'`, org).Scan(&audit); e != nil || audit != 1 {
		t.Fatal("retry duplicate audit", audit, e)
	}
	for _, index := range []int{0, 1} {
		f.request(t, f.handler, "GET", path, "", f.tokens[index], 200, nil)
	}
	f.request(t, f.handler, "GET", path, "", "", 401, nil)
	f.request(t, f.handler, "GET", path, "", "unknown-session", 401, nil)
	for _, index := range []int{2, 3} {
		f.request(t, f.handler, "GET", path, "", f.tokens[index], 403, nil)
	}
	f.request(t, f.handler, "GET", path, "", f.newSession(f.accountIDs[0], true, false), 401, nil)
	f.request(t, f.handler, "GET", path, "", f.newSession(f.accountIDs[0], false, true), 401, nil)
	for _, role := range []string{"member", "moderator"} {
		f.exec(`UPDATE organization_memberships SET role=$3 WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1], role)
		f.request(t, f.handler, "GET", path, "", f.tokens[1], 403, nil)
		f.request(t, f.handler, "PUT", path+"/"+id, body, f.tokens[1], 403, nil)
		f.request(t, f.handler, "DELETE", path+"/"+id, `{"expectedVersion":1}`, f.tokens[1], 403, nil)
	}
	f.exec(`UPDATE organization_memberships SET role='admin',status='removed' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1])
	f.request(t, f.handler, "GET", path, "", f.tokens[1], 403, nil)
	f.exec(`UPDATE organization_memberships SET status='active' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1])
	in.ExpectedVersion = 1
	in.Summary = "合成当前新版本"
	second := orgMemoryHTTPView(t, f.request(t, f.handler, "PUT", path+"/"+id, orgMemoryHTTPBody(t, in), f.tokens[1], 200, nil))
	if second.Memory.Version != 2 {
		t.Fatal("actual version did not advance")
	}
	f.request(t, f.handler, "PUT", path+"/"+id, body, f.tokens[0], 409, nil)
	deleted := orgMemoryHTTPView(t, f.request(t, f.handler, "DELETE", path+"/"+id, `{"expectedVersion":2}`, f.tokens[0], 200, nil))
	if deleted.Memory.Status != agentmemory.StatusDeleted || deleted.Memory.Summary != "" || string(deleted.Memory.StructuredValue) != "{}" {
		t.Fatal("registered delete retained private contents")
	}
	f.request(t, f.handler, "DELETE", path+"/"+id, `{"expectedVersion":2}`, f.tokens[0], 200, nil)
	w = f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
	if strings.TrimSpace(w.Body.String()) != "{\"data\":[]}" {
		t.Fatal("tombstone listed", w.Body.String())
	}
}

func TestOrganizationMemoryRegisteredHTTPStrictWireNoAuthorityFields(t *testing.T) {
	f, org := orgMemoryHTTPFixture(t)
	path := "/v1/me/organizations/" + org + "/agent-memories"
	id := orgMemoryHTTPID(t, f)
	body := orgMemoryHTTPBody(t, orgMemoryHTTPInput())
	tests := []struct {
		name, method, path, body, media string
		status                          int
		workspace                       bool
	}{
		{"query", "GET", path + "?ownerId=" + f.accountIDs[0], "", "application/json", 400, false},
		{"empty_query", "GET", path + "?", "", "application/json", 400, false},
		{"get_body", "GET", path, `{}`, "application/json", 400, false},
		{"workspace", "PUT", path + "/" + id, body, "application/json", 400, true},
		{"wrong_media", "PUT", path + "/" + id, body, "text/plain", 415, false},
		{"null_version", "PUT", path + "/" + id, strings.Replace(body, `"expectedVersion":0`, `"expectedVersion":null`, 1), "application/json", 400, false},
		{"duplicate", "PUT", path + "/" + id, `{"expectedVersion":0,` + body[1:], "application/json", 400, false},
		{"authority", "PUT", path + "/" + id, `{"ownerId":"` + f.accountIDs[2] + `",` + body[1:], "application/json", 400, false},
		{"null_content", "PUT", path + "/" + id, strings.Replace(body, `"structuredValue":{"note":"合成内容","tags":["测试"]}`, `"structuredValue":null`, 1), "application/json", 400, false},
		{"oversize", "PUT", path + "/" + id, strings.Repeat("x", agentmemory.MaxBodyBytes+1), "application/json", 400, false},
		{"zero_delete_version", "DELETE", path + "/" + id, `{"expectedVersion":0}`, "application/json", 400, false},
		{"delete_authority", "DELETE", path + "/" + id, `{"expectedVersion":1,"confirmed":true}`, "application/json", 400, false},
		{"uppercase_uuid", "GET", "/v1/me/organizations/" + strings.ToUpper(org) + "/agent-memories", "", "application/json", 400, false},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			r := privateProfileHTTPRequest(c.method, c.path, c.body, f.tokens[0], c.media)
			if c.workspace {
				r.Header.Set("X-Birdtie-Organization-Workspace", org)
			}
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != c.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s status=%d want=%d", c.name, w.Code, c.status)
			}
		})
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_memories WHERE owner_id=$1`, f.accountIDs[2]).Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid wire wrote native ledger", count, e)
	}
}

// Production Store delegates every operation. The hook is after its actual
// committed result, before the handler's separate final current authority read.
type orgMemoryHTTPAfter struct {
	*postgres.Store
	after func()
}

func (s *orgMemoryHTTPAfter) ListOrganizationMemories(ctx context.Context, a agentorganizationmemory.Access) ([]agentorganizationmemory.View, error) {
	v, e := s.Store.ListOrganizationMemories(ctx, a)
	if e == nil && s.after != nil {
		s.after()
	}
	return v, e
}
func (s *orgMemoryHTTPAfter) PutOrganizationMemory(ctx context.Context, a agentorganizationmemory.Access, id string, in agentorganizationmemory.PutInput) (agentorganizationmemory.View, error) {
	v, e := s.Store.PutOrganizationMemory(ctx, a, id, in)
	if e == nil && s.after != nil {
		s.after()
	}
	return v, e
}
func (s *orgMemoryHTTPAfter) DeleteOrganizationMemory(ctx context.Context, a agentorganizationmemory.Access, id string, version int64) (agentorganizationmemory.View, error) {
	v, e := s.Store.DeleteOrganizationMemory(ctx, a, id, version)
	if e == nil && s.after != nil {
		s.after()
	}
	return v, e
}

func TestOrganizationMemoryRegisteredHTTPActualPostCommitRevocation(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, change := range []string{"session", "role", "account", "agent"} {
			t.Run(method+"_"+change, func(t *testing.T) {
				f, org := orgMemoryHTTPFixture(t)
				id := orgMemoryHTTPID(t, f)
				path := "/v1/me/organizations/" + org + "/agent-memories"
				token := f.tokens[1]
				digest, e := identity.ParseBearer("Bearer " + token)
				if e != nil {
					t.Fatal(e)
				}
				in := orgMemoryHTTPInput()
				body := orgMemoryHTTPBody(t, in)
				if method == "DELETE" || method == "GET" {
					f.request(t, f.handler, "PUT", path+"/"+id, body, token, 200, nil)
					if method == "DELETE" {
						body = `{"expectedVersion":1}`
					}
				}
				if method != "GET" {
					path += "/" + id
				}
				store := &orgMemoryHTTPAfter{Store: f.store, after: func() {
					switch change {
					case "session":
						f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:])
					case "role":
						f.exec(`UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1])
					case "account":
						f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[1])
					case "agent":
						f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[2])
					}
				}}
				if method == "GET" {
					body = ""
				}
				w := f.request(t, privateProfileHTTPNew(store, f.store), method, path, body, token, 403, nil)
				if strings.Contains(w.Body.String(), in.Summary) || strings.Contains(w.Body.String(), id) {
					t.Fatal("late revoked native response leaked row")
				}
				var count int
				sql := `SELECT count(*) FROM agent_memories WHERE id=$1`
				if method == "PUT" {
					sql += ` AND version=1 AND status='ACTIVE'`
				}
				if method == "DELETE" {
					sql += ` AND version=2 AND status='DELETED'`
				}
				if e := f.pool.QueryRow(f.ctx, sql, id).Scan(&count); e != nil {
					t.Fatal(e)
				}
				expected := 1
				if count != expected {
					t.Fatal("postcommit rejection misrepresented as rollback", method, count)
				}
				t.Log("Actual domain commit precedes final denial; write outcome requires current reread, not a rollback claim")
			})
		}
	}
}

func TestOrganizationMemoryRegisteredHTTPFinalSessionWaitNaturalExpiry(t *testing.T) {
	for _, kind := range []string{"absolute", "idle"} {
		t.Run(kind, func(t *testing.T) {
			f, org := orgMemoryHTTPFixture(t)
			path := "/v1/me/organizations/" + org + "/agent-memories"
			privateInput := orgMemoryHTTPInput()
			privateID := orgMemoryHTTPID(t, f)
			f.request(t, f.handler, "PUT", path+"/"+privateID, orgMemoryHTTPBody(t, privateInput), f.tokens[0], 200, nil)
			digest, e := identity.ParseBearer("Bearer " + f.tokens[0])
			if e != nil {
				t.Fatal(e)
			}
			type proof struct {
				waiting, expired bool
				err              error
			}
			done := make(chan proof, 1)
			store := &orgMemoryHTTPAfter{Store: f.store, after: func() {
				var deadline time.Time
				query := `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',idle_expires_at=stamp.at+interval '400 milliseconds',expires_at=stamp.at+interval '1 hour' FROM stamp WHERE token_sha256=$1 RETURNING idle_expires_at`
				if kind == "absolute" {
					query = strings.Replace(query, "expires_at=stamp.at+interval '1 hour'", "expires_at=stamp.at+interval '400 milliseconds'", 1)
				}
				if e := f.pool.QueryRow(f.ctx, query, digest[:]).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
				lock, e := f.pool.Begin(f.ctx)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = lock.Exec(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]); e != nil {
					_ = lock.Rollback(context.Background())
					t.Fatal(e)
				}
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
					defer cancel()
					defer lock.Rollback(context.Background())
					p := proof{}
					for ctx.Err() == nil {
						var now time.Time
						p.err = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid))),clock_timestamp()`, int(lock.Conn().PgConn().PID())).Scan(&p.waiting, &now)
						p.expired = !now.Before(deadline)
						if p.err != nil || p.waiting && p.expired {
							break
						}
						time.Sleep(5 * time.Millisecond)
					}
					if p.err == nil {
						p.err = ctx.Err()
					}
					if e := lock.Commit(context.Background()); p.err == nil {
						p.err = e
					}
					done <- p
				}()
			}}
			w := f.request(t, privateProfileHTTPNew(store, f.store), "GET", path, "", f.tokens[0], 403, nil)
			if strings.Contains(w.Body.String(), privateInput.Summary) || strings.Contains(w.Body.String(), privateID) {
				t.Fatal("final Session wait leaked actual nonempty private row")
			}
			select {
			case p := <-done:
				if p.err != nil || !p.waiting || !p.expired {
					t.Fatalf("no actual final session/PG expiry proof %+v", p)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("final guard remained blocked")
			}
		})
	}
}

func TestOrganizationMemoryRegisteredHTTPListExpiryIsReadProjection(t *testing.T) {
	f, org := orgMemoryHTTPFixture(t)
	path := "/v1/me/organizations/" + org + "/agent-memories"
	id := orgMemoryHTTPID(t, f)
	in := orgMemoryHTTPInput()
	f.request(t, f.handler, "PUT", path+"/"+id, orgMemoryHTTPBody(t, in), f.tokens[0], 200, nil)
	// Preserve every native version/time invariant while simulating past validity.
	f.exec(`UPDATE agent_memories SET version=version+1,valid_from=clock_timestamp()-interval '2 hours',valid_until=clock_timestamp()-interval '1 hour',updated_at=clock_timestamp() WHERE id=$1`, id)
	var before string
	if e := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, id).Scan(&before); e != nil {
		t.Fatal(e)
	}
	w := f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
	var v struct {
		Data []agentorganizationmemory.View `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || len(v.Data) != 1 || v.Data[0].Memory.Status != agentmemory.StatusExpired {
		t.Fatal("expired projection missing", e)
	}
	var after string
	if e := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, id).Scan(&after); e != nil || before != after {
		t.Fatal("list wrote authoritative expiry", e)
	}
	t.Log(fmt.Sprintf("actual expired private declaration remains manageable at native version %d", v.Data[0].Memory.Version))
}
