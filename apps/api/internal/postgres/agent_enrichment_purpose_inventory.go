package postgres

import (
	"context"
	"encoding/json"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

var _ aep.InventoryStore = (*Store)(nil)

// A single statement observes current grant metadata, including expired and
// withdrawn records. Reading history neither consumes a source approval nor
// requires the original source/Task/Session to remain usable. There is no
// grant->preview row-lock upgrade; actual by-ID revoke retains its own locks.
const enrichmentInventorySQL = `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at), selected AS MATERIALIZED (
 SELECT g.id,g.revision,g.created_at,g.expires_at,g.revoked_at,p.id preview_id,p.selection,
 (g.revoked_at IS NULL AND g.expires_at>clk.at) current_permit
 FROM consent_grants g JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=g.id
 JOIN agent_enrichment_purpose_previews p ON p.id=eb.preview_id
 CROSS JOIN clk
 WHERE g.owner_account_id=$1 AND g.recipient_account_id=$1
 AND p.owner_id=$1 AND p.agent_id=$2 AND p.owner_type='PERSON'
 AND g.resource_type='agent_context' AND g.resource_id=p.id::text
 AND g.purpose='MOMENT_LOCAL_ANALYSIS' AND g.actions=ARRAY['analyze_local']::text[]
 ORDER BY current_permit DESC,g.created_at DESC,g.id LIMIT 51
)
 SELECT clk.at,COALESCE((SELECT jsonb_agg(jsonb_build_object(
 'schemaVersion','agent-enrichment-purpose-v1','id',s.id,'previewId',s.preview_id,
 'purpose','MOMENT_LOCAL_ANALYSIS','owner',jsonb_build_object('type','PERSON','id',$1::uuid),
 'agentId',$2::uuid,'selection',s.selection,'revision',s.revision,
 'createdAt',s.created_at,'expiresAt',s.expires_at,'revokedAt',s.revoked_at,'observedAt',clk.at,
 'modelAccess',false,'candidateRetentionAllowed',false) ORDER BY s.current_permit DESC,s.created_at DESC,s.id)
 FROM selected s),'[]'::jsonb) FROM clk`

func (s *Store) listEnrichmentInventoryTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding) (aep.Inventory, error) {
	var out aep.Inventory
	var raw []byte
	if e := tx.QueryRow(ctx, enrichmentInventorySQL, b.owner, b.agent).Scan(&out.ObservedAt, &raw); e != nil {
		return out, aep.ErrUnavailable
	}
	if len(raw) > aep.InventoryMaxBytes || json.Unmarshal(raw, &out.Grants) != nil || out.Grants == nil || len(out.Grants) > aep.InventoryLimit+1 {
		return aep.Inventory{}, aep.ErrUnavailable
	}
	out.SchemaVersion, out.Owner, out.AgentID = aep.InventorySchema, actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}, b.agent
	out.Limit, out.Truncated = aep.InventoryLimit, len(out.Grants) > aep.InventoryLimit
	out.ValidUntil = out.ObservedAt.Add(aep.InventoryReadTTL)
	if b.limit.Before(out.ValidUntil) {
		out.ValidUntil = b.limit
	}
	// Validate all fetched rows before hiding the 51st. Corruption is never a
	// fake empty list or silently omitted metadata.
	all := out.Grants
	if out.Truncated {
		out.Grants = all[:aep.InventoryLimit]
	}
	if aep.ValidateInventory(out) != nil {
		return aep.Inventory{}, aep.ErrUnavailable
	}
	if out.Truncated {
		tail := out
		tail.Truncated = false
		tail.Grants = []aep.Grant{all[aep.InventoryLimit]}
		if aep.ValidateInventory(tail) != nil {
			return aep.Inventory{}, aep.ErrUnavailable
		}
		for _, g := range out.Grants {
			if g.ID == tail.Grants[0].ID {
				return aep.Inventory{}, aep.ErrUnavailable
			}
		}
	}
	// The observation timestamp stays the actual statement clock. The final
	// native clock checks the same current owner/Agent/Session and read deadline.
	at, e := s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return aep.Inventory{}, enrichmentError(e)
	}
	if at.Before(out.ObservedAt) || !at.Before(out.ValidUntil) {
		return aep.Inventory{}, aep.ErrUnavailable
	}
	return out, nil
}

func (s *Store) ListOwnEnrichmentPurposes(ctx context.Context, a agentprofile.PrivateAccess) (aep.Inventory, error) {
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, false)
	if e != nil {
		return aep.Inventory{}, e
	}
	defer tx.Rollback(context.Background())
	out, e := s.listEnrichmentInventoryTx(ctx, tx, b)
	if e != nil {
		return aep.Inventory{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return aep.Inventory{}, aep.ErrUnavailable
	}
	return out, nil
}
