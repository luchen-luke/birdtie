package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.PublicMomentStore = (*Store)(nil)

// One PG wall-clock statement returns only the current public payload and
// current authority. A supplied invalid session is never anonymous fallback.
const publicMomentProjectionSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
SELECT m.id,m.title,m.body,p.id,p.name,c.id,m.revision,m.published_at,
 concat_ws(':',m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text)
FROM moments m JOIN accounts a ON a.id=m.author_account_id
JOIN places p ON p.id=m.place_id AND p.city_id=m.city_id JOIN cities c ON c.id=p.city_id CROSS JOIN cl
WHERE m.id=$1 AND m.status='published' AND m.visibility='public' AND m.location_precision='place'
AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL
AND m.author_confirmed_at<=m.published_at AND m.published_at<=cl.at
AND a.account_type='person' AND a.status='active'
AND p.publication_status='published' AND c.publication_status='published'
AND (p.expires_at IS NULL OR p.expires_at>cl.at) AND (c.expires_at IS NULL OR c.expires_at>cl.at)
AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
AND ((b.blocker_account_id=$2 AND b.blocked_account_id=a.id) OR (b.blocker_account_id=a.id AND b.blocked_account_id=$2)))
AND ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM accounts viewer JOIN sessions ss ON ss.account_id=viewer.id
WHERE viewer.id=$2 AND viewer.account_type='person' AND viewer.status='active' AND ss.token_sha256=$3
AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at
AND ($4::boolean OR ss.authentication_method<>'dev_phone')))`

func (s *Store) GetPublicMoment(ctx context.Context, access content.PublicMomentAccess, id string) (content.PublicMoment, error) {
	if !validHumanMomentID(id) {
		return content.PublicMoment{}, content.ErrInvalid
	}
	var viewer any
	if access.Actor.ID != "" {
		if access.Actor.AccountType != "person" || !validHumanMomentID(access.Actor.ID) || access.SessionDigest == ([32]byte{}) {
			return content.PublicMoment{}, identity.ErrUnauthorized
		}
		viewer = access.Actor.ID
	} else if access.SessionDigest != ([32]byte{}) {
		return content.PublicMoment{}, identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return content.PublicMoment{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	var out content.PublicMoment
	var binding string
	err = tx.QueryRow(ctx, publicMomentProjectionSQL, id, viewer, access.SessionDigest[:], s.devPhoneEnabled).Scan(&out.ID, &out.Title, &out.Body, &out.PlaceID, &out.PlaceName, &out.CityID, &out.Revision, &out.PublishedAt, &binding)
	if errors.Is(err, pgx.ErrNoRows) {
		if viewer != nil {
			if e := checkHumanMomentSession(ctx, tx, access.SessionDigest, access.Actor.ID, s.devPhoneEnabled); e != nil {
				return content.PublicMoment{}, e
			}
		}
		return content.PublicMoment{}, content.ErrNotFound
	}
	if err != nil || ctx.Err() != nil {
		return content.PublicMoment{}, content.ErrUnavailable
	}
	digest := sha256.Sum256([]byte(binding))
	out.SchemaVersion, out.SourceBinding = "public-moment-v1", hex.EncodeToString(digest[:])
	out.PublishedAt = out.PublishedAt.UTC()
	if content.ValidatePublicMoment(out, id) != nil {
		return content.PublicMoment{}, content.ErrUnavailable
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return content.PublicMoment{}, content.ErrUnavailable
	}
	return out, nil
}
