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

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// HTTP routing/buffering contracts only. All authority, source and model
// replies below are explicit synthetic doubles; no SQL/provider/device proof.
const nowHTTPPrivateAnswer = "UNIT_NATIVE_MODEL_ANSWER_DO_NOT_LEAK_AFTER_REVOKE"

type nowHTTPTestCatalog struct{ httpActionSafetyCatalog }

func (nowHTTPTestCatalog) ListPlaces(_ context.Context, city, _ string) ([]foundation.Place, error) {
	return []foundation.Place{{ID: httpActionSafetyActivity, CityID: city, Name: "UNIT same native place"}}, nil
}
func (c nowHTTPTestCatalog) BuildOwnAgentContext(ctx context.Context, r acb.Request) (acb.BuiltContext, error) {
	b, e := c.httpActionSafetyCatalog.BuildOwnAgentContext(ctx, r)
	if e != nil || r.Selection != acb.PlaceSearch {
		return b, e
	}
	b.Bundle.Sections.Places = "AVAILABLE"
	for _, id := range r.PlaceIDs {
		v, _ := acb.PublicVersion("PUBLIC_PLACE", r.TaskUpdatedAt, []byte(`{"unitHTTPOnly":true}`))
		b.Bundle.Sources = append(b.Bundle.Sources, acb.Source{Kind: "PUBLIC_PLACE", ID: id, Version: v, NativeTime: r.TaskUpdatedAt})
		b.Places = append(b.Places, foundation.Place{ID: id, CityID: r.CityID, Name: "UNIT same native place"})
		b.Bundle.Places = append(b.Bundle.Places, acb.PublicPlace{ID: id, Name: "UNIT same native place"})
	}
	return b, nil
}

type nowHTTPNativeStore struct {
	*currentReadonlySearchHTTPSpy
	afterEncode func()
}

func nowHTTPAnchoredReceipt(kind string, r arp.Receipt) arp.Receipt {
	r.Items[0].Entity.Type = kind
	r.Items[0].Anchor = &arp.Anchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 57.15, Longitude: -2.09}
	return r
}
func (s *nowHTTPNativeStore) ReadAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query) (arp.Receipt, error) {
	r, e := s.currentReadonlySearchHTTPSpy.ReadAgentResultProjection(ctx, a, q)
	return nowHTTPAnchoredReceipt(q.Kind, r), e
}
func (s *nowHTTPNativeStore) ReadOwnCurrentSearch(ctx context.Context, q agenttool.CurrentSearch) (agenttool.CurrentSearchReceipt, error) {
	r, e := s.currentReadonlySearchHTTPSpy.ReadOwnCurrentSearch(ctx, q)
	if e == nil {
		r.Source = nowHTTPAnchoredReceipt(q.Query.Kind, r.Source)
	}
	return r, e
}
func (s *nowHTTPNativeStore) RevalidateAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query, r arp.Receipt) error {
	if s.afterEncode != nil {
		s.afterEncode()
	}
	return s.currentReadonlySearchHTTPSpy.RevalidateAgentResultProjection(ctx, a, q, r)
}
func (s *nowHTTPNativeStore) RevalidateOwnCurrentSearch(ctx context.Context, q agenttool.CurrentSearch, r agenttool.CurrentSearchReceipt) error {
	if s.afterEncode != nil {
		s.afterEncode()
	}
	return s.currentReadonlySearchHTTPSpy.RevalidateOwnCurrentSearch(ctx, q, r)
}

type nowHTTPFakeAnswers struct {
	t                    *testing.T
	store                *httpActionSafetyTaskStore
	eligibleCalls, sends int
	eligible             bool
	err                  error
	nilReply             bool
	answerErr            error
	reply                *nowHTTPFakeReply
}

