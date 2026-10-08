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

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Boundary doubles only: these tests prove original HTTP routing/persistence,
// not PUBLIC_PLACE authorization, external search or actual model content.
type placeFollowupHTTPStore struct {
	*nowHTTPNativeStore
	calls int
	err   error
	bad   bool
}

func (s *placeFollowupHTTPStore) ContinueOwnPublicPlace(ctx context.Context, a arp.Access, query string) (agentworkspace.Task, error) {
	s.calls++
	if s.err != nil {
		return agentworkspace.Task{}, s.err
	}
	previous := s.task
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(previous))
	var expected, actual any
	_ = json.Unmarshal(raw, &actual)
	_ = json.Unmarshal(a.ExpectedTask, &expected)
	if !reflect.DeepEqual(expected, actual) || a.Actor.ID != previous.PrincipalID || a.TaskID != previous.ID {
		return agentworkspace.Task{}, arp.ErrChanged
	}
	f, ok := agentworkspace.ParsePlaceFollowup(query, &previous)
	if !ok {
		return agentworkspace.Task{}, arp.ErrDenied
	}
	next := previous
	next.Filters = map[string]string{}
	for key, value := range previous.Filters {
		next.Filters[key] = value
	}
	next.Intent, next.Status = agentworkspace.FindPlace, agentworkspace.TaskActive
	next.Filters["targetIntent"], next.Filters["currentQuery"], next.Filters["timePreference"] = agentworkspace.FindPlace, query, f.TimePreference
	if f.Name != "" {
		next.Filters["searchTerm"] = "Aberdeen Maritime Museum"
	}
	next.Filters[agentworkspace.PlaceFollowupIDFilter] = httpActionSafetyActivity
	next.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("a", 64)
	next.Filters[agentworkspace.PlaceFollowupTopicFilter] = f.Topic
	next.Conversation = append(append([]agentworkspace.Message{}, previous.Conversation...), agentworkspace.Message{Role: "user", Text: query})
	if s.bad {
		next.CityID = "unrelated-city"
		return next, nil
	}
	return s.UpdateTask(ctx, next)
}

type placeFollowupHTTPAnswers struct {
	*nowHTTPFakeAnswers
	seen []agentworkspace.Task
}

func (s *placeFollowupHTTPAnswers) Execute(ctx context.Context, digest [32]byte, task agentworkspace.Task) (agentworkspace.LiveReply, error) {
	s.sends++
	s.seen = append(s.seen, task)
	if digest == ([32]byte{}) || task.Status != agentworkspace.TaskActive || !reflect.DeepEqual(task, s.store.task) || task.Filters["currentQuery"] == "" || task.Filters[agentworkspace.PlaceFollowupTopicFilter] != agentworkspace.PlaceOpeningHours || task.Filters["timePreference"] != "weekend" {
		s.t.Fatal("native continuation/current slots were not persisted before the model boundary")
	}
	if s.err != nil {
		return nil, s.err
	}
	sources := []agentworkspace.AnswerSource{{ID: "unit-new-source", Title: "UNIT newly retrieved source", URL: "https://example.com/unit-new-source"}}
	task.Status = agentworkspace.TaskCompleted
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: nowHTTPPrivateAnswer, Sources: sources})
	current, e := s.store.UpdateTask(ctx, task)
	if e != nil {
		return nil, e
	}
	s.reply = &nowHTTPFakeReply{task: current, sources: sources}
	return s.reply, nil
}

func placeFollowupHTTPFixture(t *testing.T) (*server, *http.ServeMux, *placeFollowupHTTPStore, *placeFollowupHTTPAnswers, string) {
	t.Helper()
	s, mux, native, fake, token := nowHTTPFixture(t)
	store := &placeFollowupHTTPStore{nowHTTPNativeStore: native}
	s.agent = store
	fakeAnswers := &placeFollowupHTTPAnswers{nowHTTPFakeAnswers: fake}
	s.liveAnswers = fakeAnswers
	task := agentworkspace.Task{PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, ContextType: "CITY", ContextID: "unit-original-city-context", CityID: "aberdeen-gb", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: "找地点 Aberdeen Art Gallery", Filters: map[string]string{"targetIntent": agentworkspace.FindPlace, "searchTerm": "Aberdeen Art Gallery", "locationPreference": "city"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "找地点 Aberdeen Art Gallery"}, {Role: "assistant", Text: "UNIT prior actual source reply"}}}
	_, e := store.SaveTask(context.Background(), task)
	if e != nil {
		t.Fatal(e)
	}
	return s, mux, store, fakeAnswers, token
}

