package agentmemorycandidate

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"time"
)

// HumanGateway exposes manual hypotheses and concrete human approval only.
// The sealed native preview stays in one service instance, never in a wire DTO.
type HumanGateway interface {
	List(context.Context, agentprofile.PrivateAccess) ([]Record, error)
	Read(context.Context, agentprofile.PrivateAccess, string) (Record, error)
	Save(context.Context, agentprofile.PrivateAccess, HumanDraft) (Record, error)
	Preview(context.Context, agentprofile.PrivateAccess, string, HumanPreviewInput) (HumanPreview, error)
	Accept(context.Context, agentprofile.PrivateAccess, string, string) (Record, error)
	Reject(context.Context, agentprofile.PrivateAccess, string, int64) (Record, error)
}

type HumanDraft struct {
	Category   string     `json:"category"`
	Sources    []Selector `json:"sources"`
	ValidUntil time.Time  `json:"validUntil"`
}
type HumanPreviewInput struct {
	PreviewID        string    `json:"previewId"`
	ExpectedVersion  int64     `json:"expectedVersion"`
	MemoryValidUntil time.Time `json:"memoryValidUntil"`
}
type HumanPreview struct {
	PreviewID string `json:"previewId"`
	Review    Review `json:"review"`
}
