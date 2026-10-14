package inbox

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const ReviewResultSchema = "inbox-review-result-v1"

// ReviewResult exposes a current review fact, never the private submitted
// material. TargetID exists only when its independent current ACL allows it.
type ReviewResult struct {
	SchemaVersion   string                     `json:"schemaVersion"`
	ItemID          string                     `json:"itemId"`
	Kind            string                     `json:"kind"`
	Status          string                     `json:"status"`
	TargetID        *string                    `json:"targetId,omitempty"`
	TargetAvailable bool                       `json:"targetAvailable"`
	SourceStamp     string                     `json:"-"`
	Session         identity.HumanControlFrame `json:"-"`
}

func (v ReviewResult) Valid(id string) bool {
	return v.SchemaVersion == ReviewResultSchema && v.ItemID == id && ValidPageID(id) &&
		(v.Kind == "activity_candidate" && (v.Status == "published" || v.Status == "rejected") ||
			v.Kind == "place_candidate" && (v.Status == "published" || v.Status == "linked_duplicate" || v.Status == "rejected")) &&
		v.TargetAvailable == (v.TargetID != nil) &&
		(v.TargetID == nil || ValidPageID(*v.TargetID) && v.Status != "rejected")
}

type HumanReviewResultStore interface {
	ReadHumanInboxReviewResult(context.Context, [32]byte, identity.Actor, string) (ReviewResult, error)
	ValidateHumanInboxReviewResultResponse(context.Context, [32]byte, identity.Actor, ReviewResult) error
}
