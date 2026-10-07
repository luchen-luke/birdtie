package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

type ownPlansFrame struct {
	proof             string
	observed, expires time.Time
}

// This is a transient read fence, not an authorization ledger. The native
// version closure includes hidden targets too, so an A -> B -> A change cannot
// restore an earlier source frame. No Agent/Profile is needed for own Plans.
const ownPlansFrameSQL = `/* own_plans_current_v1 */
 WITH cl AS MATERIALIZED(SELECT clock_timestamp() at),
 own AS MATERIALIZED(
 SELECT 'plan' kind,id,activity_id,xmin::text version FROM activity_plans WHERE owner_account_id=$1 
 ), own_part AS MATERIALIZED(
 SELECT 'participation' kind,id,activity_id,xmin::text version FROM activity_participations WHERE participant_account_id=$1 
 ), selected AS MATERIALIZED(SELECT activity_id FROM own UNION SELECT activity_id FROM own_part),
 acts AS MATERIALIZED(SELECT a.* FROM activities a JOIN selected x ON x.activity_id=a.id),
 organizers AS MATERIALIZED(SELECT ao.* FROM activity_organizers ao JOIN selected x ON x.activity_id=ao.activity_id),
 versions AS MATERIALIZED(
 SELECT kind,id::text,version FROM own UNION ALL SELECT kind,id::text,version FROM own_part
 UNION ALL SELECT 'activity',a.id::text,a.xmin::text FROM activities a JOIN selected x ON x.activity_id=a.id
 UNION ALL SELECT 'city',c.id,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM acts)
 UNION ALL SELECT 'place',p.id::text,p.xmin::text FROM places p WHERE p.id IN(SELECT place_id FROM acts)
 UNION ALL SELECT 'venue',v.place_id::text,v.xmin::text FROM venues v WHERE v.place_id IN(SELECT venue_place_id FROM acts)
 UNION ALL SELECT 'candidate',vc.id::text,vc.xmin::text FROM venue_candidates vc WHERE vc.id IN(SELECT source_candidate_id FROM venues WHERE place_id IN(SELECT venue_place_id FROM acts))
 UNION ALL SELECT 'organizer',ao.activity_id::text,ao.xmin::text FROM activity_organizers ao JOIN selected x ON x.activity_id=ao.activity_id
 UNION ALL SELECT 'organization',o.id::text,o.xmin::text FROM organizations o WHERE o.id IN(SELECT organization_id FROM organizers)
 UNION ALL SELECT 'community',c.id::text,c.xmin::text FROM communities c WHERE c.id IN(SELECT community_id FROM organizers)
 UNION ALL SELECT 'business_venue_relation',r.business_id::text||':'||r.place_id::text,r.xmin::text FROM business_venue_relations r WHERE r.business_id IN(SELECT business_id FROM organizers) AND r.place_id IN(SELECT place_id FROM acts)
 UNION ALL SELECT 'business',b.id::text,b.xmin::text FROM businesses b WHERE b.id IN(SELECT business_id FROM organizers)
 UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT host_account_id FROM acts UNION SELECT account_id FROM businesses WHERE id IN(SELECT business_id FROM organizers))
 UNION ALL SELECT 'org_member',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.user_account_id=$1 AND m.organization_id IN(SELECT organization_id FROM organizers)
 UNION ALL SELECT 'community_member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.user_account_id=$1 AND m.community_id IN(SELECT community_id FROM organizers)
 UNION ALL SELECT 'business_member',m.id::text,m.xmin::text FROM business_memberships m WHERE m.user_account_id=$1 AND m.business_id IN(SELECT business_id FROM organizers)
 UNION ALL SELECT 'invitation',i.id::text,i.xmin::text FROM activity_invitations i JOIN selected x ON x.activity_id=i.activity_id WHERE i.invitee_account_id=$1
 UNION ALL SELECT 'block',b.blocker_account_id::text||':'||b.blocked_account_id::text,b.xmin::text FROM account_blocks b
 WHERE (b.blocker_account_id=$1 AND b.blocked_account_id IN(SELECT host_account_id FROM acts)) OR (b.blocked_account_id=$1 AND b.blocker_account_id IN(SELECT host_account_id FROM acts))
 ), deadlines AS MATERIALIZED(
 SELECT a.starts_at expires_at FROM acts a WHERE a.starts_at>(SELECT at FROM cl)
 UNION ALL SELECT a.ends_at FROM acts a WHERE a.ends_at>(SELECT at FROM cl)
 UNION ALL SELECT a.expires_at FROM acts a WHERE a.publication_status='published' AND a.expires_at>(SELECT at FROM cl)
 UNION ALL SELECT c.expires_at FROM cities c WHERE c.id IN(SELECT city_id FROM acts) AND c.publication_status='published' AND c.expires_at>(SELECT at FROM cl)
 UNION ALL SELECT p.expires_at FROM places p WHERE p.id IN(SELECT place_id FROM acts) AND p.publication_status='published' AND p.expires_at>(SELECT at FROM cl)
 UNION ALL SELECT v.expires_at FROM venues v WHERE v.place_id IN(SELECT venue_place_id FROM acts) AND v.expires_at>(SELECT at FROM cl)
 UNION ALL SELECT vc.expires_at FROM venue_candidates vc WHERE vc.id IN(SELECT source_candidate_id FROM venues WHERE place_id IN(SELECT venue_place_id FROM acts)) AND vc.status='approved' AND vc.expires_at>(SELECT at FROM cl)
 )
 SELECT encode(sha256(convert_to(jsonb_build_array(owner.id,owner.xmin::text,ss.id,ss.xmin::text,ss.created_at,ss.authentication_method,ss.expires_at,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(kind,id,version) ORDER BY kind,id),'[]'::jsonb) FROM versions))::text,'UTF8')),'hex'),
 cl.at,least(cl.at+interval '30 seconds',ss.expires_at,ss.idle_expires_at,(SELECT min(expires_at) FROM deadlines))
 FROM cl JOIN accounts owner ON owner.id=$1 AND owner.account_type='person' AND owner.status='active'
 JOIN sessions ss ON ss.account_id=owner.id AND ss.token_sha256=$2 AND ss.revoked_at IS NULL
 AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($3::boolean OR ss.authentication_method<>'dev_phone')`

