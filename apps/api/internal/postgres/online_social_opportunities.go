package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	oso "github.com/birdtie/birdtie/apps/api/internal/onlinesocialopportunity"
	"github.com/jackc/pgx/v5"
	"reflect"
	"sync"
)

var _ oso.Store = (*Store)(nil)
var onlineSocialKey struct {
	sync.Once
	key [32]byte
	err error
}

func onlineSocialSeal(a oso.Access, r oso.Receipt) (string, error) {
	onlineSocialKey.Do(func() { _, onlineSocialKey.err = rand.Read(onlineSocialKey.key[:]) })
	if onlineSocialKey.err != nil {
		return "", oso.ErrUnavailable
	}
	raw, e := json.Marshal(struct {
		Actor  identity.Actor
		Digest string
		View   oso.View
		Proof  string
	}{a.Actor, hex.EncodeToString(a.Digest[:]), r.View, r.Proof})
	if e != nil {
		return "", oso.ErrUnavailable
	}
	h := hmac.New(sha256.New, onlineSocialKey.key[:])
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// The single final RC statement runs after all table, Account and Session waits.
// Only the user's own Intent, minimally visible source titles and original action
// references cross the human wire. Row tokens and private constraints stay here.
const onlineSocialSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n),
 native_actor AS MATERIALIZED(SELECT a.id,a.xmin::text owner_token,se.id session_id,se.created_at,se.expires_at,se.idle_expires_at,
 ag.id agent_id,ag.xmin::text agent_token,ap.xmin::text profile_token
 FROM accounts a JOIN sessions se ON se.account_id=a.id JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_type='PERSON' AND ap.owner_id=a.id CROSS JOIN stamp
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND se.token_sha256=$2 AND se.revoked_at IS NULL
 AND se.expires_at>stamp.n AND se.idle_expires_at>stamp.n AND ($3::boolean OR se.authentication_method<>'dev_phone')),
 own_raw AS MATERIALIZED(SELECT i.*,i.xmin::text row_token,ctx.xmin::text context_token FROM social_intents i LEFT JOIN contexts ctx ON ctx.id=i.context_id,stamp
 WHERE i.creator_account_id=$1 AND i.modality='ONLINE' AND i.status='ACTIVE' AND isfinite(i.expires_at) AND i.expires_at>stamp.n
 AND (i.context_id IS NULL OR ctx.context_type='ONLINE') AND ($4='' OR i.id::text=$4)
 ORDER BY i.updated_at DESC,i.id LIMIT 21),
 selected AS MATERIALIZED(SELECT * FROM own_raw WHERE $4<>''),
 ties AS MATERIALIZED(SELECT t.id,CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END peer,
 concat_ws(':',t.xmin::text,r.xmin::text,pa.xmin::text,pb.xmin::text) token
 FROM person_ties t JOIN connection_requests r ON r.id=t.request_id JOIN accounts pa ON pa.id=t.person_a_account_id JOIN accounts pb ON pb.id=t.person_b_account_id
 WHERE ` + humanTiePredicate + `),
 joined AS MATERIALIZED(SELECT c.id,m.xmin::text membership_token,c.xmin::text community_token,c.expires_at
 FROM community_memberships m JOIN communities c ON c.id=m.community_id,stamp
 WHERE m.user_account_id=$1 AND m.status='active' AND c.lifecycle_status='active' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>stamp.n)),
 source_intents AS MATERIALIZED(SELECT 'SOCIAL_INTENT'::text kind,i.id,i.title,i.updated_at,
 least(i.expires_at,j.expires_at) deadline,
 CASE WHEN t.id IS NOT NULL THEN 'FRIEND' WHEN j.id IS NOT NULL THEN 'COMMUNITY' ELSE 'PUBLIC' END relation,
 CASE WHEN t.id IS NOT NULL THEN t.id::text ELSE '' END tie_id,
 CASE WHEN t.id IS NULL AND j.id IS NOT NULL THEN j.id::text ELSE '' END community_id,
 concat_ws(':',i.xmin::text,creator.xmin::text,creator_profile.xmin::text,ctx.xmin::text,target.xmin::text,t.token,j.membership_token,j.community_token,owner_member.xmin::text) token
 FROM selected own JOIN social_intents i ON i.id<>own.id JOIN accounts creator ON creator.id=i.creator_account_id
 LEFT JOIN user_profiles creator_profile ON creator_profile.account_id=creator.id
 LEFT JOIN contexts ctx ON ctx.id=i.context_id LEFT JOIN social_intent_audience_targets target ON target.intent_id=i.id
 LEFT JOIN community_memberships owner_member ON owner_member.community_id=target.community_id AND owner_member.user_account_id=creator.id
 LEFT JOIN ties t ON t.peer=i.creator_account_id LEFT JOIN joined j ON j.id=target.community_id CROSS JOIN stamp
 WHERE i.creator_account_id<>$1 AND creator.account_type='person' AND creator.status='active'
 AND i.modality='ONLINE' AND i.status='ACTIVE' AND isfinite(i.expires_at) AND i.expires_at>stamp.n
 AND (i.context_id IS NULL OR ctx.context_type='ONLINE') AND (own.context_id IS NULL OR i.context_id=own.context_id)
 AND i.audience IN ('PUBLIC','FRIENDS','COMMUNITY') AND birdtie_social_intent_visible_to(i.id,$1::uuid)
 AND (i.audience<>'FRIENDS' OR t.id IS NOT NULL) AND (i.audience<>'COMMUNITY' OR j.id IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$1 AND bl.blocked_account_id=creator.id) OR (bl.blocked_account_id=$1 AND bl.blocker_account_id=creator.id))
 AND ((coalesce(own.constraints->>'category','')<>'' AND i.constraints->>'category'=own.constraints->>'category')
 OR (coalesce(own.constraints->>'category','')='' AND EXISTS(SELECT 1 FROM regexp_split_to_table(regexp_replace(lower(own.title),'帮我找|帮我|找一下|查询|线上|在线|公开|意图|伙伴|有没有|请|find|online|public|intent',' ','g'),'[[:space:][:punct:]，。！？；]+') term WHERE length(term)>=2 AND strpos(lower(i.title),term)>0)))),
 source_activities AS MATERIALIZED(SELECT 'ACTIVITY'::text kind,a.id,a.title,a.updated_at,least(a.ends_at,a.expires_at,c.expires_at,j.expires_at,activity_community.expires_at) deadline,
 CASE WHEN t.id IS NOT NULL THEN 'FRIEND' WHEN j.id IS NOT NULL THEN 'COMMUNITY' ELSE 'PUBLIC' END relation,
 CASE WHEN t.id IS NOT NULL THEN t.id::text ELSE '' END tie_id,
 CASE WHEN t.id IS NULL AND j.id IS NOT NULL THEN j.id::text ELSE '' END community_id,
 concat_ws(':',a.xmin::text,ao.xmin::text,c.xmin::text,host.xmin::text,t.token,j.membership_token,j.community_token,
 org.xmin::text,org_account.xmin::text,biz.xmin::text,biz_account.xmin::text,activity_community.xmin::text) token
 FROM selected own JOIN activities a ON true JOIN activity_organizers ao ON ao.activity_id=a.id JOIN cities c ON c.id=a.city_id
 LEFT JOIN accounts host ON host.id=a.host_account_id LEFT JOIN ties t ON t.peer=ao.person_account_id LEFT JOIN joined j ON j.id=ao.community_id
 LEFT JOIN communities activity_community ON activity_community.id=ao.community_id
 LEFT JOIN organizations org ON org.id=ao.organization_id LEFT JOIN accounts org_account ON org_account.id=org.account_id
 LEFT JOIN businesses biz ON biz.id=ao.business_id LEFT JOIN accounts biz_account ON biz_account.id=biz.account_id CROSS JOIN stamp
 WHERE a.modality='online' AND a.publication_status='published' AND a.cancelled_at IS NULL AND a.ends_at>stamp.n
 AND (a.expires_at IS NULL OR a.expires_at>stamp.n) AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>stamp.n)
 AND a.visibility IN ('public','organizer_members') AND birdtie_activity_visible_to(a.id,$1::uuid)
 AND (a.visibility='public' OR j.id IS NOT NULL)
 AND (ao.person_account_id IS NULL OR (host.status='active' AND host.account_type='person'))
 AND (ao.community_id IS NULL OR j.id IS NOT NULL OR EXISTS(SELECT 1 FROM communities oc WHERE oc.id=ao.community_id AND oc.lifecycle_status='active' AND oc.publication_status='published' AND (oc.expires_at IS NULL OR oc.expires_at>stamp.n)))
 AND (ao.organization_id IS NULL OR (org.status='active' AND org_account.status='active'))
 AND (ao.business_id IS NULL OR (biz.status='active' AND biz.claim_status='verified' AND biz_account.status='active'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$1 AND bl.blocked_account_id=a.host_account_id) OR (bl.blocked_account_id=$1 AND bl.blocker_account_id=a.host_account_id)
 OR (bl.blocker_account_id=$1 AND bl.blocked_account_id=coalesce(org.account_id,biz.account_id)) OR (bl.blocked_account_id=$1 AND bl.blocker_account_id=coalesce(org.account_id,biz.account_id)))
 AND ((coalesce(own.constraints->>'category','')<>'' AND a.category_code=own.constraints->>'category')
 OR (coalesce(own.constraints->>'category','')='' AND EXISTS(SELECT 1 FROM regexp_split_to_table(regexp_replace(lower(own.title),'帮我找|帮我|找一下|查询|线上|在线|公开|意图|伙伴|有没有|请|find|online|public|intent',' ','g'),'[[:space:][:punct:]，。！？；]+') term WHERE length(term)>=2 AND strpos(lower(a.title),term)>0)))),
 all_sources AS MATERIALIZED(SELECT * FROM source_intents UNION ALL SELECT * FROM source_activities),
 ranked AS MATERIALIZED(SELECT *,row_number() OVER(ORDER BY relation,kind,id) rank FROM all_sources),
 bounded AS MATERIALIZED(SELECT * FROM ranked WHERE rank<=31),
 body AS MATERIALIZED(SELECT coalesce(jsonb_agg(jsonb_build_object('id',$4||':'||kind||':'||id,'title',title,
 'sourceRef',jsonb_build_object('type',kind,'id',id),'relation',relation,'sourceVersion',updated_at,
 'expiresAt',deadline,'tieId',tie_id,'communityId',community_id) ORDER BY rank) FILTER(WHERE rank<=30),'[]'::jsonb) items,
 min(deadline) deadline,coalesce(bool_or(rank>30),false) truncated,
 coalesce(jsonb_agg(jsonb_build_array(kind,id,token) ORDER BY rank),'[]'::jsonb) tokens FROM bounded)
 SELECT stamp.n,least(stamp.n+interval '2 minutes',(SELECT min(expires_at) FROM own_raw),body.deadline,(SELECT min(least(expires_at,idle_expires_at)) FROM native_actor)),
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',id,'title',title,'updatedAt',updated_at,'expiresAt',expires_at) ORDER BY updated_at DESC,id) FROM (SELECT * FROM own_raw LIMIT 20) list),'[]'::jsonb),body.items,
 body.truncated OR (SELECT count(*)>20 FROM own_raw),
 encode(sha256(convert_to(jsonb_build_array((SELECT jsonb_agg(jsonb_build_array(id,row_token,context_token) ORDER BY id) FROM own_raw),body.tokens,
 (SELECT jsonb_agg(jsonb_build_array(id,owner_token,session_id,created_at,expires_at,idle_expires_at,agent_id,agent_token,profile_token)) FROM native_actor),
 (SELECT jsonb_agg(jsonb_build_array(c.id,c.xmin::text) ORDER BY c.id) FROM communities c WHERE c.id IN(SELECT community_id::uuid FROM bounded WHERE community_id<>'')))::text,'UTF8')),'hex'),
 EXISTS(SELECT 1 FROM native_actor),($4='' OR EXISTS(SELECT 1 FROM selected)) FROM stamp CROSS JOIN body`

func (s *Store) captureOnlineSocial(ctx context.Context, a oso.Access, id string) (oso.Receipt, error) {
	var r oso.Receipt
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || !a.Valid() {
		return r, oso.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return r, oso.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';`+humanSocialRelations+`;LOCK TABLE agent_profiles IN ACCESS SHARE MODE`); e != nil {
		return r, oso.ErrUnavailable
	}
	var account string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.Actor.ID).Scan(&account); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return r, oso.ErrDenied
		}
		return r, oso.ErrUnavailable
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID); e != nil {
		return r, e
	}
	var intents, items []byte
	var current, found bool
	e = tx.QueryRow(ctx, onlineSocialSQL, a.Actor.ID, a.Digest[:], s.devPhoneEnabled, id).Scan(&r.View.ObservedAt, &r.View.ValidUntil, &intents, &items, &r.View.Truncated, &r.Proof, &current, &found)
	if e != nil || ctx.Err() != nil {
		return r, oso.ErrUnavailable
	}
	if !current {
		return r, identity.ErrUnauthorized
	}
	if !found {
		return r, oso.ErrDenied
	}
	r.View.SchemaVersion = oso.SchemaVersion
	r.View.OwnerID = a.Actor.ID
	r.View.IntentID = id
	if json.Unmarshal(intents, &r.View.Intents) != nil || json.Unmarshal(items, &r.View.Items) != nil || oso.Validate(r.View) != nil {
		return r, oso.ErrUnavailable
	}
	// Commit cannot wait on any writes or deferred triggers: this is read-only.
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return oso.Receipt{}, oso.ErrUnavailable
	}
	return r, nil
}

