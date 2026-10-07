package community

import (
	"context"
	"errors"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

var ErrUnavailable = errors.New("community current access unavailable")
var ErrApprovalRequired = errors.New("community current confirmation required")
var ErrApprovalStale = errors.New("community confirmation changed or expired")

// HumanRead and HumanCommand are ordinary PERSON social actions. They never
// grant Agent analysis, model egress, organization membership or automatic acts.
type HumanRead struct {
	Kind, CommunityID, CityID string
	Mine, Requests            bool
}
type HumanCommand struct {
	Operation, CommunityID, TargetID, Role string
	Input                                  SocialInput
	Snapshot                               string
	Preview                                bool
	ActionCondition                        *ea.BoundCondition
}
type HumanApproval struct {
	Snapshot    string    `json:"snapshot"`
	ActorID     string    `json:"actorId"`
	CommunityID string    `json:"communityId"`
	TargetID    string    `json:"targetId,omitempty"`
	Operation   string    `json:"operation"`
	Role        string    `json:"role,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
type HumanResult struct {
	Community   *SocialRecord
	Communities []SocialRecord
	Member      *Membership
	Members     []Membership
	Approval    *HumanApproval
}
type HumanSocialStore interface {
	ReadHumanCommunity(context.Context, [32]byte, identity.Actor, HumanRead) (HumanResult, error)
	MutateHumanCommunity(context.Context, [32]byte, identity.Actor, HumanCommand) (HumanResult, error)
}

func RequiresConfirmation(operation string) bool {
	switch operation {
	case "update", "archive", "invite", "approve", "reject", "role", "remove", "transfer":
		return true
	}
	return false
}
