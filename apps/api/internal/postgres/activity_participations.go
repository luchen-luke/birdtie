package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
)

const participationColumns = `id, activity_id, status, cancelled_at, created_at, updated_at`

func (s *Store) ListParticipations(ctx context.Context, personID string) ([]activityparticipation.Overview, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.activity_id, p.status,
		COALESCE(a.title, ''), COALESCE(a.city_id, ''), a.starts_at, a.ends_at,
		CASE WHEN a.id IS NULL THEN 'unavailable'
			WHEN a.cancelled_at IS NOT NULL THEN 'cancelled'
			WHEN a.ends_at <= clock_timestamp() THEN 'completed'
			WHEN a.starts_at <= clock_timestamp() THEN 'ongoing'
			ELSE 'upcoming' END,
		a.id IS NOT NULL, COALESCE(a.modality,''), COALESCE(a.physical_place_status,''),
        COALESCE(loc.id::text,''), COALESCE(loc.name,''),
        CASE WHEN venue.place_id IS NOT NULL THEN venue.place_id::text ELSE '' END, COALESCE(a.time_zone,'')
        FROM activity_participations p
		LEFT JOIN activities a ON a.id=p.activity_id
			AND a.publication_status='published' AND birdtie_activity_visible_to(a.id,$1::uuid)
			AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS (SELECT 1 FROM cities c WHERE c.id=a.city_id AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()))
			AND NOT EXISTS (SELECT 1 FROM account_blocks b WHERE a.host_account_id IS NOT NULL
				AND ((b.blocker_account_id=$1 AND b.blocked_account_id=a.host_account_id)
				 OR (b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$1)))
        LEFT JOIN places loc ON loc.id=a.place_id AND loc.city_id=a.city_id AND loc.publication_status='published'
          AND (loc.expires_at IS NULL OR loc.expires_at>clock_timestamp())
        LEFT JOIN venues venue ON venue.place_id=a.venue_place_id AND venue.place_id=loc.id AND venue.city_id=a.city_id
          AND venue.expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM venue_candidates vc WHERE vc.id=venue.source_candidate_id
              AND vc.status='approved' AND vc.place_id=venue.place_id AND vc.city_id=venue.city_id
              AND vc.reviewed_by=venue.reviewed_by AND vc.expires_at>clock_timestamp())
        WHERE p.participant_account_id=$1
		ORDER BY a.starts_at DESC NULLS LAST, p.updated_at DESC LIMIT 200`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]activityparticipation.Overview, 0)
	for rows.Next() {
		var item activityparticipation.Overview
		if err := rows.Scan(&item.ID, &item.ActivityID, &item.Status, &item.Title,
			&item.CityID, &item.StartsAt, &item.EndsAt, &item.ActivityStatus, &item.Available, &item.Modality, &item.PhysicalPlaceStatus, &item.PlaceID, &item.PlaceName, &item.VenuePlaceID, &item.TimeZone); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanParticipation(row scanner) (activityparticipation.Participation, error) {
	var item activityparticipation.Participation
	err := row.Scan(&item.ID, &item.ActivityID, &item.Status, &item.CancelledAt,
		&item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) GetParticipation(ctx context.Context, personID, activityID string) (activityparticipation.Participation, error) {
	item, err := scanParticipation(s.pool.QueryRow(ctx, `SELECT `+participationColumns+`
		FROM activity_participations WHERE participant_account_id=$1 AND activity_id=$2`, personID, activityID))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, activityparticipation.ErrNotFound
	}
	return item, err
}

func (s *Store) JoinActivity(ctx context.Context, personID, activityID string) (activityparticipation.Participation, bool, error) {
	return s.joinActivity(ctx, personID, activityID, nil, nil)
}
func (s *Store) JoinActivityBound(ctx context.Context, a ea.Access, id string, b ea.BoundCondition) (activityparticipation.Participation, bool, error) {
	if b.Kind != ea.Join || b.Operation != "JOIN" {
		return activityparticipation.Participation{}, false, ea.ErrInvalid
	}
	return s.joinActivity(ctx, a.Actor.ID, id, &a, &b)
}
func (s *Store) joinActivity(ctx context.Context, personID, activityID string, access *ea.Access, bound *ea.BoundCondition) (activityparticipation.Participation, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return activityparticipation.Participation{}, false, err
	}
	defer tx.Rollback(ctx)
	if access != nil {
		if e := s.lockEntityActionWriter(ctx, tx, *access); e != nil {
			return activityparticipation.Participation{}, false, e
		}
	}
	var capacity *int
	var publication, cityPublication string
	var visible bool
	var cancelledAt, expiresAt *time.Time
	var endsAt time.Time
	var blocked bool
	err = tx.QueryRow(ctx, `SELECT a.capacity, a.publication_status,
		a.cancelled_at, a.expires_at, a.ends_at, c.publication_status,
		birdtie_activity_visible_to(a.id,$2::uuid),
		EXISTS (SELECT 1 FROM account_blocks b WHERE a.host_account_id IS NOT NULL
			AND ((b.blocker_account_id=$2 AND b.blocked_account_id=a.host_account_id)
			  OR (b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$2)))
		FROM activities a JOIN cities c ON c.id=a.city_id
		WHERE a.id=$1 FOR UPDATE OF a`, activityID, personID).Scan(
		&capacity, &publication, &cancelledAt, &expiresAt, &endsAt,
		&cityPublication, &visible, &blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return activityparticipation.Participation{}, false, activityparticipation.ErrNotFound
	}
	if err != nil {
		return activityparticipation.Participation{}, false, err
	}
	if !visible || blocked {
		return activityparticipation.Participation{}, false, activityparticipation.ErrNotFound
	}
	var fence entityActionWriteFence
	if bound != nil {
		fence, err = s.checkEntityActionWrite(ctx, tx, *access, ea.Ref{Type: "activity", ID: activityID}, *bound)
		if err != nil {
			return activityparticipation.Participation{}, false, err
		}
	}
	if publication != "published" || cityPublication != "published" ||
		cancelledAt != nil || !endsAt.After(time.Now()) ||
		(expiresAt != nil && !expiresAt.After(time.Now())) {
		return activityparticipation.Participation{}, false, activityparticipation.ErrUnavailable
	}
	existing, err := scanParticipation(tx.QueryRow(ctx, `SELECT `+participationColumns+`
		FROM activity_participations WHERE participant_account_id=$1 AND activity_id=$2`, personID, activityID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return activityparticipation.Participation{}, false, err
	}
	if err == nil && (existing.Status == "going" || existing.Status == "pending") {
		if bound != nil {
			if e := s.finishEntityActionWrite(ctx, tx, fence); e != nil {
				return activityparticipation.Participation{}, false, e
			}
		}
		return existing, false, tx.Commit(ctx)
	}
	if capacity != nil {
		var occupied int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM activity_participations
			WHERE activity_id=$1 AND status IN ('going','pending')`, activityID).Scan(&occupied); err != nil {
			return activityparticipation.Participation{}, false, err
		}
		if occupied >= *capacity {
			return activityparticipation.Participation{}, false, activityparticipation.ErrFull
		}
	}
	joined, err := scanParticipation(tx.QueryRow(ctx, `INSERT INTO activity_participations
		(activity_id, participant_account_id, status) VALUES ($1,$2,'going')
		ON CONFLICT (activity_id,participant_account_id) DO UPDATE
		SET status='going', cancelled_at=NULL, updated_at=now()
		RETURNING `+participationColumns, activityID, personID))
	if err != nil {
		return activityparticipation.Participation{}, false, err
	}
	if err = insertDomainAudit(ctx, tx, personID, "join", "activity_participation", joined.ID, "human_rsvp", &activityID); err != nil {
		return activityparticipation.Participation{}, false, err
	}
	if bound != nil {
		var exact bool
		if e := tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(id=$3::uuid AND status='going') FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2`, activityID, personID, joined.ID).Scan(&exact); e != nil || !exact {
			return activityparticipation.Participation{}, false, ea.ErrChanged
		}
		if e := s.finishEntityActionWrite(ctx, tx, fence); e != nil {
			return activityparticipation.Participation{}, false, e
		}
	}
	return joined, true, tx.Commit(ctx)
}

func (s *Store) CancelParticipationBound(ctx context.Context, a ea.Access, id string, b ea.BoundCondition) (activityparticipation.Participation, error) {
	if b.Kind != ea.Join || b.Operation != "CANCEL_RSVP" {
		return activityparticipation.Participation{}, ea.ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return activityparticipation.Participation{}, ea.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if e = s.lockEntityActionWriter(ctx, tx, a); e != nil {
		return activityparticipation.Participation{}, e
	}
	var locked string
	if e = tx.QueryRow(ctx, `SELECT id FROM activities WHERE id=$1 FOR UPDATE`, id).Scan(&locked); e != nil {
		return activityparticipation.Participation{}, activityparticipation.ErrNotFound
	}
	previous, e := scanParticipation(tx.QueryRow(ctx, `SELECT `+participationColumns+` FROM activity_participations WHERE participant_account_id=$1 AND activity_id=$2 FOR UPDATE`, a.Actor.ID, id))
	if e != nil {
		return activityparticipation.Participation{}, activityparticipation.ErrNotFound
	}
	fence, e := s.checkEntityActionWrite(ctx, tx, a, ea.Ref{Type: "activity", ID: id}, b)
	if e != nil {
		return activityparticipation.Participation{}, e
	}
	out, e := scanParticipation(tx.QueryRow(ctx, `UPDATE activity_participations SET status='cancelled',cancelled_at=coalesce(cancelled_at,now()),updated_at=CASE WHEN status='cancelled' THEN updated_at ELSE now() END WHERE id=$1 AND participant_account_id=$2 AND activity_id=$3 RETURNING `+participationColumns, previous.ID, a.Actor.ID, id))
	if e != nil {
		return activityparticipation.Participation{}, e
	}
	if out.ID != previous.ID || out.Status != "cancelled" {
		return activityparticipation.Participation{}, ea.ErrChanged
	}
	if previous.Status != "cancelled" {
		if e = insertDomainAudit(ctx, tx, a.Actor.ID, "cancel", "activity_participation", out.ID, "human_rsvp", &id); e != nil {
			return activityparticipation.Participation{}, e
		}
	}
	if e = s.finishEntityActionWrite(ctx, tx, fence); e != nil {
		return activityparticipation.Participation{}, e
	}
	return out, tx.Commit(ctx)
}

func (s *Store) CancelParticipation(ctx context.Context, personID, activityID string) (activityparticipation.Participation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return activityparticipation.Participation{}, err
	}
	defer tx.Rollback(ctx)
	var item activityparticipation.Participation
	var changed bool
	err = tx.QueryRow(ctx, `WITH previous AS MATERIALIZED(
        SELECT `+participationColumns+` FROM activity_participations
        WHERE participant_account_id=$1 AND activity_id=$2 FOR UPDATE),
    changed AS (UPDATE activity_participations
		SET status='cancelled', cancelled_at=COALESCE(cancelled_at,now()),
			updated_at=CASE WHEN status='cancelled' THEN updated_at ELSE now() END
		WHERE id IN(SELECT id FROM previous) AND status<>'cancelled'
		RETURNING `+participationColumns+`)
    SELECT `+participationColumns+`,true FROM changed
    UNION ALL SELECT `+participationColumns+`,false FROM previous WHERE NOT EXISTS(SELECT 1 FROM changed)`, personID, activityID).Scan(
		&item.ID, &item.ActivityID, &item.Status, &item.CancelledAt, &item.CreatedAt, &item.UpdatedAt, &changed)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, activityparticipation.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if changed {
		if err = insertDomainAudit(ctx, tx, personID, "cancel", "activity_participation", item.ID, "human_rsvp", &activityID); err != nil {
			return item, err
		}
	}
	return item, tx.Commit(ctx)
}
