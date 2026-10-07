package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"

	"github.com/birdtie/birdtie/apps/api/internal/communitychat"
	"github.com/jackc/pgx/v5"
)

const communityChatEligible = `c.lifecycle_status='active' AND c.publication_status='published'
    AND EXISTS(SELECT 1 FROM accounts WHERE id=$2 AND account_type='person' AND status='active')
    AND EXISTS(SELECT 1 FROM community_memberships WHERE community_id=c.id AND user_account_id=$2 AND status='active')`

func communityChatState(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor, communityID string) (communitychat.State, error) {
	out := communitychat.State{CommunityID: communityID, ViewerAccountID: actor}
	err := q.QueryRow(ctx, `SELECT coalesce(conversation.id::text,''),coalesce(member.status='active',false),true,
        EXISTS(SELECT 1 FROM community_memberships WHERE community_id=c.id AND user_account_id=$2 AND status='active' AND role IN ('owner','admin'))
        FROM communities c LEFT JOIN community_conversations conversation ON conversation.community_id=c.id
        LEFT JOIN community_conversation_members member ON member.conversation_id=conversation.id AND member.account_id=$2
        WHERE c.id=$1 AND `+communityChatEligible, communityID, actor).Scan(&out.ID, &out.Joined, &out.CanJoin, &out.Moderator)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, communitychat.ErrNotFound
	}
	out.CanSend = out.Joined
	return out, err
}

func lockCommunityChat(ctx context.Context, tx pgx.Tx, actor, communityID string) (communitychat.State, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM communities WHERE id=$1 FOR UPDATE`, communityID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return communitychat.State{}, communitychat.ErrNotFound
	}
	if err != nil {
		return communitychat.State{}, err
	}
	// Community mutation uses Community then membership; the revoke trigger follows.
	err = tx.QueryRow(ctx, `SELECT id FROM community_memberships WHERE community_id=$1 AND user_account_id=$2 FOR SHARE`, communityID, actor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return communitychat.State{}, communitychat.ErrNotFound
	}
	if err != nil {
		return communitychat.State{}, err
	}
	err = tx.QueryRow(ctx, `SELECT m.account_id FROM community_conversation_members m JOIN community_conversations c ON c.id=m.conversation_id WHERE c.community_id=$1 AND m.account_id=$2 FOR UPDATE OF m`, communityID, actor).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return communitychat.State{}, err
	}
	return communityChatState(ctx, tx, actor, communityID)
}

func communityChatAudit(ctx context.Context, tx pgx.Tx, actor, action, resource string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,$2,'community_conversation',$3,'allowed','explicit_community_chat')`, actor, action, resource)
	return err
}

func (s *Store) CommunityChatState(ctx context.Context, actor, communityID string) (communitychat.State, error) {
	return communityChatState(ctx, s.pool, actor, communityID)
}

