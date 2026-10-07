package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"

	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
	"github.com/jackc/pgx/v5"
)

const activityChatEligible = `a.publication_status='published'
    AND (a.expires_at IS NULL OR a.expires_at>statement_timestamp())
    AND EXISTS(SELECT 1 FROM cities WHERE id=a.city_id AND publication_status='published')
    AND EXISTS(SELECT 1 FROM accounts WHERE id=$2 AND account_type='person' AND status='active')
    AND birdtie_activity_visible_to(a.id,$2)
    AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
        (b.blocker_account_id=$2 AND b.blocked_account_id=a.host_account_id) OR
        (b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$2))
    AND (` + socialActivityManager + ` OR EXISTS(SELECT 1 FROM activity_participations p
        WHERE p.activity_id=a.id AND p.participant_account_id=$2 AND p.status='going'))`

func chatState(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor, activity string) (activitychat.State, error) {
	out := activitychat.State{ActivityID: activity, ViewerAccountID: actor}
	err := q.QueryRow(ctx, `SELECT coalesce(c.id::text,''),coalesce(m.status='active',false),
        a.cancelled_at IS NULL AND a.ends_at>now(),`+socialActivityManager+`
        FROM activities a LEFT JOIN activity_conversations c ON c.activity_id=a.id
        LEFT JOIN activity_conversation_members m ON m.conversation_id=c.id AND m.account_id=$2
        WHERE a.id=$1 AND `+activityChatEligible, activity, actor).Scan(&out.ID, &out.Joined, &out.CanJoin, &out.Moderator)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, activitychat.ErrNotFound
	}
	out.CanSend = out.Joined && out.CanJoin
	return out, err
}

func lockActivityChat(ctx context.Context, tx pgx.Tx, actor, activity string) (activitychat.State, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM activities WHERE id=$1 FOR UPDATE`, activity).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return activitychat.State{}, activitychat.ErrNotFound
	}
	if err != nil {
		return activitychat.State{}, err
	}
	// A cancellation owns the participation row before its member-revoke trigger.
	// Lock in the same order, then the chat member, to serialize send vs revoke.
	rows, err := tx.Query(ctx, `SELECT id FROM activity_participations WHERE activity_id=$1 AND participant_account_id=$2 FOR SHARE`, activity, actor)
	if err != nil {
		return activitychat.State{}, err
	}
	for rows.Next() {
		var unused string
		if err = rows.Scan(&unused); err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return activitychat.State{}, err
	}
	rows, err = tx.Query(ctx, `SELECT m.account_id FROM activity_conversation_members m JOIN activity_conversations c ON c.id=m.conversation_id WHERE c.activity_id=$1 AND m.account_id=$2 FOR UPDATE OF m`, activity, actor)
	if err != nil {
		return activitychat.State{}, err
	}
	for rows.Next() {
		var unused string
		if err = rows.Scan(&unused); err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return activitychat.State{}, err
	}
	return chatState(ctx, tx, actor, activity)
}

func chatAudit(ctx context.Context, tx pgx.Tx, actor, action, resource string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,$2,'activity_conversation',$3,'allowed','explicit_activity_chat')`, actor, action, resource)
	return err
}

func (s *Store) ActivityChatState(ctx context.Context, actor, activity string) (activitychat.State, error) {
	return chatState(ctx, s.pool, actor, activity)
}

