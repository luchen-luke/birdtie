package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/jackc/pgx/v5"
)

const inboxColumns = `i.id,i.category,i.title,i.detail,i.resource_type,i.resource_id,
    i.created_at,i.read_at,i.target_activity_id,i.target_conversation_id,
    COALESCE(d.category,''),COALESCE(d.disposition,''),COALESCE(d.priority,0),
    CASE WHEN i.resource_type='community_message' AND d.community_message_id IS NOT NULL THEN
      (SELECT cv.community_id FROM community_conversation_messages m
       JOIN community_conversations cv ON cv.id=m.conversation_id WHERE m.id=d.community_message_id)
      WHEN i.resource_type='community' THEN i.resource_id ELSE NULL END,
    CASE WHEN i.resource_type='agent_task' AND i.routing_decision_id IS NULL THEN i.resource_id
      WHEN i.resource_type='agent_task' THEN d.agent_task_id ELSE NULL END,
    CASE WHEN i.resource_type='business_claim_review' THEN d.business_claim_id
      WHEN i.resource_type='business_update' THEN (SELECT audit.business_id FROM business_public_profile_audit audit WHERE audit.id=d.source_id) ELSE NULL END`

// Legacy rows retain their IDs but do not become permanent cached permissions.
// New routed rows are checked against their exact native source and policy.
const inboxVisibility = `EXISTS(SELECT 1 FROM accounts current_owner WHERE current_owner.id=i.recipient_account_id AND current_owner.status='active') AND
 ((i.routing_decision_id IS NOT NULL AND d.recipient_id=i.recipient_account_id AND birdtie_native_notification_visible(d)) OR
  (i.routing_decision_id IS NULL AND CASE i.resource_type
    WHEN 'community' THEN EXISTS(SELECT 1 FROM communities co WHERE co.id=i.resource_id AND co.owner_account_id=i.recipient_account_id AND co.lifecycle_status='active')
    ELSE EXISTS(SELECT 1 FROM birdtie_native_notification_source(
      CASE i.resource_type WHEN 'place_candidate' THEN 'place_review' WHEN 'activity_candidate' THEN 'activity_review'
        WHEN 'conversation_message' THEN 'direct_message' WHEN 'connection_request' THEN
          CASE WHEN EXISTS(SELECT 1 FROM connection_requests cr WHERE cr.id=i.resource_id AND cr.recipient_account_id=i.recipient_account_id)
            THEN 'connection_request' ELSE 'connection_decision' END
        ELSE i.resource_type END,
      CASE WHEN i.resource_type IN ('activity_change','activity_cancelled','activity_reminder') THEN COALESCE(i.target_activity_id,i.resource_id) ELSE i.resource_id END,
      i.recipient_account_id)) END))`

func scanInboxItem(row scanner) (inbox.Item, error) {
	var item inbox.Item
	err := row.Scan(&item.ID, &item.Category, &item.Title, &item.Detail,
		&item.ResourceType, &item.ResourceID, &item.CreatedAt, &item.ReadAt,
		&item.TargetActivityID, &item.TargetConversationID, &item.SemanticCategory, &item.NotificationRoute, &item.NotificationPriority,
		&item.TargetCommunityID, &item.TargetTaskID, &item.TargetBusinessID)
	return item, err
}

func (s *Store) ListInbox(ctx context.Context, ownerID string) ([]inbox.Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+inboxColumns+` FROM inbox_items i
        LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id
        WHERE i.recipient_account_id = $1 AND `+inboxVisibility+`
        ORDER BY COALESCE(d.priority,50) DESC,i.created_at DESC,i.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]inbox.Item, 0)
	for rows.Next() {
		item, err := scanInboxItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReadInboxItem(ctx context.Context, ownerID, id string) (inbox.Item, error) {
	item, err := scanInboxItem(s.pool.QueryRow(ctx, `WITH permitted AS MATERIALIZED (
       SELECT i.id FROM inbox_items i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id
       WHERE i.id=$1 AND i.recipient_account_id=$2 AND `+inboxVisibility+`), marked AS (
       UPDATE inbox_items SET read_at=COALESCE(read_at,clock_timestamp()) WHERE id IN(SELECT id FROM permitted) RETURNING *)
       SELECT `+inboxColumns+` FROM marked i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id`, id, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return inbox.Item{}, inbox.ErrNotFound
	}
	return item, err
}

func insertReviewInboxItem(ctx context.Context, tx pgx.Tx, ownerID, resourceType, resourceID, _title, _detail string) error {
	kind := agentnotification.KindPlaceReview
	if resourceType == "activity_candidate" {
		kind = agentnotification.KindActivityReview
	} else if resourceType != "place_candidate" {
		return agentnotification.ErrUnavailable
	}
	_, err := routeNativeNotification(ctx, tx, kind, resourceID, ownerID)
	return err
}

// Human read uses the current native Session and final source predicate in one
// RC transaction. A successful mark is not permission to reuse a cached target.
// Source snapshot is statement-local; HTTP validates the Session after JSON.
func (s *Store) beginHumanInboxTx(ctx context.Context, digest [32]byte, actor identity.Actor) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, socialnow.ErrUnavailable
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || actor.AccountType != "person" || principal.Type != actorref.Person || principal.ID != actor.ID || digest == ([32]byte{}) {
		return nil, identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, socialnow.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, error) { _ = tx.Rollback(context.Background()); return nil, e }
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return fail(socialnow.ErrUnavailable)
	}
	// Relation waits precede Session locks. These are relation, not source-row
	// locks. Current content is still resolved by the final statement below.
	if _, err = tx.Exec(ctx, humanSocialRelations); err != nil {
		return fail(socialnow.ErrUnavailable)
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE inbox_items,native_notification_decisions,native_notification_policies,
 activity_participations,conversations,conversation_messages,community_conversations,
 community_conversation_members,community_conversation_messages,activity_conversations,
 activity_conversation_members,activity_conversation_messages,agent_tasks,activity_candidates,place_candidates,
 business_claim_controls,business_console_audit_events,business_review_grants,business_memberships,businesses IN ACCESS SHARE MODE`); err != nil {
		return fail(socialnow.ErrUnavailable)
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, actor.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(identity.ErrUnauthorized)
	}
	if err != nil {
		return fail(socialnow.ErrUnavailable)
	}
	if err = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); err != nil {
		return fail(err)
	}
	return tx, nil
}

