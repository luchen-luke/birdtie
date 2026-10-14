package httpapi

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

// OFFLINE_TEST_ONLY. The actual discovery/cache/singleflight composition uses
// synthetic public facts; these tests make no provider, database or device calls.
const restaurantCancellationWait = 5 * time.Second

type restaurantCancellationPrivateKey struct{}

// With the fixed catalog used below, Done is first evaluated by the caller's
// select after DoChan has returned. This signal therefore proves the follower
// joined the existing flight before either caller is canceled, without sleeps.
type restaurantCancellationWaitContext struct {
	context.Context
	reached chan struct{}
	once    sync.Once
}

func (c *restaurantCancellationWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.reached) })
	return c.Context.Done()
}

type restaurantCancellationOutcome struct {
	search *agentworkspace.OnlinePlaceSearch
	err    error
}

func waitRestaurantCancellation[T any](t *testing.T, ch <-chan T, stage string) T {
	t.Helper()
	timer := time.NewTimer(restaurantCancellationWait)
	defer timer.Stop()
	select {
	case value := <-ch:
		return value
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", stage)
		var zero T
		return zero
	}
}

type restaurantCancellationSource struct {
	calls   atomic.Int32
	started chan context.Context
	release chan struct{}
	once    sync.Once
}

func (s *restaurantCancellationSource) unblock() {
	s.once.Do(func() { close(s.release) })
}

func (s *restaurantCancellationSource) Search(ctx context.Context, _ agenttool.OpenPlaceQuery) ([]cityseed.SubmitInput, error) {
	s.calls.Add(1)
	s.started <- ctx
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return []cityseed.SubmitInput{onlineRestaurantFixture("101", "Synthetic retained restaurant", 57.15, -2.0943)}, nil
	}
}

type restaurantCancellationCache struct {
	mu       sync.Mutex
	memory   restaurantMemoryCache
	contexts []context.Context
	inputs   []*agentworkspace.OnlinePlaceSearch
}

func (c *restaurantCancellationCache) ReadPublicOnlinePlaceCache(ctx context.Context, scope agentworkspace.OnlinePlaceCacheScope) (*agentworkspace.OnlinePlaceSearch, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.contexts = append(c.contexts, ctx)
	return c.memory.ReadPublicOnlinePlaceCache(ctx, scope)
}

func (c *restaurantCancellationCache) StorePublicOnlinePlaceCache(ctx context.Context, scope agentworkspace.OnlinePlaceCacheScope, search *agentworkspace.OnlinePlaceSearch) (*agentworkspace.OnlinePlaceSearch, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.contexts = append(c.contexts, ctx)
	// Inspect the input before the existing cache double strips private fields.
	c.inputs = append(c.inputs, copyRestaurantSearch(search))
	return c.memory.StorePublicOnlinePlaceCache(ctx, scope, search)
}

func restaurantCancellationServer(t *testing.T) (*server, *restaurantCancellationSource, *restaurantCancellationCache) {
	t.Helper()
	s, _, _, _ := onlineRestaurantServer()
	city, err := s.catalog.GetCity(context.Background(), "aberdeen-gb")
	if err != nil {
		t.Fatal(err)
	}
	s.catalog = restaurantFixedCatalog{city: city}
	source := &restaurantCancellationSource{started: make(chan context.Context, 2), release: make(chan struct{})}
	cache := &restaurantCancellationCache{memory: restaurantMemoryCache{rows: map[string]*agentworkspace.OnlinePlaceSearch{}}}
	s.openPlaces = source
	WithPublicOnlinePlaceCache(cache)(s)
	t.Cleanup(source.unblock)
	return s, source, cache
}

func startRestaurantCancellationQuery(s *server, ctx context.Context, query, requestID string) <-chan restaurantCancellationOutcome {
	result := make(chan restaurantCancellationOutcome, 1)
	go func() {
		search, err := s.discoverOnlineRestaurants(ctx, "aberdeen-gb", query, requestID, nil)
		result <- restaurantCancellationOutcome{search: search, err: err}
	}()
	return result
}

