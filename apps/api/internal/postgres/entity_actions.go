package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

type entityActionWriteFence struct {
	proof            string
	deadline         time.Time
	ref              ea.Ref
	access           ea.Access
	kind             string
	createdRequestID string
	cleanupRequests  []byte
}

func (s *Store) lockEntityActionWriter(ctx context.Context, tx pgx.Tx, a ea.Access) error {
	if !a.Valid() || a.Public {
		return ea.ErrInvalid
	}
	if _, e := tx.Exec(ctx, resultProjectionRelations+`;LOCK TABLE saved_items IN ACCESS SHARE MODE`); e != nil {
		return ea.ErrUnavailable
	}
	var owner string
	if e := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR SHARE`, a.Actor.ID).Scan(&owner); e != nil {
		return identity.ErrUnauthorized
	}
	return s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID)
}

// The tail proof excludes ONLY the row that this original writer is expected
// to change. Other participants, source rows, ACL and ABA versions remain.
const entityActionFieldCommunitiesCTE = `action_field_communities AS MATERIALIZED(SELECT c.id,c.expires_at FROM communities c WHERE $5='person' AND c.id::text IN(
 SELECT scope.value#>>'{}' FROM agent_profile_field_visibility v CROSS JOIN LATERAL jsonb_array_elements(coalesce(v.rules->'displayName'->'communityIds','[]'::jsonb)) scope(value)
 WHERE v.owner_id=$1 AND v.rules->'displayName'->>'visibility'='COMMUNITY'))`

func entityActionWriteClosureExpr(ref ea.Ref, kind string) string {
	q := entityActionClosureSQL()
	q = strings.Replace(q, `(SELECT title FROM action_source) token`, `''::text token`, 1)
	if kind == ea.Join && ref.Type == "activity" {
		q = strings.Replace(q, `FROM activity_participations p WHERE p.activity_id=$1`, `FROM activity_participations p WHERE p.activity_id=$1 AND p.participant_account_id<>$2`, 1)
	}
	if kind == ea.Join && ref.Type == "community" {
		q = strings.Replace(q, `FROM community_memberships m WHERE m.user_account_id=$2 OR m.community_id=$1`, `FROM community_memberships m WHERE (m.user_account_id=$2 OR m.community_id=$1) AND NOT(m.community_id=$1 AND m.user_account_id=$2)`, 1)
	}
	if kind == ea.Message {
		q = strings.Replace(q, "FROM conversations cv JOIN connection_requests r ON r.id=cv.request_id WHERE", "FROM conversations cv JOIN connection_requests r ON r.id=cv.request_id WHERE cv.id<>$7::uuid AND", 1)
	}
	if kind == ea.Save {
		q = strings.Replace(q, `FROM saved_items s WHERE s.owner_account_id=$2 AND (s.place_id=$1 OR s.activity_id=$1 OR s.community_id=$1)`, `FROM saved_items s WHERE false`, 1)
	}
	if kind == ea.Connect {
		// Preserve the original version token only for the exact old rows
		// captured before native housekeeping. The final statement separately
		// proves their precise expected transition in THIS transaction.
		q = strings.Replace(q, "SELECT 'request',r.id::text,r.xmin::text", "SELECT 'request',r.id::text,coalesce((SELECT x.item->>'xmin' FROM action_request_cleanup x WHERE x.item->>'id'=r.id::text),r.xmin::text)", 1)
		q = strings.Replace(q, `FROM connection_requests r WHERE r.sender_account_id=$2 OR (r.sender_account_id=$2 AND r.recipient_account_id=$1) OR (r.recipient_account_id=$2 AND r.sender_account_id=$1)`, `FROM connection_requests r WHERE (r.sender_account_id=$2 OR (r.sender_account_id=$2 AND r.recipient_account_id=$1) OR (r.recipient_account_id=$2 AND r.sender_account_id=$1)) AND r.id<>$7::uuid`, 1)
	}
	return `encode(sha256(convert_to((` + q + `)::text,'UTF8')),'hex')`
}

func (s *Store) checkEntityActionWrite(ctx context.Context, tx pgx.Tx, a ea.Access, ref ea.Ref, b ea.BoundCondition) (entityActionWriteFence, error) {
	var fence entityActionWriteFence
	if !b.Valid(ref) || a.Public || !a.Valid() {
		return fence, ea.ErrInvalid
	}
	var r ea.Receipt
	var live, visible bool
	var raw []byte
	tail := entityActionWriteClosureExpr(ref, b.Kind)
	cleanup := `action_request_cleanup AS MATERIALIZED(SELECT jsonb_build_object('id',r.id::text,'xmin',r.xmin::text,'body',to_jsonb(r)-'state'-'decided_at') item FROM connection_requests r WHERE r.state='pending' AND r.expires_at<=transaction_timestamp() AND (r.sender_account_id=$2 OR r.recipient_account_id=$2))`
	if b.Kind != ea.Connect {
		cleanup = `action_request_cleanup AS MATERIALIZED(SELECT '{}'::jsonb item WHERE false)`
	}
	query := `WITH binding_args AS(SELECT $7::uuid ignored),` + cleanup + `,` + strings.TrimPrefix(strings.TrimSuffix(entityActionSQL(ref.Type), ` FROM action_clock`), `WITH `) + `,` + tail + `,(SELECT coalesce(jsonb_agg(item ORDER BY item->>'id'),'[]'::jsonb) FROM action_request_cleanup) FROM action_clock`
	e := tx.QueryRow(ctx, query, ref.ID, a.Actor.ID, a.SessionDigest[:], s.devPhoneEnabled, ref.Type, false, "00000000-0000-0000-0000-000000000000").Scan(&r.View.ObservedAt, &r.View.ValidUntil, &live, &visible, &r.View.Title, &raw, &r.Proof, &fence.proof, &fence.cleanupRequests)
	if e != nil {
		return fence, ea.ErrUnavailable
	}
	if !live {
		return fence, identity.ErrUnauthorized
	}
	if !visible {
		return fence, ea.ErrNotFound
	}
	if b.SourceVersion != r.Proof || !b.ValidUntil.After(r.View.ObservedAt) || b.ValidUntil.After(r.View.ObservedAt.Add(30*time.Second)) {
		return fence, ea.ErrChanged
	}
	var facts ea.Facts
	if json.Unmarshal(raw, &facts) != nil {
		return fence, ea.ErrUnavailable
	}
	v := ea.Project(ref, facts, r.View.ObservedAt, r.View.ValidUntil, r.View.Title, r.Proof)
	allowed := false
	for _, d := range v.Actions {
		if d.Kind == b.Kind && d.State == ea.Available {
			for _, op := range d.AllowedOperations {
				allowed = allowed || op == b.Operation
			}
		}
	}
	if !allowed {
		return fence, ea.ErrChanged
	}
	fence = entityActionWriteFence{proof: fence.proof, deadline: b.ValidUntil, ref: ref, access: a, kind: b.Kind, createdRequestID: "00000000-0000-0000-0000-000000000000", cleanupRequests: fence.cleanupRequests}
	if r.View.ValidUntil.Before(fence.deadline) {
		fence.deadline = r.View.ValidUntil
	}
	return fence, nil
}
func (s *Store) finishEntityActionWrite(ctx context.Context, tx pgx.Tx, f entityActionWriteFence) error {
	var now time.Time
	var live, cleanupExact bool
	var proof string
	// Both current session and all immutable source versions are evaluated in
	// the same final statement AFTER the original writes and FK/trigger waits.
	closure := entityActionWriteClosureExpr(f.ref, f.kind)
	// This is an exact expected housekeeping transition, not permission to
	// ignore arbitrary requests. New/unexpected expired writes also reject.
	cleanup := `action_request_cleanup AS MATERIALIZED(SELECT value item FROM jsonb_array_elements($8::jsonb))`
	exact := `NOT EXISTS(SELECT 1 FROM action_request_cleanup x LEFT JOIN connection_requests r ON r.id=(x.item->>'id')::uuid WHERE r.id IS NULL OR r.state<>'expired' OR r.decided_at IS DISTINCT FROM transaction_timestamp() OR (to_jsonb(r)-'state'-'decided_at') IS DISTINCT FROM x.item->'body' OR r.xmin::text<>mod(pg_current_xact_id()::text::numeric,4294967296)::text) AND (SELECT count(*) FROM connection_requests r WHERE (r.sender_account_id=$2 OR r.recipient_account_id=$2) AND r.state='expired' AND r.decided_at=transaction_timestamp() AND r.xmin::text=mod(pg_current_xact_id()::text::numeric,4294967296)::text)=(SELECT count(*) FROM action_request_cleanup)`
	if f.kind != ea.Connect {
		exact = `true`
	}
	q := `WITH args AS(SELECT $6::boolean publicflag,$7::uuid ignored),` + cleanup + `,` + entityActionFieldCommunitiesCTE + `, stamp AS MATERIALIZED(SELECT statement_timestamp() n) SELECT n,EXISTS(SELECT 1 FROM sessions ss JOIN accounts aa ON aa.id=ss.account_id WHERE ss.token_sha256=$3 AND ss.account_id=$2 AND aa.status='active' AND aa.account_type='person' AND ss.revoked_at IS NULL AND ss.expires_at>n AND ss.idle_expires_at>n AND ($4::boolean OR ss.authentication_method<>'dev_phone')),` + closure + `,(` + exact + `) FROM stamp`
	if e := tx.QueryRow(ctx, q, f.ref.ID, f.access.Actor.ID, f.access.SessionDigest[:], s.devPhoneEnabled, f.ref.Type, false, f.createdRequestID, f.cleanupRequests).Scan(&now, &live, &proof, &cleanupExact); e != nil {
		return ea.ErrUnavailable
	}
	if !live {
		return identity.ErrUnauthorized
	}
	if !f.deadline.After(now) || proof != f.proof || !cleanupExact {
		return ea.ErrChanged
	}
	return nil
}

var _ ea.Store = (*Store)(nil)

// The same source/relationship rows are read in ONE statement after relation,
// actor and session waits. Nothing from a Console/private Profile is projected.
func entityActionSourceSQL(kind string) string {
	switch kind {
	case "activity":
		return `SELECT a.title` + publishedActivityFrom + ` AND a.id=$1 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())`
	case "community":
		return `SELECT c.name AS title FROM communities c JOIN accounts creator ON creator.id=c.owner_account_id
 WHERE c.id=$1 AND c.lifecycle_status='active' AND c.publication_status='published' AND creator.status='active'
 AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND (c.city_id IS NULL OR EXISTS(SELECT 1 FROM cities city WHERE city.id=c.city_id AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp())))
 AND (c.visibility<>'hidden' OR EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$2 AND m.status IN('active','invited')))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$2 AND b.blocked_account_id=creator.id) OR (b.blocked_account_id=$2 AND b.blocker_account_id=creator.id))`
	default:
		return chatEntityQuery(kind)
	}
}

// Only opaque native versions enter the internal proof. These rows are not
// returned by the action API; in particular no recipients, private invitations,
// descriptions, coordinates or source URLs are exposed here.
func entityActionClosureSQL() string {
	return `SELECT coalesce(jsonb_agg(jsonb_build_array(kind,id,token) ORDER BY kind,id),'[]'::jsonb) FROM (
 SELECT 'source' kind,$5::text id,(SELECT title FROM action_source) token
 UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN($1,$2) OR a.id IN(SELECT host_account_id FROM activities WHERE id=$1) OR a.id IN(SELECT owner_account_id FROM communities WHERE id=$1) OR a.id IN(SELECT account_id FROM organizations WHERE id=$1) OR a.id IN(SELECT account_id FROM businesses WHERE id=$1)
 UNION ALL SELECT 'activity',a.id::text,a.xmin::text FROM activities a WHERE a.id=$1
 UNION ALL SELECT 'organizer',a.activity_id::text,a.xmin::text FROM activity_organizers a WHERE a.activity_id=$1
 UNION ALL SELECT 'place',p.id::text,p.xmin::text FROM places p WHERE p.id=$1 OR p.id IN(SELECT place_id FROM activities WHERE id=$1)
 UNION ALL SELECT 'city',c.id::text,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM activities WHERE id=$1 UNION SELECT city_id FROM places WHERE id=$1 UNION SELECT city_id FROM communities WHERE id=$1)
 UNION ALL SELECT 'profile',p.account_id::text,p.xmin::text FROM user_profiles p WHERE p.account_id=$1
 UNION ALL SELECT 'profileacl',v.agent_id::text,v.xmin::text FROM agent_profile_field_visibility v WHERE v.owner_id=$1
 UNION ALL SELECT 'agent',a.id::text,a.xmin::text FROM agents a WHERE a.principal_account_id=$1
 UNION ALL SELECT 'agent-profile',p.agent_id::text,p.xmin::text FROM agent_profiles p WHERE p.owner_id=$1
 UNION ALL SELECT 'org-member',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.organization_id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=$1)
 UNION ALL SELECT 'host-account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT person_account_id FROM activity_organizers WHERE activity_id=$1 UNION SELECT account_id FROM organizations WHERE id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=$1) UNION SELECT account_id FROM businesses WHERE id IN(SELECT business_id FROM activity_organizers WHERE activity_id=$1) UNION SELECT owner_account_id FROM communities WHERE id IN(SELECT community_id FROM activity_organizers WHERE activity_id=$1))
 UNION ALL SELECT 'host-community-member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id IN(SELECT community_id FROM activity_organizers WHERE activity_id=$1)
 UNION ALL SELECT 'org',o.id::text,o.xmin::text FROM organizations o WHERE o.id=$1 OR o.id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=$1)
 UNION ALL SELECT 'business',b.id::text,b.xmin::text FROM businesses b WHERE b.id=$1 OR b.id IN(SELECT business_id FROM activity_organizers WHERE activity_id=$1)
 UNION ALL SELECT 'community',c.id::text,c.xmin::text FROM communities c WHERE c.id=$1 OR c.id IN(SELECT community_id FROM activity_organizers WHERE activity_id=$1)
 UNION ALL SELECT 'blocks',b.blocker_account_id::text||':'||b.blocked_account_id::text,b.xmin::text FROM account_blocks b WHERE b.blocker_account_id=$2 OR b.blocked_account_id=$2
 UNION ALL SELECT 'contact-intent',i.id::text,i.xmin::text FROM intents i WHERE $5='person' AND i.owner_account_id=$1
 UNION ALL SELECT 'contact-city',c.id::text,c.xmin::text FROM cities c WHERE $5='person' AND c.id IN(SELECT city_id FROM intents WHERE owner_account_id=$1)
 UNION ALL SELECT 'contact-conversation',cv.id::text,concat_ws(':',cv.xmin::text,r.xmin::text) FROM conversations cv JOIN connection_requests r ON r.id=cv.request_id WHERE $5='person' AND LEAST(cv.member_a_account_id,cv.member_b_account_id)=LEAST($1::uuid,$2::uuid) AND GREATEST(cv.member_a_account_id,cv.member_b_account_id)=GREATEST($1::uuid,$2::uuid)
 UNION ALL SELECT 'tie',t.id::text,t.xmin::text FROM person_ties t WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid)
 UNION ALL SELECT 'request',r.id::text,r.xmin::text FROM connection_requests r WHERE r.sender_account_id=$2 OR (r.sender_account_id=$2 AND r.recipient_account_id=$1) OR (r.recipient_account_id=$2 AND r.sender_account_id=$1)
 UNION ALL SELECT 'participation',p.id::text,p.xmin::text FROM activity_participations p WHERE p.activity_id=$1
 UNION ALL SELECT 'member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.user_account_id=$2 OR m.community_id=$1
 UNION ALL SELECT 'field-community',c.id::text,c.xmin::text FROM communities c WHERE c.id IN (SELECT id FROM action_field_communities)
 UNION ALL SELECT 'field-community-member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.community_id IN(SELECT id FROM action_field_communities) AND m.user_account_id IN($1,$2)
 UNION ALL SELECT 'invitation',i.id::text,i.xmin::text FROM activity_invitations i WHERE i.activity_id=$1
 UNION ALL SELECT 'saved',s.id::text,s.xmin::text FROM saved_items s WHERE s.owner_account_id=$2 AND (s.place_id=$1 OR s.activity_id=$1 OR s.community_id=$1)
 UNION ALL SELECT 'venue',v.place_id::text,v.xmin::text FROM venues v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1)
 UNION ALL SELECT 'venuecandidate',v.id::text,v.xmin::text FROM venue_candidates v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1)
 UNION ALL SELECT 'businessvenue',v.business_id::text||':'||v.place_id::text,v.xmin::text FROM business_venue_relations v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1)
 UNION ALL SELECT 'venue-operator',o.id::text,concat_ws(':',o.xmin::text,a.xmin::text) FROM organizations o JOIN accounts a ON a.id=o.account_id WHERE o.id IN(SELECT operator_organization_id FROM venues WHERE place_id IN(SELECT place_id FROM activities WHERE id=$1))
 ) tokens`
}

// This server-only facts expression is reused by both direct detail and typed
// Agent result producers. It never accepts a client action/state/permission.
func entityActionFactsSQL() string {
	return `jsonb_build_object(
 'CanConnect',(SELECT count(*) FROM connection_requests quota WHERE quota.sender_account_id=$2 AND quota.state='pending' AND quota.expires_at>(SELECT n FROM action_clock))<10 AND $5='person' AND $1::uuid<>$2::uuid AND NOT EXISTS(SELECT 1 FROM person_ties t WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid) AND t.status='active') AND NOT EXISTS(SELECT 1 FROM connection_requests r WHERE ((r.sender_account_id=$2 AND r.recipient_account_id=$1) OR (r.sender_account_id=$1 AND r.recipient_account_id=$2)) AND r.state='pending' AND r.expires_at>(SELECT n FROM action_clock)),
 'CanRequestConversation',$5='person' AND $1::uuid<>$2::uuid AND (SELECT count(*) FROM connection_requests quota WHERE quota.sender_account_id=$2 AND quota.state='pending' AND quota.expires_at>(SELECT n FROM action_clock))<10 AND EXISTS(SELECT 1 FROM intents i JOIN cities c ON c.id=i.city_id JOIN user_profiles p ON p.account_id=i.owner_account_id WHERE i.owner_account_id=$1 AND p.visibility='public' AND i.audience='public' AND i.state='active' AND i.owner_confirmed_at IS NOT NULL AND i.expires_at>(SELECT n FROM action_clock) AND i.available_from<=(SELECT n FROM action_clock) AND i.available_until>(SELECT n FROM action_clock) AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>(SELECT n FROM action_clock))) AND NOT EXISTS(SELECT 1 FROM conversations cv JOIN connection_requests cr ON cr.id=cv.request_id WHERE cr.state='accepted' AND LEAST(cv.member_a_account_id,cv.member_b_account_id)=LEAST($1::uuid,$2::uuid) AND GREATEST(cv.member_a_account_id,cv.member_b_account_id)=GREATEST($1::uuid,$2::uuid)) AND NOT EXISTS(SELECT 1 FROM connection_requests r WHERE LEAST(r.sender_account_id,r.recipient_account_id)=LEAST($1::uuid,$2::uuid) AND GREATEST(r.sender_account_id,r.recipient_account_id)=GREATEST($1::uuid,$2::uuid) AND r.state='pending' AND r.expires_at>(SELECT n FROM action_clock)),
 'CanMessage',$5='person' AND $1::uuid<>$2::uuid AND EXISTS(SELECT 1 FROM person_ties t WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid) AND t.status='active' AND EXISTS(SELECT 1 FROM connection_requests r WHERE r.id=t.request_id AND r.scope='friend' AND r.state='accepted' AND LEAST(r.sender_account_id,r.recipient_account_id)=t.person_a_account_id AND GREATEST(r.sender_account_id,r.recipient_account_id)=t.person_b_account_id)),
 'Connected',EXISTS(SELECT 1 FROM person_ties t WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid) AND t.status='active'),
 'Pending',EXISTS(SELECT 1 FROM connection_requests r WHERE ((r.sender_account_id=$2 AND r.recipient_account_id=$1) OR (r.sender_account_id=$1 AND r.recipient_account_id=$2)) AND r.state='pending' AND r.expires_at>(SELECT n FROM action_clock)),
 'CanShare',EXISTS(SELECT 1 FROM share_source),
 'CanExport',$5='place' OR ($5='activity' AND EXISTS(SELECT 1 FROM activities a WHERE a.id=$1 AND a.visibility='public')),
 'CanJoin',($5='activity' AND EXISTS(SELECT 1 FROM activity_participations own WHERE own.activity_id=$1 AND own.participant_account_id=$2 AND own.status IN('going','pending'))) OR ($5='community' AND EXISTS(SELECT 1 FROM community_memberships own WHERE own.community_id=$1 AND own.user_account_id=$2 AND own.status IN('active','pending') AND own.role<>'owner')) OR ($5='activity' AND EXISTS(SELECT 1 FROM activities a WHERE a.id=$1 AND a.cancelled_at IS NULL AND a.ends_at>(SELECT n FROM action_clock) AND (a.expires_at IS NULL OR a.expires_at>(SELECT n FROM action_clock)) AND (a.capacity IS NULL OR a.capacity>(SELECT count(*) FROM activity_participations p WHERE p.activity_id=a.id AND p.status IN('going','pending'))))) OR ($5='community' AND EXISTS(SELECT 1 FROM communities c WHERE c.id=$1 AND (c.join_policy<>'invite_only' OR EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=$1 AND m.user_account_id=$2 AND m.status='invited')) AND NOT EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=$1 AND m.user_account_id=$2 AND m.status IN('active','pending')))),
 'Joined',($5='activity' AND EXISTS(SELECT 1 FROM activity_participations p WHERE p.activity_id=$1 AND p.participant_account_id=$2 AND p.status='going')) OR ($5='community' AND EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=$1 AND m.user_account_id=$2 AND m.status='active')),
 'JoinPending',($5='activity' AND EXISTS(SELECT 1 FROM activity_participations p WHERE p.activity_id=$1 AND p.participant_account_id=$2 AND p.status='pending')) OR ($5='community' AND EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=$1 AND m.user_account_id=$2 AND m.status='pending')),
 'Invited',EXISTS(SELECT 1 FROM community_memberships own WHERE own.community_id=$1 AND own.user_account_id=$2 AND own.status='invited'),
 'RequestJoin',$5='community' AND EXISTS(SELECT 1 FROM communities c WHERE c.id=$1 AND c.join_policy='request' AND NOT EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=$1 AND m.user_account_id=$2 AND m.status='invited')),
 'CanSave',($5='place') OR ($5='activity' AND EXISTS(SELECT 1 FROM activities a WHERE a.id=$1 AND a.visibility='public')) OR ($5='community' AND EXISTS(SELECT 1 FROM communities c WHERE c.id=$1 AND c.visibility='public' AND c.owner_confirmed_at IS NOT NULL AND c.city_id IS NOT NULL)),
 'Saved',EXISTS(SELECT 1 FROM saved_items s WHERE s.owner_account_id=$2 AND (($5='place' AND s.place_id=$1) OR ($5='activity' AND s.activity_id=$1) OR ($5='community' AND s.community_id=$1))),
 'CanNavigate',($5='place' AND EXISTS(SELECT 1 FROM places p WHERE p.id=$1 AND p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL)) OR ($5='activity' AND EXISTS(SELECT 1 FROM activities a JOIN places p ON p.id=a.place_id JOIN activity_organizers ao ON ao.activity_id=a.id WHERE a.id=$1 AND a.modality<>'online' AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>(SELECT n FROM action_clock)) AND p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL
 AND (ao.business_id IS NULL OR EXISTS(SELECT 1 FROM business_venue_relations relation JOIN venues venue ON venue.place_id=relation.place_id JOIN venue_candidates candidate ON candidate.id=venue.source_candidate_id AND candidate.status='approved' AND candidate.place_id=venue.place_id AND candidate.city_id=venue.city_id AND candidate.reviewed_by=venue.reviewed_by
 LEFT JOIN organizations operator ON operator.id=venue.operator_organization_id LEFT JOIN accounts operator_account ON operator_account.id=operator.account_id
 WHERE relation.business_id=ao.business_id AND relation.place_id=a.place_id AND relation.status='verified' AND relation.reviewed_at IS NOT NULL AND relation.reviewed_at<=(SELECT n FROM action_clock) AND isfinite(venue.expires_at) AND venue.expires_at>(SELECT n FROM action_clock) AND (venue.operator_organization_id IS NULL OR (operator.status='active' AND operator_account.status='active')))))))`
}

// Every typed producer resolves share visibility through the original public
// sharing contract. A readable private Community is not a shareable one.
func entityActionTypedShareSQL() string {
	var choices []string
	for _, kind := range []string{"person", "activity", "place", "community", "organization", "business"} {
		source := strings.NewReplacer("$1", "action_ref.id::uuid", "$2", "$2::uuid", "clock_timestamp()", "(SELECT n FROM result_clock)").Replace(chatEntityQuery(kind))
		choices = append(choices, "WHEN action_ref.kind='"+kind+"' THEN EXISTS("+source+")")
	}
	return "(CASE " + strings.Join(choices, " ") + " ELSE false END)"
}

func entityActionSQL(kind string) string {
	return entityActionSQLMode(kind, false)
}
func entityActionSQLMode(kind string, public bool) string {
	source := entityActionSourceSQL(kind)
	share := chatEntityQuery(kind)
	closure := entityActionClosureSQL()
	facts := entityActionFactsSQL()
	if public {
		// Anonymous projection never uses recipient relationships, owned saves,
		// private profile rules or session/grant rows as its proposal sources.
		closure = `SELECT coalesce(jsonb_agg(jsonb_build_array(kind,id,token) ORDER BY kind,id),'[]'::jsonb) FROM (
SELECT 'source' kind,$1::text id,(SELECT title FROM action_source) token
UNION ALL SELECT 'activity',a.id::text,a.xmin::text FROM activities a WHERE a.id=$1
UNION ALL SELECT 'place',p.id::text,p.xmin::text FROM places p WHERE p.id=$1 OR p.id IN(SELECT place_id FROM activities WHERE id=$1)
UNION ALL SELECT 'city',c.id::text,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM activities WHERE id=$1 UNION SELECT city_id FROM places WHERE id=$1)
UNION ALL SELECT 'organizer',o.activity_id::text,o.xmin::text FROM activity_organizers o WHERE o.activity_id=$1
UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT host_account_id FROM activities WHERE id=$1 UNION SELECT person_account_id FROM activity_organizers WHERE activity_id=$1 UNION SELECT account_id FROM organizations WHERE id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=$1) UNION SELECT account_id FROM businesses WHERE id IN(SELECT business_id FROM activity_organizers WHERE activity_id=$1) UNION SELECT owner_account_id FROM communities WHERE id IN(SELECT community_id FROM activity_organizers WHERE activity_id=$1))
UNION ALL SELECT 'org',o.id::text,o.xmin::text FROM organizations o WHERE o.id IN(SELECT organization_id FROM activity_organizers WHERE activity_id=$1)
UNION ALL SELECT 'business',b.id::text,b.xmin::text FROM businesses b WHERE b.id IN(SELECT business_id FROM activity_organizers WHERE activity_id=$1)
UNION ALL SELECT 'community',c.id::text,c.xmin::text FROM communities c WHERE c.id IN(SELECT community_id FROM activity_organizers WHERE activity_id=$1)
UNION ALL SELECT 'venue',v.place_id::text,v.xmin::text FROM venues v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1)
UNION ALL SELECT 'venuecandidate',v.id::text,v.xmin::text FROM venue_candidates v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1)
UNION ALL SELECT 'businessvenue',v.business_id::text||':'||v.place_id::text,v.xmin::text FROM business_venue_relations v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1)
UNION ALL SELECT 'venue-operator',o.id::text,concat_ws(':',o.xmin::text,a.xmin::text) FROM organizations o JOIN accounts a ON a.id=o.account_id WHERE o.id IN(SELECT operator_organization_id FROM venues WHERE place_id IN(SELECT place_id FROM activities WHERE id=$1))
) public_tokens`
		marker := " 'CanNavigate',"
		nav := strings.TrimSuffix(facts[strings.LastIndex(facts, marker)+len(marker):], ")")
		facts = "jsonb_build_object('CanNavigate'," + nav + ",'CanExport',true)"
		share = "SELECT NULL::text title WHERE false"
	}
	q := `WITH action_clock AS MATERIALIZED(SELECT clock_timestamp() n),
 action_field_communities AS MATERIALIZED(SELECT c.id,c.expires_at FROM communities c WHERE $5='person' AND c.id::text IN(
 SELECT scope.value#>>'{}' FROM agent_profile_field_visibility v CROSS JOIN LATERAL jsonb_array_elements(coalesce(v.rules->'displayName'->'communityIds','[]'::jsonb)) scope(value)
 WHERE v.owner_id=$1 AND v.rules->'displayName'->>'visibility'='COMMUNITY')),
 action_source AS MATERIALIZED (SELECT * FROM (` + source + `) original_source WHERE NOT $6::boolean OR ($5='place' OR ($5='activity' AND EXISTS(SELECT 1 FROM activities public_activity WHERE public_activity.id=$1 AND public_activity.visibility='public')))),share_source AS MATERIALIZED (` + share + `),
 current_session AS MATERIALIZED(SELECT se.id,se.created_at,se.expires_at,se.idle_expires_at,se.authentication_method,a.xmin::text account_epoch
 FROM sessions se JOIN accounts a ON a.id=se.account_id CROSS JOIN action_clock WHERE se.token_sha256=$3 AND se.account_id=$2 AND a.account_type='person' AND a.status='active' AND se.revoked_at IS NULL AND se.expires_at>n AND se.idle_expires_at>n AND ($4::boolean OR se.authentication_method<>'dev_phone'))
 SELECT n,least(n+interval '30 seconds',(SELECT least(expires_at,idle_expires_at) FROM current_session),
 (SELECT a.expires_at FROM activities a WHERE a.id=$1),(SELECT a.ends_at FROM activities a WHERE a.id=$1 AND a.ends_at>n),
 (SELECT p.expires_at FROM places p WHERE p.id=$1 OR p.id IN(SELECT place_id FROM activities WHERE id=$1) ORDER BY p.expires_at LIMIT 1),
 (SELECT c.expires_at FROM communities c WHERE c.id=$1),
 (SELECT min(c.expires_at) FROM action_field_communities c WHERE c.expires_at>n),
 (SELECT min(city.expires_at) FROM cities city WHERE city.id IN(SELECT city_id FROM activities WHERE id=$1 UNION SELECT city_id FROM places WHERE id=$1 UNION SELECT city_id FROM communities WHERE id=$1)),
 (SELECT min(least(i.expires_at,i.available_until,c.expires_at)) FROM intents i JOIN cities c ON c.id=i.city_id WHERE $5='person' AND i.owner_account_id=$1 AND i.audience='public' AND i.state='active' AND i.owner_confirmed_at IS NOT NULL AND i.expires_at>n AND i.available_from<=n AND i.available_until>n AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>n)),
 (SELECT min(r.expires_at) FROM connection_requests r WHERE r.sender_account_id=$2 AND r.state='pending' AND r.expires_at>n),
 (SELECT min(v.expires_at) FROM venues v WHERE v.place_id IN(SELECT place_id FROM activities WHERE id=$1) AND v.expires_at>n)),
 ($6::boolean OR EXISTS(SELECT 1 FROM current_session)),EXISTS(SELECT 1 FROM action_source),coalesce((SELECT title FROM action_source),''),` + entityActionFactsSQL() + `,
 encode(sha256(convert_to(jsonb_build_array((` + closure + `),(SELECT jsonb_build_array(id,created_at,expires_at,authentication_method,account_epoch) FROM current_session))::text,'UTF8')),'hex')
 FROM action_clock`
	q = strings.Replace(q, entityActionFactsSQL(), facts, 1)
	return strings.NewReplacer("clock_timestamp()", "(SELECT n FROM action_clock)").Replace(strings.Replace(q, "SELECT clock_timestamp() n", "SELECT statement_timestamp() n", 1))
}

func (s *Store) captureEntityActions(ctx context.Context, a ea.Access, ref ea.Ref) (ea.Receipt, error) {
	var r ea.Receipt
	if !a.Valid() || !ref.Valid() {
		return r, ea.ErrInvalid
	}
	if a.Public && ref.Type != "place" && ref.Type != "activity" {
		return r, ea.ErrInvalid
	}
	if s == nil || s.pool == nil || ctx == nil || ctx.Err() != nil {
		return r, ea.ErrUnavailable
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return r, ea.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, resultProjectionRelations+`;LOCK TABLE saved_items IN ACCESS SHARE MODE`); e != nil {
		return r, ea.ErrUnavailable
	}
	var actor any
	if !a.Public {
		actor = a.Actor.ID
		var owner string
		if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR SHARE`, a.Actor.ID).Scan(&owner); e != nil {
			return r, identity.ErrUnauthorized
		}
		if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
			return r, e
		}
	}
	var live, visible bool
	var raw []byte
	e = tx.QueryRow(ctx, entityActionSQLMode(ref.Type, a.Public), ref.ID, actor, a.SessionDigest[:], s.devPhoneEnabled, ref.Type, a.Public).Scan(&r.View.ObservedAt, &r.View.ValidUntil, &live, &visible, &r.View.Title, &raw, &r.Proof)
	if e != nil {
		return ea.Receipt{}, ea.ErrUnavailable
	}
	if !live {
		return ea.Receipt{}, identity.ErrUnauthorized
	}
	if !visible {
		return ea.Receipt{}, ea.ErrNotFound
	}
	var f ea.Facts
	if json.Unmarshal(raw, &f) != nil {
		return ea.Receipt{}, ea.ErrUnavailable
	}
	if a.Public {
		f = ea.Facts{CanNavigate: f.CanNavigate, CanExport: f.CanExport}
	}
	r.View = ea.Project(ref, f, r.View.ObservedAt, r.View.ValidUntil, r.View.Title, r.Proof)
	if !r.View.Valid() {
		return ea.Receipt{}, ea.ErrChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return ea.Receipt{}, ea.ErrUnavailable
	}
	return r, nil
}
func entityActionSeal(a ea.Access, ref ea.Ref, r ea.Receipt) (string, error) {
	resultProjectionKey.Do(func() { _, resultProjectionKey.err = rand.Read(resultProjectionKey.key[:]) })
	if resultProjectionKey.err != nil {
		return "", ea.ErrUnavailable
	}
	b, e := json.Marshal(struct {
		Access ea.Access
		Ref    ea.Ref
		View   ea.View
		Proof  string
	}{a, ref, r.View, r.Proof})
	if e != nil {
		return "", ea.ErrUnavailable
	}
	h := hmac.New(sha256.New, resultProjectionKey.key[:])
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Store) ReadEntityActions(ctx context.Context, a ea.Access, ref ea.Ref) (ea.Receipt, error) {
	r, e := s.captureEntityActions(ctx, a, ref)
	if e != nil {
		return r, e
	}
	r.Seal, e = entityActionSeal(a, ref, r)
	return r, e
}
func (s *Store) RevalidateEntityActions(ctx context.Context, a ea.Access, ref ea.Ref, r ea.Receipt) error {
	if !a.Valid() || !ref.Valid() || !r.View.Valid() || r.View.Entity != ref {
		return ea.ErrInvalid
	}
	seal, e := entityActionSeal(a, ref, r)
	if e != nil {
		return e
	}
	if !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return ea.ErrChanged
	}
	n, e := s.captureEntityActions(ctx, a, ref)
	if e != nil {
		return e
	}
	if !r.View.ValidUntil.After(n.View.ObservedAt) || r.Proof != n.Proof || !reflect.DeepEqual(r.View.Actions, n.View.Actions) {
		return ea.ErrChanged
	}
	return nil
}
