package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Explicit original-store/history/empty-search doubles. No native DB, real
// sources, provider billing or device proof is claimed by these HTTP tests.
type emptyPlaceHTTPPort struct {
	*liveReplyHistoryHTTPPort
	calls     int
	decline   bool
	err       error
	emptyRead *emptyPlaceHTTPRead
	mutate    func(*agentworkspace.Task)
}
type emptyPlaceHTTPRead struct {
	checks int
	err    error
}

func (r *emptyPlaceHTTPRead) Revalidate(context.Context) error { r.checks++; return r.err }

func (p *emptyPlaceHTTPPort) TryOwnEmptyPlaceRefinementReply(ctx context.Context, a arp.Access) (agentworkspace.Task, agentworkspace.EmptyPlaceReplyRead, bool, error) {
	p.calls++
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(p.task))
	if a.Actor.ID != p.task.PrincipalID || a.SessionDigest == ([32]byte{}) || a.TaskID != p.task.ID || string(raw) != string(a.ExpectedTask) || !agentworkspace.IsExplicitPlaceRefinementTask(p.task) {
		return agentworkspace.Task{}, nil, false, arp.ErrDenied
	}
	if p.err != nil {
		return agentworkspace.Task{}, nil, false, p.err
	}
	if p.decline {
		return p.task, nil, false, nil
	}
	task := p.task
	task.Conversation = append(append([]agentworkspace.Message{}, task.Conversation...), agentworkspace.Message{Role: "assistant", Text: agentworkspace.EmptyPlaceRefinementReply(task)})
	i := len(task.Conversation) - 1
	var e error
	task.Conversation[i].ResultMembership, e = agentworkspace.NewReplyMembership(task, i, "place", []arp.Ref{})
	if e != nil {
		return task, nil, false, e
	}
	task.Status = agentworkspace.TaskCompleted
	task, e = p.UpdateTask(ctx, task)
	if e != nil {
		return task, nil, false, e
	}
	result := agentworkspace.WithContract(agentworkspace.Results{Task: &task, CityID: task.CityID, NativeProjection: true, ProjectionItems: []arp.Item{}}, task, "UNIT_EMPTY")
	member := task.Conversation[i].ResultMembership
	result.ResultSet.ID = member.ResultSetID
	p.entries = []agentworkspace.MessageResult{{MessageIndex: i, TurnDigest: member.TurnDigest, ValidUntil: time.Now().Add(20 * time.Second), ResultSet: result.ResultSet, MapEffects: result.MapEffects}}
	if p.mutate != nil {
		p.mutate(&task)
	}
	return task, p.emptyRead, true, nil
}

type emptyPlaceModelGuard struct {
	eligible, sends int
	err             error
}

func (m *emptyPlaceModelGuard) Eligible(context.Context, [32]byte, agentworkspace.Task) bool {
	m.eligible++
	return true
}
func (m *emptyPlaceModelGuard) Execute(context.Context, [32]byte, agentworkspace.Task) (agentworkspace.LiveReply, error) {
	m.sends++
	return nil, m.err
}

func emptyPlaceHTTPFixture(t *testing.T) (*server, *http.ServeMux, *emptyPlaceHTTPPort, *emptyPlaceModelGuard, string) {
	s, mux, tasks, token := httpActionSafetyServer(t, nil)
	s.catalog = nowHTTPTestCatalog{}
	tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, ContextType: "CITY", CityID: "aberdeen-gb", Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskCompleted, Query: "找地点", Filters: map[string]string{"targetIntent": agentworkspace.FindPlace, "category": "restaurant", "searchTerm": "UNIT old place", "locationPreference": "city", "timePreference": "anytime"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "找地点"}, {Role: "assistant", Text: "UNIT original retained history"}}}
	port := &emptyPlaceHTTPPort{liveReplyHistoryHTTPPort: &liveReplyHistoryHTTPPort{httpActionSafetyTaskStore: tasks}, emptyRead: &emptyPlaceHTTPRead{}}
	s.agent = port
	model := &emptyPlaceModelGuard{err: errors.New("UNIT unexpected provider call")}
	s.liveAnswers = model
	return s, mux, port, model, token
}

