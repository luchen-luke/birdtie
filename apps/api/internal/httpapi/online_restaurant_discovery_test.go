package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// OFFLINE_TEST_ONLY: these are public-data transport/HTTP doubles. They do
// not query OpenStreetMap, create published Places, or prove phone rendering.
func onlineRestaurantFixture(id, name string, lat, lon float64) cityseed.SubmitInput {
	at := time.Now().UTC().Add(-time.Second)
	until := at.Add(24 * time.Hour)
	in := cityseed.SubmitInput{Name: name, CategoryCode: "restaurant", AddressLabel: "Synthetic public address",
		Latitude: &lat, Longitude: &lon, LocationPrecision: "point", ProviderCode: "openstreetmap", ProviderPlaceID: "node/" + id,
		SourceURL: "https://www.openstreetmap.org/node/" + id, Attribution: onlineRestaurantAttribution, ExpiresAt: until,
		Rights: &cityseed.SourcePermissionInput{State: cityseed.PermissionConfirmed, Acquisition: "OPEN_LICENSE", OriginProvider: "OPEN_DATA_DOWNLOAD",
			LicenseURL: "https://opendatacommons.org/licenses/odbl/1-0/", LicenseVersion: "ODbL-1.0", Attribution: onlineRestaurantAttribution, AcquiredAt: at, RetentionUntil: until}}
	field := func(key string, value any) cityseed.PlaceFieldInput {
		raw, _ := json.Marshal(value)
		return cityseed.PlaceFieldInput{Key: key, Value: raw, ObservedAt: at, ValidFrom: at, ValidTo: until, RefreshDue: until, ConflictState: "NONE"}
	}
	in.Fields = []cityseed.PlaceFieldInput{field("name", name), field("address", in.AddressLabel), field("coordinates", map[string]any{
		"coordinateSystem": "wgs84", "latitude": lat, "longitude": lon})}
	return in
}

func onlineRestaurantServer() (*server, *openCandidateStore, *openCatalog, *openSource) {
	seed, catalog := &openCandidateStore{}, &openCatalog{}
	source := &openSource{facts: []cityseed.SubmitInput{onlineRestaurantFixture("101", "Synthetic restaurant A", 57.15, -2.0943)}}
	return &server{catalog: catalog, seed: seed, openPlaces: source}, seed, catalog, source
}

func TestOnlineRestaurantDiscoveryPublicReadHasOneBoundedCallAndNoImport(t *testing.T) {
	s, seed, catalog, source := onlineRestaurantServer()
	source.facts = []cityseed.SubmitInput{onlineRestaurantFixture("102", "Synthetic farther restaurant", 57.18, -2.0943), source.facts[0]}
	before := time.Now().UTC()
	got, err := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅 PRIVATE_QUERY_CANARY", "request-public", nil)
	if err != nil || got.Status != "SUCCESS" || len(got.Places) != 2 || source.calls != 1 || catalog.read != 1 || seed.listed != 0 || seed.written != 0 {
		t.Fatalf("public query became native acquisition or lost online results: %#v err=%v calls=%d writes=%d", got, err, source.calls, seed.written)
	}
	if source.query.Latitude != 57.1497 || source.query.Longitude != -2.0943 || source.query.RadiusMeters != 6000 || source.query.Category != "restaurant" {
		t.Fatalf("query did not use authoritative city coordinates and closed category: %#v", source.query)
	}
	if got.Schema != "birdtie-online-places-v2" || got.RequestID != "request-public" || got.CityID != "aberdeen-gb" || got.Provider != "openstreetmap" || got.Scope != "city" || got.Attribution != onlineRestaurantAttribution || got.ObservedAt.Before(before) || got.ExpiresAt.Sub(got.ObservedAt) < 23*time.Hour || got.ExpiresAt.Sub(got.ObservedAt) > 24*time.Hour {
		t.Fatalf("retained search provenance/freshness mismatch: %#v", got)
	}
	if got.Places[0].ID != "osm:node:101" || got.Places[1].ID != "osm:node:102" {
		t.Fatal("restaurants must sort by factual proximity rather than ungrounded quality", got.Places)
	}
	for _, p := range got.Places {
		if p.ReviewStatus != "UNREVIEWED" || p.CityID != got.CityID || p.CategoryCode != "restaurant" || p.Location.CoordinateSystem != "wgs84" || p.Location.Precision != "point" || p.Attribution != onlineRestaurantAttribution || p.ExpiresAt.After(got.ExpiresAt) {
			t.Fatal("candidate borrowed native publication or missing map/source contract", p)
		}
	}
	message := onlineRestaurantMessage(got, 0)
	if !strings.Contains(message, "已联网找到 2 家餐厅") || !strings.Contains(message, "尚未核验评分和当前营业状态") || strings.Contains(message, "已发布") {
		t.Fatal("public online candidates were presented as verified recommendations", message)
	}
}

