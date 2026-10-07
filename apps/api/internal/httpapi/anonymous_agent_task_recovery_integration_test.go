package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func anonymousRecoveryNativeWire(t *testing.T, label, method, path, body string, w *httptest.ResponseRecorder) {
	t.Helper()
	v := map[string]any{"scope": "ACTUAL_REGISTERED_HTTP_NATIVE096_SYNTHETIC_NOT_IDP_OR_PILOT", "method": method, "path": path, "requestBody": body, "status": w.Code, "requestID": w.Header().Get("X-Request-ID"), "responseBody": w.Body.String()}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("ANONYMOUS_RECOVERY_NATIVE_WIRE " + label + " " + string(raw))
	if dir := os.Getenv("BIRDTIE_ANONYMOUS_RECOVERY_WIRE_DIR"); dir != "" {
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for n := 0; ; n++ {
			name := fmt.Sprintf("%s-%03d.json", label, n)
			f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if os.IsExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.Write(raw)
			closed := f.Close()
			if err != nil || closed != nil {
				t.Fatal("wire evidence write", err, closed)
			}
			break
		}
	}
}

func anonymousRecoveryNativeCall(t *testing.T, h http.Handler, label, method, path, body, token, workspace string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if workspace != "" {
		r.Header.Set("X-Birdtie-Organization-Workspace", workspace)
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("actual registered response missing request ID")
	}
	anonymousRecoveryNativeWire(t, label, method, path, body, w)
	return w
}

