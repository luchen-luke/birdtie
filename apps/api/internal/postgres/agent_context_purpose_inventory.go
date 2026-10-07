package postgres

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

var _ acb.PurposeInventoryStore = (*Store)(nil)

const taskContextInventorySQL = `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at), selected AS MATERIALIZED (
 SELECT g.id,g.revision,g.created_at,g.expires_at,g.revoked_at,
 p.selection->>'taskId' task_id,p.selection->>'cityId' city_id,p.selection->'taskUpdatedAt' task_updated_at,
 p.selection->'profileFields' profile_fields,p.selection->'memoryIds' memory_ids,p.selection->'placeIds' place_ids,
 p.selection->'activityIds' activity_ids,p.selection->'relationshipTieIds' relationship_tie_ids,p.selection->'policyFamilies' policy_families,
 (g.revoked_at IS NULL AND g.expires_at>clk.at) current_permit
 FROM consent_grants g JOIN agent_context_purpose_bindings binding ON binding.grant_id=g.id
 JOIN agent_context_purpose_previews p ON p.id=binding.preview_id CROSS JOIN clk
 WHERE g.owner_account_id=$1 AND g.recipient_account_id=$1 AND p.owner_id=$1 AND p.agent_id=$2 AND p.session_id=$3
 AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose='TASK_CONTEXT_READ' AND g.actions=ARRAY['read']::text[]
 ORDER BY current_permit DESC,g.created_at DESC,g.id LIMIT 51
 ) SELECT clk.at,COALESCE((SELECT jsonb_agg(jsonb_build_object(
 'id',s.id,'revision',s.revision,'purpose','TASK_CONTEXT_READ','taskId',s.task_id,'cityId',s.city_id,'taskUpdatedAt',s.task_updated_at,
 'profileFields',s.profile_fields,'memoryIds',s.memory_ids,'placeIds',s.place_ids,'activityIds',s.activity_ids,
 'relationshipTieIds',s.relationship_tie_ids,'policyFamilies',s.policy_families,
 'createdAt',s.created_at,'expiresAt',s.expires_at,'revokedAt',s.revoked_at)
 ORDER BY s.current_permit DESC,s.created_at DESC,s.id) FROM selected s),'[]'::jsonb) FROM clk`

func (s *Store) listTaskContextInventoryTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding) (acb.PurposeInventory, error) {
	var out acb.PurposeInventory
	var raw []byte
	if e := tx.QueryRow(ctx, taskContextInventorySQL, b.owner, b.agent, b.session).Scan(&out.ObservedAt, &raw); e != nil {
		return out, acb.ErrUnavailable
	}
	if len(raw) > acb.PurposeInventoryMaxBytes || json.Unmarshal(raw, &out.Grants) != nil || out.Grants == nil || len(out.Grants) > 51 {
		return acb.PurposeInventory{}, acb.ErrUnavailable
	}
	out.SchemaVersion = acb.PurposeInventorySchema
	out.Owner = actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}
	out.AgentID = b.agent
	out.Limit = 50
	out.Truncated = len(out.Grants) > 50
	out.ValidUntil = out.ObservedAt.Add(acb.PurposeInventoryReadTTL)
	if b.limit.Before(out.ValidUntil) {
		out.ValidUntil = b.limit
	}
	all := out.Grants
	seen := map[string]bool{}
	// The sentinel row must be validated before hiding it; corruption is not empty.
	for _, g := range all {
		if seen[g.ID] || acb.ValidatePurposeInventoryGrant(g, out.ObservedAt) != nil {
			return acb.PurposeInventory{}, acb.ErrUnavailable
		}
		seen[g.ID] = true
	}
	if out.Truncated {
		out.Grants = all[:50]
	}
	if acb.ValidatePurposeInventory(out) != nil {
		return acb.PurposeInventory{}, acb.ErrUnavailable
	}
	at, e := s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return acb.PurposeInventory{}, e
	}
	if at.Before(out.ObservedAt) || !at.Before(out.ValidUntil) {
		return acb.PurposeInventory{}, acb.ErrUnavailable
	}
	return out, nil
}
func (s *Store) ListOwnContextPurposes(ctx context.Context, a agentprofile.PrivateAccess) (acb.PurposeInventory, error) {
	tx, b, e := s.beginContextBuilder(ctx, a)
	if e != nil {
		return acb.PurposeInventory{}, e
	}
	defer tx.Rollback(context.Background())
	out, e := s.listTaskContextInventoryTx(ctx, tx, b)
	if e != nil {
		return acb.PurposeInventory{}, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return acb.PurposeInventory{}, acb.ErrUnavailable
	}
	return out, nil
}
