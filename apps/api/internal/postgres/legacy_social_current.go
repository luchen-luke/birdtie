package postgres

import (
	"context"
	"encoding/json"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
)

const legacySocialVisibleSelect = `SELECT cv.id,
 COALESCE(other.id,'00000000-0000-0000-0000-000000000000'::uuid) AS other_id,
 CASE WHEN cv.kind='PERSONAL_GROUP' THEN COALESCE(NULLIF(cv.legacy_title,''),'私人群聊')
 WHEN birdtie_profile_visible_to(other.id,$1::uuid) AND birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName')
 THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员') ELSE 'Birdtie 成员' END AS other_name,
 CASE WHEN cv.kind='DIRECT' THEN birdtie_person_avatar_metadata(other.id,$1::uuid) END AS avatar,
 cv.created_at,cms.last_read_at,
 (SELECT count(*) FROM conversation_messages m WHERE m.conversation_id=cv.id AND m.sender_account_id<>$1
  AND m.legacy_sequence>cms.legacy_read_sequence AND birdtie_legacy_social_message_visible(m.id,$1::uuid)) AS unread_count,
 COALESCE(CASE WHEN last.legacy_recalled_at IS NOT NULL OR last.legacy_moderated_at IS NOT NULL THEN '消息已不可查看'
 WHEN last.message_kind='IMAGE' THEN '图片' WHEN last.message_kind='ENTITY' THEN '内容卡片'
 ELSE left(regexp_replace(last.body,E'[\r\n]+',' ','g'),80) END,'') AS preview,
 COALESCE(last.created_at,cv.created_at) AS sort_at,last.created_at AS last_message_at,
 cv.kind,true AS historical,
 COALESCE((cv.kind='DIRECT' AND r.state='accepted' AND other.status='active' AND
 (r.scope='conversation' OR r.scope='friend' AND EXISTS(SELECT 1 FROM person_ties t WHERE t.request_id=r.id AND t.status='active'))),false) AS can_send
 FROM conversations cv
 JOIN conversation_member_states cms ON cms.conversation_id=cv.id AND cms.member_account_id=$1
 LEFT JOIN accounts other ON other.id=CASE WHEN cv.member_a_account_id=$1 THEN cv.member_b_account_id ELSE cv.member_a_account_id END
 LEFT JOIN user_profiles p ON p.account_id=other.id
 LEFT JOIN connection_requests r ON r.id=cv.request_id
 LEFT JOIN LATERAL(SELECT m.* FROM conversation_messages m WHERE m.conversation_id=cv.id
 AND birdtie_legacy_social_message_visible(m.id,$1::uuid) ORDER BY m.legacy_sequence DESC LIMIT 1) last ON true
 WHERE cv.legacy_source_id IS NOT NULL AND birdtie_legacy_social_member_visible(cv.id,$1::uuid)`

func (s *Store) chatPageOriginalSession(ctx context.Context, tx pgx.Tx, a ea.Access, lock bool) error {
	next, _, e := s.humanWriteSession(ctx, tx, a.SessionDigest, a.Actor, lock)
	if e != nil {
		return e
	}
	if first, valid, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, a.Actor); present && (!valid || first != next) {
		return identity.ErrControlChanged
	}
	return nil
}

