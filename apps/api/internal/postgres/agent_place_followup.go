package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/jackc/pgx/v5"
)

var _ agentworkspace.PublicPlaceFollowupPort = (*Store)(nil)

// Comparable private data keeps the original CALL -> MODEL preparation's
// exact comparison. No native row generation or identifier reaches the model.
type nativeResolvedPublicPlace struct {
	id, name, generation, raw string
	valid                     bool
}

func livePlaceGeneration(value string) bool {
	decoded, e := hex.DecodeString(value)
	return e == nil && len(decoded) == 32 && value == strings.ToLower(value) && value != strings.Repeat("0", 64)
}

func nativePublicFollowupPlaceTx(ctx context.Context, tx pgx.Tx, id, city string, deadline time.Time) (nativeResolvedPublicPlace, error) {
	var p nativeResolvedPublicPlace
	var expiry *time.Time
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT p.id,p.name,p.expires_at,
 jsonb_build_object('id',p.id,'name',p.name,'category',p.category_code,'updatedAt',p.updated_at,'expiry',p.expires_at,'row',p.xmin::text,'sourceLabel',p.source_label,'sourceRef',p.source_ref,'verifiedAt',p.verified_at)
 FROM places p JOIN cities c ON c.id=p.city_id JOIN city_contexts cc ON cc.city_id=c.id
 WHERE p.id=$1 AND p.city_id=$2 AND p.publication_status='published' AND c.publication_status='published' AND cc.status='active'
 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND p.updated_at<=clock_timestamp() FOR SHARE OF p,c,cc`, id, city).Scan(&p.id, &p.name, &expiry, &raw)
	if e != nil || !agentworkspace.ValidPlaceFollowupName(p.name) || (expiry != nil && deadline.After(*expiry)) {
		return nativeResolvedPublicPlace{}, agentworkspace.ErrPlaceFollowupChanged
	}
	p.raw, p.valid = string(raw), true
	p.generation = liveDigestBytes("birdtie.native-public-place-followup.v1", raw)
	return p, nil
}

func latestSinglePlaceReply(t agentworkspace.Task) (*nativeReplySelector, error) {
	i := len(t.Conversation) - 1
	if t.Status != agentworkspace.TaskCompleted || i < 0 || agentworkspace.ValidateReplyMembership(t, i) != nil {
		return nil, agentworkspace.ErrPlaceFollowupAmbiguous
	}
	m := *t.Conversation[i].ResultMembership
	if m.Kind != "place" || len(m.Refs) != 1 || m.Refs[0].Type != "place" || !egressUUID(m.Refs[0].ID) {
		return nil, agentworkspace.ErrPlaceFollowupAmbiguous
	}
	return &nativeReplySelector{index: i, membership: m}, nil
}

func publicPlaceMatchesCurrentSearch(name, term string) bool {
	return agentworkspace.ValidPlaceFollowupName(name) && agentworkspace.ValidPlaceFollowupName(term) && strings.Contains(strings.ToLower(name), strings.ToLower(term))
}

// ContinueOwnPublicPlace resolves only the current explicit public question,
// then appends that user turn through the original Task writer in the SAME
// native transaction. Human read policy is a brake, never model-egress approval.
func (s *Store) ContinueOwnPublicPlace(ctx context.Context, a arp.Access, query string) (agentworkspace.Task, error) {
	tx, _, policy, e := s.beginCurrentToolRead(ctx, a.Actor, a.SessionDigest, resultProjectionRelations)
	if e != nil {
		return agentworkspace.Task{}, e
	}
	defer tx.Rollback(context.Background())
	t, e := ownReplyTaskTx(ctx, tx, a, true)
	if e != nil {
		return t, e
	}
	f, ok := agentworkspace.ParsePlaceFollowup(query, &t)
	if !ok || t.Status != agentworkspace.TaskCompleted || agenttool.Restrict(agenttool.CurrentSearchTool("place"), actorref.Person, policy.level).Denied {
		return agentworkspace.Task{}, arp.ErrDenied
	}
	var id, expectedName string
	if f.Pronoun {
		selector, selectErr := latestSinglePlaceReply(t)
		if selectErr != nil {
			return agentworkspace.Task{}, selectErr
		}
		r, readErr := s.captureAgentResultProjectionSelectedTx(ctx, tx, a, arp.Query{CityID: t.CityID, Kind: "place", CompareIDs: []string{}}, nil, policy, selector)
		if readErr != nil {
			return agentworkspace.Task{}, readErr
		}
		if len(r.Items) != 1 || r.Items[0].Entity != selector.membership.Refs[0] {
			return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupChanged
		}
		id, expectedName = r.Items[0].Entity.ID, r.Items[0].Title
		if !publicPlaceMatchesCurrentSearch(expectedName, t.Filters["searchTerm"]) {
			return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupChanged
		}
		_, retainedID := t.Filters[agentworkspace.PlaceFollowupIDFilter]
		_, retainedGeneration := t.Filters[agentworkspace.PlaceFollowupGenerationFilter]
		_, retainedTopic := t.Filters[agentworkspace.PlaceFollowupTopicFilter]
		if retainedID || retainedGeneration || retainedTopic {
			previous, _, proofErr := nativeResolvedPlaceFromTaskTx(ctx, tx, t, policy.until)
			if proofErr != nil || !previous.valid || previous.id != id || previous.name != expectedName {
				return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupChanged
			}
		}
	} else {
		rows, readErr := tx.Query(ctx, `SELECT id,name FROM places WHERE city_id=$1 AND publication_status='published'
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND updated_at<=clock_timestamp()
 AND strpos(lower(name),lower($2))>0 ORDER BY name,id LIMIT 2 FOR SHARE`, t.CityID, f.Name)
		if readErr != nil {
			return agentworkspace.Task{}, arp.ErrUnavailable
		}
		count := 0
		for rows.Next() {
			count++
			if e = rows.Scan(&id, &expectedName); e != nil {
				rows.Close()
				return agentworkspace.Task{}, arp.ErrUnavailable
			}
		}
		rows.Close()
		if rows.Err() != nil {
			return agentworkspace.Task{}, arp.ErrUnavailable
		}
		if count == 0 {
			return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupNotFound
		}
		if count != 1 {
			return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupAmbiguous
		}
	}
	place, e := nativePublicFollowupPlaceTx(ctx, tx, id, t.CityID, policy.until)
	if e != nil || place.name != expectedName {
		return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupChanged
	}
	// Do not mutate the caller's map or turn before all source checks succeed.
	filters := make(map[string]string, len(t.Filters)+3)
	for key, value := range t.Filters {
		filters[key] = value
	}
	t.Filters = filters
	t.Intent, t.Status = agentworkspace.FindPlace, agentworkspace.TaskActive
	t.Filters["targetIntent"], t.Filters["searchTerm"], t.Filters["currentQuery"] = agentworkspace.FindPlace, place.name, query
	t.Filters["timePreference"] = f.TimePreference
	t.Filters[agentworkspace.PlaceFollowupIDFilter], t.Filters[agentworkspace.PlaceFollowupGenerationFilter], t.Filters[agentworkspace.PlaceFollowupTopicFilter] = place.id, place.generation, f.Topic
	t.Conversation = append(t.Conversation, agentworkspace.Message{Role: "user", Text: query})
	t, e = s.updateTaskInTx(ctx, tx, t)
	if e != nil {
		return agentworkspace.Task{}, e
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(t))
	if e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	a.ExpectedTask = raw
	_, receipt, e := s.captureReplyCurrentSourceTx(ctx, tx, a, t, policy)
	if e != nil {
		return agentworkspace.Task{}, e
	}
	if len(receipt.Items) != 1 || receipt.Items[0].Entity != (arp.Ref{Type: "place", ID: place.id}) || receipt.Items[0].Title != place.name {
		return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupChanged
	}
	current, e := nativePublicFollowupPlaceTx(ctx, tx, place.id, t.CityID, receipt.ValidUntil)
	if e != nil || current != place || ctx.Err() != nil {
		return agentworkspace.Task{}, agentworkspace.ErrPlaceFollowupChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	return t, nil
}

func nativeResolvedPlaceFromTaskTx(ctx context.Context, tx pgx.Tx, t agentworkspace.Task, deadline time.Time) (nativeResolvedPublicPlace, string, error) {
	id, generation, topic := t.Filters[agentworkspace.PlaceFollowupIDFilter], t.Filters[agentworkspace.PlaceFollowupGenerationFilter], t.Filters[agentworkspace.PlaceFollowupTopicFilter]
	_, hasID := t.Filters[agentworkspace.PlaceFollowupIDFilter]
	_, hasGeneration := t.Filters[agentworkspace.PlaceFollowupGenerationFilter]
	_, hasTopic := t.Filters[agentworkspace.PlaceFollowupTopicFilter]
	if !hasID && !hasGeneration && !hasTopic {
		return nativeResolvedPublicPlace{}, "", nil
	}
	if !hasID || !hasGeneration || !hasTopic || !egressUUID(id) || !livePlaceGeneration(generation) || t.Intent != agentworkspace.FindPlace || t.Filters["targetIntent"] != agentworkspace.FindPlace || (topic != "" && topic != agentworkspace.PlaceOpeningHours) {
		return nativeResolvedPublicPlace{}, "", modelegressbudget.ErrDenied
	}
	place, e := nativePublicFollowupPlaceTx(ctx, tx, id, t.CityID, deadline)
	if e != nil || place.generation != generation || place.name != t.Filters["searchTerm"] {
		return nativeResolvedPublicPlace{}, "", modelegressbudget.ErrDenied
	}
	term := place.name
	if topic == agentworkspace.PlaceOpeningHours {
		term += " opening hours"
	}
	if strings.TrimSpace(term) != term || len(term) > 240 {
		return nativeResolvedPublicPlace{}, "", modelegressbudget.ErrDenied
	}
	return place, term, nil
}

func resolvedPublicPlaceContextEvidence(public modelegressbudget.LiveResolvedPublicSearchContext, nativeCity []byte, place nativeResolvedPublicPlace) (string, error) {
	if !place.valid || !egressUUID(place.id) || !livePlaceGeneration(place.generation) {
		return "", modelegressbudget.ErrDenied
	}
	raw, e := json.Marshal(struct {
		Context modelegressbudget.LiveResolvedPublicSearchContext
		City    json.RawMessage
		Place   json.RawMessage
	}{public, nativeCity, json.RawMessage(place.raw)})
	if e != nil {
		return "", modelegressbudget.ErrDenied
	}
	return liveDigestBytes("birdtie.native-public-place-search-context.v1", raw), nil
}
