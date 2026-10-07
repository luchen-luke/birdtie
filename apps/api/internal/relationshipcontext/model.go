package relationshipcontext

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
)

var ErrNotFound = errors.New("relationship context owner unavailable")
var ErrAgentUnavailable = errors.New("personal agent unavailable")

type Consent struct {
	Enabled bool `json:"enabled"`
}
type Peer struct {
	TieID                 string                    `json:"tieId"`
	AccountID             string                    `json:"accountId"`
	DisplayName           string                    `json:"displayName"`
	Frequency             string                    `json:"frequency"`
	SentMessages          int                       `json:"sentMessages"`
	ActiveDays            int                       `json:"activeDays"`
	InteractionCategories []string                  `json:"interactionCategories"`
	SharedActivities      []socialcontext.Reference `json:"sharedActivities"`
	ActivitiesTruncated   bool                      `json:"activitiesTruncated"`
}
type Context struct {
	Enabled    bool   `json:"enabled"`
	WindowDays int    `json:"windowDays"`
	Peers      []Peer `json:"peers"`
	Truncated  bool   `json:"truncated"`
}

func Frequency(days int) string {
	if days >= 3 {
		return "RECENT_REPEATED"
	}
	if days > 0 {
		return "RECENT"
	}
	return "NO_RECENT_DIRECT_CHAT"
}

type Store interface {
	OwnRelationshipConsent(context.Context, string) (Consent, error)
	SetRelationshipConsent(context.Context, string, Consent) (Consent, error)
	OwnRelationshipContext(context.Context, string) (Context, error)
}
