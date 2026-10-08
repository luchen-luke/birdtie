package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/jackc/pgx/v5"
)

var _ arp.NativeStore = (*Store)(nil)
var resultProjectionKey struct {
	sync.Once
	key [32]byte
	err error
}

func resultProjectionSeal(a arp.Access, q arp.Query, r arp.Receipt) (string, error) {
	resultProjectionKey.Do(func() { _, resultProjectionKey.err = rand.Read(resultProjectionKey.key[:]) })
	if resultProjectionKey.err != nil {
		return "", arp.ErrUnavailable
	}
	raw, e := json.Marshal(struct {
		Access             arp.Access
		Query              arp.Query
		Items              []arp.Item
		CommercialRefs     []arp.Ref
		Activities         []foundation.Activity
		Places             []foundation.Place
		Observed, Deadline any
		Proof              string
	}{a, q, r.Items, r.PublicCommercialRefs, r.Activities, r.Places, r.ObservedAt, r.ValidUntil, r.Proof})
	if e != nil {
		return "", arp.ErrUnavailable
	}
	h := hmac.New(sha256.New, resultProjectionKey.key[:])
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// All relation acquisition and Account/Task/Session row waits precede the ONE
// payload statement. No later ValidateTask or other pool read follows it.
const resultProjectionRelations = humanSocialRelations + `;LOCK TABLE agent_tasks,organization_map_locations,
 business_claim_controls,business_console_profiles,business_console_venue_facts,business_review_grants,business_public_profile_permissions,consent_grants,sponsored_opportunity_declarations,sponsored_opportunity_review_grants,city_editor_memberships,activity_sources,activity_participations,saved_items IN ACCESS SHARE MODE`

func resultProjectionSQL() string {
	// Reuse the original human opportunity resolver, never the older multi-pool
	// LoadOpportunityInputs or caller-provided candidates. Its clock is shared
	// with the final Task/Session/Agent and all public sources.
	human := strings.NewReplacer("$6", "''", "$5", "'opportunities'", "$4", "$4", "$3", "$3", "$2", "$2", "$1", "$2").Replace(humanSocialSQL)
	human = strings.ReplaceAll(human, "clock_timestamp()", "(SELECT n FROM result_clock)")
	human = strings.ReplaceAll(human, "now()", "(SELECT n FROM result_clock)")
	// Rename the inner Business alias BEFORE correlating the outer b.id. Without
	// this, replacing $1 by b.id would accidentally make the source predicate a
	// tautology against the inner Business and mix independent profiles.
	business := strings.ReplaceAll(businessPublicationSourceSQL, "b.", "publication_business.")
	business = strings.ReplaceAll(business, "to_jsonb(b)", "to_jsonb(publication_business)")
	business = strings.ReplaceAll(business, "JOIN businesses b ON", "JOIN businesses publication_business ON")
	business = strings.NewReplacer("$1", "b.id", "($2::uuid IS NULL OR m.user_account_id=$2)", "true").Replace(business)
	business = strings.ReplaceAll(business, "clock_timestamp()", "(SELECT n FROM result_clock)")
	// Only the final current source tokens are hashed, not exposed. The person
	// label is the same field ACL used by its native public detail/chat card.
	actionFacts := strings.NewReplacer("$1", "action_ref.id::uuid", "$5", "action_ref.kind", "action_clock", "result_clock", "EXISTS(SELECT 1 FROM share_source)", entityActionTypedShareSQL()).Replace(entityActionFactsSQL())
	return `WITH result_clock AS MATERIALIZED(SELECT clock_timestamp() n),
 city AS MATERIALIZED(SELECT c.*,c.xmin::text token FROM cities c,result_clock WHERE c.id=$1 AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>n)),
 query AS MATERIALIZED(SELECT $5::jsonb q),
 native_authority AS MATERIALIZED(SELECT jsonb_build_array(a.id,a.xmin::text,se.id,se.created_at,se.expires_at,se.authentication_method,ag.id,ag.xmin::text,ap.xmin::text,t.xmin::text) token,
 least(se.expires_at,se.idle_expires_at) deadline
 FROM accounts a JOIN sessions se ON se.account_id=a.id JOIN agent_tasks t ON t.id=$6 AND t.owner_account_id=a.id AND t.principal_type='person' AND t.acting_user_account_id=a.id
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_type='PERSON' AND ap.owner_id=a.id CROSS JOIN result_clock
 WHERE a.id=$2 AND a.account_type='person' AND a.status='active' AND se.token_sha256=$3 AND se.revoked_at IS NULL
 AND se.expires_at>n AND se.idle_expires_at>n AND ($4::boolean OR se.authentication_method<>'dev_phone')),
 public_places AS MATERIALIZED(SELECT p.*,p.xmin::text token FROM places p JOIN city c ON c.id=p.city_id,result_clock WHERE p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>n)),
 public_activities AS MATERIALIZED(SELECT a.*,a.xmin::text token,ao.xmin::text organizer_token,ao.person_account_id,ao.organization_id organizer_org,ao.community_id organizer_community,ao.business_id organizer_business
 FROM activities a JOIN city c ON c.id=a.city_id JOIN activity_organizers ao ON ao.activity_id=a.id CROSS JOIN result_clock
 WHERE a.publication_status='published' AND birdtie_activity_visible_to(a.id,$2::uuid) AND a.cancelled_at IS NULL AND a.ends_at>n AND (a.expires_at IS NULL OR a.expires_at>n)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$2 AND bl.blocked_account_id=a.host_account_id) OR (bl.blocked_account_id=$2 AND bl.blocker_account_id=a.host_account_id))
 AND (ao.person_account_id IS NULL OR EXISTS(SELECT 1 FROM accounts h WHERE h.id=ao.person_account_id AND h.status='active' AND h.account_type='person'))
 AND (ao.organization_id IS NULL OR EXISTS(SELECT 1 FROM organizations o JOIN accounts h ON h.id=o.account_id WHERE o.id=ao.organization_id AND o.status='active' AND h.status='active'))
 AND (ao.community_id IS NULL OR EXISTS(SELECT 1 FROM communities c WHERE c.id=ao.community_id AND c.lifecycle_status='active' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>n)))
 AND (ao.business_id IS NULL OR EXISTS(SELECT 1 FROM businesses b JOIN accounts h ON h.id=b.account_id WHERE b.id=ao.business_id AND b.status='active' AND b.claim_status='verified' AND h.status='active'))
 AND (ao.business_id IS NULL OR a.place_id IS NULL OR EXISTS(SELECT 1 FROM business_venue_relations r JOIN venues v ON v.place_id=r.place_id
 JOIN venue_candidates vc ON vc.id=v.source_candidate_id AND vc.status='approved' AND vc.place_id=v.place_id AND vc.city_id=v.city_id AND vc.reviewed_by=v.reviewed_by
 LEFT JOIN organizations operator ON operator.id=v.operator_organization_id LEFT JOIN accounts operator_account ON operator_account.id=operator.account_id
 WHERE r.business_id=ao.business_id AND r.place_id=a.place_id AND r.status='verified' AND r.reviewed_at IS NOT NULL AND r.reviewed_at<=n
 AND isfinite(v.expires_at) AND v.expires_at>n AND (v.operator_organization_id IS NULL OR (operator.status='active' AND operator_account.status='active'))))),
 own_opportunity AS MATERIALIZED(SELECT raw FROM (` + human + `) original(raw) WHERE $5::jsonb->>'Kind'='opportunity'),
 opportunity_activities AS MATERIALIZED(SELECT DISTINCT a.*,a.xmin::text activity_token,ao.xmin::text organizer_token,
 ao.person_account_id person,ao.community_id community,ao.organization_id organization,ao.business_id business
 FROM own_opportunity own CROSS JOIN LATERAL jsonb_array_elements(own.raw->'inputs'->'Supply') entry
 JOIN activities a ON a.id::text=entry->'activity'->>'id' JOIN activity_organizers ao ON ao.activity_id=a.id),
 opportunity_deadline AS MATERIALIZED(SELECT min(deadline) deadline FROM (
 SELECT i.expires_at deadline FROM social_intents i WHERE i.id::text IN(SELECT entry->>'id' FROM own_opportunity own CROSS JOIN LATERAL jsonb_array_elements(own.raw->'inputs'->'Intents') entry)
 UNION ALL SELECT least(a.ends_at,a.expires_at,p.expires_at,c.expires_at,v.expires_at) FROM opportunity_activities a JOIN places p ON p.id=a.place_id JOIN cities c ON c.id=a.city_id LEFT JOIN venues v ON v.place_id=p.id
 UNION ALL SELECT c.expires_at FROM communities c WHERE c.id::text IN(SELECT jsonb_object_keys(own.raw->'inputs'->'JoinedCommunities') FROM own_opportunity own)
 ) bounded_deadlines),
 sources AS MATERIALIZED(
 SELECT 'place'::text kind,p.id::text id,p.name title,p.summary,p.updated_at::text version,
 CASE WHEN p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL THEN jsonb_build_object('coordinateSystem','wgs84','precision','point','latitude',p.latitude,'longitude',p.longitude,'placeId',p.id) END anchor,
 p.expires_at deadline,p.token token FROM public_places p
 UNION ALL SELECT 'activity',a.id::text,a.title,a.summary,a.updated_at::text,
 CASE WHEN a.modality<>'online' AND p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL THEN jsonb_build_object('coordinateSystem','wgs84','precision','point','latitude',p.latitude,'longitude',p.longitude,'placeId',p.id) END,
 least(a.ends_at,a.expires_at,p.expires_at,(SELECT v.expires_at FROM venues v WHERE v.place_id=p.id AND a.organizer_business IS NOT NULL)),concat_ws(':',a.token,a.organizer_token,p.token)
 FROM public_activities a LEFT JOIN public_places p ON p.id=a.place_id CROSS JOIN query CROSS JOIN result_clock CROSS JOIN city c
 WHERE (coalesce(q->>'Category','')='' OR a.category_code=q->>'Category' OR strpos(lower(a.title||' '||a.summary),lower(q->>'Category'))>0)
 AND (coalesce(q->>'TimePreference','') NOT IN ('weekend','today','tomorrow','tonight')
 OR (q->>'TimePreference'='weekend' AND a.starts_at>=((date_trunc('week',n AT TIME ZONE c.time_zone)+interval '5 days') AT TIME ZONE c.time_zone) AND a.starts_at<((date_trunc('week',n AT TIME ZONE c.time_zone)+interval '7 days') AT TIME ZONE c.time_zone))
 OR (q->>'TimePreference'='today' AND (a.starts_at AT TIME ZONE c.time_zone)::date=(n AT TIME ZONE c.time_zone)::date)
 OR (q->>'TimePreference'='tomorrow' AND (a.starts_at AT TIME ZONE c.time_zone)::date=(n AT TIME ZONE c.time_zone)::date+1)
 OR (q->>'TimePreference'='tonight' AND (a.starts_at AT TIME ZONE c.time_zone)::date=(n AT TIME ZONE c.time_zone)::date AND (a.starts_at AT TIME ZONE c.time_zone)::time>=time '18:00'))
 AND (NOT coalesce((q->>'Comparison')::boolean,false) OR a.id::text IN(SELECT jsonb_array_elements_text(q->'CompareIDs')))
 UNION ALL SELECT DISTINCT ON(i.owner_account_id) 'person',i.owner_account_id::text,
 CASE WHEN birdtie_agent_profile_field_allowed(owner.id,$2::uuid,'displayName') THEN coalesce(nullif(profile.display_name,''),nullif(owner.handle,''),'Birdtie 成员') ELSE 'Birdtie 成员' END,
 i.topic,i.updated_at::text,NULL::jsonb,least(i.expires_at,i.available_until),concat_ws(':',i.xmin::text,owner.xmin::text,profile.xmin::text)
 FROM intents i JOIN city c ON c.id=i.city_id JOIN accounts owner ON owner.id=i.owner_account_id AND owner.status='active' AND owner.account_type='person'
 JOIN user_profiles profile ON profile.account_id=owner.id AND profile.visibility='public' CROSS JOIN result_clock
 WHERE i.state='active' AND i.audience='public' AND i.owner_confirmed_at IS NOT NULL AND i.expires_at>n AND i.available_from<=n AND i.available_until>n
 AND NOT EXISTS(SELECT 1 FROM intents newer WHERE newer.owner_account_id=i.owner_account_id AND newer.city_id=i.city_id AND newer.state='active' AND newer.audience='public' AND newer.owner_confirmed_at IS NOT NULL AND newer.expires_at>n AND newer.available_from<=n AND newer.available_until>n AND (newer.updated_at,newer.id)>(i.updated_at,i.id))
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$2 AND bl.blocked_account_id=owner.id) OR (bl.blocked_account_id=$2 AND bl.blocker_account_id=owner.id))
 UNION ALL SELECT 'community',co.id::text,co.name,co.summary,co.updated_at::text,
 CASE WHEN p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL THEN jsonb_build_object('coordinateSystem','wgs84','precision','point','latitude',p.latitude,'longitude',p.longitude,'placeId',p.id) END,
 least(co.expires_at,p.expires_at),concat_ws(':',co.xmin::text,creator.xmin::text,p.token)
 FROM communities co JOIN city c ON c.id=co.city_id JOIN accounts creator ON creator.id=co.owner_account_id AND creator.account_type='person' AND creator.status='active'
 LEFT JOIN public_places p ON p.id=co.place_id CROSS JOIN result_clock
 WHERE co.publication_status='published' AND co.visibility='public' AND co.lifecycle_status='active' AND co.owner_confirmed_at IS NOT NULL AND (co.expires_at IS NULL OR co.expires_at>n)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$2 AND bl.blocked_account_id=creator.id) OR (bl.blocked_account_id=$2 AND bl.blocker_account_id=creator.id))
 UNION ALL SELECT 'organization',o.id::text,o.name,o.description,o.updated_at::text,
 CASE WHEN loc.visibility='public' AND loc.review_status='approved' AND loc.city_id=$1 AND loc.coordinate_system='wgs84' AND loc.precision='point' THEN jsonb_build_object('coordinateSystem','wgs84','precision','point','latitude',loc.latitude,'longitude',loc.longitude) END,
 NULL::timestamptz,concat_ws(':',o.xmin::text,principal.xmin::text,loc.xmin::text)
 FROM organizations o JOIN accounts principal ON principal.id=o.account_id AND principal.status='active' AND principal.account_type='organization'
 LEFT JOIN organization_map_locations loc ON loc.organization_id=o.id
 WHERE o.status='active' AND o.visibility='public' AND o.verification_status='verified'
 AND (EXISTS(SELECT 1 FROM public_activities a WHERE a.organizer_org=o.id AND a.visibility='public') OR (loc.city_id=$1 AND loc.visibility='public' AND loc.review_status='approved'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$2 AND bl.blocked_account_id=principal.id) OR (bl.blocked_account_id=$2 AND bl.blocker_account_id=principal.id))
 UNION ALL SELECT 'business',b.id::text,
 CASE WHEN permission.version IS NOT NULL THEN source.facts->>'name' ELSE b.name END,
 CASE WHEN permission.version IS NOT NULL THEN source.facts->>'description' ELSE '' END,
 b.updated_at::text,NULL::jsonb,CASE WHEN permission.version IS NOT NULL THEN least(permission.valid_until,source.valid_until) END,
 concat_ws(':',b.xmin::text,principal.xmin::text,permission.xmin::text,source.source_snapshot)
 FROM businesses b JOIN accounts principal ON principal.id=b.account_id AND principal.status='active' AND principal.account_type='business'
 LEFT JOIN LATERAL(` + business + `) source ON true
 LEFT JOIN business_public_profile_permissions permission ON permission.business_id=b.id AND permission.state='active' AND permission.profile_version=source.version
 AND permission.source_snapshot=source.source_snapshot AND permission.approved_by=source.user_account_id AND permission.valid_until>(SELECT n FROM result_clock)
 WHERE b.status='active' AND b.claim_status='verified' AND EXISTS(SELECT 1 FROM public_activities a WHERE a.organizer_business=b.id AND a.visibility='public')
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$2 AND bl.blocked_account_id=principal.id) OR (bl.blocked_account_id=$2 AND bl.blocker_account_id=principal.id))
 ), selected AS MATERIALIZED(SELECT s.*,CASE WHEN coalesce((q->>'Closer')::boolean,false) AND s.kind='activity' THEN coalesce(power((s.anchor->>'latitude')::float8-(SELECT map_center_latitude FROM city),2)+power(((s.anchor->>'longitude')::float8-(SELECT map_center_longitude FROM city))*cos(radians((SELECT map_center_latitude FROM city))),2),1e9) ELSE 0 END ordering FROM sources s,query WHERE s.kind=q->>'Kind' AND (coalesce(q->>'SearchTerm','')='' OR strpos(lower(s.title||' '||s.summary),lower(q->>'SearchTerm'))>0)
 AND (q->'Bounds' IS NULL OR q->'Bounds'='null'::jsonb OR (s.anchor->>'longitude')::float8 BETWEEN (q->'Bounds'->>'West')::float8 AND (q->'Bounds'->>'East')::float8 AND (s.anchor->>'latitude')::float8 BETWEEN (q->'Bounds'->>'South')::float8 AND (q->'Bounds'->>'North')::float8)
 ORDER BY CASE WHEN coalesce((q->>'Closer')::boolean,false) AND s.kind='activity' THEN coalesce(power((s.anchor->>'latitude')::float8-(SELECT map_center_latitude FROM city),2)+power(((s.anchor->>'longitude')::float8-(SELECT map_center_longitude FROM city))*cos(radians((SELECT map_center_latitude FROM city))),2),1e9) ELSE 0 END,
 s.title,s.id LIMIT 30),
 commercial_sources AS MATERIALIZED(SELECT d.*,d.xmin::text declaration_epoch,
 birdtie_sponsor_source_snapshot(d.business_id,d.submitted_by,CASE WHEN d.activity_id IS NOT NULL THEN 'ACTIVITY' ELSE 'PLACE' END,coalesce(d.activity_id,d.place_id),n) current_source,
 birdtie_sponsor_review_snapshot(d.city_id,d.reviewed_by,n) current_review,
 EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=d.business_id AND m.user_account_id=d.reviewed_by AND m.status='active') reviewer_member
 FROM sponsored_opportunity_declarations d CROSS JOIN result_clock
 WHERE EXISTS(SELECT 1 FROM selected s WHERE (s.kind='activity' AND d.activity_id::text=s.id AND EXISTS(SELECT 1 FROM activities public_target WHERE public_target.id=d.activity_id AND public_target.visibility='public')) OR (s.kind='place' AND d.place_id::text=s.id))),
 commercial_deadline AS MATERIALIZED(SELECT min(deadline) deadline FROM (
 SELECT expires_at deadline FROM commercial_sources WHERE expires_at>(SELECT n FROM result_clock)
 UNION ALL SELECT g.valid_until FROM sponsored_opportunity_review_grants g WHERE g.valid_until>(SELECT n FROM result_clock) AND EXISTS(SELECT 1 FROM commercial_sources d WHERE d.city_id=g.city_id AND d.reviewed_by=g.reviewer_account_id)
 UNION ALL SELECT g.valid_until FROM business_review_grants g WHERE g.valid_until>(SELECT n FROM result_clock) AND g.business_id IN(SELECT business_id FROM commercial_sources)
 UNION ALL SELECT v.valid_until FROM business_console_venue_facts v WHERE v.valid_until>(SELECT n FROM result_clock) AND v.business_id IN(SELECT business_id FROM commercial_sources)
 ) deadlines),
 action_refs AS MATERIALIZED(SELECT s.kind,s.id FROM selected s UNION SELECT 'activity',a.id::text FROM opportunity_activities a),
 action_facts AS MATERIALIZED(SELECT coalesce(jsonb_object_agg(action_ref.kind||':'||action_ref.id,` + actionFacts + `),'{}'::jsonb) raw FROM action_refs action_ref),
 legacy_sources AS MATERIALIZED(SELECT ` + resultProjectionLegacySQL() + `),
 payload AS MATERIALIZED(SELECT coalesce(jsonb_agg(jsonb_strip_nulls(jsonb_build_object('entityRef',jsonb_build_object('type',kind,'id',id),'title',btrim(title),'summary',btrim(left(summary,1000)),'scope','AUTHORIZED_VIEW',
 'detailRef',jsonb_build_object('type',kind,'id',id),'shareRef',jsonb_build_object('type',kind,'id',id),'sourceVersion',version,'anchor',anchor)) ORDER BY ordering,title,id),'[]'::jsonb) items,
 coalesce(jsonb_agg(jsonb_build_array(kind,id,token) ORDER BY kind,id),'[]'::jsonb) tokens,min(deadline) deadline FROM selected)
 SELECT result_clock.n,least(result_clock.n+interval '30 seconds',city.expires_at,payload.deadline,(SELECT min(deadline) FROM native_authority),(SELECT deadline FROM opportunity_deadline),(SELECT deadline FROM commercial_deadline)),payload.items,
 (SELECT coalesce(jsonb_agg(jsonb_build_object('type',s.kind,'id',s.id) ORDER BY s.kind,s.id),'[]'::jsonb) FROM selected s WHERE s.kind='place' OR (s.kind='activity' AND EXISTS(SELECT 1 FROM activities public_target WHERE public_target.id::text=s.id AND public_target.visibility='public'))),
 (SELECT raw FROM own_opportunity),
 encode(sha256(convert_to(jsonb_build_array(city.token,payload.tokens,(SELECT jsonb_agg(token) FROM native_authority),(SELECT raw->'inputs' FROM own_opportunity),legacy_sources.activities,legacy_sources.places,(SELECT raw FROM action_facts),` + resultProjectionClosureSQL() + `)::text,'UTF8')),'hex'),
 EXISTS(SELECT 1 FROM native_authority),legacy_sources.activities,legacy_sources.places,
 EXISTS(SELECT 1 FROM accounts viewer JOIN sessions current_session ON current_session.account_id=viewer.id CROSS JOIN result_clock
 WHERE viewer.id=$2 AND viewer.account_type='person' AND viewer.status='active' AND current_session.token_sha256=$3
 AND current_session.revoked_at IS NULL AND current_session.expires_at>n AND current_session.idle_expires_at>n AND ($4::boolean OR current_session.authentication_method<>'dev_phone')),(SELECT raw FROM action_facts)
 FROM result_clock JOIN city ON true CROSS JOIN payload CROSS JOIN legacy_sources`
}

// Reuse the original complete domain column projections and decoders, not
// guessed compatibility fields. Both arrays are captured in the last payload
// SQL, restricted to the same selected authorized refs, and sealed with Items.
func resultProjectionLegacySQL() string {
	activities := `SELECT jsonb_build_array(` + activityColumns + `) row,a.id` + publishedActivityFrom + `
 AND a.id::text IN(SELECT id FROM selected WHERE kind='activity')`
	places := `SELECT jsonb_build_array(` + placeColumns + `) row,p.id FROM places p WHERE p.id::text IN(SELECT id FROM selected WHERE kind='place')`
	legacy := `(SELECT coalesce(jsonb_agg(row ORDER BY id),'[]'::jsonb) FROM (` + activities + `) legacy_activity_rows) activities,
 (SELECT coalesce(jsonb_agg(row ORDER BY id),'[]'::jsonb) FROM (` + places + `) legacy_place_rows) places`
	return strings.NewReplacer("clock_timestamp()", "(SELECT n FROM result_clock)", "statement_timestamp()", "(SELECT n FROM result_clock)", "now()", "(SELECT n FROM result_clock)").Replace(legacy)
}

type resultProjectionJSONRow []json.RawMessage

func (row resultProjectionJSONRow) Scan(dest ...any) error {
	if len(row) != len(dest) {
		return arp.ErrUnavailable
	}
	for i := range dest {
		if e := json.Unmarshal(row[i], dest[i]); e != nil {
			return e
		}
	}
	return nil
}

// Proof includes all authority rows consulted by the public-field functions
// and human opportunity resolver, including withdrawn rows and xmin ABA. It is
// server-only; private profile values/constraints never become wire fields.
func resultProjectionClosureSQL() string {
	return `(SELECT coalesce(jsonb_agg(jsonb_build_array(kind,key,token) ORDER BY kind,key),'[]'::jsonb) FROM (
 SELECT 'commercial' kind,d.id::text key,jsonb_build_array(d.declaration_epoch,d.current_source,d.current_review,d.reviewer_member)::text token FROM commercial_sources d
 UNION ALL SELECT 'legacy-activity-profile',p.account_id::text,p.xmin::text FROM user_profiles p WHERE p.account_id IN(SELECT person_account_id FROM public_activities)
 UNION ALL SELECT 'legacy-activity-agent',a.id::text,a.xmin::text FROM agents a WHERE a.principal_account_id IN(SELECT person_account_id FROM public_activities)
 UNION ALL SELECT 'legacy-activity-agent-profile',p.agent_id::text,p.xmin::text FROM agent_profiles p WHERE p.owner_id IN(SELECT person_account_id FROM public_activities)
 UNION ALL SELECT 'legacy-activity-field-visibility',v.owner_id::text,v.xmin::text FROM agent_profile_field_visibility v WHERE v.owner_id IN(SELECT person_account_id FROM public_activities)
 UNION ALL SELECT 'legacy-profile-grant',g.id::text,g.xmin::text FROM consent_grants g WHERE g.recipient_account_id=$2 AND g.owner_account_id IN(SELECT person_account_id FROM public_activities) AND g.resource_type='profile' AND g.purpose='profile_view'
 UNION ALL SELECT 'legacy-activity-source',concat_ws(':',s.activity_id,s.candidate_id),s.xmin::text FROM activity_sources s WHERE s.activity_id IN(SELECT id FROM public_activities)
 UNION ALL SELECT 'legacy-participation',p.id::text,p.xmin::text FROM activity_participations p WHERE p.activity_id IN(SELECT id FROM public_activities)
 UNION ALL SELECT 'action-private-participation',p.id::text,p.xmin::text FROM activity_participations p WHERE p.activity_id IN(SELECT id FROM opportunity_activities)
 UNION ALL SELECT 'action-saved',s.id::text,s.xmin::text FROM saved_items s WHERE s.owner_account_id=$2 AND (s.activity_id IN(SELECT id FROM public_activities UNION SELECT id FROM opportunity_activities) OR s.place_id IN(SELECT id FROM public_places) OR s.community_id IN(SELECT id::uuid FROM selected WHERE kind='community'))
 UNION ALL SELECT 'legacy-organization',o.id::text,o.xmin::text FROM organizations o WHERE o.id IN(SELECT organization_id FROM public_activities)
 UNION ALL SELECT 'opportunity-activity',a.id::text key,concat_ws(':',a.activity_token,a.organizer_token) token FROM opportunity_activities a
 UNION ALL SELECT 'opportunity-place',p.id::text,p.xmin::text FROM places p WHERE p.id IN(SELECT place_id FROM opportunity_activities)
 UNION ALL SELECT 'opportunity-city',c.id,c.xmin::text FROM cities c WHERE c.id IN(SELECT city_id FROM opportunity_activities) OR c.id IN(SELECT city_id FROM contexts WHERE id IN(SELECT context_id FROM social_intents WHERE creator_account_id=$2))
 UNION ALL SELECT 'opportunity-host',h.id::text,h.xmin::text FROM accounts h WHERE h.id IN(SELECT host_account_id FROM opportunity_activities UNION SELECT person FROM opportunity_activities)
 UNION ALL SELECT 'opportunity-org',o.id::text,concat_ws(':',o.xmin::text,h.xmin::text) FROM organizations o JOIN accounts h ON h.id=o.account_id WHERE o.id IN(SELECT organization FROM opportunity_activities) OR o.id IN(SELECT organization_id FROM follows WHERE follower_account_id=$2)
 UNION ALL SELECT 'opportunity-business',b.id::text,concat_ws(':',b.xmin::text,h.xmin::text) FROM businesses b JOIN accounts h ON h.id=b.account_id WHERE b.id IN(SELECT business FROM opportunity_activities) OR b.id IN(SELECT business_id FROM follows WHERE follower_account_id=$2)
 UNION ALL SELECT 'opportunity-community',c.id::text,c.xmin::text FROM communities c WHERE c.id IN(SELECT community FROM opportunity_activities) OR c.id IN(SELECT community_id FROM follows WHERE follower_account_id=$2)
 UNION ALL SELECT 'opportunity-invite',i.id::text,i.xmin::text FROM activity_invitations i WHERE i.activity_id IN(SELECT id FROM opportunity_activities)
 UNION ALL SELECT 'opportunity-org-member',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.organization_id IN(SELECT organization FROM opportunity_activities)
 UNION ALL SELECT 'opportunity-venue',v.place_id::text,v.xmin::text FROM venues v WHERE v.place_id IN(SELECT place_id FROM opportunity_activities)
 UNION ALL SELECT 'opportunity-venue-candidate',c.id::text,c.xmin::text FROM venue_candidates c WHERE c.id IN(SELECT source_candidate_id FROM venues WHERE place_id IN(SELECT place_id FROM opportunity_activities))
 UNION ALL SELECT 'opportunity-venue-relation',concat_ws(':',r.business_id,r.place_id),r.xmin::text FROM business_venue_relations r WHERE r.place_id IN(SELECT place_id FROM opportunity_activities)
 UNION ALL
 SELECT 'block' kind,concat_ws(':',b.blocker_account_id,b.blocked_account_id) key,b.xmin::text token FROM account_blocks b WHERE b.blocker_account_id=$2 OR b.blocked_account_id=$2
 UNION ALL SELECT 'activity',a.id::text,a.token FROM public_activities a
 UNION ALL SELECT 'organizer',a.id::text,a.organizer_token FROM public_activities a
 UNION ALL SELECT 'place',p.id::text,p.token FROM public_places p
 UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id=$2 OR a.id IN(SELECT host_account_id FROM public_activities UNION SELECT person_account_id FROM public_activities UNION SELECT owner_account_id FROM intents WHERE city_id=$1 UNION SELECT owner_account_id FROM communities WHERE city_id=$1 UNION SELECT o.account_id FROM organizations o WHERE o.id IN(SELECT organizer_org FROM public_activities) OR o.id IN(SELECT id::uuid FROM selected WHERE kind='organization') UNION SELECT b.account_id FROM businesses b WHERE b.id IN(SELECT organizer_business FROM public_activities))
 UNION ALL SELECT 'profile',p.account_id::text,p.xmin::text FROM user_profiles p WHERE p.account_id IN(SELECT owner_account_id FROM intents WHERE city_id=$1)
 UNION ALL SELECT 'agent',a.id::text,a.xmin::text FROM agents a WHERE a.principal_account_id=$2 OR a.principal_account_id IN(SELECT owner_account_id FROM intents WHERE city_id=$1)
 UNION ALL SELECT 'agent-profile',p.agent_id::text,p.xmin::text FROM agent_profiles p WHERE p.owner_id=$2 OR p.owner_id IN(SELECT owner_account_id FROM intents WHERE city_id=$1)
 UNION ALL SELECT 'visibility',v.owner_id::text,v.xmin::text FROM agent_profile_field_visibility v WHERE v.owner_id IN(SELECT owner_account_id FROM intents WHERE city_id=$1)
 UNION ALL SELECT 'tie',t.id::text,t.xmin::text FROM person_ties t WHERE t.person_a_account_id=$2 OR t.person_b_account_id=$2
 UNION ALL SELECT 'request',r.id::text,r.xmin::text FROM connection_requests r WHERE r.sender_account_id=$2 OR r.recipient_account_id=$2
 UNION ALL SELECT 'member',m.id::text,m.xmin::text FROM community_memberships m WHERE m.user_account_id=$2 OR m.community_id IN(SELECT community_id FROM community_memberships WHERE user_account_id=$2)
 UNION ALL SELECT 'community',c.id::text,c.xmin::text FROM communities c WHERE c.city_id=$1 OR c.id IN(SELECT community_id FROM community_memberships WHERE user_account_id=$2)
 UNION ALL SELECT 'follow',f.id::text,f.xmin::text FROM follows f WHERE f.follower_account_id=$2
 UNION ALL SELECT 'intent',i.id::text,i.xmin::text FROM social_intents i WHERE i.creator_account_id=$2
 UNION ALL SELECT 'target',t.intent_id::text,t.xmin::text FROM social_intent_audience_targets t WHERE t.intent_id IN(SELECT id FROM social_intents WHERE creator_account_id=$2)
 UNION ALL SELECT 'context',c.id::text,c.xmin::text FROM contexts c WHERE c.city_id=$1 OR c.id IN(SELECT context_id FROM social_intents WHERE creator_account_id=$2)
 UNION ALL SELECT 'declaration',concat_ws(':',p.person_account_id,p.context_id,p.relation),p.xmin::text FROM person_contexts p WHERE p.person_account_id=$2
 UNION ALL SELECT 'activity-invite',concat_ws(':',i.activity_id,i.invitee_account_id),i.xmin::text FROM activity_invitations i WHERE i.activity_id IN(SELECT id FROM public_activities)
 UNION ALL SELECT 'organization',o.id::text,o.xmin::text FROM organizations o WHERE o.id IN(SELECT organizer_org FROM public_activities) OR o.id IN(SELECT id::uuid FROM selected WHERE kind='organization')
 UNION ALL SELECT 'org-member',m.id::text,m.xmin::text FROM organization_memberships m WHERE m.organization_id IN(SELECT organizer_org FROM public_activities)
 UNION ALL SELECT 'business',b.id::text,b.xmin::text FROM businesses b WHERE b.id IN(SELECT organizer_business FROM public_activities)
 UNION ALL SELECT 'business-member',m.id::text,m.xmin::text FROM business_memberships m WHERE m.business_id IN(SELECT organizer_business FROM public_activities)
 UNION ALL SELECT 'venue-relation',concat_ws(':',r.business_id,r.place_id),r.xmin::text FROM business_venue_relations r WHERE r.place_id IN(SELECT id FROM public_places)
 UNION ALL SELECT 'venue',v.place_id::text,v.xmin::text FROM venues v WHERE v.place_id IN(SELECT id FROM public_places)
 UNION ALL SELECT 'venue-candidate',c.id::text,c.xmin::text FROM venue_candidates c WHERE c.id IN(SELECT source_candidate_id FROM venues WHERE place_id IN(SELECT id FROM public_places))
 UNION ALL SELECT 'venue-operator',o.id::text,concat_ws(':',o.xmin::text,a.xmin::text) FROM organizations o JOIN accounts a ON a.id=o.account_id WHERE o.id IN(SELECT operator_organization_id FROM venues WHERE place_id IN(SELECT id FROM public_places))
 UNION ALL SELECT 'opportunity-operator',o.id::text,concat_ws(':',o.xmin::text,a.xmin::text) FROM organizations o JOIN accounts a ON a.id=o.account_id WHERE o.id IN(SELECT operator_organization_id FROM venues WHERE place_id IN(SELECT place_id FROM opportunity_activities))
 ) native_source_versions)`
}

func (s *Store) captureAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query) (arp.Receipt, error) {
	var r arp.Receipt
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || !a.Valid() || !q.Valid() {
		return r, arp.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return r, arp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	r, e = s.captureAgentResultProjectionTx(ctx, tx, a, q, nil)
	if e != nil {
		return r, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return arp.Receipt{}, arp.ErrUnavailable
	}
	return r, nil
}

// The native ModelRun uses this same domain projection in its existing
// transaction. No model input or client DTO can supply the optional fence.
func (s *Store) captureAgentResultProjectionTx(ctx context.Context, tx pgx.Tx, a arp.Access, q arp.Query, fence *modelOutputFence) (arp.Receipt, error) {
	return s.captureAgentResultProjectionCurrentTx(ctx, tx, a, q, fence, nil)
}

// The ordinary human tool adds a restriction to the SAME final payload. The
// original ModelRun fence/signature and all seven-source readers remain intact.
func (s *Store) captureAgentResultProjectionCurrentTx(ctx context.Context, tx pgx.Tx, a arp.Access, q arp.Query, fence *modelOutputFence, policy *nativeToolPolicy) (arp.Receipt, error) {
	return s.captureAgentResultProjectionSelectedTx(ctx, tx, a, q, fence, policy, nil)
}

// history is minted only from this owned Task's persisted message by the
// private history reader. The original current-query guard stays unchanged.
func (s *Store) captureAgentResultProjectionSelectedTx(ctx context.Context, tx pgx.Tx, a arp.Access, q arp.Query, fence *modelOutputFence, policy *nativeToolPolicy, history *nativeReplySelector) (arp.Receipt, error) {
	var r arp.Receipt
	if ctx == nil || ctx.Err() != nil || tx == nil || !a.Valid() || !q.Valid() {
		return r, arp.ErrDenied
	}
	var e error
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';`+resultProjectionRelations); e != nil {
		return r, arp.ErrUnavailable
	}
	var owner string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.Actor.ID).Scan(&owner); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return r, identity.ErrUnauthorized
		}
		return r, arp.ErrUnavailable
	}
	current, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND principal_type='person' FOR SHARE`, a.TaskID, a.Actor.ID))
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, agentworkspace.ErrNotFound) {
			return r, agentworkspace.ErrNotFound
		}
		return r, arp.ErrDenied
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(current))
	if e != nil {
		return r, arp.ErrUnavailable
	}
	var expected, actual any
	if json.Unmarshal(a.ExpectedTask, &expected) != nil || json.Unmarshal(raw, &actual) != nil || !reflect.DeepEqual(expected, actual) || current.CityID != q.CityID {
		return r, arp.ErrChanged
	}
	// A caller cannot turn a public activity Task into an owner-private
	// opportunity read by changing an internal Query. The original task's closed
	// operation, explicit slots and viewport remain the authoritative request.
	if history != nil {
		if !history.valid(current, q) || fence != nil || policy == nil {
			return r, arp.ErrDenied
		}
	} else {
		allowedKind := map[string]string{agentworkspace.FindActivity: "activity", agentworkspace.AreaDiscovery: "activity", agentworkspace.RefineResults: "activity", agentworkspace.CompareResults: "activity", agentworkspace.FindPlace: "place", agentworkspace.FindPerson: "person", agentworkspace.FindCommunity: "community", agentworkspace.FindOrganization: "organization", agentworkspace.FindBusiness: "business", agentworkspace.FindOpportunity: "opportunity"}[current.Intent]
		if q.Comparison != (current.Intent == agentworkspace.CompareResults) {
			return r, arp.ErrDenied
		}
		if q.Comparison {
			for _, id := range q.CompareIDs {
				found := false
				for _, old := range strings.Split(current.Filters["resultIDs"], ",") {
					if id == old {
						found = true
						break
					}
				}
				if !found {
					return r, arp.ErrDenied
				}
			}
		}
		if q.Kind != allowedKind || q.SearchTerm != current.Filters["searchTerm"] || q.Category != current.Filters["category"] || q.TimePreference != current.Filters["timePreference"] || q.Closer != (current.Filters["distancePreference"] == "closer") {
			return r, arp.ErrDenied
		}
		bounds, boundsErr := agentworkspace.BoundsFromFilters(current.Filters)
		if boundsErr != nil {
			return r, arp.ErrDenied
		}
		if (bounds == nil) != (q.Bounds == nil) || (bounds != nil && *q.Bounds != (arp.Bounds{West: bounds.West, South: bounds.South, East: bounds.East, North: bounds.North})) {
			return r, arp.ErrDenied
		}
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return r, e
	}
	query, e := json.Marshal(q)
	if history != nil {
		query, e = json.Marshal(struct {
			arp.Query
			HistoryRefs []arp.Ref
		}{q, history.membership.Refs})
	}
	if e != nil {
		return r, arp.ErrInvalid
	}
	var items, commercialRefs, ownRaw, activityRows, placeRows, actionRaw []byte
	var authority, currentSession bool
	sql, args := modelOutputProjectionStatement(fence, q.CityID, a.Actor.ID, a.SessionDigest[:], s.devPhoneEnabled, query, a.TaskID)
	if history != nil {
		// Constrain original membership BEFORE the original LIMIT 30. Filtering
		// a latest-search receipt afterward would lose rows and its proof.
		sql = historicalReplyProjectionSQL(sql)
	}
	if policy != nil {
		sql, args = currentToolProjectionStatement(sql, args, policy)
	}
	e = tx.QueryRow(ctx, sql, args...).Scan(&r.ObservedAt, &r.ValidUntil, &items, &commercialRefs, &ownRaw, &r.Proof, &authority, &activityRows, &placeRows, &currentSession, &actionRaw)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, arp.ErrDenied
	}
	if e != nil {
		return r, arp.ErrUnavailable
	}
	if !currentSession {
		return r, identity.ErrUnauthorized
	}
	if !authority {
		return r, agentworkspace.ErrNotFound
	}
	if json.Unmarshal(items, &r.Items) != nil || json.Unmarshal(commercialRefs, &r.PublicCommercialRefs) != nil {
		return r, arp.ErrUnavailable
	}
	var originalActivities, originalPlaces []resultProjectionJSONRow
	if json.Unmarshal(activityRows, &originalActivities) != nil || json.Unmarshal(placeRows, &originalPlaces) != nil {
		return r, arp.ErrUnavailable
	}
	r.Activities = []foundation.Activity{}
	r.Places = []foundation.Place{}
	for _, row := range originalActivities {
		activity, e := scanActivity(row)
		if e != nil {
			return r, arp.ErrUnavailable
		}
		activity.Source.SetFreshness(r.ObservedAt)
		activity.Status = "upcoming"
		if !activity.StartsAt.After(r.ObservedAt) {
			activity.Status = "ongoing"
		}
		r.Activities = append(r.Activities, activity)
	}
	for _, row := range originalPlaces {
		place, e := scanPlace(row)
		if e != nil {
			return r, arp.ErrUnavailable
		}
		place.Source.SetFreshness(r.ObservedAt)
		r.Places = append(r.Places, place)
	}
	if q.Kind == "opportunity" {
		var p humanSocialPayload
		if len(ownRaw) == 0 || json.Unmarshal(ownRaw, &p) != nil {
			return r, arp.ErrUnavailable
		}
		p.Inputs.PersonID = a.Actor.ID
		for _, c := range opportunity.Generate(p.Now, p.Inputs) {
			if len(r.Items) >= 30 {
				break
			}
			if c.Entity.Type != "ACTIVITY" {
				continue
			}
			ref := arp.Ref{Type: "activity", ID: c.Entity.ID}
			r.Items = append(r.Items, arp.Item{Entity: arp.Ref{Type: "opportunity", ID: c.ID}, Title: c.Title, Summary: c.Reason, Scope: arp.SelfPrivate, Detail: &ref, Share: &ref, SourceVersion: c.RuleVersion})
		}
	}
	// The existing action descriptors share the SAME restricted source lifetime;
	// a short current policy cannot leave a longer-lived card proposal behind.
	if policy != nil {
		r.ValidUntil = retryMinimum(r.ValidUntil, policy.until)
		if !r.ValidUntil.After(r.ObservedAt) {
			return arp.Receipt{}, arp.ErrChanged
		}
	}
	var actionFacts map[string]ea.Facts
	if json.Unmarshal(actionRaw, &actionFacts) != nil {
		return arp.Receipt{}, arp.ErrUnavailable
	}
	for i := range r.Items {
		ref := r.Items[i].Entity
		if ref.Type == "opportunity" && r.Items[i].Detail != nil {
			ref = *r.Items[i].Detail
		}
		facts, ok := actionFacts[ref.Key()]
		if !ok {
			return arp.Receipt{}, arp.ErrUnavailable
		}
		r.Items[i].Actions = ea.Project(ea.Ref{Type: ref.Type, ID: ref.ID}, facts, r.ObservedAt, r.ValidUntil, r.Items[i].Title, r.Proof).Actions
		r.Items[i].ActionsSourceVersion = r.Proof
		deadline := r.ValidUntil
		r.Items[i].ActionsValidUntil = &deadline
	}
	if arp.ValidateItems(r.Items) != nil || !r.ValidUntil.After(r.ObservedAt) {
		return arp.Receipt{}, arp.ErrChanged
	}
	return r, nil
}

func (s *Store) ReadAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query) (arp.Receipt, error) {
	r, e := s.captureAgentResultProjection(ctx, a, q)
	if e != nil {
		return r, e
	}
	r.Seal, e = resultProjectionSeal(a, q, r)
	return r, e
}
func (s *Store) RevalidateAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query, r arp.Receipt) error {
	if !a.Valid() || !q.Valid() || !r.Valid() {
		return arp.ErrDenied
	}
	seal, e := resultProjectionSeal(a, q, r)
	if e != nil {
		return e
	}
	if !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return arp.ErrDenied
	}
	next, e := s.captureAgentResultProjection(ctx, a, q)
	if e != nil {
		return e
	}
	// Action proposal deadlines are newly observed, bounded clocks. They cannot
	// extend the original sealed receipt's validity; compare the source and
	// descriptor values separately instead of treating a fresh clock as ABA.
	beforeItems := append([]arp.Item(nil), r.Items...)
	afterItems := append([]arp.Item(nil), next.Items...)
	for i := range beforeItems {
		beforeItems[i].ActionsValidUntil = nil
	}
	for i := range afterItems {
		afterItems[i].ActionsValidUntil = nil
	}
	if !r.ValidUntil.After(next.ObservedAt) || r.Proof != next.Proof || !reflect.DeepEqual(beforeItems, afterItems) || !reflect.DeepEqual(r.PublicCommercialRefs, next.PublicCommercialRefs) || !reflect.DeepEqual(r.Activities, next.Activities) || !reflect.DeepEqual(r.Places, next.Places) {
		return arp.ErrChanged
	}
	return nil
}
