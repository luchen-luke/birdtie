package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	sc "github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5"
)

var _ sc.CurrentStore = (*Store)(nil)

type sharedHistoryFrame struct {
	data              sc.Signals
	proof             string
	observed, expires time.Time
}

const sharedHistoryRelations = `/* shared_history_relations */ LOCK TABLE accounts,sessions,user_profiles,person_social_disclosure,person_ties,connection_requests,
 account_blocks,audit_events,community_memberships,communities,activity_participations,activities,activity_organizers,cities,places,
 organizations,businesses,business_venue_relations,venues,venue_candidates,activity_invitations,organization_memberships,business_memberships IN ACCESS SHARE MODE`

// One MATERIALIZED clock and snapshot project the data and complete related
// candidate metadata together. Only IDs/xmin/expiry eligibility enter the
// transient proof; no source body, private trajectory or member address.
const sharedHistorySQL = `/* shared_relationship_history_current_v1 */
 WITH clock AS MATERIALIZED(SELECT clock_timestamp() n),
 session_current AS MATERIALIZED(SELECT s.* FROM sessions s JOIN accounts a ON a.id=s.account_id,clock
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND s.token_sha256=$3 AND s.revoked_at IS NULL
 AND s.expires_at>clock.n AND s.idle_expires_at>clock.n AND ($4::boolean OR s.authentication_method<>'dev_phone')),
 authority AS MATERIALIZED(SELECT v.id viewer,t.id target,coalesce(vd.mutual_ties AND td.mutual_ties,false) ties,
 coalesce(vd.shared_communities AND td.shared_communities,false) communities,coalesce(vd.shared_activities AND td.shared_activities,false) activities
 FROM accounts v JOIN accounts t ON t.id=$2 JOIN user_profiles p ON p.account_id=t.id AND p.visibility='public'
 LEFT JOIN person_social_disclosure vd ON vd.account_id=v.id LEFT JOIN person_social_disclosure td ON td.account_id=t.id
 WHERE v.id=$1 AND v.id<>t.id AND v.account_type='person' AND t.account_type='person' AND v.status='active' AND t.status='active'
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=$2) OR (b.blocker_account_id=$2 AND b.blocked_account_id=$1))),
 ties AS MATERIALIZED(SELECT t.* FROM person_ties t,authority a WHERE a.ties AND ($1 IN(t.person_a_account_id,t.person_b_account_id) OR $2 IN(t.person_a_account_id,t.person_b_account_id))),
 friend_ties AS MATERIALIZED(SELECT t.* FROM ties t JOIN connection_requests r ON r.id=t.request_id AND r.scope='friend' AND r.state='accepted'
 AND LEAST(r.sender_account_id,r.recipient_account_id)=t.person_a_account_id AND GREATEST(r.sender_account_id,r.recipient_account_id)=t.person_b_account_id WHERE t.status='active'),
 v_peers AS MATERIALIZED(SELECT CASE WHEN person_a_account_id=$1 THEN person_b_account_id ELSE person_a_account_id END id FROM friend_ties WHERE $1 IN(person_a_account_id,person_b_account_id)),
 t_peers AS MATERIALIZED(SELECT CASE WHEN person_a_account_id=$2 THEN person_b_account_id ELSE person_a_account_id END id FROM friend_ties WHERE $2 IN(person_a_account_id,person_b_account_id)),
 mutual_candidates AS MATERIALIZED(SELECT v.id FROM v_peers v JOIN t_peers t USING(id)),
 mutual AS MATERIALIZED(SELECT m.id FROM mutual_candidates m JOIN accounts a ON a.id=m.id AND a.account_type='person' AND a.status='active'
 JOIN user_profiles p ON p.account_id=a.id AND p.visibility='public' JOIN person_social_disclosure d ON d.account_id=a.id AND d.mutual_ties
 WHERE NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=a.id AND b.blocked_account_id IN($1,$2)) OR (b.blocked_account_id=a.id AND b.blocker_account_id IN($1,$2)))),
 comm_candidates AS MATERIALIZED(SELECT v.community_id id FROM community_memberships v JOIN community_memberships t ON t.community_id=v.community_id AND t.user_account_id=$2,authority a
 WHERE a.communities AND v.user_account_id=$1),
 shared_communities AS MATERIALIZED(SELECT c.* FROM comm_candidates x JOIN communities c ON c.id=x.id LEFT JOIN cities city ON city.id=c.city_id
 JOIN accounts owner ON owner.id=c.owner_account_id AND owner.status='active',clock
 WHERE c.visibility='public' AND c.publication_status='published' AND c.lifecycle_status='active' AND (c.expires_at IS NULL OR c.expires_at>clock.n)
 AND (c.city_id IS NULL OR (city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock.n)))
 AND EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$1 AND m.status='active')
 AND EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$2 AND m.status='active')
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=owner.id AND b.blocked_account_id IN($1,$2)) OR (b.blocked_account_id=owner.id AND b.blocker_account_id IN($1,$2)))
 ORDER BY c.name,c.id LIMIT 51),
 activity_candidates AS MATERIALIZED(SELECT v.activity_id id FROM activity_participations v JOIN activity_participations t ON t.activity_id=v.activity_id AND t.participant_account_id=$2,authority a WHERE a.activities AND v.participant_account_id=$1),
 acts AS MATERIALIZED(SELECT a.*,a.xmin::text native_epoch FROM activities a JOIN activity_candidates x ON x.id=a.id),
 organizers AS MATERIALIZED(SELECT o.* FROM activity_organizers o JOIN acts a ON a.id=o.activity_id),
 shared_activities AS MATERIALIZED(SELECT a.* FROM acts a JOIN cities city ON city.id=a.city_id JOIN accounts host ON host.id=a.host_account_id AND host.status='active'
 JOIN activity_organizers ao ON ao.activity_id=a.id LEFT JOIN organizations org ON org.id=ao.organization_id LEFT JOIN communities comm ON comm.id=ao.community_id
 LEFT JOIN businesses biz ON biz.id=ao.business_id LEFT JOIN accounts ba ON ba.id=biz.account_id,clock
 WHERE a.visibility='public' AND a.publication_status='published' AND a.cancelled_at IS NULL AND (a.expires_at IS NULL OR a.expires_at>clock.n)
 AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock.n)
 AND (ao.organization_id IS NULL OR org.status='active') AND (ao.community_id IS NULL OR (comm.lifecycle_status='active' AND (comm.expires_at IS NULL OR comm.expires_at>clock.n)))
 AND (ao.business_id IS NULL OR (biz.status='active' AND biz.claim_status='verified' AND ba.status='active' AND (a.place_id IS NULL OR EXISTS(
 SELECT 1 FROM business_venue_relations r JOIN venues v ON v.place_id=r.place_id JOIN venue_candidates vc ON vc.id=v.source_candidate_id AND vc.status='approved'
 JOIN places p ON p.id=v.place_id AND p.city_id=v.city_id JOIN cities c ON c.id=p.city_id
 WHERE r.business_id=biz.id AND r.place_id=a.place_id AND r.status='verified' AND v.expires_at>clock.n AND vc.expires_at>clock.n
 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock.n) AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>clock.n)))))
 AND EXISTS(SELECT 1 FROM activity_participations p WHERE p.activity_id=a.id AND p.participant_account_id=$1 AND p.status='going')
 AND EXISTS(SELECT 1 FROM activity_participations p WHERE p.activity_id=a.id AND p.participant_account_id=$2 AND p.status='going')
 AND birdtie_activity_visible_to(a.id,$1) AND birdtie_activity_visible_to(a.id,$2)
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=host.id AND b.blocked_account_id IN($1,$2)) OR (b.blocked_account_id=host.id AND b.blocker_account_id IN($1,$2)))
 ORDER BY a.starts_at DESC,a.id LIMIT 51),
 visible_activities AS MATERIALIZED(SELECT * FROM shared_activities ORDER BY starts_at DESC,id LIMIT 50),
 visible_communities AS MATERIALIZED(SELECT * FROM shared_communities ORDER BY name,id LIMIT 50),
 related_places AS MATERIALIZED(SELECT p.id,p.name,array_agg(a.id ORDER BY a.id) activity_ids,p.expires_at FROM visible_activities a JOIN places p ON p.id=a.place_id AND p.city_id=a.city_id,clock
 WHERE a.modality IN('in_person','hybrid') AND a.physical_place_status='confirmed' AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock.n) GROUP BY p.id,p.name,p.expires_at ORDER BY p.name,p.id LIMIT 51),
 related_accounts AS MATERIALIZED(SELECT $1::uuid id UNION SELECT $2::uuid UNION SELECT id FROM mutual_candidates UNION SELECT host_account_id FROM acts
 UNION SELECT account_id FROM businesses WHERE id IN(SELECT business_id FROM organizers) UNION SELECT owner_account_id FROM communities WHERE id IN(SELECT id FROM comm_candidates UNION SELECT community_id FROM organizers)),
 related_cities AS MATERIALIZED(SELECT city_id id FROM acts UNION SELECT city_id FROM communities WHERE id IN(SELECT id FROM comm_candidates UNION SELECT community_id FROM organizers)),
 related_place_ids AS MATERIALIZED(SELECT place_id id FROM acts UNION SELECT venue_place_id FROM acts),
 versions AS MATERIALIZED(
 SELECT 'account' k,id::text id,xmin::text e FROM accounts WHERE id IN(SELECT id FROM related_accounts)
 UNION ALL SELECT 'profile',account_id::text,xmin::text FROM user_profiles WHERE account_id IN(SELECT id FROM related_accounts)
 UNION ALL SELECT 'disclosure',account_id::text,xmin::text FROM person_social_disclosure WHERE account_id IN(SELECT id FROM related_accounts)
 UNION ALL SELECT 'tie',id::text,xmin::text FROM person_ties WHERE id IN(SELECT id FROM ties)
 UNION ALL SELECT 'request',id::text,xmin::text FROM connection_requests WHERE id IN(SELECT request_id FROM ties)
 UNION ALL SELECT 'block',blocker_account_id::text||':'||blocked_account_id::text,xmin::text FROM account_blocks WHERE (blocker_account_id IN($1,$2) AND blocked_account_id IN(SELECT id FROM related_accounts)) OR (blocked_account_id IN($1,$2) AND blocker_account_id IN(SELECT id FROM related_accounts))
 UNION ALL SELECT 'safety_audit',id::text,xmin::text FROM audit_events WHERE resource_type='account' AND action IN('block','unblock') AND purpose='personal_safety'
 AND ((actor_account_id IN($1,$2) AND resource_id IN(SELECT id::text FROM related_accounts)) OR (actor_account_id IN(SELECT id FROM related_accounts) AND resource_id IN($1::text,$2::text)))
 UNION ALL SELECT 'comm_member',id::text,xmin::text FROM community_memberships WHERE community_id IN(SELECT id FROM comm_candidates) AND user_account_id IN($1,$2)
 UNION ALL SELECT 'community',id::text,xmin::text||':'||(expires_at IS NULL OR expires_at>(SELECT n FROM clock))::text FROM communities WHERE id IN(SELECT id FROM comm_candidates UNION SELECT community_id FROM organizers)
 UNION ALL SELECT 'participation',id::text,xmin::text FROM activity_participations WHERE activity_id IN(SELECT id FROM activity_candidates) AND participant_account_id IN($1,$2)
 UNION ALL SELECT 'activity',id::text,native_epoch||':'||(expires_at IS NULL OR expires_at>(SELECT n FROM clock))::text FROM acts
 UNION ALL SELECT 'organizer',activity_id::text,xmin::text FROM activity_organizers WHERE activity_id IN(SELECT id FROM acts)
 UNION ALL SELECT 'city',id,xmin::text||':'||(expires_at IS NULL OR expires_at>(SELECT n FROM clock))::text FROM cities WHERE id IN(SELECT id FROM related_cities)
 UNION ALL SELECT 'place',id::text,xmin::text||':'||(expires_at IS NULL OR expires_at>(SELECT n FROM clock))::text FROM places WHERE id IN(SELECT id FROM related_place_ids)
 UNION ALL SELECT 'organization',id::text,xmin::text FROM organizations WHERE id IN(SELECT organization_id FROM organizers)
 UNION ALL SELECT 'business',id::text,xmin::text FROM businesses WHERE id IN(SELECT business_id FROM organizers)
 UNION ALL SELECT 'business_venue',business_id::text||':'||place_id::text,xmin::text FROM business_venue_relations WHERE business_id IN(SELECT business_id FROM organizers) AND place_id IN(SELECT id FROM related_place_ids)
 UNION ALL SELECT 'venue',place_id::text,xmin::text||':'||(expires_at>(SELECT n FROM clock))::text FROM venues WHERE place_id IN(SELECT id FROM related_place_ids)
 UNION ALL SELECT 'venue_candidate',id::text,xmin::text||':'||(expires_at>(SELECT n FROM clock))::text FROM venue_candidates WHERE id IN(SELECT source_candidate_id FROM venues WHERE place_id IN(SELECT id FROM related_place_ids))
 ),
 deadlines AS MATERIALIZED(SELECT expires_at FROM visible_activities UNION ALL SELECT expires_at FROM visible_communities UNION ALL SELECT expires_at FROM related_places
 UNION ALL SELECT expires_at FROM cities WHERE id IN(SELECT city_id FROM visible_activities UNION SELECT city_id FROM visible_communities)
 UNION ALL SELECT expires_at FROM communities WHERE id IN(SELECT community_id FROM organizers WHERE activity_id IN(SELECT id FROM visible_activities))
 UNION ALL SELECT expires_at FROM venues WHERE place_id IN(SELECT place_id FROM visible_activities WHERE id IN(SELECT activity_id FROM organizers WHERE business_id IS NOT NULL))
 UNION ALL SELECT expires_at FROM venue_candidates WHERE id IN(SELECT source_candidate_id FROM venues WHERE place_id IN(SELECT place_id FROM visible_activities WHERE id IN(SELECT activity_id FROM organizers WHERE business_id IS NOT NULL)))),
 data AS MATERIALIZED(SELECT jsonb_build_object('mutualCount',(SELECT count(*) FROM mutual),
 'communities',coalesce((SELECT jsonb_agg(jsonb_build_object('id',id,'title',name) ORDER BY name,id) FROM visible_communities),'[]'),
 'activities',coalesce((SELECT jsonb_agg(jsonb_build_object('id',id,'title',title,'startsAt',starts_at,'endsAt',ends_at,'timeZone',time_zone,'modality',modality) ORDER BY starts_at DESC,id) FROM visible_activities),'[]'),
 'places',coalesce((SELECT jsonb_agg(jsonb_build_object('id',id,'title',name,'activityIds',activity_ids) ORDER BY name,id) FROM (SELECT * FROM related_places ORDER BY name,id LIMIT 50) p),'[]'),
 'communitiesTruncated',(SELECT count(*)>50 FROM shared_communities),'activitiesTruncated',(SELECT count(*)>50 FROM shared_activities),'placesTruncated',(SELECT count(*)>50 FROM related_places)) body),
 bound AS MATERIALIZED(SELECT least(clock.n+interval '30 seconds',s.expires_at,s.idle_expires_at,(SELECT min(expires_at) FROM deadlines WHERE expires_at>clock.n)) until FROM clock LEFT JOIN session_current s ON true)
 SELECT data.body,encode(sha256(convert_to(jsonb_build_array(
 (SELECT jsonb_build_array(id,account_id,encode(token_sha256,'hex'),created_at,authentication_method,expires_at) FROM session_current),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(k,id,e) ORDER BY k,id),'[]') FROM versions),data.body)::text,'UTF8')),'hex'),
 clock.n,bound.until,EXISTS(SELECT 1 FROM session_current),EXISTS(SELECT 1 FROM authority) FROM clock,data,bound`