func TestEmptyPlaceRefinementHTTPRegisteredSameTaskEmptyCardsMapAndZeroModel(t *testing.T) {
	_, mux, port, model, token := emptyPlaceHTTPFixture(t)
	code, raw := httpActionSafetyCall(t, mux, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"only cafes","taskId":"`+httpActionSafetyTaskID+`"}`)
	if code != http.StatusOK {
		t.Fatal(code, string(raw))
	}
	var wire struct{ Data agentworkspace.Results }
	if json.Unmarshal(raw, &wire) != nil {
		t.Fatal("bad wire")
	}
	r := wire.Data
	if port.calls != 1 || model.eligible != 0 || model.sends != 0 || port.emptyRead.checks != 1 || port.handle == nil || port.handle.checks != 1 {
		t.Fatal("native empty/final history gates or zero-provider path", port.calls, model, port.emptyRead)
	}
	if r.Task == nil || r.Task.ID != httpActionSafetyTaskID || r.Task.Status != agentworkspace.TaskCompleted || len(r.Task.Conversation) != 4 || r.Task.Conversation[1].Text != "UNIT original retained history" || r.Task.Filters["category"] != "cafe" || r.Task.Filters["searchTerm"] != "" || r.Mode != "rules" || !strings.Contains(r.Message, "没有符合筛选条件的咖啡馆") {
		t.Fatal("same task/category/truthful current answer", string(raw))
	}
	if r.ResultSet.Status != "empty" || len(r.ResultSet.Items) != 0 || len(r.ResultSet.Entities) != 0 || len(r.Places) != 0 || len(r.MapEffects.PinEntityIDs) != 0 || r.ResultSet.AnswerBinding != nil || len(r.ResultSet.Sources) != 0 || r.ResultSet.PublicFieldEvidence != nil || len(r.MessageResults) != 1 || len(r.Task.Conversation[3].ResultMembership.Refs) != 0 {
		t.Fatal("old card/pin/fake source leaked into current empty round", string(raw))
	}
}

func TestEmptyPlaceRefinementHTTPNonemptyDeclinesAndPreservesFactsAndSourceErrors(t *testing.T) {
	for name, e := range map[string]error{"licensed facts insufficient": agentworkspace.ErrLicensedPlaceFactsUnavailable, "source changed": arp.ErrChanged} {
		t.Run(name, func(t *testing.T) {
			_, mux, port, model, token := emptyPlaceHTTPFixture(t)
			port.decline = true
			model.err = e
			code, raw := httpActionSafetyCall(t, mux, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"change to restaurants","taskId":"`+httpActionSafetyTaskID+`"}`)
			if code == http.StatusOK || model.sends != 1 || port.calls != 1 || port.task.Status != agentworkspace.TaskActive || len(port.task.Conversation) != 3 || port.task.Conversation[1].Text != "UNIT original retained history" {
				t.Fatal("nonempty model error collapsed into empty success", code, string(raw))
			}
			if errors.Is(e, agentworkspace.ErrLicensedPlaceFactsUnavailable) && (code != 503 || !strings.Contains(string(raw), "place_answer_unavailable")) {
				t.Fatal("existing 503 facts semantics changed", code, string(raw))
			}
		})
	}
}

