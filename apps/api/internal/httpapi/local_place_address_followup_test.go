package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

// Only the registered handler and native-port contracts are exercised here.
// Synthetic permissions/facts are never inserted or sent to a provider.
func TestLocalPlaceAddressFollowupHTTPUsesOriginalNativePortsWithoutModel(t *testing.T) {
	for _, mode := range []string{"no_model", "ineligible_model"} {
		for _, query := range []string{"地址", "它的地址是什么？", "What is its address?", "Where is it?"} {
			t.Run(mode+"/"+query, func(t *testing.T) {
				s, mux, store, live, token := localGateHTTPFixture(t)
				if mode == "no_model" {
					s.liveAnswers = nil
				}
				before := append([]agentworkspace.Message(nil), store.task.Conversation...)
				code, raw := localGateHTTPPost(t, mux, token, query)
				if code != http.StatusOK || store.calls != 1 || store.localCalls != 1 || live.sends != 0 || live.eligibleCalls != 0 {
					t.Fatal("bounded address waited for model/search eligibility", code, string(raw), store.calls, store.localCalls, live.sends)
				}
				var wire struct{ Data agentworkspace.Results }
				if json.Unmarshal(raw, &wire) != nil || wire.Data.Task == nil {
					t.Fatal("invalid response")
				}
				r := wire.Data
				if r.Task.ID != httpActionSafetyTaskID || r.Task.Status != agentworkspace.TaskCompleted || r.Mode != "rules" || len(r.Task.Conversation) != len(before)+2 || r.Task.Conversation[len(before)].Text != query || store.task.Filters[agentworkspace.PlaceFollowupTopicFilter] != agentworkspace.PlaceAddress {
					t.Fatal("address lost exact current query, canonical subject or original task", string(raw))
				}
				for i := range before {
					if !reflect.DeepEqual(before[i], r.Task.Conversation[i]) {
						t.Fatal("old history overwritten")
					}
				}
				if len(r.ResultSet.Entities) != 1 || len(r.ResultSet.Items) != 1 || len(r.MapEffects.PinEntityIDs) != 1 || r.ResultSet.Entities[0] != r.ResultSet.Items[0].Entity || r.MapEffects.PinEntityIDs[0] != r.ResultSet.Entities[0].Key() || r.ResultSet.Items[0].Detail == nil || *r.ResultSet.Items[0].Detail != r.ResultSet.Entities[0] || len(r.ResultSet.Sources) != 1 || r.ResultSet.AnswerBinding == nil {
					t.Fatal("source/card/map/detail canonical entity diverged")
				}
			})
		}
	}
}

func TestLocalPlaceAddressFollowupHTTPKeepsAmbiguousRevokedAndMissingSelectionClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"ambiguous", agentworkspace.ErrPlaceFollowupAmbiguous, http.StatusConflict},
		{"revoked_or_expired", agentworkspace.ErrPlaceFollowupChanged, http.StatusConflict},
		{"missing", agentworkspace.ErrPlaceFollowupNotFound, http.StatusNotFound},
		{"denied", arp.ErrDenied, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, mux, store, live, token := localGateHTTPFixture(t)
			s.liveAnswers = nil
			store.err = test.err
			before, _ := json.Marshal(store.task)
			code, raw := localGateHTTPPost(t, mux, token, "地址")
			after, _ := json.Marshal(store.task)
			if code != test.status || store.calls != 1 || store.localCalls != 0 || live.sends != 0 || live.eligibleCalls != 0 || string(before) != string(after) || strings.Contains(string(raw), localGateTestAnswer) {
				t.Fatal("selection/permission denial mutated task or became fallback", code, string(raw))
			}
		})
	}
}

func TestLocalPlaceAddressFollowupHTTPFinalWithdrawalWithholdsSourceAndCardBytes(t *testing.T) {
	s, mux, store, live, token := localGateHTTPFixture(t)
	s.liveAnswers = nil
	store.afterEncode = func() {
		if store.reply != nil {
			store.reply.invalid = true
		}
	}
	code, raw := localGateHTTPPost(t, mux, token, "地址")
	if code != http.StatusConflict || store.reply == nil || store.reply.checks != 1 || live.sends != 0 || strings.Contains(string(raw), localGateTestAnswer) || strings.Contains(string(raw), "test-only-local-place") || strings.Contains(string(raw), `"items"`) {
		t.Fatal("new address grammar bypassed original final source revalidation", code, string(raw))
	}
}
