package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ ph.Store = (*Store)(nil)

// One final READ COMMITTED statement supplies all public payloads and their
// PG CheckedAt. A supplied session is never silently treated as anonymous.
// No Profile/Memory/Participation/private links enter this projection.
const placeHistoryPayloadSQL = `WITH clock AS MATERIALIZED (SELECT clock_timestamp() at),
 target AS MATERIALIZED (SELECT p.id,p.city_id FROM places p JOIN cities c ON c.id=p.city_id CROSS JOIN clock
   WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published'
   AND (p.expires_at IS NULL OR p.expires_at>clock.at) AND (c.expires_at IS NULL OR c.expires_at>clock.at)),
 recent AS MATERIALIZED (SELECT m.id,m.title,left(m.body,280) excerpt,m.revision,m.published_at FROM moments m
   JOIN target t ON t.id=m.place_id AND t.city_id=m.city_id JOIN accounts a ON a.id=m.author_account_id CROSS JOIN clock
   WHERE m.status='published' AND m.visibility='public' AND m.location_precision='place'
   AND m.author_confirmed_at IS NOT NULL AND m.author_confirmed_at<=m.published_at
   AND m.published_at>=clock.at-interval '30 days' AND m.published_at<=clock.at
   AND a.account_type='person' AND a.status='active'
   AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
    AND ((b.blocker_account_id=$2 AND b.blocked_account_id=a.id) OR (b.blocker_account_id=a.id AND b.blocked_account_id=$2)))),
 arrangements AS MATERIALIZED (SELECT COALESCE(a.category_code,'') category,
   CASE WHEN extract(isodow FROM a.starts_at AT TIME ZONE a.time_zone) IN (6,7) THEN 'WEEKEND' ELSE 'WEEKDAY' END day_kind
   FROM activities a JOIN target t ON t.id=a.place_id AND t.city_id=a.city_id
   JOIN activity_organizers ao ON ao.activity_id=a.id CROSS JOIN clock
   WHERE a.publication_status='published' AND a.visibility='public' AND a.cancelled_at IS NULL
   AND a.ends_at>=clock.at-interval '30 days' AND a.ends_at<=clock.at
   AND (a.expires_at IS NULL OR a.expires_at>clock.at)
   AND (a.host_account_id IS NULL OR EXISTS(SELECT 1 FROM accounts host WHERE host.id=a.host_account_id AND host.status='active'))
   AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND a.host_account_id IS NOT NULL
      AND ((b.blocker_account_id=$2 AND b.blocked_account_id=a.host_account_id) OR (b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$2)))
   AND ((ao.person_account_id IS NOT NULL AND EXISTS(SELECT 1 FROM accounts person WHERE person.id=ao.person_account_id AND person.account_type='person' AND person.status='active'))
    OR (ao.community_id IS NOT NULL AND EXISTS(SELECT 1 FROM communities co JOIN accounts creator ON creator.id=co.owner_account_id
       WHERE co.id=ao.community_id AND co.lifecycle_status='active' AND co.visibility='public' AND co.publication_status='published'
       AND (co.expires_at IS NULL OR co.expires_at>clock.at) AND creator.status='active'))
    OR (ao.organization_id IS NOT NULL AND EXISTS(SELECT 1 FROM organizations org JOIN accounts principal ON principal.id=org.account_id
       WHERE org.id=ao.organization_id AND org.status='active' AND org.visibility='public' AND principal.status='active'))
    OR (ao.business_id IS NOT NULL AND EXISTS(SELECT 1 FROM businesses biz JOIN accounts principal ON principal.id=biz.account_id
       WHERE biz.id=ao.business_id AND biz.status='active' AND biz.claim_status='verified' AND principal.status='active'
       AND birdtie_verified_business_venue(biz.id,a.place_id)
       AND EXISTS(SELECT 1 FROM venues venue WHERE venue.place_id=a.place_id AND venue.expires_at>clock.at)))))
 SELECT clock.at,t.city_id,(SELECT count(*) FROM recent),
   COALESCE((SELECT jsonb_agg(jsonb_build_object('id',r.id,'title',r.title,'excerpt',r.excerpt,'revision',r.revision,'publishedAt',r.published_at)
     ORDER BY r.published_at DESC,r.id DESC) FROM (SELECT * FROM recent ORDER BY published_at DESC,id DESC LIMIT 5) r),'[]'::jsonb),
   COALESCE((SELECT jsonb_agg(jsonb_build_object('category',p.category,'dayKind',p.day_kind,'arrangements',p.n) ORDER BY p.category,p.day_kind)
     FROM (SELECT category,day_kind,count(*) n FROM arrangements GROUP BY category,day_kind) p),'[]'::jsonb),
   (SELECT jsonb_build_object('schemaVersion','place-semantic-v1','placeId',pr.place_id,'cityId',pr.city_id,'version',pr.version,
       'facts',c.facts,'source',jsonb_build_object('label',c.source_label,'url',c.source_url,'observedAt',c.observed_at,'reviewedAt',c.reviewed_at,'expiresAt',c.expires_at),
       'confidence',jsonb_build_object('kind',c.confidence_kind,'level',c.confidence_level),'checkedAt',clock.at)
     FROM place_semantic_profiles pr JOIN place_semantic_candidates c ON c.id=pr.source_candidate_id AND c.place_id=pr.place_id AND c.city_id=pr.city_id
     JOIN places p ON p.id=pr.place_id AND p.city_id=pr.city_id JOIN cities city ON city.id=pr.city_id
     JOIN accounts a ON a.id=c.submitted_by JOIN city_editor_memberships m ON m.city_id=c.city_id AND m.account_id=a.id
     WHERE pr.place_id=t.id AND pr.state='published' AND c.status='approved' AND c.observed_at<=c.reviewed_at AND c.reviewed_at<=clock.at
       AND ` + placeSemanticOriginPredicate + `)
 FROM target t CROSS JOIN clock
 WHERE $2::uuid IS NULL OR EXISTS(SELECT 1 FROM sessions ss JOIN accounts act ON act.id=ss.account_id
    WHERE ss.token_sha256=$3 AND act.id=$2 AND act.account_type=$4 AND act.status='active' AND ss.revoked_at IS NULL
    AND ss.expires_at>clock.at AND ss.idle_expires_at>clock.at AND ($5::boolean OR ss.authentication_method<>'dev_phone'))`

