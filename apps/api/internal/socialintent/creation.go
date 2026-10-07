package socialintent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const CreationSchema = "social-intent-creations-v1"

var ErrCreationInvalid = errors.New("invalid private intent creation")
var ErrCreationDenied = errors.New("private intent creation denied")
var ErrCreationConflict = errors.New("private intent operation content changed")
var ErrCreationUnavailable = errors.New("private intent creation unavailable")

type CreationAccess struct {
	Actor         identity.Actor
	SessionDigest [32]byte
}
type CreationReceipt struct {
	SchemaVersion string    `json:"schemaVersion"`
	OwnerID       string    `json:"ownerAccountId"`
	OperationID   string    `json:"operationId"`
	RequestDigest string    `json:"requestDigest"`
	SourceTaskID  string    `json:"sourceTaskId,omitempty"`
	Status        string    `json:"status"`
	IntentID      string    `json:"intentId,omitempty"`
	PriorIntentID string    `json:"priorIntentId,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	RecordedAt    time.Time `json:"recordedAt"`
	Intent        *Record   `json:"intent,omitempty"`
}

// TaskDraftReceipt also describes legacy source-backed creations without
// inventing an operation key for them. It is an owner-only projection.
type TaskDraftReceipt struct {
	SchemaVersion string `json:"schemaVersion"`
	OwnerID       string `json:"ownerAccountId"`
	SourceTaskID  string `json:"sourceTaskId"`
	IntentID      string `json:"intentId"`
	Intent        Record `json:"intent"`
}
type CreationGateway interface {
	CreatePrivateDraft(context.Context, CreationAccess, string, DraftInput) (CreationReceipt, bool, error)
	ReadCreation(context.Context, CreationAccess, string) (CreationReceipt, error)
	ReadTaskDraft(context.Context, CreationAccess, string) (TaskDraftReceipt, error)
}

func ValidCreationAccess(a CreationAccess) bool {
	return a.Actor.AccountType == "person" && uuid.MatchString(a.Actor.ID) && a.SessionDigest != ([32]byte{})
}

// NormalizeCreation performs static shape validation. Time eligibility is
// checked only for a new operation using the native transaction's clock;
// an immutable committed operation remains readable after its intent expires.
func NormalizeCreation(in DraftInput, source string) (DraftInput, error) {
	in.OperationID = strings.ToLower(in.OperationID)
	in.Title = strings.TrimSpace(in.Title)
	if !uuid.MatchString(in.OperationID) || len([]rune(in.Title)) < 1 || len([]rune(in.Title)) > 160 || in.ExpiresAt.Year() < 2000 || in.ExpiresAt.Year() > 2200 || in.Audience != "PRIVATE" || in.CityID != "" || in.CommunityID != "" || len(in.InviteeIDs) != 0 || (in.ContextID != "" && !uuid.MatchString(in.ContextID)) || (source != "" && (!uuid.MatchString(source) || in.Type != "FIND_ACTIVITY")) {
		return in, ErrCreationInvalid
	}
	switch in.Type {
	case "FIND_ACTIVITY", "FIND_COMPANION", "ORGANIZE", "ASK_HELP", "OTHER":
	default:
		return in, ErrCreationInvalid
	}
	c, _, e := ParseConstraints(in.Constraints, in.Modality)
	if e != nil {
		return in, ErrCreationInvalid
	}
	if c.StartsAt != nil {
		x, y := c.StartsAt.UTC().Truncate(time.Microsecond), c.EndsAt.UTC().Truncate(time.Microsecond)
		c.StartsAt, c.EndsAt = &x, &y
	}
	in.Constraints, _ = json.Marshal(c)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	return in, nil
}

// CreationDigest uses sorted JSON object keys and integer microseconds encoded
// as decimal strings. It excludes the operation key and includes the owner and
// original source, so retries cannot silently change identity or content.
func CreationDigest(owner, source string, in DraftInput) (string, error) {
	n, e := NormalizeCreation(in, source)
	if e != nil || !uuid.MatchString(owner) {
		return "", ErrCreationInvalid
	}
	c, _, _ := ParseConstraints(n.Constraints, n.Modality)
	values := map[string]any{}
	if c.Category != "" {
		values["category"] = c.Category
	}
	if c.AreaLabel != "" {
		values["areaLabel"] = c.AreaLabel
	}
	if c.OnlinePlatform != "" {
		values["onlinePlatform"] = c.OnlinePlatform
	}
	if c.PlaceID != "" {
		values["placeId"] = strings.ToLower(c.PlaceID)
	}
	if c.MinParticipants != 0 {
		values["minParticipants"] = c.MinParticipants
	}
	if c.MaxParticipants != 0 {
		values["maxParticipants"] = c.MaxParticipants
	}
	if c.StartsAt != nil {
		values["startsAtMicros"] = microString(*c.StartsAt)
		values["endsAtMicros"] = microString(*c.EndsAt)
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	e = encoder.Encode(map[string]any{"ownerAccountId": strings.ToLower(owner), "sourceTaskId": strings.ToLower(source), "draft": map[string]any{"type": n.Type, "title": n.Title, "audience": n.Audience, "modality": n.Modality, "contextId": strings.ToLower(n.ContextID), "expiresAtMicros": microString(n.ExpiresAt), "constraints": values}})
	if e != nil {
		return "", ErrCreationInvalid
	}
	digest := sha256.Sum256(bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}))
	return hex.EncodeToString(digest[:]), nil
}
func microString(t time.Time) string { return fmtMicro(t.UTC().UnixMicro()) }
func fmtMicro(n int64) string { // no floating-point precision enters the wire digest
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte(n%10) + '0'
		n /= 10
	}
	if negative {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
func ValidateCreationReceipt(r CreationReceipt, owner, operation string) error {
	if r.SchemaVersion != CreationSchema || r.OwnerID != owner || r.OperationID != operation || !uuid.MatchString(operation) || len(r.RequestDigest) != 64 || r.RecordedAt.IsZero() || (r.SourceTaskID != "" && !uuid.MatchString(r.SourceTaskID)) {
		return ErrCreationUnavailable
	}
	if _, e := hex.DecodeString(r.RequestDigest); e != nil {
		return ErrCreationUnavailable
	}
	switch r.Status {
	case "COMMITTED":
		if !uuid.MatchString(r.IntentID) || r.PriorIntentID != "" || r.Reason != "" || r.Intent == nil || r.Intent.ID != r.IntentID || r.Intent.CreatorID != owner {
			return ErrCreationUnavailable
		}
	case "NO_EFFECT":
		switch r.Reason {
		case "EXPIRED", "SOURCE_UNAVAILABLE", "SOURCE_CHANGED":
			if r.IntentID != "" || r.PriorIntentID != "" || r.Intent != nil {
				return ErrCreationUnavailable
			}
		case "SOURCE_ALREADY_EXISTS":
			if r.IntentID != "" || !uuid.MatchString(r.PriorIntentID) || r.SourceTaskID == "" || r.Intent == nil || r.Intent.ID != r.PriorIntentID || r.Intent.CreatorID != owner {
				return ErrCreationUnavailable
			}
		default:
			return ErrCreationUnavailable
		}
	default:
		return ErrCreationUnavailable
	}
	return nil
}
