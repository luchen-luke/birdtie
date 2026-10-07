package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/jackc/pgx/v5"
)

func managedActivityColumns(viewerSQL string) string {
	return `a.id, COALESCE(a.organization_id::text,''), a.city_id, a.place_id, a.title,
    a.summary, a.description, a.starts_at, a.ends_at, a.time_zone,
    a.category_code, a.capacity, a.price_minor, a.currency, a.eligibility,
    a.language_code, a.visibility, a.publication_status, a.published_at,
    a.cancelled_at, a.revision, a.modality, a.physical_place_status, a.venue_place_id,
    COALESCE((SELECT CASE WHEN ao.person_account_id IS NOT NULL THEN 'PERSON'
                          WHEN ao.community_id IS NOT NULL THEN 'COMMUNITY'
                          WHEN ao.business_id IS NOT NULL THEN 'BUSINESS'
                          ELSE 'ORGANIZATION' END
              FROM activity_organizers ao WHERE ao.activity_id=a.id),''),
    COALESCE((SELECT COALESCE(ao.person_account_id::text,ao.community_id::text,ao.organization_id::text,ao.business_id::text)
              FROM activity_organizers ao WHERE ao.activity_id=a.id),''),
    COALESCE((SELECT CASE WHEN ao.person_account_id IS NOT NULL THEN ` + projectedActivityLabelSQL(viewerSQL, "a.host_label") + `
                          WHEN ao.community_id IS NOT NULL THEN c.name
                          WHEN ao.business_id IS NOT NULL THEN b.name ELSE o.name END
              FROM activity_organizers ao
              LEFT JOIN communities c ON c.id=ao.community_id
              LEFT JOIN organizations o ON o.id=ao.organization_id
              LEFT JOIN businesses b ON b.id=ao.business_id
              WHERE ao.activity_id=a.id),a.host_label),
    (SELECT c.avatar_url FROM activity_organizers ao JOIN communities c ON c.id=ao.community_id
     WHERE ao.activity_id=a.id)`
}

const activityLocationPublishGuard = `
    AND (a.physical_place_status <> 'confirmed' OR EXISTS (
      SELECT 1 FROM places p WHERE p.id=a.place_id AND p.city_id=a.city_id
        AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>now())
    ))
    AND (a.venue_place_id IS NULL OR EXISTS (
      SELECT 1 FROM venues v JOIN venue_candidates vc ON vc.id=v.source_candidate_id
      WHERE v.place_id=a.venue_place_id AND v.city_id=a.city_id
        AND v.expires_at>now() AND vc.status='approved'
    ))`

func scanManagedActivity(row scanner) (activitypublish.Activity, error) {
	var out activitypublish.Activity
	err := row.Scan(&out.ID, &out.OrganizationID, &out.CityID, &out.PlaceID,
		&out.Title, &out.Summary, &out.Description, &out.StartsAt, &out.EndsAt,
		&out.TimeZone, &out.CategoryCode, &out.Capacity, &out.PriceMinor,
		&out.Currency, &out.Eligibility, &out.LanguageCode, &out.Visibility,
		&out.PublicationStatus, &out.PublishedAt, &out.CancelledAt, &out.Revision,
		&out.Modality, &out.PhysicalPlaceStatus, &out.VenuePlaceID,
		&out.Organizer.Type, &out.Organizer.ID, &out.Organizer.Name, &out.Organizer.AvatarURL)
	return out, err
}

