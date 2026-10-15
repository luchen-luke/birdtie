// Package legacyhistory preserves private original history without model or delivery effects.
package legacyhistory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
	"time"
)

const Protocol = "birdtie-civu-social-history-source-v1"
const ManifestProtocol = "birdtie-civu-social-history-preservation-manifest-v1"
const SourceSchemaSHA256 = "88cea064b7c88a39457730631f73d30c4d71067d2e2d845925fb0ba08eef05e0"
const MaxInputBytes = 16 << 20

var ErrInvalid = errors.New("legacy_history_source_invalid")

type Fact struct{ SourceKey, SourceSHA256, TargetID string }
type Thread struct {
	Fact
	OwnerSourceID, JourneySourceID, JourneyTargetID, DraftContextID, Summary, SourceStatus string
	ContextRevision                                                                        int64
	CreatedAt, UpdatedAt                                                                   time.Time
}
type Message struct {
	Fact
	ThreadSourceID, Role, Body, RequestID string
	CreatedAt                             time.Time
}
type Notification struct {
	Fact
	OwnerSourceID, ActorSourceID, OriginalType, EventType, Category, TargetKind, TargetSourceID, NativeTargetKind, NativeTargetID, AnchorType, AnchorID, Action string
	CreatedAt                                                                                                                                                   time.Time
	ReadAt, SeenAt, DeletedAt                                                                                                                                   *time.Time
	PayloadSchemaVersion, InboxVersion                                                                                                                          int64
	ResidualKeys                                                                                                                                                []string
}
type Preferences struct {
	Fact
	OwnerSourceID                  string
	Settings                       map[string]bool
	CollaborationPush              *bool
	TimeZone, QuietStart, QuietEnd string
	Version                        int64
	UpdatedAt                      time.Time
}
type Counts struct {
	Threads              int `json:"threads"`
	Messages             int `json:"messages"`
	Notifications        int `json:"notifications"`
	DeletedNotifications int `json:"deletedNotifications"`
	HistoricalUnread     int `json:"historicalUnread"`
	Preferences          int `json:"preferences"`
}
type Plan struct {
	RunID, SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, SocialSourceFileSHA256, SocialManifestFileSHA256, SocialSnapshotSHA256, ProjectionSHA256 string
	Original                                                                                                                                              legacymigration.Plan
	Threads                                                                                                                                               []Thread
	Messages                                                                                                                                              []Message
	Notifications                                                                                                                                         []Notification
	Preferences                                                                                                                                           []Preferences
	Counts                                                                                                                                                Counts
}

func (Plan) String() string { return "private original history plan; NOT_IMPORTED" }
func TargetID(kind, key string) string {
	return legacynative.StableTargetID("history-v1-"+kind, "", key)
}
func SHA(v []byte) string { d := sha256.Sum256(v); return hex.EncodeToString(d[:]) }
