package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/placematch"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"github.com/jackc/pgx/v5"
)

func (s *Store) LoadPlaceMatchInputs(ctx context.Context, personID, intentID string) (placematch.Inputs, error) {
	in := placematch.Inputs{PersonID: personID, Supply: []placematch.Supply{}}
	intent, err := s.GetOwnSocialIntent(ctx, personID, intentID)
	if err != nil {
		return in, err
	}
	in.Intent = intent
	if intent.Status != socialintent.Active || !intent.ExpiresAt.After(time.Now()) ||
		intent.Modality != "IN_PERSON" || (intent.Type != "FIND_ACTIVITY" && intent.Type != "FIND_COMPANION" && intent.Type != "ORGANIZE") {
		return in, nil
	}
	c, _, err := socialintent.ParseConstraints(intent.Constraints, intent.Modality)
	if err != nil {
		return in, nil
	}
	cityID := intent.CityID
	if intent.ContextID != nil {
		var selected string
		err = s.pool.QueryRow(ctx, `SELECT x.city_id FROM contexts x JOIN cities city ON city.id=x.city_id
			WHERE x.id=$1 AND x.context_type='CITY' AND city.publication_status='published'`, *intent.ContextID).Scan(&selected)
		if errors.Is(err, pgx.ErrNoRows) {
			return in, nil
		}
		if err != nil {
			return in, err
		}
		if cityID != "" && cityID != selected {
			return in, nil
		}
		cityID = selected
	}
	in.CityID = cityID
	var places []foundation.Place
	if c.PlaceID != "" {
		p, e := s.GetPlace(ctx, c.PlaceID)
		if errors.Is(e, foundation.ErrNotFound) {
			return in, nil
		}
		if e != nil {
			return in, e
		}
		if cityID != "" && p.CityID != cityID {
			return in, nil
		}
		places = []foundation.Place{p}
	} else {
		if cityID == "" {
			return in, nil
		}
		needed := c.MinParticipants
		if c.MaxParticipants > needed {
			needed = c.MaxParticipants
		}
		rows, e := s.pool.Query(ctx, `SELECT v.place_id FROM venues v
			JOIN venue_candidates vc ON vc.id=v.source_candidate_id AND vc.status='approved'
			JOIN places p ON p.id=v.place_id AND p.city_id=v.city_id
			JOIN cities city ON city.id=p.city_id
			WHERE v.city_id=$1 AND city.publication_status='published'
			AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>now())
			AND v.expires_at>now()
			AND ($2::text='' OR $2=ANY(v.suitability))
			AND ($3::integer=0 OR v.capacity >= $3)
			AND ($4::text='' OR strpos(lower(p.name),lower($4))>0
			     OR strpos(lower(coalesce(p.address_label,'')),lower($4))>0)
			ORDER BY p.id LIMIT 200`, cityID, c.Category, needed, c.AreaLabel)
		if e != nil {
			return in, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return in, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return in, e
		}
		for _, id := range ids {
			p, e := s.GetPlace(ctx, id)
			if errors.Is(e, foundation.ErrNotFound) {
				continue
			}
			if e != nil {
				return in, e
			}
			places = append(places, p)
		}
	}
	for _, p := range places {
		v, e := s.GetPublicVenue(ctx, p.ID)
		if errors.Is(e, venue.ErrNotFound) {
			in.Supply = append(in.Supply, placematch.Supply{Place: p})
			continue
		}
		if e != nil {
			return in, e
		}
		in.Supply = append(in.Supply, placematch.Supply{Place: p, Venue: &v})
	}
	return in, nil
}

