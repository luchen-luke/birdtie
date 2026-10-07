package agentworkspace

import (
	"encoding/json"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

func TestAgentResponseReferencesOnlyReturnedEntities(t *testing.T) {
	lat, lon := 57.14, -2.1
	updated := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	task := Task{ID: "task-1", Status: TaskCompleted, UpdatedAt: updated,
		Filters: map[string]string{"category": "badminton", "resultIDs": "private", "reason": "internal"}}
	result := Results{CityID: "aberdeen-gb", Query: "找羽毛球",
		Activities: []foundation.Activity{{ID: "activity-1", Location: &foundation.Location{
			CoordinateSystem: "wgs84", Precision: "point", Latitude: &lat, Longitude: &lon}}},
		Organizations: []Organization{{ID: "org-1"}},
		Places:        []foundation.Place{}, People: []Person{}, Groups: []Group{}}
	got := WithContract(result, task, "request-1")
	if got.RequestID != "request-1" || got.ConversationID != "task-1" ||
		got.ResultSet.ID != "task-1:"+strconv.FormatInt(updated.UnixNano(), 10) || got.ResultSet.TaskID != "task-1" ||
		got.ResultSet.Status != "ready" || len(got.ResultSet.Entities) != 2 {
		t.Fatalf("unexpected contract: %#v", got)
	}
	if got.ResultSet.Entities[0] != (EntityRef{Type: "activity", ID: "activity-1"}) ||
		got.ResultSet.Entities[1] != (EntityRef{Type: "organization", ID: "org-1"}) {
		t.Fatalf("incorrect entity refs: %#v", got.ResultSet.Entities)
	}
	if len(got.MapEffects.PinEntityIDs) != 1 || got.MapEffects.PinEntityIDs[0] != "activity:activity-1" || got.MapEffects.Camera != "preserve" {
		t.Fatalf("map effect leaked or missed pins: %#v", got.MapEffects)
	}
	if _, ok := got.ResultSet.Filters["resultIDs"]; ok {
		t.Fatal("internal result IDs leaked")
	}
	if got.ResultSet.GeneratedAt != updated {
		t.Fatal("result set timestamp changed")
	}
}

func TestTypedResultAllSevenRefsAndActionsShareOneProjection(t *testing.T) {
	activity := arp.Ref{Type: "activity", ID: "original-activity"}
	business := arp.Ref{Type: "business", ID: "original-business"}
	result := WithContract(Results{
		Activities:    []foundation.Activity{{ID: activity.ID, Title: "活动", Summary: "第一行\n第二行"}},
		Organizations: []Organization{{ID: "original-organization", Name: "组织"}},
		Places:        []foundation.Place{{ID: "original-place", Name: "地点"}},
		People:        []Person{{AccountID: "original-person", DisplayName: "用户"}},
		Groups:        []Group{{EntityType: "community", ID: "original-community", Name: "社区"}},
		ProjectionItems: []arp.Item{
			{Entity: business, Title: "商家", Scope: arp.AuthorizedView, Detail: &business, Share: &business},
			{Entity: arp.Ref{Type: "opportunity", ID: "own-intent:original-activity"}, Title: "活动", Scope: arp.SelfPrivate, Detail: &activity, Share: &activity},
		},
	}, Task{}, "typed-7")
	if result.ResultSet.Schema != arp.Schema || len(result.ResultSet.Items) != 7 || arp.ValidateItems(result.ResultSet.Items) != nil {
		t.Fatalf("seven current projections absent: %+v", result.ResultSet)
	}
	for n, item := range result.ResultSet.Items {
		if item.Entity != result.ResultSet.Entities[n] {
			t.Fatal("ref changed between projections")
		}
	}
	if len(result.MapEffects.PinEntityIDs) != 0 {
		t.Fatal("inferred coordinate")
	}
	if result.ResultSet.Items[4].Entity.Type != "community" {
		t.Fatal("native Community mislabeled Group")
	}
	if *result.ResultSet.Items[6].Share != activity {
		t.Fatal("private opportunity shared itself")
	}
}

func TestTypedResultUnknownLegacyGroupIsNotCommunityAndConflictHidesBoth(t *testing.T) {
	r := arp.Ref{Type: "activity", ID: "same"}
	got := WithContract(Results{Groups: []Group{{ID: "legacy", Name: "社群"}}, Activities: []foundation.Activity{{ID: r.ID, Title: "原活动"}}, ProjectionItems: []arp.Item{{Entity: r, Title: "矛盾来源", Scope: arp.AuthorizedView}}}, Task{}, "conflict")
	if len(got.ResultSet.Items) != 1 || got.ResultSet.Items[0].Entity.Type != "group" || got.ResultSet.Items[0].Detail != nil || got.ResultSet.Items[0].Share != nil {
		t.Fatalf("ambiguous/renamed projection: %+v", got.ResultSet)
	}
}

func TestTypedResultNativeCompatibilityDoesNotDuplicateAuthoritativeItems(t *testing.T) {
	ref := arp.Ref{Type: "activity", ID: "original-activity"}
	got := WithContract(Results{NativeProjection: true, Activities: []foundation.Activity{{ID: ref.ID, Title: "当前同源活动"}}, ProjectionItems: []arp.Item{{Entity: ref, Title: "当前同源活动", Scope: arp.AuthorizedView, Detail: &ref, Share: &ref}}}, Task{}, "native")
	if len(got.Activities) != 1 || len(got.ResultSet.Items) != 1 || len(got.ResultSet.Entities) != 1 || got.ResultSet.Items[0].Entity != ref {
		t.Fatal("compatibility duplicated or lost native ref", got)
	}
}

func TestTypedResultOnlineAndInvalidPointCannotPin(t *testing.T) {
	lat, lon, invalid := 57.14, -2.1, 200.0
	got := WithContract(Results{Activities: []foundation.Activity{
		{ID: "online", Title: "线上", Modality: "online", Location: &foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat, Longitude: &lon}},
		{ID: "invalid", Title: "错误点位", Location: &foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat, Longitude: &invalid}},
	}}, Task{}, "no-point")
	if len(got.ResultSet.Items) != 2 || len(got.MapEffects.PinEntityIDs) != 0 {
		t.Fatalf("invalid or online pin: %+v", got)
	}
}

