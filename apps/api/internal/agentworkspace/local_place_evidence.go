package agentworkspace

import (
	"time"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
)

const LocalPlaceReplySchema = "birdtie.local-place-reply.v1"

// These native IDs and versions correlate one private reply to independent
// public field permissions and current explicit Memory. They grant no read,
// provider export, shared cache, automatic collection or external action.
type LocalPlaceFactRef struct {
	EvidenceID   string `json:"evidenceId"`
	PlaceID      string `json:"placeId"`
	Field        string `json:"field"`
	Version      int64  `json:"version"`
	PermissionID string `json:"permissionId"`
}
type LocalPlaceReplyEvidence struct {
	Schema               string                    `json:"schema"`
	ID                   string                    `json:"id"`
	SourceEvidenceDigest string                    `json:"sourceEvidenceDigest"`
	ValidUntil           time.Time                 `json:"validUntil"`
	Facts                []LocalPlaceFactRef       `json:"facts"`
	MemoryUses           []acb.LocalFoodPreference `json:"memoryUses"`
	Reason               string                    `json:"reason"`
	SearchCalls          int                       `json:"searchCalls"`
	ModelCalls           int                       `json:"modelCalls"`
}
