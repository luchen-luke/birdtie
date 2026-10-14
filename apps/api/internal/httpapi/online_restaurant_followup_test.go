package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Real registered handler, synthetic native history authority. No real source,
// SQL or provider is used by these tests; frozen-build probes are separate.
type restaurantOwnedHistoryStore struct {
	*restaurantNewTargetTaskStore
	t       *testing.T
	reads   int
	err     error
	nilRead bool
	final   error
	handle  *restaurantOwnedHistoryRead
}

type restaurantOwnedHistoryRead struct {
	store    *restaurantOwnedHistoryStore
	expected []byte
	checks   int
}

type restaurantOwnedHistoryCatalog struct {
	nowHTTPTestCatalog
	city foundation.PublicCatalog
}

func (c restaurantOwnedHistoryCatalog) GetCity(ctx context.Context, id string) (foundation.City, error) {
	return c.city.GetCity(ctx, id)
}

func (s *restaurantOwnedHistoryStore) SaveTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	if task.ContextType == "" && task.CityID != "" {
		task.ContextType = "CITY" // The real PostgreSQL INSERT default.
	}
	return s.restaurantNewTargetTaskStore.SaveTask(ctx, task)
}

func (s *restaurantOwnedHistoryStore) CaptureOwnHumanReply(context.Context, arp.Access) (agentworkspace.Task, error) {
	s.t.Fatal("public restaurant/unsupported route acquired native rules capture")
	return agentworkspace.Task{}, arp.ErrDenied
}

func (s *restaurantOwnedHistoryStore) ReadOwnMessageResults(_ context.Context, a arp.Access) (agentworkspace.MessageResultsRead, error) {
	s.reads++
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(s.task))
	if !a.Valid() || a.Actor.ID != s.task.PrincipalID || a.Actor.AccountType != "person" || a.TaskID != s.task.ID || !reflect.DeepEqual(raw, []byte(a.ExpectedTask)) {
		return nil, arp.ErrDenied
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.nilRead {
		return nil, nil
	}
	s.handle = &restaurantOwnedHistoryRead{store: s, expected: raw}
	return s.handle, nil
}

func (r *restaurantOwnedHistoryRead) Results() []agentworkspace.MessageResult {
	return nil // The fixture has public snapshots, no native memberships.
}

func (r *restaurantOwnedHistoryRead) Revalidate(context.Context) error {
	r.checks++
	if r.store.final != nil {
		return r.store.final
	}
	raw, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(r.store.task))
	if !reflect.DeepEqual(raw, r.expected) {
		return arp.ErrChanged
	}
	return nil
}

func restaurantOwnedHistoryFixture(t *testing.T) (*server, *http.ServeMux, *restaurantOwnedHistoryStore, *nowHTTPFakeAnswers, string, *openSource) {
	t.Helper()
	s, h, tasks, token, source, _, _ := followupRestaurantServer(t)
	s.catalog = restaurantOwnedHistoryCatalog{city: s.catalog}
	store := &restaurantOwnedHistoryStore{restaurantNewTargetTaskStore: &restaurantNewTargetTaskStore{httpActionSafetyTaskStore: tasks}, t: t}
	s.agent = store
	model := &nowHTTPFakeAnswers{t: t, store: tasks, eligible: true}
	s.liveAnswers = model
	return s, h, store, model, token, source
}

func TestOnlineRestaurantFollowupSignedOrdinaryPOSTAndGETShareAuthoritativeHistoryEnvelope(t *testing.T) {
	_, h, store, model, token, source := restaurantOwnedHistoryFixture(t)
	var original *agentworkspace.OnlinePlaceSearch
	for turn, query := range []string{"有没有好的餐厅", "compare sports", "第一家地址"} {
		taskID := ""
		if turn > 0 {
			taskID = store.task.ID
		}
		code, raw, post := restaurantFollowupCall(t, h, token, query, taskID, nil)
		if code != http.StatusOK || post.Task == nil || post.MessageResults == nil || len(post.MessageResults) != 0 || store.reads != turn*2+1 || store.handle == nil || store.handle.checks != 1 {
			t.Fatal("ordinary signed POST has null/unread/unrevalidated history envelope", code, string(raw), store.reads)
		}
		if turn == 0 {
			original = post.Task.Conversation[1].ExternalPlaceReply
			if original == nil || post.OnlinePlaceSearch == nil {
				t.Fatal("original restaurant snapshot missing")
			}
		} else if post.OnlinePlaceSearch != nil || post.Task.Conversation[len(post.Task.Conversation)-1].ExternalPlaceReply != nil || post.Task.Status != agentworkspace.TaskFailed || post.Task.Intent != []string{"", agentworkspace.CompareResults, agentworkspace.UnsupportedIntent}[turn] || strings.Contains(post.Message, "本轮复用已保存的公开资料") {
			t.Fatal("new sports topic or later ordinal reached an older restaurant snapshot", string(raw))
		}
		getCode, getRaw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+store.task.ID, "")
		var envelope struct{ Data agentworkspace.Results }
		if getCode != http.StatusOK || json.Unmarshal(getRaw, &envelope) != nil || envelope.Data.MessageResults == nil || !reflect.DeepEqual(post.MessageResults, envelope.Data.MessageResults) || !reflect.DeepEqual(post.Task.Conversation, envelope.Data.Task.Conversation) || store.reads != (turn+1)*2 || store.handle.checks != 1 {
			t.Fatal("same Task POST/GET history envelopes differ", getCode, string(getRaw))
		}
		if !reflect.DeepEqual(post.Task.Conversation[1].ExternalPlaceReply, original) || len(post.Task.Conversation) != (turn+1)*2 || source.calls != 0 || model.sends != 0 || model.eligibleCalls != 0 || store.followupCalls != 0 {
			t.Fatal("source/model called, old snapshot changed, or conversation lost", code)
		}
	}
}

