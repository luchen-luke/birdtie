package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.PublicMomentStore = (*Store)(nil)
var _ content.PublicMomentWithAuthorStore = (*Store)(nil)

// Fingerprints are only a final-read generation fence, never an authorization
// or a public DTO field. Match the canonical source and include TTL/permission
// row epochs so shortening retained evidence cannot preserve an older lease.
func retainedPlaceReadBindingSQL(alias string) string {
	return `jsonb_build_array(
 (SELECT coalesce(jsonb_agg(jsonb_build_array(src.candidate_id,src.xmin::text,src.permission_id) ORDER BY src.candidate_id),'[]'::jsonb)
  FROM place_sources src WHERE src.place_id=` + alias + `.id AND src.permission_id IS NOT NULL),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.xmin::text,rights.xmin::text,origin.xmin::text) ORDER BY f.id),'[]'::jsonb)
  FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
  WHERE f.place_id=` + alias + `.id AND rights.source_url=` + alias + `.source_ref AND f.field_key IN('name','address','coordinates')))::text`
}

func publicMomentAuthorProfileSQL(alias string) string {
	return `(SELECT CASE WHEN profile.visibility='public'
 AND birdtie_agent_profile_field_allowed(` + alias + `.id,NULL::uuid,'displayName')
 AND length(btrim(profile.display_name))>0 AND octet_length(profile.display_name)<=80
 THEN jsonb_build_object('accountId',profile.account_id,'displayName',profile.display_name) ELSE NULL END
 FROM user_profiles profile WHERE profile.account_id=` + alias + `.id)`
}

func publicMomentAuthorBindingSQL(alias string) string {
	return `jsonb_build_array(
 (SELECT profile.xmin::text FROM user_profiles profile WHERE profile.account_id=` + alias + `.id),
 (SELECT visibility.xmin::text FROM agent_profile_field_visibility visibility WHERE visibility.owner_id=` + alias + `.id AND visibility.owner_type='PERSON'),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(agent.id,agent.xmin::text,profile.xmin::text) ORDER BY agent.id),'[]'::jsonb)
 FROM agents agent LEFT JOIN agent_profiles profile ON profile.agent_id=agent.id AND profile.owner_id=` + alias + `.id AND profile.owner_type='PERSON'
 WHERE agent.principal_account_id=` + alias + `.id AND agent.agent_type='personal'))::text`
}

var publicMomentProjectionSQL = publicMomentReadSQL(false)
var publicMomentWithAuthorProjectionSQL = publicMomentReadSQL(true)

// One PG wall-clock statement returns only the current public payload and
// current authority. A supplied invalid session is never anonymous fallback.
func publicMomentReadSQL(withAuthor bool) string {
	authorBinding, authorColumn := "", ""
	if withAuthor {
		authorBinding = "," + publicMomentAuthorBindingSQL("a")
		authorColumn = "," + publicMomentAuthorProfileSQL("a")
	}
	return `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
SELECT m.id,m.title,m.body,coalesce(p.id::text,''),coalesce(p.name,''),CASE WHEN m.location_precision='none' THEN '' ELSE coalesce(c.id,'') END,m.revision,m.published_at,
 concat_ws(':',birdtie_moment_public_binding(m.id,$2,cl.at),` + momentMediaReadBindingSQL("m") + `,m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text,` + retainedPlaceReadBindingSQL("p") + authorBinding + `)` + authorColumn + `
FROM moments m JOIN accounts a ON a.id=m.author_account_id
LEFT JOIN places p ON p.id=m.place_id AND(m.city_id IS NULL OR p.city_id=m.city_id) LEFT JOIN cities c ON c.id=m.city_id CROSS JOIN cl
WHERE m.id=$1 AND m.status='published' AND m.visibility='public'
AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL
AND m.author_confirmed_at<=m.published_at AND m.published_at<=cl.at
AND a.account_type='person' AND a.status='active'
AND birdtie_moment_public_binding(m.id,$2,cl.at) IS NOT NULL
AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
AND ((b.blocker_account_id=$2 AND b.blocked_account_id=a.id) OR (b.blocker_account_id=a.id AND b.blocked_account_id=$2)))
AND ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM accounts viewer JOIN sessions ss ON ss.account_id=viewer.id
WHERE viewer.id=$2 AND viewer.account_type='person' AND viewer.status='active' AND ss.token_sha256=$3
AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at
AND ($4::boolean OR ss.authentication_method<>'dev_phone')))`
}

