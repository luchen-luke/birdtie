package placematch

import (
	"sort"
	"strings"
	"time"

	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

// Generate only ranks public Place and independently reviewed Venue supply.
// It never infers travel distance, availability, ownership or a booking.
func Generate(now time.Time, in Inputs) []Match {
	out := []Match{}
	intent := in.Intent
	if intent.CreatorID != in.PersonID || intent.Status != socialintent.Active ||
		!intent.ExpiresAt.After(now) || intent.Modality != "IN_PERSON" ||
		(intent.Type != "FIND_ACTIVITY" && intent.Type != "FIND_COMPANION" && intent.Type != "ORGANIZE") {
		return out
	}
	c, _, err := socialintent.ParseConstraints(intent.Constraints, intent.Modality)
	if err != nil {
		return out
	}
	if c.PlaceID == "" && in.CityID == "" {
		return out
	}
	seen := map[string]bool{}
	for _, item := range in.Supply {
		p, v := item.Place, item.Venue
		if p.ID == "" || seen[p.ID] || p.Source.Freshness == "expired" ||
			(in.CityID != "" && p.CityID != in.CityID) ||
			(c.PlaceID != "" && p.ID != c.PlaceID) {
			continue
		}
		if c.AreaLabel != "" && !contains(p.Name, c.AreaLabel) && !contains(p.AddressLabel, c.AreaLabel) {
			continue
		}
		if v != nil && (v.PlaceID != p.ID || v.CityID != p.CityID || !v.ExpiresAt.After(now)) {
			v = nil
		}
		// A generic Place is returned only when the user explicitly selected it.
		if v == nil && c.PlaceID == "" {
			continue
		}
		codes := []string{}
		phrases := []string{}
		rank := 3
		if c.PlaceID != "" {
			codes = append(codes, "EXPLICIT_PLACE")
			phrases = append(phrases, "与你指定的地点一致")
			rank = 1
		}
		if c.AreaLabel != "" {
			codes = append(codes, "REVIEWED_AREA_TEXT")
			phrases = append(phrases, "地点名称或审核地址包含你填写的区域文字")
		}
		if in.CityID != "" {
			codes = append(codes, "SELECTED_CITY")
			phrases = append(phrases, "位于你选择的城市情境")
		}
		var capacity *int
		reservation := ""
		var expiry *time.Time
		if v != nil {
			if c.PlaceID != "" {
				rank = 0
			}
			if c.Category != "" {
				found := false
				for _, code := range v.Suitability {
					if code == c.Category {
						found = true
						break
					}
				}
				if !found {
					continue
				}
				codes = append(codes, "REVIEWED_SUITABILITY")
				phrases = append(phrases, "审核场地资料标记适合该活动类别")
			}
			needed := c.MinParticipants
			if c.MaxParticipants > needed {
				needed = c.MaxParticipants
			}
			if needed > 0 {
				if v.Capacity == nil || *v.Capacity < needed {
					continue
				}
				codes = append(codes, "REVIEWED_CAPACITY")
				phrases = append(phrases, "审核容量满足你填写的人数")
			}
			capacity = v.Capacity
			reservation = v.ReservationSupport
			expiry = &v.ExpiresAt
			if c.PlaceID == "" {
				rank = 2
			}
		} else {
			if c.Category != "" || c.MinParticipants > 0 || c.MaxParticipants > 0 {
				continue
			}
			codes = append(codes, "VENUE_UNKNOWN")
			phrases = append(phrases, "该地点的举办条件尚未核验")
		}
		var profile *pp.Public
		semanticScore := 0
		candidate := item.Profile
		if candidate != nil && candidate.PlaceID == p.ID && candidate.CityID == p.CityID && pp.ValidatePublic(*candidate, now) == nil {
			copy := *candidate
			profile = &copy
			if c.Category != "" {
				for _, codeset := range [][]string{candidate.Facts.GoodFor, candidate.Facts.Suitability} {
					for _, code := range codeset {
						if code == c.Category {
							semanticScore += 2
							break
						}
					}
				}
				if semanticScore > 0 {
					codes = append(codes, "REVIEWED_SEMANTIC_CATEGORY")
					phrases = append(phrases, "公开审核声明与你填写的活动类别相符（审核判断，非概率）")
				}
			}
			neededMin, neededMax := c.MinParticipants, c.MaxParticipants
			if neededMin == 0 {
				neededMin = neededMax
			}
			if neededMax == 0 {
				neededMax = neededMin
			}
			if g := candidate.Facts.GroupSize; g != nil && neededMin > 0 && g.Min <= neededMin && g.Max >= neededMax {
				semanticScore += 3
				codes = append(codes, "REVIEWED_SEMANTIC_GROUP_SIZE")
				phrases = append(phrases, "人数位于公开声明的适合范围；不代表实时可用或已预约")
			}
		}
		ref := EntityRef{Type: "PLACE", ID: p.ID}
		out = append(out, Match{ID: intent.ID + ":" + p.ID, IntentID: intent.ID, Place: ref,
			HasReviewedVenue: v != nil, Capacity: capacity, ReservationSupport: reservation,
			ReasonCodes: codes, Reason: strings.Join(phrases, "；"),
			Action: Action{Type: "OPEN_PLACE", Target: ref}, RuleVersion: RuleVersion,
			Rank: rank, SourceExpiresAt: expiry, SemanticScore: semanticScore, SemanticProfile: profile})
		seen[p.ID] = true
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		if out[i].SemanticScore != out[j].SemanticScore {
			return out[i].SemanticScore > out[j].SemanticScore
		}
		return out[i].Place.ID < out[j].Place.ID
	})
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

func contains(haystack, needle string) bool {
	return haystack != "" && strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
