package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const activityCandidateColumns = `id, city_id, submitted_by,
    COALESCE(place_id::text, ''), title, summary, host_label,
    starts_at, ends_at, time_zone, source_label, source_url, rights_note,
    expires_at, status, COALESCE(reviewed_by::text, ''), reviewed_at,
    COALESCE(review_note, ''), COALESCE(resolved_activity_id::text, ''), created_at`

const boundCandidateColumns = activityCandidateColumns + `,
    CASE WHEN binding.person_account_id IS NOT NULL THEN 'PERSON'
         WHEN binding.community_id IS NOT NULL THEN 'COMMUNITY'
         WHEN binding.organization_id IS NOT NULL THEN 'ORGANIZATION'
         WHEN binding.business_id IS NOT NULL THEN 'BUSINESS' END,
    COALESCE(binding.person_account_id,binding.community_id,binding.organization_id,binding.business_id)::text`
const candidateBindingJoin = ` LEFT JOIN city_activity_candidate_organizers binding ON binding.candidate_id=activity_candidates.id `

func scanActivityCandidate(row scanner, bound ...bool) (cityseed.ActivityCandidate, error) {
	var c cityseed.ActivityCandidate
	var kind, id *string
	fields := []any{&c.ID, &c.CityID, &c.SubmittedBy, &c.PlaceID,
		&c.Title, &c.Summary, &c.HostLabel, &c.StartsAt, &c.EndsAt,
		&c.TimeZone, &c.SourceLabel, &c.SourceURL, &c.RightsNote,
		&c.ExpiresAt, &c.Status, &c.ReviewedBy, &c.ReviewedAt,
		&c.ReviewNote, &c.ResolvedActivityID, &c.CreatedAt}
	if len(bound) > 0 && bound[0] {
		fields = append(fields, &kind, &id)
	}
	err := row.Scan(fields...)
	if err == nil && (kind != nil || id != nil) {
		if kind == nil || id == nil {
			return cityseed.ActivityCandidate{}, cityseed.ErrInvalidOrganizer
		}
		c.Organizer, err = cityseed.NormalizeActivityOrganizer(&actorref.ActorRef{Type: actorref.Type(*kind), ID: *id})
	}
	return c, err
}

// Lock the existing city-editor membership and native account. This is domain
// authority, not a caller-supplied organizer or an Agent permission.
func lockCandidateEditor(ctx context.Context, tx pgx.Tx, actor, city string, reviewer bool) (*time.Time, error) {
	var until *time.Time
	err := tx.QueryRow(ctx, `SELECT c.expires_at FROM city_editor_memberships m
 JOIN cities c ON c.id=m.city_id JOIN accounts a ON a.id=m.account_id
 WHERE m.city_id=$1 AND m.account_id=$2 AND m.state='active'
 AND (NOT $3::boolean OR m.role='reviewer') AND a.status='active' AND a.account_type='person'
 AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 FOR SHARE OF m,c,a`, city, actor, reviewer).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, cityseed.ErrForbidden
	}
	return until, err
}

