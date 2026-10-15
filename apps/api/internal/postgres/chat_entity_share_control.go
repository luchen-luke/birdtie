package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

var _ connection.EntityShareControlStore = (*Store)(nil)

func (s *Store) beginEntityShareControl(ctx context.Context, a connection.EntityShareAccess, target connection.EntityShareRecipient, op string) (context.Context, humanWriteFrame, error) {
	ctx, first, e := s.humanWriteBound(ctx, a.SessionDigest, a.Actor)
	f := humanWriteFrame{a: a.Actor, d: a.SessionDigest, session: first, family: "ENTITY_SHARE", op: op}
	if e != nil {
		return ctx, f, e
	}
	if !connection.ValidEntityShareRecipient(target) || !identity.HumanControlUUID(op) {
		return ctx, f, identity.ErrControlInvalid
	}
	f.tx, e = s.pool.Begin(ctx)
	if e != nil {
		return ctx, f, identity.ErrControlUnavailable
	}
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,143))`, a.Actor.ID+":ENTITY_SHARE:"+op); e != nil {
		_ = f.tx.Rollback(context.Background())
		return ctx, f, identity.ErrControlUnavailable
	}
	return ctx, f, nil
}
func entityShareControlReadTx(ctx context.Context, f humanWriteFrame) (connection.EntityShareControlReceipt, error) {
	v := connection.EntityShareControlReceipt{OperationID: f.op}
	var cv, message, kind, id *string
	var digest []byte
	var at time.Time
	e := f.tx.QueryRow(ctx, `SELECT recipient_type,recipient_id,status,conversation_id,message_id,entity_type,entity_id,payload_sha256,committed_at FROM human_entity_share_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.a.ID, f.op).Scan(&v.Recipient.Type, &v.Recipient.ID, &v.Status, &cv, &message, &kind, &id, &digest, &at)
	if e != nil {
		return v, e
	}
	if v.Status == "STOPPED" {
		return v, nil
	}
	if cv == nil || message == nil || kind == nil || id == nil || len(digest) != 32 {
		return v, identity.ErrControlUnavailable
	}
	v.ConversationID = *cv
	v.MessageID = *message
	v.Entity = &connection.EntityShareRef{Type: *kind, ID: *id}
	v.PayloadDigest = hex.EncodeToString(digest)
	at = at.UTC()
	v.CommittedAt = &at
	return v, nil
}