func (s *Store) captureOwnPlans(ctx context.Context, a activityplan.CurrentAccess) (ownPlansFrame, error) {
	var f ownPlansFrame
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return f, activityplan.ErrChanged
	}
	if _, e := actorref.ParsePrincipal("person", a.OwnerID); e != nil || a.SessionDigest == ([32]byte{}) {
		return f, identity.ErrUnauthorized
	}
	// The CTE references cl in its deadline subqueries; use a scalar clock from
	// that same MATERIALIZED statement, not a transaction-start or host clock.
	q := ownPlansFrameSQL
	e := s.pool.QueryRow(ctx, q, a.OwnerID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&f.proof, &f.observed, &f.expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return ownPlansFrame{}, identity.ErrUnauthorized
	}
	if e != nil {
		return ownPlansFrame{}, fmt.Errorf("own plans current capture: %w", e)
	}
	return f, nil
}

func (s *Store) ownPlansValidation(a activityplan.CurrentAccess, original ownPlansFrame) activityplan.CurrentValidation {
	return func(ctx context.Context) error {
		current, e := s.captureOwnPlans(ctx, a)
		if e != nil {
			return e
		}
		if current.proof != original.proof || current.observed.Before(original.observed) || !current.observed.Before(original.expires) {
			return activityplan.ErrChanged
		}
		return nil
	}
}

func (s *Store) ListActivityPlansCurrent(ctx context.Context, a activityplan.CurrentAccess) ([]activityplan.Plan, activityplan.CurrentValidation, error) {
	f, e := s.captureOwnPlans(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	data, e := s.ListActivityPlans(ctx, a.OwnerID)
	if e != nil {
		return nil, nil, e
	}
	check := s.ownPlansValidation(a, f)
	if e = check(ctx); e != nil {
		return nil, nil, e
	}
	return data, check, nil
}
func (s *Store) ListParticipationsCurrent(ctx context.Context, a activityplan.CurrentAccess) ([]activityparticipation.Overview, activityplan.CurrentValidation, error) {
	f, e := s.captureOwnPlans(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	data, e := s.ListParticipations(ctx, a.OwnerID)
	if e != nil {
		return nil, nil, e
	}
	check := s.ownPlansValidation(a, f)
	if e = check(ctx); e != nil {
		return nil, nil, e
	}
	return data, check, nil
}
