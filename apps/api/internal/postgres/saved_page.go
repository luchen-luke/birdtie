package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
	"github.com/jackc/pgx/v5"
)

var _ saved.HumanListStore = (*Store)(nil)
var _ saved.HumanPageStore = (*Store)(nil)
var _ saved.HumanTargetStore = (*Store)(nil)

// Visibility applies only to the original entity projection. Private notes
// belong to the human bookmark and never grant source access or enter its body.
var humanSavedRowsSQL = `WITH anchor AS MATERIALIZED (
 SELECT id,created_at FROM saved_items WHERE owner_account_id=$1 AND id=$2::uuid
)
SELECT s.id,
 CASE WHEN s.place_id IS NOT NULL THEN 'place' WHEN s.activity_id IS NOT NULL THEN 'activity' ELSE 'group' END,
 COALESCE(s.place_id,s.activity_id,s.community_id)::text,
 COALESCE(p.name,a.title,g.name,''),COALESCE(p.summary,a.summary,g.summary,''),
 COALESCE(p.city_id,a.city_id,g.city_id,''),
 (p.id IS NOT NULL OR a.id IS NOT NULL OR g.id IS NOT NULL),s.created_at,
 COALESCE(n.note,''),COALESCE(n.revision,0),
 COALESCE((SELECT c.name FROM cities c WHERE c.id=COALESCE(p.city_id,a.city_id,g.city_id)
  AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp())),''),
 CASE WHEN g.id IS NOT NULL THEN birdtie_community_avatar_metadata(g.id,$1::uuid) END,
 CASE WHEN p.location_precision='point' AND p.coordinate_system='wgs84' AND p.latitude BETWEEN -90 AND 90 AND p.longitude BETWEEN -180 AND 180 THEN p.latitude END,
 CASE WHEN p.location_precision='point' AND p.coordinate_system='wgs84' AND p.latitude BETWEEN -90 AND 90 AND p.longitude BETWEEN -180 AND 180 THEN p.longitude END,
 CASE WHEN p.location_precision='point' AND p.coordinate_system='wgs84' AND p.latitude BETWEEN -90 AND 90 AND p.longitude BETWEEN -180 AND 180 THEN
 least(clock_timestamp()+interval '60 seconds',p.expires_at,
  (SELECT c.expires_at FROM cities c WHERE c.id=p.city_id),` + currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()") + `) END,
 jsonb_build_array(s.xmin::text,n.xmin::text,p.xmin::text,a.xmin::text,g.xmin::text,
  (SELECT owner.xmin::text FROM accounts owner WHERE owner.id=coalesce(a.host_account_id,g.owner_account_id)),
  (SELECT c.xmin::text FROM cities c WHERE c.id=coalesce(p.city_id,a.city_id,g.city_id)),
  (SELECT ca.xmin::text FROM community_avatars ca WHERE ca.community_id=g.id),` + retainedPlaceReadBindingSQL("p") + `,
  CASE WHEN a.id IS NOT NULL THEN birdtie_entity_cover_authority('activity',a.id,$1::uuid) END,
  (SELECT cover.xmin::text FROM entity_covers cover WHERE cover.entity_type='activity' AND cover.entity_id=a.id),
  CASE WHEN p.id IS NOT NULL THEN birdtie_place_photo_proof(p.id) END)::text,
 CASE WHEN a.id IS NOT NULL THEN birdtie_entity_cover_metadata('activity',a.id,$1::uuid,clock_timestamp()) END,
 CASE WHEN p.id IS NOT NULL THEN birdtie_place_photo_metadata(p.id,clock_timestamp()) END
FROM saved_items s
LEFT JOIN private_saved_notes n ON n.saved_item_id=s.id
LEFT JOIN places p ON p.id=s.place_id AND p.publication_status='published'
 AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp())
 AND ` + currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()") + `
 AND EXISTS(SELECT 1 FROM cities c WHERE c.id=p.city_id AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()))
LEFT JOIN activities a ON a.id=s.activity_id AND a.publication_status='published' AND a.visibility='public'
 AND(a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM cities c WHERE c.id=a.city_id AND c.publication_status='published')
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE a.host_account_id IS NOT NULL AND
  ((b.blocker_account_id=$1 AND b.blocked_account_id=a.host_account_id) OR(b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$1)))
LEFT JOIN communities g ON g.id=s.community_id AND g.publication_status='published' AND g.visibility='public' AND g.lifecycle_status='active'
 AND g.owner_confirmed_at IS NOT NULL AND(g.expires_at IS NULL OR g.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM accounts owner WHERE owner.id=g.owner_account_id AND owner.status='active')
 AND EXISTS(SELECT 1 FROM cities c WHERE c.id=g.city_id AND c.publication_status='published')
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
  (b.blocker_account_id=$1 AND b.blocked_account_id=g.owner_account_id) OR(b.blocker_account_id=g.owner_account_id AND b.blocked_account_id=$1))
WHERE s.owner_account_id=$1
 AND($2::uuid IS NULL OR(s.created_at,s.id)<(SELECT created_at,id FROM anchor))
