package content

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"time"
	"unicode/utf8"
)

type MomentInteractionInput struct {
	OperationID                  string `json:"operationId"`
	Action                       string `json:"action"`
	MomentID                     string `json:"momentId"`
	ExpectedMomentRevision       int64  `json:"expectedMomentRevision"`
	ExpectedLikeVersion          int64  `json:"expectedLikeVersion"`
	CommentID                    string `json:"commentId"`
	Body                         string `json:"body"`
	ParentCommentID              string `json:"parentCommentId,omitempty"`
	ExpectedParentCommentVersion int64  `json:"expectedParentCommentVersion,omitempty"`
	SourceVersion                string `json:"sourceVersion,omitempty"`
	Approval                     string `json:"approval,omitempty"`
}

func MomentInteractionDigest(v MomentInteractionInput) string {
	facts := []any{"moment-interaction-v1", v.OperationID, v.Action, v.MomentID, v.ExpectedMomentRevision, v.ExpectedLikeVersion, v.CommentID, base64.StdEncoding.EncodeToString([]byte(v.Body))}
	if v.ParentCommentID != "" {
		facts = append(facts, "reply-v1", v.ParentCommentID, v.ExpectedParentCommentVersion)
	}
	return MomentControlHash(facts)
}
func DecodeMomentInteraction(raw []byte, commit bool) (MomentInteractionInput, error) {
	var v MomentInteractionInput
	required := []string{"operationId", "action", "momentId", "expectedMomentRevision", "expectedLikeVersion", "commentId", "body"}
	if commit {
		required = append(required, "sourceVersion", "approval")
	}
	if _, e := momentControlClosed(raw, required, []string{"parentCommentId", "expectedParentCommentVersion"}, ""); e != nil {
		return v, e
	}
	if json.Unmarshal(raw, &v) != nil {
		return v, identity.ErrControlInvalid
	}
	return NormalizeMomentInteraction(v, commit)
}
func NormalizeMomentInteraction(v MomentInteractionInput, commit bool) (MomentInteractionInput, error) {
	bad := func() (MomentInteractionInput, error) { return MomentInteractionInput{}, identity.ErrControlInvalid }
	if !identity.HumanControlUUID(v.OperationID) || !identity.HumanControlUUID(v.MomentID) || v.ExpectedMomentRevision < 2 || v.ExpectedMomentRevision > MomentControlMaxRevision || v.ExpectedLikeVersion < 0 || v.ExpectedLikeVersion >= MomentControlMaxRevision {
		return bad()
	}
	if v.ParentCommentID == "" {
		if v.ExpectedParentCommentVersion != 0 {
			return bad()
		}
	} else if v.Action != "COMMENT" || !identity.HumanControlUUID(v.ParentCommentID) || v.ExpectedParentCommentVersion < 1 || v.ExpectedParentCommentVersion > MomentControlMaxRevision {
		return bad()
	}
	switch v.Action {
	case "LIKE", "UNLIKE":
		if v.Body != "" || v.CommentID != "" {
			return bad()
		}
	case "COMMENT":
		v.Body = strings.TrimSpace(v.Body)
		if v.CommentID != "" || v.Body == "" || len(v.Body) > 2000 || !utf8.ValidString(v.Body) || strings.ContainsRune(v.Body, 0) {
			return bad()
		}
	case "DELETE_COMMENT":
		if !identity.HumanControlUUID(v.CommentID) || v.Body != "" {
			return bad()
		}
	default:
		return bad()
	}
	if commit {
		if !MomentControlHex(v.SourceVersion) || len(v.Approval) < 1 || len(v.Approval) > 8192 || !utf8.ValidString(v.Approval) {
			return bad()
		}
	} else if v.SourceVersion != "" || v.Approval != "" {
		return bad()
	}
	return v, nil
}

type MomentInteractionPreview struct {
	identity.HumanControlPreview
	Input         MomentInteractionInput `json:"input"`
	SourceVersion string                 `json:"sourceVersion"`
}
type MomentInteractionReceipt struct {
	OperationID    string     `json:"operationId"`
	Status         string     `json:"status"`
	Action         string     `json:"action,omitempty"`
	MomentID       string     `json:"momentId,omitempty"`
	CommentID      string     `json:"commentId,omitempty"`
	AppliedVersion int64      `json:"appliedVersion"`
	PayloadDigest  string     `json:"payloadDigest,omitempty"`
	CommittedAt    *time.Time `json:"committedAt,omitempty"`
}

func ValidMomentInteractionReceipt(v MomentInteractionReceipt, op string) bool {
	if !identity.HumanControlUUID(op) || v.OperationID != op {
		return false
	}
	if v.Status == "STOPPED" {
		return v.Action == "" && v.MomentID == "" && v.CommentID == "" && v.AppliedVersion == 0 && v.PayloadDigest == "" && v.CommittedAt == nil
	}
	if v.Status != "SUCCEEDED" || !identity.HumanControlUUID(v.MomentID) || v.AppliedVersion < 1 || v.AppliedVersion > MomentControlMaxRevision || !MomentControlHex(v.PayloadDigest) || v.CommittedAt == nil || v.CommittedAt.IsZero() {
		return false
	}
	switch v.Action {
	case "LIKE", "UNLIKE":
		return v.CommentID == ""
	case "COMMENT", "DELETE_COMMENT":
		return identity.HumanControlUUID(v.CommentID)
	}
	return false
}

