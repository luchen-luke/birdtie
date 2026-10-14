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
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// All native permissions, sources and gateway responses here are synthetic
// boundary doubles. Registered HTTP routing is real; no DB/provider is opened.
const localGateTestAnswer = "TEST_ONLY local place record 10:00-17:00 [source-local-unit]"

type localGateHTTPReply struct{ *nowHTTPFakeReply }

func (r *localGateHTTPReply) Answer(request string) (*agentworkspace.SourcedAnswer, error) {
	task := agentworkspace.SanitizeTaskForResponse(r.task)
	query, e := agentworkspace.SourcedAnswerQueryDigest(task)
	if e != nil {
		return nil, e
	}
	digest, e := agentworkspace.SourcedAnswerTaskDigest(task)
	if e != nil {
		return nil, e
	}
	return agentworkspace.NewSourcedAnswer(agentworkspace.SourcedAnswerBinding{TaskID: task.ID, RequestID: request, CurrentQueryDigest: query, TaskSnapshotDigest: digest, SourceEvidenceDigest: strings.Repeat("a", 64), RunID: "local-native:synthetic-http", GeneratedAt: task.UpdatedAt, ValidUntil: task.UpdatedAt.Add(time.Minute)}, localGateTestAnswer, r.sources)
}

type localGateHTTPStore struct {
	*placeFollowupHTTPStore
	t          *testing.T
	localCalls int
	decline    bool
	localErr   error
	reply      *localGateHTTPReply
}

func (s *localGateHTTPStore) TryOwnLocalPlaceReply(ctx context.Context, a arp.Access) (agentworkspace.LiveReply, bool, error) {
	s.localCalls++
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(s.task))
	var expected, actual any
	_ = json.Unmarshal(a.ExpectedTask, &expected)
	_ = json.Unmarshal(raw, &actual)
	if !a.Valid() || !reflect.DeepEqual(expected, actual) || a.TaskID != s.task.ID || a.Actor.ID != s.task.PrincipalID || s.task.Status != agentworkspace.TaskActive || s.task.Filters["currentQuery"] == "" {
		s.t.Fatal("local boundary was called without exact current owned Task/session")
	}
	if s.localErr != nil {
		return nil, false, s.localErr
	}
	if s.decline {
		return nil, false, nil
	}
	task := s.task
	sources := []agentworkspace.AnswerSource{{ID: "source-local-unit", Title: "TEST_ONLY native field attribution", URL: "https://example.invalid/test-only-local-place"}}
	task.Status = agentworkspace.TaskCompleted
	task.Conversation = append(append([]agentworkspace.Message{}, task.Conversation...), agentworkspace.Message{Role: "assistant", Text: localGateTestAnswer, Sources: sources, SourceEvidenceDigest: strings.Repeat("a", 64)})
	task, e := s.UpdateTask(ctx, task)
	if e != nil {
		return nil, false, e
	}
	s.reply = &localGateHTTPReply{nowHTTPFakeReply: &nowHTTPFakeReply{task: task, sources: sources}}
	return s.reply, true, nil
}

// Only a local fact port; the original resolver is intentionally not promoted.
type localGateNoFollowupPort struct {
	*nowHTTPNativeStore
	delegate *localGateHTTPStore
}

func (s *localGateNoFollowupPort) TryOwnLocalPlaceReply(ctx context.Context, a arp.Access) (agentworkspace.LiveReply, bool, error) {
	return s.delegate.TryOwnLocalPlaceReply(ctx, a)
}

func localGateHTTPFixture(t *testing.T) (*server, *http.ServeMux, *localGateHTTPStore, *placeFollowupHTTPAnswers, string) {
	t.Helper()
	s, mux, followup, live, token := placeFollowupHTTPFixture(t)
	local := &localGateHTTPStore{placeFollowupHTTPStore: followup, t: t}
	s.agent = local
	live.eligible = false
	return s, mux, local, live, token
}

func localGateHTTPPost(t *testing.T, mux *http.ServeMux, token, query string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": query, "taskId": httpActionSafetyTaskID})
	return httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", string(body))
}

