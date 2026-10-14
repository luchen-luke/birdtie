package postgres

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ connection.PagedStore = (*Store)(nil)

// Existing accepted conversation consent or an active friendship is required.
// A cursor neither supplies a member nor bypasses the current bilateral block.
// No Agent/Profile identity or optional enrichment grant is needed by a human.
const chatPageVisibleSQL = `WITH visible AS MATERIALIZED (
 SELECT cv.id,other.id AS other_id,
 CASE WHEN birdtie_profile_visible_to(other.id,$1::uuid)
 AND (birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName') OR (NOT EXISTS(
  SELECT 1 FROM agents ag JOIN agent_profiles ap ON ap.agent_id=ag.id
   AND ap.owner_type='PERSON' AND ap.owner_id=other.id
  WHERE ag.principal_account_id=other.id AND ag.agent_type='personal' AND ag.status='active')
  AND NOT EXISTS(SELECT 1 FROM agent_profile_field_visibility fv
   WHERE fv.owner_id=other.id AND fv.owner_type='PERSON'
    AND COALESCE(fv.rules#>>'{displayName,visibility}','')<>'PUBLIC')))
 THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员')
 ELSE 'Birdtie 成员' END AS other_name,
 birdtie_person_avatar_metadata(other.id,$1::uuid) AS avatar,
 cv.created_at,cms.last_read_at,
 (SELECT count(*) FROM conversation_messages m
  WHERE m.conversation_id=cv.id AND m.sender_account_id<>$1
   AND m.created_at>cms.baseline_at
   AND (cms.last_read_message_id IS NULL OR (m.created_at,m.id)>(
    SELECT cursor.created_at,cursor.id FROM conversation_messages cursor
    WHERE cursor.id=cms.last_read_message_id))) AS unread_count,
 COALESCE(left(regexp_replace(last.body,E'[\\r\\n]+',' ','g'),80),'') AS preview,
 COALESCE(last.created_at,cv.created_at) AS sort_at,last.created_at AS last_message_at
 FROM conversations cv
 JOIN connection_requests r ON r.id=cv.request_id AND r.state='accepted'
  AND LEAST(r.sender_account_id,r.recipient_account_id)=LEAST(cv.member_a_account_id,cv.member_b_account_id)
  AND GREATEST(r.sender_account_id,r.recipient_account_id)=GREATEST(cv.member_a_account_id,cv.member_b_account_id)
  AND (r.scope='conversation' OR r.scope='friend' AND EXISTS(
   SELECT 1 FROM person_ties t WHERE t.status='active'
    AND t.person_a_account_id=LEAST(cv.member_a_account_id,cv.member_b_account_id)
    AND t.person_b_account_id=GREATEST(cv.member_a_account_id,cv.member_b_account_id)))
 JOIN accounts own ON own.id=$1 AND own.account_type='person' AND own.status='active'
 JOIN conversation_member_states cms ON cms.conversation_id=cv.id AND cms.member_account_id=$1
 JOIN accounts other ON other.id=CASE WHEN cv.member_a_account_id=$1
  THEN cv.member_b_account_id ELSE cv.member_a_account_id END
  AND other.account_type='person' AND other.status='active'
 LEFT JOIN user_profiles p ON p.account_id=other.id
 LEFT JOIN LATERAL (SELECT m.body,m.created_at FROM conversation_messages m
  WHERE m.conversation_id=cv.id ORDER BY m.created_at DESC,m.id DESC LIMIT 1) last ON true
 WHERE (cv.member_a_account_id=$1 OR cv.member_b_account_id=$1)
 AND NOT EXISTS (SELECT 1 FROM account_blocks b
  WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=other.id)
   OR (b.blocker_account_id=other.id AND b.blocked_account_id=$1))
) `

func chatPageNativeFailure(err error) error {
	if err == nil || errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, connection.ErrInvalidPage) ||
		errors.Is(err, connection.ErrNotFound) || errors.Is(err, connection.ErrForbidden) || errors.Is(err, connection.ErrConflict) {
		return err
	}
	return mp.ErrUnavailable
}

