package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

const selectedHTTPPlaceID = "33333333-3333-4333-8333-333333333333"

type selectedHTTPStore struct {
	*localGateHTTPStore
	selectedCalls int
	selectedID    string
	selectedErr   error
}

func (s *selectedHTTPStore) ContinueOwnSelectedPlace(ctx context.Context, a arp.Access, query string, selected agentworkspace.SelectedPlaceSelector) (agentworkspace.Task, error) {
	s.selectedCalls++
	s.selectedID = selected.PlaceID
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(s.task))
	var expected, actual any
	_ = json.Unmarshal(a.ExpectedTask, &expected)
	_ = json.Unmarshal(raw, &actual)
	if !a.Valid() || !reflect.DeepEqual(expected, actual) || a.Actor.ID != s.task.PrincipalID || a.TaskID != s.task.ID {
		s.t.Fatal("selected port lost native owned Task/session CAS")
	}
	if s.selectedErr != nil {
		return agentworkspace.Task{}, s.selectedErr
	}
	f, day, ok := agentworkspace.ParseSelectedPlaceFollowup(query)
	if !ok || !selected.Valid() {
		return agentworkspace.Task{}, arp.ErrDenied
	}
	task := s.task
	filters := map[string]string{}
	for k, v := range task.Filters {
		filters[k] = v
	}
	task.Filters = filters
	if task.Status == agentworkspace.TaskCompleted {
		task.Conversation = append(append([]agentworkspace.Message{}, task.Conversation...), agentworkspace.Message{Role: "user", Text: query})
	}
	task.Intent, task.Status = agentworkspace.FindPlace, agentworkspace.TaskActive
	task.Filters["currentQuery"], task.Filters["searchTerm"], task.Filters["targetIntent"] = query, "UNIT exact map-selected place", agentworkspace.FindPlace
	task.Filters[agentworkspace.PlaceFollowupIDFilter], task.Filters[agentworkspace.PlaceFollowupGenerationFilter], task.Filters[agentworkspace.PlaceFollowupTopicFilter] = selected.PlaceID, strings.Repeat("a", 64), f.Topic
	task.Filters[agentworkspace.SelectedPlaceContextFilter] = "native-map-v1"
	if day == 2 {
		task.Filters[agentworkspace.SelectedPlaceDayFilter] = "2"
	}
	return s.UpdateTask(ctx, task)
}

type selectedHTTPReply struct{ *nowHTTPFakeReply }

func (r *selectedHTTPReply) Answer(request string) (*agentworkspace.SourcedAnswer, error) {
	task := agentworkspace.SanitizeTaskForResponse(r.task)
	query, e := agentworkspace.SourcedAnswerQueryDigest(task)
	if e != nil {
		return nil, e
	}
	digest, e := agentworkspace.SourcedAnswerTaskDigest(task)
	if e != nil {
		return nil, e
	}
	return agentworkspace.NewSourcedAnswer(agentworkspace.SourcedAnswerBinding{TaskID: task.ID, RequestID: request, CurrentQueryDigest: query, TaskSnapshotDigest: digest, SourceEvidenceDigest: strings.Repeat("a", 64), RunID: "local-native:synthetic-selected-http", GeneratedAt: task.UpdatedAt, ValidUntil: task.UpdatedAt.Add(time.Minute)}, task.Conversation[len(task.Conversation)-1].Text, r.sources)
}

func (s *selectedHTTPStore) TryOwnLocalPlaceReply(ctx context.Context, a arp.Access) (agentworkspace.LiveReply, bool, error) {
	_, handled, e := s.localGateHTTPStore.TryOwnLocalPlaceReply(ctx, a)
	if e != nil || !handled {
		return nil, handled, e
	}
	task := s.task
	if task.Filters[agentworkspace.SelectedPlaceDayFilter] == "2" {
		task.Conversation[len(task.Conversation)-1].Text = agentworkspace.SelectedPlaceDayDraftText(task.Filters["searchTerm"])
		task, e = s.UpdateTask(ctx, task)
		if e != nil {
			return nil, false, e
		}
	}
	r := &selectedHTTPReply{&nowHTTPFakeReply{task: task, sources: s.reply.sources}}
	return r, true, nil
}

