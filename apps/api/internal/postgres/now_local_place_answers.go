package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
)

var _ agentworkspace.LocalPlaceReplyPort = (*Store)(nil)

const localObjectivePlaceFields = "objective_fields"

// Classification only: a successful native source read still precedes this
// check. A literal name cannot authorize paid inference when that current read
// contains no matching public Place. Pronouns, recommendations and category
// refinements retain their separate existing resolvers and empty-result paths.
func localNamedFactHasNoCurrentPlace(query string, places []foundation.Place) bool {
	if len(places) != 0 {
		return false
	}
	question, ok := agentworkspace.ParseLocalPlaceQuestion(query)
	return ok && question.Name != "" && !question.Recommendation &&
		(question.Field == agentworkspace.PlaceAddress || question.Field == agentworkspace.PlaceOpeningHours)
}

func currentLocalPlaceQuestion(t agentworkspace.Task) (agentworkspace.LocalPlaceQuestion, bool) {
	if _, ok := agentworkspace.ParseObjectivePlaceFields(t.Filters["currentQuery"]); ok && t.Intent == agentworkspace.FindPlace && t.Status == agentworkspace.TaskActive && t.ContextType == "CITY" {
		intent := agentworkspace.ParseMVPIntent(t.Filters["currentQuery"], &t)
		if intent.Supported && intent.Category == t.Filters["category"] && t.Filters["searchTerm"] == "" {
			return agentworkspace.LocalPlaceQuestion{Field: localObjectivePlaceFields}, true
		}
	}
	q, ok := agentworkspace.ParseLocalPlaceQuestion(t.Filters["currentQuery"])
	if ok {
		return q, true
	}
	field := ""
	if agentworkspace.IsLocalPlaceHoursFollowupQuery(t.Filters["currentQuery"]) {
		field = agentworkspace.PlaceOpeningHours
	} else if agentworkspace.IsLocalPlaceAddressFollowupQuery(t.Filters["currentQuery"]) {
		field = agentworkspace.PlaceAddress
	}
	if field != "" {
		if t.Intent == agentworkspace.FindPlace && t.Status == agentworkspace.TaskActive &&
			t.ContextType == "CITY" && t.CityID != "" && t.Filters["targetIntent"] == agentworkspace.FindPlace &&
			t.Filters[agentworkspace.PlaceFollowupTopicFilter] == field &&
			egressUUID(t.Filters[agentworkspace.PlaceFollowupIDFilter]) && livePlaceGeneration(t.Filters[agentworkspace.PlaceFollowupGenerationFilter]) &&
			agentworkspace.ValidPlaceFollowupName(t.Filters["searchTerm"]) && len(t.Conversation) > 0 &&
			t.Conversation[len(t.Conversation)-1].Role == "user" && t.Conversation[len(t.Conversation)-1].Text == t.Filters["currentQuery"] {
			return agentworkspace.LocalPlaceQuestion{Name: t.Filters["searchTerm"], Field: field}, true
		}
		return q, false
	}
	return q, false
}

func localPlaceHoursSourceMatches(place nativeResolvedPublicPlace, source arp.Receipt) bool {
	return place.valid && len(source.Places) == 1 && len(source.Items) == 1 &&
		source.Places[0].ID == place.id && source.Places[0].Name == place.name &&
		source.Items[0].Entity == (arp.Ref{Type: "place", ID: place.id}) && source.Items[0].Title == place.name
}

func localFieldsForPlace(p foundation.Place, fields []cityseed.PlaceFieldEvidence) (map[string]cityseed.PlaceFieldEvidence, bool) {
	by := map[string]cityseed.PlaceFieldEvidence{}
	for _, f := range fields {
		if existing, duplicate := by[f.FieldKey]; duplicate {
			if string(existing.Value) != string(f.Value) {
				return nil, false
			}
			continue
		}
		by[f.FieldKey] = f
	}
	var name, address string
	if json.Unmarshal(by["name"].Value, &name) != nil || name != p.Name {
		return nil, false
	}
	if p.AddressLabel != "" && (json.Unmarshal(by["address"].Value, &address) != nil || address != p.AddressLabel) {
		return nil, false
	}
	if p.Location.Precision == "point" {
		var coords struct {
			CoordinateSystem    string
			Latitude, Longitude float64
		}
		if json.Unmarshal(by["coordinates"].Value, &coords) != nil || p.Location.Latitude == nil || p.Location.Longitude == nil || coords.CoordinateSystem != "wgs84" || coords.Latitude != *p.Location.Latitude || coords.Longitude != *p.Location.Longitude {
			return nil, false
		}
	}
	return by, true
}