func TestOnlineRestaurantDiscoveryViewportFiltersCandidatesInsideCity(t *testing.T) {
	s, seed, _, source := onlineRestaurantServer()
	source.facts = append(source.facts, onlineRestaurantFixture("102", "Synthetic outside viewport", 57.18, -2.0943))
	bounds := &agentworkspace.MapBounds{West: -2.11, East: -2.08, South: 57.14, North: 57.16}
	got, err := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "附近有没有好的餐厅", "viewport-read", bounds)
	if err != nil || got.Status != "SUCCESS" || got.Scope != "viewport" || len(got.Places) != 1 || got.Places[0].ID != "osm:node:101" || source.calls != 1 || source.query.Latitude != 57.1497 || seed.written != 0 {
		t.Fatal("viewport replaced city center or leaked outside candidates", got, err)
	}
	bounds.South, bounds.North = 57.20, 57.21
	got, err = s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "附近有没有好的餐厅", "empty-viewport", bounds)
	if err != nil || got.Status != "EMPTY" || len(got.Places) != 0 || source.calls != 2 {
		t.Fatal("genuine empty viewport was confused with provider unavailability", got, err)
	}
}

func TestOnlineRestaurantDiscoveryUnavailableIsDifferentFromSuccessfulEmpty(t *testing.T) {
	for _, tc := range []string{"missing-provider", "missing-map", "provider-error", "empty"} {
		t.Run(tc, func(t *testing.T) {
			s, seed, catalog, source := onlineRestaurantServer()
			want, calls := "UNAVAILABLE", 0
			switch tc {
			case "missing-provider":
				s.openPlaces = nil
			case "missing-map":
				catalog.withoutMap = true
			case "provider-error":
				source.err, calls = errors.New("RAW_UPSTREAM_SECRET_CANARY"), 1
			case "empty":
				source.facts, want, calls = nil, "EMPTY", 1
			}
			got, err := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "state-read", nil)
			if err != nil || got.Status != want || len(got.Places) != 0 || source.calls != calls || seed.listed != 0 || seed.written != 0 {
				t.Fatal("failure became successful empty or caused acquisition", got, err)
			}
			raw, _ := json.Marshal(got)
			message := onlineRestaurantMessage(got, 0)
			if strings.Contains(string(raw)+message, "RAW_UPSTREAM_SECRET_CANARY") || (want == "UNAVAILABLE" && !strings.Contains(message, "暂时不可用")) || (want == "EMPTY" && !strings.Contains(message, "已联网查询")) {
				t.Fatal("provider state/error message mismatch", string(raw), message)
			}
		})
	}
}

