package inbox

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("inbox item not found")

type Item struct {
	ID                   string     `json:"id"`
	Category             string     `json:"category"`
	Title                string     `json:"title"`
	Detail               string     `json:"detail"`
	ResourceType         string     `json:"resourceType"`
	ResourceID           string     `json:"resourceId"`
	TargetActivityID     *string    `json:"targetActivityId,omitempty"`
	TargetConversationID *string    `json:"targetConversationId,omitempty"`
	TargetCommunityID    *string    `json:"targetCommunityId,omitempty"`
	TargetTaskID         *string    `json:"targetTaskId,omitempty"`
	TargetBusinessID     *string    `json:"targetBusinessId,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	ReadAt               *time.Time `json:"readAt,omitempty"`
	SemanticCategory     string     `json:"semanticCategory,omitempty"`
	NotificationRoute    string     `json:"notificationRoute,omitempty"`
	NotificationPriority int        `json:"notificationPriority,omitempty"`
}

type Store interface {
	ListInbox(context.Context, string) ([]Item, error)
	ReadInboxItem(context.Context, string, string) (Item, error)
}
