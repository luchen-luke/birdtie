package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// OFFLINE_TEST_ONLY. These doubles exercise cost and immutable-history
// behavior; live SQL/source/device evidence is recorded separately.
type restaurantMemoryCache struct {
	rows   map[string]*agentworkspace.OnlinePlaceSearch
	writes int
}

func copyRestaurantSearch(p *agentworkspace.OnlinePlaceSearch) *agentworkspace.OnlinePlaceSearch {
	raw, _ := json.Marshal(p)
	var out agentworkspace.OnlinePlaceSearch
	_ = json.Unmarshal(raw, &out)
	return &out
}
func (c *restaurantMemoryCache) ReadPublicOnlinePlaceCache(_ context.Context, q agentworkspace.OnlinePlaceCacheScope) (*agentworkspace.OnlinePlaceSearch, bool, error) {
	p, ok := c.rows[q.CacheKey()]
	if !ok {
		return nil, false, nil
	}
	return copyRestaurantSearch(p), true, nil
}
func (c *restaurantMemoryCache) StorePublicOnlinePlaceCache(_ context.Context, q agentworkspace.OnlinePlaceCacheScope, p *agentworkspace.OnlinePlaceSearch) (*agentworkspace.OnlinePlaceSearch, error) {
	copy := copyRestaurantSearch(p)
	copy.Query = ""
	copy.RequestID = ""
	copy.TaskID = ""
	copy.SnapshotID = ""
	copy.TurnDigest = ""
	copy.MessageIndex = -1
	copy.Origin = ""
	copy.Freshness = ""
	copy.RefreshFailed = false
	c.writes++
	copy.CacheVersion = int64(c.writes)
	c.rows[q.CacheKey()] = copy
	return copyRestaurantSearch(copy), nil
}
func restaurantCachedServer() (*server, *openSource, *restaurantMemoryCache) {
	s, _, _, source := onlineRestaurantServer()
	cache := &restaurantMemoryCache{rows: map[string]*agentworkspace.OnlinePlaceSearch{}}
	WithPublicOnlinePlaceCache(cache)(s)
	return s, source, cache
}

func TestOnlineRestaurantCacheReuseStaleAndExplicitRefresh(t *testing.T) {
	s, source, cache := restaurantCachedServer()
	ctx := context.Background()
	first, e := s.discoverOnlineRestaurants(ctx, "aberdeen-gb", "有没有好的餐厅", "first", nil)
	if e != nil || first.Origin != "LIVE" || first.CacheVersion != 1 {
		t.Fatal(first, e)
	}
	second, e := s.discoverOnlineRestaurants(ctx, "aberdeen-gb", "阿布丁有没有好的餐厅", "second", nil)
	if e != nil || source.calls != 1 || cache.writes != 1 || second.Origin != "CACHE" || !second.ObservedAt.Equal(first.ObservedAt) || second.SnapshotID == first.SnapshotID || second.Query == first.Query || second.RequestID != "second" {
		t.Fatal("repeat query paid for provider again or borrowed another round", second, e, source.calls)
	}
	for _, p := range cache.rows {
		if p.Query != "" || p.RequestID != "" || p.TaskID != "" || p.SnapshotID != "" || p.TurnDigest != "" {
			t.Fatal("private turn entered public cache", p)
		}
		p.ObservedAt = p.ObservedAt.Add(-48 * time.Hour)
		p.ExpiresAt = p.ExpiresAt.Add(-48 * time.Hour)
		for i := range p.Places {
			p.Places[i].ObservedAt = p.ObservedAt
			p.Places[i].ExpiresAt = p.ExpiresAt
		}
	}
	stale, e := s.discoverOnlineRestaurants(ctx, "aberdeen-gb", "有没有好的餐厅", "stale", nil)
	if e != nil || source.calls != 1 || stale.Freshness != "STALE" || len(stale.Places) != 1 || stale.ExpiresAt.After(time.Now()) {
		t.Fatal("stale saved content disappeared or refreshed silently", stale, e)
	}
	source.err = errors.New("upstream unavailable")
	fallback, e := s.discoverOnlineRestaurants(ctx, "aberdeen-gb", "更新餐厅信息", "refresh-failed", nil, true)
	if e != nil || source.calls != 2 || !fallback.RefreshFailed || fallback.Freshness != "STALE" || len(fallback.Places) != 1 || !fallback.ObservedAt.Equal(stale.ObservedAt) || cache.writes != 1 {
		t.Fatal("refresh failure deleted or laundered original cache", fallback, e)
	}
	source.err = nil
	source.facts = []cityseed.SubmitInput{onlineRestaurantFixture("102", "Synthetic refreshed restaurant", 57.15, -2.0943)}
	fresh, e := s.discoverOnlineRestaurants(ctx, "aberdeen-gb", "更新餐厅信息", "refresh", nil, true)
	if e != nil || source.calls != 3 || cache.writes != 2 || fresh.CacheVersion != 2 || fresh.Places[0].ID != "osm:node:102" || fresh.Freshness != "FRESH" || first.Places[0].ID != "osm:node:101" {
		t.Fatal("explicit refresh failed or mutated old snapshot", fresh, e)
	}
}

