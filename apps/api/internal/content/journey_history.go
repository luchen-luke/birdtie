package content

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"time"
)

const JourneyHistorySchema = "owned-journey-history-v1"

type JourneyHistoryQuery struct {
	JourneyID, ThreadID, Cursor string
	Preferences                 bool
}
type JourneyHistoryThread struct {
	ID                    string    `json:"id"`
	JourneyID             string    `json:"journeyId"`
	ContextKind           string    `json:"contextKind"`
	Summary               string    `json:"summary"`
	SourceStatus          string    `json:"sourceStatus"`
	SourceContextRevision int64     `json:"sourceContextRevision"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}
type JourneyHistoryMessage struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}
type OriginalNotificationPreferences struct {
	SourceVersion     int64           `json:"sourceVersion"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	Settings          map[string]bool `json:"settings"`
	CollaborationPush *bool           `json:"collaborationPush"`
	TimeZone          string          `json:"timeZone"`
	QuietStart        string          `json:"quietStart"`
	QuietEnd          string          `json:"quietEnd"`
}
type JourneyHistoryPage struct {
	SchemaVersion string                           `json:"schemaVersion"`
	Threads       []JourneyHistoryThread           `json:"threads"`
	Messages      []JourneyHistoryMessage          `json:"messages"`
	Preferences   *OriginalNotificationPreferences `json:"preferences"`
	ObservedAt    time.Time                        `json:"observedAt"`
	ValidUntil    time.Time                        `json:"validUntil"`
	SourceVersion string                           `json:"sourceVersion"`
	NextCursor    *string                          `json:"nextCursor"`
	ResponseProof identity.HumanControlProof       `json:"-"`
}
type JourneyHistoryStore interface {
	ReadJourneyHistory(context.Context, [32]byte, identity.Actor, JourneyHistoryQuery) (JourneyHistoryPage, error)
}
