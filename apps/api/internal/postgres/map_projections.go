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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/jackc/pgx/v5"
)

var _ mp.Store = (*Store)(nil)
var mapProjectionKey struct {
	sync.Once
	key [32]byte
	err error
}

func mapProjectionSeal(a mp.Access, r mp.Receipt) (string, error) {
	mapProjectionKey.Do(func() { _, mapProjectionKey.err = rand.Read(mapProjectionKey.key[:]) })
	if mapProjectionKey.err != nil {
		return "", mp.ErrUnavailable
	}
	raw, e := json.Marshal(struct {
		Actor   identity.Actor
		Digest  string
		Receipt struct {
			View  mp.View
			Query mp.Query
			Proof string
		}
	}{Actor: a.Actor, Digest: hex.EncodeToString(a.Digest[:]), Receipt: struct {
		View  mp.View
		Query mp.Query
		Proof string
	}{r.View, r.Query, r.Proof}})
	if e != nil {
		return "", mp.ErrUnavailable
	}
	h := hmac.New(sha256.New, mapProjectionKey.key[:])
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Public-only source rows. Personal profiles, Moment body/private links,
// Console facts, rights notes and participant data are never selected here.
// Organization uses its reviewed point; other types use current public Place.
// Native Session generation distinguishes security changes from ordinary idle
// renewal. The generation never crosses the map wire and cannot grant access.
const mapProjectionSessionAuthoritySQL = `jsonb_build_array(a.id,a.xmin::text,se.id,se.authorization_generation,se.created_at,se.expires_at,se.authentication_method,encode(se.token_sha256,'hex'))`

const mapProjectionRowsSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at),
 city AS MATERIALIZED(SELECT c.* FROM cities c,stamp WHERE c.id=$1 AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>stamp.at)),
 point AS MATERIALIZED(SELECT p.*,p.xmin::text row_token FROM places p JOIN city c ON c.id=p.city_id CROSS JOIN stamp
 WHERE p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>stamp.at)
 AND p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL
 AND p.longitude BETWEEN $2 AND $4 AND p.latitude BETWEEN $3 AND $5),
 raw_sources AS MATERIALIZED(
 SELECT 'PLACE'::text kind,p.id::text id,p.name title,p.id::text place_id,p.name label,p.latitude,p.longitude,p.updated_at::text version,p.expires_at deadline,p.row_token token FROM point p
 UNION ALL SELECT 'ACTIVITY',a.id::text,a.title,p.id::text,p.name,p.latitude,p.longitude,a.updated_at::text,least(a.expires_at,a.ends_at,p.expires_at),
 concat_ws(':',a.xmin::text,ao.xmin::text,p.row_token,host.xmin::text,
 (SELECT o.xmin::text||':'||principal.xmin::text FROM organizations o JOIN accounts principal ON principal.id=o.account_id WHERE o.id=ao.organization_id),
 (SELECT c.xmin::text FROM communities c WHERE c.id=ao.community_id),
 (SELECT b.xmin::text||':'||principal.xmin::text FROM businesses b JOIN accounts principal ON principal.id=b.account_id WHERE b.id=ao.business_id))
 FROM activities a JOIN point p ON p.id=a.place_id JOIN activity_organizers ao ON ao.activity_id=a.id LEFT JOIN accounts host ON host.id=a.host_account_id,stamp
 WHERE a.city_id=$1 AND a.publication_status='published' AND a.visibility='public' AND a.modality IN ('in_person','hybrid') AND a.physical_place_status='confirmed'
 AND a.cancelled_at IS NULL AND a.ends_at>stamp.at AND (a.expires_at IS NULL OR a.expires_at>stamp.at)
 AND (a.host_account_id IS NULL OR host.status='active')
 AND (ao.organization_id IS NULL OR EXISTS(SELECT 1 FROM organizations o JOIN accounts principal ON principal.id=o.account_id WHERE o.id=ao.organization_id AND o.status='active' AND principal.status='active'))
 AND (ao.community_id IS NULL OR EXISTS(SELECT 1 FROM communities c WHERE c.id=ao.community_id AND c.lifecycle_status='active' AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>stamp.at)))
 AND (ao.business_id IS NULL OR EXISTS(SELECT 1 FROM business_venue_relations relation JOIN venues venue ON venue.place_id=relation.place_id JOIN venue_candidates candidate ON candidate.id=venue.source_candidate_id AND candidate.status='approved' AND candidate.place_id=venue.place_id AND candidate.city_id=venue.city_id AND candidate.reviewed_by=venue.reviewed_by WHERE relation.business_id=ao.business_id AND relation.place_id=p.id AND relation.status='verified' AND venue.expires_at>stamp.at))
 AND birdtie_activity_visible_to(a.id,$6::uuid)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE $6::uuid IS NOT NULL AND ((bl.blocker_account_id=$6 AND bl.blocked_account_id=a.host_account_id) OR (bl.blocker_account_id=a.host_account_id AND bl.blocked_account_id=$6)))
 UNION ALL SELECT 'MOMENT',m.id::text,m.title,p.id::text,p.name,p.latitude,p.longitude,m.revision::text,p.expires_at,concat_ws(':',m.xmin::text,author.xmin::text,p.row_token)
 FROM moments m JOIN point p ON p.id=m.place_id AND p.city_id=m.city_id JOIN accounts author ON author.id=m.author_account_id,stamp
 WHERE m.status='published' AND m.visibility='public' AND m.location_precision='place' AND m.author_confirmed_at IS NOT NULL
 AND m.author_confirmed_at<=m.published_at AND m.published_at<=stamp.at AND author.account_type='person' AND author.status='active'
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE $6::uuid IS NOT NULL AND ((bl.blocker_account_id=$6 AND bl.blocked_account_id=author.id) OR (bl.blocker_account_id=author.id AND bl.blocked_account_id=$6)))
 UNION ALL SELECT 'ORGANIZATION',o.id::text,o.name,''::text,'组织明确公开地点',l.latitude,l.longitude,l.revision::text,NULL::timestamptz,concat_ws(':',o.xmin::text,principal.xmin::text,l.xmin::text)
 FROM organization_map_locations l JOIN organizations o ON o.id=l.organization_id JOIN accounts principal ON principal.id=o.account_id JOIN city c ON c.id=l.city_id
 WHERE l.visibility='public' AND l.review_status='approved' AND l.coordinate_system='wgs84' AND l.precision='point'
 AND l.longitude BETWEEN $2 AND $4 AND l.latitude BETWEEN $3 AND $5 AND o.status='active' AND o.visibility='public' AND o.verification_status='verified'
 AND principal.status='active' AND principal.account_type='organization'
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE $6::uuid IS NOT NULL AND ((bl.blocker_account_id=$6 AND bl.blocked_account_id=principal.id) OR (bl.blocker_account_id=principal.id AND bl.blocked_account_id=$6)))
 UNION ALL SELECT 'BUSINESS',biz.id::text,biz.name,p.id::text,p.name,p.latitude,p.longitude,biz.updated_at::text,least(v.expires_at,p.expires_at),
 concat_ws(':',biz.xmin::text,principal.xmin::text,r.xmin::text,v.xmin::text,vc.xmin::text,p.row_token,operator.xmin::text)
 FROM businesses biz JOIN accounts principal ON principal.id=biz.account_id
 JOIN business_venue_relations r ON r.business_id=biz.id JOIN venues v ON v.place_id=r.place_id
 JOIN venue_candidates vc ON vc.id=v.source_candidate_id AND vc.status='approved' AND vc.place_id=v.place_id AND vc.city_id=v.city_id AND vc.reviewed_by=v.reviewed_by
 JOIN point p ON p.id=v.place_id AND p.city_id=v.city_id LEFT JOIN organizations operator ON operator.id=v.operator_organization_id,stamp
 WHERE biz.status='active' AND biz.claim_status='verified' AND principal.account_type='business' AND principal.status='active'
 AND r.status='verified' AND r.reviewed_at IS NOT NULL AND r.reviewed_at<=stamp.at AND isfinite(v.expires_at) AND v.expires_at>stamp.at
 AND (v.operator_organization_id IS NULL OR operator.status='active') AND birdtie_verified_business_venue(biz.id,p.id)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE $6::uuid IS NOT NULL AND ((bl.blocker_account_id=$6 AND bl.blocked_account_id=principal.id) OR (bl.blocker_account_id=principal.id AND bl.blocked_account_id=$6)))
 ),
 logical AS MATERIALIZED(SELECT DISTINCT ON(kind,id) * FROM raw_sources ORDER BY kind,id,place_id),
 ranked AS MATERIALIZED(SELECT logical.*,row_number() OVER(PARTITION BY kind ORDER BY id) rank FROM logical),
 bounded AS MATERIALIZED(SELECT * FROM ranked WHERE rank<=31),
 payload AS MATERIALIZED(SELECT coalesce(jsonb_agg(jsonb_build_object('kind',kind,'id',id,'title',title,
 'entityRef',jsonb_build_object('type',kind,'id',id),'detailRef',jsonb_build_object('type',kind,'id',id),
 'anchor',jsonb_build_object('placeId',place_id,'label',label,'coordinateSystem','wgs84','precision','point','latitude',latitude,'longitude',longitude),
 'sourceVersion',version) ORDER BY kind,id) FILTER(WHERE rank<=30),'[]'::jsonb) items,
 min(deadline) deadline,coalesce(bool_or(rank>30),false) truncated,
 coalesce(jsonb_agg(jsonb_build_array(kind,id,token) ORDER BY kind,id),'[]'::jsonb) tokens FROM bounded),
 native_actor AS MATERIALIZED(SELECT ` + mapProjectionSessionAuthoritySQL + ` token,least(se.expires_at,se.idle_expires_at) deadline
 FROM accounts a JOIN sessions se ON se.account_id=a.id CROSS JOIN stamp WHERE a.id=$6 AND a.account_type='person' AND a.status='active'
 AND se.token_sha256=$7 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at AND ($8::boolean OR se.authentication_method<>'dev_phone'))
 SELECT stamp.at,least(stamp.at+interval '2 minutes',c.expires_at,payload.deadline,(SELECT deadline FROM native_actor)),payload.items,payload.truncated,
 encode(sha256(convert_to(jsonb_build_array((SELECT xmin::text FROM cities WHERE id=$1),payload.tokens,(SELECT token FROM native_actor),PRIVATE_SOURCE_TOKENS)::text,'UTF8')),'hex'),
 ($6::uuid IS NULL OR EXISTS(SELECT 1 FROM native_actor)),PRIVATE_HUMAN_PAYLOAD
 FROM city c CROSS JOIN stamp CROSS JOIN payload`

func mapProjectionSQL(private bool) string {
	if !private {
		return strings.ReplaceAll(strings.ReplaceAll(mapProjectionRowsSQL, "PRIVATE_SOURCE_TOKENS", "NULL::jsonb"), "PRIVATE_HUMAN_PAYLOAD", "NULL::jsonb")
	}
	replacements := map[string]string{"$1": "$6", "$2": "$6", "$3": "$7", "$4": "$8", "$5": "'opportunities'", "$6": "''"}
	query := regexp.MustCompile(`\$[1-6]`).ReplaceAllStringFunc(humanSocialSQL, func(k string) string { return replacements[k] })
	// Fingerprints stay server-side. They preserve exact original private source
	// versions even when text/updatedAt are restored; no second grant ledger.
	privateTokens := `jsonb_build_array(
 (SELECT coalesce(jsonb_agg(jsonb_build_array(i.id,i.xmin::text) ORDER BY i.id),'[]'::jsonb) FROM social_intents i WHERE i.id IN (SELECT id FROM social_intents WHERE creator_account_id=$6 ORDER BY created_at DESC,id DESC LIMIT 100)),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(pc.context_id,pc.xmin::text,c.xmin::text) ORDER BY pc.context_id),'[]'::jsonb) FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id WHERE pc.person_account_id=$6),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(t.id,t.xmin::text,r.xmin::text,pa.xmin::text,pb.xmin::text) ORDER BY t.id),'[]'::jsonb) FROM person_ties t JOIN connection_requests r ON r.id=t.request_id JOIN accounts pa ON pa.id=t.person_a_account_id JOIN accounts pb ON pb.id=t.person_b_account_id WHERE t.person_a_account_id=$6 OR t.person_b_account_id=$6),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.xmin::text) ORDER BY f.id),'[]'::jsonb) FROM follows f WHERE f.follower_account_id=$6),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(m.community_id,m.xmin::text,c.xmin::text) ORDER BY m.community_id),'[]'::jsonb) FROM community_memberships m JOIN communities c ON c.id=m.community_id WHERE m.user_account_id=$6))`
	return strings.ReplaceAll(strings.ReplaceAll(mapProjectionRowsSQL, "PRIVATE_SOURCE_TOKENS", privateTokens), "PRIVATE_HUMAN_PAYLOAD", "("+query+")")
}

func (s *Store) captureMapLayers(ctx context.Context, a mp.Access, q mp.Query) (mp.Receipt, error) {
	var r mp.Receipt
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || !a.Valid() || mp.ValidateQuery(q) != nil {
		return r, mp.ErrInvalid
	}
	if q.Private && a.Anonymous() {
		return r, identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return r, mp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return r, mp.ErrUnavailable
	}
	// All relation/Account/Session waits occur before the one payload statement.
	if _, e = tx.Exec(ctx, humanSocialRelations+`; LOCK TABLE moments,organization_map_locations IN ACCESS SHARE MODE`); e != nil {
		return r, mp.ErrUnavailable
	}
	var viewer any
	if !a.Anonymous() {
		viewer = a.Actor.ID
		var id string
		if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, viewer).Scan(&id); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return r, identity.ErrUnauthorized
			}
			return r, mp.ErrUnavailable
		}
		if e = s.lockHumanMomentSession(ctx, tx, a.Digest, a.Actor.ID); e != nil {
			return r, e
		}
	}
	var raw, human []byte
	var current bool
	e = tx.QueryRow(ctx, mapProjectionSQL(q.Private), q.CityID, q.West, q.South, q.East, q.North, viewer, a.Digest[:], s.devPhoneEnabled).
		Scan(&r.View.ObservedAt, &r.View.ValidUntil, &raw, &r.View.Truncated, &r.Proof, &current, &human)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, mp.ErrNotFound
	}
	if e != nil || ctx.Err() != nil {
		return r, mp.ErrUnavailable
	}
	if !current {
		return r, identity.ErrUnauthorized
	}
	r.Query = q
	r.View.SchemaVersion = mp.SchemaVersion
	r.View.CityID = q.CityID
	r.View.Scope = mp.Public
	r.View.ObservedAt = r.View.ObservedAt.UTC()
	r.View.ValidUntil = r.View.ValidUntil.UTC()
	if e = json.Unmarshal(raw, &r.View.Items); e != nil {
		return r, mp.ErrUnavailable
	}
	if q.Private {
		if len(human) == 0 {
			return mp.Receipt{}, identity.ErrUnauthorized
		}
		var p humanSocialPayload
		if json.Unmarshal(human, &p) != nil {
			return mp.Receipt{}, mp.ErrUnavailable
		}
		p.Inputs.PersonID = a.Actor.ID
		all := opportunity.Generate(p.Now, p.Inputs)
		anchors := map[string]mp.Item{}
		for _, item := range r.View.Items {
			if item.Kind == "ACTIVITY" {
				anchors[item.ID] = item
			}
		}
		r.View.Scope = mp.SelfPrivate
		r.View.Items = []mp.Item{}
		for _, candidate := range all {
			anchor, ok := anchors[candidate.Entity.ID]
			if !ok || candidate.Place.ID != anchor.Anchor.PlaceID {
				continue
			}
			item := mp.Item{Kind: "OPPORTUNITY", ID: candidate.ID, Title: candidate.Title, Entity: mp.Ref{Type: "OPPORTUNITY", ID: candidate.ID}, Detail: mp.Ref{Type: "ACTIVITY", ID: candidate.Entity.ID}, Anchor: anchor.Anchor, SourceVersion: anchor.SourceVersion}
			r.View.Items = append(r.View.Items, item)
			for _, intent := range p.Inputs.Intents {
				if intent.ID == candidate.IntentID && intent.ExpiresAt.Before(r.View.ValidUntil) {
					r.View.ValidUntil = intent.ExpiresAt.UTC()
				}
			}
		}
		// Internal private input fingerprint excludes clocks; never crosses the wire.
		p.Now = time.Time{}
		privateRaw, e := json.Marshal(p)
		if e != nil {
			return mp.Receipt{}, mp.ErrUnavailable
		}
		hash := sha256.Sum256(append([]byte(r.Proof), privateRaw...))
		r.Proof = hex.EncodeToString(hash[:])
	}
	if mp.ValidateView(r.View) != nil {
		return mp.Receipt{}, mp.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return mp.Receipt{}, mp.ErrUnavailable
	}
	return r, nil
}
func (s *Store) ReadMapLayers(ctx context.Context, a mp.Access, q mp.Query) (mp.Receipt, error) {
	r, e := s.captureMapLayers(ctx, a, q)
	if e != nil {
		return r, e
	}
	r.Seal, e = mapProjectionSeal(a, r)
	return r, e
}
func (s *Store) RevalidateMapLayers(ctx context.Context, a mp.Access, r mp.Receipt) error {
	if mp.ValidateView(r.View) != nil || mp.ValidateQuery(r.Query) != nil || !a.Valid() {
		return mp.ErrInvalid
	}
	seal, e := mapProjectionSeal(a, r)
	if e != nil || !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return mp.ErrChanged
	}
	current, e := s.captureMapLayers(ctx, a, r.Query)
	if e != nil {
		return e
	}
	if !r.View.ValidUntil.After(current.View.ObservedAt) || r.Proof != current.Proof || !reflect.DeepEqual(r.View.Items, current.View.Items) || r.View.Truncated != current.View.Truncated {
		return mp.ErrChanged
	}
	return nil
}
