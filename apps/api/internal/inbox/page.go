package inbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const NotificationsView = "notifications"
const DefaultPageLimit = 20
const MaxPageLimit = 50

var ErrInvalidPage = errors.New("invalid inbox page")
var ErrSourceChanged = errors.New("inbox source changed")
var pageUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type PageRequest struct {
	View, Cursor         string
	Origin               string
	Limit                int
	Category             string
	UnreadOnly           bool
	SupportsPersonFollow bool
}
type Page struct {
	Items                 []Item  `json:"items"`
	NextCursor            *string `json:"nextCursor"`
	UnreadCount           int     `json:"unreadCount"`
	HistoricalUnreadCount int     `json:"historicalUnreadCount"`
}

// Optional: original Store/ListInbox/mark-read contracts remain unchanged.
type PagedStore interface {
	ListHumanInboxPage(context.Context, [32]byte, identity.Actor, PageRequest) (Page, error)
	ValidateHumanInboxPageResponse(context.Context, [32]byte, identity.Actor, PageRequest, Page) error
}

// The closed hint only locates a row. It never supplies current authorization.
type PageCursor struct {
	Version              int       `json:"v"`
	Origin               string    `json:"origin,omitempty"`
	OwnerID              string    `json:"owner"`
	View                 string    `json:"view"`
	Priority             int       `json:"priority"`
	Time                 time.Time `json:"time"`
	ID                   string    `json:"id"`
	Category             string    `json:"category,omitempty"`
	UnreadOnly           bool      `json:"unreadOnly,omitempty"`
	SupportsPersonFollow bool      `json:"personFollow,omitempty"`
}

func ValidCategory(v string) bool {
	return v == "" || v == "needs_attention" || v == "messages" || v == "requests" || v == "agent_updates" || v == "updates"
}

func ValidPageID(id string) bool {
	return pageUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}
func ValidPagePriority(p int) bool { return p == 0 || p == 10 || p == 50 || p == 100 }

func EncodePageCursor(c PageCursor) (string, error) {
	if c.Version != 1 || (c.Origin != "" && c.Origin != "LEGACY_HISTORY") || !ValidCategory(c.Category) || !ValidPageID(c.OwnerID) || !ValidPageID(c.ID) || c.View != NotificationsView || !ValidPagePriority(c.Priority) || c.Time.IsZero() || c.Time.Year() < 1 || c.Time.Year() > 9999 || c.Time.Location() != time.UTC {
		return "", ErrInvalidPage
	}
	b, e := json.Marshal(c)
	if e != nil {
		return "", ErrInvalidPage
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func DecodePageCursor(raw, owner, view string) (*PageCursor, error) {
	if !ValidPageID(owner) || view != NotificationsView {
		return nil, ErrInvalidPage
	}
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 1024 {
		return nil, ErrInvalidPage
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		return nil, ErrInvalidPage
	}
	var c PageCursor
	if json.Unmarshal(b, &c) != nil || c.OwnerID != owner || c.View != view {
		return nil, ErrInvalidPage
	}
	encoded, e := EncodePageCursor(c)
	if e != nil || encoded != raw {
		return nil, ErrInvalidPage
	}
	return &c, nil
}

func NormalizePage(in PageRequest, owner string) (PageRequest, *PageCursor, error) {
	if in.Origin == "NATIVE" {
		in.Origin = ""
	}
	if in.Origin != "" && in.Origin != "LEGACY_HISTORY" {
		return in, nil, ErrInvalidPage
	}
	if in.Limit == 0 {
		in.Limit = DefaultPageLimit
	}
	if in.Limit < 1 || in.Limit > MaxPageLimit || !ValidCategory(in.Category) {
		return in, nil, ErrInvalidPage
	}
	c, e := DecodePageCursor(in.Cursor, owner, in.View)
	if e == nil && c != nil && (c.Category != in.Category || c.UnreadOnly != in.UnreadOnly || c.Origin != in.Origin || c.SupportsPersonFollow != in.SupportsPersonFollow) {
		return in, nil, ErrInvalidPage
	}
	return in, c, e
}