func (s *Store) GetPublicMoment(ctx context.Context, access content.PublicMomentAccess, id string) (content.PublicMoment, error) {
	out, err := s.getPublicMoment(ctx, access, id, false)
	return out.PublicMoment, err
}

func (s *Store) GetPublicMomentWithAuthor(ctx context.Context, access content.PublicMomentAccess, id string) (content.PublicMomentWithAuthor, error) {
	return s.getPublicMoment(ctx, access, id, true)
}

func (s *Store) getPublicMoment(ctx context.Context, access content.PublicMomentAccess, id string, withAuthor bool) (content.PublicMomentWithAuthor, error) {
	if !validHumanMomentID(id) {
		return content.PublicMomentWithAuthor{}, content.ErrInvalid
	}
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return content.PublicMomentWithAuthor{}, content.ErrUnavailable
	}
	var viewer any
	if access.Actor.ID != "" {
		if access.Actor.AccountType != "person" || !validHumanMomentID(access.Actor.ID) || access.SessionDigest == ([32]byte{}) {
			return content.PublicMomentWithAuthor{}, identity.ErrUnauthorized
		}
		viewer = access.Actor.ID
	} else if access.SessionDigest != ([32]byte{}) {
		return content.PublicMomentWithAuthor{}, identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return content.PublicMomentWithAuthor{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	var out content.PublicMomentWithAuthor
	var binding string
	var author []byte
	query := publicMomentProjectionSQL
	targets := []any{&out.ID, &out.Title, &out.Body, &out.PlaceID, &out.PlaceName, &out.CityID, &out.Revision, &out.PublishedAt, &binding}
	if withAuthor {
		query = publicMomentWithAuthorProjectionSQL
		targets = append(targets, &author)
	}
	err = tx.QueryRow(ctx, query, id, viewer, access.SessionDigest[:], s.devPhoneEnabled).Scan(targets...)
	if errors.Is(err, pgx.ErrNoRows) {
		if viewer != nil {
			if e := checkHumanMomentSession(ctx, tx, access.SessionDigest, access.Actor.ID, s.devPhoneEnabled); e != nil {
				return content.PublicMomentWithAuthor{}, e
			}
		}
		return content.PublicMomentWithAuthor{}, content.ErrNotFound
	}
	if err != nil || ctx.Err() != nil {
		return content.PublicMomentWithAuthor{}, content.ErrUnavailable
	}
	digest := sha256.Sum256([]byte(binding))
	out.SchemaVersion, out.SourceBinding = "public-moment-v1", hex.EncodeToString(digest[:])
	out.PublishedAt = out.PublishedAt.UTC()
	if content.ValidatePublicMoment(out.PublicMoment, id) != nil {
		return content.PublicMomentWithAuthor{}, content.ErrUnavailable
	}
	if withAuthor {
		out.SchemaVersion = content.PublicMomentAuthorSchemaVersion
		if len(author) > 0 && json.Unmarshal(author, &out.AuthorPublicProfile) != nil {
			return content.PublicMomentWithAuthor{}, content.ErrUnavailable
		}
		if out.AuthorPublicProfile != nil {
			id := out.AuthorPublicProfile.AccountID
			out.AuthorID = &id
		}
		if content.ValidatePublicMomentWithAuthor(out, id) != nil {
			return content.PublicMomentWithAuthor{}, content.ErrUnavailable
		}
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return content.PublicMomentWithAuthor{}, content.ErrUnavailable
	}
	return out, nil
}