// A retained test/development source can have valid rights for its own fixture.
// Those rights do not make it provenance for a real-place recommendation. This
// is an additional exclusion, never a replacement for independent field review.
func localRecommendationPublicSource(raw string) bool {
	if !cityseed.SourceURLValid(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if net.ParseIP(host) != nil {
		return false
	}
	for _, reserved := range []string{"example", "test", "invalid", "localhost", "local", "internal", "home.arpa", "example.com", "example.net", "example.org"} {
		if host == reserved || strings.HasSuffix(host, "."+reserved) {
			return false
		}
	}
	return strings.Contains(host, ".")
}

func localRecommendationFieldsUsable(place foundation.Place, fields []cityseed.PlaceFieldEvidence, at time.Time) bool {
	if at.IsZero() || !localRecommendationPublicSource(place.Source.Reference) || len(fields) == 0 {
		return false
	}
	if _, valid := localFieldsForPlace(place, fields); !valid {
		return false
	}
	for _, field := range fields {
		permission := field.Permission
		if field.PlaceID != place.ID || field.ID == "" || field.Version < 1 || field.RowToken == "" ||
			field.PermissionID != permission.ID || field.PermissionVersion != permission.Version || field.SourceCandidateID != permission.CandidateID ||
			!cityseed.PermissionUsable(permission, at) || !cityseed.ValidPlaceFieldValue(field.FieldKey, field.Value) ||
			!localRecommendationPublicSource(permission.SourceURL) || !localRecommendationPublicSource(permission.LicenseURL) ||
			field.ConflictState != "NONE" || field.ObservedAt.IsZero() || field.ObservedAt.After(at) ||
			field.VerifiedAt.IsZero() || field.VerifiedAt.Before(field.ObservedAt) || field.VerifiedAt.After(at) ||
			field.ValidFrom.IsZero() || field.ValidFrom.After(at) || !field.ValidTo.After(at) || !field.RefreshDue.After(at) {
			return false
		}
	}
	return true
}

func localFoodFacts(fields map[string]cityseed.PlaceFieldEvidence) acb.LocalFoodFacts {
	value := func(key string) string {
		f, ok := fields[key]
		if !ok {
			return "UNKNOWN"
		}
		if string(f.Value) != "true" && string(f.Value) != "false" {
			return "UNKNOWN"
		}
		var b bool
		if json.Unmarshal(f.Value, &b) != nil {
			return "UNKNOWN"
		}
		if b {
			return "yes"
		}
		return "no"
	}
	return acb.LocalFoodFacts{Seafood: value("food_seafood"), Spicy: value("food_spicy")}
}

// Hours remain the independently reviewed source's literal record. This does
// not interpret a timezone, holiday exception or whether the venue is open now.
func localPlaceHoursText(place foundation.Place, fields []cityseed.PlaceFieldEvidence, now time.Time) (string, error) {
	var hours string
	var verified, refresh time.Time
	found := false
	for _, f := range fields {
		if f.FieldKey != agentworkspace.PlaceOpeningHours {
			continue
		}
		var value string
		if f.PlaceID != place.ID || f.Version < 1 || f.ID == "" || f.RowToken == "" ||
			!cityseed.ValidPlaceFieldValue(f.FieldKey, f.Value) || json.Unmarshal(f.Value, &value) != nil ||
			f.PermissionID != f.Permission.ID || f.PermissionVersion != f.Permission.Version ||
			f.SourceCandidateID != f.Permission.CandidateID || !cityseed.PermissionUsable(f.Permission, now) ||
			f.ConflictState != "NONE" || f.ObservedAt.IsZero() || f.ObservedAt.After(now) ||
			f.VerifiedAt.IsZero() || f.VerifiedAt.Before(f.ObservedAt) || f.VerifiedAt.After(now) ||
			f.ValidFrom.IsZero() || f.ValidFrom.After(now) || !f.ValidTo.After(now) || !f.RefreshDue.After(now) ||
			(found && value != hours) {
			return "", agentworkspace.ErrLocalPlaceHoursUnavailable
		}
		found, hours = true, value
		if f.VerifiedAt.After(verified) {
			verified = f.VerifiedAt
		}
		refresh = retryMinimum(refresh, retryMinimum(f.RefreshDue, retryMinimum(f.ValidTo, f.Permission.RetentionUntil)))
	}
	if !found || now.IsZero() || place.Name == "" {
		return "", agentworkspace.ErrLocalPlaceHoursUnavailable
	}
	return fmt.Sprintf("%s的营业时间记录：“%s”。该字段最近核验于%s；需在%s前刷新，不代表当前开门状态", place.Name, hours, verified.UTC().Format(time.RFC3339), refresh.UTC().Format(time.RFC3339)), nil
}

func localFieldDigest(fields []cityseed.PlaceFieldEvidence, uses []acb.LocalFoodPreference) string {
	copyFields := append([]cityseed.PlaceFieldEvidence(nil), fields...)
	sort.Slice(copyFields, func(i, j int) bool { return copyFields[i].ID < copyFields[j].ID })
	type proofField struct {
		Field    cityseed.PlaceFieldEvidence
		RowToken string
	}
	proof := []proofField{}
	for _, f := range copyFields {
		proof = append(proof, proofField{f, f.RowToken})
	}
	raw, _ := json.Marshal(struct {
		Fields []proofField
		Memory []acb.LocalFoodPreference
	}{proof, uses})
	return liveDigestBytes("birdtie.local-place-fields.v1", raw)
}

type localPlaceCandidate struct {
	place    foundation.Place
	score    int
	excluded bool
	fields   []cityseed.PlaceFieldEvidence
	uses     []acb.LocalFoodPreference
	factText string
}

// Keep every admitted place's complete native evidence within the existing
// answer limits. A whole place is omitted if its sources or facts cannot fit;
// its card must never survive after one of its required sources was discarded.
func boundedLocalPlaceCandidates(input []localPlaceCandidate) ([]localPlaceCandidate, bool) {
	candidates := append([]localPlaceCandidate(nil), input...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].excluded != candidates[j].excluded {
			return !candidates[i].excluded
		}
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].place.Name != candidates[j].place.Name {
			return candidates[i].place.Name < candidates[j].place.Name
		}
		return candidates[i].place.ID < candidates[j].place.ID
	})
	selected := []localPlaceCandidate{}
	sources := map[string]bool{}
	factCount := 0
	for _, candidate := range candidates {
		newSources := map[string]bool{}
		valid := candidate.place.ID != "" && len(candidate.fields) > 0
		for _, field := range candidate.fields {
			url := field.Permission.SourceURL
			if field.PlaceID != candidate.place.ID || url == "" {
				valid = false
			}
			if !sources[url] {
				newSources[url] = true
			}
		}
		if !valid || len(sources)+len(newSources) > 10 || factCount+len(candidate.fields) > 100 {
			continue
		}
		selected = append(selected, candidate)
		factCount += len(candidate.fields)
		for url := range newSources {
			sources[url] = true
		}
	}
	return selected, len(selected) < len(input)
}

