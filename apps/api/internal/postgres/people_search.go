package postgres

import (
	"context"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *Store) SearchPublicPersonIDs(ctx context.Context, a identity.ProfileAccess, query string) ([]string, error) {
	if s == nil || s.pool == nil || a.Actor.AccountType != "person" || a.SessionDigest == ([32]byte{}) {
		return nil, identity.ErrUnauthorized
	}
	// Escape LIKE operators: a name search must not become a wildcard directory
	// dump. All profile data is re-read and proven by the canonical profile ACL.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, e := s.pool.Query(ctx, `SELECT p.account_id::text FROM user_profiles p
 JOIN accounts a ON a.id=p.account_id AND a.account_type='person' AND a.status='active'
 WHERE p.visibility='public' AND p.account_id<>$1
 AND (p.display_name ILIKE $2 ESCAPE '\' OR p.account_id::text=$3)
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
   (b.blocker_account_id=$1 AND b.blocked_account_id=p.account_id) OR
   (b.blocked_account_id=$1 AND b.blocker_account_id=p.account_id))
 AND EXISTS(SELECT 1 FROM sessions x JOIN accounts owner ON owner.id=x.account_id
   WHERE x.account_id=$1 AND x.token_sha256=$4 AND owner.account_type='person' AND owner.status='active'
   AND x.revoked_at IS NULL AND x.expires_at>clock_timestamp() AND x.idle_expires_at>clock_timestamp()
   AND ($5::boolean OR x.authentication_method<>'dev_phone'))
 ORDER BY lower(p.display_name),p.account_id LIMIT 20`, a.Actor.ID, "%"+escaped+"%", query, a.SessionDigest[:], s.devPhoneEnabled)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := make([]string, 0, 20)
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