// History reads never use the send-receipt validator: there was no new send.
func (s *Store) ReadImportedImageMessageContent(ctx context.Context, a ea.Access, conversation, message, variant string, encode func(connection.ImageDerivativeMetadata, []byte) ([]byte, error)) ([]byte, bool, error) {
	if !connection.ValidChatPageID(conversation) || !connection.ValidChatPageID(message) || !connection.ValidChatImageVariant(variant) || encode == nil {
		return nil, true, connection.ErrInvalidImage
	}
	tx, e := s.beginChatPage(ctx, a)
	if e != nil {
		return nil, true, e
	}
	defer tx.Rollback(context.Background())
	var imported bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM human_conversation_images WHERE message_id=$1 AND conversation_id=$2 AND legacy_source_id IS NOT NULL)`, message, conversation).Scan(&imported); e != nil {
		return nil, true, chatPageNativeFailure(e)
	}
	if !imported {
		return nil, false, nil
	}
	if e = s.chatPageOriginalSession(ctx, tx, a, true); e != nil {
		return nil, true, e
	}
	// Prevent membership, Block and source retirement while final pixels are
	// encoded. Locks precede a fresh READ COMMITTED projection and clock check.
	if _, e = tx.Exec(ctx, `LOCK TABLE account_blocks IN SHARE MODE`); e != nil {
		return nil, true, chatPageNativeFailure(e)
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT m.id FROM conversation_messages m JOIN conversation_member_states cms ON cms.conversation_id=m.conversation_id AND cms.member_account_id=$3
 JOIN conversations cv ON cv.id=m.conversation_id
 WHERE m.id=$1 AND cv.id=$2 AND birdtie_legacy_social_message_visible(m.id,$3::uuid)
 AND m.legacy_recalled_at IS NULL AND m.legacy_moderated_at IS NULL
 FOR SHARE OF m,cv,cms`, message, conversation, a.Actor.ID).Scan(&id); e != nil {
		return nil, true, connection.ErrNotFound
	}
	var rawMeta, content []byte
	column := "i.content"
	if variant == "thumbnail" {
		column = "i.thumbnail_content"
	}
	if e = tx.QueryRow(ctx, `SELECT `+chatImageProjectionSQL+`,`+column+` FROM human_conversation_images i WHERE i.message_id=$1 AND i.conversation_id=$2 AND i.legacy_source_id IS NOT NULL FOR SHARE OF i`, message, conversation).Scan(&rawMeta, &content); e != nil {
		return nil, true, connection.ErrNotFound
	}
	var image connection.ImageMetadata
	if json.Unmarshal(rawMeta, &image) != nil || !connection.ValidImageMetadata(image) {
		return nil, true, connection.ErrConflict
	}
	meta, ok := connection.ImageVariantMetadata(image, variant)
	if !ok || !connection.ValidImageContent(meta, content, variant == "thumbnail") || media.ValidateChatImageDerivative(ctx, content, meta.MIME, meta.Width, meta.Height) != nil {
		return nil, true, connection.ErrConflict
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return nil, true, e
	}
	raw, e := encode(meta, content)
	if e != nil {
		return nil, true, e
	}
	// finishChatPage reuses and checks the original captured frame/generation.
	if e = s.finishChatPage(ctx, tx, a); e != nil {
		return nil, true, e
	}
	return raw, true, nil
}

// Returns the original sequence page cursor for a source conversation; normal
// native messages retain their original v1 cursor bytes and time ordering.
func legacySocialCursor(owner, conversation string, m connection.Message) (string, error) {
	version := 1
	if m.Sequence > 0 {
		version = 2
	}
	return connection.EncodePageCursor(connection.PageCursor{Version: version, OwnerID: owner, ConversationID: conversation, Time: m.CreatedAt, ID: m.ID, Sequence: m.Sequence})
}

func legacySocialMarkRead(ctx context.Context, tx pgx.Tx, owner, conversation, message string) (connection.ReadState, bool, error) {
	var historical bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND legacy_source_id IS NOT NULL)`, conversation).Scan(&historical); e != nil {
		return connection.ReadState{}, true, e
	}
	if !historical {
		return connection.ReadState{}, false, nil
	}
	var seq int64
	if e := tx.QueryRow(ctx, `SELECT legacy_sequence FROM conversation_messages WHERE id=$1 AND conversation_id=$2 AND birdtie_legacy_social_message_visible(id,$3::uuid) FOR SHARE`, message, conversation, owner).Scan(&seq); e != nil {
		return connection.ReadState{}, true, connection.ErrNotFound
	}
	var state connection.ReadState
	state.ConversationID = conversation
	var previous int64
	if e := tx.QueryRow(ctx, `SELECT legacy_read_sequence FROM conversation_member_states WHERE conversation_id=$1 AND member_account_id=$2 FOR UPDATE`, conversation, owner).Scan(&previous); e != nil {
		return state, true, e
	}
	if seq > previous {
		e := tx.QueryRow(ctx, `UPDATE conversation_member_states SET legacy_read_sequence=$3,legacy_marked_unread=false,last_read_message_id=$4,last_read_at=clock_timestamp() WHERE conversation_id=$1 AND member_account_id=$2 RETURNING last_read_message_id,last_read_at`, conversation, owner, seq, message).Scan(&state.LastReadMessageID, &state.LastReadAt)
		return state, true, e
	}
	// Original read sequence may point at a gap. Never reset it to a lower page.
	e := tx.QueryRow(ctx, `SELECT id FROM conversation_messages WHERE conversation_id=$1 AND legacy_sequence<=$2 AND birdtie_legacy_social_message_visible(id,$3::uuid) ORDER BY legacy_sequence DESC LIMIT 1`, conversation, previous, owner).Scan(&state.LastReadMessageID)
	if e != nil {
		return state, true, connection.ErrNotFound
	}
	e = tx.QueryRow(ctx, `UPDATE conversation_member_states SET last_read_message_id=$3,last_read_at=COALESCE(last_read_at,clock_timestamp()) WHERE conversation_id=$1 AND member_account_id=$2 RETURNING last_read_at`, conversation, owner, state.LastReadMessageID).Scan(&state.LastReadAt)
	return state, true, e
}
