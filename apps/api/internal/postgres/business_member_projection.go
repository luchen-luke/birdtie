package postgres

import (
	"context"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/jackc/pgx/v5"
)

// Only labels for the already-authorized, locked Business roster are read.
// Membership does not grant access to a Person's private Profile, contacts,
// declarations, Agent processing or a cross-Business directory. Both the
// ordinary public resource and its additional field policy must allow the
// actual human reader; the Business principal is never used as the viewer.
const businessMemberDisplayNamesSQL = `SELECT m.user_account_id,p.display_name
 FROM business_memberships m
 JOIN accounts person ON person.id=m.user_account_id AND person.account_type='person' AND person.status='active'
 JOIN user_profiles p ON p.account_id=person.id AND p.visibility='public'
 WHERE m.business_id=$1 AND m.user_account_id=ANY($2::uuid[])
 AND birdtie_profile_visible_to(person.id,$3::uuid)
 AND birdtie_agent_profile_field_allowed(person.id,$3::uuid,'displayName')
 ORDER BY m.user_account_id`

func attachBusinessMemberDisplayNames(ctx context.Context, tx pgx.Tx, b businessBinding, members []businessconsole.Member) error {
	if len(members) == 0 {
		return nil
	}
	ids := make([]string, len(members))
	byID := make(map[string]int, len(members))
	for i := range members {
		ids[i] = members[i].PersonID
		byID[ids[i]] = i
	}
	rows, e := tx.Query(ctx, businessMemberDisplayNamesSQL, b.access.BusinessID, ids, b.access.ActingPersonID)
	if e != nil {
		return businessError(e)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if e = rows.Scan(&id, &name); e != nil {
			return businessError(e)
		}
		if i, ok := byID[id]; ok {
			members[i].DisplayName = businessconsole.NormalizeMemberDisplayName(name)
		}
	}
	if e = rows.Err(); e != nil {
		return businessError(e)
	}
	return nil
}