func TestSelectedPlaceFollowupRegisteredCurrentMapSelectorAndDayDraft(t *testing.T) {
	for _, query := range []string{"地址", "它周末几点开门？", "把这个加入第二天"} {
		t.Run(query, func(t *testing.T) {
			s, mux, local, live, token := localGateHTTPFixture(t)
			store := &selectedHTTPStore{localGateHTTPStore: local}
			s.agent = store
			body, _ := json.Marshal(map[string]any{"query": query, "taskId": httpActionSafetyTaskID, "selectedPlace": map[string]string{"placeId": selectedHTTPPlaceID}})
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(body))
			if code != http.StatusOK || store.selectedCalls != 1 || store.calls != 0 || store.localCalls != 1 || live.sends != 0 || live.eligibleCalls != 0 || store.selectedID != selectedHTTPPlaceID {
				t.Fatal("explicit map selector lost exact native/local no-model path", code, string(raw))
			}
			if query == "把这个加入第二天" && (!strings.Contains(string(raw), "UNIT exact map-selected place") || !strings.Contains(string(raw), "第2天") || !strings.Contains(string(raw), "尚未保存")) {
				t.Fatal("pending day target was lost or claimed saved", string(raw))
			}
			if strings.Contains(string(raw), agentworkspace.PlaceFollowupGenerationFilter) || strings.Contains(string(raw), agentworkspace.SelectedPlaceDayFilter) {
				t.Fatal("private source correlation leaked")
			}
		})
	}
}

func TestSelectedPlaceFollowupRejectsForeignRetiredAndMixedSelectorsWithoutModel(t *testing.T) {
	for _, mode := range []string{"retired", "denied", "mixed_bounds", "mixed_online", "smuggled_fields", "invalid_id", "unrelated_action", "local_declined"} {
		t.Run(mode, func(t *testing.T) {
			s, mux, local, live, token := localGateHTTPFixture(t)
			store := &selectedHTTPStore{localGateHTTPStore: local}
			s.agent = store
			body := map[string]any{"query": "地址", "taskId": httpActionSafetyTaskID, "selectedPlace": map[string]any{"placeId": selectedHTTPPlaceID}}
			expected := http.StatusBadRequest
			switch mode {
			case "retired":
				store.selectedErr = agentworkspace.ErrPlaceFollowupChanged
				expected = http.StatusConflict
			case "denied":
				store.selectedErr = arp.ErrDenied
				expected = http.StatusForbidden
			case "mixed_bounds":
				body["mapBounds"] = map[string]float64{"west": -3, "south": 56, "east": -1, "north": 58}
			case "mixed_online":
				body["onlinePlaceContext"] = map[string]any{}
			case "smuggled_fields":
				body["selectedPlace"] = map[string]any{"placeId": selectedHTTPPlaceID, "membership": "forged", "latitude": 57.1}
			case "invalid_id":
				body["selectedPlace"] = map[string]any{"placeId": "osm:node:123"}
			case "unrelated_action":
				body["query"] = "帮我预订它"
			case "local_declined":
				store.decline = true
				expected = http.StatusServiceUnavailable
			}
			before, _ := json.Marshal(store.task)
			wire, _ := json.Marshal(body)
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(wire))
			after, _ := json.Marshal(store.task)
			if code != expected || live.sends != 0 || live.eligibleCalls != 0 || strings.Contains(string(raw), localGateTestAnswer) {
				t.Fatal("denied selection became fallback or leaked old answer", mode, code, string(raw))
			}
			if mode != "local_declined" && !errors.Is(store.selectedErr, nil) && string(before) != string(after) {
				t.Fatal("denial mutated existing task")
			}
		})
	}
}
