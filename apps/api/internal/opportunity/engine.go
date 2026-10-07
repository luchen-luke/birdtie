package opportunity

import (
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

const RuleVersion = "activity-place-v2"

const (
	TierExistingTie     = "EXISTING_TIE"
	TierSharedCommunity = "SHARED_COMMUNITY"
	TierFollowedPublic  = "FOLLOWED_PUBLIC"
	TierPublic          = "PUBLIC"
	TierOtherVisible    = "OTHER_VISIBLE"
)

// Generate returns only references to supplied entities. Supplying live,
// authorized records is the Store's responsibility; this pure layer also
// rejects stale/incompatible inputs to keep fixture behavior deterministic.
func Generate(now time.Time, in Inputs) []Candidate {
	result := []Candidate{}
	for _, intent := range in.Intents {
		if intent.CreatorID != in.PersonID || intent.Type != "FIND_ACTIVITY" ||
			intent.Status != socialintent.Active || !intent.ExpiresAt.After(now) ||
			intent.Modality != "IN_PERSON" {
			continue
		}
		constraints, _, err := socialintent.ParseConstraints(intent.Constraints, intent.Modality)
		if err != nil {
			continue
		}
		cityID := intent.CityID
		if intent.ContextID != nil {
			selected, ok := in.ContextCities[*intent.ContextID]
			if !ok || (cityID != "" && cityID != selected) {
				continue
			}
			cityID = selected
		}
		if constraints.Category == "" && constraints.PlaceID == "" &&
			constraints.AreaLabel == "" && cityID == "" {
			// An unconstrained global search is not a defensible opportunity.
			continue
		}
		for _, supply := range in.Supply {
			activity, place := supply.Activity, supply.Place
			if activity.ID == "" || place.ID == "" || activity.PlaceID != place.ID ||
				activity.CityID != place.CityID || !activity.EndsAt.After(now) ||
				activity.Status == "cancelled" || activity.Status == "past" ||
				place.Source.Freshness == "expired" ||
				activity.Source.Freshness == "expired" {
				continue
			}
			if cityID != "" && activity.CityID != cityID {
				continue
			}
			if constraints.Category != "" && activity.CategoryCode != constraints.Category {
				continue
			}
			if constraints.PlaceID != "" && place.ID != constraints.PlaceID {
				continue
			}
			if constraints.AreaLabel != "" && !textContains(place.Name, constraints.AreaLabel) &&
				!textContains(place.AddressLabel, constraints.AreaLabel) {
				continue
			}
			codes := []string{}
			phrases := []string{}
			if constraints.Category != "" {
				codes = append(codes, "INTENT_CATEGORY")
				phrases = append(phrases, "活动类别与你保存的意图一致")
			}
			if constraints.PlaceID != "" {
				codes = append(codes, "INTENT_PLACE")
				phrases = append(phrases, "地点与你指定的地点一致")
			} else if constraints.AreaLabel != "" {
				codes = append(codes, "PLACE_TEXT_AREA")
				phrases = append(phrases, "地点名称或审核地址包含你填写的区域文字")
			}
			if cityID != "" {
				codes = append(codes, "INTENT_CITY_CONTEXT")
				phrases = append(phrases, "活动位于你选择的城市情境")
			} else if relation := in.DeclaredCities[activity.CityID]; relation == "current" || relation == "destination" {
				codes = append(codes, "DECLARED_CITY_CONTEXT")
				phrases = append(phrases, "活动位于你主动选择的城市情境")
			}
			if activity.Organizer.Type == "PERSON" && in.TiedPeople[activity.Organizer.ID] {
				codes = append(codes, "TIE_ORGANIZER")
				phrases = append(phrases, "主办者与你已有好友关系")
			}
			if activity.Organizer.Type == "ORGANIZATION" && strings.TrimSpace(activity.Organizer.ID) != "" {
				codes = append(codes, "ORGANIZATION_ACTIVITY")
				phrases = append(phrases, "这是一条组织主办的活动")
			}
			tier := TierOtherVisible
			switch {
			case activity.Organizer.Type == "PERSON" && in.TiedPeople[activity.Organizer.ID]:
				tier = TierExistingTie
			case activity.Organizer.Type == "COMMUNITY" && in.JoinedCommunities[activity.Organizer.ID]:
				tier = TierSharedCommunity
				codes = append(codes, "JOINED_COMMUNITY")
				phrases = append(phrases, "活动来自你已加入的社群")
			case activity.Visibility == "public" && in.FollowedOrganizers[activity.Organizer.Type+":"+activity.Organizer.ID]:
				tier = TierFollowedPublic
				codes = append(codes, "FOLLOWED_ORGANIZER")
				phrases = append(phrases, "活动来自你关注的主办者")
			case activity.Visibility == "public":
				tier = TierPublic
			}
			entity := EntityRef{Type: "ACTIVITY", ID: activity.ID}
			result = append(result, Candidate{
				ID: intent.ID + ":" + activity.ID, IntentID: intent.ID,
				Entity: entity, Place: EntityRef{Type: "PLACE", ID: place.ID},
				Title: activity.Title, PlaceName: place.Name,
				ReasonCodes: codes, Reason: strings.Join(phrases, "；"),
				Action:      Action{Type: "OPEN_ACTIVITY", Target: entity},
				RuleVersion: RuleVersion, RouteTier: tier, StartsAt: activity.StartsAt,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if tierRank(result[i].RouteTier) != tierRank(result[j].RouteTier) {
			return tierRank(result[i].RouteTier) < tierRank(result[j].RouteTier)
		}
		if !result[i].StartsAt.Equal(result[j].StartsAt) {
			return result[i].StartsAt.Before(result[j].StartsAt)
		}
		return result[i].ID < result[j].ID
	})
	if len(result) > 50 {
		result = result[:50]
	}
	return result
}

func tierRank(tier string) int {
	switch tier {
	case TierExistingTie:
		return 0
	case TierSharedCommunity:
		return 1
	case TierFollowedPublic:
		return 2
	case TierPublic:
		return 3
	default:
		return 4
	}
}

func textContains(haystack, needle string) bool {
	return haystack != "" && strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
