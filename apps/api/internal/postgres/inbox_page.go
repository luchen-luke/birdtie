package postgres

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"github.com/jackc/pgx/v5"
)

var _ inbox.PagedStore = (*Store)(nil)

func inboxPageNativeFailure(err error) error {
	if err == nil || errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, inbox.ErrInvalidPage) || errors.Is(err, inbox.ErrSourceChanged) || errors.Is(err, inbox.ErrNotFound) {
		return err
	}
	return socialnow.ErrUnavailable
}

// Use the existing current source/policy projection. Filter before both global
// unread and LIMIT; direct human-message delivery rows never crowd out events.
const inboxPageVisibleSQL = `WITH visible AS MATERIALIZED (
 SELECT ` + inboxColumns + `,COALESCE(d.priority,50) AS sort_priority
 FROM inbox_items i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id
 WHERE i.recipient_account_id=$1 AND i.resource_type<>'conversation_message' AND ($10::boolean OR i.resource_type<>'person_follow') AND ` + inboxVisibility + `
) `

func inboxPageRead(ctx context.Context, tx pgx.Tx, owner string, in inbox.PageRequest, cursor *inbox.PageCursor) (inbox.Page, error) {
	out := inbox.Page{Items: []inbox.Item{}}
	// One statement gives count, page, cursor and projected destinations the same
	// native RC source snapshot; final response gate repeats this after all waits.
	var priority int
	var stamp time.Time
	var id string
	hasCursor := cursor != nil
	if cursor != nil {
		priority, stamp, id = cursor.Priority, cursor.Time, cursor.ID
	}
	rows, err := tx.Query(ctx, inboxPageVisibleSQL+`
 SELECT v.*, totals.unread,totals.historical_unread FROM (SELECT count(*) FILTER(WHERE read_at IS NULL AND origin='NATIVE') AS unread,count(*)FILTER(WHERE read_at IS NULL AND origin='LEGACY_HISTORY')AS historical_unread FROM visible) totals
 LEFT JOIN LATERAL (SELECT * FROM visible
  WHERE ($7='' OR category=$7) AND (NOT $8::boolean OR read_at IS NULL) AND origin=CASE WHEN $9='' THEN 'NATIVE' ELSE $9 END
  AND (NOT $2::boolean OR (sort_priority,created_at,id)<($3::int,$4::timestamptz,$5::uuid))
  ORDER BY sort_priority DESC,created_at DESC,id DESC LIMIT $6) v ON true
 ORDER BY v.sort_priority DESC,v.created_at DESC,v.id DESC`, owner, hasCursor, priority, stamp, inboxPageNullableID(id), in.Limit+1, in.Category, in.UnreadOnly, in.Origin, in.SupportsPersonFollow)
	if err != nil {
		return out, inboxPageNativeFailure(err)
	}
	defer rows.Close()
	var lastPriority int
	for rows.Next() {
		// The left lateral join retains the global aggregate for a genuinely empty
		// end page. Null ID means no projected row; none of its contents is released.
		var itemID, category, title, detail, resourceType, resourceID *string
		var createdAt, readAt *time.Time
		var activity, conversation, semantic, route, community, task, business *string
		var itemPriority, sortPriority *int
		var avatar *profileavatar.Metadata
		var origin, historicalType *string
		var originalReadAt, originalSeenAt *time.Time
		if err = rows.Scan(&itemID, &category, &title, &detail, &resourceType, &resourceID, &createdAt, &readAt, &activity, &conversation, &semantic, &route, &itemPriority, &community, &task, &business, &avatar, &origin, &historicalType, &originalReadAt, &originalSeenAt, &sortPriority, &out.UnreadCount, &out.HistoricalUnreadCount); err != nil {
			return out, inboxPageNativeFailure(err)
		}
		if itemID == nil {
			continue
		}
		if origin == nil || historicalType == nil || category == nil || title == nil || detail == nil || resourceType == nil || resourceID == nil || createdAt == nil || semantic == nil || route == nil || itemPriority == nil || sortPriority == nil {
			return out, socialnow.ErrUnavailable
		}
		item := inbox.Item{Origin: *origin, HistoricalType: *historicalType, OriginalReadAt: originalReadAt, OriginalSeenAt: originalSeenAt, ID: *itemID, Category: *category, Title: *title, Detail: *detail, ResourceType: *resourceType, ResourceID: *resourceID, CreatedAt: createdAt.UTC(), ReadAt: readAt, TargetActivityID: activity, TargetConversationID: conversation, SemanticCategory: *semantic, NotificationRoute: *route, NotificationPriority: *itemPriority, TargetCommunityID: community, TargetTaskID: task, TargetBusinessID: business, ActorAvatar: avatar}
		if err = projectInboxActorMedia(&item); err != nil {
			return out, err
		}
		if item.ReadAt != nil {
			v := item.ReadAt.UTC()
			item.ReadAt = &v
		}
		out.Items = append(out.Items, item)
		if len(out.Items) == in.Limit {
			lastPriority = *sortPriority
		}
	}
	if err = rows.Err(); err != nil {
		return out, inboxPageNativeFailure(err)
	}
	if len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
		last := out.Items[len(out.Items)-1]
		raw, e := inbox.EncodePageCursor(inbox.PageCursor{Version: 1, Origin: in.Origin, OwnerID: owner, View: in.View, Priority: lastPriority, Time: last.CreatedAt, ID: last.ID, Category: in.Category, UnreadOnly: in.UnreadOnly, SupportsPersonFollow: in.SupportsPersonFollow})
		if e != nil {
			return out, socialnow.ErrUnavailable
		}
		out.NextCursor = &raw
	}
	return out, nil
}

func inboxPageNullableID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func (s *Store) ListHumanInboxPage(ctx context.Context, digest [32]byte, actor identity.Actor, in inbox.PageRequest) (inbox.Page, error) {
	out := inbox.Page{Items: []inbox.Item{}}
	in, cursor, err := inbox.NormalizePage(in, actor.ID)
	if err != nil {
		return out, err
	}
	tx, err := s.beginHumanInboxTx(ctx, digest, actor)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.Background())
	// Finish the real Session wait before reading titles, destinations and counts.
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return out, inboxPageNativeFailure(err)
	}
	out, err = inboxPageRead(ctx, tx, actor.ID, in, cursor)
	if err == nil {
		err = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil || ctx.Err() != nil {
		if err == nil {
			err = socialnow.ErrUnavailable
		}
		return inbox.Page{Items: []inbox.Item{}}, inboxPageNativeFailure(err)
	}
	return out, nil
}

func (s *Store) ValidateHumanInboxPageResponse(ctx context.Context, digest [32]byte, actor identity.Actor, in inbox.PageRequest, expected inbox.Page) error {
	in, cursor, err := inbox.NormalizePage(in, actor.ID)
	if err != nil {
		return err
	}
	tx, err := s.beginHumanInboxTx(ctx, digest, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return inboxPageNativeFailure(err)
	}
	current, err := inboxPageRead(ctx, tx, actor.ID, in, cursor)
	if err != nil {
		return err
	}
	if err = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); err != nil {
		return inboxPageNativeFailure(err)
	}
	if !reflect.DeepEqual(current, expected) {
		return inbox.ErrSourceChanged
	}
	if err = tx.Commit(ctx); err != nil || ctx.Err() != nil {
		return socialnow.ErrUnavailable
	}
	return nil
}
