package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
)

var _ connection.HumanMessageOperationStore = (*Store)(nil)

func readHumanMessageOperation(ctx context.Context, tx pgx.Tx, owner, op string) (connection.Message, error) {
	var m connection.Message
	e := tx.QueryRow(ctx, `SELECT id,conversation_id,sender_account_id,speaker_kind,body,created_at FROM conversation_messages
 WHERE sender_account_id=$1 AND client_operation_id=$2 AND entity_type IS NULL AND entity_id IS NULL`, owner, op).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.SpeakerKind, &m.Body, &m.CreatedAt)
	return m, e
}
func (s *Store) SendHumanMessageOperation(ctx context.Context, a ea.Access, id string, in connection.MessageOperationInput) (connection.MessageOperationReceipt, error) {
	if !validHumanMomentID(id) || !connection.ValidMessageOperation(in) {
		return connection.MessageOperationReceipt{}, connection.ErrConflict
	}
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	m, e := s.sendMessageWithOperation(ctx, a.Actor.ID, id, in.Body, "", "", in.OperationID)
	if e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	return connection.NewMessageOperationReceipt(a.Actor.ID, in.OperationID, m), nil
}
func (s *Store) ReadHumanMessageOperation(ctx context.Context, a ea.Access, id, op string) (connection.MessageOperationReceipt, error) {
	if !validHumanMomentID(id) || !validHumanMomentID(op) {
		return connection.MessageOperationReceipt{}, connection.ErrConflict
	}
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	f, e := messageConversationFence(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	if _, e = conversationMember(ctx, tx, a.Actor.ID, id); e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	m, e := readHumanMessageOperation(ctx, tx, a.Actor.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		return connection.MessageOperationReceipt{}, connection.ErrNotFound
	}
	if e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	if m.ConversationID != id || m.SenderID != a.Actor.ID {
		return connection.MessageOperationReceipt{}, connection.ErrConflict
	}
	// Original route closure and captured Session share the original final clock.
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.MessageOperationReceipt{}, ctx.Err()
	}
	if e = tx.Commit(ctx); e != nil {
		return connection.MessageOperationReceipt{}, e
	}
	return connection.NewMessageOperationReceipt(a.Actor.ID, op, m), nil
}
func (s *Store) ValidateHumanMessageOperationResponse(ctx context.Context, a ea.Access, expected connection.MessageOperationReceipt) error {
	if !connection.ValidMessageOperationReceipt(expected, a.Actor.ID, expected.ConversationID, expected.OperationID) {
		return connection.ErrConflict
	}
	current, e := s.ReadHumanMessageOperation(ctx, a, expected.ConversationID, expected.OperationID)
	if e != nil {
		return e
	}
	if current != expected {
		return connection.ErrConflict
	}
	return ctx.Err()
}
