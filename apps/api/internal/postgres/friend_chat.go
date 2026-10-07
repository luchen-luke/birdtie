package postgres

import (
	"context"
	"errors"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
)

// StartFriendConversation is an explicit action by one member of an active Tie.
// It reuses an existing consented conversation for the pair when one exists.
func (s *Store) StartFriendConversation(ctx context.Context, actorID, tieID string) (connection.Conversation, error) {
	return s.startFriendConversation(ctx, actorID, tieID, nil, nil)
}

func (s *Store) StartFriendConversationBound(ctx context.Context, access ea.Access, tieID string, condition ea.BoundCondition) (connection.Conversation, error) {
	if condition.Kind != ea.Message || condition.Operation != "OPEN_CHAT" {
		return connection.Conversation{}, ea.ErrInvalid
	}
	var e error
	ctx, e = s.messageBindAccess(ctx, access)
	if e != nil {
		return connection.Conversation{}, e
	}
	return s.startFriendConversation(ctx, access.Actor.ID, tieID, &access, &condition)
}

func (s *Store) startFriendConversation(ctx context.Context, actorID, tieID string, access *ea.Access, condition *ea.BoundCondition) (connection.Conversation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return connection.Conversation{}, err
	}
	defer tx.Rollback(ctx)
	if err = messageAuthenticateCurrent(ctx, tx, actorID); err != nil {
		return connection.Conversation{}, err
	}
	var routePeer string
	err = tx.QueryRow(ctx, `SELECT CASE WHEN person_a_account_id=$1 THEN person_b_account_id ELSE person_a_account_id END FROM person_ties WHERE id=$2 AND status='active' AND (person_a_account_id=$1 OR person_b_account_id=$1)`, actorID, tieID).Scan(&routePeer)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Conversation{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.Conversation{}, err
	}
	routeFence, err := startMessageRoute(ctx, tx, actorID, routePeer)
	if err != nil {
		return connection.Conversation{}, err
	}
	if routeFence.route != mp.Allow {
		return connection.Conversation{}, connection.ErrNotFound
	}
	if access != nil {
		if err = s.lockEntityActionWriter(ctx, tx, *access); err != nil {
			return connection.Conversation{}, err
		}
	}
	var requestID, otherID, otherName string
	err = tx.QueryRow(ctx, `SELECT t.request_id,
		CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END,
		CASE WHEN birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName')
            THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员')
            ELSE 'Birdtie 成员' END
		FROM person_ties t
		JOIN accounts own ON own.id=$1 AND own.account_type='person' AND own.status='active'
		JOIN accounts other ON other.id=CASE WHEN t.person_a_account_id=$1
			THEN t.person_b_account_id ELSE t.person_a_account_id END
			AND other.account_type='person' AND other.status='active'
		LEFT JOIN user_profiles p ON p.account_id=other.id
		WHERE t.id=$2 AND t.status='active'
		  AND (t.person_a_account_id=$1 OR t.person_b_account_id=$1)
		  AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
			(b.blocker_account_id=$1 AND b.blocked_account_id=other.id) OR
			(b.blocker_account_id=other.id AND b.blocked_account_id=$1))
		FOR UPDATE OF t`, actorID, tieID).Scan(&requestID, &otherID, &otherName)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Conversation{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.Conversation{}, err
	}
	var fence entityActionWriteFence
	if access != nil {
		fence, err = s.checkEntityActionWrite(ctx, tx, *access, ea.Ref{Type: "person", ID: otherID}, *condition)
		if err != nil {
			return connection.Conversation{}, err
		}
	}
	item := connection.Conversation{OtherAccountID: otherID, OtherName: otherName}
	err = tx.QueryRow(ctx, `SELECT cv.id,cv.created_at FROM conversations cv
		JOIN connection_requests r ON r.id=cv.request_id AND r.state='accepted'
		WHERE (cv.member_a_account_id=$1 AND cv.member_b_account_id=$2) OR
		      (cv.member_a_account_id=$2 AND cv.member_b_account_id=$1)
		ORDER BY cv.created_at,cv.id LIMIT 1`, actorID, otherID).Scan(&item.ID, &item.CreatedAt)
	created := false
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id)
			VALUES($1,LEAST($2::uuid,$3::uuid),GREATEST($2::uuid,$3::uuid))
			RETURNING id,created_at`, requestID, actorID, otherID).Scan(&item.ID, &item.CreatedAt)
		created = true
	}
	if err != nil {
		return connection.Conversation{}, err
	}
	if created {
		_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
			VALUES($1,'start','conversation',$2,'allowed','human_friend_chat')`, actorID, item.ID)
		if err != nil {
			return connection.Conversation{}, err
		}
	}
	var lastReadAt *time.Time
	err = tx.QueryRow(ctx, `SELECT cms.last_read_at,
		(SELECT count(*) FROM conversation_messages m WHERE m.conversation_id=$1
		  AND m.sender_account_id<>$2 AND m.created_at>cms.baseline_at
		  AND (cms.last_read_message_id IS NULL OR (m.created_at,m.id)>(
		    SELECT cursor.created_at,cursor.id FROM conversation_messages cursor
		    WHERE cursor.id=cms.last_read_message_id)))
		FROM conversation_member_states cms WHERE cms.conversation_id=$1 AND cms.member_account_id=$2`,
		item.ID, actorID).Scan(&lastReadAt, &item.UnreadCount)
	if err != nil {
		return connection.Conversation{}, err
	}
	item.LastReadAt = lastReadAt
	if access != nil {
		if created {
			fence.createdRequestID = item.ID
		}
		var exact bool
		if err = tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(cv.id=$3::uuid) FROM conversations cv JOIN connection_requests cr ON cr.id=cv.request_id AND cr.state='accepted' WHERE cv.id=$3::uuid AND LEAST(cv.member_a_account_id,cv.member_b_account_id)=LEAST($1::uuid,$2::uuid) AND GREATEST(cv.member_a_account_id,cv.member_b_account_id)=GREATEST($1::uuid,$2::uuid)`, actorID, otherID, item.ID).Scan(&exact); err != nil || !exact {
			return connection.Conversation{}, ea.ErrChanged
		}
		if err = s.finishEntityActionWrite(ctx, tx, fence); err != nil {
			return connection.Conversation{}, err
		}
	}
	if err = finishMessageRoute(ctx, tx, routeFence); err != nil {
		return connection.Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return connection.Conversation{}, err
	}
	return item, nil
}