func TestOnlineRestaurantFollowupOrdinaryPOSTHistoryReadFailureWithholdsResultBytes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		final error
		nil   bool
		code  int
	}{
		{"denied", arp.ErrDenied, nil, false, 403},
		{"changed", arp.ErrChanged, nil, false, 409},
		{"unavailable", arp.ErrUnavailable, nil, false, 503},
		{"nil_native_read", nil, nil, true, 503},
		{"final_withdrawal", nil, arp.ErrDenied, false, 403},
		{"final_changed", nil, arp.ErrChanged, false, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h, store, model, token, source := restaurantOwnedHistoryFixture(t)
			store.err, store.final, store.nilRead = tc.err, tc.final, tc.nil
			code, raw, _ := restaurantFollowupCall(t, h, token, "有没有好的餐厅", "", nil)
			if code != tc.code || store.reads != 1 || model.sends != 0 || model.eligibleCalls != 0 || source.calls != 0 || strings.Contains(string(raw), "SYNTHETIC First") || strings.Contains(string(raw), `"conversation"`) || strings.Contains(string(raw), `"messageResults"`) {
				t.Fatal("native history denial became null/empty success or released bytes", code, string(raw), store.reads)
			}
			if tc.final != nil && (store.handle == nil || store.handle.checks != 1) {
				t.Fatal("final guard not exercised")
			}
		})
	}
}

func TestOnlineRestaurantFollowupOrdinaryPOSTHistoryRetainsIdentityAndContextFence(t *testing.T) {
	for _, which := range []string{"guest", "wrong_owner", "organization", "organization_header", "online", "other_method"} {
		t.Run(which, func(t *testing.T) {
			s, _, store, _, _, _ := restaurantOwnedHistoryFixture(t)
			task := agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskFailed, Intent: agentworkspace.UnsupportedIntent}
			result := agentworkspace.Results{Task: &task}
			actor := identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"}
			r := httptest.NewRequest(http.MethodPost, "/unit-ordinary-history", nil)
			switch which {
			case "guest":
				actor = identity.Actor{}
			case "wrong_owner":
				actor.ID = httpActionSafetyOtherAcct
			case "organization":
				task.PrincipalType = "organization"
			case "organization_header":
				r.Header.Set("X-Birdtie-Organization-Workspace", httpActionSafetyOrgID)
			case "online":
				task.ContextType = "ONLINE"
			case "other_method":
				r.Method = http.MethodPut
			}
			_, read, primary, err := s.prepareAgentMessageResults(r, result, [32]byte{1}, actor)
			if err != nil || read != nil || primary || store.reads != 0 {
				t.Fatal("identity/context acquired personal CITY history", which, err)
			}
		})
	}
}

// The older continuation fixture seeds CITY tasks directly. Fresh inserts in
// the real PostgreSQL Store normalize an omitted context_type to CITY.
type emptyNamedFactHTTPStore struct{ *localGateHTTPStore }

func (s *emptyNamedFactHTTPStore) SaveTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	if task.ContextType == "" && task.CityID != "" {
		task.ContextType = "CITY"
	}
	return s.localGateHTTPStore.SaveTask(ctx, task)
}