func assertRestaurantCancellationBoundedPublicContext(t *testing.T, ctx context.Context, stage string) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if remaining := time.Until(deadline); !ok || remaining <= 0 || remaining > 15*time.Second {
		t.Errorf("%s public operation has no usable finite deadline within 15 seconds: deadline=%v present=%t", stage, deadline, ok)
	}
	if ctx.Value(restaurantCancellationPrivateKey{}) != nil {
		t.Errorf("%s public operation inherited private caller context values", stage)
	}
}

func assertRestaurantCancellationPublicCache(t *testing.T, cache *restaurantCancellationCache, bounded bool) {
	t.Helper()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.memory.writes != 1 || len(cache.inputs) != 1 || len(cache.memory.rows) != 1 {
		t.Fatalf("shared read did not create exactly one reusable public batch: writes=%d inputs=%d rows=%d", cache.memory.writes, len(cache.inputs), len(cache.memory.rows))
	}
	check := func(search *agentworkspace.OnlinePlaceSearch, stage string) {
		if search.Query != "" || search.RequestID != "" || search.TaskID != "" || search.SnapshotID != "" || search.TurnDigest != "" || search.MessageIndex > 0 {
			t.Errorf("%s contained private turn identity: %#v", stage, search)
		}
		if search.CityID != "aberdeen-gb" || search.Scope != "city" || search.Provider != "openstreetmap" || search.Status != "SUCCESS" || len(search.Places) != 1 {
			t.Errorf("%s did not retain the validated full public city batch: %#v", stage, search)
		}
	}
	check(cache.inputs[0], "public cache input before sanitization")
	for _, search := range cache.memory.rows {
		check(search, "stored reusable public batch")
	}
	for _, ctx := range cache.contexts {
		if bounded {
			assertRestaurantCancellationBoundedPublicContext(t, ctx, "cache")
		} else if ctx.Value(restaurantCancellationPrivateKey{}) != nil {
			t.Error("public cache operation inherited private follower context values")
		}
	}
}

func assertRestaurantCancellationResult(t *testing.T, outcome restaurantCancellationOutcome, query, requestID string) *agentworkspace.OnlinePlaceSearch {
	t.Helper()
	search := outcome.search
	if outcome.err != nil || search == nil || search.Status != "SUCCESS" || len(search.Places) != 1 {
		t.Fatalf("active caller lost the shared public result: search=%#v err=%v", search, outcome.err)
	}
	if search.Query != query || search.RequestID != requestID || search.SnapshotID == "" || search.TurnDigest != agentworkspace.OnlinePlaceTurnDigest(query) || search.TaskID != "" || search.MessageIndex != 1 || search.CacheVersion != 1 {
		t.Fatalf("caller did not bind its own private snapshot after the public read: %#v", search)
	}
	return search
}

