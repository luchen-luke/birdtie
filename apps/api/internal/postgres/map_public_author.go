package postgres

import "strings"

// Original Block/Unblock writes retain this pair's safety event generation even
// after account_blocks is deleted. No audit text or generation crosses the wire.
// It is a final-read fence, not a new grant or a replacement for current blocks.
const mapPublicAuthorSafetyGenerationSQL = `coalesce((SELECT max(safety.id) FROM audit_events safety
 WHERE safety.resource_type='account' AND safety.decision='allowed' AND safety.purpose='personal_safety'
 AND safety.action IN('block','unblock') AND $6::uuid IS NOT NULL
 AND ((safety.actor_account_id=$6::uuid AND safety.resource_id=a.id::text)
 OR (safety.actor_account_id=a.id AND safety.resource_id=$6::uuid::text))),0)`

var mapPublicAuthorProjectionSQL = mapPublicAuthorReadSQL()

// The current public-author selector is an opt-in over the original MapLayers
// source, not an author catalog. Reuse its approved point/bounds, bounded typed
// payload, session generation, deadlines and proof instead of broadening them.
func mapPublicAuthorReadSQL() string {
	before, rest, ok := strings.Cut(mapProjectionRowsSQL, " raw_sources AS MATERIALIZED(")
	if !ok {
		return ""
	}
	_, after, ok := strings.Cut(rest, " logical AS MATERIALIZED(")
	if !ok {
		return ""
	}
	target := ` public_author_source AS MATERIALIZED(SELECT a.id,
 concat_ws(':',a.xmin::text,` + publicMomentAuthorBindingSQL("a") + `,` + mapPublicAuthorSafetyGenerationSQL + `) token,
 ` + publicMomentAuthorProfileSQL("a") + ` public_profile
 FROM accounts a CROSS JOIN stamp WHERE a.id=$9::uuid AND a.account_type='person' AND a.status='active'
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $6::uuid IS NOT NULL
 AND ((b.blocker_account_id=$6::uuid AND b.blocked_account_id=a.id)
 OR (b.blocker_account_id=a.id AND b.blocked_account_id=$6::uuid)))),
 author_target AS MATERIALIZED(SELECT id,token FROM public_author_source WHERE public_profile IS NOT NULL),
 raw_sources AS MATERIALIZED(
 SELECT 'MOMENT'::text kind,m.id::text id,m.title title,p.id::text place_id,p.name label,p.latitude,p.longitude,
 m.revision::text version,p.retained_until deadline,concat_ws(':',m.xmin::text,author.xmin::text,p.row_token) token
 FROM moments m JOIN point p ON p.id=m.place_id AND p.city_id=m.city_id
 JOIN accounts author ON author.id=m.author_account_id JOIN author_target target ON target.id=author.id CROSS JOIN stamp
 WHERE m.status='published' AND m.visibility='public' AND m.location_precision='place' AND m.author_confirmed_at IS NOT NULL
 AND m.author_confirmed_at<=m.published_at AND m.published_at<=stamp.at
 AND author.account_type='person' AND author.status='active'),
`
	query := before + target + " logical AS MATERIALIZED(" + after
	query = strings.ReplaceAll(query, "PRIVATE_SOURCE_TOKENS", "(SELECT token FROM author_target)")
	query = strings.ReplaceAll(query, "PRIVATE_HUMAN_PAYLOAD", "NULL::jsonb")
	// Keep the authorized target in the final row even when payload has no items.
	// Unknown/private/blocked targets all yield the same native ErrNotFound.
	return strings.Replace(query, "FROM city c CROSS JOIN stamp CROSS JOIN payload", "FROM city c CROSS JOIN stamp CROSS JOIN payload CROSS JOIN author_target", 1)
}