type MomentCommentReply struct {
	CommentID           string              `json:"commentId"`
	Version             int64               `json:"version"`
	Status              string              `json:"status"`
	Body                string              `json:"body,omitempty"`
	AuthorPublicProfile *PublicMomentAuthor `json:"authorPublicProfile,omitempty"`
}
type MomentComment struct {
	ID                  string              `json:"id"`
	Body                string              `json:"body"`
	CreatedAt           time.Time           `json:"createdAt"`
	AuthorID            *string             `json:"authorId"`
	AuthorPublicProfile *PublicMomentAuthor `json:"authorPublicProfile"`
	CanDelete           bool                `json:"canDelete"`
	Version             int64               `json:"version"`
	CanReply            bool                `json:"canReply"`
	ReplyTo             *MomentCommentReply `json:"replyTo"`
}
type MomentInteractionPage struct {
	SchemaVersion  string                     `json:"schemaVersion"`
	MomentID       string                     `json:"momentId"`
	MomentRevision int64                      `json:"momentRevision"`
	CanComment     bool                       `json:"canComment"`
	LikeCount      int                        `json:"likeCount"`
	CommentCount   int                        `json:"commentCount"`
	Liked          bool                       `json:"liked"`
	LikeVersion    int64                      `json:"likeVersion"`
	Comments       []MomentComment            `json:"comments"`
	NextCursor     *string                    `json:"nextCursor"`
	ObservedAt     time.Time                  `json:"observedAt"`
	ValidUntil     time.Time                  `json:"validUntil"`
	SourceVersion  string                     `json:"sourceVersion"`
	ReadProof      string                     `json:"-"`
	ResponseProof  identity.HumanControlProof `json:"-"`
}
type MomentInteractionStore interface {
	ReadMomentInteractions(context.Context, PublicMomentAccess, string, string, string) (MomentInteractionPage, error)
	PreviewMomentInteraction(context.Context, [32]byte, identity.Actor, MomentInteractionInput) (MomentInteractionPreview, error)
	CommitMomentInteraction(context.Context, [32]byte, identity.Actor, MomentInteractionInput) (MomentInteractionReceipt, error)
	ReadMomentInteractionOperation(context.Context, [32]byte, identity.Actor, string, bool) (MomentInteractionReceipt, error)
}

// Inbox management has a separate namespace and original fact table. It never
// deletes the underlying domain object or changes its authorization.
type InboxManagementInput struct {
	OperationID   string     `json:"operationId"`
	Action        string     `json:"action"`
	ItemID        string     `json:"itemId"`
	Category      string     `json:"category"`
	UnreadOnly    bool       `json:"unreadOnly"`
	Cutoff        *time.Time `json:"cutoff,omitempty"`
	SourceVersion string     `json:"sourceVersion,omitempty"`
	Approval      string     `json:"approval,omitempty"`
}

func InboxManagementDigest(v InboxManagementInput) string {
	cutoff := ""
	if v.Cutoff != nil {
		cutoff = v.Cutoff.UTC().Format(time.RFC3339Nano)
	}
	return MomentControlHash([]any{"inbox-management-v1", v.OperationID, v.Action, v.ItemID, v.Category, v.UnreadOnly, cutoff, v.SourceVersion})
}
func ValidInboxCategory(v string) bool {
	return v == "" || v == "needs_attention" || v == "messages" || v == "requests" || v == "agent_updates" || v == "updates"
}
func DecodeInboxManagement(raw []byte, commit bool) (InboxManagementInput, error) {
	var v InboxManagementInput
	required := []string{"operationId", "action", "itemId", "category", "unreadOnly"}
	if commit {
		required = append(required, "cutoff", "sourceVersion", "approval")
	}
	if _, e := momentControlClosed(raw, required, nil, ""); e != nil {
		return v, e
	}
	if json.Unmarshal(raw, &v) != nil || !identity.HumanControlUUID(v.OperationID) || !ValidInboxCategory(v.Category) {
		return v, identity.ErrControlInvalid
	}
	switch v.Action {
	case "MARK_ALL_READ", "CLEAR_READ":
		if v.ItemID != "" {
			return v, identity.ErrControlInvalid
		}
	case "DELETE_ITEM":
		if !identity.HumanControlUUID(v.ItemID) {
			return v, identity.ErrControlInvalid
		}
	default:
		return v, identity.ErrControlInvalid
	}
	if commit {
		if v.Cutoff == nil || v.Cutoff.IsZero() || !MomentControlHex(v.SourceVersion) || v.Approval == "" || len(v.Approval) > 8192 {
			return v, identity.ErrControlInvalid
		}
	} else if v.Cutoff != nil || v.SourceVersion != "" || v.Approval != "" {
		return v, identity.ErrControlInvalid
	}
	return v, nil
}

type InboxManagementPreview struct {
	identity.HumanControlPreview
	Input         InboxManagementInput `json:"input"`
	Cutoff        time.Time            `json:"cutoff"`
	Count         int                  `json:"count"`
	SourceVersion string               `json:"sourceVersion"`
}
type InboxManagementReceipt struct {
	OperationID   string     `json:"operationId"`
	Status        string     `json:"status"`
	Action        string     `json:"action,omitempty"`
	AffectedCount int        `json:"affectedCount"`
	PayloadDigest string     `json:"payloadDigest,omitempty"`
	CommittedAt   *time.Time `json:"committedAt,omitempty"`
	UnreadCount   *int       `json:"unreadCount,omitempty"`
	Cutoff        *time.Time `json:"cutoff,omitempty"`
	SourceVersion string     `json:"sourceVersion,omitempty"`
}
type InboxManagementStore interface {
	PreviewInboxManagement(context.Context, [32]byte, identity.Actor, InboxManagementInput) (InboxManagementPreview, error)
	CommitInboxManagement(context.Context, [32]byte, identity.Actor, InboxManagementInput) (InboxManagementReceipt, error)
	ReadInboxManagementOperation(context.Context, [32]byte, identity.Actor, string, bool) (InboxManagementReceipt, error)
}
