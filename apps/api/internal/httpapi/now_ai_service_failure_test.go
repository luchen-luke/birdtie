package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

type aiServiceHTTPTestTransport func(*http.Request) (*http.Response, error)

func (f aiServiceHTTPTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func aiServiceHTTPTestFailure(t *testing.T, status int, body string) error {
	t.Helper()
	config, e := modelgateway.ParseTencentTokenHubConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"deepseek-v4-pro-0813","maxOutputTokens":768,"apiKey":"synthetic-unit-placeholder"}`))
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	a, e := modelgateway.NewTencentTokenHubAdapter(config, aiServiceHTTPTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	_, err := a.Complete(context.Background(), modelgateway.ProviderRequest{TaskKind: modelgateway.ActivityQuery, PromptVersion: "unit.http.availability.v1", OutputMode: modelgateway.Text, OutputSchemaVersion: "air.answer.v1", MaxOutputTokens: 128, DeadlineAt: time.Now().Add(time.Minute), Messages: []modelgateway.Message{{Role: "system", Content: "Synthetic public context"}, {Role: "user", Content: "Synthetic public question"}}})
	if calls != 1 || err == nil {
		t.Fatal("fixture did not exercise one actual selected-model adapter failure")
	}
	return modelgateway.WithLiveFailureReason(err, "provider_UNKNOWN")
}

// Original 2026-10-09 receipt: 402/403004/source=gateway. Its provider packet
// is NOT_CAPTURED. This is a minimal synthetic body from those known fields.
const aiServiceHTTPTestBody = `{"error":{"code":"403004","source":"gateway","message":"PRIVATE_BODY_SENTINEL https://private.example/?key=PRIVATE_KEY"}}`
const aiServicePublicMessage = "AI 服务目前不可用，本轮没有生成新回答。原对话仍保留。"

func assertAIServiceHTTPResponse(t *testing.T, status int, raw []byte) {
	t.Helper()
	var response struct {
		Error struct{ Code, Message string }
	}
	if status != http.StatusServiceUnavailable || json.Unmarshal(raw, &response) != nil || response.Error.Code != "ai_service_unavailable" || response.Error.Message != aiServicePublicMessage {
		t.Fatal("known provider service failure remained generic or lost recovery contract", status, string(raw))
	}
	for _, forbidden := range []string{"PRIVATE", "private.example", "403004", "402", "deepseek", "synthetic-unit-placeholder", nowHTTPPrivateAnswer, "找到 0 个已发布地点", "找到 1 个已发布地点", "余额", "充值"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("service failure exposed internals or fabricated an answer/account diagnosis", forbidden)
		}
	}
}

func TestNowAIServiceFailureRegisteredFreshQueriesRemainActiveWithoutFakeReply(t *testing.T) {
	for _, query := range []string{"找羽毛球活动", "找地点"} {
		t.Run(query, func(t *testing.T) {
			_, mux, _, live, token := nowHTTPFixture(t)
			live.err = aiServiceHTTPTestFailure(t, 402, aiServiceHTTPTestBody)
			status, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"`+query+`"}`)
			assertAIServiceHTTPResponse(t, status, raw)
			if live.sends != 1 || live.reply != nil || live.store.task.Status != agentworkspace.TaskActive || len(live.store.task.Conversation) != 1 || live.store.task.Conversation[0].Role != "user" {
				t.Fatal("failure completed the task, fabricated a reply or retried")
			}
		})
	}
}

func TestNowAIServiceFailureRegisteredFollowupKeepsExactEarlierMessages(t *testing.T) {
	_, mux, store, live, token := placeFollowupHTTPFixture(t)
	before := append([]agentworkspace.Message(nil), store.task.Conversation...)
	live.err = aiServiceHTTPTestFailure(t, 402, aiServiceHTTPTestBody)
	status, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"它周末几点开门？","taskId":"`+httpActionSafetyTaskID+`"}`)
	assertAIServiceHTTPResponse(t, status, raw)
	if store.calls != 1 || live.sends != 1 || live.reply != nil || store.task.ID != httpActionSafetyTaskID || store.task.Status != agentworkspace.TaskActive || len(store.task.Conversation) != len(before)+1 || !reflect.DeepEqual(before, store.task.Conversation[:len(before)]) || store.task.Conversation[len(before)].Role != "user" {
		t.Fatal("known model failure rewrote earlier history or produced a fake completed followup")
	}
}

func TestNowAIServiceFailureUnrelatedAndHigherPriorityErrorsKeepOriginalMapping(t *testing.T) {
	known := aiServiceHTTPTestFailure(t, 402, aiServiceHTTPTestBody)
	for _, tc := range []struct {
		name string
		err  error
		org  bool
	}{
		{"unknown", errors.New("synthetic unrelated"), false},
		{"lookalike", errors.New("MODEL status=402 business=403004 source=gateway"), false},
		{"wrong_http", aiServiceHTTPTestFailure(t, 403, aiServiceHTTPTestBody), false},
		{"wrong_business", aiServiceHTTPTestFailure(t, 402, `{"error":{"code":"403005","source":"gateway"}}`), false},
		{"missing_body", aiServiceHTTPTestFailure(t, 402, ""), false},
		{"organization", known, true},
		{"cancelled", errors.Join(known, context.Canceled), false},
		{"deadline", errors.Join(known, context.DeadlineExceeded), false},
		{"budget_denied", errors.Join(known, modelegressbudget.ErrDenied), false},
		{"budget_unknown", errors.Join(known, modelegressbudget.ErrUnavailable), false},
		{"budget_limit", errors.Join(known, modelegressbudget.ErrBudget), false},
		{"budget_conflict", errors.Join(known, modelegressbudget.ErrConflict), false},
		{"source_denied", errors.Join(known, arp.ErrDenied), false},
		{"source_changed", errors.Join(known, arp.ErrChanged), false},
		{"org_denied", errors.Join(known, organization.ErrForbidden), false},
		{"settlement", modelgateway.WithLiveFailureReason(known, "live_release_or_settlement_unknown"), false},
		{"current", modelgateway.WithLiveFailureReason(known, "live_current_authority_unavailable"), false},
		{"local_place", errors.Join(known, agentworkspace.ErrLicensedPlaceFactsUnavailable), false},
		{"place_hours", errors.Join(known, agentworkspace.ErrLocalPlaceHoursUnavailable), false},
		{"retention", errors.Join(known, agentworkspace.ErrSearchRetentionPending), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			agentTaskWriteFailure(w, tc.err, tc.org)
			if w.Code == 200 || strings.Contains(w.Body.String(), "ai_service_unavailable") || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal("AI service classification replaced original failure", w.Code, w.Body.String())
			}
		})
	}
}