func (s *Store) JoinCommunityChat(ctx context.Context, actor, communityID string) (communitychat.State, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return communitychat.State{}, err
	}
	defer tx.Rollback(ctx)
	state, err := lockCommunityChat(ctx, tx, actor, communityID)
	if err != nil {
		return state, err
	}
	if !state.CanJoin {
		return state, communitychat.ErrNotFound
	}
	err = tx.QueryRow(ctx, `INSERT INTO community_conversations(community_id) VALUES($1)
        ON CONFLICT(community_id) DO UPDATE SET community_id=EXCLUDED.community_id RETURNING id`, communityID).Scan(&state.ID)
	if err != nil {
		return state, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO community_conversation_members(conversation_id,account_id) VALUES($1,$2)
        ON CONFLICT(conversation_id,account_id) DO UPDATE SET status='active',joined_at=now(),updated_at=now()`, state.ID, actor)
	if err != nil {
		return state, err
	}
	if !state.Joined {
		if err = communityChatAudit(ctx, tx, actor, "join", state.ID); err != nil {
			return state, err
		}
	}
	state.Joined = true
	state.CanSend = true
	return state, tx.Commit(ctx)
}

func (s *Store) LeaveCommunityChat(ctx context.Context, actor, communityID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `UPDATE community_conversation_members m SET status='left',updated_at=now()
        FROM community_conversations c WHERE c.id=m.conversation_id AND c.community_id=$1 AND m.account_id=$2 AND m.status='active' RETURNING c.id`, communityID, actor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = communityChatAudit(ctx, tx, actor, "leave", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const communityChatMessageColumns = `m.id,m.sender_account_id,CASE
    WHEN birdtie_agent_profile_field_allowed(sender.id,$2::uuid,'displayName')
    THEN coalesce(nullif(p.display_name,''),nullif(sender.handle,''),'Birdtie 成员')
    ELSE 'Birdtie 成员' END,m.body,m.created_at,m.removed_at IS NOT NULL`
const communityChatMessageFrom = ` FROM community_conversation_messages m JOIN accounts sender ON sender.id=m.sender_account_id
    LEFT JOIN user_profiles p ON p.account_id=sender.id`

func scanCommunityChatMessage(row scanner) (communitychat.Message, error) {
	var m communitychat.Message
	err := row.Scan(&m.ID, &m.SenderAccountID, &m.SenderName, &m.Body, &m.CreatedAt, &m.Removed)
	return m, err
}

func (s *Store) CommunityChatMessages(ctx context.Context, actor, communityID, before string) (communitychat.Page, error) {
	out := communitychat.Page{Messages: []communitychat.Message{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	state, err := communityChatState(ctx, tx, actor, communityID)
	if err != nil {
		return out, err
	}
	if !state.Joined {
		return out, communitychat.ErrNotFound
	}
	var cursor any
	if before != "" {
		var valid bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM community_conversation_messages WHERE id=$1 AND conversation_id=$2)`, before, state.ID).Scan(&valid)
		if err != nil {
			return out, err
		}
		if !valid {
			return out, communitychat.ErrNotFound
		}
		cursor = before
	}
	rows, err := tx.Query(ctx, `SELECT `+communityChatMessageColumns+communityChatMessageFrom+`
        WHERE m.conversation_id=$1 AND sender.status='active'
        AND EXISTS(SELECT 1 FROM community_conversations current_room
            JOIN communities c ON c.id=current_room.community_id
            JOIN community_conversation_members current_member ON current_member.conversation_id=current_room.id
                AND current_member.account_id=$2 AND current_member.status='active'
            WHERE current_room.id=m.conversation_id AND c.id=$4 AND `+communityChatEligible+`)
        AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
            (b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id) OR
            (b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))
        AND ($3::uuid IS NULL OR (m.created_at,m.id)<(SELECT created_at,id FROM community_conversation_messages WHERE id=$3))
        ORDER BY m.created_at DESC,m.id DESC LIMIT 101`, state.ID, actor, cursor, communityID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		m, e := scanCommunityChatMessage(rows)
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

func (s *Store) SendCommunityChatMessage(ctx context.Context, actor, communityID, clientID, body string) (communitychat.Message, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return communitychat.Message{}, err
	}
	defer tx.Rollback(ctx)
	state, err := lockCommunityChat(ctx, tx, actor, communityID)
	if err != nil {
		return communitychat.Message{}, err
	}
	if !state.Joined {
		return communitychat.Message{}, communitychat.ErrNotFound
	}
	if !state.CanSend {
		return communitychat.Message{}, communitychat.ErrNotFound
	}
	// Lock the sender across rooms for the per-Person rate limit.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' FOR UPDATE`, actor).Scan(&locked); err != nil {
		return communitychat.Message{}, err
	}
	existing, e := scanCommunityChatMessage(tx.QueryRow(ctx, `SELECT `+communityChatMessageColumns+communityChatMessageFrom+` WHERE m.conversation_id=$1 AND m.sender_account_id=$2 AND m.client_message_id=$3`, state.ID, actor, clientID))
	if e == nil {
		if existing.Removed || existing.Body != body {
			return existing, communitychat.ErrConflict
		}
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return existing, e
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM community_conversation_messages WHERE sender_account_id=$1 AND created_at>now()-interval '1 minute'`, actor).Scan(&recent); err != nil {
		return existing, err
	}
	if recent >= 60 {
		return existing, communitychat.ErrRateLimited
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO community_conversation_messages(conversation_id,sender_account_id,client_message_id,body) VALUES($1,$2,$3,$4) RETURNING id`, state.ID, actor, clientID, body).Scan(&id); err != nil {
		return existing, err
	}
	m, err := scanCommunityChatMessage(tx.QueryRow(ctx, `SELECT `+communityChatMessageColumns+communityChatMessageFrom+` WHERE m.id=$1`, id, actor))
	if err != nil {
		return m, err
	}
	if err = communityChatAudit(ctx, tx, actor, "send", id); err != nil {
		return m, err
	}
	if err = routeNativeChatRecipients(ctx, tx, agentnotification.KindCommunityMessage, id, state.ID, actor); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

func (s *Store) RemoveCommunityChatMessage(ctx context.Context, actor, communityID, message string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	state, err := lockCommunityChat(ctx, tx, actor, communityID)
	if err != nil {
		return err
	}
	if !state.Joined {
		return communitychat.ErrNotFound
	}
	var sender string
	if err = tx.QueryRow(ctx, `SELECT sender_account_id FROM community_conversation_messages WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, message, state.ID).Scan(&sender); errors.Is(err, pgx.ErrNoRows) {
		return communitychat.ErrNotFound
	}
	if err != nil {
		return err
	}
	if sender != actor && !state.Moderator {
		return communitychat.ErrForbidden
	}
	result, err := tx.Exec(ctx, `UPDATE community_conversation_messages SET body='',removed_at=now(),removed_by=$2 WHERE id=$1 AND removed_at IS NULL`, message, actor)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		if err = communityChatAudit(ctx, tx, actor, "remove_message", message); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
