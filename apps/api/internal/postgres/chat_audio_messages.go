package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
)

var _ connection.HumanAudioMessageStore = (*Store)(nil)

func readAudioMessage(ctx context.Context, tx pgx.Tx, owner, op string) (connection.AudioMessageReceipt, error) {
	var r connection.AudioMessageReceipt
	e := tx.QueryRow(ctx, `SELECT m.id,m.conversation_id,m.sender_account_id,m.client_operation_id,m.created_at,a.mime_type,a.byte_size,a.sha256,a.duration_millis
 FROM conversation_messages m JOIN human_conversation_audio a ON a.message_id=m.id
 WHERE m.sender_account_id=$1 AND m.client_operation_id=$2 AND m.entity_type IS NULL AND m.entity_id IS NULL
 AND a.owner_account_id=m.sender_account_id AND a.conversation_id=m.conversation_id AND a.operation_id=m.client_operation_id`, owner, op).
		Scan(&r.MessageID, &r.ConversationID, &r.OwnerID, &r.OperationID, &r.CreatedAt, &r.MIME, &r.ByteSize, &r.SHA256, &r.DurationMillis)
	if e != nil {
		return r, e
	}
	return connection.NewAudioMessageReceipt(r.OwnerID, r.ConversationID, r.OperationID, r.MessageID, r.AudioMetadata, r.CreatedAt), nil
}
func (s *Store) SendHumanAudioMessage(ctx context.Context, a ea.Access, id string, in connection.AudioMessageInput) (connection.AudioMessageReceipt, error) {
	if !validHumanMomentID(id) || connection.ValidateAudioMessage(in) != nil {
		return connection.AudioMessageReceipt{}, connection.ErrConflict
	}
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	f, e := messageConversationFence(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	// Original account lock serializes quota and mixed text/audio operation replay.
	var sender string
	e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' FOR NO KEY UPDATE`, a.Actor.ID).Scan(&sender)
	if errors.Is(e, pgx.ErrNoRows) {
		return connection.AudioMessageReceipt{}, connection.ErrNotFound
	}
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	peer, e := conversationMember(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	var exists bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2)`, a.Actor.ID, in.OperationID).Scan(&exists)
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if exists {
		r, e := readAudioMessage(ctx, tx, a.Actor.ID, in.OperationID)
		if errors.Is(e, pgx.ErrNoRows) {
			return connection.AudioMessageReceipt{}, connection.ErrConflict
		}
		if e != nil {
			return r, e
		}
		if r.ConversationID != id || r.AudioMetadata != in.AudioMetadata {
			return connection.AudioMessageReceipt{}, connection.ErrConflict
		}
		if e = finishMessageRoute(ctx, tx, f); e != nil {
			return connection.AudioMessageReceipt{}, e
		}
		if ctx.Err() != nil {
			return connection.AudioMessageReceipt{}, ctx.Err()
		}
		if e = tx.Commit(ctx); e != nil {
			return connection.AudioMessageReceipt{}, e
		}
		return r, nil
	}
	var recent int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND created_at>now()-interval '1 hour'`, a.Actor.ID).Scan(&recent); e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if recent >= 60 {
		return connection.AudioMessageReceipt{}, connection.ErrRateLimit
	}
	var r connection.AudioMessageReceipt
	e = tx.QueryRow(ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body,client_operation_id) VALUES($1,$2,'语音消息',$3) RETURNING id,created_at`, id, a.Actor.ID, in.OperationID).Scan(&r.MessageID, &r.CreatedAt)
	if e != nil {
		return r, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO human_conversation_audio(message_id,owner_account_id,conversation_id,operation_id,mime_type,byte_size,sha256,duration_millis,content,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.MessageID, a.Actor.ID, id, in.OperationID, in.MIME, in.ByteSize, in.SHA256, in.DurationMillis, in.Content, r.CreatedAt)
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if _, e = routeNativeNotification(ctx, tx, agentnotification.KindDirectMessage, r.MessageID, peer); e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.AudioMessageReceipt{}, ctx.Err()
	}
	if e = tx.Commit(ctx); e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.AudioMessageReceipt{}, ctx.Err()
	}
	return connection.NewAudioMessageReceipt(a.Actor.ID, id, in.OperationID, r.MessageID, in.AudioMetadata, r.CreatedAt), nil
}
func (s *Store) ReadHumanAudioMessageOperation(ctx context.Context, a ea.Access, id, op string) (connection.AudioMessageReceipt, error) {
	if !validHumanMomentID(id) || !validHumanMomentID(op) {
		return connection.AudioMessageReceipt{}, connection.ErrConflict
	}
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	f, e := messageConversationFence(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if _, e = conversationMember(ctx, tx, a.Actor.ID, id); e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	r, e := readAudioMessage(ctx, tx, a.Actor.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, connection.ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if r.ConversationID != id || r.OwnerID != a.Actor.ID {
		return connection.AudioMessageReceipt{}, connection.ErrConflict
	}
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.AudioMessageReceipt{}, ctx.Err()
	}
	if e = tx.Commit(ctx); e != nil {
		return connection.AudioMessageReceipt{}, e
	}
	return r, nil
}
func (s *Store) ValidateHumanAudioMessageResponse(ctx context.Context, a ea.Access, expected connection.AudioMessageReceipt) error {
	if !connection.ValidAudioMessageReceipt(expected, a.Actor.ID, expected.ConversationID, expected.OperationID) {
		return connection.ErrConflict
	}
	r, e := s.ReadHumanAudioMessageOperation(ctx, a, expected.ConversationID, expected.OperationID)
	if e != nil {
		return e
	}
	if r != expected {
		return connection.ErrConflict
	}
	return ctx.Err()
}

// Encode the bytes before the final complete Session/route check. Do not append
// a later Session-only database wait after this authorization boundary.
func (s *Store) ReadHumanAudioMessageContent(ctx context.Context, a ea.Access, id, message string, encode connection.AudioContentEncode) ([]byte, error) {
	if !validHumanMomentID(id) || !validHumanMomentID(message) || encode == nil {
		return nil, connection.ErrConflict
	}
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return nil, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	f, e := messageConversationFence(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return nil, e
	}
	if _, e = conversationMember(ctx, tx, a.Actor.ID, id); e != nil {
		return nil, e
	}
	var r connection.AudioMessageReceipt
	var content []byte
	e = tx.QueryRow(ctx, `SELECT m.sender_account_id,m.client_operation_id,m.created_at,a.mime_type,a.byte_size,a.sha256,a.duration_millis,a.content
 FROM conversation_messages m JOIN human_conversation_audio a ON a.message_id=m.id WHERE m.id=$1 AND m.conversation_id=$2
 AND a.owner_account_id=m.sender_account_id AND a.conversation_id=m.conversation_id AND a.operation_id=m.client_operation_id`, message, id).
		Scan(&r.OwnerID, &r.OperationID, &r.CreatedAt, &r.MIME, &r.ByteSize, &r.SHA256, &r.DurationMillis, &content)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, connection.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	r = connection.NewAudioMessageReceipt(r.OwnerID, id, r.OperationID, message, r.AudioMetadata, r.CreatedAt)
	if connection.ValidateAudioMessage(connection.AudioMessageInput{OperationID: r.OperationID, AudioMetadata: r.AudioMetadata, Content: content}) != nil {
		return nil, connection.ErrConflict
	}
	raw, e := encode(r, content)
	if e != nil {
		return nil, e
	}
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return nil, e
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return raw, nil
}
