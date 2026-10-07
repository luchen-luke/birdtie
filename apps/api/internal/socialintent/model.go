package socialintent

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("social intent not found")
var ErrInvalidAudience = errors.New("invalid social intent audience")
var ErrConflict = errors.New("social intent transition conflict")

// DraftInput is an owner-only declaration. Audience and modality are recorded
// now; discovery, publication and lifecycle transitions have separate gates.
type DraftInput struct {
	OperationID string          `json:"operationId,omitempty"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Constraints json.RawMessage `json:"constraints"`
	Audience    string          `json:"audience"`
	Modality    string          `json:"modality"`
	ContextID   string          `json:"contextId,omitempty"`
	CityID      string          `json:"cityId,omitempty"`
	CommunityID string          `json:"communityId,omitempty"`
	InviteeIDs  []string        `json:"inviteeAccountIds,omitempty"`
	ExpiresAt   time.Time       `json:"expiresAt"`
}

type Record struct {
	ConvertedActivityID      *string         `json:"convertedActivityId,omitempty"`
	ConvertedParticipationID *string         `json:"convertedParticipationId,omitempty"`
	ConvertedAt              *time.Time      `json:"convertedAt,omitempty"`
	ID                       string          `json:"id"`
	CreatorID                string          `json:"creatorAccountId"`
	Type                     string          `json:"type"`
	Title                    string          `json:"title"`
	Constraints              json.RawMessage `json:"constraints"`
	Audience                 string          `json:"audience"`
	Modality                 string          `json:"modality"`
	ContextID                *string         `json:"contextId,omitempty"`
	CityID                   string          `json:"cityId,omitempty"`
	CommunityID              string          `json:"communityId,omitempty"`
	InviteeIDs               []string        `json:"inviteeAccountIds,omitempty"`
	Status                   string          `json:"status"`
	ExpiresAt                time.Time       `json:"expiresAt"`
	CreatedAt                time.Time       `json:"createdAt"`
	UpdatedAt                time.Time       `json:"updatedAt"`
}

type Store interface {
	CreateSocialIntentDraft(context.Context, string, DraftInput) (Record, error)
	CreateSocialIntentDraftFromTask(context.Context, string, string, DraftInput) (Record, error)
	ListOwnSocialIntents(context.Context, string) ([]Record, error)
	GetOwnSocialIntent(context.Context, string, string) (Record, error)
	ListVisibleSocialIntents(context.Context, string) ([]Record, error)
	GetVisibleSocialIntent(context.Context, string, string) (Record, error)
	ActivateSocialIntent(context.Context, string, string) (Record, error)
	CancelSocialIntent(context.Context, string, string) (Record, error)
}
