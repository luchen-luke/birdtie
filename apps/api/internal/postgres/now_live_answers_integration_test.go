package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// This fixture exercises the actual original Store, Session, Task, pinned 058
// configuration, 062 ledger and 109 source pipeline. Tariffs and both provider
// HTTP responses are synthetic. The worker never runs a database or provider.
const nowLiveDriverText = "UNIT 供应商原文：公开来源提供以下信息，请打开同一消息中的来源核对。"
const nowLiveDriverPassage = "UNIT Now driver public evidence passage"

type nowLiveDriverGate bool

func (g nowLiveDriverGate) InferenceEnabled(context.Context) bool { return bool(g) }

type nowLiveDriverFixture struct {
	base                    *liveNativeFixture
	driver                  agentworkspace.LiveAnswers
	task                    agentworkspace.Task
	options                 NowLiveAnswerOptions
	search                  *agenttool.TencentWSAAdapter
	searchCalls, modelCalls int
	searchQueries           []string
	selectedCity            modelegressbudget.LiveSelectedCity
	failure                 string
}

func (d *nowLiveDriverFixture) currentQuery() string {
	if query := d.task.Filters["currentQuery"]; query != "" {
		return query
	}
	return d.task.Query
}

func (d *nowLiveDriverFixture) publicSearchContext() modelegressbudget.LiveResolvedPublicSearchContext {
	return modelegressbudget.LiveResolvedPublicSearchContext{SelectedCity: d.selectedCity, ResolvedSlots: modelegressbudget.LiveResolvedSlots{
		Operation: d.task.Intent, Target: d.task.Filters["targetIntent"], Category: d.task.Filters["category"],
		TimePreference: d.task.Filters["timePreference"], DistancePreference: d.task.Filters["distancePreference"], SearchTerm: d.task.Filters["searchTerm"],
	}}
}

func (d *nowLiveDriverFixture) compiledSearchQuery() string {
	// Independent fixed-vocabulary expectation for this FindPlace fixture;
	// do not call the production compiler to derive its own expected output.
	parts := []string{d.selectedCity.Name, d.selectedCity.CountryCode, "places"}
	for _, key := range []string{"category", "timePreference", "searchTerm"} {
		if value := d.task.Filters[key]; value != "" {
			parts = append(parts, value)
		}
	}
	if d.task.Filters["distancePreference"] == "closer" {
		parts = append(parts, "closer to city centre")
	}
	return strings.Join(append(parts, d.currentQuery()), " ")
}

func newNowLiveDriverFixture(t *testing.T) *nowLiveDriverFixture {
	return nowLiveDriverFixtureFromNative(t, newLiveNativeFixture(t))
}