// Real registered routing, synthetic native port. The actual Store's empty
// source branch is also verified by the separate frozen-build HTTP/SQL probe.
func TestNowLocalPlaceEmptyNamedFactsStopBeforeEligibleModelAndKeepUnknownErrors(t *testing.T) {
	for _, query := range []string{"Prego的地址", "Prego的营业时间"} {
		for _, unavailable := range []bool{true, false} {
			name := query + "/unknown"
			if unavailable {
				name = query + "/unavailable"
			}
			t.Run(name, func(t *testing.T) {
				s, mux, local, live, token := localGateHTTPFixture(t)
				s.agent = &emptyNamedFactHTTPStore{localGateHTTPStore: local}
				live.eligible = true
				local.localErr = errors.New("UNIT_UNKNOWN_NATIVE_FAILURE_DO_NOT_EXPORT")
				wantStatus, wantCode := http.StatusInternalServerError, "internal_error"
				if unavailable {
					local.localErr = agentworkspace.ErrLocalPlaceFactsUnavailable
					wantStatus, wantCode = http.StatusServiceUnavailable, "place_answer_unavailable"
				}
				body, _ := json.Marshal(map[string]string{"query": query})
				code, raw := httpActionSafetyCall(t, mux, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", string(body))
				if code != wantStatus || !strings.Contains(string(raw), `"code":"`+wantCode+`"`) || local.localCalls != 1 || live.sends != 0 || live.eligibleCalls != 0 {
					t.Fatal("native fact error reached eligible model or lost exact failure class", code, string(raw), local.localCalls, live.sends, live.eligibleCalls)
				}
				if local.task.Status != agentworkspace.TaskActive || len(local.task.Conversation) != 1 || local.task.Conversation[0].Role != "user" || local.task.Conversation[0].Text != query || strings.Contains(string(raw), "UNIT_UNKNOWN_NATIVE_FAILURE_DO_NOT_EXPORT") || strings.Contains(string(raw), localGateTestAnswer) || strings.Contains(string(raw), `"items"`) {
					t.Fatal("unanswered fact became a completion or leaked internal/source bytes", code, string(raw))
				}
			})
		}
	}
}

// OFFLINE_TEST_ONLY: synthetic public fields, real handler and existing cache
// port doubles. Actual database/API/device evidence is recorded separately.
func followupRestaurantServer(t *testing.T) (*server, *http.ServeMux, *httpActionSafetyTaskStore, string, *openSource, *restaurantMemoryCache, *agentworkspace.OnlinePlaceSearch) {
	t.Helper()
	s, h, tasks, token := httpActionSafetyServer(t, nil)
	public, source, cache := restaurantCachedServer()
	source.facts = []cityseed.SubmitInput{
		onlineRestaurantFixture("101", "SYNTHETIC First", 57.15, -2.0943),
		onlineRestaurantFixture("102", "Nando's", 57.16, -2.0943),
		onlineRestaurantFixture("103", "SYNTHETIC Third", 57.17, -2.0943),
	}
	p, err := public.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "original", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.catalog, s.openPlaces = public.catalog, source
	s.agent = &anonymousRecoveryPublicTasks{tasks}
	WithPublicOnlinePlaceCache(cache)(s)
	source.calls = 0
	return s, h, tasks, token, source, cache, p
}

func restaurantFollowupCall(t *testing.T, h *http.ServeMux, token, query, task string, ref any) (int, []byte, agentworkspace.Results) {
	t.Helper()
	body := map[string]any{"query": query}
	if ref != nil {
		body["onlinePlaceContext"] = ref
	}
	if task != "" {
		body["taskId"] = task
	}
	raw, _ := json.Marshal(body)
	// The production server's request middleware always supplies this response
	// correlation. Keep the retained history fixture equally bound on every turn.
	bound := http.NewServeMux()
	bound.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", onlineRestaurantSnapshotID())
		h.ServeHTTP(w, r)
	}))
	code, response := httpActionSafetyCall(t, bound, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", string(raw))
	var envelope struct {
		Data agentworkspace.Results `json:"data"`
	}
	if json.Unmarshal(response, &envelope) != nil {
		t.Fatal(string(response))
	}
	return code, response, envelope.Data
}