// Old074 may legitimately contain the same operation in several conversations.
// Read only the exact original namespace; never invent a global zero effect.
func entityShareControlLegacy(ctx context.Context, f humanWriteFrame, target connection.EntityShareRecipient) (connection.EntityShareControlReceipt, bool, error) {
	v := connection.EntityShareControlReceipt{OperationID: f.op, Recipient: target}
	if target.Type == "CONVERSATION" {
		var ref connection.EntityShareRef
		var at time.Time
		e := f.tx.QueryRow(ctx, `SELECT conversation_id,id,entity_type,entity_id,created_at FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2 AND conversation_id=$3 AND speaker_kind='human' AND body='分享了一张卡片' AND entity_type IN('activity','place','person','community','organization','business','moment') AND entity_id IS NOT NULL`, f.a.ID, f.op, target.ID).Scan(&v.ConversationID, &v.MessageID, &ref.Type, &ref.ID, &at)
		if e == nil {
			v.Status = "SUCCEEDED"
			v.Entity = &ref
			in := connection.EntityShareControlInput{OperationID: f.op, Entity: ref}
			v.PayloadDigest = connection.EntityShareControlDigest(target, in)
			at = at.UTC()
			v.CommittedAt = &at
			return v, true, nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return v, false, identity.ErrControlUnavailable
		}
	}
	var any bool
	if e := f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2)`, f.a.ID, f.op).Scan(&any); e != nil {
		return v, false, identity.ErrControlUnavailable
	}
	if any {
		return v, false, identity.ErrControlChanged
	}
	return v, false, nil
}
func (s *Store) entityShareControlHistory(ctx context.Context, a connection.EntityShareAccess, target connection.EntityShareRecipient, op string, stop bool) (connection.EntityShareControlReceipt, error) {
	ctx, f, e := s.beginEntityShareControl(ctx, a, target, op)
	if e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	v, e := entityShareControlReadTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) {
		var found bool
		v, found, e = entityShareControlLegacy(ctx, f, target)
		if e != nil {
			return v, e
		}
		if !found {
			if !stop {
				if e = s.humanWriteFinish(ctx, f); e != nil {
					return v, e
				}
				return v, connection.ErrNotFound
			}
			_, e = f.tx.Exec(ctx, `INSERT INTO human_entity_share_operations(actor_account_id,operation_id,recipient_type,recipient_id,status)VALUES($1,$2,$3,$4,'STOPPED')`, f.a.ID, op, target.Type, target.ID)
			if e != nil {
				return v, identity.ErrControlUnavailable
			}
			v.Status = "STOPPED"
		}
	} else if e != nil {
		return v, identity.ErrControlUnavailable
	}
	if v.Recipient != target {
		return connection.EntityShareControlReceipt{}, identity.ErrControlChanged
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	return v, nil
}
func (s *Store) ReadEntityShareControlOperation(ctx context.Context, a connection.EntityShareAccess, target connection.EntityShareRecipient, op string) (connection.EntityShareControlReceipt, error) {
	return s.entityShareControlHistory(ctx, a, target, op, false)
}
func (s *Store) StopEntityShareControlOperation(ctx context.Context, a connection.EntityShareAccess, target connection.EntityShareRecipient, op string) (connection.EntityShareControlReceipt, error) {
	return s.entityShareControlHistory(ctx, a, target, op, true)
}

type entityShareRecipientSource struct {
	peer, request, conversation, epoch string
	route                              messageRouteFence
}

func entityShareRecipientCurrent(ctx context.Context, f humanWriteFrame, target connection.EntityShareRecipient, lock bool) (entityShareRecipientSource, error) {
	var r entityShareRecipientSource
	var query string
	if target.Type == "CONVERSATION" {
		query = `SELECT CASE WHEN cv.member_a_account_id=$1 THEN cv.member_b_account_id ELSE cv.member_a_account_id END,cr.id::text,cv.id::text,concat_ws(':',cv.xmin::text,cr.xmin::text,own.xmin::text,peer.xmin::text,COALESCE((SELECT string_agg(concat_ws(':',t.id::text,t.xmin::text),',' ORDER BY t.id) FROM person_ties t WHERE t.person_a_account_id=LEAST(cv.member_a_account_id,cv.member_b_account_id) AND t.person_b_account_id=GREATEST(cv.member_a_account_id,cv.member_b_account_id)),'')) FROM conversations cv JOIN connection_requests cr ON cr.id=cv.request_id AND cr.state='accepted' AND LEAST(cr.sender_account_id,cr.recipient_account_id)=LEAST(cv.member_a_account_id,cv.member_b_account_id) AND GREATEST(cr.sender_account_id,cr.recipient_account_id)=GREATEST(cv.member_a_account_id,cv.member_b_account_id) JOIN accounts own ON own.id=$1 AND own.account_type='person' AND own.status='active' JOIN accounts peer ON peer.id=CASE WHEN cv.member_a_account_id=$1 THEN cv.member_b_account_id ELSE cv.member_a_account_id END AND peer.account_type='person' AND peer.status='active' WHERE cv.id=$2 AND $1 IN(cv.member_a_account_id,cv.member_b_account_id) AND(cr.scope='conversation' OR EXISTS(SELECT 1 FROM person_ties t WHERE t.person_a_account_id=LEAST(cv.member_a_account_id,cv.member_b_account_id) AND t.person_b_account_id=GREATEST(cv.member_a_account_id,cv.member_b_account_id) AND t.status='active'))`
		if lock {
			query += " FOR UPDATE OF cv"
		}
	} else {
		query = `SELECT CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END,cr.id::text,''::text,concat_ws(':',t.xmin::text,cr.xmin::text,own.xmin::text,peer.xmin::text) FROM person_ties t JOIN connection_requests cr ON cr.id=t.request_id AND cr.state='accepted' AND cr.scope='friend' AND LEAST(cr.sender_account_id,cr.recipient_account_id)=t.person_a_account_id AND GREATEST(cr.sender_account_id,cr.recipient_account_id)=t.person_b_account_id JOIN accounts own ON own.id=$1 AND own.account_type='person' AND own.status='active' JOIN accounts peer ON peer.id=CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END AND peer.account_type='person' AND peer.status='active' WHERE t.id=$2 AND t.status='active' AND $1 IN(t.person_a_account_id,t.person_b_account_id)`
		if lock {
			query += " FOR UPDATE OF t"
		}
	}
	e := f.tx.QueryRow(ctx, query, f.a.ID, target.ID).Scan(&r.peer, &r.request, &r.conversation, &r.epoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, connection.ErrNotFound
	}
	if e != nil {
		return r, identity.ErrControlUnavailable
	}
	return r, nil
}
func entitySharePairLocks(ctx context.Context, f humanWriteFrame, peer string) error {
	first, second := f.a.ID+":BLOCKPAIR:"+peer, peer+":BLOCKPAIR:"+f.a.ID
	if first > second {
		first, second = second, first
	}
	if _, e := f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,142)),pg_advisory_xact_lock(hashtextextended($2,142))`, first, second); e != nil {
		return identity.ErrControlUnavailable
	}
	return nil
}
func entityShareRecipientFence(ctx context.Context, f humanWriteFrame, target connection.EntityShareRecipient) (entityShareRecipientSource, error) {
	first, e := entityShareRecipientCurrent(ctx, f, target, false)
	if e != nil {
		return first, e
	}
	if e = entitySharePairLocks(ctx, f, first.peer); e != nil {
		return first, e
	}
	var route messageRouteFence
	if target.Type == "CONVERSATION" {
		route, e = messageConversationFence(ctx, f.tx, f.a.ID, target.ID)
	} else {
		route, e = startMessageRoute(ctx, f.tx, f.a.ID, first.peer)
		if e == nil && route.route != mp.Allow {
			e = connection.ErrNotFound
		}
	}
	if e != nil {
		return first, e
	}
	current, e := entityShareRecipientCurrent(ctx, f, target, true)
	if e != nil {
		return current, e
	}
	if current.peer != first.peer || current.epoch != first.epoch {
		return current, identity.ErrControlChanged
	}
	current.route = route
	return current, nil
}
func entityShareSourceClosure() string {
	q := entityActionClosureSQL()
	q = strings.Replace(q, "FROM conversations cv JOIN connection_requests r ON r.id=cv.request_id WHERE", "FROM conversations cv JOIN connection_requests r ON r.id=cv.request_id WHERE cv.id<>$3::uuid AND", 1)
	extra := ` UNION ALL SELECT 'moment',m.id::text,m.xmin::text FROM moments m WHERE m.id=$1
 UNION ALL SELECT 'moment-author',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT author_account_id FROM moments WHERE id=$1)
 UNION ALL SELECT 'moment-place',p.id::text,p.xmin::text FROM places p WHERE p.id IN(SELECT place_id FROM moments WHERE id=$1)
 UNION ALL SELECT 'moment-city',c.id::text,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM moments WHERE id=$1)
 UNION ALL SELECT 'moment-place-city',c.id::text,c.xmin::text FROM cities c WHERE c.id IN(SELECT p.city_id FROM places p JOIN moments m ON m.place_id=p.id WHERE m.id=$1)
 UNION ALL SELECT 'moment-media-set',s.moment_id::text,s.xmin::text FROM public_moment_media_sets s WHERE s.moment_id=$1
 UNION ALL SELECT 'moment-media',concat_ws(':',a.set_version,a.position),a.xmin::text FROM public_moment_media_attachments a WHERE a.moment_id=$1
 UNION ALL SELECT 'moment-video',concat_ws(':',v.set_version,v.position),v.xmin::text FROM public_moment_media_video_payloads v WHERE v.moment_id=$1
 UNION ALL SELECT 'retained-place',p.id::text,` + retainedPlaceReadBindingSQL("p") + ` FROM places p WHERE p.id=$1 OR p.id IN(SELECT place_id FROM activities WHERE id=$1 UNION SELECT place_id FROM moments WHERE id=$1)
 UNION ALL SELECT 'block-generation',b.owner_account_id::text||':'||b.target_account_id::text,concat_ws(':',b.version::text,b.xmin::text) FROM person_block_control_epochs b WHERE b.owner_account_id=$2 OR b.target_account_id=$2
 UNION ALL SELECT 'business-member',b.business_id::text||':'||b.user_account_id::text,b.xmin::text FROM business_memberships b WHERE b.business_id=$1 OR b.business_id IN(SELECT business_id FROM activity_organizers WHERE activity_id=$1)`
	return strings.Replace(q, ") tokens", extra+") tokens", 1)
}
func entityShareSource(ctx context.Context, f humanWriteFrame, viewer string, ref connection.EntityShareRef, createdConversation string) (string, string, *time.Time, error) {
	if createdConversation == "" {
		createdConversation = "00000000-0000-0000-0000-000000000000"
	}
	var title string
	var raw []byte
	var deadline *time.Time
	q := `WITH share_clock AS MATERIALIZED(SELECT clock_timestamp() at),args AS(SELECT $3::uuid ignored,$4::boolean ignoredflag),` + entityActionFieldCommunitiesCTE + `,action_source AS MATERIALIZED(` + chatEntityQuery(ref.Type) + `) SELECT COALESCE((SELECT title FROM action_source),''),(` + entityShareSourceClosure() + `),least((SELECT a.expires_at FROM activities a WHERE a.id=$1),(SELECT c.expires_at FROM communities c WHERE c.id=$1),(SELECT min(city.expires_at) FROM cities city WHERE city.id IN(SELECT city_id FROM activities WHERE id=$1 UNION SELECT city_id FROM places WHERE id=$1 UNION SELECT city_id FROM communities WHERE id=$1 UNION SELECT city_id FROM moments WHERE id=$1)),(SELECT min(` + currentRetainedPlaceDeadlineSQL("p", "(SELECT at FROM share_clock)") + `) FROM places p WHERE p.id=$1 OR p.id IN(SELECT place_id FROM activities WHERE id=$1 UNION SELECT place_id FROM moments WHERE id=$1)),(SELECT min(c.expires_at) FROM action_field_communities c),(SELECT min(v.expires_at) FROM venues v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1 UNION SELECT venue_place_id FROM activities WHERE id=$1)))`
	q = strings.ReplaceAll(q, "clock_timestamp()", "(SELECT at FROM share_clock)")
	q = strings.Replace(q, "SELECT (SELECT at FROM share_clock) at", "SELECT clock_timestamp() at", 1)
	if e := f.tx.QueryRow(ctx, q, ref.ID, viewer, createdConversation, false, ref.Type).Scan(&title, &raw, &deadline); e != nil {
		return "", "", nil, identity.ErrControlUnavailable
	}
	if title == "" {
		return "", "", nil, connection.ErrNotFound
	}
	return title, identity.HumanControlTextHash(string(raw)), deadline, nil
}
func entityShareQuota(ctx context.Context, f humanWriteFrame) (int, string, *time.Time, error) {
	var n int
	var epochs string
	var deadline *time.Time
	e := f.tx.QueryRow(ctx, `SELECT count(*)::int,COALESCE(string_agg(concat_ws(':',id::text,xmin::text),',' ORDER BY id),''),min(created_at+interval '1 hour') FROM conversation_messages WHERE sender_account_id=$1 AND created_at>clock_timestamp()-interval '1 hour'`, f.a.ID).Scan(&n, &epochs, &deadline)
	if e != nil {
		return n, "", deadline, identity.ErrControlUnavailable
	}
	return n, identity.HumanControlTextHash(epochs), deadline, nil
}
func entityShareDeadline(values ...*time.Time) *time.Time {
	var out *time.Time
	for _, v := range values {
		if v != nil && (out == nil || v.Before(*out)) {
			x := *v
			out = &x
		}
	}
	return out
}
func entityShareSourceCurrent(ctx context.Context, f humanWriteFrame, r entityShareRecipientSource, ref connection.EntityShareRef, created string) (string, string, *time.Time, error) {
	title, sender, until, e := entityShareSource(ctx, f, f.a.ID, ref, created)
	if e != nil {
		return "", "", nil, e
	}
	_, receiver, receiverUntil, e := entityShareSource(ctx, f, r.peer, ref, created)
	if e != nil {
		return "", "", nil, e
	}
	n, quota, quotaUntil, e := entityShareQuota(ctx, f)
	if e != nil {
		return "", "", nil, e
	}
	if n >= 60 {
		return "", "", nil, connection.ErrRateLimit
	}
	source := humanWriteHashJSON([]any{r.peer, r.epoch, r.route.static, r.route.version, sender, receiver, quota})
	return title, source, entityShareDeadline(until, receiverUntil, quotaUntil, &r.route.until), nil
}