func (s *Store) JoinActivityChat(ctx context.Context, actor, activity string) (activitychat.State, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitychat.State{}, err
	}
	defer tx.Rollback(ctx)
	state, err := lockActivityChat(ctx, tx, actor, activity)
	if err != nil {
		return state, err
	}
	if !state.CanJoin {
		return state, activitychat.ErrClosed
	}
	err = tx.QueryRow(ctx, `INSERT INTO activity_conversations(activity_id) VALUES($1)
        ON CONFLICT(activity_id) DO UPDATE SET activity_id=EXCLUDED.activity_id RETURNING id`, activity).Scan(&state.ID)
	if err != nil {
		return state, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO activity_conversation_members(conversation_id,account_id) VALUES($1,$2)
        ON CONFLICT(conversation_id,account_id) DO UPDATE SET status='active',joined_at=now(),updated_at=now()`, state.ID, actor)
	if err != nil {
		return state, err
	}
	if !state.Joined {
		if err = chatAudit(ctx, tx, actor, "join", state.ID); err != nil {
			return state, err
		}
	}
	state.Joined = true
	state.CanSend = true
	return state, tx.Commit(ctx)
}

func (s *Store) LeaveActivityChat(ctx context.Context, actor, activity string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `UPDATE activity_conversation_members m SET status='left',updated_at=now()
        FROM activity_conversations c WHERE c.id=m.conversation_id AND c.activity_id=$1 AND m.account_id=$2 AND m.status='active' RETURNING c.id`, activity, actor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = chatAudit(ctx, tx, actor, "leave", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const chatMessageColumns = `m.id,m.sender_account_id,CASE
    WHEN birdtie_agent_profile_field_allowed(sender.id,$2::uuid,'displayName')
    THEN coalesce(nullif(p.display_name,''),nullif(sender.handle,''),'Birdtie 成员')
    ELSE 'Birdtie 成员' END,m.body,m.created_at,m.removed_at IS NOT NULL`
const chatMessageFrom = ` FROM activity_conversation_messages m JOIN accounts sender ON sender.id=m.sender_account_id
    LEFT JOIN user_profiles p ON p.account_id=sender.id`

func scanActivityChatMessage(row scanner) (activitychat.Message, error) {
	var m activitychat.Message
	err := row.Scan(&m.ID, &m.SenderAccountID, &m.SenderName, &m.Body, &m.CreatedAt, &m.Removed)
	return m, err
}

func (s *Store) ActivityChatMessages(ctx context.Context, actor, activity, before string) (activitychat.Page, error) {
	out := activitychat.Page{Messages: []activitychat.Message{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	state, err := chatState(ctx, tx, actor, activity)
	if err != nil {
		return out, err
	}
	if !state.Joined {
		return out, activitychat.ErrNotFound
	}
	var cursor any
	if before != "" {
		var valid bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM activity_conversation_messages WHERE id=$1 AND conversation_id=$2)`, before, state.ID).Scan(&valid)
		if err != nil {
			return out, err
		}
		if !valid {
			return out, activitychat.ErrNotFound
		}
		cursor = before
	}
	rows, err := tx.Query(ctx, `SELECT `+chatMessageColumns+chatMessageFrom+`
        WHERE m.conversation_id=$1 AND sender.status='active'
        AND EXISTS(SELECT 1 FROM activity_conversations current_room
            JOIN activities a ON a.id=current_room.activity_id
            JOIN activity_conversation_members current_member ON current_member.conversation_id=current_room.id
                AND current_member.account_id=$2 AND current_member.status='active'
            WHERE current_room.id=m.conversation_id AND a.id=$4 AND `+activityChatEligible+`)
        AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
            (b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id) OR
            (b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))
        AND ($3::uuid IS NULL OR (m.created_at,m.id)<(SELECT created_at,id FROM activity_conversation_messages WHERE id=$3))
        ORDER BY m.created_at DESC,m.id DESC LIMIT 101`, state.ID, actor, cursor, activity)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		m, e := scanActivityChatMessage(rows)
		if e != nil {
			return out, e
		}
		out.Messages = append(out.Messages, m)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	if len(out.Messages) > 100 {
		out.Messages = out.Messages[:100]
		out.HasMore = true
	}
	for i, j := 0, len(out.Messages)-1; i < j; i, j = i+1, j-1 {
		out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
	}
	return out, tx.Commit(ctx)
}

func (s *Store) SendActivityChatMessage(ctx context.Context, actor, activity, clientID, body string) (activitychat.Message, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return activitychat.Message{}, err
	}
	defer tx.Rollback(ctx)
	state, err := lockActivityChat(ctx, tx, actor, activity)
	if err != nil {
		return activitychat.Message{}, err
	}
	if !state.Joined {
		return activitychat.Message{}, activitychat.ErrNotFound
	}
	if !state.CanSend {
		return activitychat.Message{}, activitychat.ErrClosed
	}
	// Lock the sender across rooms for the per-Person rate limit.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' FOR UPDATE`, actor).Scan(&locked); err != nil {
		return activitychat.Message{}, err
	}
	existing, e := scanActivityChatMessage(tx.QueryRow(ctx, `SELECT `+chatMessageColumns+chatMessageFrom+` WHERE m.conversation_id=$1 AND m.sender_account_id=$2 AND m.client_message_id=$3`, state.ID, actor, clientID))
	if e == nil {
		if existing.Removed || existing.Body != body {
			return existing, activitychat.ErrConflict
		}
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return existing, e
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM activity_conversation_messages WHERE sender_account_id=$1 AND created_at>now()-interval '1 minute'`, actor).Scan(&recent); err != nil {
		return existing, err
	}
	if recent >= 60 {
		return existing, activitychat.ErrRateLimited
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO activity_conversation_messages(conversation_id,sender_account_id,client_message_id,body) VALUES($1,$2,$3,$4) RETURNING id`, state.ID, actor, clientID, body).Scan(&id); err != nil {
		return existing, err
	}
	m, err := scanActivityChatMessage(tx.QueryRow(ctx, `SELECT `+chatMessageColumns+chatMessageFrom+` WHERE m.id=$1`, id, actor))
	if err != nil {
		return m, err
	}
	if err = chatAudit(ctx, tx, actor, "send", id); err != nil {
		return m, err
	}
	if err = routeNativeChatRecipients(ctx, tx, agentnotification.KindActivityMessage, id, state.ID, actor); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

func (s *Store) RemoveActivityChatMessage(ctx context.Context, actor, activity, message string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	state, err := lockActivityChat(ctx, tx, actor, activity)
	if err != nil {
		return err
	}
	if !state.Joined {
		return activitychat.ErrNotFound
	}
	var sender string
	if err = tx.QueryRow(ctx, `SELECT sender_account_id FROM activity_conversation_messages WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, message, state.ID).Scan(&sender); errors.Is(err, pgx.ErrNoRows) {
		return activitychat.ErrNotFound
	}
	if err != nil {
		return err
	}
	if sender != actor && !state.Moderator {
		return activitychat.ErrForbidden
	}
	result, err := tx.Exec(ctx, `UPDATE activity_conversation_messages SET body='',removed_at=now(),removed_by=$2 WHERE id=$1 AND removed_at IS NULL`, message, actor)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		if err = chatAudit(ctx, tx, actor, "remove_message", message); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