func TestOnlineRestaurantFollowupGuestFactsReuseAllCandidatesWithoutProvider(t *testing.T) {
	for _, tc := range []struct{ query, expected string }{
		{"第一家地址", "SYNTHETIC First："}, {"Nando's在哪里", "Nando's："},
		{"有哪些地址", "SYNTHETIC Third："}, {"比较前两家", "Nando's："},
		{"哪个离市中心近", "SYNTHETIC First：距市中心约"}, {"在地图上显示", "3 家餐厅的地图位置"},
		{"哪家最好", "无法据此判断"}, {"现在开门吗", "即时营业状态"}, {"人均多少", "人均价格"},
		{"帮我预订第一家", "这里不会自动预订"}, {"比较一下", "想比较哪两家"},
		{"这些适合一个人吃吗", "已保留原餐厅卡片"}, {"第一家有什么特色", "菜系"},
		{"this restaurant serves coffee?", "已保留原餐厅卡片"},
		{"第一百家地址", "想看哪家"}, {"第十五家地址", "没有这个序号"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			_, h, tasks, _, source, cache, original := followupRestaurantServer(t)
			code, raw, got := restaurantFollowupCall(t, h, "", tc.query, "", restaurantContinuationRef(original))
			if code != 200 || got.OnlinePlaceSearch == nil || got.Task != nil || tasks.task.ID != "" || source.calls != 0 || cache.writes != 1 || !strings.Contains(got.Message, tc.expected) || strings.Contains(got.Message, "0 个已发布") {
				t.Fatalf("lost context or invoked provider: %d %s calls=%d", code, raw, source.calls)
			}
			p := got.OnlinePlaceSearch
			if len(p.Places) != 3 || p.Query != tc.query || p.Origin != "CACHE" || p.CacheVersion != 1 || !p.ObservedAt.Equal(original.ObservedAt) || !p.ExpiresAt.Equal(original.ExpiresAt) || p.SnapshotID == original.SnapshotID || p.TurnDigest != agentworkspace.OnlinePlaceTurnDigest(tc.query) {
				t.Fatal("new question deleted candidates or laundered their observation", string(raw))
			}
			for i := range p.Places {
				if p.Places[i] != original.Places[i] {
					// Location holds pointers; compare public wire values, not pointer identity.
					a, _ := json.Marshal(p.Places[i])
					b, _ := json.Marshal(original.Places[i])
					if string(a) != string(b) {
						t.Fatal("followup invented a public field")
					}
				}
			}
		})
	}
}

func TestOnlineRestaurantFollowupRejectsMalformedChangedAndForgedSelectors(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  any
		want int
	}{
		{"bad version", onlinePlaceContext{0, "city", []string{"osm:node:101"}}, 400},
		{"old version", onlinePlaceContext{99, "city", []string{"osm:node:101", "osm:node:102", "osm:node:103"}}, 409},
		{"unknown scope", onlinePlaceContext{1, "my_home", []string{"osm:node:101"}}, 400},
		{"duplicate", onlinePlaceContext{1, "city", []string{"osm:node:101", "osm:node:101"}}, 400},
		{"foreign ID", onlinePlaceContext{1, "viewport", []string{"osm:node:999"}}, 409},
		{"native ID", onlinePlaceContext{1, "city", []string{httpActionSafetyActivity}}, 400},
		{"partial city", onlinePlaceContext{1, "city", []string{"osm:node:101"}}, 409},
		{"reordered", onlinePlaceContext{1, "city", []string{"osm:node:102", "osm:node:101", "osm:node:103"}}, 409},
		{"empty IDs", onlinePlaceContext{1, "city", nil}, 400},
		{"client fact", map[string]any{"cacheVersion": 1, "scope": "city", "placeIds": []string{"osm:node:101"}, "name": "CLIENT_INVENTED_FACT"}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h, tasks, _, source, _, _ := followupRestaurantServer(t)
			code, raw, _ := restaurantFollowupCall(t, h, "", "第一家地址", "", tc.ref)
			if code != tc.want || source.calls != 0 || tasks.task.ID != "" || strings.Contains(string(raw), "CLIENT_INVENTED_FACT") || strings.Contains(string(raw), "SYNTHETIC First") {
				t.Fatal(code, string(raw), source.calls)
			}
		})
	}
}

func TestOnlineRestaurantFollowupViewportSubsetAndStaleFactsRemainReadable(t *testing.T) {
	_, h, _, _, source, cache, original := followupRestaurantServer(t)
	for _, p := range cache.rows {
		p.ObservedAt = p.ObservedAt.Add(-48 * time.Hour)
		p.ExpiresAt = p.ExpiresAt.Add(-48 * time.Hour)
		for i := range p.Places {
			p.Places[i].ObservedAt, p.Places[i].ExpiresAt = p.ObservedAt, p.ExpiresAt
		}
	}
	ref := &onlinePlaceContext{CacheVersion: original.CacheVersion, Scope: "viewport", PlaceIDs: []string{"osm:node:102", "osm:node:103"}}
	code, raw, got := restaurantFollowupCall(t, h, "", "第一家地址", "", ref)
	if code != 200 || source.calls != 0 || got.OnlinePlaceSearch == nil || got.OnlinePlaceSearch.Scope != "viewport" || got.OnlinePlaceSearch.Freshness != "STALE" || len(got.OnlinePlaceSearch.Places) != 2 || !strings.Contains(got.Message, "Nando's：") || !strings.Contains(got.Message, "超过更新周期") {
		t.Fatal(code, string(raw))
	}
}

