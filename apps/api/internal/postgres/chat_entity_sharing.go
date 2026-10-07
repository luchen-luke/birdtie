package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ connection.HumanEntityShareStore = (*Store)(nil)

func (s *Store) beginEntityShare(ctx context.Context, a connection.EntityShareAccess, conversation string) (pgx.Tx, string, error) {
	if a.Actor.AccountType != "person" || !validHumanMomentID(a.Actor.ID) || a.SessionDigest == ([32]byte{}) || !validHumanMomentID(conversation) {
		return nil, "", identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, "", err
	}
	fail := func(e error) (pgx.Tx, string, error) { _ = tx.Rollback(context.Background()); return nil, "", e }
	// Same serialization as the original per-person hourly limit. Session is
	// checked after these waits; an earlier Authenticate is not sufficient.
	var sender string
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR NO KEY UPDATE`, a.Actor.ID).Scan(&sender); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = identity.ErrUnauthorized
		}
		return fail(err)
	}
	other, err := conversationMember(ctx, tx, a.Actor.ID, conversation)
	if err != nil {
		return fail(err)
	}
	if err = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); err != nil {
		return fail(err)
	}
	return tx, other, nil
}

func readEntityShare(ctx context.Context, tx pgx.Tx, owner, conversation, operation string) (connection.EntityShareReceipt, string, string, error) {
	var r connection.EntityShareReceipt
	var kind, id string
	err := tx.QueryRow(ctx, `SELECT client_operation_id,id,conversation_id,sender_account_id,speaker_kind,body,created_at,entity_type,entity_id
FROM conversation_messages WHERE sender_account_id=$1 AND conversation_id=$2 AND client_operation_id=$3`, owner, conversation, operation).Scan(&r.OperationID, &r.Message.ID, &r.Message.ConversationID, &r.Message.SenderID, &r.Message.SpeakerKind, &r.Message.Body, &r.Message.CreatedAt, &kind, &id)
	return r, kind, id, err
}

func projectEntityShare(ctx context.Context, tx pgx.Tx, owner, kind, id string, r *connection.EntityShareReceipt) error {
	title, err := visibleChatEntity(ctx, tx, owner, kind, id)
	if err != nil {
		return err
	}
	r.Message.Entity = &connection.EntityCard{Type: kind}
	if title != "" {
		r.Message.Entity.ID = id
		r.Message.Entity.Title = title
		r.Message.Entity.Available = true
	}
	r.Message.CreatedAt = r.Message.CreatedAt.UTC()
	return nil
}

func (s *Store) finishEntityShare(ctx context.Context, tx pgx.Tx, a connection.EntityShareAccess, conversation, other string, kind, id string, r *connection.EntityShareReceipt, fresh bool) error {
	// All notification/FK/unique waits are before this single final authority
	// statement. Session, both peers, relationship, block and entity projection
	// share one snapshot and one PG wall clock. Only Commit follows it.
	if fresh {
		if e := insertDomainAudit(ctx, tx, a.Actor.ID, "share", "chat_message", r.Message.ID, "human_entity_card_share", nil); e != nil {
			return e
		}
	}
	source := chatEntityQuery(kind)
	if source == "" {
		return connection.ErrNotFound
	}
	source = strings.ReplaceAll(source, "clock_timestamp()", "(SELECT at FROM share_clock)")
	query := `WITH share_clock AS MATERIALIZED(SELECT clock_timestamp() at),
 sender_source AS MATERIALIZED (` + strings.ReplaceAll(source, "$2", "$5") + `),
 recipient_source AS MATERIALIZED (` + strings.ReplaceAll(source, "$2", "$6") + `)
 SELECT $2::uuid=$5::uuid AND EXISTS(SELECT 1 FROM sessions ss JOIN accounts own ON own.id=ss.account_id CROSS JOIN share_clock cl
 WHERE own.id=$5 AND own.status='active' AND own.account_type='person' AND ss.token_sha256=$3
 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at
 AND ($7::boolean OR ss.authentication_method<>'dev_phone')),
 EXISTS(SELECT 1 FROM conversations cv JOIN connection_requests cr ON cr.id=cv.request_id AND cr.state='accepted'
 JOIN accounts peer ON peer.id=$6 AND peer.status='active' AND peer.account_type='person'
 WHERE cv.id=$4 AND (cv.member_a_account_id,cv.member_b_account_id) IN (($5::uuid,$6::uuid),($6::uuid,$5::uuid))
 AND (cr.scope='conversation' OR EXISTS(SELECT 1 FROM person_ties tie WHERE tie.status='active'
 AND tie.person_a_account_id=LEAST($5::uuid,$6::uuid) AND tie.person_b_account_id=GREATEST($5::uuid,$6::uuid)))
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE (block.blocker_account_id=$5 AND block.blocked_account_id=$6)
 OR (block.blocker_account_id=$6 AND block.blocked_account_id=$5))),
 (SELECT title FROM sender_source),(SELECT title FROM recipient_source)`
	var session, relationship bool
	var title, recipientTitle *string
	if e := tx.QueryRow(ctx, query, id, a.Actor.ID, a.SessionDigest[:], conversation, a.Actor.ID, other, s.devPhoneEnabled).Scan(&session, &relationship, &title, &recipientTitle); e != nil {
		return e
	}
	if !session {
		return identity.ErrUnauthorized
	}
	if !relationship || (fresh && (title == nil || recipientTitle == nil)) {
		return connection.ErrNotFound
	}
	r.Message.Entity = &connection.EntityCard{Type: kind}
	if title != nil {
		r.Message.Entity.ID = id
		r.Message.Entity.Title = *title
		r.Message.Entity.Available = true
	}
	r.Message.CreatedAt = r.Message.CreatedAt.UTC()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return tx.Commit(ctx)
}

func (s *Store) ShareHumanEntity(ctx context.Context, a connection.EntityShareAccess, conversation string, in connection.EntityShareInput) (connection.EntityShareReceipt, error) {
	if !connection.ValidEntityShare(in) {
		return connection.EntityShareReceipt{}, connection.ErrConflict
	}
	tx, other, err := s.beginEntityShare(ctx, a, conversation)
	if err != nil {
		return connection.EntityShareReceipt{}, err
	}
	defer tx.Rollback(context.Background())
	r, kind, id, err := readEntityShare(ctx, tx, a.Actor.ID, conversation, in.OperationID)
	if err == nil {
		if kind != in.Entity.Type || id != in.Entity.ID || r.Message.Body != "分享了一张卡片" {
			return connection.EntityShareReceipt{}, connection.ErrConflict
		}
		if err = s.finishEntityShare(ctx, tx, a, conversation, other, kind, id, &r, false); err != nil {
			return connection.EntityShareReceipt{}, err
		}
		return r, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return connection.EntityShareReceipt{}, err
	}
	for _, viewer := range []string{a.Actor.ID, other} {
		title, e := visibleChatEntity(ctx, tx, viewer, in.Entity.Type, in.Entity.ID)
		if e != nil {
			return connection.EntityShareReceipt{}, e
		}
		if title == "" {
			return connection.EntityShareReceipt{}, connection.ErrNotFound
		}
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND created_at>clock_timestamp()-interval '1 hour'`, a.Actor.ID).Scan(&recent); err != nil {
		return connection.EntityShareReceipt{}, err
	}
	if recent >= 60 {
		return connection.EntityShareReceipt{}, connection.ErrRateLimit
	}
	r.OperationID = in.OperationID
	err = tx.QueryRow(ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body,entity_type,entity_id,client_operation_id)
