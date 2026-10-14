package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const TaskImagePurpose = "HUMAN_TASK_MESSAGE_ATTACHMENT"

// A read-only disposition for an existing preview whose original upload proof
// no longer holds. It is never stored as an image state or accepted by PUT.
const TaskImageInvalidatedPreview = "invalidated_preview"

// Human-only association to an existing original Task message. It is not an
// Agent context export, publication, Memory write or permission to read pixels.
type TaskImageInput struct {
	OperationID   string    `json:"operationId"`
	MessageIndex  int       `json:"messageIndex"`
	PrefixDigest  string    `json:"prefixDigest"`
	TaskUpdatedAt time.Time `json:"expectedTaskUpdatedAt"`
	MIME          string    `json:"mimeType"`
	ByteSize      int64     `json:"byteSize"`
	SHA256        string    `json:"sha256"`
	PixelRisk     string    `json:"pixelRisk"`
	Purpose       string    `json:"purpose"`
}
type TaskImageReceipt struct {
	ID               string    `json:"id"`
	OperationID      string    `json:"operationId"`
	TaskID           string    `json:"taskId"`
	OwnerID          string    `json:"ownerAccountId"`
	MessageIndex     int       `json:"messageIndex"`
	PrefixDigest     string    `json:"prefixDigest"`
	TaskUpdatedAt    time.Time `json:"expectedTaskUpdatedAt"`
	MIME             string    `json:"mimeType"`
	InputSize        int64     `json:"inputSize"`
	ByteSize         int64     `json:"byteSize"`
	InputSHA256      string    `json:"inputSha256"`
	SHA256           string    `json:"sha256"`
	PixelRisk        string    `json:"pixelRisk"`
	Purpose          string    `json:"purpose"`
	Status           string    `json:"status"`
	Revision         int64     `json:"revision"`
	PreviewExpiresAt time.Time `json:"previewExpiresAt"`
	RetainUntil      time.Time `json:"retainUntil"`
}
type TaskImageEncode func([]TaskImageReceipt, []byte) ([]byte, error)
type HumanTaskImageStore interface {
	PreviewHumanTaskImage(context.Context, [32]byte, identity.Actor, string, TaskImageInput) (TaskImageReceipt, error)
	SaveHumanTaskImage(context.Context, [32]byte, identity.Actor, string, string, string, []byte) (TaskImageReceipt, error)
	ReadHumanTaskImages(context.Context, [32]byte, identity.Actor, string, string, bool, bool, TaskImageEncode) ([]byte, error)
	DeleteHumanTaskImage(context.Context, [32]byte, identity.Actor, string, string, int64) error
}

func ValidateTaskImageInput(p TaskImageInput) error {
	if !ValidPrivateImageID(p.OperationID) || p.MessageIndex < 0 || p.MessageIndex > 255 ||
		!privateImageSHA.MatchString(p.PrefixDigest) || p.TaskUpdatedAt.IsZero() || p.TaskUpdatedAt.Year() < 2020 || p.TaskUpdatedAt.Year() > 9999 ||
		(p.MIME != "image/png" && p.MIME != "image/jpeg") || p.ByteSize < 1 || p.ByteSize > MaxImageInputBytes ||
		!privateImageSHA.MatchString(p.SHA256) || (p.PixelRisk != "UNKNOWN" && p.PixelRisk != "USER_MASKED") || p.Purpose != TaskImagePurpose {
		return content.ErrInvalid
	}
	return nil
}

// Separate user-prefix validation; the existing assistant ReplyTurnDigest
// semantics and original conversation wire remain unchanged.
func TaskImageMessageDigest(t agentworkspace.Task, index int) (string, error) {
	if index < 0 || index >= len(t.Conversation) || index > 255 || t.Conversation[index].Role != "user" {
		return "", content.ErrInvalid
	}
	type turn struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	prefix := make([]turn, 0, index+1)
	for _, m := range t.Conversation[:index+1] {
		prefix = append(prefix, turn{m.Role, m.Text})
	}
	raw, e := json.Marshal(prefix)
	if e != nil {
		return "", content.ErrInvalid
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}
func TaskImageMatches(t agentworkspace.Task, p TaskImageInput, latest bool) error {
	if t.PrincipalType != "person" || t.PrincipalID == "" || t.ActingUserID != t.PrincipalID || t.ContextType != "CITY" || t.CityID == "" || !ValidPrivateImageID(t.ContextID) {
		return content.ErrNotFound
	}
	digest, e := TaskImageMessageDigest(t, p.MessageIndex)
	if e != nil || digest != p.PrefixDigest {
		return content.ErrConflict
	}
	if latest {
		last := -1
		for i, m := range t.Conversation {
			if m.Role == "user" {
				last = i
			}
		}
		if t.Status != agentworkspace.TaskCompleted || last != p.MessageIndex || !t.UpdatedAt.Equal(p.TaskUpdatedAt) {
			return content.ErrConflict
		}
	}
	return nil
}