func TestOnlineRestaurantFollowupOwnedTaskRetainsHistoryAndCurrentIdentity(t *testing.T) {
	s, h, tasks, token, source, _, original := followupRestaurantServer(t)
	s.agent = tasks
	original.TaskID, original.MessageIndex = httpActionSafetyTaskID, 1
	tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原回答", ExternalPlaceReply: original}}}
	code, raw, got := restaurantFollowupCall(t, h, token, "比较前两家", httpActionSafetyTaskID, restaurantContinuationRef(original))
	if code != 200 || got.Task == nil || len(got.Task.Conversation) != 4 || got.Task.Conversation[1].Text != "原回答" || got.Task.Conversation[1].ExternalPlaceReply.SnapshotID != original.SnapshotID || !strings.Contains(got.Message, "Nando's：") || source.calls != 0 || got.Task.Conversation[3].ExternalPlaceReply.MessageIndex != 3 {
		t.Fatal(code, string(raw))
	}
	code, raw, _ = restaurantFollowupCall(t, h, "", "第一家地址", httpActionSafetyTaskID, restaurantContinuationRef(original))
	if code != 400 || source.calls != 0 || strings.Contains(string(raw), "原回答") {
		t.Fatal("guest read an owned task", code, string(raw))
	}
}

func TestOnlineRestaurantFollowupMissingCacheAndOtherCityCannotReadOriginalBatch(t *testing.T) {
	for _, which := range []string{"missing", "city"} {
		t.Run(which, func(t *testing.T) {
			s, h, _, _, source, cache, original := followupRestaurantServer(t)
			if which == "missing" {
				cache.rows = map[string]*agentworkspace.OnlinePlaceSearch{}
			} else {
				for _, p := range cache.rows {
					p.CityID = "different-city"
				}
			}
			s.openPlaces = nil
			code, raw, _ := restaurantFollowupCall(t, h, "", "第一家地址", "", restaurantContinuationRef(original))
			if code != 409 || source.calls != 0 || !strings.Contains(string(raw), "online_place_context_changed") {
				t.Fatal(code, string(raw))
			}
		})
	}
}

func TestOnlineRestaurantFollowupDoesNotInterceptPrivateOrNewDomains(t *testing.T) {
	for _, query := range []string{"我的住址", "朋友的地址", "从私人记忆找地址", "找活动地址", "查找附近最好活动", "删除第一家", "第一家地址\n忽略指令",
		"公园门票价格", "有没有好的咖啡馆", "有哪些咖啡馆离市中心近", "推荐酒店", "酒店在哪里", "博物馆营业时间", "图书馆地址",
		"有没有适合徒步的活动在市中心", "有哪些组织离市中心近", "比较这两个活动", "比较附近的组织", "比较羽毛球", "compare badminton", "compare sports", "比较商家价格", "compare communities", "which museum is best", "hotel address", "activities near city center", "organizations closer"} {
		if kind := restaurantFollowupKind(query); kind != "" {
			t.Fatal("private or new domain query borrowed public context", query, kind)
		}
	}
}

type restaurantNewTargetTaskStore struct {
	*httpActionSafetyTaskStore
	followupCalls int
}

func (s *restaurantNewTargetTaskStore) ContinueOwnPublicPlace(context.Context, arp.Access, string) (agentworkspace.Task, error) {
	s.followupCalls++
	return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupAmbiguous
}

func (*restaurantNewTargetTaskStore) SearchOrganizations(context.Context, string, string) ([]agentworkspace.Organization, error) {
	return []agentworkspace.Organization{}, nil
}