func localPlaceCandidateEvidence(candidates []localPlaceCandidate) ([]cityseed.PlaceFieldEvidence, []acb.LocalFoodPreference) {
	fields := []cityseed.PlaceFieldEvidence{}
	uses := []acb.LocalFoodPreference{}
	seenUses := map[string]bool{}
	for _, candidate := range candidates {
		fields = append(fields, candidate.fields...)
		for _, use := range candidate.uses {
			if !seenUses[use.MemoryID] {
				uses = append(uses, use)
				seenUses[use.MemoryID] = true
			}
		}
	}
	return fields, uses
}

func localPlaceRecommendationText(parts []string, hasUses, bounded bool) string {
	message := ""
	if len(parts) == 0 {
		message = "本次核验的餐饮地点中，没有符合已确认口味的结果。"
	} else if hasUses {
		message = "已按你确认的口味筛选和排序：" + strings.Join(parts, "、") + "。"
	} else {
		message = "当前站内可供选择的餐饮地点：" + strings.Join(parts, "、") + "。"
	}
	if bounded {
		message = "本次展示已核验的地点子集。" + message
	}
	return message
}

// This branch answers only sufficient, independently permitted local facts.
// No Run, provider reservation, search, model or shared answer cache is made.
func (s *Store) TryOwnLocalPlaceReply(ctx context.Context, a arp.Access) (agentworkspace.LiveReply, bool, error) {
	if !a.Valid() {
		return nil, false, arp.ErrDenied
	}
	tx, b, p, e := s.beginCurrentToolRead(ctx, a.Actor, a.SessionDigest, resultProjectionRelations+`;LOCK TABLE place_source_permissions,place_field_evidence,agent_memories IN ACCESS SHARE MODE`)
	if e != nil {
		return nil, false, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL jit=off`); e != nil {
		return nil, false, arp.ErrUnavailable
	}
	t, e := ownReplyTaskTx(ctx, tx, a, true)
	if e != nil {
		return nil, false, e
	}
	question, ok := currentLocalPlaceQuestion(t)
	if !ok || t.Intent != agentworkspace.FindPlace {
		return nil, false, nil
	}
	boundFactPlace := nativeResolvedPublicPlace{}
	if (question.Field == agentworkspace.PlaceOpeningHours && agentworkspace.IsLocalPlaceHoursFollowupQuery(t.Filters["currentQuery"])) ||
		(question.Field == agentworkspace.PlaceAddress && agentworkspace.IsLocalPlaceAddressFollowupQuery(t.Filters["currentQuery"])) {
		// The grammar and retained scalar filters are not a grant. Reuse the
		// original selected entity's live city/name/generation proof in this tx.
		boundFactPlace, _, e = nativeResolvedPlaceFromTaskTx(ctx, tx, t, p.until)
		if e != nil || !boundFactPlace.valid || boundFactPlace.name != question.Name {
			return nil, false, agentworkspace.ErrPlaceFollowupChanged
		}
	}
	q, source, e := s.captureReplyCurrentSourceTx(ctx, tx, a, t, p)
	if e != nil {
		return nil, false, e
	}
	if boundFactPlace.valid && !localPlaceHoursSourceMatches(boundFactPlace, source) {
		return nil, false, agentworkspace.ErrPlaceFollowupChanged
	}
	objectiveFields, objective := agentworkspace.ParseObjectivePlaceFields(t.Filters["currentQuery"])
	objective = objective && question.Field == localObjectivePlaceFields
	if localNamedFactHasNoCurrentPlace(t.Filters["currentQuery"], source.Places) {
		return nil, false, agentworkspace.ErrLocalPlaceFactsUnavailable
	}
	if !question.Recommendation && !objective && len(source.Places) != 1 {
		return nil, false, nil
	}
	if len(source.Places) == 0 {
		return nil, false, nil
	}
	preferences := []acb.LocalFoodPreference{}
	if question.Recommendation {
		preferences, e = contextBuilderLocalFoodTx(ctx, tx, b, t, source.ObservedAt)
		if e != nil {
			return nil, false, e
		}
	}
	candidates := []localPlaceCandidate{}
	recommendationSourcesOmitted := false
	for _, place := range source.Places {
		keys := []string{"name", "address", "coordinates"}
		if question.Recommendation {
			keys = append(keys, "food_seafood", "food_spicy")
		} else if question.Field == agentworkspace.PlaceOpeningHours {
			keys = append(keys, agentworkspace.PlaceOpeningHours)
		} else if objective {
			for _, field := range objectiveFields {
				if field == agentworkspace.PlaceOpeningHours {
					keys = append(keys, field)
				}
			}
		}
		fields, err := ReadPlaceFieldEvidenceTx(ctx, tx, place.ID, keys, source.ObservedAt)
		if err != nil {
			return nil, false, err
		}
		if question.Recommendation && !localRecommendationFieldsUsable(place, fields, source.ObservedAt) {
			// Omit the whole entity before scoring or collecting Memory uses.
			// Its fixture fields must not consume the real answer's source budget.
			recommendationSourcesOmitted = true
			continue
		}
		if objective && !localRecommendationFieldsUsable(place, fields, source.ObservedAt) {
			// A field listing consumes the same independently permitted public
			// evidence as a local answer, never a native row without provenance.
			return nil, false, agentworkspace.ErrLocalPlaceFactsUnavailable
		}
		by, valid := localFieldsForPlace(place, fields)
		if !valid {
			return nil, false, nil
		} // Missing license/field is not silently treated as sufficient.
		if question.Field == agentworkspace.PlaceAddress && place.AddressLabel == "" {
			return nil, false, nil
		}
		factText := ""
		if question.Field == agentworkspace.PlaceOpeningHours {
			factText, err = localPlaceHoursText(place, fields, source.ObservedAt)
			if err != nil {
				// A native identity does not fill in missing/conflicting/stale hours
				// and must not silently start a supplier call to disguise that gap.
				return nil, false, err
			}
		}
		score, excluded, used := 0, false, []acb.LocalFoodPreference{}
		if question.Recommendation {
			score, excluded, used = acb.ScoreLocalFood(localFoodFacts(by), preferences)
			// A hard exclusion requires the relevant permitted field for EVERY
			// candidate; unknown spice cannot be described as safe to eat.
			for _, pref := range preferences {
				if pref.Value == "DISLIKE" {
					key := map[string]string{"seafood": "food_seafood", "spicy": "food_spicy"}[pref.Topic]
					if _, known := by[key]; !known {
						return nil, false, nil
					}
				}
			}
		}
		candidates = append(candidates, localPlaceCandidate{place: place, score: score, excluded: excluded, fields: fields, uses: used, factText: factText})
	}
	if question.Recommendation && recommendationSourcesOmitted && len(candidates) == 0 {
		// No sufficient real-place provenance is not an empty recommendation,
		// and must not fall through to a rule count or an external provider.
		return nil, false, agentworkspace.ErrLocalPlaceFactsUnavailable
	}
	candidates, bounded := boundedLocalPlaceCandidates(candidates)
	bounded = bounded || recommendationSourcesOmitted
	if objective {
		order := map[string]int{}
		for i, place := range source.Places {
			order[place.ID] = i
		}
		sort.SliceStable(candidates, func(i, j int) bool { return order[candidates[i].place.ID] < order[candidates[j].place.ID] })
	}
	if len(candidates) == 0 {
		return nil, false, arp.ErrChanged
	}
	if objective {
		// Keep the complete card/evidence set inside the existing sourced-answer
		// text bound. Omit a whole Place rather than clipping an address or hours.
		boundedCandidates := []localPlaceCandidate{}
		previewSources := map[string]string{}
		textBytes := 0
		for _, candidate := range candidates {
			for _, field := range candidate.fields {
				if previewSources[field.Permission.SourceURL] == "" {
					previewSources[field.Permission.SourceURL] = fmt.Sprintf("source-%d", len(previewSources)+1)
				}
			}
			text, err := localObjectivePlaceText(candidate.place, candidate.fields, objectiveFields, previewSources)
			if err != nil {
				return nil, false, err
			}
			if textBytes+len(text)+1 > 6500 {
				bounded = true
				continue
			}
			textBytes += len(text) + 1
			boundedCandidates = append(boundedCandidates, candidate)
		}
		candidates = boundedCandidates
		if len(candidates) == 0 {
			return nil, false, agentworkspace.ErrLocalPlaceFactsUnavailable
		}
	}
	allFields, uses := localPlaceCandidateEvidence(candidates)
	selected := []foundation.Place{}
	items := []arp.Item{}
	parts := []string{}
	sources := []agentworkspace.AnswerSource{}
	sourceIDs := map[string]string{}
	for _, f := range allFields {
		if _, present := sourceIDs[f.Permission.SourceURL]; !present {
			id := fmt.Sprintf("source-%d", len(sources)+1)
			sourceIDs[f.Permission.SourceURL] = id
			sources = append(sources, agentworkspace.AnswerSource{ID: id, Title: f.Permission.Attribution, URL: f.Permission.SourceURL})
		}
	}
	for _, c := range candidates {
		if c.excluded {
			continue
		}
		selected = append(selected, c.place)
		for _, item := range source.Items {
			if item.Entity == (arp.Ref{Type: "place", ID: c.place.ID}) {
				items = append(items, item)
			}
		}
		text := c.place.Name
		if question.Field == agentworkspace.PlaceAddress {
			text += "的地址是：" + c.place.AddressLabel
		}
		if question.Field == agentworkspace.PlaceOpeningHours {
			text = c.factText
		}
		if objective {
			text, e = localObjectivePlaceText(c.place, c.fields, objectiveFields, sourceIDs)
			if e != nil {
				return nil, false, e
			}
			parts = append(parts, text)
		} else {
			parts = append(parts, text+localPlaceFieldCitations(c.fields, sourceIDs, question.Field, question.Recommendation))
		}
	}
	message := strings.Join(parts, "；") + "。"
	if question.Recommendation {
		message = localPlaceRecommendationText(parts, len(uses) > 0, bounded)
	} else if objective {
		message = "已按当前来源整理地点资料：\n" + strings.Join(parts, "\n") + "\n营业时间为来源原始记录，不代表现在开门；未提供的字段明确保留未知。"
		if bounded {
			message += "本轮显示资料来源完整的地点子集。"
		}
	}
	until := source.ValidUntil
	factRefs := []agentworkspace.LocalPlaceFactRef{}
	for _, f := range allFields {
		for _, deadline := range []time.Time{f.ValidTo, f.RefreshDue, f.Permission.RetentionUntil} {
			until = retryMinimum(until, deadline)
		}
		factRefs = append(factRefs, agentworkspace.LocalPlaceFactRef{EvidenceID: f.ID, PlaceID: f.PlaceID, Field: f.FieldKey, Version: f.Version, PermissionID: f.PermissionID})
	}
	if len(sources) == 0 || len(sources) > 10 || len(factRefs) > 100 || !until.After(time.Now().UTC()) {
		return nil, false, arp.ErrChanged
	}
	sort.Slice(uses, func(i, j int) bool { return uses[i].MemoryID < uses[j].MemoryID })
	evidence := &agentworkspace.LocalPlaceReplyEvidence{Schema: agentworkspace.LocalPlaceReplySchema, ID: fmt.Sprintf("local-native:%s:%d", t.ID, len(t.Conversation)), SourceEvidenceDigest: localFieldDigest(allFields, uses), ValidUntil: until, Facts: factRefs, MemoryUses: uses, Reason: "LOCAL_FIELDS_SUFFICIENT", SearchCalls: 0, ModelCalls: 0}
	if len(selected) == 0 {
		message += localPlaceCitations(allFields, sourceIDs, true)
	}
	_, validatedUntil, checkErr := validateLocalReplyEvidenceTx(ctx, tx, b, agentworkspace.Message{SourceEvidenceDigest: evidence.SourceEvidenceDigest, LocalReplyEvidence: evidence}, time.Now().UTC())
	if checkErr != nil {
		return nil, false, checkErr
	}
	until = retryMinimum(until, validatedUntil)
	evidence.ValidUntil = until
	source.Items, source.Places = items, selected
	t, h, e := s.persistReplyMembershipTx(ctx, tx, a, t, p, q.Kind, source, agentworkspace.Message{Role: "assistant", Text: message, Sources: sources, SourceEvidenceDigest: evidence.SourceEvidenceDigest, LocalReplyEvidence: evidence})
	if e != nil {
		return nil, false, e
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(t))
	if e != nil {
		return nil, false, arp.ErrUnavailable
	}
	a.ExpectedTask = raw
	if _, _, e = validateLocalReplyEvidenceTx(ctx, tx, b, t.Conversation[len(t.Conversation)-1], time.Now().UTC()); e != nil {
		return nil, false, e
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return nil, false, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return nil, false, arp.ErrUnavailable
	}
	if !until.After(time.Now().UTC()) || !nativeReplyReadUnexpired(h.state(), h.state().until, time.Now()) {
		return nil, false, arp.ErrChanged
	}
	state := &nativeLocalPlaceReply{store: s, access: a, task: t, history: h, until: until}
	return state, true, nil
}

// Literal evidence remains the answer, so an existing postcode cannot be
// described as missing by a model. Each displayed field cites its own source;
// absent hours/address are unknown rather than derived from names or location.
func localObjectivePlaceText(place foundation.Place, fields []cityseed.PlaceFieldEvidence, requested []string, sources map[string]string) (string, error) {
	by, valid := localFieldsForPlace(place, fields)
	if !valid || len(requested) == 0 || len(requested) > 4 {
		return "", agentworkspace.ErrLocalPlaceFactsUnavailable
	}
	parts := []string{}
	for _, key := range requested {
		label, value := "", ""
		switch key {
		case "name":
			label, value = "名称", place.Name
		case "address":
			label, value = "地址", place.AddressLabel
		case "coordinates":
			label = "坐标"
			if place.Location.Precision == "point" && place.Location.Latitude != nil && place.Location.Longitude != nil && place.Location.CoordinateSystem == "wgs84" {
				value = fmt.Sprintf("WGS84 %g, %g", *place.Location.Latitude, *place.Location.Longitude)
			}
		case agentworkspace.PlaceOpeningHours:
			label = "营业时间记录"
			if f, ok := by[key]; ok && json.Unmarshal(f.Value, &value) != nil {
				return "", agentworkspace.ErrLocalPlaceFactsUnavailable
			}
		default:
			return "", agentworkspace.ErrLocalPlaceFactsUnavailable
		}
		if value == "" {
			parts = append(parts, label+"：来源未提供")
			continue
		}
		citation := localPlaceFieldCitations(fields, sources, key, false)
		if citation == "" {
			return "", agentworkspace.ErrLocalPlaceFactsUnavailable
		}
		parts = append(parts, label+"："+value+citation)
	}
	return place.Name + " — " + strings.Join(parts, "；"), nil
}

type nativeLocalPlaceReply struct {
	store   *Store
	access  arp.Access
	task    agentworkspace.Task
	history *nativeMessageResultsRead
	until   time.Time
}

func (*nativeLocalPlaceReply) MarshalJSON() ([]byte, error) { return nil, agenttool.ErrServerOnly }
func (h *nativeLocalPlaceReply) UnmarshalJSON([]byte) error {
	if h != nil {
		*h = nativeLocalPlaceReply{}
	}
	return agenttool.ErrServerOnly
}
func (*nativeLocalPlaceReply) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("native local place reply"))
}

func (h *nativeLocalPlaceReply) Task() agentworkspace.Task {
	raw, _ := json.Marshal(h.task)
	var t agentworkspace.Task
	_ = json.Unmarshal(raw, &t)
	return t
}
func (h *nativeLocalPlaceReply) Answer(request string) (*agentworkspace.SourcedAnswer, error) {
	t := agentworkspace.SanitizeTaskForResponse(h.task)
	m := t.Conversation[len(t.Conversation)-1]
	query, e := agentworkspace.SourcedAnswerQueryDigest(t)
	if e != nil {
		return nil, e
	}
	digest, e := agentworkspace.SourcedAnswerTaskDigest(t)
	if e != nil {
		return nil, e
	}
	return agentworkspace.NewSourcedAnswer(agentworkspace.SourcedAnswerBinding{TaskID: t.ID, RequestID: request, CurrentQueryDigest: query, TaskSnapshotDigest: digest, SourceEvidenceDigest: m.SourceEvidenceDigest, RunID: m.LocalReplyEvidence.ID, GeneratedAt: t.UpdatedAt, ValidUntil: h.until}, m.Text, m.Sources)
}
func (h *nativeLocalPlaceReply) Revalidate(ctx context.Context) error {
	if h == nil || !h.until.After(time.Now().UTC()) {
		return arp.ErrChanged
	}
	if e := h.history.Revalidate(ctx); e != nil {
		return e
	}
	tx, b, _, e := h.store.beginCurrentToolRead(ctx, h.access.Actor, h.access.SessionDigest, resultProjectionRelations+`;LOCK TABLE place_source_permissions,place_field_evidence,agent_memories IN ACCESS SHARE MODE`)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	t, e := ownReplyTaskTx(ctx, tx, h.access, false)
	if e != nil {
		return e
	}
	if _, _, e = validateLocalReplyEvidenceTx(ctx, tx, b, t.Conversation[len(t.Conversation)-1], time.Now().UTC()); e != nil {
		return e
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return arp.ErrUnavailable
	}
	if !h.until.After(time.Now().UTC()) || !nativeReplyReadUnexpired(h.history.state(), h.history.state().until, time.Now()) {
		return arp.ErrChanged
	}
	return nil
}

func localPlaceCitations(fields []cityseed.PlaceFieldEvidence, sourceIDs map[string]string, recommendation bool) string {
	return localPlaceFieldCitations(fields, sourceIDs, agentworkspace.PlaceAddress, recommendation)
}

func localPlaceFieldCitations(fields []cityseed.PlaceFieldEvidence, sourceIDs map[string]string, field string, recommendation bool) string {
	ids := []string{}
	seen := map[string]bool{}
	for _, f := range fields {
		if !recommendation && f.FieldKey != "name" && f.FieldKey != field {
			continue
		}
		id := sourceIDs[f.Permission.SourceURL]
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, "["+id+"]")
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return " " + strings.Join(ids, " ")
}

func validateLocalReplyEvidenceTx(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, m agentworkspace.Message, now time.Time) ([]cityseed.PlaceFieldEvidence, time.Time, error) {
	evidence := m.LocalReplyEvidence
	if evidence == nil || evidence.Schema != agentworkspace.LocalPlaceReplySchema || evidence.ID == "" || evidence.SourceEvidenceDigest != m.SourceEvidenceDigest || len(evidence.Facts) == 0 || len(evidence.Facts) > 100 || len(evidence.MemoryUses) > 2 || evidence.SearchCalls != 0 || evidence.ModelCalls != 0 {
		return nil, time.Time{}, arp.ErrChanged
	}
	// Persisted observation deadlines do not become new grants on history GET.
	// Fields are reread now; the process-local handle retains its fixed lease.
	fields := []cityseed.PlaceFieldEvidence{}
	until := time.Time{}
	seen := map[string]bool{}
	for _, ref := range evidence.Facts {
		if seen[ref.EvidenceID] {
			return nil, time.Time{}, arp.ErrChanged
		}
		seen[ref.EvidenceID] = true
		rows, e := ReadPlaceFieldEvidenceTx(ctx, tx, ref.PlaceID, []string{ref.Field}, now)
		if e != nil {
			return nil, time.Time{}, e
		}
		matched := false
		for _, f := range rows {
			if f.ID == ref.EvidenceID && f.Version == ref.Version && f.PermissionID == ref.PermissionID {
				fields = append(fields, f)
				until = retryMinimum(until, retryMinimum(f.ValidTo, retryMinimum(f.RefreshDue, f.Permission.RetentionUntil)))
				matched = true
				break
			}
		}
		if !matched {
			return nil, time.Time{}, arp.ErrChanged
		}
	}
	for _, use := range evidence.MemoryUses {
		memory, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND owner_id=$2 AND agent_id=$3 AND owner_type='PERSON' FOR SHARE`, use.MemoryID, b.accountID, b.agentID))
		if e != nil {
			return nil, time.Time{}, arp.ErrChanged
		}
		value, e := agentworkspace.ParseNowPreferenceMemory(memory, now)
		if e != nil || memory.Version != use.Version || value.Topic != use.Topic || value.Value != use.Value {
			return nil, time.Time{}, arp.ErrChanged
		}
		if e = validateNowPreferenceSourcesTx(ctx, tx, b, value); e != nil {
			return nil, time.Time{}, e
		}
		until = retryMinimum(until, memory.ValidUntil)
	}
	if !reflect.DeepEqual(localFieldDigest(fields, evidence.MemoryUses), evidence.SourceEvidenceDigest) {
		return nil, time.Time{}, arp.ErrChanged
	}
	if !until.After(time.Now().UTC()) {
		return nil, time.Time{}, arp.ErrChanged
	}
	return fields, until, nil
}
