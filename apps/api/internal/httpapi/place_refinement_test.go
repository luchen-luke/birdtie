package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// Registered HTTP and persistence doubles only, not native SQL or real sources.
type placeRefinementUnitCatalog struct {
	nowHTTPTestCatalog
	t            *testing.T
	store        *httpActionSafetyTaskStore
	wantCategory string
	wantScope    string
	checks       int
	fail         error
}

func (c *placeRefinementUnitCatalog) ListPlaces(_ context.Context, city, term string) ([]foundation.Place, error) {
	c.checks++
	if c.store.task.Intent != agentworkspace.FindPlace || c.store.task.Filters["category"] != c.wantCategory || c.store.task.Filters["locationPreference"] != c.wantScope || c.store.task.Filters["searchTerm"] != term || c.store.task.Filters["currentQuery"] == "" {
		c.t.Fatal("original task must hold final explicit place slots before source read", c.store.task.Filters)
	}
	for _, key := range []string{"resultIDs", agentworkspace.PlaceFollowupIDFilter, agentworkspace.PlaceFollowupGenerationFilter, agentworkspace.PlaceFollowupTopicFilter} {
		if _, exists := c.store.task.Filters[key]; exists {
			c.t.Fatal("old selection survived before current source read", key)
		}
	}
	if c.fail != nil {
		return nil, c.fail
	}
	lat, lon := 57.15, -2.09
	return []foundation.Place{
		{ID: httpActionSafetyActivity, CityID: city, Name: "UNIT Cafe", CategoryCode: "cafe", Location: foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat, Longitude: &lon}},
		{ID: httpActionSafetyOtherOrg, CityID: city, Name: "UNIT Restaurant", CategoryCode: "restaurant", Location: foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat, Longitude: &lon}},
	}, nil
}

func TestPlaceRefinementSourceFailureRetainsHistoryWithoutSuccessfulOldResults(t *testing.T) {
	s, mux, store, token := httpActionSafetyServer(t, nil)
	c := &placeRefinementUnitCatalog{t: t, store: store, wantCategory: "cafe", wantScope: "city", fail: errors.New("UNIT source unavailable")}
	s.catalog = c
	store.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", ContextType: "CITY", Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskCompleted, Query: "找地点 UNIT old place", Filters: map[string]string{"targetIntent": agentworkspace.FindPlace, "locationPreference": "city", "searchTerm": "UNIT old place", "category": "restaurant", "resultIDs": "UNIT_OLD_RESULTS"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "找地点 UNIT old place"}, {Role: "assistant", Text: "UNIT_OLD_ANSWER"}}}
	body, _ := json.Marshal(map[string]string{"query": "只看咖啡馆", "taskId": httpActionSafetyTaskID})
	code, raw := httpActionSafetyCall(t, mux, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", string(body))
	if code == http.StatusOK || strings.Contains(string(raw), "UNIT_OLD_ANSWER") || strings.Contains(string(raw), "找到") {
		t.Fatal("source failure masqueraded as old result success", code, string(raw))
	}
	if store.task.Status != agentworkspace.TaskActive || len(store.task.Conversation) != 3 || store.task.Conversation[1].Text != "UNIT_OLD_ANSWER" || store.task.Conversation[2].Text != "只看咖啡馆" || store.task.Filters["category"] != "cafe" {
		t.Fatal("source failure lost original history/new raw turn", store.task)
	}
}
func (c *placeRefinementUnitCatalog) BuildOwnAgentContext(ctx context.Context, r acb.Request) (acb.BuiltContext, error) {
	b, e := c.nowHTTPTestCatalog.BuildOwnAgentContext(ctx, r)
	if e != nil {
		return b, e
	}
	lat, lon := 57.15, -2.09
	for i := range b.Places {
		p := &b.Places[i]
		p.Name, p.CategoryCode = "UNIT Cafe", "cafe"
		if p.ID == httpActionSafetyOtherOrg {
			p.Name, p.CategoryCode = "UNIT Restaurant", "restaurant"
		}
		p.Location = foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat, Longitude: &lon}
		b.Bundle.Places[i].Name = p.Name
	}
	return b, nil
}

func TestPlaceRefinementRegisteredSameTaskUpdatesSlotsBeforeOriginalSourceRead(t *testing.T) {
	s, mux, store, token := httpActionSafetyServer(t, nil)
	c := &placeRefinementUnitCatalog{t: t, store: store, wantCategory: "cafe", wantScope: "viewport"}
	s.catalog = c
	store.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalType: "person", PrincipalID: httpActionSafetyOwner, ActingUserID: httpActionSafetyOwner, ContextType: "CITY", CityID: "aberdeen-gb", Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskCompleted, Query: "找地点 UNIT old place", Filters: map[string]string{
		"targetIntent": agentworkspace.FindPlace, "searchTerm": "UNIT old place", "category": "restaurant", "locationPreference": "viewport", "timePreference": "weekend", "distancePreference": "closer", "mapWest": "-3", "mapSouth": "56", "mapEast": "-1", "mapNorth": "58", "resultIDs": "OLD_CANARY", agentworkspace.PlaceFollowupIDFilter: httpActionSafetyOtherAcct, agentworkspace.PlaceFollowupGenerationFilter: strings.Repeat("a", 64), agentworkspace.PlaceFollowupTopicFilter: agentworkspace.PlaceAddress,
	}, Conversation: []agentworkspace.Message{{Role: "user", Text: "找地点 UNIT old place"}, {Role: "assistant", Text: "UNIT earlier retained answer"}}}
	for turn, query := range []string{"只看咖啡馆", "改成餐馆", "全城的呢"} {
		if turn > 0 {
			c.wantCategory = "restaurant"
		}
		if turn == 2 {
			c.wantScope = "city"
		}
		body, _ := json.Marshal(map[string]string{"query": query, "taskId": httpActionSafetyTaskID})
		code, raw := httpActionSafetyCall(t, mux, token, "", http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks", string(body))
		if code != http.StatusOK {
			t.Fatal(code, string(raw))
		}
		var wire struct{ Data agentworkspace.Results }
		if json.Unmarshal(raw, &wire) != nil || wire.Data.Task == nil {
			t.Fatal("actual response missing task")
		}
		r := wire.Data
		if r.Task.ID != httpActionSafetyTaskID || r.Task.Query != "找地点 UNIT old place" || r.Task.CityID != "aberdeen-gb" || r.Task.Filters["currentQuery"] != query || r.Task.Filters["category"] != c.wantCategory || r.Task.Filters["searchTerm"] != "" || len(r.Task.Conversation) != 4+2*turn {
			t.Fatal("original task/history/canonical slots diverged", string(raw))
		}
		if len(r.Activities) != 0 || len(r.Places) != 1 || r.Places[0].CategoryCode != c.wantCategory || len(r.ResultSet.Entities) != 1 || len(r.MapEffects.PinEntityIDs) != 1 || r.ResultSet.Entities[0].Key() != r.MapEffects.PinEntityIDs[0] {
			t.Fatal("same place subset did not drive card/map", string(raw))
		}
		if turn == 2 {
			if b, err := agentworkspace.BoundsFromFilters(store.task.Filters); err != nil || b != nil {
				t.Fatal("explicit city scope kept prior viewport", store.task.Filters)
			}
		} else {
			if b, err := agentworkspace.BoundsFromFilters(store.task.Filters); err != nil || b == nil {
				t.Fatal("category refinement lost explicit scope")
			}
		}
	}
	if c.checks != 3 {
		t.Fatal("unexpected source calls", c.checks)
	}
}
