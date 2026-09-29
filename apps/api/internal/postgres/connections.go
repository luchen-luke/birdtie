package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) CreateRequest(ctx context.Context, senderID, recipientID, cityID, note string) (connection.Request, error) {
	if senderID == recipientID {
		return connection.Request{}, connection.ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return connection.Request{}, err
	}
	defer tx.Rollback(ctx)
	var sender string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id = $1 AND status = 'active' FOR UPDATE`, senderID).Scan(&sender)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Request{}, connection.ErrForbidden
	}
	if err != nil {
		return connection.Request{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE connection_requests SET state = 'expired', decided_at = now()
        WHERE state = 'pending' AND expires_at <= now()
          AND (sender_account_id = $1 OR recipient_account_id = $1)`, senderID)
	if err != nil {
		return connection.Request{}, err
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM accounts a
        JOIN user_profiles p ON p.account_id = a.id AND p.visibility = 'public'
        JOIN intents i ON i.owner_account_id = a.id AND i.city_id = $3
          AND i.audience = 'public' AND i.state = 'active'
          AND i.owner_confirmed_at IS NOT NULL AND i.expires_at > now()
          AND i.available_from <= now() AND i.available_until > now()
        JOIN cities c ON c.id = i.city_id AND c.publication_status = 'published'
        WHERE a.id = $2 AND a.status = 'active'
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = $2)
                 OR (b.blocker_account_id = $2 AND b.blocked_account_id = $1))
          AND NOT EXISTS (SELECT 1 FROM conversations cv
              JOIN connection_requests cr ON cr.id = cv.request_id
              WHERE cr.state = 'accepted'
                AND ((cv.member_a_account_id = $1 AND cv.member_b_account_id = $2)
                  OR (cv.member_a_account_id = $2 AND cv.member_b_account_id = $1)))
    )`, senderID, recipientID, cityID).Scan(&eligible)
	if err != nil {
		return connection.Request{}, err
	}
	if !eligible {
		return connection.Request{}, connection.ErrNotFound
	}
	var pending int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM connection_requests
        WHERE sender_account_id = $1 AND state = 'pending' AND expires_at > now()`, senderID).Scan(&pending)
	if err != nil {
		return connection.Request{}, err
	}
	if pending >= 10 {
		return connection.Request{}, connection.ErrRateLimit
	}
	var req connection.Request
	req.Direction = "outgoing"
	req.OtherAccountID = recipientID
	req.CityID = cityID
	req.Note = note
	req.Scope = "conversation"
	req.State = "pending"
	err = tx.QueryRow(ctx, `INSERT INTO connection_requests
        (sender_account_id, recipient_account_id, city_id, note, expires_at)
        VALUES ($1, $2, $3, $4, now() + interval '7 days')
        RETURNING id, expires_at, created_at`, senderID, recipientID, cityID, note).
		Scan(&req.ID, &req.ExpiresAt, &req.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return connection.Request{}, connection.ErrConflict
		}
		return connection.Request{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO inbox_items
        (recipient_account_id, category, title, detail, resource_type, resource_id)
        VALUES ($1, 'requests', 'New contact request',
                'Open Requests to review it.', 'connection_request', $2)`, recipientID, req.ID)
	if err != nil {
		return connection.Request{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'request', 'connection_request', $2, 'allowed', 'human_contact')`, senderID, req.ID)
	if err != nil {
		return connection.Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return connection.Request{}, err
	}
	return req, nil
}