func TestLocalPlaceFollowupGateRegisteredPersonLocalAnswerWithoutModelEligibility(t *testing.T) {
	for _, mode := range []string{"no_model", "ineligible_model"} {
		for _, query := range []string{"what are its opening hours", "它的开放时间是什么？", "What are the opening hours of Maritime Museum?", "What is the address of Maritime Museum?"} {
			t.Run(mode+"/"+query, func(t *testing.T) {
				s, mux, store, live, token := localGateHTTPFixture(t)
				if mode == "no_model" {
					s.liveAnswers = nil
				}
				before := append([]agentworkspace.Message{}, store.task.Conversation...)
				code, raw := localGateHTTPPost(t, mux, token, query)
				if code != http.StatusOK || store.calls != 1 || store.localCalls != 1 || live.sends != 0 || live.eligibleCalls != 0 || store.reply == nil || store.reply.checks != 1 {
					t.Fatal("local answer waited for model eligibility or skipped original current boundary", code, string(raw), store.calls, store.localCalls, live.sends)
				}
				var wire struct{ Data agentworkspace.Results }
				if json.Unmarshal(raw, &wire) != nil || wire.Data.Task == nil {
					t.Fatal("invalid registered POST result")
				}
				r := wire.Data
				if r.Task.ID != httpActionSafetyTaskID || r.Task.Status != agentworkspace.TaskCompleted || r.Mode != "rules" || r.Message != localGateTestAnswer || len(r.Task.Conversation) != len(before)+2 || r.Task.Conversation[len(before)].Text != query || len(r.ResultSet.Sources) != 1 || r.ResultSet.AnswerBinding == nil {
					t.Fatal("local fact lost same Task, raw turn or source binding", string(raw))
				}
				for i := range before {
					if !reflect.DeepEqual(before[i], r.Task.Conversation[i]) {
						t.Fatal("native local followup overwrote prior conversation")
					}
				}
				if len(r.ResultSet.Entities) != 1 || len(r.ResultSet.Items) != 1 || len(r.MapEffects.PinEntityIDs) != 1 || r.ResultSet.Entities[0] != r.ResultSet.Items[0].Entity || r.MapEffects.PinEntityIDs[0] != r.ResultSet.Entities[0].Key() || r.ResultSet.Items[0].Detail == nil || *r.ResultSet.Items[0].Detail != r.ResultSet.Entities[0] {
					t.Fatal("local card/detail/map stopped using one current native entity")
				}
				if strings.Contains(string(raw), "找到 1 个已发布地点") || strings.Contains(string(raw), nowHTTPPrivateAnswer) || strings.Contains(string(raw), agentworkspace.PlaceFollowupGenerationFilter) {
					t.Fatal("count/model answer or private native proof escaped")
				}
			})
		}
	}
}

func TestLocalPlaceFollowupGateRegisteredDeclineHasNoRulesOrProviderFallback(t *testing.T) {
	for _, mode := range []string{"no_model", "ineligible_model"} {
		t.Run(mode, func(t *testing.T) {
			s, mux, store, live, token := localGateHTTPFixture(t)
			if mode == "no_model" {
				s.liveAnswers = nil
			}
			store.decline = true
			code, raw := localGateHTTPPost(t, mux, token, "what are its opening hours")
			if code != http.StatusServiceUnavailable || !strings.Contains(string(raw), `"code":"place_answer_unavailable"`) || store.calls != 1 || store.localCalls != 1 || live.sends != 0 || store.task.Status != agentworkspace.TaskActive || len(store.task.Conversation) != 3 || store.task.Conversation[2].Role != "user" {
				t.Fatal("declined local fact became an answer or external call", code, string(raw), store.task)
			}
			if strings.Contains(string(raw), "找到") || strings.Contains(string(raw), localGateTestAnswer) || strings.Contains(string(raw), `"items"`) || store.reply != nil {
				t.Fatal("unanswered turn emitted synthetic completion/source/card bytes")
			}
		})
	}
}

func TestLocalPlaceFollowupGateRegisteredMissingHoursKeepsOriginal422(t *testing.T) {
	_, mux, store, live, token := localGateHTTPFixture(t)
	store.localErr = agentworkspace.ErrLocalPlaceHoursUnavailable
	code, raw := localGateHTTPPost(t, mux, token, "what are its opening hours")
	if code != http.StatusUnprocessableEntity || !strings.Contains(string(raw), `"code":"place_hours_unavailable"`) || store.calls != 1 || store.localCalls != 1 || live.sends != 0 || live.eligibleCalls != 0 || store.task.Status != agentworkspace.TaskActive || len(store.task.Conversation) != 3 {
		t.Fatal("missing independently permitted hours became eligibility/fallback answer", code, string(raw))
	}
}

func TestLocalPlaceFollowupGateNonLocalQueriesRetainTheOriginalModelGate(t *testing.T) {
	for _, query := range []string{"它周末几点开门？", "what are its admission fees?", "它怎么去？", "那 Maritime Museum 呢？"} {
		t.Run(query, func(t *testing.T) {
			_, mux, store, live, token := localGateHTTPFixture(t)
			before, _ := json.Marshal(store.task)
			code, raw := localGateHTTPPost(t, mux, token, query)
			after, _ := json.Marshal(store.task)
			if code != http.StatusServiceUnavailable || !strings.Contains(string(raw), "place_answer_unavailable") || store.calls != 0 || store.localCalls != 0 || live.sends != 0 || string(before) != string(after) {
				t.Fatal("unconnected broader question acquired the local closed grammar", code, string(raw))
			}
		})
	}
}

