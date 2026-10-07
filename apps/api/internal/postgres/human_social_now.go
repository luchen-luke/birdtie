package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ socialnow.HumanTiesStore = (*Store)(nil)
var _ socialnow.HumanIntentsStore = (*Store)(nil)
var _ socialnow.HumanOpportunitiesStore = (*Store)(nil)

// Reuse the existing native Account-before-Session current-clock primitives.
// Their name denotes their first consumer, not a requirement for Org/Agent data.
func (s *Store) AuthenticateHumanSocial(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	return s.AuthenticateOrganizationAgent(ctx, digest)
}
func (s *Store) ValidateHumanSocialResponse(ctx context.Context, digest [32]byte, actor identity.Actor) error {
	if ctx == nil || s == nil || s.pool == nil || ctx.Err() != nil {
		return socialnow.ErrUnavailable
	}
	principal, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if e != nil || principal.Type != actorref.Person || actor.AccountType != "person" || principal.ID != actor.ID || digest == ([32]byte{}) {
		return identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return socialnow.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	// Separate statements guarantee Account precedes Session, independent of JOIN
	// planner order, and match the actual human Profile writer's lock order.
	var id string
	e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, actor.ID).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.ErrUnauthorized
	}
	if e != nil {
		return socialnow.ErrUnavailable
	}
	if e = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			return e
		}
		return socialnow.ErrUnavailable
	}
	// New statement after every wait, with actual PG clock. Never refresh idle.
	if e = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			return e
		}
		return socialnow.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return socialnow.ErrUnavailable
	}
	return nil
}

var _ socialnow.HumanSessionStore = (*Store)(nil)

// Relation waits happen before the Session row is locked. They are schema/read
// relation locks, not row-level revocation locks or a global write lock. Current
// source rows are then materialized in one final RC statement. A source change
// committed after that statement is not represented as permanently fresh.
const humanSocialRelations = `LOCK TABLE accounts,sessions,person_ties,connection_requests,
 account_blocks,social_intents,social_intent_audience_targets,social_intent_invitations,
 user_profiles,agents,agent_profiles,agent_profile_field_visibility,communities,community_memberships,activities,activity_organizers,
 activity_invitations,places,cities,contexts,person_contexts,follows,organizations,
 organization_memberships,businesses,business_memberships,business_venue_relations,
 venues,venue_candidates IN ACCESS SHARE MODE`

type humanSocialPayload struct {
	Now     time.Time             `json:"now"`
	Ties    []connection.Tie      `json:"ties"`
	Intents []socialintent.Record `json:"intents"`
	Inputs  opportunity.Inputs    `json:"inputs"`
}

func (s *Store) humanSocialRead(ctx context.Context, digest [32]byte, actor identity.Actor, mode, id string) (humanSocialPayload, error) {
	var payload humanSocialPayload
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return payload, socialnow.ErrUnavailable
	}
	principal, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if e != nil || principal.Type != actorref.Person || principal.ID != actor.ID || digest == ([32]byte{}) {
		return payload, identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return payload, socialnow.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return payload, socialnow.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, humanSocialRelations); e != nil {
		return payload, socialnow.ErrUnavailable
	}
	var account string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, actor.ID).Scan(&account); errors.Is(e, pgx.ErrNoRows) {
		return payload, identity.ErrUnauthorized
	} else if e != nil {
		return payload, socialnow.ErrUnavailable
	}
	var session string
	if e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE account_id=$1 AND token_sha256=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, actor.ID, digest[:], s.devPhoneEnabled).Scan(&session); errors.Is(e, pgx.ErrNoRows) {
		return payload, identity.ErrUnauthorized
	} else if e != nil {
		return payload, socialnow.ErrUnavailable
	}
	var raw []byte
	// $1 principal and $2 viewer are deliberately the same authenticated account;
	// publishedActivityFrom retains its original $2 viewer ACL parameter.
	e = tx.QueryRow(ctx, humanSocialSQL, actor.ID, actor.ID, digest[:], s.devPhoneEnabled, mode, id).Scan(&raw)
	if e != nil || ctx.Err() != nil {
		return payload, socialnow.ErrUnavailable
	}
	if len(raw) == 0 {
		return payload, identity.ErrUnauthorized
	}
	if e = json.Unmarshal(raw, &payload); e != nil {
		return payload, socialnow.ErrUnavailable
	}
	if e = checkHumanMomentSession(ctx, tx, digest, actor.ID, s.devPhoneEnabled); e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			return humanSocialPayload{}, e
		}
		return humanSocialPayload{}, socialnow.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return humanSocialPayload{}, socialnow.ErrUnavailable
	}
	return payload, nil
}