func anonymousRecoveryReply(t *testing.T, w *httptest.ResponseRecorder) agentworkspace.Results {
	t.Helper()
	var out struct {
		Data agentworkspace.Results `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("public native reply", w.Code, w.Body.String())
	}
	if out.Data.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatal("response correlation mismatch")
	}
	return out.Data
}

func anonymousRecoveryCounts(t *testing.T, f *contextBuilderHTTPFixture) string {
	t.Helper()
	var out string
	if err := f.pool.QueryRow(f.ctx, `SELECT json_build_object('tasks',(SELECT count(*) FROM agent_tasks),'intents',(SELECT count(*) FROM social_intents),'receipts',(SELECT count(*) FROM social_intent_creation_receipts),'participation',(SELECT count(*) FROM activity_participations),'connections',(SELECT count(*) FROM connection_requests),'messages',(SELECT count(*) FROM conversation_messages),'audit',(SELECT count(*) FROM audit_events))::text`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAnonymousAgentRecoveryNativePublicProtocolAndZeroPrivateEffects(t *testing.T) {
	modelEgressHTTPOwnedDatabase(t)
	f := contextBuilderHTTPNative(t)
	h := contextBuilderHTTPNew(f.store, f.store, f.store)
	before := anonymousRecoveryCounts(t, f)
	for _, tc := range []struct{ name, query, city, status, text string }{
		{"WEEKEND", "weekend", f.city, "unsupported", "暂时无法处理"},
		{"UNSUPPORTED", "帮我买飞机票", f.city, "unsupported", "暂时无法处理"},
		{"CLARIFY", "近一点的呢？", f.city, "unsupported", "请先搜索活动"},
		{"BOUNDS_REQUIRED", "搜索此区域", f.city, "unsupported", "请先移动地图"},
		{"PUBLIC_PLACE", "找地点", f.city, "ready", "已发布地点"},
		{"PUBLIC_ORGANIZATION", "找组织", "aberdeen-gb", "ready", "公开组织"},
		{"ACTIVITY_CONTROL", "badminton", f.city, "ready", "公开活动"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/v1/cities/" + tc.city + "/agent/tasks"
			body, _ := json.Marshal(map[string]string{"query": tc.query})
			w := anonymousRecoveryNativeCall(t, h, tc.name, "POST", path, string(body), "", "")
			r := anonymousRecoveryReply(t, w)
			if r.ResultSet.Status != tc.status || !strings.Contains(r.Message, tc.text) || r.Task != nil || r.TaskID != "" || r.ResultSet.TaskID != "" || r.ConversationID != "" || r.PrincipalID != "" || len(r.Actions) != 0 || r.RelationshipContext != nil {
				t.Fatal("native public protocol hid clarification or borrowed private authority", w.Body.String())
			}
			if tc.status == "unsupported" && (len(r.ResultSet.Items) != 0 || strings.Contains(r.Message, "没有找到")) {
				t.Fatal("unsupported became false empty", w.Body.String())
			}
			if tc.name == "PUBLIC_PLACE" && (len(r.Places) != 1 || r.Places[0].ID != f.place || len(r.ResultSet.Items) != 1 || r.ResultSet.Items[0].Entity.ID != f.place) {
				t.Fatal("place original source/ref lost", w.Body.String())
			}
			if tc.name == "PUBLIC_ORGANIZATION" {
				actual, err := f.store.SearchOrganizations(f.ctx, tc.city, "")
				if err != nil || len(actual) == 0 || len(r.Organizations) != len(actual) || len(r.ResultSet.Items) != len(actual) {
					t.Fatal("organization source count differs or seed lacks public organization", err, w.Body.String())
				}
				for i, x := range actual {
					if r.Organizations[i].ID != x.ID || r.ResultSet.Items[i].Entity.ID != x.ID {
						t.Fatal("public organization identity changed")
					}
				}
			}
			for _, x := range r.Activities {
				if x.ID == f.private {
					t.Fatal("anonymous response leaked invitation-only activity")
				}
			}
		})
	}
	if after := anonymousRecoveryCounts(t, f); after != before {
		t.Fatal("anonymous read created private/business effect", before, after)
	}
}

func TestAnonymousAgentRecoveryNativeSignedAndPrivateBoundaries(t *testing.T) {
	modelEgressHTTPOwnedDatabase(t)
	f := contextBuilderHTTPNative(t)
	h := contextBuilderHTTPNew(f.store, f.store, f.store)
	path := "/v1/cities/" + f.city + "/agent/tasks"
	own := anonymousRecoveryReply(t, anonymousRecoveryNativeCall(t, h, "SIGNED_VALID", "POST", path, `{"query":"weekend"}`, f.tokens[0], ""))
	if own.Task == nil || own.TaskID == "" || own.Task.PrincipalID != f.accountIDs[0] || own.Task.Status != agentworkspace.TaskFailed || own.ResultSet.Status != "unsupported" {
		t.Fatal("signed original current Task/unsupported contract changed", own)
	}
	var org string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&org); err != nil {
		t.Fatal(err)
	}
	follow, _ := json.Marshal(map[string]string{"query": "weekend", "taskId": own.TaskID})
	before := anonymousRecoveryCounts(t, f)
	for _, tc := range []struct {
		name, method, path, body, token, workspace string
		code                                       int
	}{
		{"INVALID_BEARER", "POST", path, `{"query":"weekend"}`, "invalid", "", 401},
		{"EXPIRED", "POST", path, `{"query":"weekend"}`, f.newSession(f.accountIDs[0], true, false), "", 401},
		{"REVOKED", "POST", path, `{"query":"weekend"}`, f.newSession(f.accountIDs[0], false, true), "", 401},
		{"FOREIGN_TASK", "POST", path, string(follow), f.tokens[1], "", 404},
		{"ANONYMOUS_TASK", "POST", path, string(follow), "", "", 400},
		{"ANONYMOUS_RESTORE", "GET", "/v1/me/agent-tasks/" + own.TaskID, "", "", "", 401},
		{"FOREIGN_RESTORE", "GET", "/v1/me/agent-tasks/" + own.TaskID, "", f.tokens[1], "", 404},
		{"ANONYMOUS_ORG", "POST", path, `{"query":"weekend"}`, "", org, 403},
		{"NONMEMBER_ORG", "POST", path, `{"query":"weekend"}`, f.tokens[0], org, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := anonymousRecoveryNativeCall(t, h, tc.name, tc.method, tc.path, tc.body, tc.token, tc.workspace)
			if w.Code != tc.code || strings.Contains(w.Body.String(), own.TaskID) || strings.Contains(w.Body.String(), f.private) {
				t.Fatal("denied query returned private task/source", tc.code, w.Code, w.Body.String())
			}
		})
	}
	if after := anonymousRecoveryCounts(t, f); after != before {
		t.Fatal("denied query changed original effect counts", before, after)
	}
}

type anonymousRecoveryCurrentTaskStore struct {
	*postgres.Store
	afterUpdate func(agentworkspace.Task)
}

func (s *anonymousRecoveryCurrentTaskStore) UpdateTask(ctx context.Context, in agentworkspace.Task) (agentworkspace.Task, error) {
	out, err := s.Store.UpdateTask(ctx, in)
	if err == nil && s.afterUpdate != nil {
		s.afterUpdate(out)
	}
	return out, err
}

func TestAnonymousAgentRecoveryNativeLateCurrentGuardStillDenies(t *testing.T) {
	for _, name := range []string{"REVOKE_SESSION", "TASK_CHANGED", "AGENT_SUSPENDED"} {
		t.Run(name, func(t *testing.T) {
			modelEgressHTTPOwnedDatabase(t)
			f := contextBuilderHTTPNative(t)
			store := &anonymousRecoveryCurrentTaskStore{Store: f.store}
			store.afterUpdate = func(task agentworkspace.Task) {
				switch name {
				case "REVOKE_SESSION":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
				case "TASK_CHANGED":
					f.exec(`UPDATE agent_tasks SET filters=filters || '{"currentQuery":"actual newer query"}'::jsonb,updated_at=clock_timestamp() WHERE id=$1`, task.ID)
				case "AGENT_SUSPENDED":
					f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
				}
			}
			h := New(f.store, f.store, nil, f.store, store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
			w := anonymousRecoveryNativeCall(t, h, name, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"weekend"}`, f.tokens[0], "")
			want := 404
			if name == "REVOKE_SESSION" {
				want = 401
			}
			if w.Code != want || strings.Contains(w.Body.String(), `"task":`) || strings.Contains(w.Body.String(), "actual newer query") {
				t.Fatal("late native current guard weakened", want, w.Code, w.Body.String())
			}
		})
	}
}