// LoadCurrentPlaceMatchInputs is the registered human intent matching path.
// It consumes current native self Intent plus public facts, never Memory.
func (s *Store) LoadCurrentPlaceMatchInputs(ctx context.Context, a pp.Access, id string) (placematch.Inputs, error) {
	empty := placematch.Inputs{}
	if ctx == nil || s == nil || s.pool == nil || ctx.Err() != nil {
		return empty, pp.ErrUnavailable
	}
	if pp.ValidateAccess(a) != nil || a.AccountType != "person" || !pp.ValidID(id) {
		return empty, pp.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return empty, pp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return empty, pp.ErrUnavailable
	}
	var owner, session string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.ActorID).Scan(&owner); e != nil {
		return empty, placeSemanticError(e)
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, a.SessionDigest[:], owner, s.devPhoneEnabled).Scan(&session); e != nil {
		return empty, placeSemanticError(e)
	}
	in := placematch.Inputs{PersonID: owner, Supply: []placematch.Supply{}}
	in.Intent, e = scanSocialIntent(tx.QueryRow(ctx, `SELECT `+socialIntentColumns+` FROM social_intents WHERE id=$1 AND creator_account_id=$2 FOR SHARE`, id, owner))
	if errors.Is(e, pgx.ErrNoRows) {
		return empty, socialintent.ErrNotFound
	}
	if e != nil {
		return empty, pp.ErrUnavailable
	}
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return empty, pp.ErrUnavailable
	}
	c, _, e := socialintent.ParseConstraints(in.Intent.Constraints, in.Intent.Modality)
	active := e == nil && in.Intent.Status == socialintent.Active && in.Intent.ExpiresAt.After(at) && in.Intent.Modality == "IN_PERSON" && (in.Intent.Type == "FIND_ACTIVITY" || in.Intent.Type == "FIND_COMPANION" || in.Intent.Type == "ORGANIZE")
	if active {
		var cityID string
		if e = tx.QueryRow(ctx, `SELECT coalesce(city_id,'') FROM social_intent_audience_targets WHERE intent_id=$1`, id).Scan(&cityID); e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return empty, pp.ErrUnavailable
		}
		if in.Intent.ContextID != nil {
			var selected string
			e = tx.QueryRow(ctx, `SELECT city_id FROM contexts WHERE id=$1 AND context_type='CITY' FOR SHARE`, *in.Intent.ContextID).Scan(&selected)
			if e != nil {
				active = false
			} else if cityID != "" && cityID != selected {
				active = false
			} else {
				cityID = selected
			}
		}
		if c.PlaceID != "" {
			var selected string
			e = tx.QueryRow(ctx, `SELECT city_id FROM places WHERE id=$1 AND publication_status='published'`, c.PlaceID).Scan(&selected)
			if errors.Is(e, pgx.ErrNoRows) {
				active = false
			} else if e != nil {
				return empty, pp.ErrUnavailable
			} else if cityID != "" && cityID != selected {
				active = false
			} else {
				cityID = selected
			}
		}
		if active && cityID != "" {
			e = tx.QueryRow(ctx, `SELECT id FROM cities WHERE id=$1 AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND updated_at<=clock_timestamp() FOR SHARE`, cityID).Scan(&in.CityID)
			if errors.Is(e, pgx.ErrNoRows) {
				active = false
			} else if e != nil {
				return empty, pp.ErrUnavailable
			}
		} else {
			active = false
		}
		if active {
			needed := c.MinParticipants
			if c.MaxParticipants > needed {
				needed = c.MaxParticipants
			}
			rows, e := tx.Query(ctx, `SELECT p.id FROM places p JOIN cities city ON city.id=p.city_id LEFT JOIN venues v ON v.place_id=p.id AND v.city_id=p.city_id LEFT JOIN venue_candidates vc ON vc.id=v.source_candidate_id AND vc.status='approved' WHERE p.city_id=$1 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp()) AND ($2::text='' OR p.id=NULLIF($2,'')::uuid) AND ($2::text<>'' OR (v.expires_at>clock_timestamp() AND vc.id IS NOT NULL AND ($3::text='' OR $3=ANY(v.suitability)) AND ($4::integer=0 OR v.capacity>=$4))) AND ($5::text='' OR strpos(lower(p.name),lower($5))>0 OR strpos(lower(coalesce(p.address_label,'')),lower($5))>0) ORDER BY p.id LIMIT 200`, in.CityID, c.PlaceID, c.Category, needed, c.AreaLabel)
			if e != nil {
				return empty, pp.ErrUnavailable
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return empty, pp.ErrUnavailable
				}
				ids = append(ids, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return empty, pp.ErrUnavailable
			}
			for _, placeID := range ids {
				p, e := scanPlace(tx.QueryRow(ctx, `SELECT `+placeColumns+` FROM places p JOIN cities city ON city.id=p.city_id WHERE p.id=$1 AND p.city_id=$2 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) FOR SHARE OF p`, placeID, in.CityID))
				if errors.Is(e, pgx.ErrNoRows) {
					continue
				}
				if e != nil {
					return empty, pp.ErrUnavailable
				}
				p.Source.SetFreshness(at)
				supply := placematch.Supply{Place: p}
				var v venue.Public
				v.PlaceID = p.ID
				v.CityID = p.CityID
				e = tx.QueryRow(ctx, `SELECT v.capacity,v.reservation_support,v.reservation_url,v.suitability,v.source_url,v.reviewed_at,v.expires_at FROM venues v JOIN venue_candidates c ON c.id=v.source_candidate_id AND c.status='approved' WHERE v.place_id=$1 AND v.city_id=$2 AND v.expires_at>clock_timestamp() AND v.reviewed_at<=clock_timestamp() FOR SHARE OF v,c`, p.ID, p.CityID).Scan(&v.Capacity, &v.ReservationSupport, &v.ReservationURL, &v.Suitability, &v.SourceURL, &v.ReviewedAt, &v.ExpiresAt)
				if e == nil {
					supply.Venue = &v
				} else if !errors.Is(e, pgx.ErrNoRows) {
					return empty, pp.ErrUnavailable
				}
				profile, e := readPlaceSemanticPublic(ctx, tx, p.ID)
				if e == nil {
					supply.Profile = &profile
				} else if !errors.Is(e, pp.ErrNotFound) {
					return empty, e
				}
				in.Supply = append(in.Supply, supply)
			}
		}
	}
	// Resolve profile selectors before the final native authority statement.
	profileCandidates := map[string]string{}
	candidateIDs := []string{}
	for _, supply := range in.Supply {
		if supply.Profile == nil {
			continue
		}
		var candidate string
		if e = tx.QueryRow(ctx, `SELECT source_candidate_id FROM place_semantic_profiles WHERE place_id=$1`, supply.Place.ID).Scan(&candidate); e != nil {
			return empty, pp.ErrUnavailable
		}
		profileCandidates[supply.Place.ID] = candidate
		candidateIDs = append(candidateIDs, candidate)
	}
	var alive bool
	var validCandidates []string
	e = tx.QueryRow(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() at) SELECT at,
	EXISTS(SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id WHERE se.id=$1 AND a.id=$2 AND a.account_type='person' AND a.status='active' AND se.revoked_at IS NULL AND se.expires_at>clock.at AND se.idle_expires_at>clock.at AND ($3::boolean OR se.authentication_method<>'dev_phone'))
	AND (NOT $4::boolean OR EXISTS(SELECT 1 FROM social_intents i JOIN cities city ON city.id=$5 WHERE i.id=$6 AND i.creator_account_id=$2 AND i.status='ACTIVE' AND i.expires_at>clock.at AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock.at))),
	ARRAY(SELECT c.id::text FROM place_semantic_candidates c JOIN accounts a ON a.id=c.submitted_by JOIN city_editor_memberships m ON m.city_id=c.city_id AND m.account_id=c.submitted_by JOIN cities city ON city.id=c.city_id JOIN places p ON p.id=c.place_id AND p.city_id=c.city_id WHERE c.id=ANY($7::uuid[]) AND `+placeSemanticOriginPredicate+`) FROM clock`, session, owner, s.devPhoneEnabled, active, in.CityID, id, candidateIDs).Scan(&in.ObservedAt, &alive, &validCandidates)
	if e != nil || ctx.Err() != nil {
		return empty, pp.ErrUnavailable
	}
	if !alive {
		return empty, pp.ErrDenied
	}
	valid := map[string]bool{}
	for _, id := range validCandidates {
		valid[id] = true
	}
	retained := []placematch.Supply{}
	for _, supply := range in.Supply {
		if supply.Place.Source.ExpiresAt != nil && !supply.Place.Source.ExpiresAt.After(in.ObservedAt) {
			continue
		}
		supply.Place.Source.SetFreshness(in.ObservedAt)
		if supply.Venue != nil && !supply.Venue.ExpiresAt.After(in.ObservedAt) {
			supply.Venue = nil
		}
		if supply.Profile != nil {
			if !valid[profileCandidates[supply.Place.ID]] || pp.ValidatePublic(*supply.Profile, in.ObservedAt) != nil {
				supply.Profile = nil
			} else {
				supply.Profile.CheckedAt = in.ObservedAt
			}
		}
		retained = append(retained, supply)
	}
	in.Supply = retained
	in.ObservedAt = in.ObservedAt.UTC()
	if ctx.Err() != nil {
		return empty, pp.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, pp.ErrUnavailable
	}
	return in, nil
}