func (s *Store) ListHumanTies(ctx context.Context, digest [32]byte, actor identity.Actor) ([]connection.Tie, error) {
	p, e := s.humanSocialRead(ctx, digest, actor, "ties", "")
	return p.Ties, e
}
func (s *Store) ListHumanVisibleSocialIntents(ctx context.Context, digest [32]byte, actor identity.Actor) ([]socialintent.Record, error) {
	p, e := s.humanSocialRead(ctx, digest, actor, "intents", "")
	return p.Intents, e
}
func (s *Store) GetHumanVisibleSocialIntent(ctx context.Context, digest [32]byte, actor identity.Actor, id string) (socialintent.Record, error) {
	principal, e := actorref.ParsePrincipal("person", id)
	if e != nil || principal.ID != id {
		return socialintent.Record{}, socialintent.ErrNotFound
	}
	p, e := s.humanSocialRead(ctx, digest, actor, "intent", id)
	if e != nil {
		return socialintent.Record{}, e
	}
	if len(p.Intents) != 1 {
		return socialintent.Record{}, socialintent.ErrNotFound
	}
	return p.Intents[0], nil
}
func (s *Store) ListHumanOpportunities(ctx context.Context, digest [32]byte, actor identity.Actor) ([]opportunity.Candidate, error) {
	p, e := s.humanSocialRead(ctx, digest, actor, "opportunities", "")
	if e != nil {
		return nil, e
	}
	p.Inputs.PersonID = actor.ID
	return opportunity.Generate(p.Now, p.Inputs), nil
}

const humanIntentJSON = `jsonb_build_object('id',i.id,'creatorAccountId',i.creator_account_id,
 'type',i.intent_type,'title',i.title,'constraints',i.constraints,'audience',i.audience,
 'cityId',coalesce((SELECT target.city_id FROM social_intent_audience_targets target WHERE target.intent_id=i.id),''),'communityId',coalesce((SELECT target.community_id::text FROM social_intent_audience_targets target WHERE target.intent_id=i.id),''),'modality',i.modality,'contextId',i.context_id,'status',i.status,'expiresAt',i.expires_at,
 'createdAt',i.created_at,'updatedAt',i.updated_at)`
const humanTiePredicate = `t.status='active' AND (t.person_a_account_id=$1 OR t.person_b_account_id=$1)
 AND r.scope='friend' AND r.state='accepted'
 AND LEAST(r.sender_account_id,r.recipient_account_id)=t.person_a_account_id
 AND GREATEST(r.sender_account_id,r.recipient_account_id)=t.person_b_account_id
 AND pa.account_type='person' AND pa.status='active' AND pb.account_type='person' AND pb.status='active'
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
 (b.blocker_account_id=t.person_a_account_id AND b.blocked_account_id=t.person_b_account_id)
 OR (b.blocker_account_id=t.person_b_account_id AND b.blocked_account_id=t.person_a_account_id))`