type entityShareControlProof struct {
	s      *Store
	a      connection.EntityShareAccess
	target connection.EntityShareRecipient
	in     connection.EntityShareControlInput
	frame  identity.HumanControlFrame
	source string
	until  time.Time
}

func (p *entityShareControlProof) RevalidateHumanControl(ctx context.Context) error {
	ctx = identity.WithHumanControlFrame(ctx, p.a.SessionDigest, p.a.Actor, p.frame)
	ctx, f, e := p.s.beginEntityShareControl(ctx, p.a, p.target, p.in.OperationID)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	r, e := entityShareRecipientFence(ctx, f, p.target)
	if e != nil {
		return e
	}
	_, source, until, e := entityShareSourceCurrent(ctx, f, r, p.in.Entity, "")
	if e != nil {
		return e
	}
	if source != p.source || until != nil && until.Before(p.until) {
		return identity.ErrControlChanged
	}
	return p.s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.until, func() error {
		latest, e := entityShareRecipientCurrent(ctx, f, p.target, true)
		if e != nil {
			return e
		}
		latest.route = r.route
		_, current, currentUntil, e := entityShareSourceCurrent(ctx, f, latest, p.in.Entity, "")
		if e != nil {
			return e
		}
		if current != p.source || currentUntil != nil && currentUntil.Before(p.until) {
			return identity.ErrControlChanged
		}
		return finishMessageRoute(ctx, f.tx, r.route)
	}))
}
func (s *Store) PreviewEntityShareControl(ctx context.Context, a connection.EntityShareAccess, target connection.EntityShareRecipient, in connection.EntityShareControlInput) (connection.EntityShareControlPreview, error) {
	in, e := connection.NormalizeEntityShareControl(target, in, false)
	if e != nil {
		return connection.EntityShareControlPreview{}, e
	}
	ctx, f, e := s.beginEntityShareControl(ctx, a, target, in.OperationID)
	if e != nil {
		return connection.EntityShareControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = entityShareControlReadTx(ctx, f); e == nil {
		return connection.EntityShareControlPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return connection.EntityShareControlPreview{}, identity.ErrControlUnavailable
	}
	if _, found, e := entityShareControlLegacy(ctx, f, target); e != nil || found {
		if e == nil {
			e = identity.ErrControlChanged
		}
		return connection.EntityShareControlPreview{}, e
	}
	r, e := entityShareRecipientFence(ctx, f, target)
	if e != nil {
		return connection.EntityShareControlPreview{}, e
	}
	title, source, until, e := entityShareSourceCurrent(ctx, f, r, in.Entity, "")
	if e != nil {
		return connection.EntityShareControlPreview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, target.Type+":"+target.ID, "SHARE", connection.EntityShareControlDigest(target, in), source, until)
	if e != nil {
		return connection.EntityShareControlPreview{}, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error {
		latest, e := entityShareRecipientCurrent(ctx, f, target, true)
		if e != nil {
			return e
		}
		latest.route = r.route
		_, current, _, e := entityShareSourceCurrent(ctx, f, latest, in.Entity, "")
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return finishMessageRoute(ctx, f.tx, r.route)
	})); e != nil {
		return connection.EntityShareControlPreview{}, e
	}
	return connection.EntityShareControlPreview{OperationID: in.OperationID, Recipient: target, Entity: in.Entity, EntityTitle: title, RecipientAccountID: r.peer, SourceVersion: source, PayloadDigest: base.PayloadDigest, ObservedAt: base.ObservedAt, ExpiresAt: base.ExpiresAt, Approval: base.Approval, ResponseProof: &entityShareControlProof{s, a, target, in, f.session, source, base.ExpiresAt}}, nil
}
func (s *Store) CommitEntityShareControl(ctx context.Context, a connection.EntityShareAccess, target connection.EntityShareRecipient, in connection.EntityShareControlInput) (connection.EntityShareControlReceipt, error) {
	in, e := connection.NormalizeEntityShareControl(target, in, true)
	if e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	ctx, f, e := s.beginEntityShareControl(ctx, a, target, in.OperationID)
	if e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := connection.EntityShareControlDigest(target, in)
	if old, e := entityShareControlReadTx(ctx, f); e == nil {
		if old.Recipient != target || old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return connection.EntityShareControlReceipt{}, identity.ErrControlChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return connection.EntityShareControlReceipt{}, e
		}
		return old, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return connection.EntityShareControlReceipt{}, identity.ErrControlUnavailable
	}
	if old, found, e := entityShareControlLegacy(ctx, f, target); e != nil {
		return old, e
	} else if found {
		if old.PayloadDigest != digest {
			return connection.EntityShareControlReceipt{}, identity.ErrControlChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return old, e
		}
		return old, nil
	}
	r, e := entityShareRecipientFence(ctx, f, target)
	if e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	_, source, sourceUntil, e := entityShareSourceCurrent(ctx, f, r, in.Entity, "")
	if e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	if source != in.SourceVersion {
		return connection.EntityShareControlReceipt{}, identity.ErrControlChanged
	}
	deadline, e := s.humanWriteApprove(ctx, f, target.Type+":"+target.ID, "SHARE", digest, source, in.Approval, sourceUntil)
	if e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	created := ""
	conversation := r.conversation
	if target.Type == "FRIEND_TIE" {
		cv, isNew, e := friendConversationEffectTx(ctx, f.tx, a.Actor.ID, r.peer, r.request)
		if e != nil {
			return connection.EntityShareControlReceipt{}, e
		}
		conversation = cv.ID
		if isNew {
			created = cv.ID
		}
	}
	legacy := connection.EntityShareInput{OperationID: in.OperationID}
	legacy.Entity.Type = in.Entity.Type
	legacy.Entity.ID = in.Entity.ID
	effect, e := entityShareMessageEffectTx(ctx, f.tx, a.Actor.ID, conversation, r.peer, legacy)
	if e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	if e = insertDomainAudit(ctx, f.tx, a.Actor.ID, "share", "chat_message", effect.Message.ID, "human_entity_card_share", nil); e != nil {
		return connection.EntityShareControlReceipt{}, identity.ErrControlUnavailable
	}
	raw, _ := hex.DecodeString(digest)
	if _, e = f.tx.Exec(ctx, `INSERT INTO human_entity_share_operations(actor_account_id,operation_id,recipient_type,recipient_id,status,conversation_id,message_id,entity_type,entity_id,payload_sha256)VALUES($1,$2,$3,$4,'SUCCEEDED',$5,$6,$7,$8,$9)`, a.Actor.ID, in.OperationID, target.Type, target.ID, conversation, effect.Message.ID, in.Entity.Type, in.Entity.ID, raw); e != nil {
		return connection.EntityShareControlReceipt{}, identity.ErrControlUnavailable
	}
	out, e := entityShareControlReadTx(ctx, f)
	if e != nil {
		return out, identity.ErrControlUnavailable
	}
	_, postQuota, quotaUntil, e := entityShareQuota(ctx, f)
	if e != nil {
		return out, e
	}
	if quotaUntil != nil && quotaUntil.Before(deadline) {
		deadline = *quotaUntil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, deadline, func() error {
		latest, e := entityShareRecipientCurrent(ctx, f, target, true)
		if e != nil {
			return e
		}
		if latest.peer != r.peer || latest.epoch != r.epoch {
			return identity.ErrControlChanged
		}
		actual, e := entityShareRecipientCurrent(ctx, f, connection.EntityShareRecipient{Type: "CONVERSATION", ID: conversation}, true)
		if e != nil || actual.peer != r.peer {
			return identity.ErrControlChanged
		}
		_, sender, _, e := entityShareSource(ctx, f, a.Actor.ID, in.Entity, created)
		if e != nil {
			return e
		}
		_, receiver, _, e := entityShareSource(ctx, f, r.peer, in.Entity, created)
		if e != nil {
			return e
		}
		_, quota, _, e := entityShareQuota(ctx, f)
		if e != nil {
			return e
		}
		if quota != postQuota {
			return identity.ErrControlChanged
		} // Re-evaluate the exact preview source with only this new message excluded
		// from its hourly quota and only this newly created conversation excluded.
		var oldQuota string
		if e = f.tx.QueryRow(ctx, `SELECT COALESCE(string_agg(concat_ws(':',id::text,xmin::text),',' ORDER BY id),'') FROM conversation_messages WHERE sender_account_id=$1 AND id<>$2 AND created_at>clock_timestamp()-interval '1 hour'`, a.Actor.ID, effect.Message.ID).Scan(&oldQuota); e != nil {
			return identity.ErrControlUnavailable
		}
		current := humanWriteHashJSON([]any{r.peer, r.epoch, r.route.static, r.route.version, sender, receiver, identity.HumanControlTextHash(oldQuota)})
		if current != source {
			return identity.ErrControlChanged
		}
		return finishMessageRoute(ctx, f.tx, r.route)
	})); e != nil {
		return connection.EntityShareControlReceipt{}, e
	}
	return out, nil
}
