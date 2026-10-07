package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/jackc/pgx/v5"
)

const socialActivityManager = `EXISTS(SELECT 1 FROM activity_organizers ao
    JOIN accounts actor ON actor.id=$2 AND actor.account_type='person' AND actor.status='active'
    LEFT JOIN communities c ON c.id=ao.community_id
    LEFT JOIN community_memberships cm ON cm.community_id=c.id AND cm.user_account_id=$2
       AND cm.status='active' AND cm.role IN ('owner','admin')
    LEFT JOIN organizations o ON o.id=ao.organization_id
    LEFT JOIN accounts orgprincipal ON orgprincipal.id=o.account_id
       AND orgprincipal.account_type='organization' AND orgprincipal.status='active'
    LEFT JOIN organization_memberships om ON om.organization_id=o.id AND om.user_account_id=$2
       AND om.status='active' AND om.role IN ('owner','admin')
    LEFT JOIN businesses b ON b.id=ao.business_id AND b.status='active' AND b.claim_status='verified'
    LEFT JOIN accounts principal ON principal.id=b.account_id AND principal.status='active'
    LEFT JOIN business_memberships bm ON bm.business_id=b.id AND bm.user_account_id=$2
       AND bm.status='active' AND bm.role IN ('owner','admin')
    WHERE ao.activity_id=a.id AND (ao.person_account_id=$2 OR
      (c.lifecycle_status='active' AND cm.id IS NOT NULL) OR
      (o.status='active' AND orgprincipal.id IS NOT NULL AND om.id IS NOT NULL) OR
      (b.id IS NOT NULL AND principal.id IS NOT NULL AND bm.id IS NOT NULL)))`

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) ListManagedBusinessOrganizers(ctx context.Context, actor string) ([]activitypublish.Organizer, error) {
	rows, err := s.pool.Query(ctx, `SELECT b.id,b.name FROM businesses b
		JOIN accounts principal ON principal.id=b.account_id AND principal.account_type='business'
		  AND principal.status='active'
		JOIN business_memberships m ON m.business_id=b.id AND m.user_account_id=$1
		  AND m.status='active' AND m.role IN ('owner','admin')
		WHERE b.status='active' AND b.claim_status='verified'
		ORDER BY b.name,b.id`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]activitypublish.Organizer, 0)
	for rows.Next() {
		item := activitypublish.Organizer{Type: "BUSINESS"}
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) getSocialManagedActivity(ctx context.Context, actor, id string) (activitypublish.Activity, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT `+socialActivityManager+` FROM activities a WHERE a.id=$1`, id, actor).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if !allowed {
		return activitypublish.Activity{}, activitypublish.ErrForbidden
	}
	// The preliminary check chooses the existing error contract. Recheck the
	// manager in the same current statement that projects its permitted label.
	out, err := scanManagedActivity(s.pool.QueryRow(ctx, `SELECT `+managedActivityColumns("$2")+`
        FROM activities a WHERE a.id=$1 AND `+socialActivityManager, id, actor))
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrForbidden
	}
	return out, err
}

func (s *Store) CreateSocialDraft(ctx context.Context, actor string, in activitypublish.Input) (activitypublish.Activity, error) {
	if in.Organizer.Type == "ORGANIZATION" {
		return s.CreateDraft(ctx, actor, in.Organizer.ID, in)
	}
	if in.Organizer.Type == "PERSON" && in.Organizer.ID != actor {
		return activitypublish.Activity{}, activitypublish.ErrForbidden
	}
	if in.Organizer.Type != "PERSON" && in.Organizer.Type != "COMMUNITY" && in.Organizer.Type != "BUSINESS" {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if in.Organizer.Type == "PERSON" && in.Visibility == "organizer_members" {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitypublish.Activity{}, err
	}
	defer tx.Rollback(ctx)
	var activePerson bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts
        WHERE id=$1 AND account_type='person' AND status='active')`, actor).Scan(&activePerson); err != nil {
		return activitypublish.Activity{}, err
	}
	if !activePerson {
		return activitypublish.Activity{}, activitypublish.ErrForbidden
	}
	var hostLabel string
	hostID := actor
	if in.Organizer.Type == "PERSON" {
		err = tx.QueryRow(ctx, `SELECT CASE
            WHEN p.visibility='public' AND birdtie_agent_profile_field_allowed(a.id,NULL,'displayName')
              THEN COALESCE(NULLIF(p.display_name,''),NULLIF(a.handle,''),'Birdtie 成员')
              ELSE 'Birdtie 成员' END
            FROM accounts a LEFT JOIN user_profiles p ON p.account_id=a.id
            WHERE a.id=$1 AND a.account_type='person' AND a.status='active'`, actor).Scan(&hostLabel)
	} else if in.Organizer.Type == "COMMUNITY" {
		err = tx.QueryRow(ctx, `SELECT c.name FROM communities c JOIN community_memberships m ON m.community_id=c.id
            WHERE c.id=$1 AND c.lifecycle_status='active' AND c.publication_status='published'
              AND m.user_account_id=$2 AND m.status='active' AND m.role IN ('owner','admin')
			FOR SHARE OF c,m`, in.Organizer.ID, actor).Scan(&hostLabel)
	} else {
		err = tx.QueryRow(ctx, `SELECT b.name,b.account_id FROM businesses b
            JOIN accounts principal ON principal.id=b.account_id
            JOIN business_memberships m ON m.business_id=b.id
            WHERE b.id=$1 AND b.status='active' AND b.claim_status='verified'
              AND principal.status='active' AND m.user_account_id=$2
              AND m.status='active' AND m.role IN ('owner','admin')
              AND ($3::uuid IS NULL OR birdtie_verified_business_venue(b.id,$3))
            FOR SHARE OF b,m`, in.Organizer.ID, actor, nullableUUID(in.PlaceID)).Scan(&hostLabel, &hostID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrForbidden
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	args := activityInputArgs(actor, "", "", in)
	args["hostLabel"] = hostLabel
	args["hostID"] = hostID
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO activities
        (host_account_id,created_by_account_id,city_id,place_id,modality,
         physical_place_status,venue_place_id,title,summary,description,
         starts_at,ends_at,time_zone,category_code,capacity,price_minor,currency,
         eligibility,language_code,visibility,host_label,source_label,source_ref,
         maintainer_label,publication_status)
		SELECT @hostID,@userID,c.id,@placeID,@modality,@physicalPlaceStatus,
           @venuePlaceID,@title,@summary,@description,
           @startsAt,@endsAt,@timeZone,@categoryCode,@capacity,@priceMinor,@currency,
           @eligibility,@languageCode,@visibility,@hostLabel,'Birdtie member',
           'birdtie:activity:' || gen_random_uuid()::text,@hostLabel,'draft'
        FROM cities c WHERE c.id=@cityID AND c.publication_status='published'
        RETURNING id`, args).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if in.Organizer.Type == "COMMUNITY" {
		_, err = tx.Exec(ctx, `UPDATE activity_organizers SET person_account_id=NULL,community_id=$2
            WHERE activity_id=$1`, id, in.Organizer.ID)
		if err != nil {
			return activitypublish.Activity{}, err
		}
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,'activity_create','activity',$2,'allowed','social_activity')`, actor, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return activitypublish.Activity{}, err
	}
	return s.getSocialManagedActivity(ctx, actor, id)
}