// All JSON below is materialized by PostgreSQL from current native rows. It is
// not caller JSON, a provider self-description, a calibrated model judgment or
// a current-purpose authorization resolver for Agent processing.
const humanSocialSQL = `WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS n),
 current_ties AS MATERIALIZED(SELECT t.id,t.created_at,
 CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END AS peer
 FROM person_ties t JOIN connection_requests r ON r.id=t.request_id
 JOIN accounts pa ON pa.id=t.person_a_account_id JOIN accounts pb ON pb.id=t.person_b_account_id
 WHERE ` + humanTiePredicate + `),
 owned_source AS MATERIALIZED(SELECT i.* FROM social_intents i WHERE $5='opportunities' AND i.creator_account_id=$1 ORDER BY i.created_at DESC,i.id DESC LIMIT 100),
 own_intents AS MATERIALIZED(SELECT i.* FROM owned_source i,clock WHERE i.intent_type='FIND_ACTIVITY' AND i.status='ACTIVE' AND i.expires_at>clock.n
 AND (i.audience<>'LOCAL' OR EXISTS(SELECT 1 FROM social_intent_audience_targets target JOIN cities city ON city.id=target.city_id WHERE target.intent_id=i.id AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock.n)))),
 visible_intents AS MATERIALIZED(SELECT i.* FROM social_intents i,clock
 WHERE $5 IN ('intents','intent') AND ($5<>'intent' OR i.id::text=$6)
 AND i.status='ACTIVE' AND i.expires_at>clock.n AND birdtie_social_intent_visible_to(i.id,$1::uuid)
 AND (i.audience<>'LOCAL' OR EXISTS(SELECT 1 FROM social_intent_audience_targets target JOIN cities city ON city.id=target.city_id WHERE target.intent_id=i.id AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock.n)))
 AND (i.audience<>'FRIENDS' OR EXISTS(SELECT 1 FROM current_ties t WHERE t.peer=i.creator_account_id))
 AND (i.audience<>'COMMUNITY' OR EXISTS(SELECT 1 FROM social_intent_audience_targets target
 JOIN communities c ON c.id=target.community_id AND c.lifecycle_status='active' AND c.publication_status='published'
 WHERE target.intent_id=i.id AND (c.expires_at IS NULL OR c.expires_at>clock.n)))
 ORDER BY i.created_at DESC,i.id DESC LIMIT 50),
 city_nodes AS MATERIALIZED(SELECT ctx.id,ctx.city_id FROM contexts ctx JOIN cities city ON city.id=ctx.city_id AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>(SELECT n FROM clock))
 WHERE ctx.context_type='CITY' AND (EXISTS(SELECT 1 FROM person_contexts pc WHERE pc.context_id=ctx.id AND pc.person_account_id=$1 AND pc.relation IN ('current','destination'))
 OR EXISTS(SELECT 1 FROM own_intents i WHERE i.context_id=ctx.id))),
 joined AS MATERIALIZED(SELECT m.community_id FROM community_memberships m JOIN communities c ON c.id=m.community_id
 WHERE m.user_account_id=$1 AND m.status='active' AND c.lifecycle_status='active' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>(SELECT n FROM clock))),
 followed AS MATERIALIZED(SELECT CASE WHEN f.person_account_id IS NOT NULL THEN 'PERSON' WHEN f.organization_id IS NOT NULL THEN 'ORGANIZATION' WHEN f.community_id IS NOT NULL THEN 'COMMUNITY' ELSE 'BUSINESS' END AS kind,
 coalesce(f.person_account_id,f.organization_id,f.community_id,f.business_id)::text AS id
 FROM follows f LEFT JOIN accounts person ON person.id=f.person_account_id AND person.status='active' AND person.account_type='person'
 LEFT JOIN user_profiles p ON p.account_id=person.id AND p.visibility='public'
 LEFT JOIN organizations o ON o.id=f.organization_id AND o.status='active'
 LEFT JOIN communities c ON c.id=f.community_id AND c.lifecycle_status='active' AND c.publication_status='published' AND c.visibility='public' AND (c.expires_at IS NULL OR c.expires_at>(SELECT n FROM clock))
 LEFT JOIN businesses b ON b.id=f.business_id AND b.status='active' AND b.claim_status='verified'
 WHERE f.follower_account_id=$1 AND (p.account_id IS NOT NULL OR
 (o.id IS NOT NULL AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=o.account_id AND a.status='active')) OR c.id IS NOT NULL OR
 (b.id IS NOT NULL AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=b.account_id AND a.status='active')))
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE f.person_account_id IS NOT NULL AND
 ((block.blocker_account_id=$1 AND block.blocked_account_id=f.person_account_id) OR (block.blocker_account_id=f.person_account_id AND block.blocked_account_id=$1))) ORDER BY f.created_at DESC,f.id DESC),
 supply AS MATERIALIZED(SELECT candidate.payload FROM own_intents i
 LEFT JOIN city_nodes selected ON selected.id=i.context_id
 CROSS JOIN LATERAL(SELECT jsonb_build_object('activity',jsonb_build_object(
 'id',a.id,'cityId',a.city_id,'placeId',a.place_id,'title',a.title,'categoryCode',a.category_code,
 'startsAt',a.starts_at,'endsAt',a.ends_at,'status','upcoming','visibility',a.visibility,
 'organizer',jsonb_build_object('type',CASE WHEN ao.person_account_id IS NOT NULL THEN 'PERSON' WHEN ao.community_id IS NOT NULL THEN 'COMMUNITY' WHEN ao.organization_id IS NOT NULL THEN 'ORGANIZATION' ELSE 'BUSINESS' END,
 'id',coalesce(ao.person_account_id,ao.community_id,ao.organization_id,ao.business_id))),
 'place',jsonb_build_object('id',p.id,'cityId',p.city_id,'name',p.name,'addressLabel',p.address_label)) AS payload
 ` + publishedActivityFrom + `
 AND (c.expires_at IS NULL OR c.expires_at>(SELECT n FROM clock))
 AND (i.audience<>'LOCAL' OR EXISTS(SELECT 1 FROM social_intent_audience_targets target WHERE target.intent_id=i.id AND target.city_id=a.city_id))
 AND i.modality='IN_PERSON' AND (i.context_id IS NULL OR selected.id IS NOT NULL)
 AND p.id IS NOT NULL AND p.city_id=a.city_id AND a.cancelled_at IS NULL AND a.ends_at>(SELECT n FROM clock)
 AND (a.expires_at IS NULL OR a.expires_at>(SELECT n FROM clock)) AND (p.expires_at IS NULL OR p.expires_at>(SELECT n FROM clock))
 AND (ao.business_id IS NULL OR a.place_id IS NULL OR EXISTS(SELECT 1 FROM business_venue_relations r
 JOIN venues current_venue ON current_venue.place_id=r.place_id
 JOIN venue_candidates current_candidate ON current_candidate.id=current_venue.source_candidate_id AND current_candidate.status='approved'
 WHERE r.business_id=ao.business_id AND r.place_id=a.place_id AND r.status='verified' AND current_venue.expires_at>(SELECT n FROM clock)))
 AND (coalesce(i.constraints->>'category','')='' OR a.category_code=i.constraints->>'category')
 AND (coalesce(i.constraints->>'placeId','')='' OR a.place_id::text=i.constraints->>'placeId')
 AND (coalesce(i.constraints->>'areaLabel','')='' OR strpos(lower(p.name),lower(i.constraints->>'areaLabel'))>0 OR strpos(lower(coalesce(p.address_label,'')),lower(i.constraints->>'areaLabel'))>0)
 AND (selected.city_id IS NULL OR a.city_id=selected.city_id)
 ORDER BY a.starts_at,a.id LIMIT 100) candidate),
 current_authority AS MATERIALIZED(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id,clock
 WHERE s.account_id=$1 AND s.token_sha256=$3 AND s.revoked_at IS NULL AND s.expires_at>clock.n AND s.idle_expires_at>clock.n
 AND ($4::boolean OR s.authentication_method<>'dev_phone') AND a.account_type='person' AND a.status='active')
 SELECT CASE WHEN EXISTS(SELECT 1 FROM current_authority) THEN jsonb_build_object(
 'now',(SELECT n FROM clock),
 'ties',CASE WHEN $5='ties' THEN coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'otherAccountId',t.peer,'otherName',CASE WHEN birdtie_agent_profile_field_allowed(t.peer,$1::uuid,'displayName') THEN coalesce(nullif(p.display_name,''),nullif(peer.handle,''),'Birdtie 成员') ELSE 'Birdtie 成员' END,'createdAt',t.created_at) ORDER BY t.created_at DESC,t.id DESC) FROM (SELECT * FROM current_ties ORDER BY created_at DESC,id DESC LIMIT 100) t JOIN accounts peer ON peer.id=t.peer LEFT JOIN user_profiles p ON p.account_id=t.peer),'[]'::jsonb) ELSE '[]'::jsonb END,
 'intents',coalesce((SELECT jsonb_agg(` + humanIntentJSON + ` ORDER BY i.created_at DESC,i.id DESC) FROM visible_intents i),'[]'::jsonb),
 'inputs',jsonb_build_object('Intents',coalesce((SELECT jsonb_agg(` + humanIntentJSON + ` ORDER BY i.created_at DESC,i.id DESC) FROM own_intents i),'[]'::jsonb),
 'Supply',coalesce((SELECT jsonb_agg(DISTINCT payload) FROM supply),'[]'::jsonb),
 'ContextCities',coalesce((SELECT jsonb_object_agg(id::text,city_id) FROM city_nodes),'{}'::jsonb),
 'DeclaredCities',coalesce((SELECT jsonb_object_agg(ctx.city_id,pc.relation) FROM person_contexts pc JOIN city_nodes ctx ON ctx.id=pc.context_id WHERE pc.person_account_id=$1 AND pc.relation IN ('current','destination')),'{}'::jsonb),
 'TiedPeople',coalesce((SELECT jsonb_object_agg(peer::text,true) FROM current_ties),'{}'::jsonb),
 'JoinedCommunities',coalesce((SELECT jsonb_object_agg(community_id::text,true) FROM joined),'{}'::jsonb),
 'FollowedOrganizers',coalesce((SELECT jsonb_object_agg(kind||':'||id,true) FROM followed),'{}'::jsonb)))
 ELSE NULL END`