func TestEmptyPlaceRefinementHTTPNativeFailureAndFinalRaceWithholdAnswer(t *testing.T) {
	for _, late := range []bool{false, true} {
		for name, e := range map[string]error{"denied": agenttool.ErrDenied, "stale": agenttool.ErrChanged, "unavailable": agenttool.ErrUnavailable, "session": identity.ErrUnauthorized} {
			t.Run(name+map[bool]string{true: "late", false: "early"}[late], func(t *testing.T) {
				_, mux, port, model, token := emptyPlaceHTTPFixture(t)
				if late {
					port.emptyRead.err = e
				} else {
					port.err = e
				}
				code, raw := httpActionSafetyCall(t, mux, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"only cafes","taskId":"`+httpActionSafetyTaskID+`"}`)
				if code == 200 || strings.Contains(string(raw), "没有符合筛选条件") || strings.Contains(string(raw), `"items"`) || model.sends != 0 {
					t.Fatal("denied/stale native empty released as answer", code, string(raw))
				}
				if !late && (port.task.Status != agentworkspace.TaskActive || len(port.task.Conversation) != 3) {
					t.Fatal("failed source changed legal prior history")
				}
			})
		}
	}
}

func TestEmptyPlaceRefinementHTTPRejectsReturnedTaskOrFakeProof(t *testing.T) {
	for name, change := range map[string]func(*agentworkspace.Task){"different task": func(t *agentworkspace.Task) { t.ID = httpActionSafetyOtherOrg }, "history changed": func(t *agentworkspace.Task) { t.Conversation[1].Text = "CHANGED" }, "fake source": func(t *agentworkspace.Task) { t.Conversation[len(t.Conversation)-1].SourceRunID = "FAKE" }, "slots changed": func(t *agentworkspace.Task) { t.Filters = map[string]string{"category": "park"} }} {
		t.Run(name, func(t *testing.T) {
			_, mux, port, model, token := emptyPlaceHTTPFixture(t)
			port.mutate = change
			code, raw := httpActionSafetyCall(t, mux, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"only cafes","taskId":"`+httpActionSafetyTaskID+`"}`)
			if code == 200 || strings.Contains(string(raw), "没有符合筛选条件") || model.sends != 0 {
				t.Fatal("mismatched/fake returned membership accepted", code, string(raw))
			}
		})
	}
}

func TestEmptyPlaceRefinementHTTPScopeGuardDoesNotInvokePort(t *testing.T) {
	for name, change := range map[string]func(*agentworkspace.Task, *http.Request, *[32]byte){"get": func(_ *agentworkspace.Task, r *http.Request, _ *[32]byte) { r.Method = "GET" }, "no session": func(_ *agentworkspace.Task, _ *http.Request, d *[32]byte) { *d = [32]byte{} }, "org header": func(_ *agentworkspace.Task, r *http.Request, _ *[32]byte) {
		r.Header.Set("X-Birdtie-Organization-Workspace", "unit")
	}, "activity": func(t *agentworkspace.Task, _ *http.Request, _ *[32]byte) { t.Intent = agentworkspace.FindActivity }, "online": func(t *agentworkspace.Task, _ *http.Request, _ *[32]byte) { t.ContextType = "ONLINE" }, "other actor": func(t *agentworkspace.Task, _ *http.Request, _ *[32]byte) { t.ActingUserID = httpActionSafetyOtherAcct }} {
		t.Run(name, func(t *testing.T) {
			s, _, p, _, _ := emptyPlaceHTTPFixture(t)
			task := p.task
			task.Status = agentworkspace.TaskActive
			task.Filters["reason"] = agentworkspace.ExplicitPlaceRefinement
			task.Filters["currentQuery"] = "only cafes"
			task.Filters["category"] = "cafe"
			task.Filters["searchTerm"] = ""
			task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: "only cafes"})
			r := httptest.NewRequest("POST", "/", nil)
			digest := [32]byte{1}
			change(&task, r, &digest)
			got, _, handled, e := s.tryEmptyPlaceRefinement(r, digest, task)
			if e != nil || handled || p.calls != 0 || !reflect.DeepEqual(got, task) {
				t.Fatal("unsupported scope reached native empty capability", handled, e)
			}
		})
	}
}
