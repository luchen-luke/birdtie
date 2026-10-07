package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

type opportunityReasonsAccess struct {
	identity.AccessStore
	actor identity.Actor
	err   error
}

func (a *opportunityReasonsAccess) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return a.actor, a.err
}

type opportunityReasonsStore struct {
	inputs opportunity.Inputs
	err    error
	calls  int
	owner  string
}

func (s *opportunityReasonsStore) LoadOpportunityInputs(_ context.Context, owner string) (opportunity.Inputs, error) {
	s.calls++
	s.owner = owner
	return s.inputs, s.err
}

func TestOpportunityReasonsHTTPContractAndOwnerBoundary(t *testing.T) {
	now := time.Now()
	const owner = "11111111-1111-4111-8111-111111111111"
	const placeID = "22222222-2222-4222-8222-222222222222"
	token, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	base := opportunity.Inputs{PersonID: owner, Intents: []socialintent.Record{{ID: "source", CreatorID: owner,
		Type: "FIND_ACTIVITY", Status: "ACTIVE", Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: now.Add(time.Hour),
		Constraints: json.RawMessage(`{"category":"badminton","placeId":"` + placeID + `"}`)}}, Supply: []opportunity.Supply{{
		Activity: foundation.Activity{ID: "activity", CityID: "city", PlaceID: placeID, Title: "授权活动名称", CategoryCode: "badminton",
			EndsAt: now.Add(time.Hour), Organizer: foundation.ActivityOrganizer{Type: "ORGANIZATION", ID: "organization"}},
		Place: foundation.Place{ID: placeID, CityID: "city", Name: "公开地点名称"},
	}}}
	for _, tc := range []struct {
		name, path, kind, workspace                  string
		anonymous, expired, unavailable, fail, empty bool
		want                                         int
	}{
		{"success", "/v1/me/opportunities", "person", "", false, false, false, false, false, 200},
		{"real empty", "/v1/me/opportunities", "person", "", false, false, false, false, true, 200},
		{"anonymous", "/v1/me/opportunities", "person", "", true, false, false, false, false, 401},
		{"expired session", "/v1/me/opportunities", "person", "", false, true, false, false, false, 401},
		{"organization account", "/v1/me/opportunities", "organization", "", false, false, false, false, false, 403},
		{"business account", "/v1/me/opportunities", "business", "", false, false, false, false, false, 403},
		{"organization workspace", "/v1/me/opportunities", "person", "workspace", false, false, false, false, false, 403},
		{"owner override", "/v1/me/opportunities?ownerAccountId=other", "person", "", false, false, false, false, false, 400},
		{"principal override", "/v1/me/opportunities?principalId=other", "person", "", false, false, false, false, false, 400},
		{"duplicate override", "/v1/me/opportunities?owner=own&owner=other", "person", "", false, false, false, false, false, 400},
		{"unsupported filter", "/v1/me/opportunities?cityId=aberdeen-gb", "person", "", false, false, false, false, false, 400},
		{"malformed query", "/v1/me/opportunities?%zz=other", "person", "", false, false, false, false, false, 400},
		{"unavailable", "/v1/me/opportunities", "person", "", false, false, true, false, false, 503},
		{"store failure", "/v1/me/opportunities", "person", "", false, false, false, true, false, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := &opportunityReasonsAccess{actor: identity.Actor{ID: owner, AccountType: tc.kind}}
			if tc.expired {
				access.err = identity.ErrUnauthorized
			}
			store := &opportunityReasonsStore{inputs: base}
			if tc.empty {
				store.inputs.Supply = nil
			}
			if tc.fail {
				store.err = errors.New("PRIVATE_DATABASE_ERROR_SENTINEL")
			}
			s := &server{access: access, opportunities: store}
			if tc.unavailable {
				s.opportunities = nil
			}
			req := httptest.NewRequest("GET", tc.path, nil)
			if !tc.anonymous {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			if tc.workspace != "" {
				req.Header.Set("X-Birdtie-Organization-Workspace", tc.workspace)
			}
			rw := httptest.NewRecorder()
			s.listOwnOpportunities(rw, req)
			if rw.Code != tc.want {
				t.Fatalf("status %d want %d: %s", rw.Code, tc.want, rw.Body.String())
			}
			if rw.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("owner response cacheable")
			}
			if strings.Contains(rw.Body.String(), "PRIVATE_DATABASE_ERROR_SENTINEL") {
				t.Fatal("private server error disclosed")
			}
			if tc.want != 200 && tc.want != 500 && store.calls != 0 {
				t.Fatal("invalid principal/query reached private Store")
			}
			if tc.want != 200 {
				return
			}
			if store.calls != 1 || store.owner != owner {
				t.Fatal("Store owner was not resolved session actor")
			}
			var payload struct {
				Data        []opportunity.Candidate `json:"data"`
				Source      string                  `json:"source"`
				RuleVersion string                  `json:"ruleVersion"`
			}
			if err := json.Unmarshal(rw.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Source != "RULE_BASED" || payload.RuleVersion != "activity-place-v2" || payload.Data == nil {
				t.Fatalf("legacy envelope changed: %+v", payload)
			}
			if tc.empty {
				if len(payload.Data) != 0 {
					t.Fatal("empty supply fabricated activity")
				}
				return
			}
			if len(payload.Data) != 1 || payload.Data[0].Title != "授权活动名称" || payload.Data[0].PlaceName != "公开地点名称" || payload.Data[0].Action.Type != "OPEN_ACTIVITY" {
				t.Fatalf("additive consumer contract: %+v", payload)
			}
			if !strings.Contains(payload.Data[0].Reason, "这是一条组织主办的活动") {
				t.Fatal("organization reason missing")
			}
		})
	}
}