func nowLiveDriverFixtureFromNative(t *testing.T, native *liveNativeFixture) *nowLiveDriverFixture {
	t.Helper()
	d := &nowLiveDriverFixture{base: native}
	b := d.base.configuration.native.private.base
	// Keep the inherited Task/binding/root intact. This fixture owns its public
	// City and current CITY Task, using the original SaveTask/context triggers.
	// A closed category is culture; library is an ordinary public search term.
	city := "unit-now-driver-" + liveNativeID(t, native)
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'UNIT Aberdeen','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:now-driver-native','合成维护者',$2)`, city, b.other.ID)
	b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, city)
	d.selectedCity = modelegressbudget.LiveSelectedCity{ID: city, Name: "UNIT Aberdeen", CountryCode: "GB"}
	original := liveSourceReplyReadTask(t, d.base, d.base.configuration.native.task.ID)
	query := "找地点 library"
	d.task = agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: city,
		Query: query, Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive,
		Filters:      map[string]string{"targetIntent": agentworkspace.FindPlace, "category": "culture", "timePreference": "today", "searchTerm": "library", "resultIDs": "unit-retained-native-entity-reference", "reason": "UNIT internal-only original explanation"},
		Conversation: append(append([]agentworkspace.Message(nil), original.Conversation...), agentworkspace.Message{Role: "user", Text: query})}
	var e error
	d.task, e = b.store.SaveTask(b.ctx, d.task)
	if e != nil {
		t.Fatal("prepare original supported Now Task", e)
	}
	if d.task.ContextType != "CITY" || d.task.ContextID == "" || d.task.CityID != city {
		t.Fatal("original SaveTask did not resolve the owned public CITY")
	}
	d.options = NowLiveAnswerOptions{OwnerID: b.person.ID, ConfigurationVersion: d.base.configuration.route.Version, RouteRevision: d.base.configuration.route.Revision, SearchPriceVersion: d.base.call.Base.Version, ModelPriceVersion: d.base.token.Base.Version}
	taskID := d.task.ID
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Retire only this new fixture Task's references before its City; the
		// inherited baseline Task, accounts, bindings and roots remain untouched.
		for _, sql := range []string{`DELETE FROM model_request_runs WHERE task_id=$1`, `DELETE FROM model_budget_audit WHERE root_trace_id IN(SELECT root_trace_id FROM model_budget_roots WHERE root_task_id=$1)`, `DELETE FROM model_budget_reservations WHERE task_id=$1`, `DELETE FROM model_egress_previews WHERE task_id=$1`, `DELETE FROM model_budget_tasks WHERE task_id=$1`, `DELETE FROM model_budget_roots WHERE root_task_id=$1`, `DELETE FROM agent_tasks WHERE id=$1`} {
			if _, e := b.pool.Exec(ctx, sql, taskID); e != nil {
				t.Errorf("owned Now driver Task cleanup: %v", e)
			}
		}
		for _, sql := range []string{`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
			if _, e := b.pool.Exec(ctx, sql, city); e != nil {
				t.Errorf("owned Now driver City cleanup: %v", e)
			}
		}
	})
	config, e := agenttool.ParseTencentWSAConfig([]byte(`{"apiKey":"unit-test-placeholder","keyName":"unit-now-native-driver"}`))
	if e != nil {
		t.Fatal(e)
	}
	d.search, e = agenttool.NewTencentWSAAdapter(config, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		d.searchCalls++
		raw, e := io.ReadAll(req.Body)
		if e != nil {
			return nil, e
		}
		var query struct {
			Query string `json:"Query"`
		}
		if e = json.Unmarshal(raw, &query); e != nil {
			t.Fatal(e)
		}
		canonical, _ := json.Marshal(query)
		want := d.compiledSearchQuery()
		if !bytes.Equal(raw, canonical) || query.Query != want || req.URL.String() != agenttool.TencentWSAEndpoint || req.GetBody != nil || !req.Close {
			t.Fatal("Now driver lost selected City/resolved slots or sent a replayable query")
		}
		d.searchQueries = append(d.searchQueries, query.Query)
		if d.failure == "search_503" {
			return liveNativeResponse(`{"error":"UNIT search unavailable"}`, 503), nil
		}
		if d.failure == "search_transport" {
			return nil, errors.New("UNIT transport failure")
		}
		page, _ := json.Marshal(map[string]string{"title": "UNIT Now native public source", "url": "https://example.com/unit-now-live-driver", "passage": nowLiveDriverPassage})
		response, _ := json.Marshal(map[string]any{"Response": map[string]any{"RequestId": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Query": query.Query, "Version": "standard", "Pages": []string{string(page)}}})
		return liveNativeResponse(string(response), 200), nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	model := liveNativeModelAdapter(t, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		d.modelCalls++
		raw, e := io.ReadAll(req.Body)
		if e != nil {
			return nil, e
		}
		var wire struct {
			Messages []modelgateway.Message `json:"messages"`
		}
		if e = json.Unmarshal(raw, &wire); e != nil || len(wire.Messages) != 2 {
			t.Fatal("Now driver MODEL wire is not the closed two-message shape", e)
		}
		var payload struct {
			Schema       string `json:"schema"`
			CurrentQuery string `json:"currentQuery"`
			Public       struct {
				City    modelegressbudget.LiveSelectedCity   `json:"selectedCity"`
				Slots   modelegressbudget.LiveResolvedSlots  `json:"resolvedSlots"`
				Sources []modelegressbudget.LivePublicSource `json:"sources"`
			} `json:"publicSearchContext"`
		}
		publicContext := d.publicSearchContext()
		if e = json.Unmarshal([]byte(wire.Messages[1].Content), &payload); e != nil || payload.Schema != modelegressbudget.LiveResolvedPublicSearchSchema || payload.CurrentQuery != d.currentQuery() || payload.Public.City != publicContext.SelectedCity || payload.Public.Slots != publicContext.ResolvedSlots || len(payload.Public.Sources) != 1 || payload.Public.Sources[0].Passage != nowLiveDriverPassage || payload.Public.Sources[0].URL != "https://example.com/unit-now-live-driver" {
			t.Fatal("MODEL differs from the same approved City/slots/current question and retrieved source", e)
		}
		if bytes.Contains(raw, []byte("合成私人会话014")) || bytes.Contains(raw, []byte("unit-retained-native-entity-reference")) || bytes.Contains(raw, []byte("UNIT internal-only original explanation")) || bytes.Contains(raw, []byte(b.person.ID)) || bytes.Contains(raw, []byte(d.task.ID)) || req.GetBody != nil || !req.Close {
			t.Fatal("Now driver model wire lost public evidence or exported private history")
		}
		if d.failure == "model_503" {
			return liveNativeResponse(`{"error":"UNIT model unavailable"}`, 503), nil
		}
		if d.failure == "model_invalid" {
			return liveNativeResponse(`{"id":"unit-invalid","model":"hy3","choices":[]}`, 200), nil
		}
		response, _ := json.Marshal(map[string]any{"id": "unit-now-live-model", "model": "hy3", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": nowLiveDriverText}}}, "usage": map[string]any{"prompt_tokens": 41, "completion_tokens": 12, "total_tokens": 53}})
		return liveNativeResponse(string(response), 200), nil
	}))
	d.driver, e = NewOwnNowLiveAnswers(b.store, d.base.controller, nowLiveDriverGate(true), model, d.search, d.options)
	if e != nil {
		t.Fatal("trusted original driver constructor", e)
	}
	return d
}

func nowLiveDriverAssertBudget(t *testing.T, d *nowLiveDriverFixture, root string, rounds int64) {
	t.Helper()
	b := d.base.configuration.native.private.base
	views, e := b.store.ReadOwnModelBudget(b.ctx, d.base.configuration.native.access, root, d.task.ID)
	if e != nil || len(views) != 4 {
		t.Fatal("original four budget scopes", e)
	}
	seen := map[string]bool{}
	for _, v := range views {
		want := modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680}
		if v.Scope == "TENANT_PERSON" || v.Scope == "SUBJECT_PERSON" {
			want = modelegressbudget.Limits{Requests: 2 * rounds, InputTokens: 196608 * rounds, OutputTokens: 768 * rounds, CostMicros: 279680 * rounds}
			if v.Limits.CostMicros != 10_000_000 {
				t.Fatal("original cumulative owner cash limit changed")
			}
		} else if v.Limits.Requests != 2 || v.Limits.CostMicros != 279680 {
			t.Fatal("fresh original root/task two-request cap changed")
		}
		if v.Currency != "CNY" || v.Allocated != want || seen[v.Scope] {
			t.Fatal("owner accounting was reset or fresh root hold changed", v.Scope)
		}
		seen[v.Scope] = true
	}
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON", "ROOT", "TASK"} {
		if !seen[scope] {
			t.Fatal("missing original budget scope")
		}
	}
}

func nowLiveDriverRequireNoExport(t *testing.T, d *nowLiveDriverFixture, before agentworkspace.Task) {
	t.Helper()
	if d.searchCalls != 0 || d.modelCalls != 0 {
		t.Fatal("denied Now request reached a provider")
	}
	if !reflect.DeepEqual(liveSourceReplyReadTask(t, d.base, d.task.ID), before) {
		t.Fatal("denied request overwrote original Task")
	}
	b := d.base.configuration.native.private.base
	var attempts int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&attempts); e != nil || attempts != 0 {
		t.Fatal("denied request consumed a native attempt", e)
	}
}

