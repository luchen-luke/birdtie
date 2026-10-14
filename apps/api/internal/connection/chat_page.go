package connection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

const DefaultChatPageLimit = 20
const MaxChatPageLimit = 50

var ErrInvalidPage = errors.New("invalid chat page")
var chatPageUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type PageRequest struct {
	Cursor string
	Limit  int
}

type ConversationPage struct {
	Items       []Conversation `json:"items"`
	NextCursor  *string        `json:"nextCursor"`
	UnreadCount int            `json:"unreadCount"`
}

type MessagePage struct {
	Items      []Message `json:"items"`
	NextCursor *string   `json:"nextCursor"`
}

// PagedStore is optional: the original Store and endpoints retain their DTOs.
// Every page and final response gate uses the actual captured caller/session.
type PagedStore interface {
	ListConversationsPage(context.Context, ea.Access, PageRequest) (ConversationPage, error)
	ListMessagesPage(context.Context, ea.Access, string, PageRequest) (MessagePage, error)
	ConversationCurrent(context.Context, ea.Access, string) (Conversation, error)
	// expected is the exact DTO already encoded by the HTTP adapter. The native
	// gate rechecks sensitive projections as well as membership after encoding.
	ValidateChatPageResponse(context.Context, ea.Access, []string, any) error
}

// PageCursor is a closed, versioned location hint, never an authorization grant.
// All current visibility checks must run even when the hint is well formed.
type PageCursor struct {
	Version        int       `json:"v"`
	OwnerID        string    `json:"owner"`
	ConversationID string    `json:"conversation"`
	Time           time.Time `json:"time"`
	ID             string    `json:"id"`
}

func ValidChatPageID(id string) bool {
	return chatPageUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

func EncodePageCursor(c PageCursor) (string, error) {
	if c.Version != 1 || !ValidChatPageID(c.OwnerID) || !ValidChatPageID(c.ID) ||
		(c.ConversationID != "" && !ValidChatPageID(c.ConversationID)) || c.Time.IsZero() ||
		c.Time.Year() < 1 || c.Time.Year() > 9999 || c.Time.Location() != time.UTC {
		return "", ErrInvalidPage
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", ErrInvalidPage
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func DecodePageCursor(raw, owner, conversation string) (*PageCursor, error) {
	if !ValidChatPageID(owner) || (conversation != "" && !ValidChatPageID(conversation)) {
		return nil, ErrInvalidPage
	}
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 1024 {
		return nil, ErrInvalidPage
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrInvalidPage
	}
	var c PageCursor
	if json.Unmarshal(b, &c) != nil || c.OwnerID != owner || c.ConversationID != conversation {
		return nil, ErrInvalidPage
	}
	// A canonical round trip rejects unknown/duplicate fields, alternate encodings,
	// trailing data and non-UTC timestamps. Only our exact emitted hints are used.
	encoded, err := EncodePageCursor(c)
	if err != nil || encoded != raw {
		return nil, ErrInvalidPage
	}
	return &c, nil
}

func NormalizeChatPage(in PageRequest, owner, conversation string) (PageRequest, *PageCursor, error) {
	if in.Limit == 0 {
		in.Limit = DefaultChatPageLimit
	}
	if in.Limit < 1 || in.Limit > MaxChatPageLimit {
		return in, nil, ErrInvalidPage
	}
	c, err := DecodePageCursor(in.Cursor, owner, conversation)
	return in, c, err
}