ORDER BY s.created_at DESC,s.id DESC LIMIT $3`

func savedPageNativeFailure(err error) error {
	if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, saved.ErrInvalidPage) || errors.Is(err, saved.ErrPageChanged) {
		return err
	}
	return saved.ErrPageUnavailable
}

func humanSavedRead(ctx context.Context, tx pgx.Tx, owner, cursor string, limit int) ([]saved.Item, error) {
	var anchor any
	if cursor != "" {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM saved_items WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, cursor, owner).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return nil, saved.ErrPageChanged
		} else if err != nil {
			return nil, savedPageNativeFailure(err)
		}
		anchor = cursor
	}
	rows, err := tx.Query(ctx, humanSavedRowsSQL, owner, anchor, limit)
	if err != nil {
		return nil, savedPageNativeFailure(err)
	}
	defer rows.Close()
	items := []saved.Item{}
	for rows.Next() {
		var item saved.Item
		var privateSource string
		if err := rows.Scan(&item.ID, &item.Kind, &item.TargetID, &item.Title, &item.Summary, &item.CityID, &item.Available, &item.SavedAt, &item.Note, &item.NoteRevision, &item.CityName, &item.CommunityAvatar, &item.Latitude, &item.Longitude, &item.SourceExpiresAt, &privateSource, &item.Cover, &item.PlacePhoto); err != nil {
			return nil, savedPageNativeFailure(err)
		}
		if !item.ValidCommunityMedia() {
			return nil, saved.ErrPageUnavailable
		}
		item.SavedAt = item.SavedAt.UTC()
		if item.SourceExpiresAt != nil {
			stamp := item.SourceExpiresAt.UTC()
			item.SourceExpiresAt = &stamp
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, savedPageNativeFailure(err)
	}
	return items, nil
}

func (s *Store) listHumanSavedRows(ctx context.Context, digest [32]byte, actor identity.Actor, cursor string, limit int) ([]saved.Item, error) {
	if !saved.ValidPageCursor(cursor) {
		return nil, saved.ErrInvalidPage
	}
	tx, err := s.beginHumanMomentTx(ctx, digest, actor)
	if err != nil {
		return nil, savedPageNativeFailure(err)
	}
	defer tx.Rollback(context.Background())
	// The anchor wait comes before the final Session lock. Read projected titles
	// and notes only after observing any revoke committed during that wait.
	if cursor != "" {
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM saved_items WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, cursor, actor.ID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return nil, saved.ErrPageChanged
		}
		if err != nil {
			return nil, savedPageNativeFailure(err)
		}
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return nil, savedPageNativeFailure(err)
	}
	items, err := humanSavedRead(ctx, tx, actor.ID, cursor, limit)
	if err != nil {
		return nil, savedPageNativeFailure(err)
	}
	if err = s.finishHumanMomentTx(ctx, tx, digest, actor.ID); err != nil {
		return nil, savedPageNativeFailure(err)
	}
	if ctx.Err() != nil {
		return nil, saved.ErrPageUnavailable
	}
	return items, nil
}

func (s *Store) ListHumanSaved(ctx context.Context, digest [32]byte, actor identity.Actor) ([]saved.Item, error) {
	return s.listHumanSavedRows(ctx, digest, actor, "", 100)
}
func (s *Store) ListHumanSavedPage(ctx context.Context, digest [32]byte, actor identity.Actor, cursor string) (saved.Page, error) {
	return s.SearchHumanSavedPage(ctx, digest, actor, saved.Query{}, cursor)
}

func (s *Store) GetHumanSavedTarget(ctx context.Context, digest [32]byte, actor identity.Actor, kind, target string) (*saved.Item, error) {
	if (kind != "place" && kind != "activity" && kind != "group") || target == "" || !saved.ValidPageCursor(target) {
		return nil, saved.ErrInvalidPage
	}
	tx, err := s.beginHumanMomentTx(ctx, digest, actor)
	if err != nil {
		return nil, savedPageNativeFailure(err)
	}
	defer tx.Rollback(context.Background())
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM saved_items WHERE owner_account_id=$1 AND
	 (($2='place' AND place_id=$3::uuid) OR($2='activity' AND activity_id=$3::uuid) OR($2='group' AND community_id=$3::uuid)) FOR SHARE`, actor.ID, kind, target).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, savedPageNativeFailure(err)
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return nil, savedPageNativeFailure(err)
	}
	var item *saved.Item
	if id != "" {
		query := strings.Replace(humanSavedRowsSQL, "WHERE s.owner_account_id=$1\n AND", "WHERE s.owner_account_id=$1 AND s.id=$4::uuid\n AND", 1)
		rows, e := tx.Query(ctx, query, actor.ID, nil, 1, id)
		if e != nil {
			return nil, savedPageNativeFailure(e)
		}
		if rows.Next() {
			value := saved.Item{}
			var privateSource string
			e = rows.Scan(&value.ID, &value.Kind, &value.TargetID, &value.Title, &value.Summary, &value.CityID, &value.Available, &value.SavedAt, &value.Note, &value.NoteRevision, &value.CityName, &value.CommunityAvatar, &value.Latitude, &value.Longitude, &value.SourceExpiresAt, &privateSource, &value.Cover, &value.PlacePhoto)
			if e == nil && !value.ValidCommunityMedia() {
				e = saved.ErrPageUnavailable
			}
			if e == nil {
				value.SavedAt = value.SavedAt.UTC()
				if value.SourceExpiresAt != nil {
					stamp := value.SourceExpiresAt.UTC()
					value.SourceExpiresAt = &stamp
				}
				item = &value
			}
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return nil, savedPageNativeFailure(e)
		}
		if item == nil {
			return nil, saved.ErrPageChanged
		}
	}
	if err = s.finishHumanMomentTx(ctx, tx, digest, actor.ID); err != nil {
		return nil, savedPageNativeFailure(err)
	}
	if ctx.Err() != nil {
		return nil, saved.ErrPageUnavailable
	}
	return item, nil
}