func (s *Store) ListRequests(ctx context.Context, ownerID string) ([]connection.Request, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id,
            CASE WHEN r.sender_account_id = $1 THEN 'outgoing' ELSE 'incoming' END,
            CASE WHEN r.sender_account_id = $1 THEN r.recipient_account_id
                 ELSE r.sender_account_id END,
            CASE WHEN other.status = 'active' AND NOT EXISTS (
                SELECT 1 FROM account_blocks b WHERE
                    (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                    OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
                THEN p.display_name ELSE 'Unavailable account' END,
            r.city_id,
            CASE WHEN other.status = 'active' AND NOT EXISTS (
                SELECT 1 FROM account_blocks b WHERE
                    (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                    OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
                THEN r.note ELSE '' END,
            r.scope,
            CASE WHEN r.state = 'pending' AND r.expires_at <= now()
                 THEN 'expired' ELSE r.state END,
            COALESCE(cv.id::text, ''), r.expires_at, r.created_at
        FROM connection_requests r
        JOIN accounts other ON other.id = CASE WHEN r.sender_account_id = $1
            THEN r.recipient_account_id ELSE r.sender_account_id END
        JOIN user_profiles p ON p.account_id = other.id
        LEFT JOIN conversations cv ON cv.request_id = r.id
        WHERE r.sender_account_id = $1 OR r.recipient_account_id = $1
        ORDER BY r.created_at DESC, r.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]connection.Request, 0)
	for rows.Next() {
		var item connection.Request
		if err := rows.Scan(&item.ID, &item.Direction, &item.OtherAccountID,
			&item.OtherName, &item.CityID, &item.Note, &item.Scope, &item.State,
			&item.ConversationID, &item.ExpiresAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DecideRequest(ctx context.Context, actorID, id, action string) (connection.Request, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return connection.Request{}, err
	}
	defer tx.Rollback(ctx)
	var senderID, recipientID, cityID, note, state string
	var expiresAt, createdAt time.Time
	err = tx.QueryRow(ctx, `SELECT sender_account_id, recipient_account_id,
        city_id, note, state, expires_at, created_at
        FROM connection_requests WHERE id = $1 FOR UPDATE`, id).
		Scan(&senderID, &recipientID, &cityID, &note, &state, &expiresAt, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Request{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.Request{}, err
	}
	if (action == "withdraw" && actorID != senderID) ||
		(action != "withdraw" && actorID != recipientID) {
		return connection.Request{}, connection.ErrNotFound
	}
	if state != "pending" || !expiresAt.After(time.Now()) {
		return connection.Request{}, connection.ErrConflict
	}
	if action == "accept" {
		var eligible bool
		err = tx.QueryRow(ctx, `SELECT EXISTS (
            SELECT 1 FROM accounts a JOIN accounts b ON b.id = $2
            WHERE a.id = $1 AND a.status = 'active' AND b.status = 'active'
              AND NOT EXISTS (SELECT 1 FROM account_blocks block
                  WHERE (block.blocker_account_id = $1 AND block.blocked_account_id = $2)
                     OR (block.blocker_account_id = $2 AND block.blocked_account_id = $1))
        )`, senderID, recipientID).Scan(&eligible)
		if err != nil {
			return connection.Request{}, err
		}
		if !eligible {
			return connection.Request{}, connection.ErrForbidden
		}
	}
	state = map[string]string{"accept": "accepted", "decline": "declined", "withdraw": "withdrawn"}[action]
	_, err = tx.Exec(ctx, `UPDATE connection_requests
        SET state = $2, decided_at = now() WHERE id = $1`, id, state)
	if err != nil {
		return connection.Request{}, err
	}
	var conversationID string
	if state == "accepted" {
		err = tx.QueryRow(ctx, `INSERT INTO conversations
            (request_id, member_a_account_id, member_b_account_id)
            VALUES ($1, $2, $3) RETURNING id`, id, senderID, recipientID).
			Scan(&conversationID)
		if err != nil {
			return connection.Request{}, err
		}
	}
	if state == "accepted" || state == "declined" {
		_, err = tx.Exec(ctx, `INSERT INTO inbox_items
            (recipient_account_id, category, title, detail, resource_type, resource_id)
            VALUES ($1, 'requests', 'Contact request answered',
                    'Open Requests to see the decision.', 'connection_request', $2)
            ON CONFLICT (recipient_account_id, resource_type, resource_id) DO NOTHING`, senderID, id)
		if err != nil {
			return connection.Request{}, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'connection_request', $3, 'allowed', 'human_contact')`, actorID, state, id)
	if err != nil {
		return connection.Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return connection.Request{}, err
	}
	return connection.Request{
		ID: id, CityID: cityID, Note: note, Scope: "conversation", State: state,
		ConversationID: conversationID, ExpiresAt: expiresAt, CreatedAt: createdAt,
	}, nil
}

func (s *Store) ListConversations(ctx context.Context, ownerID string) ([]connection.Conversation, error) {
	rows, err := s.pool.Query(ctx, `SELECT cv.id, other.id, p.display_name, cv.created_at
        FROM conversations cv
        JOIN connection_requests r ON r.id = cv.request_id AND r.state = 'accepted'
        JOIN accounts other ON other.id = CASE WHEN cv.member_a_account_id = $1
            THEN cv.member_b_account_id ELSE cv.member_a_account_id END
            AND other.status = 'active'
        JOIN user_profiles p ON p.account_id = other.id
        WHERE (cv.member_a_account_id = $1 OR cv.member_b_account_id = $1)
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                 OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
        ORDER BY cv.created_at DESC, cv.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]connection.Conversation, 0)
	for rows.Next() {
		var item connection.Conversation
		if err := rows.Scan(&item.ID, &item.OtherAccountID, &item.OtherName,
			&item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func conversationMember(ctx context.Context, tx pgx.Tx, actorID, id string) (string, error) {
	var otherID string
	err := tx.QueryRow(ctx, `SELECT CASE WHEN cv.member_a_account_id = $1
            THEN cv.member_b_account_id ELSE cv.member_a_account_id END
        FROM conversations cv
        JOIN connection_requests r ON r.id = cv.request_id AND r.state = 'accepted'
        JOIN accounts own ON own.id = $1 AND own.status = 'active'
        JOIN accounts other ON other.id = CASE WHEN cv.member_a_account_id = $1
            THEN cv.member_b_account_id ELSE cv.member_a_account_id END
            AND other.status = 'active'
        WHERE cv.id = $2 AND (cv.member_a_account_id = $1 OR cv.member_b_account_id = $1)
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                 OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
        FOR UPDATE OF cv`, actorID, id).Scan(&otherID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", connection.ErrNotFound
	}
	return otherID, err
}

func (s *Store) ListMessages(ctx context.Context, actorID, id string) ([]connection.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := conversationMember(ctx, tx, actorID, id); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, conversation_id, sender_account_id,
        speaker_kind, body, created_at FROM conversation_messages
        WHERE conversation_id = $1
        ORDER BY created_at DESC, id DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	items := make([]connection.Message, 0)
	for rows.Next() {
		var item connection.Message
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.SenderID,
			&item.SpeakerKind, &item.Body, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func (s *Store) SendMessage(ctx context.Context, actorID, id, body string) (connection.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return connection.Message{}, err
	}
	defer tx.Rollback(ctx)
	// Serialize the per-account hourly limit across all conversations.
	var sender string
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts
        WHERE id = $1 AND status = 'active' FOR UPDATE`, actorID).Scan(&sender); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return connection.Message{}, connection.ErrNotFound
		}
		return connection.Message{}, err
	}
	otherID, err := conversationMember(ctx, tx, actorID, id)
	if err != nil {
		return connection.Message{}, err
	}
	var recent int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM conversation_messages
        WHERE sender_account_id = $1 AND created_at > now() - interval '1 hour'`, actorID).Scan(&recent)
	if err != nil {
		return connection.Message{}, err
	}
	if recent >= 60 {
		return connection.Message{}, connection.ErrRateLimit
	}
	var item connection.Message
	err = tx.QueryRow(ctx, `INSERT INTO conversation_messages
        (conversation_id, sender_account_id, body)
        VALUES ($1, $2, $3)
        RETURNING id, conversation_id, sender_account_id, speaker_kind, body, created_at`,
		id, actorID, body).Scan(&item.ID, &item.ConversationID,
		&item.SenderID, &item.SpeakerKind, &item.Body, &item.CreatedAt)
	if err != nil {
		return connection.Message{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO inbox_items
        (recipient_account_id, category, title, detail, resource_type, resource_id)
        VALUES ($1, 'messages', 'New human message',
                'Open Conversations to read it.', 'conversation_message', $2)`, otherID, item.ID)
	if err != nil {
		return connection.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return connection.Message{}, err
	}
	return item, nil
}