func activityInputArgs(userID, organizationID, activityID string, input activitypublish.Input) pgx.NamedArgs {
	if input.Modality == "" && input.PhysicalPlaceStatus == "" {
		if input.PlaceID == "" {
			input.Modality, input.PhysicalPlaceStatus = "unspecified", "unknown"
		} else {
			input.Modality, input.PhysicalPlaceStatus = "in_person", "confirmed"
		}
	}
	var placeID, categoryCode, currency, languageCode any
	if input.PlaceID != "" {
		placeID = input.PlaceID
	}
	if input.CategoryCode != "" {
		categoryCode = input.CategoryCode
	}
	if input.Currency != "" {
		currency = input.Currency
	}
	if input.LanguageCode != "" {
		languageCode = input.LanguageCode
	}
	return pgx.NamedArgs{
		"userID": userID, "organizationID": organizationID, "activityID": activityID,
		"cityID": input.CityID, "placeID": placeID, "title": input.Title,
		"modality": input.Modality, "physicalPlaceStatus": input.PhysicalPlaceStatus,
		"venuePlaceID": nullableUUID(input.VenuePlaceID),
		"summary":      input.Summary, "description": input.Description,
		"startsAt": input.StartsAt, "endsAt": input.EndsAt, "timeZone": input.TimeZone,
		"categoryCode": categoryCode, "capacity": input.Capacity,
		"priceMinor": input.PriceMinor, "currency": currency,
		"eligibility": input.Eligibility, "languageCode": languageCode,
		"visibility": input.Visibility,
	}
}

