package agentcontextbuilder

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"time"
)

const PurposeInventorySchema = "agent-task-context-inventory-v1"
const PurposeInventoryLimit = 50
const PurposeInventoryReadTTL = 30 * time.Second
const PurposeInventoryMaxBytes = 64 * 1024

// Human metadata only. No query/body/digest, native authority, source handles,
// execution eligibility or additional grant is represented by this projection.
type PurposeInventoryGrant struct {
	ID                 string                       `json:"id"`
	Revision           int64                        `json:"revision"`
	Purpose            string                       `json:"purpose"`
	TaskID             string                       `json:"taskId"`
	CityID             string                       `json:"cityId"`
	TaskUpdatedAt      time.Time                    `json:"taskUpdatedAt"`
	ProfileFields      []string                     `json:"profileFields"`
	MemoryIDs          []string                     `json:"memoryIds"`
	PlaceIDs           []string                     `json:"placeIds"`
	ActivityIDs        []string                     `json:"activityIds"`
	RelationshipTieIDs []string                     `json:"relationshipTieIds"`
	PolicyFamilies     []agentpolicysettings.Family `json:"policyFamilies"`
	CreatedAt          time.Time                    `json:"createdAt"`
	ExpiresAt          time.Time                    `json:"expiresAt"`
	RevokedAt          *time.Time                   `json:"revokedAt,omitempty"`
}
type PurposeInventory struct {
	SchemaVersion string                  `json:"schemaVersion"`
	Owner         actorref.PrincipalRef   `json:"owner"`
	AgentID       string                  `json:"agentId"`
	ObservedAt    time.Time               `json:"observedAt"`
	ValidUntil    time.Time               `json:"validUntil"`
	Limit         int                     `json:"limit"`
	Truncated     bool                    `json:"truncated"`
	Grants        []PurposeInventoryGrant `json:"grants"`
}
type PurposeInventoryStore interface {
	ListOwnContextPurposes(context.Context, agentprofile.PrivateAccess) (PurposeInventory, error)
}

func purposeInventoryTime(t time.Time) bool {
	return ValidTime(t) && t.Equal(t.Truncate(time.Microsecond))
}
func ValidatePurposeInventoryGrant(g PurposeInventoryGrant, observed time.Time) error {
	if !validID(g.ID) || g.Revision < 1 || g.Revision > 9007199254740991 || g.Purpose != TaskContextRead || !validID(g.TaskID) || !validText(g.CityID, 100, false) ||
		!purposeInventoryTime(g.TaskUpdatedAt) || !purposeInventoryTime(g.CreatedAt) || !purposeInventoryTime(g.ExpiresAt) || g.TaskUpdatedAt.After(g.CreatedAt) || g.CreatedAt.After(observed) || !g.ExpiresAt.After(g.CreatedAt) || g.ExpiresAt.Sub(g.CreatedAt) > PurposePreviewTTL ||
		(g.RevokedAt != nil && (!purposeInventoryTime(*g.RevokedAt) || g.RevokedAt.Before(g.CreatedAt) || g.RevokedAt.After(observed))) ||
		g.ProfileFields == nil || g.MemoryIDs == nil || g.PlaceIDs == nil || g.ActivityIDs == nil || g.RelationshipTieIDs == nil || g.PolicyFamilies == nil || len(g.ProfileFields) > 3 || !uniqueIDs(g.MemoryIDs, 3) || !uniqueIDs(g.PlaceIDs, 5) || !uniqueIDs(g.ActivityIDs, 5) || !uniqueIDs(g.RelationshipTieIDs, 3) || len(g.PolicyFamilies) > 1 {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, f := range g.ProfileFields {
		if !hasKey(PrivateFieldKeys(), f) || seen[f] {
			return ErrUnavailable
		}
		seen[f] = true
	}
	for _, f := range g.PolicyFamilies {
		if !agentpolicysettings.ValidFamily(f) {
			return ErrUnavailable
		}
	}
	if len(g.ProfileFields)+len(g.MemoryIDs)+len(g.PlaceIDs)+len(g.ActivityIDs)+len(g.RelationshipTieIDs)+len(g.PolicyFamilies) == 0 {
		return ErrUnavailable
	}
	return nil
}
func ValidatePurposeInventory(v PurposeInventory) error {
	if v.SchemaVersion != PurposeInventorySchema || v.Owner.Type != actorref.Person || !validID(v.Owner.ID) || !validID(v.AgentID) || !purposeInventoryTime(v.ObservedAt) || !purposeInventoryTime(v.ValidUntil) || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > PurposeInventoryReadTTL || v.Limit != 50 || v.Grants == nil || len(v.Grants) > 50 || (v.Truncated && len(v.Grants) != 50) {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, g := range v.Grants {
		if seen[g.ID] || ValidatePurposeInventoryGrant(g, v.ObservedAt) != nil {
			return ErrUnavailable
		}
		seen[g.ID] = true
	}
	return nil
}