func (s *Store) ReadOnlineSocialOpportunityOptions(ctx context.Context, a oso.Access) (oso.Receipt, error) {
	return s.readOnlineSocial(ctx, a, "")
}
func (s *Store) ReadOnlineSocialOpportunities(ctx context.Context, a oso.Access, id string) (oso.Receipt, error) {
	if !businessconsole.ValidID(id) {
		return oso.Receipt{}, oso.ErrInvalid
	}
	return s.readOnlineSocial(ctx, a, id)
}
func (s *Store) readOnlineSocial(ctx context.Context, a oso.Access, id string) (oso.Receipt, error) {
	r, e := s.captureOnlineSocial(ctx, a, id)
	if e != nil {
		return r, e
	}
	r.Seal, e = onlineSocialSeal(a, r)
	return r, e
}
func (s *Store) RevalidateOnlineSocialOpportunities(ctx context.Context, a oso.Access, r oso.Receipt) error {
	if !a.Valid() || r.View.OwnerID != a.Actor.ID || oso.Validate(r.View) != nil {
		return oso.ErrDenied
	}
	seal, e := onlineSocialSeal(a, r)
	if e != nil {
		return e
	}
	if !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return oso.ErrDenied
	}
	next, e := s.captureOnlineSocial(ctx, a, r.View.IntentID)
	if e != nil {
		return e
	}
	if !r.View.ValidUntil.After(next.View.ObservedAt) {
		return oso.ErrChanged
	}
	if next.Proof != r.Proof || !reflect.DeepEqual(next.View.Intents, r.View.Intents) || !reflect.DeepEqual(next.View.Items, r.View.Items) || next.View.Truncated != r.View.Truncated {
		return oso.ErrChanged
	}
	return nil
}