func TestOnlineRestaurantDiscoveryInvalidBatchNeverPublishesEarlierCandidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		bad  func(*cityseed.SubmitInput)
	}{
		{"node-zero", func(p *cityseed.SubmitInput) { p.ProviderPlaceID = "node/0" }},
		{"wrong-provider", func(p *cityseed.SubmitInput) { p.ProviderCode = "tencent_wsa" }},
		{"source-node-mismatch", func(p *cityseed.SubmitInput) { p.SourceURL = "https://www.openstreetmap.org/node/999" }},
		{"source-http", func(p *cityseed.SubmitInput) { p.SourceURL = "http://www.openstreetmap.org/node/102" }},
		{"category", func(p *cityseed.SubmitInput) { p.CategoryCode = "cafe" }},
		{"missing-attribution", func(p *cityseed.SubmitInput) { p.Attribution = "" }},
		{"invalid-name", func(p *cityseed.SubmitInput) { p.Name = "name\nprovider instruction" }},
		{"missing-location", func(p *cityseed.SubmitInput) { p.Latitude = nil }},
		{"invalid-coordinate", func(p *cityseed.SubmitInput) { v := math.NaN(); p.Latitude = &v }},
		{"outside-city", func(p *cityseed.SubmitInput) {
			v := 57.5
			p.Latitude = &v
			p.Fields[2].Value = json.RawMessage(`{"coordinateSystem":"wgs84","latitude":57.5,"longitude":-2.0943}`)
		}},
		{"coordinate-field-mismatch", func(p *cityseed.SubmitInput) {
			p.Fields[2].Value = json.RawMessage(`{"coordinateSystem":"wgs84","latitude":57.5,"longitude":-2.0943}`)
		}},
		{"missing-rights", func(p *cityseed.SubmitInput) { p.Rights = nil }},
		{"pending-rights", func(p *cityseed.SubmitInput) { p.Rights.State = cityseed.PermissionPending }},
		{"wrong-license-url", func(p *cityseed.SubmitInput) { p.Rights.LicenseURL = "https://example.invalid/license" }},
		{"wrong-license-version", func(p *cityseed.SubmitInput) { p.Rights.LicenseVersion = "synthetic-v1" }},
		{"wrong-license-attribution", func(p *cityseed.SubmitInput) { p.Rights.Attribution = "Unrelated rights attribution" }},
		{"wrong-acquisition", func(p *cityseed.SubmitInput) {
			p.Rights.Acquisition, p.Rights.OriginProvider = "FIRST_PARTY", "BIRDTIE_FIRST_PARTY"
		}},
		{"expired-facts", func(p *cityseed.SubmitInput) { p.ExpiresAt = time.Now().Add(-time.Minute) }},
		{"future-observation", func(p *cityseed.SubmitInput) { p.Fields[0].ObservedAt = time.Now().Add(time.Hour) }},
		{"stale-observation", func(p *cityseed.SubmitInput) { p.Fields[0].ObservedAt = time.Now().Add(-24 * time.Hour) }},
		{"stale-acquisition", func(p *cityseed.SubmitInput) { p.Rights.AcquiredAt = time.Now().Add(-24 * time.Hour) }},
		{"future-validity", func(p *cityseed.SubmitInput) { p.Fields[0].ValidFrom = time.Now().Add(time.Hour) }},
		{"refresh-overdue", func(p *cityseed.SubmitInput) { p.Fields[0].RefreshDue = p.Fields[0].ValidFrom }},
		{"unresolved-field", func(p *cityseed.SubmitInput) { p.Fields[0].ConflictState = "UNRESOLVED" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, seed, _, source := onlineRestaurantServer()
			bad := onlineRestaurantFixture("102", "Synthetic invalid candidate", 57.151, -2.0943)
			tc.bad(&bad)
			source.facts = append(source.facts, bad)
			got, err := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "invalid-source", nil)
			if err != nil || got.Status != "UNAVAILABLE" || len(got.Places) != 0 || source.calls != 1 || seed.written != 0 || seed.listed != 0 {
				t.Fatal("invalid second fact left a misleading successful/partial recommendation", got, err)
			}
		})
	}
}

func TestOnlineRestaurantDiscoveryDuplicateAndOversizedBatchesAreUnavailable(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		s, seed, _, source := onlineRestaurantServer()
		if oversized {
			for len(source.facts) < 16 {
				source.facts = append(source.facts, source.facts[0])
			}
		} else {
			source.facts = append(source.facts, source.facts[0])
		}
		got, err := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "bounded-batch", nil)
		if err != nil || got.Status != "UNAVAILABLE" || len(got.Places) != 0 || source.calls != 1 || seed.written != 0 {
			t.Fatal("duplicate/oversized provider batch silently became usable", got, err)
		}
	}
}

func TestOnlineRestaurantCandidateLifetimeDoesNotOutliveSourceFields(t *testing.T) {
	p := onlineRestaurantFixture("101", "Synthetic short-lived source", 57.15, -2.0943)
	now := time.Now().UTC()
	until := now.Add(30 * time.Second)
	p.Fields[2].ValidTo, p.Fields[2].RefreshDue = until, until
	got, valid := onlineRestaurantCandidate(p, "aberdeen-gb", now, now.Add(10*time.Minute))
	if !valid || got.ExpiresAt.After(until) {
		t.Fatal("candidate renewed coordinate facts past their source validity", got, valid)
	}
	s, _, _, source := onlineRestaurantServer()
	source.facts = append(source.facts, p)
	source.facts[0].ProviderPlaceID, source.facts[0].SourceURL = "node/102", "https://www.openstreetmap.org/node/102"
	search, err := s.discoverOnlineRestaurants(context.Background(), "aberdeen-gb", "有没有好的餐厅", "source-lifetime", nil)
	if err != nil || search.Status != "SUCCESS" || !search.ExpiresAt.Equal(until) {
		t.Fatal("batch search lifetime exceeded the earliest source field", search, err)
	}
	for _, candidate := range search.Places {
		if !candidate.ExpiresAt.Equal(search.ExpiresAt) {
			t.Fatal("one candidate escaped the batch's source lifetime", candidate)
		}
	}
}

type onlineRestaurantHTTPCatalog struct {
	*openCatalog
	lists int
}

