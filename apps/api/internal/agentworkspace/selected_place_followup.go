package agentworkspace

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

// A map selection identifies a public row to reread, never membership or a grant.
type SelectedPlaceSelector struct {
	PlaceID string `json:"placeId"`
}

var selectedPlaceID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s SelectedPlaceSelector) Valid() bool {
	return selectedPlaceID.MatchString(s.PlaceID) && s.PlaceID != "00000000-0000-0000-0000-000000000000"
}

type SelectedPlaceFollowupPort interface {
	ContinueOwnSelectedPlace(context.Context, arp.Access, string, SelectedPlaceSelector) (Task, error)
}

const (
	SelectedPlaceContextFilter = "selectedPlaceContext"
	SelectedPlaceDayFilter     = "selectedPlacePendingDay"
	SelectedPlaceDayDraft      = "selected_place_day_draft"
)

// Only the literal current question selects this bounded local path. No model,
// booking, message, itinerary write or inferred operation is admitted here.
func ParseSelectedPlaceFollowup(query string) (PlaceFollowup, int, bool) {
	plain := strings.ToLower(strings.TrimRight(strings.TrimSpace(query), " ?？!！。."))
	switch plain {
	case "把这个加入第二天", "把这个地方加入第二天", "把它加入第二天", "将这个加入第二天", "将它加入第二天", "加入第二天", "add this to day 2", "add this place to day 2", "add it to day 2":
		return PlaceFollowup{Topic: PlacePublicInformation, TimePreference: "anytime", Pronoun: true}, 2, true
	}
	current := Task{Intent: FindPlace, Filters: map[string]string{"timePreference": "anytime"}}
	f, ok := ParsePlaceFollowup(query, &current)
	return f, 0, ok && f.Pronoun && f.Name == "" && (f.Topic == PlaceAddress || f.Topic == PlaceOpeningHours)
}

func SelectedPlaceDayDraftTask(t Task) bool {
	_, day, ok := ParseSelectedPlaceFollowup(t.Filters["currentQuery"])
	return ok && day == 2 && t.Filters[SelectedPlaceContextFilter] == "native-map-v1" &&
		t.Filters[SelectedPlaceDayFilter] == "2" && t.Filters[PlaceFollowupTopicFilter] == PlacePublicInformation &&
		t.Intent == FindPlace && t.Status == TaskActive && t.ContextType == "CITY" &&
		ValidPlaceFollowupName(t.Filters["searchTerm"])
}

func SelectedPlaceDayDraftText(name string) string {
	return fmt.Sprintf("先保留“%s”作为第2天的待安排地点。你想用它补充哪一项安排？目前是待确认草稿，尚未保存到计划。", name)
}