func (a *nowHTTPFakeAnswers) Eligible(context.Context, [32]byte, agentworkspace.Task) bool {
	a.eligibleCalls++
	return a.eligible
}
func (a *nowHTTPFakeAnswers) Execute(ctx context.Context, digest [32]byte, task agentworkspace.Task) (agentworkspace.LiveReply, error) {
	a.sends++
	if digest == ([32]byte{}) || task.Status != agentworkspace.TaskActive || !reflect.DeepEqual(task, a.store.task) || task.Filters["currentQuery"] == "" {
		a.t.Fatal("live execution preceded exact ACTIVE task/filter persistence")
	}
	for _, m := range task.Conversation {
		if m.Role == "assistant" {
			a.t.Fatal("count/rules assistant appended before native model execution")
		}
	}
	if a.err != nil {
		return nil, a.err
	}
	if a.nilReply {
		return nil, nil
	}
	sources := []agentworkspace.AnswerSource{{ID: "source-unit", Title: "UNIT public source", URL: "https://example.com/now-http-unit"}}
	task.Status = agentworkspace.TaskCompleted
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: nowHTTPPrivateAnswer, Sources: sources, SourceRunID: "unit-http-run", SourceEvidenceDigest: strings.Repeat("a", 64)})
	current, e := a.store.UpdateTask(ctx, task)
	if e != nil {
		return nil, e
	}
	a.reply = &nowHTTPFakeReply{task: current, sources: sources, answerErr: a.answerErr}
	return a.reply, nil
}

type nowHTTPFakeReply struct {
	task      agentworkspace.Task
	sources   []agentworkspace.AnswerSource
	invalid   bool
	checks    int
	answerErr error
}

func (p *nowHTTPFakeReply) Task() agentworkspace.Task { return p.task }
func (p *nowHTTPFakeReply) Answer(id string) (*agentworkspace.SourcedAnswer, error) {
	if p.answerErr != nil {
		return nil, p.answerErr
	}
	task := agentworkspace.SanitizeTaskForResponse(p.task)
	query, e := agentworkspace.SourcedAnswerQueryDigest(task)
	if e != nil {
		return nil, e
	}
	digest, e := agentworkspace.SourcedAnswerTaskDigest(task)
	if e != nil {
		return nil, e
	}
	return agentworkspace.NewSourcedAnswer(agentworkspace.SourcedAnswerBinding{TaskID: task.ID, RequestID: id, CurrentQueryDigest: query, TaskSnapshotDigest: digest, SourceEvidenceDigest: strings.Repeat("a", 64), RunID: "unit-http-run", GeneratedAt: task.UpdatedAt, ValidUntil: task.UpdatedAt.Add(time.Minute)}, nowHTTPPrivateAnswer, p.sources)
}
func (p *nowHTTPFakeReply) Revalidate(context.Context) error {
	p.checks++
	if p.invalid {
		return arp.ErrChanged
	}
	return nil
}

func nowHTTPFixture(t *testing.T) (*server, *http.ServeMux, *nowHTTPNativeStore, *nowHTTPFakeAnswers, string) {
	t.Helper()
	s, mux, tasks, token := httpActionSafetyServer(t, nil)
	s.catalog = nowHTTPTestCatalog{}
	native := &nowHTTPNativeStore{currentReadonlySearchHTTPSpy: &currentReadonlySearchHTTPSpy{httpActionSafetyTaskStore: tasks}}
	s.agent = native
	a := &nowHTTPFakeAnswers{t: t, store: tasks, eligible: true}
	WithNowLiveAnswers(a)(s)
	// The shared small router omits the real server's request-ID middleware.
	// Supply the same response-header seam, rather than accepting empty IDs.
	registered := http.NewServeMux()
	registered.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "UNIT_NOW_HTTP_REQUEST")
		mux.ServeHTTP(w, r)
	})
	return s, registered, native, a, token
}

