package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const venueCandidateColumns = `id,place_id,city_id,submitted_by,capacity,reservation_support,
    reservation_url,suitability,amenities,operator_organization_id,source_url,rights_note,
    expires_at,status,reviewed_by,reviewed_at,review_note,created_at`

func scanVenueCandidate(row scanner) (venue.Candidate, error) {
	var c venue.Candidate
	err := row.Scan(&c.ID, &c.PlaceID, &c.CityID, &c.SubmittedBy, &c.Capacity,
		&c.ReservationSupport, &c.ReservationURL, &c.Suitability, &c.Amenities,
		&c.OperatorOrganizationID, &c.SourceURL, &c.RightsNote, &c.ExpiresAt,
		&c.Status, &c.ReviewedBy, &c.ReviewedAt, &c.ReviewNote, &c.CreatedAt)
	return c, err
}

func (s *Store) SubmitVenueCandidate(ctx context.Context, actorID, cityID, placeID string, input venue.SubmitInput) (venue.Candidate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return venue.Candidate{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanVenueCandidate(tx.QueryRow(ctx, `INSERT INTO venue_candidates
        (place_id,city_id,submitted_by,capacity,reservation_support,reservation_url,
         suitability,amenities,operator_organization_id,source_url,rights_note,expires_at)
        SELECT p.id,p.city_id,a.id,$4,$5,$6,$7,$8,$9,$10,$11,$12
        FROM places p JOIN cities city ON city.id=p.city_id
        JOIN city_editor_memberships m ON m.city_id=p.city_id
        JOIN accounts a ON a.id=m.account_id
        WHERE p.id=$1 AND p.city_id=$2 AND m.account_id=$3 AND m.state='active'
          AND a.status='active' AND p.publication_status='published'
          AND city.publication_status='published'
          AND (p.expires_at IS NULL OR p.expires_at>now())
          AND ($9::uuid IS NULL OR EXISTS(SELECT 1 FROM organizations o
              WHERE o.id=$9 AND o.status='active'))
        RETURNING `+venueCandidateColumns,
		placeID, cityID, actorID, input.Capacity, input.ReservationSupport,
		input.ReservationURL, input.Suitability, input.Amenities,
		input.OperatorOrganizationID, input.SourceURL, input.RightsNote, input.ExpiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return venue.Candidate{}, venue.ErrForbidden
	}
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			return venue.Candidate{}, venue.ErrConflict
		}
		return venue.Candidate{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,'submit','venue_candidate',$2,'allowed','city_seed')`, actorID, c.ID); err != nil {
		return venue.Candidate{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return venue.Candidate{}, err
	}
	return c, nil
}

func (s *Store) ListVenueCandidates(ctx context.Context, actorID, cityID string) ([]venue.Candidate, error) {
	var allowed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM city_editor_memberships m
        JOIN accounts a ON a.id=m.account_id WHERE m.city_id=$1 AND m.account_id=$2
        AND m.role='reviewer' AND m.state='active' AND a.status='active')`, cityID, actorID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, venue.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT `+venueCandidateColumns+` FROM venue_candidates
        WHERE city_id=$1 AND status='pending' ORDER BY created_at,id LIMIT 100`, cityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]venue.Candidate, 0)
	for rows.Next() {
		c, err := scanVenueCandidate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (s *Store) ReviewVenueCandidate(ctx context.Context, actorID, candidateID string, input venue.ReviewInput) (venue.Candidate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return venue.Candidate{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanVenueCandidate(tx.QueryRow(ctx, `SELECT `+venueCandidateColumns+` FROM venue_candidates
        WHERE id=$1 AND city_id IN (SELECT m.city_id FROM city_editor_memberships m
        JOIN accounts a ON a.id=m.account_id WHERE m.account_id=$2 AND m.role='reviewer'
        AND m.state='active' AND a.status='active') FOR UPDATE`, candidateID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return venue.Candidate{}, venue.ErrForbidden
	}
	if err != nil {
		return venue.Candidate{}, err
	}
	if c.Status != "pending" || c.SubmittedBy == actorID {
		return venue.Candidate{}, venue.ErrConflict
	}
	if input.Decision == "approve" {
		if !c.ExpiresAt.After(time.Now()) {
			return venue.Candidate{}, venue.ErrConflict
		}
		var placeOK bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM places p JOIN cities city ON city.id=p.city_id
            WHERE p.id=$1 AND p.city_id=$2 AND p.publication_status='published'
            AND city.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>now())
            AND ($3::uuid IS NULL OR EXISTS(SELECT 1 FROM organizations o
                 WHERE o.id=$3 AND o.status='active')))`, c.PlaceID, c.CityID, c.OperatorOrganizationID).Scan(&placeOK); err != nil {
			return venue.Candidate{}, err
		}
		if !placeOK {
			return venue.Candidate{}, venue.ErrConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO venues(place_id,city_id,capacity,reservation_support,reservation_url,
            suitability,amenities,operator_organization_id,source_candidate_id,source_url,
            reviewed_by,reviewed_at,expires_at)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now(),$12)
            ON CONFLICT(place_id) DO UPDATE SET capacity=EXCLUDED.capacity,
            reservation_support=EXCLUDED.reservation_support,reservation_url=EXCLUDED.reservation_url,
            suitability=EXCLUDED.suitability,amenities=EXCLUDED.amenities,
            operator_organization_id=EXCLUDED.operator_organization_id,
            source_candidate_id=EXCLUDED.source_candidate_id,source_url=EXCLUDED.source_url,
            reviewed_by=EXCLUDED.reviewed_by,reviewed_at=EXCLUDED.reviewed_at,
            expires_at=EXCLUDED.expires_at,updated_at=now()`, c.PlaceID, c.CityID, c.Capacity,
			c.ReservationSupport, c.ReservationURL, c.Suitability, c.Amenities,
			c.OperatorOrganizationID, c.ID, c.SourceURL, actorID, c.ExpiresAt)
		if err != nil {
			return venue.Candidate{}, err
		}
	} else if input.Decision != "reject" {
		return venue.Candidate{}, venue.ErrConflict
	}
	status := "approved"
	if input.Decision == "reject" {
		status = "rejected"
	}
	c, err = scanVenueCandidate(tx.QueryRow(ctx, `UPDATE venue_candidates SET status=$2,reviewed_by=$3,
        reviewed_at=now(),review_note=$4 WHERE id=$1 AND status='pending'
        RETURNING `+venueCandidateColumns, candidateID, status, actorID, input.Note))
	if err != nil {
		return venue.Candidate{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,$2,'venue_candidate',$3,'allowed','city_seed')`, actorID, status, c.ID); err != nil {
		return venue.Candidate{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return venue.Candidate{}, err
	}
	return c, nil
}

func (s *Store) GetPublicVenue(ctx context.Context, placeID string) (venue.Public, error) {
	r, e := s.ReadCurrentPublicVenue(ctx, venue.PublicAccess{}, placeID)
	if e != nil {
		return venue.Public{}, e
	}
	if e = s.RevalidateCurrentPublicVenue(ctx, venue.PublicAccess{}, placeID, r); e != nil {
		return venue.Public{}, e
	}
	return r.View, nil
}
