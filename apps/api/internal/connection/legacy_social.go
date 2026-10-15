package connection

import (
	"context"
	"encoding/json"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

const LegacySocialOrigin = "LEGACY_IMPORT"

type Sticker struct {
	PackID    string `json:"packId"`
	StickerID string `json:"stickerId"`
}

// ImportedImageContentStore reads a current historical attachment without
// inventing a send-operation ID or a successful human send receipt.
type ImportedImageContentStore interface {
	ReadImportedImageMessageContent(context.Context, ea.Access, string, string, string, func(ImageDerivativeMetadata, []byte) ([]byte, error)) ([]byte, bool, error)
}

func ValidConversationPeer(v Conversation, owner string) bool {
	if v.Origin == LegacySocialOrigin && v.Kind == "PERSONAL_GROUP" {
		return v.OtherAccountID == "" && v.Avatar == nil && v.CanSend != nil && !*v.CanSend
	}
	return ValidChatPageID(v.OtherAccountID) && v.OtherAccountID != owner
}
func MessageBefore(a, b Message) bool {
	if a.Sequence > 0 || b.Sequence > 0 {
		return a.Sequence > 0 && b.Sequence > 0 && a.Sequence < b.Sequence
	}
	return a.CreatedAt.Before(b.CreatedAt) || a.CreatedAt.Equal(b.CreatedAt) && a.ID < b.ID
}

// Unknown source expiry is encoded as null, rather than the zero Go clock or
// an invented deadline. The native request type keeps its stable old API.
func (r Request) MarshalJSON() ([]byte, error) {
	type alias Request
	if r.Origin != LegacySocialOrigin {
		return json.Marshal(alias(r))
	}
	var expiry any
	if !r.ExpiresAt.IsZero() {
		expiry = r.ExpiresAt
	}
	return json.Marshal(struct {
		alias
		Expiry any `json:"expiresAt"`
	}{alias: alias(r), Expiry: expiry})
}