func TestNowLiveHTTPRegisteredOrdinaryAndPlacePOSTShareCurrentTaskEntities(t *testing.T) {
	for _, query := range []string{"找羽毛球活动", "找地点"} {
		t.Run(query, func(t *testing.T) {
			_, mux, native, live, token := nowHTTPFixture(t)
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"`+query+`"}`)
			if code != http.StatusOK || live.sends != 1 || live.reply == nil || live.reply.checks != 1 {
				t.Fatal("registered live POST/final revalidation", code, string(raw), live.sends)
			}
			var wire struct{ Data agentworkspace.Results }
			if json.Unmarshal(raw, &wire) != nil {
				t.Fatal("invalid actual response JSON")
			}
			r := wire.Data
			if r.Mode != "live" || r.Message != nowHTTPPrivateAnswer || r.Task == nil || r.Task.Status != agentworkspace.TaskCompleted || r.ResultSet.TaskID != r.Task.ID || r.ConversationID != r.Task.ID || len(r.ResultSet.Sources) != 1 || r.ResultSet.AnswerBinding == nil {
				t.Fatal("same completed current task/source/answer binding absent", string(raw))
			}
			if len(r.ResultSet.Items) != 1 || len(r.ResultSet.Entities) != 1 || len(r.MapEffects.PinEntityIDs) != 1 || r.ResultSet.Items[0].Entity != r.ResultSet.Entities[0] || r.ResultSet.Items[0].Detail == nil || *r.ResultSet.Items[0].Detail != r.ResultSet.Entities[0] || r.MapEffects.PinEntityIDs[0] != r.ResultSet.Entities[0].Key() {
				t.Fatal("card/detail/map identities diverged", string(raw))
			}
			if strings.Contains(string(raw), "PRIVATE_FILTER_CANARY") || len(r.Task.Conversation) != 2 || r.Task.Conversation[1].Text != nowHTTPPrivateAnswer {
				t.Fatal("rules assistant duplicated or private filters escaped", string(raw))
			}
			// Retrieval restores persisted data only. It must never execute a
			// fresh provider call or require an unexpired live dispatch grant.
			live.reply.invalid = true
			getCode, getRaw := httpActionSafetyCall(t, mux, token, "", "GET", "/v1/me/agent-tasks/"+r.Task.ID, "")
			if getCode != http.StatusOK || live.sends != 1 || live.eligibleCalls != 1 {
				t.Fatal("history GET paid again or borrowed live eligibility", getCode, string(getRaw), live.sends)
			}
			var history struct{ Data agentworkspace.Results }
			if json.Unmarshal(getRaw, &history) != nil || history.Data.Message != nowHTTPPrivateAnswer || len(history.Data.ResultSet.Sources) != 1 || history.Data.ResultSet.Sources[0].URL != r.ResultSet.Sources[0].URL || history.Data.ResultSet.AnswerBinding != nil {
				t.Fatal("history failed to restore original source/message as presentation data", string(getRaw))
			}
			if native.legacyFinal+native.late < 2 {
				t.Fatal("native projection current check omitted")
			}
		})
	}
}

func TestNowLiveHTTPModelFailureKeepsActiveWithoutCountAnswer(t *testing.T) {
	for _, query := range []string{"找羽毛球活动", "找地点"} {
		t.Run(query, func(t *testing.T) {
			_, mux, _, live, token := nowHTTPFixture(t)
			live.err = errors.New("UNIT_PROVIDER_FAILURE_SECRET")
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"`+query+`"}`)
			if code == http.StatusOK || live.sends != 1 || live.store.task.Status != agentworkspace.TaskActive || len(live.store.task.Conversation) != 1 || strings.Contains(string(raw), "UNIT_PROVIDER_FAILURE_SECRET") || strings.Contains(string(raw), nowHTTPPrivateAnswer) {
				t.Fatal("model failure became completed/rules text or leaked error", code, string(raw))
			}
		})
	}
}

