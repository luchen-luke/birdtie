package communitychat

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
)

var ErrNotFound = errors.New("community conversation not available")
var ErrConflict = errors.New("community message retry conflict")
var ErrRateLimited = errors.New("community messages rate limited")
var ErrForbidden = errors.New("community message removal forbidden")

type State struct {
	ID              string `json:"id"`
	CommunityID     string `json:"communityId"`
	ViewerAccountID string `json:"viewerAccountId"`
	Joined          bool   `json:"joined"`
	CanJoin         bool   `json:"canJoin"`
	CanSend         bool   `json:"canSend"`
	Moderator       bool   `json:"moderator"`
}

// Message payload is shared; authorization and durable membership stay separate.
type Message = activitychat.Message
type Page = activitychat.Page
type Store interface {
	CommunityChatState(context.Context, string, string) (State, error)
	JoinCommunityChat(context.Context, string, string) (State, error)
	LeaveCommunityChat(context.Context, string, string) error
	CommunityChatMessages(context.Context, string, string, string) (Page, error)
	SendCommunityChatMessage(context.Context, string, string, string, string) (Message, error)
	RemoveCommunityChatMessage(context.Context, string, string, string) error
}