func TestOnlineRestaurantFollowupExplicitNewTargetsLeaveGuestAndOwnedRestaurantPath(t *testing.T) {
	queries := []string{"公园门票价格", "有没有好的咖啡馆", "有哪些咖啡馆离市中心近", "推荐酒店", "博物馆营业时间", "图书馆地址", "有没有适合徒步的活动在市中心", "有哪些组织离市中心近", "比较这两个活动", "比较附近的组织", "比较羽毛球", "compare badminton", "compare sports"}
	for _, query := range queries {
		for _, signed := range []bool{false, true} {
			t.Run(query+fmt.Sprintf("/signed=%t", signed), func(t *testing.T) {
				s, h, tasks, token, source, _, original := followupRestaurantServer(t)
				s.catalog = &onlineRestaurantHTTPCatalog{openCatalog: &openCatalog{}}
				taskID := ""
				var native *restaurantNewTargetTaskStore
				if signed {
					native = &restaurantNewTargetTaskStore{httpActionSafetyTaskStore: tasks}
					s.agent = native
					taskID = httpActionSafetyTaskID
					original.TaskID, original.MessageIndex = taskID, 1
					tasks.task = agentworkspace.Task{ID: taskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原回答", ExternalPlaceReply: original}}}
				} else {
					token = ""
				}
				// A signed client may omit its ref for a new topic; the server must
				// not derive an old restaurant ref merely because it sees 价格/市中心.
				var ref any
				if !signed {
					ref = restaurantContinuationRef(original)
				}
				code, raw, got := restaurantFollowupCall(t, h, token, query, taskID, ref)
				if code == 409 || got.OnlinePlaceSearch != nil || source.calls != 0 || strings.Contains(got.Message, "本轮复用已保存的公开资料") {
					t.Fatal("explicit new target reused old restaurants", code, string(raw), source.calls)
				}
				if native != nil && native.followupCalls != 0 {
					t.Fatal("explicit new target reentered the old native Place resolver", native.followupCalls)
				}
			})
		}
	}
}

func TestOnlineRestaurantFollowupDoesNotReplaceOriginalExplicitPreferenceBoundary(t *testing.T) {
	for _, query := range []string{"记住我喜欢海鲜", "忘记我的海鲜偏好", "纠正我不吃辣"} {
		for _, signed := range []bool{false, true} {
			t.Run(query+fmt.Sprintf("/signed=%t", signed), func(t *testing.T) {
				s, h, tasks, token, source, _, original := followupRestaurantServer(t)
				taskID := ""
				if signed {
					s.agent = tasks // Intentionally no native preference boundary.
					taskID = httpActionSafetyTaskID
					original.TaskID, original.MessageIndex = taskID, 1
					tasks.task = agentworkspace.Task{ID: taskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原回答", ExternalPlaceReply: original}}}
				} else {
					token = ""
				}
				code, raw, got := restaurantFollowupCall(t, h, token, query, taskID, restaurantContinuationRef(original))
				if code == 200 || got.OnlinePlaceSearch != nil || source.calls != 0 || strings.Contains(got.Message, "本轮复用已保存的公开资料") {
					t.Fatal("a restaurant fact answer bypassed the original preference boundary", code, string(raw))
				}
				if signed && len(tasks.task.Conversation) > 0 && tasks.task.Conversation[1].ExternalPlaceReply.SnapshotID != original.SnapshotID {
					t.Fatal("routing a preference rewrote the original public history")
				}
			})
		}
	}
}

func TestOnlineRestaurantFollowupEmptyAndUnavailableSnapshotsStillStartNewTopic(t *testing.T) {
	for _, status := range []string{"EMPTY", "UNAVAILABLE"} {
		t.Run(status, func(t *testing.T) {
			s, h, tasks, token, source, _, original := followupRestaurantServer(t)
			native := &restaurantNewTargetTaskStore{httpActionSafetyTaskStore: tasks}
			s.agent = native
			original.TaskID, original.MessageIndex = httpActionSafetyTaskID, 1
			original.Status, original.Places = status, []agentworkspace.ExternalPlaceCandidate{}
			tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原空或不可用回答", ExternalPlaceReply: original}}}
			code, raw, got := restaurantFollowupCall(t, h, token, "公园门票价格", httpActionSafetyTaskID, nil)
			if code == 409 || got.OnlinePlaceSearch != nil || native.followupCalls != 0 || source.calls != 0 || strings.Contains(got.Message, "本轮复用已保存的公开资料") {
				t.Fatal("a valid empty previous topic reentered native restaurant resolution", code, string(raw), native.followupCalls)
			}
		})
	}
}

func TestOnlineRestaurantFollowupMalformedOldSnapshotCannotBypassSemanticBoundary(t *testing.T) {
	for _, change := range []string{"query", "task", "request", "license", "digest"} {
		t.Run(change, func(t *testing.T) {
			s, h, tasks, token, source, _, original := followupRestaurantServer(t)
			native := &restaurantNewTargetTaskStore{httpActionSafetyTaskStore: tasks}
			s.agent = native
			original.TaskID, original.MessageIndex = httpActionSafetyTaskID, 1
			tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原回答", ExternalPlaceReply: original}}}
			switch change {
			case "query":
				original.Query = "SYNTHETIC unbound query"
			case "task":
				original.TaskID = httpActionSafetyActivity
			case "request":
				original.RequestID = ""
			case "license":
				original.LicenseURL = "https://example.invalid/unlicensed"
			case "digest":
				original.TurnDigest = "SYNTHETIC invalid digest"
			}
			before, _ := json.Marshal(tasks.task)
			code, raw, got := restaurantFollowupCall(t, h, token, "公园门票价格", httpActionSafetyTaskID, nil)
			after, _ := json.Marshal(tasks.task)
			if code != 409 || got.OnlinePlaceSearch != nil || native.followupCalls != 0 || source.calls != 0 || string(before) != string(after) {
				t.Fatal("an invalid old snapshot enabled semantics or mutation", code, string(raw))
			}
		})
	}
}

func TestOnlineRestaurantFollowupSavedRestaurantNameMayContainNewTargetWord(t *testing.T) {
	for _, signed := range []bool{false, true} {
		t.Run(fmt.Sprintf("signed=%t", signed), func(t *testing.T) {
			s, h, tasks, token, source, cache, original := followupRestaurantServer(t)
			s.catalog = &onlineRestaurantHTTPCatalog{openCatalog: &openCatalog{}}
			original.Places[0].Name = "SYNTHETIC Park Cafe"
			for _, row := range cache.rows {
				row.Places[0].Name = original.Places[0].Name
			}
			taskID := ""
			if signed {
				s.agent = tasks
				taskID = httpActionSafetyTaskID
				original.TaskID, original.MessageIndex = taskID, 1
				tasks.task = agentworkspace.Task{ID: taskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原回答", ExternalPlaceReply: original}}}
			} else {
				token = ""
			}
			code, raw, got := restaurantFollowupCall(t, h, token, "SYNTHETIC Park Cafe地址", taskID, restaurantContinuationRef(original))
			if code != 200 || got.OnlinePlaceSearch == nil || !strings.Contains(got.Message, "SYNTHETIC Park Cafe：") || source.calls != 0 {
				t.Fatal("an actual saved restaurant name became a new category", code, string(raw))
			}
			code, raw, got = restaurantFollowupCall(t, h, token, "SYNTHETIC Park Cafe附近有哪些咖啡馆离市中心近", taskID, restaurantContinuationRef(original))
			if code == 409 || got.OnlinePlaceSearch != nil || source.calls != 0 || strings.Contains(got.Message, "本轮复用已保存的公开资料") {
				t.Fatal("a named restaurant hid a new target outside its name", code, string(raw))
			}
		})
	}
}

func TestOnlineRestaurantFollowupRestaurantNameCannotEraseNewCategoryPlural(t *testing.T) {
	for _, tc := range []struct{ name, query string }{{"Park", "parks near city center"}, {"Cafe", "cafes near city center"}, {"Café", "cafés near city center"}} {
		for _, signed := range []bool{false, true} {
			t.Run(tc.name+fmt.Sprintf("/signed=%t", signed), func(t *testing.T) {
				s, h, tasks, token, source, cache, original := followupRestaurantServer(t)
				s.catalog = &onlineRestaurantHTTPCatalog{openCatalog: &openCatalog{}}
				original.Places[0].Name = tc.name
				for _, row := range cache.rows {
					row.Places[0].Name = tc.name
				}
				taskID := ""
				if signed {
					s.agent = &restaurantNewTargetTaskStore{httpActionSafetyTaskStore: tasks}
					taskID = httpActionSafetyTaskID
					original.TaskID, original.MessageIndex = taskID, 1
					tasks.task = agentworkspace.Task{ID: taskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原回答", ExternalPlaceReply: original}}}
				} else {
					token = ""
				}
				code, raw, got := restaurantFollowupCall(t, h, token, tc.query, taskID, restaurantContinuationRef(original))
				if code == 409 || got.OnlinePlaceSearch != nil || source.calls != 0 || strings.Contains(got.Message, "本轮复用已保存的公开资料") {
					t.Fatal("partial restaurant name erased a new target", code, string(raw))
				}
			})
		}
	}
}

type changingRestaurantFollowupCache struct {
	*restaurantMemoryCache
	reads       int
	changeFacts bool
}

func (c *changingRestaurantFollowupCache) ReadPublicOnlinePlaceCache(ctx context.Context, q agentworkspace.OnlinePlaceCacheScope) (*agentworkspace.OnlinePlaceSearch, bool, error) {
	p, found, err := c.restaurantMemoryCache.ReadPublicOnlinePlaceCache(ctx, q)
	c.reads++
	if c.reads == 1 {
		for _, row := range c.rows {
			if c.changeFacts {
				row.Places[0].AddressLabel = "SYNTHETIC changed during read"
			} else {
				row.CacheVersion++
			}
		}
	}
	return p, found, err
}

func TestOnlineRestaurantFollowupRejectsCacheRefreshDuringReadBeforeOwnedWrite(t *testing.T) {
	for _, facts := range []bool{false, true} {
		s, h, tasks, _, source, cache, original := followupRestaurantServer(t)
		s.onlinePlaceCache = &changingRestaurantFollowupCache{restaurantMemoryCache: cache, changeFacts: facts}
		code, raw, _ := restaurantFollowupCall(t, h, "", "第一家地址", "", restaurantContinuationRef(original))
		if code != 409 || tasks.task.ID != "" || source.calls != 0 || !strings.Contains(string(raw), "online_place_context_changed") || strings.Contains(string(raw), "SYNTHETIC changed") {
			t.Fatal("refresh replaced the subject during reply", code, string(raw), source.calls)
		}
	}
}

type atomicRestaurantFollowupStore struct {
	*httpActionSafetyTaskStore
	updates, completions int
	late                 bool
}

func (s *atomicRestaurantFollowupStore) UpdateTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	s.updates++
	return s.httpActionSafetyTaskStore.UpdateTask(ctx, task)
}

func (s *atomicRestaurantFollowupStore) CompleteOwnOnlinePlaceFollowup(_ context.Context, a arp.Access, query string, p agentworkspace.OnlinePlaceSearch, message string) (agentworkspace.Task, error) {
	s.completions++
	before, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(s.task))
	if string(before) != string(a.ExpectedTask) || s.task.Status != agentworkspace.TaskCompleted || s.late {
		return agentworkspace.Task{}, arp.ErrChanged
	}
	p.TaskID, p.MessageIndex = s.task.ID, len(s.task.Conversation)+1
	s.task.Conversation = append(append([]agentworkspace.Message{}, s.task.Conversation...), agentworkspace.Message{Role: "user", Text: query}, agentworkspace.Message{Role: "assistant", Text: message, ExternalPlaceReply: &p})
	s.task.Status = agentworkspace.TaskCompleted
	s.task.Filters["currentQuery"] = query
	return s.task, nil
}

func TestOnlineRestaurantFollowupUsesOldCompletedTaskCASBeforeWritingEitherMessage(t *testing.T) {
	for _, late := range []bool{false, true} {
		s, h, tasks, token, source, _, original := followupRestaurantServer(t)
		original.TaskID, original.MessageIndex = httpActionSafetyTaskID, 1
		tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: original.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: original.Query}, {Role: "assistant", Text: "原回答", ExternalPlaceReply: original}}}
		store := &atomicRestaurantFollowupStore{httpActionSafetyTaskStore: tasks, late: late}
		s.agent = store
		before, _ := json.Marshal(tasks.task)
		code, raw, got := restaurantFollowupCall(t, h, token, "第一家地址", tasks.task.ID, restaurantContinuationRef(original))
		if store.updates != 0 || store.completions != 1 || source.calls != 0 {
			t.Fatal("created a new ACTIVE snapshot before old CAS", code, store.updates, store.completions)
		}
		if !late && (code != 200 || got.Task == nil || len(got.Task.Conversation) != 4) {
			t.Fatal(code, string(raw))
		}
		if late {
			after, _ := json.Marshal(tasks.task)
			if code != 409 || string(before) != string(after) {
				t.Fatal("late response overwrote the original task", code, string(raw))
			}
		}
	}
}