func (s *Store) organizationAdmin(ctx context.Context, userID, organizationID string) error {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM organizations o
        JOIN accounts principal ON principal.id = o.account_id
        JOIN organization_memberships m ON m.organization_id = o.id
        JOIN accounts u ON u.id = m.user_account_id
        WHERE o.id = $1 AND o.status = 'active'
          AND principal.account_type = 'organization' AND principal.status = 'active'
          AND m.user_account_id = $2 AND m.status = 'active'
          AND m.role IN ('owner', 'admin')
          AND u.account_type = 'person' AND u.status = 'active')`,
		organizationID, userID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return activitypublish.ErrForbidden
	}
	return nil
}

func (s *Store) CreateDraft(ctx context.Context, userID, organizationID string, input activitypublish.Input) (activitypublish.Activity, error) {
	if err := s.organizationAdmin(ctx, userID, organizationID); err != nil {
		return activitypublish.Activity{}, err
	}
	args := activityInputArgs(userID, organizationID, "", input)
	return scanManagedActivity(auditPoolQueryRow(ctx, s.pool, `WITH inserted AS (
        INSERT INTO activities (organization_id, host_account_id, created_by_account_id,
          city_id, place_id, modality, physical_place_status, venue_place_id,
          title, summary, description, starts_at, ends_at,
          time_zone, category_code, capacity, price_minor, currency, eligibility,
          language_code, visibility, host_label, source_label, source_ref,
          maintainer_label, publication_status)
        SELECT o.id, o.account_id, @userID, c.id, @placeID, @modality,
          @physicalPlaceStatus, @venuePlaceID, @title, @summary,
          @description, @startsAt, @endsAt, @timeZone, @categoryCode,
          @capacity, @priceMinor, @currency, @eligibility, @languageCode,
          @visibility, o.name, 'Birdtie Organization',
          'birdtie:organization:' || o.id::text, o.name, 'draft'
        FROM organizations o
        JOIN accounts principal ON principal.id = o.account_id
        JOIN organization_memberships m ON m.organization_id = o.id
        JOIN accounts u ON u.id = m.user_account_id
        JOIN cities c ON c.id = @cityID AND c.publication_status = 'published'
        WHERE o.id = @organizationID AND o.status = 'active'
          AND principal.account_type = 'organization' AND principal.status = 'active'
          AND m.user_account_id = @userID AND m.status = 'active'
          AND m.role IN ('owner', 'admin') AND u.status = 'active'
          AND u.account_type = 'person'
        RETURNING *
    ), logged AS (
      INSERT INTO admin_audit_events
        (actor_account_id,organization_id,resource_type,resource_id,action)
      SELECT @userID,@organizationID,'activity',id,'activity_create' FROM inserted
      RETURNING id
    ) SELECT `+managedActivityColumns("@userID")+` FROM inserted a JOIN logged l ON true`, args))
}

func (s *Store) UpdateActivity(ctx context.Context, userID, organizationID, activityID string, input activitypublish.Input) (activitypublish.Activity, error) {
	if err := s.organizationAdmin(ctx, userID, organizationID); err != nil {
		return activitypublish.Activity{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitypublish.Activity{}, err
	}
	defer tx.Rollback(ctx)
	var oldStatus string
	var oldStart, oldEnd time.Time
	var oldPlace *string
	err = tx.QueryRow(ctx, `SELECT publication_status, starts_at, ends_at, place_id
		FROM activities WHERE id=$1 AND organization_id=$2 FOR UPDATE`, activityID, organizationID).
		Scan(&oldStatus, &oldStart, &oldEnd, &oldPlace)
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	args := activityInputArgs(userID, organizationID, activityID, input)
	out, err := scanManagedActivity(tx.QueryRow(ctx, `WITH updated AS (
        UPDATE activities a SET place_id = @placeID, modality = @modality,
          physical_place_status = @physicalPlaceStatus, venue_place_id = @venuePlaceID,
          title = @title,
          summary = @summary, description = @description, starts_at = @startsAt,
          ends_at = @endsAt, time_zone = @timeZone, category_code = @categoryCode,
          capacity = @capacity, price_minor = @priceMinor, currency = @currency,
          eligibility = @eligibility, language_code = @languageCode,
          visibility = @visibility, revision = revision + 1, updated_at = now()
        FROM organization_memberships m, organizations o, accounts principal
        WHERE a.id = @activityID AND a.organization_id = @organizationID
          AND o.id = a.organization_id AND o.status = 'active'
          AND principal.id = o.account_id AND principal.account_type = 'organization'
          AND principal.status = 'active'
          AND a.city_id = @cityID AND a.cancelled_at IS NULL
          AND (a.publication_status = 'draft' OR
			   (a.publication_status = 'published' AND @visibility IN ('public','organizer_members','invite_only')
                AND a.starts_at > now()
                AND @startsAt::timestamptz > now()))
          AND m.organization_id = a.organization_id AND m.user_account_id = @userID
          AND m.status = 'active' AND m.role IN ('owner', 'admin')
        RETURNING a.*
    ) SELECT `+managedActivityColumns("@userID")+` FROM updated a`, args))
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if oldStatus == "published" {
		if !oldStart.Equal(input.StartsAt) || !oldEnd.Equal(input.EndsAt) {
			if _, err := tx.Exec(ctx, `DELETE FROM inbox_items WHERE target_activity_id=$1
				AND resource_type='activity_reminder'`, activityID); err != nil {
				return activitypublish.Activity{}, err
			}
			if err := insertActivityChangeInbox(ctx, tx, activityID, "activity_change",
				"活动时间已调整", out.Title+" 的时间已变更，请查看最新安排。"); err != nil {
				return activitypublish.Activity{}, err
			}
		}
		placeChanged := (oldPlace == nil && input.PlaceID != "") ||
			(oldPlace != nil && *oldPlace != input.PlaceID)
		if placeChanged {
			if err := insertActivityChangeInbox(ctx, tx, activityID, "activity_change",
				"活动地点已调整", out.Title+" 的地点已变更，请查看最新安排。"); err != nil {
				return activitypublish.Activity{}, err
			}
		}
	}
	if oldStatus == "published" {
		if err := routeOpportunityActivity(ctx, tx, activityID); err != nil {
			return activitypublish.Activity{}, err
		}
	}
	if err := insertAdminAudit(ctx, tx, userID, organizationID, "activity", out.ID, "activity_update"); err != nil {
		return activitypublish.Activity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return activitypublish.Activity{}, err
	}
	return out, nil
}

func (s *Store) PublishActivity(ctx context.Context, userID, organizationID, activityID string) (activitypublish.Activity, error) {
	if err := s.organizationAdmin(ctx, userID, organizationID); err != nil {
		return activitypublish.Activity{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitypublish.Activity{}, err
	}
	defer tx.Rollback(ctx)
	out, err := scanManagedActivity(tx.QueryRow(ctx, `WITH updated AS (
        UPDATE activities a SET publication_status = 'published',
          published_at = now(), revision = revision + 1, updated_at = now()
        FROM organization_memberships m, cities c, organizations o, accounts principal
        WHERE a.id = $1 AND a.organization_id = $2
          AND o.id = a.organization_id AND o.status = 'active'
          AND principal.id = o.account_id AND principal.account_type = 'organization'
          AND principal.status = 'active'
          AND a.publication_status = 'draft' AND a.cancelled_at IS NULL
          AND a.starts_at > now() AND a.ends_at > a.starts_at
		  AND a.visibility IN ('public','organizer_members','invite_only')
          AND c.id = a.city_id AND c.publication_status = 'published'
		  `+activityLocationPublishGuard+`
          AND m.organization_id = a.organization_id AND m.user_account_id = $3
          AND m.status = 'active' AND m.role IN ('owner', 'admin')
        RETURNING a.*
    ), logged AS (
      INSERT INTO admin_audit_events
        (actor_account_id,organization_id,resource_type,resource_id,action)
      SELECT $3,$2,'activity',id,'activity_publish' FROM updated
      RETURNING id
    ) SELECT `+managedActivityColumns("$3")+` FROM updated a JOIN logged l ON true`, activityID, organizationID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if err = routeOpportunityActivity(ctx, tx, activityID); err != nil {
		return activitypublish.Activity{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return activitypublish.Activity{}, err
	}
	return out, nil
}

func (s *Store) CancelActivity(ctx context.Context, userID, organizationID, activityID string) (activitypublish.Activity, error) {
	if err := s.organizationAdmin(ctx, userID, organizationID); err != nil {
		return activitypublish.Activity{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitypublish.Activity{}, err
	}
	defer tx.Rollback(ctx)
	out, err := scanManagedActivity(tx.QueryRow(ctx, `WITH updated AS (
        UPDATE activities a SET cancelled_at = now(), revision = revision + 1,
          updated_at = now()
        FROM organization_memberships m, organizations o, accounts principal
        WHERE a.id = $1 AND a.organization_id = $2
          AND o.id = a.organization_id AND o.status = 'active'
          AND principal.id = o.account_id AND principal.account_type = 'organization'
          AND principal.status = 'active'
          AND a.publication_status = 'published' AND a.cancelled_at IS NULL
          AND a.ends_at > now()
          AND m.organization_id = a.organization_id AND m.user_account_id = $3
          AND m.status = 'active' AND m.role IN ('owner', 'admin')
        RETURNING a.*
    ) SELECT `+managedActivityColumns("$3")+` FROM updated a`, activityID, organizationID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM inbox_items WHERE target_activity_id=$1
		AND resource_type='activity_reminder'`, activityID); err != nil {
		return activitypublish.Activity{}, err
	}
	if err := insertActivityChangeInbox(ctx, tx, activityID, "activity_cancelled",
		"活动已取消", out.Title+" 已由主办方取消，请查看活动详情。"); err != nil {
		return activitypublish.Activity{}, err
	}
	if err := insertAdminAudit(ctx, tx, userID, organizationID, "activity", out.ID, "activity_cancel"); err != nil {
		return activitypublish.Activity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return activitypublish.Activity{}, err
	}
	return out, nil
}

func (s *Store) ListManagedActivities(ctx context.Context, userID, organizationID string) ([]activitypublish.Activity, error) {
	if err := s.organizationAdmin(ctx, userID, organizationID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+managedActivityColumns("$2")+` FROM activities a
        JOIN organization_memberships m ON m.organization_id = a.organization_id
        JOIN organizations o ON o.id = a.organization_id AND o.status = 'active'
        WHERE a.organization_id = $1 AND m.user_account_id = $2
          AND m.status = 'active' AND m.role IN ('owner', 'admin')
        ORDER BY a.created_at DESC, a.id DESC LIMIT 100`, organizationID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]activitypublish.Activity, 0)
	for rows.Next() {
		item, err := scanManagedActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
