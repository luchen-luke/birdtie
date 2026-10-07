package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
)

// LoadOpportunityInputs reads only the owner's active Intents and Activities
// already authorized by the existing Activity visibility/Block predicate.
// No Agent text, attendance inference or unpublished Place is a supply source.
func (s *Store) LoadOpportunityInputs(ctx context.Context, personID string) (opportunity.Inputs, error) {
	in := opportunity.Inputs{
		PersonID: personID, Intents: []socialintent.Record{}, Supply: []opportunity.Supply{},
		ContextCities: map[string]string{}, DeclaredCities: map[string]string{},
		TiedPeople:         map[string]bool{},
		JoinedCommunities:  map[string]bool{},
		FollowedOrganizers: map[string]bool{},
	}
	intents, err := s.ListOwnSocialIntents(ctx, personID)
	if err != nil {
		return in, err
	}
	for _, intent := range intents {
		if intent.Status == socialintent.Active && intent.ExpiresAt.After(time.Now()) &&
			intent.Type == "FIND_ACTIVITY" {
			in.Intents = append(in.Intents, intent)
		}
	}
	if len(in.Intents) == 0 {
		return in, nil
	}
	contextRows, err := s.pool.Query(ctx, `SELECT pc.context_id::text,c.city_id,pc.relation
		FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id
		JOIN cities city ON city.id=c.city_id AND city.publication_status='published'
		WHERE pc.person_account_id=$1 AND c.context_type='CITY'
		AND pc.relation IN ('current','destination')`, personID)
	if err != nil {
		return in, err
	}
	for contextRows.Next() {
		var id, cityID, relation string
		if err := contextRows.Scan(&id, &cityID, &relation); err != nil {
			contextRows.Close()
			return in, err
		}
		in.ContextCities[id] = cityID
		in.DeclaredCities[cityID] = relation
	}
	err = contextRows.Err()
	contextRows.Close()
	if err != nil {
		return in, err
	}
	// An explicitly selected CITY Context on an Intent need not also be a
	// current/destination Person declaration. Resolve the typed public node.
	for _, intent := range in.Intents {
		if intent.ContextID == nil || in.ContextCities[*intent.ContextID] != "" {
			continue
		}
		var cityID string
		err := s.pool.QueryRow(ctx, `SELECT c.city_id FROM contexts c
			JOIN cities city ON city.id=c.city_id AND city.publication_status='published'
			WHERE c.id=$1 AND c.context_type='CITY'`, *intent.ContextID).Scan(&cityID)
		if err == nil {
			in.ContextCities[*intent.ContextID] = cityID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return in, err
		}
	}
	tieRows, err := s.pool.Query(ctx, `SELECT CASE WHEN t.person_a_account_id=$1
		THEN t.person_b_account_id::text ELSE t.person_a_account_id::text END
		FROM person_ties t
		JOIN accounts person_a ON person_a.id=t.person_a_account_id
		AND person_a.account_type='person' AND person_a.status='active'
		JOIN accounts person_b ON person_b.id=t.person_b_account_id
		AND person_b.account_type='person' AND person_b.status='active'
		JOIN connection_requests friend_request ON friend_request.id=t.request_id
		AND friend_request.scope='friend' AND friend_request.state='accepted'
		AND LEAST(friend_request.sender_account_id,friend_request.recipient_account_id)=t.person_a_account_id
		AND GREATEST(friend_request.sender_account_id,friend_request.recipient_account_id)=t.person_b_account_id
		WHERE t.status='active'
		AND ($1=t.person_a_account_id OR $1=t.person_b_account_id)
		AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
		  (b.blocker_account_id=t.person_a_account_id AND b.blocked_account_id=t.person_b_account_id) OR
		  (b.blocker_account_id=t.person_b_account_id AND b.blocked_account_id=t.person_a_account_id))`, personID)
	if err != nil {
		return in, err
	}
	for tieRows.Next() {
		var id string
		if err := tieRows.Scan(&id); err != nil {
			tieRows.Close()
			return in, err
		}
		in.TiedPeople[id] = true
	}
	err = tieRows.Err()
	tieRows.Close()
	if err != nil {
		return in, err
	}
	communityRows, err := s.pool.Query(ctx, `SELECT m.community_id::text
		FROM community_memberships m JOIN communities c ON c.id=m.community_id
		WHERE m.user_account_id=$1 AND m.status='active'
		AND c.lifecycle_status='active' AND c.publication_status='published'`, personID)
	if err != nil {
		return in, err
	}
	for communityRows.Next() {
		var id string
		if err := communityRows.Scan(&id); err != nil {
			communityRows.Close()
			return in, err
		}
		in.JoinedCommunities[id] = true
	}
	err = communityRows.Err()
	communityRows.Close()
	if err != nil {
		return in, err
	}
	followed, err := s.ListOwnFollows(ctx, personID)
	if err != nil {
		return in, err
	}
	for _, item := range followed {
		in.FollowedOrganizers[item.Target.Type+":"+item.Target.ID] = true
	}
	seen := map[string]bool{}
	placeCache := map[string]foundation.Place{}
	for _, intent := range in.Intents {
		if intent.Modality != "IN_PERSON" {
			continue
		}
		constraints, _, err := socialintent.ParseConstraints(intent.Constraints, intent.Modality)
		if err != nil {
			continue
		}
		cityID := intent.CityID
		if intent.ContextID != nil {
			selected := in.ContextCities[*intent.ContextID]
			if selected == "" || (cityID != "" && cityID != selected) {
				continue
			}
			cityID = selected
		}
		var placeID any
		if constraints.PlaceID != "" {
			placeID = constraints.PlaceID
		}
		rows, err := s.pool.Query(ctx, `SELECT `+activityColumns+publishedActivityFrom+`
			AND p.id IS NOT NULL AND a.cancelled_at IS NULL AND a.ends_at>now()
			AND EXISTS(SELECT 1 FROM accounts owner WHERE owner.id=$1::uuid
				AND owner.account_type='person' AND owner.status='active')
			AND (a.expires_at IS NULL OR a.expires_at>now())
			AND ($3::text='' OR a.category_code=$3)
			AND ($4::uuid IS NULL OR a.place_id=$4)
			AND ($5::text='' OR strpos(lower(p.name),lower($5))>0
				OR strpos(lower(coalesce(p.address_label,'')),lower($5))>0)
			AND ($6::text='' OR a.city_id=$6)
			ORDER BY a.starts_at,a.id LIMIT 100`, personID, personID,
			constraints.Category, placeID, constraints.AreaLabel, cityID)
		if err != nil {
			return in, err
		}
		activities := []foundation.Activity{}
		for rows.Next() {
			activity, err := scanActivity(rows)
			if err != nil {
				rows.Close()
				return in, err
			}
			activities = append(activities, activity)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return in, err
		}
		for _, activity := range activities {
			if seen[activity.ID] {
				continue
			}
			place, ok := placeCache[activity.PlaceID]
			if !ok {
				// Querying again against Place public policy handles expiry
				// changes between the Activity and Place reads.
				place, err = s.GetPlace(ctx, activity.PlaceID)
				if errors.Is(err, foundation.ErrNotFound) {
					continue
				} else if err != nil {
					return in, err
				}
				placeCache[activity.PlaceID] = place
			}
			seen[activity.ID] = true
			in.Supply = append(in.Supply, opportunity.Supply{Activity: activity, Place: place})
		}
	}
	return in, nil
}