func (s *Store) captureSharedHistory(ctx context.Context, a sc.CurrentAccess) (sharedHistoryFrame, error) {
	var f sharedHistoryFrame
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return f, sc.ErrUnavailable
	}
	if !sc.ValidID(a.ViewerID) || !sc.ValidID(a.TargetID) || a.ViewerID == a.TargetID || a.SessionDigest == ([32]byte{}) {
		return f, sc.ErrNotFound
	}
	// Explicit RC overrides a RepeatableRead-configured pool. Relation waits
	// happen before Account/Session rows; final payload/clock follows all waits.
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return f, sc.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return f, e
	}
	if _, e = tx.Exec(ctx, sharedHistoryRelations); e != nil {
		return f, e
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR SHARE`, a.ViewerID).Scan(&id); errors.Is(e, pgx.ErrNoRows) {
		return f, identity.ErrUnauthorized
	} else if e != nil {
		return f, e
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR SHARE`, a.TargetID).Scan(&id); errors.Is(e, pgx.ErrNoRows) {
		return f, sc.ErrNotFound
	} else if e != nil {
		return f, e
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.ViewerID); e != nil {
		return f, e
	}
	var raw []byte
	var currentSession, currentTarget bool
	e = tx.QueryRow(ctx, sharedHistorySQL, a.ViewerID, a.TargetID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&raw, &f.proof, &f.observed, &f.expires, &currentSession, &currentTarget)
	if e != nil {
		return f, fmt.Errorf("shared history native capture: %w", e)
	}
	if !currentSession {
		return f, identity.ErrUnauthorized
	}
	if !currentTarget {
		return f, sc.ErrNotFound
	}
	if e = json.Unmarshal(raw, &f.data); e != nil {
		return f, sc.ErrUnavailable
	}
	f.data.Schema = sc.HistorySchema
	f.data.ViewerID = a.ViewerID
	f.data.TargetID = a.TargetID
	f.data.ObservedAt = f.observed
	f.data.ValidUntil = f.expires
	f.data.Attendance = "UNKNOWN"
	f.data.Visit = "UNKNOWN"
	if e = sc.ValidateCurrent(f.data, a); e != nil {
		return sharedHistoryFrame{}, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return sharedHistoryFrame{}, sc.ErrUnavailable
	}
	return f, nil
}

func (s *Store) SharedSocialContextCurrent(ctx context.Context, a sc.CurrentAccess) (sc.Signals, sc.CurrentValidation, error) {
	f, e := s.captureSharedHistory(ctx, a)
	if e != nil {
		return sc.Signals{}, nil, e
	}
	check := func(ctx context.Context) error {
		current, e := s.captureSharedHistory(ctx, a)
		if e != nil {
			return e
		}
		if current.proof != f.proof || current.observed.Before(f.observed) || !current.observed.Before(f.expires) {
			return sc.ErrChanged
		}
		return nil
	}
	return f.data, check, nil
}