func TestNowLiveDriverOriginalTaskSourceAndSecondTurnIntegration(t *testing.T) {
	d := newNowLiveDriverFixture(t)
	b := d.base.configuration.native.private.base
	firstExpected := d.task
	reply, e := d.driver.Execute(b.ctx, d.base.configuration.native.access.SessionDigest, d.task)
	if e != nil {
		t.Fatal("native Now driver completed source answer", e)
	}
	first, ok := reply.(*nativeLiveSourceReply)
	if !ok {
		t.Fatal("driver returned an unbound reply implementation")
	}
	if first.run.original.resolved.context != d.publicSearchContext() || first.run.original.SearchQuery() != d.compiledSearchQuery() || !first.run.original.resolved.valid {
		t.Fatal("first Now turn did not retain the same native public City and slots")
	}
	d.task = reply.Task()
	if d.task.ID != firstExpected.ID || d.task.Status != agentworkspace.TaskCompleted || len(d.task.Conversation) != len(firstExpected.Conversation)+1 || d.searchCalls != 1 || d.modelCalls != 1 || reply.Revalidate(b.ctx) != nil {
		t.Fatal("same original Task did not complete exactly once")
	}
	message := d.task.Conversation[len(d.task.Conversation)-1]
	if message.Text != nowLiveDriverText || len(message.Sources) != 1 || message.Sources[0].URL != "https://example.com/unit-now-live-driver" || message.SourceRunID != first.run.id || message.SourceEvidenceDigest != first.run.batch.EvidenceDigest() {
		t.Fatal("actual model text and exact retrieved source are not in one message")
	}
	if !reflect.DeepEqual(liveSourceReplyReadTask(t, d.base, d.task.ID), d.task) || !reflect.DeepEqual(d.task.Filters, firstExpected.Filters) {
		t.Fatal("original Task restore or filters changed")
	}
	if _, e = reply.Answer("unit-now-original-request"); e != nil {
		t.Fatal(e)
	}
	answer, e := reply.Answer("unit-now-wire-projection")
	if e != nil {
		t.Fatal(e)
	}
	responseTask := agentworkspace.SanitizeTaskForResponse(d.task)
	projected := agentworkspace.Results{RequestID: "unit-now-wire-projection", ConversationID: d.task.ID, NativeProjection: true, ResultSet: agentworkspace.ResultSet{ID: "unit-now-result-set", TaskID: d.task.ID, GeneratedAt: d.task.UpdatedAt, Status: "empty"}}
	applied, e := agentworkspace.ApplySourcedAnswer(projected, responseTask, projected.RequestID, answer, time.Now())
	if e != nil || applied.Message != nowLiveDriverText || len(applied.ResultSet.Sources) != 1 || responseTask.Filters["resultIDs"] != "" || responseTask.Filters["reason"] != "" || reply.Revalidate(b.ctx) != nil {
		t.Fatal("sanitized HTTP presentation lost exact reply or changed full native authority", e)
	}
	nowLiveDriverAssertBudget(t, d, first.run.search.RootTraceID, 1)
	assertLiveNativePhase(t, d.base, first.run.search.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
	assertLiveNativePhase(t, d.base, first.run.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
	if _, e = d.driver.Execute(b.ctx, d.base.configuration.native.access.SessionDigest, firstExpected); e == nil || d.searchCalls != 1 || d.modelCalls != 1 {
		t.Fatal("stale original expected Task replayed after completion")
	}
	// An explicit new user turn keeps the Task and history, but needs fresh
	// native binding/root/run identities and may not reset owner accounting.
	d.task.Status = agentworkspace.TaskActive
	d.task.Filters["currentQuery"] = "近一点的呢？"
	// This original driver test accepts an already-resolved native Task. It
	// does not claim coverage for the separate NL parser's place-refinement path.
	d.task.Intent = agentworkspace.RefineResults
	d.task.Filters["distancePreference"] = "closer"
	d.task.Conversation = append(d.task.Conversation, agentworkspace.Message{Role: "user", Text: d.task.Filters["currentQuery"]})
	d.task, e = b.store.UpdateTask(b.ctx, d.task)
	if e != nil {
		t.Fatal(e)
	}
	if reply.Revalidate(b.ctx) == nil {
		t.Fatal("old reply became a current grant for a new turn")
	}
	secondReply, e := d.driver.Execute(b.ctx, d.base.configuration.native.access.SessionDigest, d.task)
	if e != nil {
		t.Fatal("second original Now turn", e)
	}
	second, ok := secondReply.(*nativeLiveSourceReply)
	if !ok || second.run.search.RootTraceID == first.run.search.RootTraceID || second.run.original.binding == first.run.original.binding || second.run.id == first.run.id || secondReply.Task().ID != firstExpected.ID || d.searchCalls != 2 || d.modelCalls != 2 || secondReply.Revalidate(b.ctx) != nil {
		t.Fatal("new turn reused an old original approval/root/binding")
	}
	if len(d.searchQueries) != 2 || d.searchQueries[1] != d.compiledSearchQuery() || d.searchQueries[0] == d.searchQueries[1] || second.run.original.resolved.context != d.publicSearchContext() || second.run.original.query != d.currentQuery() || !strings.Contains(d.searchQueries[1], "UNIT Aberdeen GB places culture today library closer to city centre 近一点的呢？") {
		t.Fatal("second source request lost current question or inherited native public City/slots")
	}
	d.task = secondReply.Task()
	nowLiveDriverAssertBudget(t, d, second.run.search.RootTraceID, 2)
	assertLiveNativePhase(t, d.base, first.run.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
	assertLiveNativePhase(t, d.base, second.run.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
	if !reflect.DeepEqual(liveSourceReplyReadTask(t, d.base, d.task.ID), d.task) {
		t.Fatal("second source reply failed native restore")
	}
}

func TestNowLiveDriverEligibilityAndCurrentExpectedFencesIntegration(t *testing.T) {
	for _, mode := range []string{"different_owner", "wrong_session_owner", "organization", "online", "unsupported", "failed", "zero_session", "disabled_gate", "disabled_controller", "stale_expected_task", "stale_route", "stale_configuration", "missing_search_price", "missing_model_price", "wrong_search_kind", "wrong_model_kind"} {
		t.Run(mode, func(t *testing.T) {
			d := newNowLiveDriverFixture(t)
			b := d.base.configuration.native.private.base
			expected, digest := d.task, d.base.configuration.native.access.SessionDigest
			n := d.driver.(*ownNowLiveAnswers)
			switch mode {
			case "different_owner":
				expected.PrincipalID = b.other.ID
			case "wrong_session_owner":
				digest = d.base.configuration.native.private.peer.SessionDigest
			case "organization":
				expected.PrincipalType = "organization"
			case "online":
				expected.ContextType = "ONLINE"
			case "unsupported":
				expected.Intent = agentworkspace.CreateActivity
			case "failed":
				expected.Status = agentworkspace.TaskFailed
			case "zero_session":
				digest = [32]byte{}
			case "disabled_gate":
				n.gate = nowLiveDriverGate(false)
			case "disabled_controller":
				n.controller = egressGate(t, false)
			case "stale_expected_task":
				d.task.Filters["currentQuery"] = "UNIT 新问题已替换旧快照"
				d.task.Conversation = append(d.task.Conversation, agentworkspace.Message{Role: "user", Text: d.task.Filters["currentQuery"]})
				var e error
				d.task, e = b.store.UpdateTask(b.ctx, d.task)
				if e != nil {
					t.Fatal(e)
				}
			case "stale_route":
				n.options.RouteRevision++
			case "stale_configuration":
				n.options.ConfigurationVersion = d.base.configuration.configs[0].Configuration.Version
			case "missing_search_price":
				n.options.SearchPriceVersion = "unit_missing_search_price"
			case "missing_model_price":
				n.options.ModelPriceVersion = "unit_missing_model_price"
			case "wrong_search_kind":
				n.options.SearchPriceVersion = d.base.token.Base.Version
			case "wrong_model_kind":
				n.options.ModelPriceVersion = d.base.call.Base.Version
			}
			before := liveSourceReplyReadTask(t, d.base, d.task.ID)
			if reply, e := d.driver.Execute(b.ctx, digest, expected); e == nil || reply != nil {
				t.Fatal("ineligible/stale request got a native reply", e)
			}
			nowLiveDriverRequireNoExport(t, d, before)
		})
	}
}

func TestNowLiveDriverExistingAccountConflictNotOverwrittenIntegration(t *testing.T) {
	for _, mode := range []string{"GBP", "different_limit"} {
		t.Run(mode, func(t *testing.T) {
			// Construct both original immutable accounts through native Configure
			// with their actual initial currency/limits. Existing rows are never
			// updated, deleted or repaired around their immutability guard.
			currency := "GBP"
			account := modelegressbudget.Limits{Requests: 1000, InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000, CostMicros: 10_000_000}
			if mode == "different_limit" {
				currency, account.CostMicros = "CNY", 9999999
			}
			d := nowLiveDriverFixtureFromNative(t, newLiveNativeFixtureWithAccount(t, currency, account, false))
			b := d.base.configuration.native.private.base
			var before, after []byte
			read := `SELECT jsonb_agg(to_jsonb(a) ORDER BY scope) FROM model_budget_accounts a WHERE owner_id=$1`
			if e := b.pool.QueryRow(b.ctx, read, b.person.ID).Scan(&before); e != nil {
				t.Fatal(e)
			}
			if reply, e := d.driver.Execute(b.ctx, d.base.configuration.native.access.SessionDigest, d.task); !errors.Is(e, modelegressbudget.ErrConflict) || reply != nil {
				t.Fatal("original incompatible budget account was replaced", e)
			}
			if e := b.pool.QueryRow(b.ctx, read, b.person.ID).Scan(&after); e != nil || !bytes.Equal(before, after) {
				t.Fatal("driver changed original budget currency/limits/counters", e)
			}
			nowLiveDriverRequireNoExport(t, d, d.task)
		})
	}
}

func TestNowLiveDriverProviderFailureKeepsUnknownAndActiveTaskIntegration(t *testing.T) {
	for _, mode := range []string{"search_503", "search_transport", "model_503", "model_invalid"} {
		t.Run(mode, func(t *testing.T) {
			d := newNowLiveDriverFixture(t)
			b := d.base.configuration.native.private.base
			d.failure = mode
			before := d.task
			if reply, e := d.driver.Execute(b.ctx, d.base.configuration.native.access.SessionDigest, d.task); e == nil || reply != nil {
				t.Fatal("failed provider response became a completed answer")
			}
			if !reflect.DeepEqual(liveSourceReplyReadTask(t, d.base, d.task.ID), before) || before.Status != agentworkspace.TaskActive {
				t.Fatal("provider failure completed or overwrote original Task")
			}
			wantRequests, wantCost, wantModel := int64(1), int64(80000), 0
			if strings.HasPrefix(mode, "model_") {
				wantRequests, wantCost, wantModel = 2, 279680, 1
			}
			if d.searchCalls != 1 || d.modelCalls != wantModel {
				t.Fatal("single original invocation automatically retried a provider")
			}
			var count, unknown, cost int64
			if e := b.pool.QueryRow(b.ctx, `SELECT count(*),count(*) FILTER(WHERE state='UNKNOWN' AND execution_status='LIVE_ATTEMPTED' AND cash_status='UNKNOWN' AND settled_cost IS NULL),coalesce(sum(upper_cost),0) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&count, &unknown, &cost); e != nil || count != wantRequests || unknown != wantRequests || cost != wantCost {
				t.Fatal("unknown supplier charge was refunded or relabeled", e)
			}
			var requests, allocated int64
			rows, e := b.pool.Query(b.ctx, `SELECT used_requests,allocated_cost FROM model_budget_accounts WHERE owner_id=$1 ORDER BY scope`, b.person.ID)
			if e != nil {
				t.Fatal(e)
			}
			for rows.Next() {
				if e = rows.Scan(&requests, &allocated); e != nil || requests != wantRequests || allocated != wantCost {
					rows.Close()
					t.Fatal("original owner full upper hold was reset", e)
				}
			}
			if e = rows.Err(); e != nil {
				rows.Close()
				t.Fatal(e)
			}
			rows.Close()
		})
	}
}

func TestNowLiveDriverNilAndEligibilityUnit(t *testing.T) {
	var nilDriver *ownNowLiveAnswers
	if nilDriver.Eligible(context.Background(), [32]byte{1}, agentworkspace.Task{}) {
		t.Fatal("nil driver eligible")
	}
	owner := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	d := &ownNowLiveAnswers{options: NowLiveAnswerOptions{OwnerID: owner}, gate: nowLiveDriverGate(false)}
	task := agentworkspace.Task{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", PrincipalType: "person", PrincipalID: owner, ActingUserID: owner, Status: agentworkspace.TaskActive, Intent: agentworkspace.FindPlace}
	if !d.Eligible(context.Background(), [32]byte{1}, task) {
		t.Fatal("supported local own Task ineligible")
	}
	if reply, e := d.Execute(context.Background(), [32]byte{1}, task); !errors.Is(e, modelegressbudget.ErrDenied) || reply != nil {
		t.Fatal("disabled gate reached store")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d.Eligible(ctx, [32]byte{1}, task) || d.Eligible(nil, [32]byte{1}, task) || d.Eligible(context.Background(), [32]byte{}, task) {
		t.Fatal("cancelled/missing session context became eligible")
	}
	if driver, e := NewOwnNowLiveAnswers(nil, nil, nil, nil, nil, NowLiveAnswerOptions{}); !errors.Is(e, modelegressbudget.ErrInvalid) || driver != nil {
		t.Fatal("invalid startup dependencies accepted")
	}
}