func TestOnlineRestaurantCanceledLeaderDoesNotAbortActiveFollower(t *testing.T) {
	s, source, cache := restaurantCancellationServer(t)
	leaderCtx, cancelLeader := context.WithCancel(context.WithValue(context.Background(), restaurantCancellationPrivateKey{}, "PRIVATE_LEADER_CONTEXT_CANARY"))
	t.Cleanup(cancelLeader)
	leaderResult := startRestaurantCancellationQuery(s, leaderCtx, "PRIVATE_LEADER_QUERY_CANARY", "leader-request")
	providerCtx := waitRestaurantCancellation(t, source.started, "leader source read")
	assertRestaurantCancellationBoundedPublicContext(t, providerCtx, "provider")

	followerCtx := &restaurantCancellationWaitContext{
		Context: context.WithValue(context.Background(), restaurantCancellationPrivateKey{}, "PRIVATE_FOLLOWER_CONTEXT_CANARY"),
		reached: make(chan struct{}),
	}
	followerResult := startRestaurantCancellationQuery(s, followerCtx, "PRIVATE_FOLLOWER_QUERY_CANARY", "follower-request")
	waitRestaurantCancellation(t, followerCtx.reached, "follower joined shared read")
	cancelLeader()
	leader := waitRestaurantCancellation(t, leaderResult, "canceled leader return")
	if leader.search != nil || !errors.Is(leader.err, context.Canceled) {
		t.Errorf("canceled leader bound a private result: search=%#v err=%v", leader.search, leader.err)
	}
	if err := providerCtx.Err(); err != nil {
		t.Errorf("initiating caller cancellation aborted the public read for an active follower: %v", err)
	}
	source.unblock()
	follower := assertRestaurantCancellationResult(t, waitRestaurantCancellation(t, followerResult, "active follower completion"), "PRIVATE_FOLLOWER_QUERY_CANARY", "follower-request")
	if follower.Origin != "LIVE" || source.calls.Load() != 1 {
		t.Fatalf("active follower restarted or lost the first shared source read: origin=%q calls=%d", follower.Origin, source.calls.Load())
	}

	repeat, err := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "PRIVATE_REPEAT_QUERY_CANARY", "repeat-request", nil)
	reused := assertRestaurantCancellationResult(t, restaurantCancellationOutcome{search: repeat, err: err}, "PRIVATE_REPEAT_QUERY_CANARY", "repeat-request")
	if reused.Origin != "CACHE" || source.calls.Load() != 1 || reused.SnapshotID == follower.SnapshotID || !reused.ObservedAt.Equal(follower.ObservedAt) || !reused.ExpiresAt.Equal(follower.ExpiresAt) {
		t.Fatalf("later query repeated source cost or borrowed/mutated the follower snapshot: repeat=%#v follower=%#v calls=%d", reused, follower, source.calls.Load())
	}
	assertRestaurantCancellationPublicCache(t, cache, true)
}

func TestOnlineRestaurantCanceledFollowerDoesNotAbortActiveLeader(t *testing.T) {
	s, source, cache := restaurantCancellationServer(t)
	leaderResult := startRestaurantCancellationQuery(s, context.Background(), "PRIVATE_ACTIVE_LEADER_QUERY_CANARY", "active-leader-request")
	providerCtx := waitRestaurantCancellation(t, source.started, "active leader source read")
	assertRestaurantCancellationBoundedPublicContext(t, providerCtx, "provider")

	followerBase, cancelFollower := context.WithCancel(context.WithValue(context.Background(), restaurantCancellationPrivateKey{}, "PRIVATE_CANCELED_FOLLOWER_CONTEXT_CANARY"))
	t.Cleanup(cancelFollower)
	followerCtx := &restaurantCancellationWaitContext{Context: followerBase, reached: make(chan struct{})}
	followerResult := startRestaurantCancellationQuery(s, followerCtx, "PRIVATE_CANCELED_FOLLOWER_QUERY_CANARY", "canceled-follower-request")
	waitRestaurantCancellation(t, followerCtx.reached, "canceling follower joined shared read")
	cancelFollower()
	follower := waitRestaurantCancellation(t, followerResult, "canceled follower return")
	if follower.search != nil || !errors.Is(follower.err, context.Canceled) {
		t.Errorf("canceled follower bound a private result: search=%#v err=%v", follower.search, follower.err)
	}
	if err := providerCtx.Err(); err != nil {
		t.Errorf("follower cancellation aborted the active leader's public read: %v", err)
	}
	source.unblock()
	leader := assertRestaurantCancellationResult(t, waitRestaurantCancellation(t, leaderResult, "active leader completion"), "PRIVATE_ACTIVE_LEADER_QUERY_CANARY", "active-leader-request")
	if leader.Origin != "LIVE" || source.calls.Load() != 1 {
		t.Fatalf("active leader restarted or lost the shared source read: origin=%q calls=%d", leader.Origin, source.calls.Load())
	}
	// This is a negative control for cancellation direction. The old source
	// already bounds its provider read but uses Background for cache operations.
	assertRestaurantCancellationPublicCache(t, cache, false)
}
