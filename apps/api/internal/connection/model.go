package connection

import (
	"context"
	"errors"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"time"
)

var (
	ErrNotFound  = errors.New("connection resource not found")
	ErrConflict  = errors.New("connection state conflict")
	ErrRateLimit = errors.New("connection rate limit")
	ErrForbidden = errors.New("connection forbidden")
)

type Request struct {
	ID                string    `json:"id"`
	Direction         string    `json:"direction"`
	OtherAccountID    string    `json:"otherAccountId"`
	OtherName         string    `json:"otherName"`
	CityID            string    `json:"cityId"`
	Note              string    `json:"note"`
	Scope             string    `json:"scope"`
	State             string    `json:"state"`
	ConversationID    string    `json:"conversationId,omitempty"`
	PolicyDisposition string    `json:"policyDisposition,omitempty"`
	ScreeningStatus   string    `json:"screeningStatus,omitempty"`
	ExpiresAt         time.Time `json:"expiresAt"`
	CreatedAt         time.Time `json:"createdAt"`
}

type Conversation struct {
	ID             string     `json:"id"`
	OtherAccountID string     `json:"otherAccountId"`
	OtherName      string     `json:"otherName"`
	CreatedAt      time.Time  `json:"createdAt"`
	UnreadCount    int        `json:"unreadCount"`
	LastReadAt     *time.Time `json:"lastReadAt,omitempty"`
}

type ReadState struct {
	ConversationID    string    `json:"conversationId"`
	LastReadMessageID string    `json:"lastReadMessageId"`
	LastReadAt        time.Time `json:"lastReadAt"`
}

type Tie struct {
	ID             string    `json:"id"`
	OtherAccountID string    `json:"otherAccountId"`
	OtherName      string    `json:"otherName"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Message struct {
	ID             string      `json:"id"`
	ConversationID string      `json:"conversationId"`
	SenderID       string      `json:"senderAccountId"`
	SpeakerKind    string      `json:"speakerKind"`
	Body           string      `json:"body"`
	Entity         *EntityCard `json:"entity,omitempty"`
	CreatedAt      time.Time   `json:"createdAt"`
}

// EntityCard carries a stable reference. Title is resolved against the current
// viewer's permissions on each read, so withdrawn content is never cached here.
type EntityCard struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	Available bool   `json:"available"`
}

type Store interface {
	CreateRequest(context.Context, string, string, string, string) (Request, error)
	CreateFriendRequest(context.Context, string, string, string) (Request, error)
	ListRequests(context.Context, string) ([]Request, error)
	DecideRequest(context.Context, string, string, string) (Request, error)
	ListTies(context.Context, string) ([]Tie, error)
	RemoveTie(context.Context, string, string) error
	ListConversations(context.Context, string) ([]Conversation, error)
	StartFriendConversation(context.Context, string, string) (Conversation, error)
	ListMessages(context.Context, string, string) ([]Message, error)
	MarkRead(context.Context, string, string, string) (ReadState, error)
	SendMessage(context.Context, string, string, string) (Message, error)
	SendEntityMessage(context.Context, string, string, string, string, string) (Message, error)
}

type BoundStore interface {
	CreateRequestBound(context.Context, ea.Access, string, string, string, ea.BoundCondition) (Request, error)
	CreateFriendRequestBound(context.Context, ea.Access, string, string, ea.BoundCondition) (Request, error)
	StartFriendConversationBound(context.Context, ea.Access, string, ea.BoundCondition) (Conversation, error)
}

// CurrentStore binds an existing human action to the actual caller's session.
// A receiver routing version is a stale-source condition, never an Agent grant.
type CurrentStore interface {
	CreateRequestCurrent(context.Context, ea.Access, string, string, string, string, *ea.BoundCondition) (Request, error)
	CreateFriendRequestCurrent(context.Context, ea.Access, string, string, string, *ea.BoundCondition) (Request, error)
	DecideRequestCurrent(context.Context, ea.Access, string, string) (Request, error)
	StartFriendConversationCurrent(context.Context, ea.Access, string, *ea.BoundCondition) (Conversation, error)
	SendMessageCurrent(context.Context, ea.Access, string, string, string, string) (Message, error)
}
