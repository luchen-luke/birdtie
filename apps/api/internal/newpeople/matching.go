package newpeople

import (
	"sort"
	"strings"
)

const RuleVersion = "v1"

// Match compares only explicit, policy-approved intent attributes. Database
// eligibility is a separate required gate, including when sending an invite.
func Match(source, peer Signal) (Candidate, bool) {
	category := normalizeText(source.Category)
	modality := strings.ToUpper(strings.TrimSpace(source.Modality))
	if strings.TrimSpace(source.IntentID) == "" || strings.TrimSpace(peer.IntentID) == "" ||
		strings.TrimSpace(source.AccountID) == "" || strings.TrimSpace(peer.AccountID) == "" ||
		normalizeID(source.AccountID) == normalizeID(peer.AccountID) ||
		category == "" || category != normalizeText(peer.Category) ||
		modality != strings.ToUpper(strings.TrimSpace(peer.Modality)) ||
		!rangesOverlap(source, peer) {
		return Candidate{}, false
	}

	candidate := Candidate{
		SourceIntentID:    strings.TrimSpace(source.IntentID),
		CandidateIntentID: strings.TrimSpace(peer.IntentID),
		AccountID:         strings.TrimSpace(peer.AccountID),
		DisplayName:       strings.TrimSpace(peer.DisplayName),
		Category:          category,
		Modality:          modality,
		ReasonCodes:       []string{"CATEGORY_EQUAL", "MODALITY_EQUAL"},
		Reasons:           []string{"双方填写的类别相同", "双方选择的参与方式相同"},
	}
	appendReason := func(code, reason string) {
		candidate.ReasonCodes = append(candidate.ReasonCodes, code)
		candidate.Reasons = append(candidate.Reasons, reason)
	}

	switch modality {
	case "ONLINE":
		if source.PlaceID != "" || peer.PlaceID != "" ||
			normalizeText(source.AreaLabel) != "" || normalizeText(peer.AreaLabel) != "" ||
			!onlineCompatible(source, peer) {
			return Candidate{}, false
		}
	case "IN_PERSON", "HYBRID":
		code, reason, ok := physicalCompatibility(source, peer)
		if !ok || (modality == "IN_PERSON" &&
			(normalizeText(source.OnlinePlatform) != "" || normalizeText(peer.OnlinePlatform) != "")) {
			return Candidate{}, false
		}
		appendReason(code, reason)
		if modality == "HYBRID" && (normalizeText(source.OnlinePlatform) == "" ||
			normalizeText(peer.OnlinePlatform) == "" || !onlineCompatible(source, peer)) {
			return Candidate{}, false
		}
	default:
		return Candidate{}, false
	}
	if normalizeText(source.OnlinePlatform) != "" {
		appendReason("ONLINE_PLATFORM_EQUAL", "双方填写的线上平台相同")
	}
	if source.MinParticipants > 0 || source.MaxParticipants > 0 ||
		peer.MinParticipants > 0 || peer.MaxParticipants > 0 {
		appendReason("PARTICIPANT_RANGE_COMPATIBLE", "已填写的人数范围没有冲突")
	}
	return candidate, true
}

func physicalCompatibility(source, peer Signal) (string, string, bool) {
	sourcePlace, peerPlace := normalizeID(source.PlaceID), normalizeID(peer.PlaceID)
	if sourcePlace != "" || peerPlace != "" {
		// An explicit Place is a hard constraint. A region cannot establish
		// that another declaration includes that Place.
		if sourcePlace != "" && sourcePlace == peerPlace {
			return "PLACE_EQUAL", "双方明确选择了同一地点", true
		}
		return "", "", false
	}
	sourceCity, peerCity := normalizeID(source.CityID), normalizeID(peer.CityID)
	sourceArea, peerArea := normalizeText(source.AreaLabel), normalizeText(peer.AreaLabel)
	if sourceCity != "" && sourceCity == peerCity && sourceArea != "" && sourceArea == peerArea {
		return "CITY_AREA_EQUAL", "双方选择的城市及填写的粗区域相同，不代表距离或所在地", true
	}
	return "", "", false
}

func onlineCompatible(source, peer Signal) bool {
	// Missing platform is unknown. It cannot satisfy the other's explicit
	// platform requirement; two omitted platforms impose no platform constraint.
	return normalizeText(source.OnlinePlatform) == normalizeText(peer.OnlinePlatform)
}

func rangesOverlap(source, peer Signal) bool {
	bounds := func(signal Signal) (int, int, bool) {
		if signal.MinParticipants < 0 || signal.MaxParticipants < 0 ||
			signal.MinParticipants > 100 || signal.MaxParticipants > 100 ||
			(signal.MinParticipants > 0 && signal.MaxParticipants > 0 &&
				signal.MinParticipants > signal.MaxParticipants) {
			return 0, 0, false
		}
		max := signal.MaxParticipants
		if max == 0 {
			max = 100 // The validated schema supports at most 100 participants.
		}
		return signal.MinParticipants, max, true
	}
	// Keep unknown bounds unconstrained within the schema's supported domain.
	sMin, sMax, validSource := bounds(source)
	pMin, pMax, validPeer := bounds(peer)
	return validSource && validPeer && sMin <= pMax && pMin <= sMax
}

func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// BuildResponse returns one stable candidate per Person. More than 50 unique
// compatible People is reported as truncation; duplicates do not count twice.
func BuildResponse(source Signal, peers []Signal) Response {
	response := Response{Source: "RULE_BASED", RuleVersion: RuleVersion,
		SourceIntentID: strings.TrimSpace(source.IntentID), Candidates: []Candidate{}}
	matched := []Candidate{}
	for _, peer := range peers {
		if candidate, ok := Match(source, peer); ok {
			matched = append(matched, candidate)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if normalizeID(matched[i].AccountID) != normalizeID(matched[j].AccountID) {
			return normalizeID(matched[i].AccountID) < normalizeID(matched[j].AccountID)
		}
		if normalizeID(matched[i].CandidateIntentID) != normalizeID(matched[j].CandidateIntentID) {
			return normalizeID(matched[i].CandidateIntentID) < normalizeID(matched[j].CandidateIntentID)
		}
		return matched[i].DisplayName < matched[j].DisplayName
	})
	seen := map[string]bool{}
	for _, candidate := range matched {
		key := normalizeID(candidate.AccountID)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(response.Candidates) == 50 {
			response.Truncated = true
			break
		}
		response.Candidates = append(response.Candidates, candidate)
	}
	return response
}