VALUES($1,$2,'分享了一张卡片',$3,$4,$5) RETURNING id,conversation_id,sender_account_id,speaker_kind,body,created_at`, conversation, a.Actor.ID, in.Entity.Type, in.Entity.ID, in.OperationID).Scan(&r.Message.ID, &r.Message.ConversationID, &r.Message.SenderID, &r.Message.SpeakerKind, &r.Message.Body, &r.Message.CreatedAt)
	if err != nil {
		return connection.EntityShareReceipt{}, err
	}
	if _, err = routeNativeNotification(ctx, tx, agentnotification.KindDirectMessage, r.Message.ID, other); err != nil {
		return connection.EntityShareReceipt{}, err
	}
	if err = s.finishEntityShare(ctx, tx, a, conversation, other, in.Entity.Type, in.Entity.ID, &r, true); err != nil {
		return connection.EntityShareReceipt{}, err
	}
	return r, nil
}

func (s *Store) GetHumanEntityShare(ctx context.Context, a connection.EntityShareAccess, conversation, operation string) (connection.EntityShareReceipt, error) {
	// Use the same sender lock as POST. A concurrent current operation either
	// commits before this exact read or rolls back. A 404 is still no license
	// for a new key: recovery retries must use the original immutable key.
	var validation connection.EntityShareInput
	validation.OperationID = operation
	validation.Entity.Type = "place"
	validation.Entity.ID = conversation
	if !connection.ValidEntityShare(validation) {
		return connection.EntityShareReceipt{}, connection.ErrConflict
	}
	tx, other, err := s.beginEntityShare(ctx, a, conversation)
	if err != nil {
		return connection.EntityShareReceipt{}, err
	}
	defer tx.Rollback(context.Background())
	r, kind, id, err := readEntityShare(ctx, tx, a.Actor.ID, conversation, operation)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.EntityShareReceipt{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.EntityShareReceipt{}, err
	}
	if err = s.finishEntityShare(ctx, tx, a, conversation, other, kind, id, &r, false); err != nil {
		return connection.EntityShareReceipt{}, err
	}
	return r, nil
}