func (s *Store) beginChatPage(ctx context.Context, a ea.Access) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, mp.ErrUnavailable
	}
	p, err := actorref.ParsePrincipal(a.Actor.AccountType, a.Actor.ID)
	if err != nil || a.Public || !a.Valid() || a.Actor.AccountType != "person" || p.Type != actorref.Person || p.ID != a.Actor.ID {
		return nil, identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, mp.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, error) {
		_ = tx.Rollback(context.Background())
		return nil, chatPageNativeFailure(e)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return fail(err)
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.Actor.ID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(identity.ErrUnauthorized)
	}
	if err != nil {
		return fail(err)
	}
	// Do not hold Session across projection/resource waits: a revocation that
	// commits while loading must be observed by the locked final clock check.
	if err = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); err != nil {
		return fail(err)
	}
	return tx, nil
}

func (s *Store) finishChatPage(ctx context.Context, tx pgx.Tx, a ea.Access) error {
	if err := s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); err != nil {
		return chatPageNativeFailure(err)
	}
	if err := checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); err != nil {
		return chatPageNativeFailure(err)
	}
	if err := tx.Commit(ctx); err != nil || ctx.Err() != nil {
		return mp.ErrUnavailable
	}
	return nil
}

func chatPageConversationScan(row pgx.Row) (connection.Conversation, time.Time, error) {
	var item connection.Conversation
	var sortAt time.Time
	err := row.Scan(&item.ID, &item.OtherAccountID, &item.OtherName, &item.CreatedAt,
		&item.LastReadAt, &item.UnreadCount, &item.LastMessagePreview, &sortAt, &item.Avatar, &item.LastMessageAt)
	item.CreatedAt, sortAt = item.CreatedAt.UTC(), sortAt.UTC()
	if item.LastReadAt != nil {
		v := item.LastReadAt.UTC()
		item.LastReadAt = &v
	}
	return item, sortAt, err
}

func chatPageConversation(ctx context.Context, tx pgx.Tx, owner, id string) (connection.Conversation, error) {
	item, _, err := chatPageConversationScan(tx.QueryRow(ctx, chatPageVisibleSQL+`
 SELECT id,other_id,other_name,created_at,last_read_at,unread_count,preview,sort_at,avatar,last_message_at FROM visible WHERE id=$2`, owner, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Conversation{}, connection.ErrNotFound
	}
	return item, chatPageNativeFailure(err)
}

func chatPageUnread(ctx context.Context, tx pgx.Tx, owner string) (int, error) {
	var count int
	err := tx.QueryRow(ctx, chatPageVisibleSQL+`SELECT COALESCE(sum(unread_count),0) FROM visible`, owner).Scan(&count)
	return count, chatPageNativeFailure(err)
}

func (s *Store) ConversationCurrent(ctx context.Context, a ea.Access, id string) (connection.Conversation, error) {
	if !connection.ValidChatPageID(id) {
		return connection.Conversation{}, connection.ErrInvalidPage
	}
	tx, err := s.beginChatPage(ctx, a)
	if err != nil {
		return connection.Conversation{}, err
	}
	defer tx.Rollback(context.Background())
	item, err := chatPageConversation(ctx, tx, a.Actor.ID, id)
	if err == nil {
		err = s.finishChatPage(ctx, tx, a)
	}
	if err != nil {
		return connection.Conversation{}, err
	}
	return item, nil
}

func (s *Store) ListConversationsPage(ctx context.Context, a ea.Access, in connection.PageRequest) (connection.ConversationPage, error) {
	out := connection.ConversationPage{Items: []connection.Conversation{}}
	in, cursor, err := connection.NormalizeChatPage(in, a.Actor.ID, "")
	if err != nil {
		return connection.ConversationPage{}, err
	}
	tx, err := s.beginChatPage(ctx, a)
	if err != nil {
		return connection.ConversationPage{}, err
	}
	defer tx.Rollback(context.Background())
	out.UnreadCount, err = chatPageUnread(ctx, tx, a.Actor.ID)
	if err != nil {
		return connection.ConversationPage{}, err
	}
	var beforeTime *time.Time
	var beforeID *string
	if cursor != nil {
		beforeTime, beforeID = &cursor.Time, &cursor.ID
	}
	rows, err := tx.Query(ctx, chatPageVisibleSQL+`
 SELECT id,other_id,other_name,created_at,last_read_at,unread_count,preview,sort_at,avatar,last_message_at
 FROM visible WHERE $2::timestamptz IS NULL OR (sort_at,id)<($2::timestamptz,$3::uuid)
 ORDER BY sort_at DESC,id DESC LIMIT $4`, a.Actor.ID, beforeTime, beforeID, in.Limit+1)
	if err != nil {
		return connection.ConversationPage{}, chatPageNativeFailure(err)
	}
	sortTimes := make([]time.Time, 0, in.Limit+1)
	for rows.Next() {
		item, sortAt, e := chatPageConversationScan(rows)
		if e != nil {
			rows.Close()
			return connection.ConversationPage{}, chatPageNativeFailure(e)
		}
		out.Items = append(out.Items, item)
		sortTimes = append(sortTimes, sortAt)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return connection.ConversationPage{}, chatPageNativeFailure(err)
	}
	if len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
		last := out.Items[len(out.Items)-1]
		raw, e := connection.EncodePageCursor(connection.PageCursor{Version: 1, OwnerID: a.Actor.ID, Time: sortTimes[len(out.Items)-1], ID: last.ID})
		if e != nil {
			return connection.ConversationPage{}, e
		}
		out.NextCursor = &raw
	}
	if err = s.finishChatPage(ctx, tx, a); err != nil {
		return connection.ConversationPage{}, err
	}
	return out, nil
}