func (s *Store) socialActivityLock(ctx context.Context, tx pgx.Tx, actor, id string) (string, time.Time, time.Time, *string, error) {
	var state string
	var starts, ends time.Time
	var place *string
	err := tx.QueryRow(ctx, `SELECT publication_status,starts_at,ends_at,place_id
        FROM activities WHERE id=$1 FOR UPDATE`, id).Scan(&state, &starts, &ends, &place)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", starts, ends, nil, activitypublish.ErrConflict
	}
	if err != nil {
		return "", starts, ends, nil, err
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT `+socialActivityManager+` FROM activities a WHERE a.id=$1`, id, actor).Scan(&allowed)
	if err != nil {
		return "", starts, ends, nil, err
	}
	if !allowed {
		return "", starts, ends, nil, activitypublish.ErrForbidden
	}
	return state, starts, ends, place, nil
}

func (s *Store) UpdateSocialActivity(ctx context.Context, actor, id string, in activitypublish.Input) (activitypublish.Activity, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitypublish.Activity{}, err
	}
	defer tx.Rollback(ctx)
	state, oldStart, oldEnd, oldPlace, err := s.socialActivityLock(ctx, tx, actor, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if state != "draft" && state != "published" {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	var org, comm, person, biz *string
	err = tx.QueryRow(ctx, `SELECT organization_id,community_id,person_account_id,business_id FROM activity_organizers WHERE activity_id=$1`, id).Scan(&org, &comm, &person, &biz)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if in.Organizer.Type != "" {
		match := (in.Organizer.Type == "PERSON" && person != nil && *person == in.Organizer.ID) ||
			(in.Organizer.Type == "COMMUNITY" && comm != nil && *comm == in.Organizer.ID) ||
			(in.Organizer.Type == "ORGANIZATION" && org != nil && *org == in.Organizer.ID) ||
			(in.Organizer.Type == "BUSINESS" && biz != nil && *biz == in.Organizer.ID)
		if !match {
			return activitypublish.Activity{}, activitypublish.ErrConflict
		}
	}
	if in.Visibility == "organizer_members" && person != nil {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if biz != nil && in.PlaceID != "" {
		var permitted bool
		err = tx.QueryRow(ctx, `SELECT birdtie_verified_business_venue($1,$2)`, *biz, in.PlaceID).Scan(&permitted)
		if err != nil {
			return activitypublish.Activity{}, err
		}
		if !permitted {
			return activitypublish.Activity{}, activitypublish.ErrConflict
		}
	}
	if state == "published" && (in.StartsAt.Before(time.Now()) || in.Visibility == "private" || in.Visibility == "unlisted") {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	args := activityInputArgs(actor, "", id, in)
	var updated string
	err = tx.QueryRow(ctx, `UPDATE activities SET place_id=@placeID,modality=@modality,
        physical_place_status=@physicalPlaceStatus,venue_place_id=@venuePlaceID,
        title=@title,summary=@summary,
        description=@description,starts_at=@startsAt,ends_at=@endsAt,time_zone=@timeZone,
        category_code=@categoryCode,capacity=@capacity,price_minor=@priceMinor,currency=@currency,
        eligibility=@eligibility,language_code=@languageCode,visibility=@visibility,
        revision=revision+1,updated_at=now()
        WHERE id=@activityID AND city_id=@cityID AND cancelled_at IS NULL
        RETURNING id`, args).Scan(&updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if state == "published" {
		if !oldStart.Equal(in.StartsAt) || !oldEnd.Equal(in.EndsAt) {
			if _, err = tx.Exec(ctx, `DELETE FROM inbox_items WHERE target_activity_id=$1 AND resource_type='activity_reminder'`, id); err != nil {
				return activitypublish.Activity{}, err
			}
			if err = insertActivityChangeInbox(ctx, tx, id, "activity_change", "活动时间已调整", in.Title+" 的时间已变更，请查看最新安排。"); err != nil {
				return activitypublish.Activity{}, err
			}
		}
		if (oldPlace == nil && in.PlaceID != "") || (oldPlace != nil && *oldPlace != in.PlaceID) {
			if err = insertActivityChangeInbox(ctx, tx, id, "activity_change", "活动地点已调整", in.Title+" 的地点已变更，请查看最新安排。"); err != nil {
				return activitypublish.Activity{}, err
			}
		}
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,'activity_update','activity',$2,'allowed','social_activity')`, actor, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if state == "published" {
		if err = routeOpportunityActivity(ctx, tx, id); err != nil {
			return activitypublish.Activity{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return activitypublish.Activity{}, err
	}
	return s.getSocialManagedActivity(ctx, actor, id)
}

func (s *Store) PublishSocialActivity(ctx context.Context, actor, id string) (activitypublish.Activity, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitypublish.Activity{}, err
	}
	defer tx.Rollback(ctx)
	state, _, _, _, err := s.socialActivityLock(ctx, tx, actor, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if state != "draft" {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE activities a SET publication_status='published',published_at=now(),
        revision=revision+1,updated_at=now() FROM cities c
        WHERE a.id=$1 AND c.id=a.city_id AND c.publication_status='published'
          AND a.cancelled_at IS NULL AND a.starts_at>now() AND a.ends_at>a.starts_at
          AND a.visibility IN ('public','organizer_members','invite_only')
          `+activityLocationPublishGuard, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if tag.RowsAffected() == 0 {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,'activity_publish','activity',$2,'allowed','social_activity')`, actor, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if err = routeOpportunityActivity(ctx, tx, id); err != nil {
		return activitypublish.Activity{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return activitypublish.Activity{}, err
	}
	return s.getSocialManagedActivity(ctx, actor, id)
}

func (s *Store) CancelSocialActivity(ctx context.Context, actor, id string) (activitypublish.Activity, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitypublish.Activity{}, err
	}
	defer tx.Rollback(ctx)
	state, _, _, _, err := s.socialActivityLock(ctx, tx, actor, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if state != "published" {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE activities SET cancelled_at=now(),revision=revision+1,updated_at=now()
        WHERE id=$1 AND cancelled_at IS NULL AND ends_at>now()`, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if tag.RowsAffected() == 0 {
		return activitypublish.Activity{}, activitypublish.ErrConflict
	}
	if _, err = tx.Exec(ctx, `DELETE FROM inbox_items WHERE target_activity_id=$1 AND resource_type='activity_reminder'`, id); err != nil {
		return activitypublish.Activity{}, err
	}
	var title string
	if err = tx.QueryRow(ctx, `SELECT title FROM activities WHERE id=$1`, id).Scan(&title); err != nil {
		return activitypublish.Activity{}, err
	}
	if err = insertActivityChangeInbox(ctx, tx, id, "activity_cancelled", "活动已取消", title+" 已由主办方取消，请查看活动详情。"); err != nil {
		return activitypublish.Activity{}, err
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,'activity_cancel','activity',$2,'allowed','social_activity')`, actor, id)
	if err != nil {
		return activitypublish.Activity{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return activitypublish.Activity{}, err
	}
	return s.getSocialManagedActivity(ctx, actor, id)
}

func (s *Store) ListSocialActivities(ctx context.Context, actor string) ([]activitypublish.Activity, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+managedActivityColumns("$1")+` FROM activities a
        WHERE `+strings.ReplaceAll(socialActivityManager, "$2", "$1")+` ORDER BY a.created_at DESC,a.id DESC LIMIT 100`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]activitypublish.Activity, 0)
	for rows.Next() {
		item, e := scanManagedActivity(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) InviteActivityPerson(ctx context.Context, actor, id, target string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, _, _, _, err = s.socialActivityLock(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	var visibility string
	if err = tx.QueryRow(ctx, `SELECT visibility FROM activities WHERE id=$1`, id).Scan(&visibility); err != nil {
		return err
	}
	if visibility != "invite_only" {
		return activitypublish.ErrConflict
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND account_type='person' AND status='active')`, target).Scan(&active); err != nil {
		return err
	}
	if !active {
		return activitypublish.ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO activity_invitations(activity_id,invitee_account_id,invited_by_account_id)
        VALUES($1,$2,$3) ON CONFLICT(activity_id,invitee_account_id)
        DO UPDATE SET status='invited',invited_by_account_id=$3`, id, target, actor)
	if err != nil {
		return err
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,'activity_invite','activity',$2,'allowed','social_activity')`, actor, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
