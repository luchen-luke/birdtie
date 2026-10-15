package postgres

import (
	"context"
	"encoding/json"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/jackc/pgx/v5"
)

func legacySocialMessages(ctx context.Context, tx pgx.Tx, owner, id string, cursor *connection.PageCursor, limit int, exactIDs []string) ([]connection.Message, error) {
	var before any
	if cursor != nil {
		if cursor.Version != 2 {
			return nil, connection.ErrInvalidPage
		}
		before = cursor.Sequence
	}
	rows, e := tx.Query(ctx, `SELECT m.id,m.conversation_id,m.sender_account_id,m.speaker_kind,m.body,m.created_at,
 m.entity_type,m.entity_id,m.message_kind,m.legacy_source_id IS NOT NULL,m.legacy_sequence,
 m.legacy_sticker_pack_id,m.legacy_sticker_id,m.legacy_entity_kind,
 m.legacy_recalled_at IS NOT NULL OR m.legacy_moderated_at IS NOT NULL,`+chatImageProjectionSQL+`,
 a.mime_type,a.byte_size,a.sha256,a.duration_millis
 FROM conversation_messages m LEFT JOIN human_conversation_images i ON i.message_id=m.id
 LEFT JOIN human_conversation_audio a ON a.message_id=m.id
 WHERE m.conversation_id=$1 AND birdtie_legacy_social_message_visible(m.id,$2::uuid)
 AND ($3::bigint IS NULL OR m.legacy_sequence<$3::bigint) AND($4::uuid[] IS NULL OR m.id=ANY($4::uuid[]))
 ORDER BY m.legacy_sequence DESC LIMIT $5`, id, owner, before, exactIDs, limit)
	if e != nil {
		return nil, chatPageNativeFailure(e)
	}
	type ref struct{ kind, target, legacyKind *string }
	refs := []ref{}
	items := []connection.Message{}
	for rows.Next() {
		var v connection.Message
		var r ref
		var historical bool
		var pack, sticker *string
		var image []byte
		var audioMIME, audioSHA *string
		var audioSize, audioDuration *int64
		if e = rows.Scan(&v.ID, &v.ConversationID, &v.SenderID, &v.SpeakerKind, &v.Body, &v.CreatedAt, &r.kind, &r.target, &v.Kind, &historical, &v.Sequence, &pack, &sticker, &r.legacyKind, &v.Unavailable, &image, &audioMIME, &audioSize, &audioSHA, &audioDuration); e != nil {
			rows.Close()
			return nil, chatPageNativeFailure(e)
		}
		v.CreatedAt = v.CreatedAt.UTC()
		if historical {
			v.Origin = connection.LegacySocialOrigin
		}
		if v.Unavailable {
			v.Body = ""
		} else {
			if len(image) > 0 && string(image) != "null" {
				var meta connection.ImageMetadata
				if json.Unmarshal(image, &meta) != nil || !connection.ValidImageMetadata(meta) {
					rows.Close()
					return nil, connection.ErrConflict
				}
				v.Image = &meta
			}
			if audioMIME != nil && audioSHA != nil && audioSize != nil && audioDuration != nil {
				meta := connection.AudioMetadata{MIME: *audioMIME, SHA256: *audioSHA, ByteSize: *audioSize, DurationMillis: *audioDuration}
				if !connection.ValidAudioMetadata(meta) {
					rows.Close()
					return nil, connection.ErrConflict
				}
				v.Audio = &meta
			}
			if v.Kind == "STICKER" {
				if pack == nil || sticker == nil {
					rows.Close()
					return nil, connection.ErrConflict
				}
				v.Sticker = &connection.Sticker{PackID: *pack, StickerID: *sticker}
			}
		}
		items = append(items, v)
		refs = append(refs, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, chatPageNativeFailure(e)
	}
	for i, r := range refs {
		if items[i].Unavailable {
			continue
		}
		kind := ""
		if r.kind != nil {
			kind = *r.kind
		} else if r.legacyKind != nil {
			kind = *r.legacyKind
		}
		if kind == "" {
			continue
		}
		card := connection.EntityCard{Type: kind}
		if r.target != nil {
			title, e := visibleChatEntity(ctx, tx, owner, kind, *r.target)
			if e != nil {
				return nil, chatPageNativeFailure(e)
			}
			if title != "" {
				card.ID, card.Title, card.Available = *r.target, title, true
			}
		}
		items[i].Entity = &card
	}
	return items, nil
}
