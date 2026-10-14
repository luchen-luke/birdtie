package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
)

var _ connection.HumanImageMessageStore = (*Store)(nil)

const chatImageProjectionSQL = `CASE WHEN i.message_id IS NULL THEN NULL ELSE jsonb_build_object(
 'schemaVersion','human.conversation_image.v1','mimeType',i.mime_type,'byteSize',i.byte_size,'sha256',i.sha256,'width',i.width,'height',i.height,
 'thumbnail',jsonb_build_object('mimeType','image/png','byteSize',i.thumbnail_byte_size,'sha256',i.thumbnail_sha256,'width',i.thumbnail_width,'height',i.thumbnail_height)) END`

// Both list projections reject partial or mixed media; final page re-read uses
// the same immutable image projection, not a new body or public media URL.
func attachChatImage(item *connection.Message, raw []byte, entityKind, entityID *string) error {
	if (entityKind == nil) != (entityID == nil) {
		return connection.ErrConflict
	}
	if len(raw) != 0 {
		var meta connection.ImageMetadata
		if json.Unmarshal(raw, &meta) != nil || !connection.ValidImageMetadata(meta) || item.SpeakerKind != "human" {
			return connection.ErrConflict
		}
		item.Image = &meta
	}
	kinds := 0
	if entityKind != nil {
		kinds++
	}
	if item.Audio != nil {
		kinds++
	}
	if item.Image != nil {
		kinds++
	}
	if kinds > 1 {
		return connection.ErrConflict
	}
	return nil
}

func readImageMessage(ctx context.Context, tx pgx.Tx, owner, op string) (connection.ImageMessageReceipt, error) {
	var r connection.ImageMessageReceipt
	var caption, captionSHA string
	var present bool
	e := tx.QueryRow(ctx, `SELECT m.id,m.conversation_id,m.sender_account_id,m.client_operation_id,m.created_at,m.body,
 i.recipient_account_id,i.purpose,i.pixel_risk,i.input_mime_type,i.input_size,i.input_sha256,i.caption_present,i.caption_sha256,
 i.mime_type,i.byte_size,i.sha256,i.width,i.height,i.thumbnail_byte_size,i.thumbnail_sha256,i.thumbnail_width,i.thumbnail_height
 FROM conversation_messages m JOIN human_conversation_images i ON i.message_id=m.id
 WHERE m.sender_account_id=$1 AND m.client_operation_id=$2 AND m.speaker_kind='human' AND m.entity_type IS NULL AND m.entity_id IS NULL
 AND i.owner_account_id=m.sender_account_id AND i.conversation_id=m.conversation_id AND i.operation_id=m.client_operation_id AND i.created_at=m.created_at
 AND NOT EXISTS(SELECT 1 FROM human_conversation_audio a WHERE a.message_id=m.id)`, owner, op).
		Scan(&r.MessageID, &r.ConversationID, &r.OwnerID, &r.OperationID, &r.CreatedAt, &caption, &r.RecipientAccountID, &r.Purpose, &r.PixelRisk, &r.InputMIME, &r.InputSize, &r.InputSHA256, &present, &captionSHA,
			&r.Image.MIME, &r.Image.ByteSize, &r.Image.SHA256, &r.Image.Width, &r.Image.Height, &r.Image.Thumbnail.ByteSize, &r.Image.Thumbnail.SHA256, &r.Image.Thumbnail.Width, &r.Image.Thumbnail.Height)
	if e != nil {
		return r, e
	}
	if !present {
		if caption != "图片消息" {
			return r, connection.ErrConflict
		}
		caption = ""
	}
	r.Image.Schema, r.Image.Thumbnail.MIME = connection.ConversationImageSchema, "image/png"
	in := connection.ImageMessageInput{OperationID: r.OperationID, RecipientAccountID: r.RecipientAccountID, Purpose: r.Purpose, Caption: caption, PixelRisk: r.PixelRisk, MIME: r.InputMIME, InputSize: r.InputSize, InputSHA256: r.InputSHA256}
	r = connection.NewImageMessageReceipt(r.OwnerID, r.ConversationID, r.MessageID, in, r.Image, r.CreatedAt)
	if r.CaptionSHA256 != captionSHA || !connection.ValidImageMessageReceipt(r, owner, r.ConversationID, op) {
		return connection.ImageMessageReceipt{}, connection.ErrConflict
	}
	return r, nil
}