func (s *Store) ListHumanInbox(ctx context.Context, digest [32]byte, actor identity.Actor) ([]inbox.Item, error) {
	tx, err := s.beginHumanInboxTx(ctx, digest, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT `+inboxColumns+` FROM inbox_items i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id
 WHERE i.recipient_account_id=$1 AND `+inboxVisibility+` AND EXISTS(SELECT 1 FROM sessions se WHERE se.account_id=$1 AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp() AND ($3::boolean OR se.authentication_method<>'dev_phone'))
 ORDER BY COALESCE(d.priority,50) DESC,i.created_at DESC,i.id DESC LIMIT 100`, actor.ID, digest[:], s.devPhoneEnabled)
	if err != nil {
		return nil, socialnow.ErrUnavailable
	}
	items := make([]inbox.Item, 0)
	for rows.Next() {
		item, e := scanInboxItem(rows)
		if e != nil {
			rows.Close()
			return nil, socialnow.ErrUnavailable
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, socialnow.ErrUnavailable
	}
	if err = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil || ctx.Err() != nil {
		return nil, socialnow.ErrUnavailable
	}
	return items, nil
}

func (s *Store) ReadHumanInboxItem(ctx context.Context, digest [32]byte, actor identity.Actor, id string) (inbox.Item, error) {
	parsed, err := actorref.ParsePrincipal("person", id)
	if err != nil || parsed.ID != id {
		return inbox.Item{}, inbox.ErrNotFound
	}
	tx, err := s.beginHumanInboxTx(ctx, digest, actor)
	if err != nil {
		return inbox.Item{}, err
	}
	defer tx.Rollback(context.Background())
	var locked string
	// The actual Inbox row wait precedes the Session lock. A revocation committed
	// during this resource wait is observable, and no read_at is written first.
	err = tx.QueryRow(ctx, `SELECT id FROM inbox_items WHERE id=$1 AND recipient_account_id=$2 FOR UPDATE`, id, actor.ID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return inbox.Item{}, inbox.ErrNotFound
	}
	if err != nil {
		return inbox.Item{}, socialnow.ErrUnavailable
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return inbox.Item{}, err
	}
	item, err := scanInboxItem(tx.QueryRow(ctx, `WITH permitted AS MATERIALIZED(
 SELECT i.id FROM inbox_items i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE i.id=$1 AND i.recipient_account_id=$2 AND `+inboxVisibility+`
 AND EXISTS(SELECT 1 FROM sessions se WHERE se.account_id=$2 AND se.token_sha256=$3 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp() AND ($4::boolean OR se.authentication_method<>'dev_phone'))), marked AS(
 UPDATE inbox_items SET read_at=COALESCE(read_at,clock_timestamp()) WHERE id IN(SELECT id FROM permitted) RETURNING *)
 SELECT `+inboxColumns+` FROM marked i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id`, id, actor.ID, digest[:], s.devPhoneEnabled))
	if err != nil {
		if e := checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); e != nil {
			return inbox.Item{}, e
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return inbox.Item{}, inbox.ErrNotFound
		}
		return inbox.Item{}, socialnow.ErrUnavailable
	}
	if err = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); err != nil {
		return inbox.Item{}, err
	}
	if err = tx.Commit(ctx); err != nil || ctx.Err() != nil {
		return inbox.Item{}, socialnow.ErrUnavailable
	}
	return item, nil
}