const chatPageMessagesSQL = `SELECT m.id,m.conversation_id,m.sender_account_id,m.speaker_kind,m.body,m.created_at,m.entity_type,m.entity_id,
 a.mime_type,a.byte_size,a.sha256,a.duration_millis,` + chatImageProjectionSQL + ` FROM conversation_messages m
 LEFT JOIN human_conversation_audio a ON a.message_id=m.id
 LEFT JOIN human_conversation_images i ON i.message_id=m.id `

func chatPageMessages(ctx context.Context, tx pgx.Tx, owner, id string, cursor *connection.PageCursor, limit int, exactIDs []string) ([]connection.Message, error) {
	var beforeTime *time.Time
	var beforeID *string
	if cursor != nil {
		beforeTime, beforeID = &cursor.Time, &cursor.ID
	}
	rows, err := tx.Query(ctx, chatPageMessagesSQL+`
 WHERE m.conversation_id=$1 AND ($2::timestamptz IS NULL OR (m.created_at,m.id)<($2::timestamptz,$3::uuid))
 AND ($5::uuid[] IS NULL OR m.id=ANY($5::uuid[]))
 ORDER BY m.created_at DESC,m.id DESC LIMIT $4`, id, beforeTime, beforeID, limit, exactIDs)
	if err != nil {
		return nil, chatPageNativeFailure(err)
	}
	type reference struct{ kind, id *string }
	refs := make([]reference, 0, limit)
	items := make([]connection.Message, 0, limit)
	for rows.Next() {
		var item connection.Message
		var ref reference
		var mime, sha *string
		var size, duration *int64
		var imageRaw []byte
		if err = rows.Scan(&item.ID, &item.ConversationID, &item.SenderID, &item.SpeakerKind, &item.Body, &item.CreatedAt,
			&ref.kind, &ref.id, &mime, &size, &sha, &duration, &imageRaw); err != nil {
			rows.Close()
			return nil, chatPageNativeFailure(err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		if mime != nil || sha != nil || size != nil || duration != nil {
			if mime == nil || sha == nil || size == nil || duration == nil {
				rows.Close()
				return nil, connection.ErrConflict
			}
			meta := connection.AudioMetadata{MIME: *mime, ByteSize: *size, SHA256: *sha, DurationMillis: *duration}
			if !connection.ValidAudioMetadata(meta) {
				rows.Close()
				return nil, connection.ErrConflict
			}
			item.Audio = &meta
		}
		if err = attachChatImage(&item, imageRaw, ref.kind, ref.id); err != nil {
			rows.Close()
			return nil, err
		}
		items, refs = append(items, item), append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, chatPageNativeFailure(err)
	}
	for i, ref := range refs {
		if ref.kind == nil || ref.id == nil {
			continue
		}
		card := connection.EntityCard{Type: *ref.kind}
		title, e := visibleChatEntity(ctx, tx, owner, *ref.kind, *ref.id)
		if e != nil {
			return nil, chatPageNativeFailure(e)
		}
		if title != "" {
			card.ID, card.Title, card.Available = *ref.id, title, true
		}
		items[i].Entity = &card
	}
	return items, nil
}

func (s *Store) ListMessagesPage(ctx context.Context, a ea.Access, id string, in connection.PageRequest) (connection.MessagePage, error) {
	in, cursor, err := connection.NormalizeChatPage(in, a.Actor.ID, id)
	if err != nil || !connection.ValidChatPageID(id) {
		return connection.MessagePage{}, connection.ErrInvalidPage
	}
	tx, err := s.beginChatPage(ctx, a)
	if err != nil {
		return connection.MessagePage{}, err
	}
	defer tx.Rollback(context.Background())
	if _, err = chatPageConversation(ctx, tx, a.Actor.ID, id); err != nil {
		return connection.MessagePage{}, err
	}
	items, err := chatPageMessages(ctx, tx, a.Actor.ID, id, cursor, in.Limit+1, nil)
	if err != nil {
		return connection.MessagePage{}, err
	}
	out := connection.MessagePage{Items: items}
	if len(items) > in.Limit {
		out.Items = items[:in.Limit]
		oldest := out.Items[len(out.Items)-1]
		raw, e := connection.EncodePageCursor(connection.PageCursor{Version: 1, OwnerID: a.Actor.ID, ConversationID: id, Time: oldest.CreatedAt, ID: oldest.ID})
		if e != nil {
			return connection.MessagePage{}, e
		}
		out.NextCursor = &raw
	}
	for i, j := 0, len(out.Items)-1; i < j; i, j = i+1, j-1 {
		out.Items[i], out.Items[j] = out.Items[j], out.Items[i]
	}
	if err = s.finishChatPage(ctx, tx, a); err != nil {
		return connection.MessagePage{}, err
	}
	return out, nil
}

// Re-read the exact source projections after serialization. Ordinary snapshots
// can be older than a newer message/read cursor, but can never keep a revoked
// profile name, entity title, missing message, friendship or bilateral block.
func (s *Store) ValidateChatPageResponse(ctx context.Context, a ea.Access, ids []string, expected any) error {
	if len(ids) > connection.MaxChatPageLimit {
		return connection.ErrInvalidPage
	}
	tx, err := s.beginChatPage(ctx, a)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Finish the captured Session row wait before resolving sensitive sources.
	// A source withdrawn while that wait is queued must not retain its old
	// encoded title/name. Final current-clock validation still runs below.
	if err = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); err != nil {
		return chatPageNativeFailure(err)
	}
	if err = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); err != nil {
		return chatPageNativeFailure(err)
	}
	current := make(map[string]connection.Conversation, len(ids))
	for _, id := range ids {
		if !connection.ValidChatPageID(id) {
			return connection.ErrInvalidPage
		}
		item, e := chatPageConversation(ctx, tx, a.Actor.ID, id)
		if e != nil {
			return e
		}
		current[id] = item
	}
	validateName := func(v connection.Conversation) error {
		now, found := current[v.ID]
		if !found || now.OtherAccountID != v.OtherAccountID || now.OtherName != v.OtherName || !reflect.DeepEqual(now.Avatar, v.Avatar) {
			return connection.ErrConflict
		}
		return nil
	}
	switch v := expected.(type) {
	case connection.Conversation:
		if len(ids) != 1 {
			return connection.ErrInvalidPage
		}
		if err = validateName(v); err != nil {
			return err
		}
	case connection.ConversationPage:
		if len(ids) != len(v.Items) {
			return connection.ErrInvalidPage
		}
		for _, item := range v.Items {
			if err = validateName(item); err != nil {
				return err
			}
		}
		// Retire an aggregate if it changed while waiting, including unread from a
		// now-blocked conversation outside the returned page. Do not return an old
		// account's or a no-longer-visible conversation's count.
		count, e := chatPageUnread(ctx, tx, a.Actor.ID)
		if e != nil {
			return e
		}
		if count != v.UnreadCount {
			return connection.ErrConflict
		}
	case connection.MessagePage:
		if len(ids) != 1 || len(v.Items) > connection.MaxChatPageLimit {
			return connection.ErrInvalidPage
		}
		messageIDs := make([]string, 0, len(v.Items))
		for _, item := range v.Items {
			if item.ConversationID != ids[0] || !connection.ValidChatPageID(item.ID) {
				return connection.ErrInvalidPage
			}
			messageIDs = append(messageIDs, item.ID)
		}
		if len(messageIDs) != 0 {
			fresh, e := chatPageMessages(ctx, tx, a.Actor.ID, ids[0], nil, len(messageIDs), messageIDs)
			if e != nil {
				return e
			}
			for i, j := 0, len(fresh)-1; i < j; i, j = i+1, j-1 {
				fresh[i], fresh[j] = fresh[j], fresh[i]
			}
			if !reflect.DeepEqual(fresh, v.Items) {
				return connection.ErrConflict
			}
		}
	default:
		return connection.ErrInvalidPage
	}
	return s.finishChatPage(ctx, tx, a)
}