func TestNowLiveHTTPAfterEncodedNativeCheckWithdrawsReplyBytes(t *testing.T) {
	for _, query := range []string{"找羽毛球活动", "找地点"} {
		t.Run(query, func(t *testing.T) {
			_, mux, native, live, token := nowHTTPFixture(t)
			native.afterEncode = func() { live.reply.invalid = true }
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"`+query+`"}`)
			if code != http.StatusConflict || live.sends != 1 || live.reply.checks != 1 || strings.Contains(string(raw), nowHTTPPrivateAnswer) || strings.Contains(string(raw), "now-http-unit") || strings.Contains(string(raw), "\"items\"") {
				t.Fatal("withdrawal after encode released private answer/source/card bytes", code, string(raw))
			}
		})
	}
}

func TestNowLiveHTTPStructuralIneligibleNeverCallsTrustedBoundary(t *testing.T) {
	for _, mode := range []string{"guest", "organization", "organization_without_header", "different_acting_user", "failed", "get"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, live, _ := nowHTTPFixture(t)
			task := agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", Query: "找地点", Status: agentworkspace.TaskActive, Filters: map[string]string{"currentQuery": "找地点"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "找地点"}}}
			method, digest := "POST", [32]byte{1}
			r := httptest.NewRequest(method, "/v1/cities/aberdeen-gb/agent/tasks", nil)
			switch mode {
			case "guest":
				digest, task.PrincipalID, task.ActingUserID = [32]byte{}, "", ""
			case "organization":
				task.PrincipalType = "organization"
				r.Header.Set("X-Birdtie-Organization-Workspace", httpActionSafetyOrgID)
			case "organization_without_header":
				task.PrincipalType = "organization"
			case "different_acting_user":
				task.ActingUserID = httpActionSafetyOtherAcct
			case "failed":
				task.Status = agentworkspace.TaskFailed
			case "get":
				r.Method = "GET"
			}
			_, _, e := s.finishCurrentNowQuery(r, digest, task, "UNIT legacy message")
			if e != nil || live.sends != 0 || live.eligibleCalls != 0 {
				t.Fatal("ineligible structure called live boundary", e, live.sends, live.eligibleCalls)
			}
		})
	}
}

func TestNowLiveHTTPDefaultOffAndIneligibleKeepExistingRulesRoute(t *testing.T) {
	for _, mode := range []string{"default_nil", "provider_ineligible"} {
		t.Run(mode, func(t *testing.T) {
			s, mux, _, live, token := nowHTTPFixture(t)
			if mode == "default_nil" {
				s.liveAnswers = nil
			} else {
				live.eligible = false
			}
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"找羽毛球活动"}`)
			if code != http.StatusOK || live.sends != 0 || strings.Contains(string(raw), nowHTTPPrivateAnswer) || live.store.task.Status != agentworkspace.TaskCompleted {
				t.Fatal("default/off legacy route changed", code, string(raw))
			}
		})
	}
}

func TestNowLiveHTTPAbsentOrInvalidReplyFailsClosed(t *testing.T) {
	for _, mode := range []string{"nil_reply", "invalid_answer_binding"} {
		t.Run(mode, func(t *testing.T) {
			_, mux, _, live, token := nowHTTPFixture(t)
			live.nilReply = mode == "nil_reply"
			if mode == "invalid_answer_binding" {
				live.answerErr = agentworkspace.ErrSourcedAnswer
			}
			code, raw := httpActionSafetyCall(t, mux, token, "", "POST", "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"找羽毛球活动"}`)
			if code == http.StatusOK || live.sends != 1 || strings.Contains(string(raw), nowHTTPPrivateAnswer) || strings.Contains(string(raw), "now-http-unit") {
				t.Fatal("absent/invalid reply released answer or fell back", code, string(raw))
			}
			if mode == "nil_reply" && live.store.task.Status != agentworkspace.TaskActive {
				t.Fatal("nil reply marked original Task completed")
			}
		})
	}
}
