package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Offline response checks only. Nil embedded business mutation methods panic
// if an anonymous request accidentally starts persistence or a private action.
type anonymousRecoveryPublicCatalog struct{ httpActionSafetyCatalog }

func (anonymousRecoveryPublicCatalog) ListPlaces(context.Context, string, string) ([]foundation.Place, error) {
	return []foundation.Place{{ID: httpActionSafetyActivity, Name: "合成公开地点"}}, nil
}

type anonymousRecoveryPublicTasks struct{ *httpActionSafetyTaskStore }

func (anonymousRecoveryPublicTasks) SearchOrganizations(context.Context, string, string) ([]agentworkspace.Organization, error) {
	return []agentworkspace.Organization{{ID: httpActionSafetyOrgID, Name: "合成公开组织"}}, nil
}

func (s *anonymousRecoveryPublicTasks) SaveTask(context.Context, agentworkspace.Task) (agentworkspace.Task, error) {
	panic("anonymous request attempted to persist a private Task")
}
func (s *anonymousRecoveryPublicTasks) UpdateTask(context.Context, agentworkspace.Task) (agentworkspace.Task, error) {
	panic("anonymous request attempted to update a private Task")
}

func TestAnonymousAgentRecoveryPublicSpecialResponses(t *testing.T) {
	for _, tc := range []struct{ query, status, message string }{
		{"weekend", "unsupported", "暂时无法处理"},
		{"帮我买飞机票", "unsupported", "暂时无法处理"},
		{"近一点的呢？", "unsupported", "请先搜索活动"},
		{"比较这两个活动", "unsupported", "请先搜索活动"},
		{"搜索此区域", "unsupported", "请先移动地图"},
		{"找地点", "ready", "已发布地点"},
		{"找组织", "ready", "公开组织"},
		{"我的关系信号", "empty", "登录"},
		{"认识新朋友", "empty", "登录"},
		{"发布活动", "empty", "发布活动需要登录"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			s, mux, tasks, _ := httpActionSafetyServer(t, nil)
			s.catalog = anonymousRecoveryPublicCatalog{}
			s.agent = &anonymousRecoveryPublicTasks{tasks}
			body, _ := json.Marshal(map[string]string{"query": tc.query})
			code, raw := httpActionSafetyCall(t, mux, "", "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(body))
			if code != 200 {
				t.Fatalf("public special request failed status=%d body=%s", code, raw)
			}
			var out struct {
				Data agentworkspace.Results `json:"data"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			r := out.Data
			if r.ResultSet.Status != tc.status || !strings.Contains(r.Message, tc.message) || r.Task != nil || r.TaskID != "" || r.ConversationID != "" || r.PrincipalID != "" || r.ResultSet.TaskID != "" || len(r.Actions) != 0 || r.RelationshipContext != nil || tasks.task.ID != "" {
				t.Fatal("anonymous public response borrowed private authority or hid clarification", string(raw))
			}
			if tc.status == "unsupported" && (len(r.ResultSet.Items) != 0 || strings.Contains(r.Message, "没有找到")) {
				t.Fatal("unsupported request became a successful empty search", string(raw))
			}
		})
	}
}

func TestAnonymousAgentRecoveryDoesNotRelaxCurrentPersonGuard(t *testing.T) {
	s, _, _, _ := httpActionSafetyServer(t, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", nil)
	s.respondCurrentAgentTask(w, r, emptyAgentResults("aberdeen-gb", "weekend"), [32]byte{}, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"task_unavailable"`) {
		t.Fatal("authenticated person without authoritative Task was allowed", w.Code, w.Body.String())
	}
}
