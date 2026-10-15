// Package legacysocial projects a sealed preservation supplement into native
// social facts. A projection is neither a new acceptance nor authentication.
package legacysocial

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
)

const Protocol = "birdtie-civu-social-source-v1"
const ManifestProtocol = "birdtie-civu-social-preservation-manifest-v1"
const SourceSchemaSHA256 = "0fa21135198733153baa2d9a3f3aa09c85769ca3bbddc99dd9db8c7b247d3aff"
const MaxInputBytes = 16 << 20

var ErrInvalid = errors.New("invalid legacy social supplement")

type Fact struct {
	SourceKey, SourceSHA256, TargetID string
}
type Friendship struct {
	Fact
	LowID, HighID, RequesterID, State string
	CreatedAt, UpdatedAt              time.Time
	RespondedAt                       *time.Time
}
type Follow struct {
	Fact
	FollowerID, FolloweeID string
	CreatedAt              time.Time
}
type Conversation struct {
	Fact
	Kind, LowID, HighID, Title, State, JoinPolicy string
	JourneyID, OwnerID, SchoolLabel               string
	ConsentEpoch, LastSequence, MemberLimit       int64
	CreatedAt, UpdatedAt                          time.Time
	ArchivedAt                                    *time.Time
	ShowOnProfile                                 bool
	HistoryForNewMembers                          bool
	// AcceptanceSource is an exact historical source key, never inferred from
	// active status, mutual follows, a greeting or the presence of messages.
	AcceptanceSource string
}
type Member struct {
	Fact
	ConversationID, OwnerID, Role                                 string
	ReadSequence, DeliveredSequence, UnreadCount, ClearedSequence int64
	JoinedAt, UpdatedAt                                           time.Time
	MutedUntil, RemovedAt                                         *time.Time
	Pinned, Hidden, MarkedUnread                                  bool
}
type Message struct {
	Fact
	ConversationID, SenderID, Kind, Body, EntityKind, EntitySourceID string
	EntityTargetID, ContextKind, ContextID, RequestID                string
	ClientMessageID, PayloadHash, StickerPackID, StickerID           string
	Sequence                                                         int64
	CreatedAt                                                        time.Time
	RecalledAt, ModeratedAt                                          *time.Time
}
type Request struct {
	Fact
	ConversationID, RequesterID, RecipientID, State, FirstMessageID string
	Epoch                                                           int64
	CreatedAt                                                       time.Time
	SeenAt, ResolvedAt, RetryAfter                                  *time.Time
}
type Image struct {
	Fact
	MessageID, ConversationID, OwnerID, StorageKey, AttachmentSHA256 string
	State, Kind, FileName                                            string
	Width, Height, ByteSize, Position                                int64
	CreatedAt                                                        time.Time
	ReadyAt                                                          *time.Time
	DurationMillis                                                   *int64
}
type Counts struct {
	Friendships     int `json:"friendships"`
	AcceptedFriends int `json:"acceptedFriends"`
	PendingFriends  int `json:"pendingFriends"`
	Follows         int `json:"follows"`
	Conversations   int `json:"conversations"`
	Members         int `json:"members"`
	Messages        int `json:"messages"`
	MessageRequests int `json:"messageRequests"`
	Images          int `json:"images"`
}
type Plan struct {
	RunID, SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, ProjectionSHA256 string
	Original                                                                      legacymigration.Plan `json:"-"`
	Friendships                                                                   []Friendship         `json:"-"`
	Follows                                                                       []Follow             `json:"-"`
	Conversations                                                                 []Conversation       `json:"-"`
	Members                                                                       []Member             `json:"-"`
	Messages                                                                      []Message            `json:"-"`
	Requests                                                                      []Request            `json:"-"`
	Images                                                                        []Image              `json:"-"`
	Counts                                                                        Counts
}

func (Plan) String() string            { return "private legacy social projection" }
func SHA(raw []byte) string            { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func TargetID(kind, key string) string { return legacynative.StableTargetID("social:"+kind, "", key) }