// Reuse the same native organizer/member domains as Activity publishing, with
// a stricter public-resource requirement for this public city-review path.
func candidateOrganizerHost(ctx context.Context, tx pgx.Tx, actor string, organizer *actorref.ActorRef, place string) (string, string, *time.Time, error) {
	if organizer == nil {
		return "", "", nil, cityseed.ErrOrganizerRequired
	}
	var host, organization string
	var until *time.Time
	var err error
	switch organizer.Type {
	case actorref.Person:
		if organizer.ID != actor {
			return "", "", nil, cityseed.ErrOrganizerUnavailable
		}
		err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR SHARE`, actor).Scan(&host)
	case actorref.Community:
		err = tx.QueryRow(ctx, `SELECT a.id::text,c.expires_at FROM communities c
 JOIN accounts owner ON owner.id=c.owner_account_id JOIN accounts a ON a.id=$2
 JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=a.id
 WHERE c.id=$1 AND c.lifecycle_status='active' AND c.publication_status='published' AND c.visibility='public'
 AND owner.status='active' AND a.status='active' AND a.account_type='person'
 AND m.status='active' AND m.role IN ('owner','admin') FOR SHARE OF c,owner,a,m`, organizer.ID, actor).Scan(&host, &until)
	case actorref.Organization:
		err = tx.QueryRow(ctx, `SELECT principal.id::text,o.id::text FROM organizations o
 JOIN accounts principal ON principal.id=o.account_id JOIN accounts a ON a.id=$2
 JOIN organization_memberships m ON m.organization_id=o.id AND m.user_account_id=a.id
 WHERE o.id=$1 AND o.status='active' AND o.visibility='public'
 AND principal.status='active' AND principal.account_type='organization' AND a.status='active' AND a.account_type='person'
 AND m.status='active' AND m.role IN ('owner','admin') FOR SHARE OF o,principal,a,m`, organizer.ID, actor).Scan(&host, &organization)
	case actorref.Business:
		err = tx.QueryRow(ctx, `SELECT principal.id::text FROM businesses b
 JOIN accounts principal ON principal.id=b.account_id JOIN accounts a ON a.id=$2
 JOIN business_memberships m ON m.business_id=b.id AND m.user_account_id=a.id
 WHERE b.id=$1 AND b.status='active' AND b.claim_status='verified'
 AND principal.status='active' AND principal.account_type='business' AND a.status='active' AND a.account_type='person'
 AND m.status='active' AND m.role IN ('owner','admin') FOR SHARE OF b,principal,a,m`, organizer.ID, actor).Scan(&host)
		if err == nil && place != "" {
			var target string
			err = tx.QueryRow(ctx, `SELECT r.place_id::text,v.expires_at FROM business_venue_relations r
 JOIN venues v ON v.place_id=r.place_id JOIN venue_candidates vc ON vc.id=v.source_candidate_id
 WHERE r.business_id=$1 AND r.place_id=$2 AND r.status='verified' AND vc.status='approved'
 AND birdtie_verified_business_venue(r.business_id,r.place_id) FOR SHARE OF r,v,vc`, organizer.ID, place).Scan(&target, &until)
		}
	default:
		return "", "", nil, cityseed.ErrInvalidOrganizer
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, cityseed.ErrOrganizerUnavailable
	}
	return host, organization, until, err
}

func candidateCurrentClock(ctx context.Context, tx pgx.Tx, limits ...*time.Time) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, err
	}
	for _, limit := range limits {
		if limit != nil && !limit.After(now) {
			return time.Time{}, cityseed.ErrOrganizerUnavailable
		}
	}
	return now, nil
}

func (s *Store) SubmitActivity(ctx context.Context, actorID, cityID string, input cityseed.ActivityInput) (cityseed.ActivityCandidate, error) {
	organizer, err := cityseed.NormalizeActivityOrganizer(input.Organizer)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	defer tx.Rollback(ctx)
	cityUntil, err := lockCandidateEditor(ctx, tx, actorID, cityID, false)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	var organizerUntil *time.Time
	if organizer != nil {
		_, _, organizerUntil, err = candidateOrganizerHost(ctx, tx, actorID, organizer, input.PlaceID)
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
	}
	if input.PlaceID != "" {
		var placeID string
		err = tx.QueryRow(ctx, `SELECT id FROM places
            WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
            FOR SHARE`, input.PlaceID, cityID).Scan(&placeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return cityseed.ActivityCandidate{}, cityseed.ErrConflict
		}
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
	}
	if _, err = candidateCurrentClock(ctx, tx, cityUntil, organizerUntil); err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	c, err := scanActivityCandidate(tx.QueryRow(ctx, `INSERT INTO activity_candidates (
        city_id, submitted_by, place_id, title, summary, host_label,
        starts_at, ends_at, time_zone, source_label, source_url, rights_note,
        expires_at
    )
    SELECT c.id, a.id, NULLIF($3, '')::uuid, $4, $5, $6,
           $7, $8, $9, $10, $11, $12, $13
    FROM city_editor_memberships m
    JOIN cities c ON c.id = m.city_id AND c.publication_status = 'published'
    JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
    WHERE m.city_id = $1 AND m.account_id = $2 AND m.state = 'active'
    RETURNING `+activityCandidateColumns,
		cityID, actorID, input.PlaceID, input.Title, input.Summary,
		input.HostLabel, input.StartsAt, input.EndsAt, input.TimeZone,
		input.SourceLabel, input.SourceURL, input.RightsNote, input.ExpiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.ActivityCandidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	if organizer != nil {
		var person, community, organization, business any
		switch organizer.Type {
		case actorref.Person:
			person = organizer.ID
		case actorref.Community:
			community = organizer.ID
		case actorref.Organization:
			organization = organizer.ID
		case actorref.Business:
			business = organizer.ID
		}
		_, err = tx.Exec(ctx, `INSERT INTO city_activity_candidate_organizers
 (candidate_id,selected_by,person_account_id,community_id,organization_id,business_id)
 VALUES($1,$2,$3,$4,$5,$6)`, c.ID, actorID, person, community, organization, business)
		if err != nil {
			// A native source can expire after the domain clock but before the
			// INSERT guard. Preserve a recoverable domain error for that gate;
			// unrelated PostgreSQL failures must still surface to observability.
			var guard *pgconn.PgError
			if errors.As(err, &guard) && guard.Code == "P0001" && strings.Contains(guard.Where, "birdtie_guard_city_candidate_organizer") {
				return cityseed.ActivityCandidate{}, cityseed.ErrOrganizerUnavailable
			}
			return cityseed.ActivityCandidate{}, err
		}
		c.Organizer = organizer
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'submit', 'activity_candidate', $2, 'allowed', 'city_seed')`,
		actorID, c.ID)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	return c, nil
}

