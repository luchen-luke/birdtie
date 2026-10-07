package activitychat

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("activity conversation not available")
var ErrClosed = errors.New("activity conversation read only")
var ErrConflict = errors.New("activity message retry conflict")
var ErrRateLimited = errors.New("activity messages rate limited")
var ErrForbidden = errors.New("activity message removal forbidden")

type State struct {
	ID              string `json:"id"`
	ActivityID      string `json:"activityId"`
	ViewerAccountID string `json:"viewerAccountId"`
	Joined          bool   `json:"joined"`
	CanJoin         bool   `json:"canJoin"`
	CanSend         bool   `json:"canSend"`
	Moderator       bool   `json:"moderator"`
}
type Message struct {
	ID              string    `json:"id"`
	SenderAccountID string    `json:"senderAccountId"`
	SenderName      string    `json:"senderName"`
	Body            string    `json:"body"`
	CreatedAt       time.Time `json:"createdAt"`
	Removed         bool      `json:"removed"`
}
type Page struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"hasMore"`
}
type Store interface {
	ActivityChatState(context.Context, string, string) (State, error)
	JoinActivityChat(context.Context, string, string) (State, error)
	LeaveActivityChat(context.Context, string, string) error
	ActivityChatMessages(context.Context, string, string, string) (Page, error)
	SendActivityChatMessage(context.Context, string, string, string, string) (Message, error)
	RemoveActivityChatMessage(context.Context, string, string, string) error
}
