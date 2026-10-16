package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/jackc/pgx/v5"
)

// Called only for a newly inserted native person Follow, in its transaction.
// Replaying a Follow or importing historical rows never manufactures an event.
func createPersonFollowNotification(ctx context.Context, tx pgx.Tx, followID, actor, recipient string) error {
	var event string
	err := tx.QueryRow(ctx, `INSERT INTO person_follow_events(source_follow_id,actor_account_id,recipient_account_id)
 SELECT id,follower_account_id,person_account_id FROM follows
 WHERE id=$1 AND follower_account_id=$2 AND person_account_id=$3 AND legacy_source_id IS NULL
 ON CONFLICT(source_follow_id) DO NOTHING RETURNING id`, followID, actor, recipient).Scan(&event)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = routeNativeNotification(ctx, tx, agentnotification.KindPersonFollow, event, recipient)
	return err
}

// The event proves a past follow only. Its actor ID is released as a destination
// only when the original recipient can still read that person's profile now.
func (s *Store) GetInboxPersonTarget(ctx context.Context, digest [32]byte, a identity.Actor, item string) (map[string]any, error) {
	if !identity.HumanControlUUID(item) {
		return nil, identity.ErrControlInvalid
	}
	ctx, original, err := s.humanWriteBound(ctx, digest, a)
	if err != nil {
		return nil, err
	}
	tx, err := s.beginHumanInboxTx(ctx, digest, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	if err = s.lockHumanMomentSession(ctx, tx, digest, a.ID); err != nil {
		return nil, err
	}
	current, _, err := s.humanWriteSession(ctx, tx, digest, a, false)
	if err != nil || current != original {
		return nil, identity.ErrControlChanged
	}
	read := func() (string, error) {
		var target string
		err := tx.QueryRow(ctx, `SELECT ev.actor_account_id FROM inbox_items i
 JOIN native_notification_decisions d ON d.id=i.routing_decision_id
 JOIN person_follow_events ev ON ev.id=d.person_follow_event_id
 WHERE i.id=$1 AND i.recipient_account_id=$2 AND ev.recipient_account_id=$2
 AND i.origin='NATIVE' AND i.resource_type='person_follow' AND i.resource_id=ev.id
 AND d.kind='person_follow' AND `+inboxVisibility+`
 AND birdtie_profile_visible_to(ev.actor_account_id,$2::uuid)`, item, a.ID).Scan(&target)
		return target, err
	}
	target, err := read()
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inbox.ErrNotFound
	}
	if err != nil {
		return nil, content.ErrUnavailable
	}
	if err = checkHumanMomentSession(ctx, tx, digest, a.ID, s.devPhoneEnabled); err != nil {
		return nil, err
	}
	fresh, err := read()
	if err != nil || fresh != target {
		return nil, identity.ErrControlChanged
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"accountId": target}, nil
}