func TestPlaceFollowupHTTPRegisteredRawTwoTurnsKeepTaskAndSourceFlow(t *testing.T) {
	_, mux, store, live, token := placeFollowupHTTPFixture(t)
	for turn, question := range []string{"它周末几点开门？", "那 Maritime Museum 呢？"} {
		body, _ := json.Marshal(map[string]string{"query": question, "taskId": httpActionSafetyTaskID})
		code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(body))
		if code != http.StatusOK || store.calls != turn+1 || live.sends != turn+1 || live.reply.checks != 1 {
			t.Fatal("same registered POST failed native source/model/final boundary", code, store.calls, live.sends)
		}
		var response struct{ Data agentworkspace.Results }
		if json.Unmarshal(raw, &response) != nil || response.Data.Task == nil {
			t.Fatal("actual HTTP response shape")
		}
		r := response.Data
		if r.Task.ID != httpActionSafetyTaskID || r.Task.Status != agentworkspace.TaskCompleted || r.Mode != "live" || r.Task.Conversation[len(r.Task.Conversation)-2].Text != question || len(r.Task.Conversation) != 4+2*turn || len(r.ResultSet.Sources) != 1 || r.ResultSet.AnswerBinding == nil || r.ResultSet.AnswerBinding.TaskID != r.Task.ID {
			t.Fatal("raw question/original history/sourced current task lost")
		}
		if len(r.ResultSet.Entities) != 1 || len(r.ResultSet.Items) != 1 || len(r.MapEffects.PinEntityIDs) != 1 || r.ResultSet.Entities[0] != r.ResultSet.Items[0].Entity || r.MapEffects.PinEntityIDs[0] != r.ResultSet.Entities[0].Key() {
			t.Fatal("same current card/map entity diverged")
		}
		if strings.Contains(string(raw), agentworkspace.PlaceFollowupGenerationFilter) || strings.Contains(string(raw), agentworkspace.PlaceFollowupIDFilter) || strings.Contains(string(raw), "暂时无法处理") || strings.Contains(string(raw), "找到 1 个已发布地点") {
			t.Fatal("private proof or canned opening-hours answer reached the actual response")
		}
		if turn == 1 && (live.seen[1].Filters["searchTerm"] != "Aberdeen Maritime Museum" || live.seen[1].Filters[agentworkspace.PlaceFollowupTopicFilter] != agentworkspace.PlaceOpeningHours) {
			t.Fatal("named public replacement lost inherited question topic")
		}
	}
}

func TestPlaceFollowupHTTPUnavailableDeniedOrChangedDoesNotWriteTurn(t *testing.T) {
	for _, mode := range []string{"default_off", "ineligible", "no_port", "ambiguous", "withdrawn", "not_found", "denied", "bad_native_result"} {
		t.Run(mode, func(t *testing.T) {
			s, mux, store, live, token := placeFollowupHTTPFixture(t)
			before, _ := json.Marshal(store.task)
			switch mode {
			case "default_off":
				s.liveAnswers = nil
			case "ineligible":
				live.eligible = false
			case "no_port":
				s.agent = store.nowHTTPNativeStore
			case "ambiguous":
				store.err = agentworkspace.ErrPlaceFollowupAmbiguous
			case "withdrawn":
				store.err = agentworkspace.ErrPlaceFollowupChanged
			case "not_found":
				store.err = agentworkspace.ErrPlaceFollowupNotFound
			case "denied":
				store.err = arp.ErrDenied
			case "bad_native_result":
				store.bad = true
			}
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"它周末几点开门？","taskId":"`+httpActionSafetyTaskID+`"}`)
			after, _ := json.Marshal(store.task)
			if code == http.StatusOK || string(before) != string(after) || live.sends != 0 || strings.Contains(string(raw), nowHTTPPrivateAnswer) || strings.Contains(string(raw), "暂时无法处理") {
				t.Fatal("unavailable/denied native source became fake completed answer or changed the original task", code)
			}
		})
	}
}

func TestPlaceFollowupHTTPIdentityAndOrganizationNeverReachNativePort(t *testing.T) {
	for _, mode := range []string{"different_actor", "guest", "organization"} {
		t.Run(mode, func(t *testing.T) {
			s, _, store, live, _ := placeFollowupHTTPFixture(t)
			r := httptest.NewRequest("POST", "/v1/cities/aberdeen-gb/agent/tasks", nil)
			digest := [32]byte{1}
			actor := identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"}
			if mode == "different_actor" {
				actor.ID = httpActionSafetyOtherAcct
			} else if mode == "guest" {
				actor, digest = identity.Actor{}, [32]byte{}
			} else {
				r.Header.Set("X-Birdtie-Organization-Workspace", httpActionSafetyOrgID)
			}
			w := httptest.NewRecorder()
			_, _, ok := s.continuePublicPlaceFollowup(w, r, digest, actor, store.task, "它周末几点开门？")
			if ok || w.Code != http.StatusForbidden || store.calls != 0 || live.sends != 0 {
				t.Fatal("identity/workspace mismatch acquired public place continuation")
			}
		})
	}
}

func TestPlaceFollowupHTTPModelFailureDoesNotFabricateCompletion(t *testing.T) {
	_, mux, store, live, token := placeFollowupHTTPFixture(t)
	before := append([]agentworkspace.Message(nil), store.task.Conversation...)
	live.err = errors.New("UNIT_SYNTHETIC_MODEL_FAILURE")
	code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"它周末几点开门？","taskId":"`+httpActionSafetyTaskID+`"}`)
	if code == http.StatusOK || store.calls != 1 || live.sends != 1 || live.reply != nil || store.task.Status != agentworkspace.TaskActive || len(store.task.Conversation) != len(before)+1 {
		t.Fatal("failed real boundary became a completed/rules answer or replay", code)
	}
	for i := range before {
		if !reflect.DeepEqual(before[i], store.task.Conversation[i]) {
			t.Fatal("failed followup rewrote prior messages")
		}
	}
	if strings.Contains(string(raw), nowHTTPPrivateAnswer) || strings.Contains(string(raw), "找到 1 个已发布地点") {
		t.Fatal("failed model response released a fabricated opening answer")
	}
}
