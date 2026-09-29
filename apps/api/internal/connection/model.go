package connection

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound  = errors.New("connection resource not found")
	ErrConflict  = errors.New("connection state conflict")
	ErrRateLimit = errors.New("connection rate limit")
	ErrForbidden = errors.New("connection forbidden")
)

type Request struct {
	ID             string    `json:"id"`
	Direction      string    `json:"direction"`
	OtherAccountID string    `json:"otherAccountId"`
	OtherName      string    `json:"otherName"`
	CityID         string    `json:"cityId"`
	Note           string    `json:"note"`
	Scope          string    `json:"scope"`
	State          string    `json:"state"`
	ConversationID string    `json:"conversationId,omitempty"`
	ExpiresAt      time.Time `json:"expiresAt"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Conversation struct {
	ID             string    `json:"id"`
	OtherAccountID string    `json:"otherAccountId"`
	OtherName      string    `json:"otherName"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId"`
	SenderID       string    `json:"senderAccountId"`
	SpeakerKind    string    `json:"speakerKind"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Store interface {
	CreateRequest(context.Context, string, string, string, string) (Request, error)
	ListRequests(context.Context, string) ([]Request, error)
	DecideRequest(context.Context, string, string, string) (Request, error)
	ListConversations(context.Context, string) ([]Conversation, error)
	ListMessages(context.Context, string, string) ([]Message, error)
	SendMessage(context.Context, string, string, string) (Message, error)
}