func (s *Store) GetPublicPlaceSocialHistory(ctx context.Context, access ph.Access, id string) (ph.Summary, error) {
	var out ph.Summary
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return out, ph.ErrUnavailable
	}
	if !pp.ValidID(id) || ph.ValidateAccess(access) != nil {
		return out, ph.ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return out, ph.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, ph.ErrUnavailable
	}
	var viewer any
	if access.Actor.ID != "" {
		viewer = access.Actor.ID
		var actor string
		e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type=$2 AND status='active' FOR SHARE`, viewer, access.Actor.AccountType).Scan(&actor)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, identity.ErrUnauthorized
		}
		if e != nil {
			return out, ph.ErrUnavailable
		}
	}
	// Target wait occurs before the session lock; committed revocation while
	// waiting remains observable. The final payload evaluates current expiry.
	var place string
	e = tx.QueryRow(ctx, `SELECT p.id FROM places p JOIN cities c ON c.id=p.city_id WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published' FOR SHARE OF c,p`, id).Scan(&place)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ph.ErrNotFound
	}
	if e != nil {
		return out, ph.ErrUnavailable
	}
	if viewer != nil {
		var session string
		e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, access.SessionDigest[:], viewer, s.devPhoneEnabled).Scan(&session)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, identity.ErrUnauthorized
		}
		if e != nil {
			return out, ph.ErrUnavailable
		}
	}
	var moments, patterns, profile []byte
	e = tx.QueryRow(ctx, placeHistoryPayloadSQL, id, viewer, access.SessionDigest[:], access.Actor.AccountType, s.devPhoneEnabled).Scan(&out.CheckedAt, &out.CityID, &out.RecentMomentCount, &moments, &patterns, &profile)
	if errors.Is(e, pgx.ErrNoRows) {
		if viewer != nil {
			var current bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions ss WHERE ss.token_sha256=$1 AND ss.account_id=$2 AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp())`, access.SessionDigest[:], viewer).Scan(&current)
			if e != nil {
				return ph.Summary{}, ph.ErrUnavailable
			}
			if !current {
				return ph.Summary{}, identity.ErrUnauthorized
			}
		}
		return ph.Summary{}, ph.ErrNotFound
	}
	if e != nil || ctx.Err() != nil {
		return ph.Summary{}, ph.ErrUnavailable
	}
	out.SchemaVersion = ph.SchemaVersion
	out.PlaceID = id
	out.WindowDays = ph.WindowDays
	out.CheckedAt = out.CheckedAt.UTC()
	out.WindowStart = out.CheckedAt.Add(-ph.WindowDays * 24 * time.Hour)
	if json.Unmarshal(moments, &out.RecentMoments) != nil || json.Unmarshal(patterns, &out.ActivityPatterns) != nil {
		return ph.Summary{}, ph.ErrUnavailable
	}
	if len(profile) > 0 {
		out.Suitability = &pp.Public{}
		if json.Unmarshal(profile, out.Suitability) != nil {
			return ph.Summary{}, ph.ErrUnavailable
		}
	}
	if ph.ValidateSummary(out) != nil || ctx.Err() != nil {
		return ph.Summary{}, ph.ErrUnavailable
	}
	var final time.Time
	var current bool
	e = tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) SELECT cl.at,
 EXISTS(SELECT 1 FROM places p JOIN cities c ON c.id=p.city_id WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>cl.at) AND (c.expires_at IS NULL OR c.expires_at>cl.at))
 AND ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM sessions ss JOIN accounts a ON a.id=ss.account_id WHERE a.id=$2 AND a.account_type=$4 AND a.status='active' AND ss.token_sha256=$3 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($5::boolean OR ss.authentication_method<>'dev_phone'))) FROM cl`, id, viewer, access.SessionDigest[:], access.Actor.AccountType, s.devPhoneEnabled).Scan(&final, &current)
	if e != nil || ctx.Err() != nil {
		return ph.Summary{}, ph.ErrUnavailable
	}
	if !current {
		if viewer != nil {
			var live bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp())`, access.SessionDigest[:], viewer).Scan(&live)
			if e != nil {
				return ph.Summary{}, ph.ErrUnavailable
			}
			if !live {
				return ph.Summary{}, identity.ErrUnauthorized
			}
		}
		return ph.Summary{}, ph.ErrNotFound
	}
	if out.Suitability != nil && pp.ValidatePublic(*out.Suitability, final) != nil {
		return ph.Summary{}, ph.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return ph.Summary{}, ph.ErrUnavailable
	}
	return out, nil
}
