package inbox

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"time"
)

var ErrNotFound = errors.New("inbox item not found")

type Item struct {
	Origin               string                  `json:"origin,omitempty"`
	HistoricalType       string                  `json:"historicalType,omitempty"`
	OriginalReadAt       *time.Time              `json:"originalReadAt,omitempty"`
	OriginalSeenAt       *time.Time              `json:"originalSeenAt,omitempty"`
	ID                   string                  `json:"id"`
	Category             string                  `json:"category"`
	Title                string                  `json:"title"`
	Detail               string                  `json:"detail"`
	ResourceType         string                  `json:"resourceType"`
	ResourceID           string                  `json:"resourceId"`
	TargetActivityID     *string                 `json:"targetActivityId,omitempty"`
	TargetConversationID *string                 `json:"targetConversationId,omitempty"`
	TargetCommunityID    *string                 `json:"targetCommunityId,omitempty"`
	TargetTaskID         *string                 `json:"targetTaskId,omitempty"`
	TargetBusinessID     *string                 `json:"targetBusinessId,omitempty"`
	CreatedAt            time.Time               `json:"createdAt"`
	ReadAt               *time.Time              `json:"readAt,omitempty"`
	SemanticCategory     string                  `json:"semanticCategory,omitempty"`
	NotificationRoute    string                  `json:"notificationRoute,omitempty"`
	NotificationPriority int                     `json:"notificationPriority,omitempty"`
	ActorID              *string                 `json:"actorId,omitempty"`
	ActorAvatar          *profileavatar.Metadata `json:"actorAvatar,omitempty"`
}

// A picture is only a current permitted projection of the original counterpart.
// It supplies neither a new recipient nor a new notification destination.
func (i Item) ValidActorMedia() bool {
	if i.ActorAvatar == nil {
		return i.ActorID == nil
	}
	return i.ActorID != nil && (i.ResourceType == "connection_request" || i.ResourceType == "conversation_message") && i.ActorAvatar.Valid() && i.ActorAvatar.AccountID == *i.ActorID
}

type Store interface {
	ListInbox(context.Context, string) ([]Item, error)
	ReadInboxItem(context.Context, string, string) (Item, error)
}