func (s *Store) ListActivityCandidates(ctx context.Context, actorID, cityID string) ([]cityseed.ActivityCandidate, error) {
	const currentEditor = `SELECT EXISTS (
        SELECT 1 FROM city_editor_memberships m
        JOIN accounts a ON a.id = m.account_id
        JOIN cities c ON c.id = m.city_id
        WHERE m.city_id = $1 AND m.account_id = $2
		AND m.state = 'active' AND a.status = 'active' AND a.account_type='person'
		AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
    )`
	check := func() error {
		var allowed bool
		if err := s.pool.QueryRow(ctx, currentEditor, cityID, actorID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return cityseed.ErrForbidden
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+boundCandidateColumns+`
        FROM activity_candidates `+candidateBindingJoin+` WHERE city_id = $1 AND status = 'pending'
		AND EXISTS(SELECT 1 FROM city_editor_memberships m JOIN accounts a ON a.id=m.account_id
		 JOIN cities c ON c.id=m.city_id WHERE m.city_id=$1 AND m.account_id=$2
		 AND m.state='active' AND a.status='active' AND a.account_type='person'
		 AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()))
        ORDER BY created_at, id LIMIT 100`, cityID, actorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]cityseed.ActivityCandidate, 0)
	for rows.Next() {
		c, err := scanActivityCandidate(rows, true)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	// The preliminary gate is not authority for a payload returned after a
	// concurrent account/editor/city withdrawal. Never release the old rows.
	if err := check(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (s *Store) ReviewActivity(ctx context.Context, actorID, candidateID string, input cityseed.ActivityReviewInput) (cityseed.ActivityCandidate, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanActivityCandidate(tx.QueryRow(ctx, `SELECT `+boundCandidateColumns+`
        FROM activity_candidates `+candidateBindingJoin+` WHERE id = $1 AND city_id IN (
            SELECT m.city_id FROM city_editor_memberships m
            JOIN accounts a ON a.id = m.account_id
            WHERE m.account_id = $2 AND m.role = 'reviewer'
              AND m.state = 'active' AND a.status = 'active'
        ) FOR UPDATE OF activity_candidates`, candidateID, actorID), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.ActivityCandidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	if c.SubmittedBy == actorID || c.Status != "pending" {
		return cityseed.ActivityCandidate{}, cityseed.ErrConflict
	}
	cityUntil, err := lockCandidateEditor(ctx, tx, actorID, c.CityID, true)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	// Reject also writes a review/audit/inbox and returns a private candidate.
	// A lock wait must not let expired city authority authorize that branch.
	if _, err = candidateCurrentClock(ctx, tx, cityUntil); err != nil {
		if errors.Is(err, cityseed.ErrOrganizerUnavailable) {
			err = cityseed.ErrForbidden
		}
		return cityseed.ActivityCandidate{}, err
	}
	selectedOrganizer := c.Organizer
	var activityID any
	if input.Decision == "publish" {
		if selectedOrganizer == nil {
			return cityseed.ActivityCandidate{}, cityseed.ErrOrganizerRequired
		}
		var lockedBinding string
		if err = tx.QueryRow(ctx, `SELECT candidate_id::text FROM city_activity_candidate_organizers WHERE candidate_id=$1 FOR SHARE`, c.ID).Scan(&lockedBinding); err != nil {
			return cityseed.ActivityCandidate{}, cityseed.ErrOrganizerUnavailable
		}
		submitterCityUntil, checkErr := lockCandidateEditor(ctx, tx, c.SubmittedBy, c.CityID, false)
		if checkErr != nil {
			if errors.Is(checkErr, cityseed.ErrForbidden) {
				return cityseed.ActivityCandidate{}, cityseed.ErrOrganizerUnavailable
			}
			return cityseed.ActivityCandidate{}, checkErr
		}
		hostID, organizationID, organizerUntil, checkErr := candidateOrganizerHost(ctx, tx, c.SubmittedBy, selectedOrganizer, c.PlaceID)
		if checkErr != nil {
			return cityseed.ActivityCandidate{}, checkErr
		}
		var placeUntil *time.Time
		if c.PlaceID != "" {
			var placeID string
			err = tx.QueryRow(ctx, `SELECT id,expires_at FROM places
                WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
                FOR SHARE`, c.PlaceID, c.CityID).Scan(&placeID, &placeUntil)
			if errors.Is(err, pgx.ErrNoRows) {
				return cityseed.ActivityCandidate{}, cityseed.ErrConflict
			}
			if err != nil {
				return cityseed.ActivityCandidate{}, err
			}
		}
		current, checkErr := candidateCurrentClock(ctx, tx, cityUntil, submitterCityUntil, organizerUntil, placeUntil)
		if checkErr != nil {
			return cityseed.ActivityCandidate{}, checkErr
		}
		if !c.ExpiresAt.After(current) || !c.EndsAt.After(current) {
			return cityseed.ActivityCandidate{}, cityseed.ErrConflict
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO activities (
            host_account_id,organization_id,created_by_account_id,
            city_id, place_id, title, summary, host_label, starts_at, ends_at,
            time_zone, publication_status, source_label, source_ref,
            maintainer_label, verified_at, expires_at, published_at
        )
        SELECT $13::uuid,NULLIF($14,'')::uuid,$15::uuid,$1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7,
               $8, 'published', $9, $10,
               '城市维护者',
               clock_timestamp(), $11, clock_timestamp()
        FROM accounts a WHERE a.id = $12 AND a.status = 'active'
        RETURNING id`, c.CityID, c.PlaceID, c.Title, c.Summary,
			c.HostLabel, c.StartsAt, c.EndsAt, c.TimeZone,
			c.SourceLabel, c.SourceURL, c.ExpiresAt, actorID, hostID, organizationID, c.SubmittedBy).Scan(&id)
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
		activityID = id
		if selectedOrganizer.Type == actorref.Community {
			if _, err = tx.Exec(ctx, `UPDATE activity_organizers SET person_account_id=NULL,community_id=$2 WHERE activity_id=$1`, id, selectedOrganizer.ID); err != nil {
				return cityseed.ActivityCandidate{}, err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO activity_sources (
            activity_id, candidate_id, source_label, source_url, rights_note,
            reviewer_account_id, verified_at, expires_at
        ) VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp(), $7)`,
			id, c.ID, c.SourceLabel, c.SourceURL, c.RightsNote,
			actorID, c.ExpiresAt)
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO city_seed_activities (
            city_id, activity_id, maintainer_account_id, valid_until
        ) VALUES ($1, $2, $3, $4)`, c.CityID, id, actorID, c.ExpiresAt)
		if err != nil {
			return cityseed.ActivityCandidate{}, err
		}
	} else if input.Decision != "reject" {
		return cityseed.ActivityCandidate{}, cityseed.ErrConflict
	}
	status := "rejected"
	if activityID != nil {
		status = "published"
	}
	c, err = scanActivityCandidate(tx.QueryRow(ctx, `UPDATE activity_candidates
        SET status = $2, reviewed_by = $3, reviewed_at = clock_timestamp(),
            review_note = $4, resolved_activity_id = $5
        WHERE id = $1 AND status = 'pending'
        RETURNING `+activityCandidateColumns,
		candidateID, status, actorID, input.Note, activityID))
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	c.Organizer = selectedOrganizer
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'activity_candidate', $3, 'allowed', 'city_seed')`,
		actorID, status, candidateID)
	if err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	detail := "你的活动建议经审核未予发布。"
	if status == "published" {
		detail = "你的活动建议已发布，可在 Birdtie 的公开城市结果中查看。"
	}
	if err := insertReviewInboxItem(ctx, tx, c.SubmittedBy, "activity_candidate", c.ID,
		"活动建议审核结果", detail); err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	if published, ok := activityID.(string); ok {
		if err := routeOpportunityActivity(ctx, tx, published); err != nil {
			return cityseed.ActivityCandidate{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.ActivityCandidate{}, err
	}
	return c, nil
}
