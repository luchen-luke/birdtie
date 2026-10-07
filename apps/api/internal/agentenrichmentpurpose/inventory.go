package agentenrichmentpurpose

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const InventorySchema = "agent-enrichment-purpose-inventory-v1"
const InventoryLimit = 50
const InventoryReadTTL = 30 * time.Second
const InventoryMaxBytes = 64 * 1024

// Inventory is a bounded human observation of existing consent metadata. It
// cannot be passed to ResolveOwnEnrichmentPurpose as execution authority.
type Inventory struct {
	SchemaVersion string                `json:"schemaVersion"`
	Owner         actorref.PrincipalRef `json:"owner"`
	AgentID       string                `json:"agentId"`
	ObservedAt    time.Time             `json:"observedAt"`
	ValidUntil    time.Time             `json:"validUntil"`
	Limit         int                   `json:"limit"`
	Truncated     bool                  `json:"truncated"`
	Grants        []Grant               `json:"grants"`
}

// Optional capability keeps every original by-ID Store implementation intact.
type InventoryStore interface {
	ListOwnEnrichmentPurposes(context.Context, agentprofile.PrivateAccess) (Inventory, error)
}

func inventoryTime(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && t.Equal(t.Truncate(time.Microsecond))
}

func ValidateInventory(v Inventory) error {
	if v.SchemaVersion != InventorySchema || v.Owner.Type != actorref.Person || !ValidID(v.Owner.ID) || !ValidID(v.AgentID) ||
		!inventoryTime(v.ObservedAt) || !inventoryTime(v.ValidUntil) || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > InventoryReadTTL ||
		v.Limit != InventoryLimit || v.Grants == nil || len(v.Grants) > InventoryLimit || (v.Truncated && len(v.Grants) != InventoryLimit) {
		return ErrUnavailable
	}
	seen := make(map[string]bool, len(v.Grants))
	for _, g := range v.Grants {
		if ValidateGrant(g) != nil || g.Owner != v.Owner || g.AgentID != v.AgentID || !g.ObservedAt.Equal(v.ObservedAt) ||
			!inventoryTime(g.CreatedAt) || !inventoryTime(g.ExpiresAt) || (g.RevokedAt != nil && !inventoryTime(*g.RevokedAt)) || seen[g.ID] {
			return ErrUnavailable
		}
		seen[g.ID] = true
	}
	return nil
}