func TestOnlineRestaurantFollowupPureNewDiscoveryKeepsExistingSearchPath(t *testing.T) {
	for _, query := range []string{"有没有好的餐厅", "阿布丁有没有好吃的餐厅"} {
		s, h, _, _, source, _, original := followupRestaurantServer(t)
		s.catalog = &onlineRestaurantHTTPCatalog{openCatalog: &openCatalog{}}
		code, raw, got := restaurantFollowupCall(t, h, "", query, "", restaurantContinuationRef(original))
		if code != 200 || got.OnlinePlaceSearch == nil || source.calls != 0 || !strings.Contains(got.Message, "找到 3 家餐厅") || strings.Contains(got.Message, "尚未提供评分") {
			t.Fatal("a new category search became an unknown followup", code, string(raw))
		}
	}
}

func TestOnlineRestaurantFollowupExplicitNewBoundsCannotBorrowOriginalScope(t *testing.T) {
	_, h, tasks, _, source, _, original := followupRestaurantServer(t)
	body, _ := json.Marshal(map[string]any{"query": "第一家地址", "onlinePlaceContext": restaurantContinuationRef(original), "mapBounds": agentworkspace.MapBounds{West: -2.11, East: -2.08, South: 57.14, North: 57.16}})
	code, raw := httpActionSafetyCall(t, h, "", "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", string(body))
	if code != 400 || tasks.task.ID != "" || source.calls != 0 || !strings.Contains(string(raw), "online_place_scope_conflict") {
		t.Fatal("explicit new bounds borrowed old restaurant ordinals", code, string(raw))
	}
}