// The bounded authorization precheck closes before decoding. No account sender
// lock is held while pixels are decoded; the write transaction rechecks all
// current authority and the shared effect address after codec completion.
func (s *Store) authorizeChatImageBuild(ctx context.Context, a ea.Access, id, recipient string) error {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	f, e := messageConversationFence(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return e
	}
	peer, e := conversationMember(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return e
	}
	if peer != recipient {
		return connection.ErrConflict
	}
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return tx.Commit(ctx)
}

func (s *Store) SendHumanImageMessage(ctx context.Context, a ea.Access, id string, in connection.ImageMessageInput) (connection.ImageMessageReceipt, error) {
	return s.sendHumanImageMessage(ctx, a, id, in, (&media.LocalChatImageDerivativeBuilder{}).Build)
}

// Unexported codec seam is for deterministic final-authority integration tests;
// HTTP callers cannot select a builder or bypass independent server decoding.
func (s *Store) sendHumanImageMessage(ctx context.Context, a ea.Access, id string, in connection.ImageMessageInput, build func(context.Context, []byte, string) (media.ChatImageDerivatives, error)) (connection.ImageMessageReceipt, error) {
	if !validHumanMomentID(id) || connection.ValidateImageMessage(in) != nil {
		return connection.ImageMessageReceipt{}, connection.ErrConflict
	}
	if build == nil {
		return connection.ImageMessageReceipt{}, connection.ErrConflict
	}
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if e = s.authorizeChatImageBuild(ctx, a, id, in.RecipientAccountID); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	derivative, e := build(ctx, in.Content, in.MIME)
	if e != nil {
		if errors.Is(e, media.ErrImageType) || errors.Is(e, media.ErrImageInvalid) || errors.Is(e, media.ErrImageLimit) {
			return connection.ImageMessageReceipt{}, connection.ErrInvalidImage
		}
		return connection.ImageMessageReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.ImageMessageReceipt{}, ctx.Err()
	}
	if !connection.ValidImageMetadata(derivative.Metadata) || derivative.Metadata.MIME != in.MIME || !connection.ValidImageContent(derivative.Metadata.ImageDerivativeMetadata, derivative.Full, false) || !connection.ValidImageContent(derivative.Metadata.Thumbnail, derivative.Thumbnail, true) {
		return connection.ImageMessageReceipt{}, connection.ErrInvalidImage
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	f, e := messageConversationFence(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	var sender string
	e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' FOR NO KEY UPDATE`, a.Actor.ID).Scan(&sender)
	if errors.Is(e, pgx.ErrNoRows) {
		return connection.ImageMessageReceipt{}, connection.ErrNotFound
	}
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	peer, e := conversationMember(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if peer != in.RecipientAccountID {
		return connection.ImageMessageReceipt{}, connection.ErrConflict
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2)`, a.Actor.ID, in.OperationID).Scan(&exists); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if exists {
		r, e := readImageMessage(ctx, tx, a.Actor.ID, in.OperationID)
		if errors.Is(e, pgx.ErrNoRows) {
			return connection.ImageMessageReceipt{}, connection.ErrConflict
		}
		if e != nil {
			return r, e
		}
		if !connection.ImageReceiptMatchesInput(r, id, in) {
			return connection.ImageMessageReceipt{}, connection.ErrConflict
		}
		if e = finishMessageRoute(ctx, tx, f); e != nil {
			return connection.ImageMessageReceipt{}, e
		}
		if ctx.Err() != nil {
			return connection.ImageMessageReceipt{}, ctx.Err()
		}
		if e = tx.Commit(ctx); e != nil {
			return connection.ImageMessageReceipt{}, e
		}
		return r, ctx.Err()
	}
	var recent int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND created_at>now()-interval '1 hour'`, a.Actor.ID).Scan(&recent); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if recent >= 60 {
		return connection.ImageMessageReceipt{}, connection.ErrRateLimit
	}
	body := in.Caption
	if body == "" {
		body = "图片消息"
	}
	var r connection.ImageMessageReceipt
	e = tx.QueryRow(ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body,client_operation_id) VALUES($1,$2,$3,$4) RETURNING id,created_at`, id, a.Actor.ID, body, in.OperationID).Scan(&r.MessageID, &r.CreatedAt)
	if e != nil {
		return r, e
	}
	r = connection.NewImageMessageReceipt(a.Actor.ID, id, r.MessageID, in, derivative.Metadata, r.CreatedAt)
	m, t := r.Image.ImageDerivativeMetadata, r.Image.Thumbnail
	_, e = tx.Exec(ctx, `INSERT INTO human_conversation_images(message_id,owner_account_id,recipient_account_id,conversation_id,operation_id,purpose,pixel_risk,input_mime_type,input_size,input_sha256,caption_present,caption_sha256,mime_type,byte_size,sha256,width,height,content,thumbnail_byte_size,thumbnail_sha256,thumbnail_width,thumbnail_height,thumbnail_content,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`, r.MessageID, a.Actor.ID, peer, id, in.OperationID, in.Purpose, in.PixelRisk, in.MIME, in.InputSize, in.InputSHA256, in.Caption != "", r.CaptionSHA256, m.MIME, m.ByteSize, m.SHA256, m.Width, m.Height, derivative.Full, t.ByteSize, t.SHA256, t.Width, t.Height, derivative.Thumbnail, r.CreatedAt)
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if _, e = routeNativeNotification(ctx, tx, agentnotification.KindDirectMessage, r.MessageID, peer); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.ImageMessageReceipt{}, ctx.Err()
	}
	if e = tx.Commit(ctx); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	return r, ctx.Err()
}

func (s *Store) ReadHumanImageMessageOperation(ctx context.Context, a ea.Access, id, op string) (connection.ImageMessageReceipt, error) {
	if !validHumanMomentID(id) || !validHumanMomentID(op) {
		return connection.ImageMessageReceipt{}, connection.ErrConflict
	}
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	f, e := messageConversationFence(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	peer, e := conversationMember(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	r, e := readImageMessage(ctx, tx, a.Actor.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, connection.ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if r.ConversationID != id || r.RecipientAccountID != peer {
		return connection.ImageMessageReceipt{}, connection.ErrConflict
	}
	if e = finishMessageRoute(ctx, tx, f); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	if ctx.Err() != nil {
		return connection.ImageMessageReceipt{}, ctx.Err()
	}
	if e = tx.Commit(ctx); e != nil {
		return connection.ImageMessageReceipt{}, e
	}
	return r, ctx.Err()
}
func (s *Store) ValidateHumanImageMessageResponse(ctx context.Context, a ea.Access, expected connection.ImageMessageReceipt) error {
	if !connection.ValidImageMessageReceipt(expected, a.Actor.ID, expected.ConversationID, expected.OperationID) {
		return connection.ErrConflict
	}
	r, e := s.ReadHumanImageMessageOperation(ctx, a, expected.ConversationID, expected.OperationID)
	if e != nil {
		return e
	}
	if r != expected {
		return connection.ErrConflict
	}
	return ctx.Err()
}
func (s *Store) ReadHumanImageMessageContent(ctx context.Context, a ea.Access, id, message, variant string, encode connection.ImageContentEncode) ([]byte, error) {
	if !validHumanMomentID(id) || !validHumanMomentID(message) || !connection.ValidChatImageVariant(variant) || encode == nil {
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
	peer, e := conversationMember(ctx, tx, a.Actor.ID, id)
	if e != nil {
		return nil, e
	}
	var owner, op string
	e = tx.QueryRow(ctx, `SELECT m.sender_account_id,m.client_operation_id FROM conversation_messages m JOIN human_conversation_images i ON i.message_id=m.id WHERE m.id=$1 AND m.conversation_id=$2`, message, id).Scan(&owner, &op)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, connection.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	r, e := readImageMessage(ctx, tx, owner, op)
	if e != nil {
		return nil, e
	}
	if r.MessageID != message || r.ConversationID != id || !((owner == a.Actor.ID && r.RecipientAccountID == peer) || (owner == peer && r.RecipientAccountID == a.Actor.ID)) {
		return nil, connection.ErrConflict
	}
	meta, ok := connection.ImageVariantMetadata(r.Image, variant)
	if !ok {
		return nil, connection.ErrConflict
	}
	column := "thumbnail_content"
	if variant == "full" {
		column = "content"
	}
	var content []byte
	e = tx.QueryRow(ctx, `SELECT `+column+` FROM human_conversation_images WHERE message_id=$1`, message).Scan(&content)
	if e != nil {
		return nil, e
	}
	if !connection.ValidImageContent(meta, content, variant == "thumbnail") || media.ValidateChatImageDerivative(ctx, content, meta.MIME, meta.Width, meta.Height) != nil {
		return nil, connection.ErrConflict
	}
	raw, e := encode(r, meta, content)
	if e != nil {
		return nil, e
	}
	// No Session-only wait after this complete current route/session gate.
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