func TestOnlineRestaurantViewportCachesCompleteCityAndEmptyReuse(t *testing.T) {
	s, source, _ := restaurantCachedServer()
	source.facts = append(source.facts, onlineRestaurantFixture("102", "Synthetic outside viewport", 57.18, -2.0943))
	viewport := &agentworkspace.MapBounds{West: -2.11, East: -2.08, South: 57.14, North: 57.16}
	a, e := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "附近有没有好的餐厅", "viewport", viewport)
	b, e2 := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "city", nil)
	if e != nil || e2 != nil || len(a.Places) != 1 || len(b.Places) != 2 || source.calls != 1 {
		t.Fatal("phone viewport poisoned reusable city cache", a, b, e, e2)
	}
	s, source, _ = restaurantCachedServer()
	source.facts = nil
	for i := 0; i < 2; i++ {
		got, e := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "empty", nil)
		if e != nil || got.Status != "EMPTY" {
			t.Fatal(got, e)
		}
	}
	if source.calls != 1 {
		t.Fatal("empty results repeatedly paid network cost", source.calls)
	}
}

func TestOnlineRestaurantOwnedGETRetainsAllRoundsWithoutNetwork(t *testing.T) {
	s, h, tasks, token := httpActionSafetyServer(t, nil)
	public, source, _ := restaurantCachedServer()
	s.catalog = public.catalog
	s.openPlaces = source
	q1, q2 := "有没有好的餐厅", "阿布丁有没有好的餐厅"
	a, e := public.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", q1, "request-original-1", nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := public.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", q2, "request-original-2", nil)
	if e != nil {
		t.Fatal(e)
	}
	a.TaskID, b.TaskID = httpActionSafetyTaskID, httpActionSafetyTaskID
	a.MessageIndex, b.MessageIndex = 1, 3
	a.ObservedAt = a.ObservedAt.Add(-48 * time.Hour)
	a.ExpiresAt = a.ExpiresAt.Add(-48 * time.Hour)
	for i := range a.Places {
		a.Places[i].ObservedAt = a.ObservedAt
		a.Places[i].ExpiresAt = a.ExpiresAt
	}
	tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: q1, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery, "currentQuery": q2, "category": "restaurant"}, Conversation: []agentworkspace.Message{{Role: "user", Text: q1}, {Role: "assistant", Text: "第一轮原回答", ExternalPlaceReply: a}, {Role: "user", Text: q2}, {Role: "assistant", Text: "第二轮原回答", ExternalPlaceReply: b}}}
	original, _ := json.Marshal(tasks.task)
	code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	var wire struct {
		Data agentworkspace.Results `json:"data"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		t.Fatal(string(raw))
	}
	got := wire.Data
	after, _ := json.Marshal(tasks.task)
	if code != 200 || source.calls != 1 || got.OnlinePlaceSearch == nil || got.OnlinePlaceSearch.Origin != "HISTORY" || got.OnlinePlaceSearch.RequestID != "request-original-2" || got.Message != "第二轮原回答" || len(got.Task.Conversation) != 4 || len(got.Task.Conversation[1].ExternalPlaceReply.Places) != 1 || string(original) != string(after) {
		t.Fatal("history was reacquired, mutated, or dropped", code, string(raw))
	}
	tasks.task.Conversation[1].ExternalPlaceReply.TurnDigest = "wrong-round"
	code, raw = httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	if code != 409 || strings.Contains(string(raw), "第一轮原回答") {
		t.Fatal("misbound snapshot escaped owned history validation", code, string(raw))
	}
}

type restaurantCASCompletionStore struct {
	*httpActionSafetyTaskStore
	lateWrites int
}

func (s *restaurantCASCompletionStore) UpdateTask(ctx context.Context, t agentworkspace.Task) (agentworkspace.Task, error) {
	s.lateWrites++
	return s.httpActionSafetyTaskStore.UpdateTask(ctx, t)
}
func (s *restaurantCASCompletionStore) CompleteOwnOnlinePlaceReply(_ context.Context, a arp.Access, p agentworkspace.OnlinePlaceSearch, m string) (agentworkspace.Task, error) {
	current, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(s.task))
	if string(current) != string(a.ExpectedTask) {
		return agentworkspace.Task{}, arp.ErrChanged
	}
	return s.task, nil
}
func TestOnlineRestaurantLateReplyDoesNotOverwriteNewerTurn(t *testing.T) {
	s, _, tasks, _ := httpActionSafetyServer(t, nil)
	before := agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskActive, Intent: agentworkspace.FindPlace, Query: "有没有好的餐厅", Filters: map[string]string{"currentQuery": "有没有好的餐厅"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "有没有好的餐厅"}}}
	tasks.task = before
	// A later turn completes while this request waits for the provider.
	tasks.task.Conversation = []agentworkspace.Message{{Role: "user", Text: "有没有好的餐厅"}, {Role: "assistant", Text: "新一轮已经保存"}}
	tasks.task.Status = agentworkspace.TaskCompleted
	store := &restaurantCASCompletionStore{httpActionSafetyTaskStore: tasks}
	s.agent = store
	_, e := s.finishOnlineRestaurantQuery(httptest.NewRequest(http.MethodPost, "/", nil), [32]byte{1}, before, &agentworkspace.OnlinePlaceSearch{}, "迟到的旧回答")
	if !errors.Is(e, arp.ErrChanged) || store.lateWrites != 0 || tasks.task.Conversation[1].Text != "新一轮已经保存" {
		t.Fatal("late response reverted newer conversation before CAS", e, store.lateWrites, tasks.task)
	}
}

type restaurantFixedCatalog struct {
	foundation.PublicCatalog
	city foundation.City
}

func (c restaurantFixedCatalog) GetCity(context.Context, string) (foundation.City, error) {
	return c.city, nil
}
func TestOnlineRestaurantColdConcurrentQueriesSharePublicCache(t *testing.T) {
	s, source, cache := restaurantCachedServer()
	city, e := s.catalog.GetCity(context.Background(), "aberdeen-gb")
	if e != nil {
		t.Fatal(e)
	}
	s.catalog = restaurantFixedCatalog{city: city}
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []*agentworkspace.OnlinePlaceSearch{}
	errs := []error{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "concurrent", nil)
			mu.Lock()
			defer mu.Unlock()
			results = append(results, got)
			errs = append(errs, e)
		}()
	}
	wg.Wait()
	ids := map[string]bool{}
	for i, p := range results {
		if errs[i] != nil || p == nil || p.Status != "SUCCESS" || ids[p.SnapshotID] {
			t.Fatal("coalesced results borrowed another turn", p, errs[i])
		}
		ids[p.SnapshotID] = true
	}
	if source.calls != 1 || cache.writes != 1 {
		t.Fatal("cold concurrent requests repeated network cost", source.calls, cache.writes)
	}
}

func TestOnlineRestaurantHistoricalCityCenterChangeAndMissingMapKeepSnapshot(t *testing.T) {
	s, h, tasks, token := httpActionSafetyServer(t, nil)
	public, _, _ := restaurantCachedServer()
	p, e := public.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "original-request", nil)
	if e != nil {
		t.Fatal(e)
	}
	p.TaskID, p.MessageIndex = httpActionSafetyTaskID, 1
	tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: p.Query, Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{{Role: "user", Text: p.Query}, {Role: "assistant", Text: "原来的餐厅资料", ExternalPlaceReply: p}}}
	city, e := public.catalog.GetCity(context.Background(), "aberdeen-gb")
	if e != nil {
		t.Fatal(e)
	}
	city.Map.Latitude = 50
	s.catalog = restaurantFixedCatalog{city: city}
	code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	if code != 200 || !strings.Contains(string(raw), "Synthetic restaurant A") {
		t.Fatal("later catalog center deleted legal history", code, string(raw))
	}
	p.Status = "UNAVAILABLE"
	p.Places = []agentworkspace.ExternalPlaceCandidate{}
	city.Map = nil
	s.catalog = restaurantFixedCatalog{city: city}
	code, raw = httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	if code != 200 || !strings.Contains(string(raw), "原来的餐厅资料") {
		t.Fatal("missing city map deleted saved failure conversation", code, string(raw))
	}
}

type restaurantFailingConcurrentSource struct{ calls atomic.Int32 }

func (s *restaurantFailingConcurrentSource) Search(ctx context.Context, q agenttool.OpenPlaceQuery) ([]cityseed.SubmitInput, error) {
	s.calls.Add(1)
	// A real provider read is in flight while simultaneous queries arrive.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(150 * time.Millisecond):
		return nil, errors.New("offline provider failure")
	}
}
func TestOnlineRestaurantColdConcurrentFailureUsesOneReadThenAllowsRetry(t *testing.T) {
	s, _, cache := restaurantCachedServer()
	city, e := s.catalog.GetCity(context.Background(), "aberdeen-gb")
	if e != nil {
		t.Fatal(e)
	}
	s.catalog = restaurantFixedCatalog{city: city}
	source := &restaurantFailingConcurrentSource{}
	s.openPlaces = source
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []*agentworkspace.OnlinePlaceSearch{}
	errs := []error{}
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, e := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "failed-flight", nil)
			mu.Lock()
			defer mu.Unlock()
			results = append(results, p)
			errs = append(errs, e)
		}()
	}
	close(start)
	wg.Wait()
	ids := map[string]bool{}
	for i, p := range results {
		if errs[i] != nil || p == nil || p.Status != "UNAVAILABLE" || len(p.Places) != 0 || ids[p.SnapshotID] {
			t.Fatal("failure invented cached empty facts or shared private snapshot", p, errs[i])
		}
		ids[p.SnapshotID] = true
	}
	if source.calls.Load() != 1 || cache.writes != 0 {
		t.Fatal("simultaneous failed reads repeated cost or permanently cached failure", source.calls.Load(), cache.writes)
	}
	p, e := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "later-retry", nil)
	if e != nil || p.Status != "UNAVAILABLE" || source.calls.Load() != 2 {
		t.Fatal("subsequent explicit query could not retry source", p, e, source.calls.Load())
	}
}