func TestPersonMapCoordinatesRequirePublicBroadZone(t *testing.T) {
	lat, lon := 57.14, -2.1
	invalid := 91.0
	tests := []struct {
		name   string
		person Person
		pinned bool
	}{
		{"no opt in", Person{AccountID: "a", MapLatitude: &lat, MapLongitude: &lon}, false},
		{"invalid zone", Person{AccountID: "a", PublicMapZone: "street", MapLatitude: &lat, MapLongitude: &lon}, false},
		{"missing coordinate", Person{AccountID: "a", PublicMapZone: "north", MapLatitude: &lat}, false},
		{"invalid coordinate", Person{AccountID: "a", PublicMapZone: "north", MapLatitude: &invalid, MapLongitude: &lon}, false},
		{"broad opt in", Person{AccountID: "a", PublicMapZone: "north", MapLatitude: &lat, MapLongitude: &lon}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WithContract(Results{People: []Person{tc.person}}, Task{}, "request")
			if pinned := len(got.MapEffects.PinEntityIDs) == 1; pinned != tc.pinned {
				t.Fatalf("pin status = %t, want %t", pinned, tc.pinned)
			}
			body, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if contains := strings.Contains(string(body), "mapLatitude"); contains != tc.pinned {
				t.Fatalf("person coordinates serialized = %t, want %t", contains, tc.pinned)
			}
		})
	}
}

func TestAgentResponseEmptyIsSuccessful(t *testing.T) {
	got := WithContract(Results{CityID: "aberdeen-gb", Query: "找地点"}, Task{Status: TaskCompleted}, "request-2")
	if got.ResultSet.ID != "request-2" || got.ResultSet.Status != "empty" ||
		got.ResultSet.Entities == nil || got.Actions == nil || got.FollowUps == nil ||
		got.MapEffects.PinEntityIDs == nil {
		t.Fatalf("empty success contract incomplete: %#v", got)
	}
	failed := WithContract(Results{}, Task{Status: TaskFailed}, "request-3")
	if failed.ResultSet.Status != "unsupported" {
		t.Fatalf("unsupported request was silently empty: %#v", failed.ResultSet)
	}
}
