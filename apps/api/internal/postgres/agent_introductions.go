package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	ai "github.com/birdtie/birdtie/apps/api/internal/agentintroduction"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"time"
)

var _ ai.Store = (*Store)(nil)

const introductionColumns = `i.id,i.creator_account_id,
 CASE WHEN birdtie_agent_profile_field_allowed(i.creator_account_id,NULL::uuid,'displayName')
 THEN COALESCE(NULLIF(p.display_name,''),NULLIF(a.handle,''),'Birdtie 成员') ELSE 'Birdtie 成员' END AS display_name,
 i.modality,i.constraints,coalesce(city.id,'') AS city_id,coalesce(place.id::text,'') AS place_id`

// This is an ordinary human projection. Social settings only suppress already
// public candidates; no peer private profile, Memory, RSVP or Moment link is read.
// The wall clock is evaluated after relation/pool waits. Each statement checks
// all identities, consent, both policies, public sources and bilateral Block.
const introductionProjectionSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at),
 current_source AS MATERIALIZED (
 SELECT ` + introductionColumns + `,i.expires_at AS source_end,
 least(i.expires_at,ss.expires_at,ss.idle_expires_at,pol.expires_at,city.expires_at,place.expires_at) AS read_end,
 concat_ws(':',i.xmin::text,i.updated_at::text,a.xmin::text,ag.xmin::text,ap.xmin::text,opt.xmin::text,
 (SELECT u.xmin::text FROM user_profiles u WHERE u.account_id=a.id),
 (SELECT v.xmin::text FROM agent_profile_field_visibility v WHERE v.owner_id=a.id),
 pol.xmin::text,ss.id::text,ss.created_at::text,ss.authentication_method,ss.expires_at::text,
 coalesce(city.xmin::text,''),coalesce(place.xmin::text,''),coalesce(ic.xmin::text,''),coalesce(target.xmin::text,'')) AS authority
 FROM social_intents i ` + newPeopleSignalJoins + `
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions ss ON ss.account_id=a.id AND ss.token_sha256=$3 AND ss.revoked_at IS NULL
 JOIN agent_policy_settings pol ON pol.agent_id=ag.id AND pol.owner_id=a.id AND pol.owner_type='PERSON' AND pol.family='SOCIAL'
 CROSS JOIN cl
 WHERE i.id=$2 AND i.creator_account_id=$1 AND i.audience='PUBLIC' AND ` + newPeopleActive + `
 AND i.expires_at>cl.at AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at
 AND (city.expires_at IS NULL OR city.expires_at>cl.at) AND (place.expires_at IS NULL OR place.expires_at>cl.at)
 AND (coalesce(target.city_id,ic.city_id) IS NULL OR city.id IS NOT NULL)
 AND (coalesce(i.constraints->>'placeId','')='' OR place.id IS NOT NULL)
 AND ($4::boolean OR ss.authentication_method<>'dev_phone')
 AND pol.valid_from<=cl.at AND pol.expires_at>cl.at
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(pol.settings->'rules') r WHERE r->>'category'='UNKNOWN_PERSON' AND r->>'preference'='REVIEW_REQUIRED')
 ), current_peers AS MATERIALIZED (
 SELECT ` + introductionColumns + `,least(i.expires_at,city.expires_at,place.expires_at) AS peer_end,pol.expires_at AS policy_end,
 common.binding AS community_binding,common.end_at AS community_end,
 concat_ws(':',i.xmin::text,i.updated_at::text,a.xmin::text,ag.xmin::text,ap.xmin::text,opt.xmin::text,
 (SELECT u.xmin::text FROM user_profiles u WHERE u.account_id=a.id),
 (SELECT v.xmin::text FROM agent_profile_field_visibility v WHERE v.owner_id=a.id),pol.xmin::text,
 coalesce(city.xmin::text,''),coalesce(place.xmin::text,''),coalesce(ic.xmin::text,''),coalesce(target.xmin::text,''),common.binding) AS authority
 FROM social_intents i ` + newPeopleSignalJoins + `
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN agent_policy_settings pol ON pol.agent_id=ag.id AND pol.owner_id=a.id AND pol.owner_type='PERSON' AND pol.family='SOCIAL'
 LEFT JOIN LATERAL (
 SELECT concat_ws(':',own.context_id::text,own.relation,peer.relation,own.xmin::text,peer.xmin::text,c.xmin::text,g.xmin::text,gc.id,gc.xmin::text) AS binding,least(g.expires_at,gc.expires_at) AS end_at
 FROM person_contexts own JOIN person_contexts peer ON peer.context_id=own.context_id
 JOIN contexts c ON c.id=own.context_id AND c.context_type='COMMUNITY'
 JOIN communities g ON g.id=c.community_id
 LEFT JOIN cities gc ON gc.id=g.city_id
 WHERE own.person_account_id=$1 AND peer.person_account_id=a.id
 AND own.visibility='public' AND peer.visibility='public'
 AND own.relation IN ('affiliation','interest') AND peer.relation IN ('affiliation','interest')
 AND g.visibility='public' AND g.publication_status='published' AND g.lifecycle_status='active'
 AND g.owner_confirmed_at IS NOT NULL
 AND (g.city_id IS NULL OR (gc.id IS NOT NULL AND gc.publication_status='published' AND (gc.expires_at IS NULL OR gc.expires_at>clock_timestamp())))
 AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp())
 ORDER BY own.context_id,own.relation,peer.relation LIMIT 1
 ) common ON true CROSS JOIN cl
 WHERE EXISTS(SELECT 1 FROM current_source) AND i.audience='PUBLIC' AND ` + newPeopleActive + newPeopleCandidateGate + `
 AND i.modality=(SELECT modality FROM current_source)
 AND i.expires_at>cl.at AND pol.valid_from<=cl.at AND pol.expires_at>cl.at
 AND (city.expires_at IS NULL OR city.expires_at>cl.at) AND (place.expires_at IS NULL OR place.expires_at>cl.at)
 AND (coalesce(target.city_id,ic.city_id) IS NULL OR city.id IS NOT NULL)
 AND (coalesce(i.constraints->>'placeId','')='' OR place.id IS NOT NULL)
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(pol.settings->'rules') r WHERE r->>'category'='UNKNOWN_PERSON' AND r->>'preference'='REVIEW_REQUIRED')
 AND (common.binding IS NULL OR (
 EXISTS(SELECT 1 FROM jsonb_array_elements(pol.settings->'rules') r WHERE r->>'category'='SHARED_COMMUNITY' AND r->>'preference'='REVIEW_REQUIRED')
 AND EXISTS(SELECT 1 FROM agent_policy_settings p JOIN agents source_agent ON source_agent.id=p.agent_id
 WHERE p.owner_id=$1 AND p.owner_type='PERSON' AND source_agent.principal_account_id=$1 AND source_agent.status='active' AND source_agent.agent_type='personal' AND p.family='SOCIAL'
 AND p.valid_from<=cl.at AND p.expires_at>cl.at
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(p.settings->'rules') r WHERE r->>'category'='SHARED_COMMUNITY' AND r->>'preference'='REVIEW_REQUIRED'))))
 ORDER BY i.creator_account_id,i.id LIMIT 101
 )
 SELECT row_to_json(source),row_to_json(peer),cl.at FROM current_source source LEFT JOIN current_peers peer ON true CROSS JOIN cl
 ORDER BY peer.creator_account_id,peer.id`

func (s *Store) ReadOwnIntroductionSuggestions(ctx context.Context, access agentprofile.PrivateAccess, sourceID string) (ai.Response, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return ai.Response{}, ai.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		return ai.Response{}, ai.ErrDenied
	}
	if !validHumanMomentID(sourceID) {
		return ai.Response{}, ai.ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if e != nil {
		return ai.Response{}, ai.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return ai.Response{}, ai.ErrUnavailable
	}
	first, e := s.introductionFrame(ctx, tx, access, sourceID)
	if e != nil {
		return ai.Response{}, e
	}
	last, e := s.introductionFrame(ctx, tx, access, sourceID)
	if e != nil {
		return ai.Response{}, e
	}
	// Idle Authenticate extension is not authority renewal. Keep the earlier read
	// upper bound while comparing native stable identities and every selected pair.
	if !reflect.DeepEqual(first.bindings, last.bindings) {
		return ai.Response{}, ai.ErrChanged
	}
	if first.response.ExpiresAt.Before(last.response.ExpiresAt) {
		last.response.ExpiresAt = first.response.ExpiresAt
	}
	for n := range last.response.Candidates {
		if first.response.Candidates[n].ExpiresAt.Before(last.response.Candidates[n].ExpiresAt) {
			last.response.Candidates[n].ExpiresAt = first.response.Candidates[n].ExpiresAt
		}
	}
	if !last.response.ObservedAt.Before(last.response.ExpiresAt) {
		return ai.Response{}, ai.ErrDenied
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return ai.Response{}, ai.ErrUnavailable
	}
	return last.response, nil
}

type introductionFrame struct {
	response ai.Response
	bindings []string
}

// Reuses the exact approval source function. It never selects private RSVP
// payload: only two explicit bounded PUBLIC registration disclosures.
var introductionActivityProjectionSQL = func() string {
	q := strings.Replace(introductionProjectionSQL, "common.end_at AS community_end,", "common.end_at AS community_end, common_activity.binding AS activity_binding, common_activity.end_at AS activity_end,", 1)
	q = strings.Replace(q, "common.binding) AS authority", "common.binding,common_activity.binding) AS authority", 1)
	q = strings.Replace(q, ") common ON true CROSS JOIN cl", `) common ON true CROSS JOIN cl
 LEFT JOIN LATERAL (
 SELECT concat_ws(':',own.id::text,peer.id::text,own.disclosure_revision::text,peer.disclosure_revision::text,own.xmin::text,peer.xmin::text,src.digest,sd.xmin::text,pd.xmin::text) AS binding,
 least(own.disclosure_expires_at,peer.disclosure_expires_at,src.expires_at) AS end_at
 FROM activity_participations own JOIN activity_participations peer ON peer.activity_id=own.activity_id
 JOIN activities act ON act.id=own.activity_id
 JOIN person_social_disclosure sd ON sd.account_id=own.participant_account_id AND sd.shared_activities
 JOIN person_social_disclosure pd ON pd.account_id=peer.participant_account_id AND pd.shared_activities
 CROSS JOIN LATERAL birdtie_participation_public_source(act.id,$1,cl.at) src
 CROSS JOIN LATERAL birdtie_participation_public_source(act.id,a.id,cl.at) peer_src
 WHERE own.participant_account_id=$1 AND peer.participant_account_id=a.id
 AND own.status='going' AND peer.status='going' AND own.cancelled_at IS NULL AND peer.cancelled_at IS NULL
 AND own.disclosure_visibility='public' AND peer.disclosure_visibility='public'
 AND own.disclosure_expires_at>cl.at AND peer.disclosure_expires_at>cl.at AND src.available AND peer_src.available
 AND own.disclosure_source_revision=own.disclosure_revision AND peer.disclosure_source_revision=peer.disclosure_revision
 AND own.disclosure_activity_revision=act.revision AND peer.disclosure_activity_revision=act.revision
 AND own.disclosure_source_digest=src.digest AND peer.disclosure_source_digest=src.digest
 ORDER BY own.activity_id LIMIT 1
 ) common_activity ON true`, 1)
	q = strings.Replace(q, "ORDER BY i.creator_account_id,i.id LIMIT 101", `AND (common_activity.binding IS NULL OR (
 EXISTS(SELECT 1 FROM jsonb_array_elements(pol.settings->'rules') r WHERE r->>'category'='SHARED_ACTIVITY' AND r->>'preference'='REVIEW_REQUIRED')
 AND EXISTS(SELECT 1 FROM agent_policy_settings p JOIN agents source_agent ON source_agent.id=p.agent_id
 WHERE p.owner_id=$1 AND p.owner_type='PERSON' AND source_agent.principal_account_id=$1 AND source_agent.status='active' AND source_agent.agent_type='personal' AND p.family='SOCIAL'
 AND p.valid_from<=cl.at AND p.expires_at>cl.at
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(p.settings->'rules') r WHERE r->>'category'='SHARED_ACTIVITY' AND r->>'preference'='REVIEW_REQUIRED'))))
 ORDER BY i.creator_account_id,i.id LIMIT 101`, 1)
	return q
}()

func (s *Store) introductionFrame(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, id string) (introductionFrame, error) {
	var activityEnabled bool
	if e := tx.QueryRow(ctx, participationDisclosureGuardSQL).Scan(&activityEnabled); e != nil {
		return introductionFrame{}, ai.ErrUnavailable
	}
	query := introductionProjectionSQL
	if activityEnabled {
		query = introductionActivityProjectionSQL
	}
	rows, e := tx.Query(ctx, query, a.WorkspacePrincipal.ID, id, a.SessionDigest[:], s.devPhoneEnabled)
	if e != nil {
		return introductionFrame{}, ai.ErrUnavailable
	}
	defer rows.Close()
	out := introductionFrame{}
	seen := map[string]bool{}
	found := false
	rowsConsidered := 0
	for rows.Next() {
		var source, peer []byte
		var now time.Time
		if e = rows.Scan(&source, &peer, &now); e != nil {
			return introductionFrame{}, ai.ErrUnavailable
		}
		sf, e := decodeIntroductionFrame(source)
		if e != nil {
			return introductionFrame{}, ai.ErrUnavailable
		}
		if !found {
			out.response = ai.NewResponse(id, now, sf.end)
			if activityEnabled {
				out.response.SourceStatus["SHARED_ACTIVITY"] = "PUBLIC_REGISTRATIONS_ONLY"
			}
			out.bindings = append(out.bindings, sf.authority)
			found = true
		}
		if len(peer) == 0 || strings.TrimSpace(string(peer)) == "null" {
			continue
		}
		pf, e := decodeIntroductionFrame(peer)
		rowsConsidered++
		if rowsConsidered >= 101 {
			out.response.Truncated = true
		}
		if e != nil {
			continue
		}
		basis, ok := ai.PublicBasis(sf.signal, pf.signal, pf.community != "")
		if pf.activity != "" {
			basis = append(basis, ai.Basis{Kind: "SHARED_ACTIVITY", Explanation: "双方逐活动明确公开了当前报名；不代表到场、成员资格或现实关系。"})
		}
		if !ok || seen[pf.signal.AccountID] {
			continue
		}
		seen[pf.signal.AccountID] = true
		if len(out.response.Candidates) >= 50 {
			out.response.Truncated = true
			continue
		}
		// This is a current human candidate selection, not an approved machine
		// source set. Unrelated public declarations are not selected authority.
		out.bindings = append(out.bindings, pf.authority)
		expiry := out.response.ExpiresAt
		for _, end := range []time.Time{pf.end, pf.policyEnd, pf.communityEnd, pf.activityEnd} {
			if !end.IsZero() && end.Before(expiry) {
				expiry = end
			}
		}
		digest := sha256.Sum256([]byte(sf.authority + "|" + pf.authority))
		out.response.Candidates = append(out.response.Candidates, ai.Candidate{SourceIntentID: id, CandidateIntentID: pf.signal.IntentID, AccountID: pf.signal.AccountID, DisplayName: pf.signal.DisplayName, Basis: basis, SourceBinding: hex.EncodeToString(digest[:]), ExpiresAt: expiry.UTC()})
	}
	if rows.Err() != nil || ctx.Err() != nil {
		return introductionFrame{}, ai.ErrUnavailable
	}
	if !found {
		return introductionFrame{}, ai.ErrDenied
	}
	return out, nil
}

// Explicit aliases below avoid depending on anonymous expression column names.
type decodedIntroduction struct {
	signal                                    newpeople.Signal
	authority, community, activity            string
	end, policyEnd, communityEnd, activityEnd time.Time
}

func decodeIntroductionFrame(raw []byte) (decodedIntroduction, error) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return decodedIntroduction{}, ai.ErrInvalid
	}
	var d decodedIntroduction
	decode := func(key string, to any) error {
		v, ok := m[key]
		if !ok {
			return ai.ErrInvalid
		}
		return json.Unmarshal(v, to)
	}
	if decode("id", &d.signal.IntentID) != nil || decode("creator_account_id", &d.signal.AccountID) != nil || decode("display_name", &d.signal.DisplayName) != nil || decode("modality", &d.signal.Modality) != nil || decode("city_id", &d.signal.CityID) != nil || decode("authority", &d.authority) != nil {
		return d, ai.ErrInvalid
	}
	var place string
	if decode("place_id", &place) != nil {
		return d, ai.ErrInvalid
	}
	signal, e := decodeNewPeopleSignal(d.signal, m["constraints"], place)
	if e != nil {
		return d, e
	}
	d.signal = signal
	if _, ok := m["source_end"]; ok {
		if decode("read_end", &d.end) != nil {
			return d, ai.ErrInvalid
		}
	} else {
		if decode("peer_end", &d.end) != nil || decode("policy_end", &d.policyEnd) != nil {
			return d, ai.ErrInvalid
		}
		_ = decode("community_binding", &d.community)
		_ = decode("community_end", &d.communityEnd)
		_ = decode("activity_binding", &d.activity)
		_ = decode("activity_end", &d.activityEnd)
	}
	return d, nil
}