func TestLocalPlaceFollowupGateRequiresBothTrustedNativePorts(t *testing.T) {
	for _, mode := range []string{"no_local_port", "no_original_resolver"} {
		t.Run(mode, func(t *testing.T) {
			s, mux, store, live, token := localGateHTTPFixture(t)
			if mode == "no_local_port" {
				s.agent = store.placeFollowupHTTPStore
			} else {
				s.agent = &localGateNoFollowupPort{nowHTTPNativeStore: store.nowHTTPNativeStore, delegate: store}
			}
			before, _ := json.Marshal(store.task)
			code, _ := localGateHTTPPost(t, mux, token, "what are its opening hours")
			after, _ := json.Marshal(store.task)
			if code != http.StatusServiceUnavailable || store.calls != 0 || store.localCalls != 0 || live.sends != 0 || string(before) != string(after) {
				t.Fatal("partial/untrusted server dependencies became local continuation authority", code)
			}
		})
	}
}

func TestLocalPlaceFollowupGateIdentityAndOriginalSelectionDenialsStayBeforeLocalAnswer(t *testing.T) {
	for _, mode := range []string{"guest", "different_actor", "organization", "zero_digest", "different_acting_user", "different_context"} {
		t.Run(mode, func(t *testing.T) {
			s, _, store, live, _ := localGateHTTPFixture(t)
			r := httptest.NewRequest("POST", "/v1/cities/aberdeen-gb/agent/tasks", nil)
			actor, digest, task := identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"}, [32]byte{1}, store.task
			switch mode {
			case "guest":
				actor = identity.Actor{}
			case "different_actor":
				actor.ID = httpActionSafetyOtherAcct
			case "organization":
				r.Header.Set("X-Birdtie-Organization-Workspace", httpActionSafetyOrgID)
			case "zero_digest":
				digest = [32]byte{}
			case "different_acting_user":
				task.ActingUserID = httpActionSafetyOtherAcct
			case "different_context":
				task.ContextType = "ONLINE"
			}
			w := httptest.NewRecorder()
			_, _, continued := s.continuePublicPlaceFollowup(w, r, digest, actor, task, "what are its opening hours")
			if continued || w.Code != http.StatusForbidden || store.calls != 0 || store.localCalls != 0 || live.sends != 0 || live.eligibleCalls != 0 {
				t.Fatal("identity mismatch reached local/native/provider dependency", w.Code)
			}
		})
	}
	for _, nativeError := range []error{agentworkspace.ErrPlaceFollowupAmbiguous, agentworkspace.ErrPlaceFollowupChanged, arp.ErrDenied} {
		_, mux, store, live, token := localGateHTTPFixture(t)
		store.err = nativeError
		before, _ := json.Marshal(store.task)
		code, _ := localGateHTTPPost(t, mux, token, "what are its opening hours")
		after, _ := json.Marshal(store.task)
		if code == http.StatusOK || store.calls != 1 || store.localCalls != 0 || live.sends != 0 || string(before) != string(after) {
			t.Fatal("original unique/retired/entity guard was bypassed", code, nativeError)
		}
	}
}

func TestLocalPlaceFollowupGateEligibleDeclineKeepsOriginalExternalBoundary(t *testing.T) {
	_, mux, store, live, token := localGateHTTPFixture(t)
	store.decline, live.eligible = true, true
	store.task.Filters["timePreference"] = "weekend"
	code, raw := localGateHTTPPost(t, mux, token, "what are its opening hours")
	if code != http.StatusOK || store.calls != 1 || store.localCalls != 1 || live.sends != 1 || live.eligibleCalls != 1 || !strings.Contains(string(raw), nowHTTPPrivateAnswer) {
		t.Fatal("explicit original model eligibility stopped authorizing its existing boundary", code, string(raw))
	}
}

func TestLocalPlaceFollowupGateLocalSourceRevalidationStillWithholdsWithdrawnBytes(t *testing.T) {
	_, mux, store, live, token := localGateHTTPFixture(t)
	store.afterEncode = func() {
		if store.reply != nil {
			store.reply.invalid = true
		}
	}
	code, raw := localGateHTTPPost(t, mux, token, "what are its opening hours")
	if code != http.StatusConflict || store.reply == nil || store.reply.checks != 1 || !errors.Is(store.reply.Revalidate(context.Background()), arp.ErrChanged) || live.sends != 0 || strings.Contains(string(raw), localGateTestAnswer) || strings.Contains(string(raw), "test-only-local-place") || strings.Contains(string(raw), `"items"`) {
		t.Fatal("local eligibility separation removed the final source withdrawal brake", code, string(raw))
	}
}