func (c *onlineRestaurantHTTPCatalog) ListPlaces(context.Context, string, string) ([]foundation.Place, error) {
	c.lists++
	return []foundation.Place{}, nil
}

func TestOnlineRestaurantGuestHTTPNaturalLanguageGivesExternalMapFacts(t *testing.T) {
	for _, query := range []string{"有没有好的餐厅", "阿布丁有没有好的餐厅", "阿伯丁有没有好的餐厅"} {
		t.Run(query, func(t *testing.T) {
			s, h, tasks, _ := httpActionSafetyServer(t, nil)
			seed := &openCandidateStore{}
			catalog := &onlineRestaurantHTTPCatalog{openCatalog: &openCatalog{}}
			source := &openSource{facts: []cityseed.SubmitInput{onlineRestaurantFixture("101", "Synthetic online restaurant", 57.15, -2.0943)}}
			s.catalog, s.seed, s.openPlaces, s.agent = catalog, seed, source, &anonymousRecoveryPublicTasks{tasks}
			body, _ := json.Marshal(map[string]any{"query": query, "mapBounds": agentworkspace.MapBounds{West: 0, East: 1, South: 0, North: 1}})
			code, raw := httpActionSafetyCall(t, h, "", "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", string(body))
			var wire struct {
				Data agentworkspace.Results `json:"data"`
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			got := wire.Data
			if code != 200 || got.OnlinePlaceSearch == nil || got.OnlinePlaceSearch.Status != "SUCCESS" || got.OnlinePlaceSearch.Scope != "city" || len(got.OnlinePlaceSearch.Places) != 1 || source.calls != 1 || catalog.lists != 1 || seed.written != 0 || seed.listed != 0 || tasks.task.ID != "" || got.Task != nil || got.TaskID != "" {
				t.Fatalf("guest natural-language request lost online facts or borrowed native authority: %d %s", code, raw)
			}
			if got.Mode != "online_places" || !strings.Contains(got.Message, "已联网找到 1 家餐厅") || strings.Contains(got.Message, "0 个已发布") || len(got.Places) != 0 {
				t.Fatal("guest result decoration hid the external search behind zero native Places", string(raw))
			}
		})
	}
}

func TestOnlineRestaurantTaskGETNeverReacquiresOrReplaysTransientCandidates(t *testing.T) {
	s, h, tasks, token := httpActionSafetyServer(t, nil)
	catalog := &onlineRestaurantHTTPCatalog{openCatalog: &openCatalog{}}
	source := &openSource{facts: []cityseed.SubmitInput{onlineRestaurantFixture("101", "Synthetic should not be read", 57.15, -2.0943)}}
	seed := &openCandidateStore{}
	native := &nowHTTPNativeStore{currentReadonlySearchHTTPSpy: &currentReadonlySearchHTTPSpy{httpActionSafetyTaskStore: tasks}}
	s.catalog, s.openPlaces, s.seed, s.agent = catalog, source, seed, native
	tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner,
		CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: "有没有好的餐厅",
		Filters:      map[string]string{"reason": agentworkspace.RestaurantDiscovery, "category": "restaurant", "locationPreference": "city", "currentQuery": "有没有好的餐厅"},
		Conversation: []agentworkspace.Message{{Role: "user", Text: "有没有好的餐厅"}, {Role: "assistant", Text: "本轮已查询公开餐厅来源；联网地点仅本轮有效，重新搜索可查看最新结果。"}}}
	code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	var wire struct {
		Data agentworkspace.Results `json:"data"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if code != 200 || source.calls != 0 || catalog.lists != 0 || seed.written != 0 || native.reads != 0 || native.late != 0 || native.legacy != 0 || native.legacyFinal != 0 || wire.Data.OnlinePlaceSearch != nil || len(wire.Data.Places) != 0 || len(wire.Data.ResultSet.Items) != 0 || !strings.Contains(wire.Data.Message, "重新查询") || strings.Contains(string(raw), "Synthetic should not be read") {
		t.Fatalf("GET silently reacquired/restored transient external provider facts: %d %s", code, raw)
	}
}

func TestOnlineRestaurantGuestNearbyWithoutMapRequiresScopeBeforeNetwork(t *testing.T) {
	s, h, tasks, _ := httpActionSafetyServer(t, nil)
	catalog := &onlineRestaurantHTTPCatalog{openCatalog: &openCatalog{}}
	source := &openSource{facts: []cityseed.SubmitInput{onlineRestaurantFixture("101", "Synthetic must not be read", 57.15, -2.0943)}}
	s.catalog, s.openPlaces, s.agent = catalog, source, &anonymousRecoveryPublicTasks{tasks}
	code, raw := httpActionSafetyCall(t, h, "", "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", `{"query":"附近有没有好的餐厅"}`)
	var wire struct {
		Data agentworkspace.Results `json:"data"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if code != 200 || source.calls != 0 || catalog.lists != 0 || wire.Data.OnlinePlaceSearch != nil || !strings.Contains(wire.Data.Message, "请先移动地图") {
		t.Fatalf("nearby request silently dropped its scope and searched the city: %d %s", code, raw)
	}
}

func TestOnlineRestaurantNamedCityWrapperRejectsMalformedRawInput(t *testing.T) {
	city := foundation.City{ID: "aberdeen-gb", Name: "Aberdeen"}
	for _, query := range []string{"\n阿布丁有没有好的餐厅", "阿伯丁有没有好的餐厅\t", "阿布丁\x00有没有好的餐厅", "\xff阿布丁有没有好的餐厅"} {
		if _, ok := restaurantDiscoveryInCity(query, city); ok {
			t.Fatalf("named-city wrapper sanitized malformed input into a valid discovery: %q", query)
		}
	}
}

type onlineRestaurantFinalReleaseStore struct {
	*httpActionSafetyTaskStore
	checks      int
	afterEncode func()
}

func (s *onlineRestaurantFinalReleaseStore) ValidateHumanAgentTask(_ context.Context, digest [32]byte, actor identity.Actor, task agentworkspace.Task) error {
	s.checks++
	if digest == ([32]byte{}) || actor.ID != task.PrincipalID || task.ID != s.task.ID {
		return identity.ErrUnauthorized
	}
	if s.afterEncode != nil {
		s.afterEncode()
	}
	return nil
}

func TestOnlineRestaurantFinalReleaseRejectsExpiryAfterEncodingAndAuthCheck(t *testing.T) {
	for _, which := range []string{"current", "batch-expires", "candidate-expires"} {
		t.Run(which, func(t *testing.T) {
			s, _, tasks, token := httpActionSafetyServer(t, nil)
			now := time.Now().UTC()
			tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner,
				CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Query: "有没有好的餐厅",
				Filters: map[string]string{"reason": agentworkspace.RestaurantDiscovery}, Conversation: []agentworkspace.Message{}}
			candidate, valid := onlineRestaurantCandidate(onlineRestaurantFixture("101", "ENCODED_RESTAURANT_RELEASE_CANARY", 57.15, -2.0943), "aberdeen-gb", now, now.Add(time.Minute))
			if !valid {
				t.Fatal("invalid final release fixture")
			}
			search := &agentworkspace.OnlinePlaceSearch{Schema: "birdtie-online-places-v1", RequestID: "final-release", CityID: "aberdeen-gb", Query: tasks.task.Query,
				Status: "SUCCESS", Provider: "openstreetmap", Attribution: onlineRestaurantAttribution, Scope: "city", ObservedAt: now, ExpiresAt: now.Add(time.Minute), Places: []agentworkspace.ExternalPlaceCandidate{candidate}}
			store := &onlineRestaurantFinalReleaseStore{httpActionSafetyTaskStore: tasks}
			store.afterEncode = func() {
				// The original response is encoded before this final owned-task
				// authority check. Model a lifetime ending while that check waited.
				if which == "batch-expires" {
					search.ExpiresAt = time.Now().Add(-time.Second)
				}
				if which == "candidate-expires" {
					search.Places[0].ExpiresAt = time.Now().Add(-time.Second)
				}
			}
			s.agent = store
			result := emptyAgentResults("aberdeen-gb", tasks.task.Query)
			result.OnlinePlaceSearch = search
			result = workspaceEnvelope(result, tasks.task, true, "person", "PERSONAL", "", nil, "final-release")
			r := httptest.NewRequest(http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			s.respondCurrentAgentTask(w, r, result, s.access.(httpActionSafetyAccess).digest, identity.Actor{ID: httpActionSafetyOwner, AccountType: "person"})
			if store.checks != 1 {
				t.Fatal("test did not reach the final authority check", store.checks, w.Code, w.Body.String())
			}
			if which == "current" {
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ENCODED_RESTAURANT_RELEASE_CANARY") || !strings.Contains(w.Body.String(), `"onlinePlaceSearch"`) {
					t.Fatal("valid final release lost or cleared its candidate", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"result_source_changed"`) || strings.Contains(w.Body.String(), `"data"`) || strings.Contains(w.Body.String(), "ENCODED_RESTAURANT_RELEASE_CANARY") {
				t.Fatal("encoded expired candidate was released or cleared into a false successful result", w.Code, w.Body.String())
			}
		})
	}
}
