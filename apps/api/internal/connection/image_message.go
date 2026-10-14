package connection

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

const ImageMessageOperationSchema = "human.image_message_operation.v1"
const ConversationImageSchema = "human.conversation_image.v1"
const ChatImagePurpose = "HUMAN_CONVERSATION_IMAGE_ATTACHMENT"
const MaxChatImageInputBytes = 10485760
const MaxChatImageFullBytes = 16777216
const MaxChatImageThumbnailBytes = 1048576

// 2000 caption UTF8 bytes can occupy 12000 JSON escape bytes. Reserve bounded
// envelope space for that legitimate input; pixel/content limits do not grow.
const MaxChatImageJSONBytes = ((MaxChatImageInputBytes + 2) / 3 * 4) + 16384

var ErrInvalidImage = errors.New("invalid human conversation image")

// These are human conversation metadata, never an Agent or public-image grant.
type ImageDerivativeMetadata struct {
	MIME     string `json:"mimeType"`
	ByteSize int64  `json:"byteSize"`
	SHA256   string `json:"sha256"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}
type ImageMetadata struct {
	Schema string `json:"schemaVersion"`
	ImageDerivativeMetadata
	Thumbnail ImageDerivativeMetadata `json:"thumbnail"`
}
type ImageMessageInput struct {
	OperationID        string `json:"operationId"`
	RecipientAccountID string `json:"recipientAccountId"`
	Purpose            string `json:"purpose"`
	Caption            string `json:"caption"`
	PixelRisk          string `json:"pixelRisk"`
	MIME               string `json:"mimeType"`
	InputSize          int64  `json:"inputSize"`
	InputSHA256        string `json:"inputSha256"`
	Content            []byte `json:"-"`
}
type ImageMessageReceipt struct {
	Schema             string        `json:"schemaVersion"`
	OwnerID            string        `json:"ownerId"`
	ConversationID     string        `json:"conversationId"`
	RecipientAccountID string        `json:"recipientAccountId"`
	OperationID        string        `json:"operationId"`
	EffectKey          string        `json:"effectKey"`
	PayloadDigest      string        `json:"payloadDigest"`
	MessageID          string        `json:"messageId"`
	InputMIME          string        `json:"inputMimeType"`
	InputSize          int64         `json:"inputSize"`
	InputSHA256        string        `json:"inputSha256"`
	PixelRisk          string        `json:"pixelRisk"`
	CaptionSHA256      string        `json:"captionSha256"`
	Purpose            string        `json:"purpose"`
	CreatedAt          time.Time     `json:"createdAt"`
	Image              ImageMetadata `json:"image"`
}
type ImageContentEncode func(ImageMessageReceipt, ImageDerivativeMetadata, []byte) ([]byte, error)
type HumanImageMessageStore interface {
	SendHumanImageMessage(context.Context, ea.Access, string, ImageMessageInput) (ImageMessageReceipt, error)
	ReadHumanImageMessageOperation(context.Context, ea.Access, string, string) (ImageMessageReceipt, error)
	ValidateHumanImageMessageResponse(context.Context, ea.Access, ImageMessageReceipt) error
	ReadHumanImageMessageContent(context.Context, ea.Access, string, string, string, ImageContentEncode) ([]byte, error)
}

func ValidChatImageVariant(v string) bool { return v == "thumbnail" || v == "full" }
func validChatImageHash(v string) bool    { return len(v) == 64 && isMessageHex(v) }
func ValidImageApproval(v ImageMessageInput) bool {
	return shareUUID.MatchString(v.OperationID) && shareUUID.MatchString(v.RecipientAccountID) &&
		v.Purpose == ChatImagePurpose && utf8.ValidString(v.Caption) && len(v.Caption) <= 2000 && !strings.ContainsRune(v.Caption, 0) &&
		(v.PixelRisk == "UNKNOWN" || v.PixelRisk == "USER_MASKED") &&
		(v.MIME == "image/png" || v.MIME == "image/jpeg") && v.InputSize > 0 && v.InputSize <= MaxChatImageInputBytes && validChatImageHash(v.InputSHA256)
}
func ValidateImageMessage(v ImageMessageInput) error {
	if !ValidImageApproval(v) || int64(len(v.Content)) != v.InputSize {
		return ErrConflict
	}
	h := sha256.Sum256(v.Content)
	if hex.EncodeToString(h[:]) != v.InputSHA256 {
		return ErrConflict
	}
	return nil
}
func DecodeImageMessage(raw []byte) (ImageMessageInput, error) {
	var v ImageMessageInput
	if len(raw) > MaxChatImageJSONBytes || !utf8.Valid(raw) {
		return v, ErrConflict
	}
	f, e := shareObject(raw, "operationId", "recipientAccountId", "purpose", "caption", "pixelRisk", "mimeType", "inputSize", "inputSha256", "contentBase64")
	if e != nil {
		return v, e
	}
	var encoded string
	for name, dst := range map[string]any{"operationId": &v.OperationID, "recipientAccountId": &v.RecipientAccountID, "purpose": &v.Purpose, "caption": &v.Caption, "pixelRisk": &v.PixelRisk, "mimeType": &v.MIME, "inputSize": &v.InputSize, "inputSha256": &v.InputSHA256, "contentBase64": &encoded} {
		if json.Unmarshal(f[name], dst) != nil {
			return ImageMessageInput{}, ErrConflict
		}
	}
	if !ValidImageApproval(v) || len(encoded) > base64.StdEncoding.EncodedLen(MaxChatImageInputBytes) {
		return ImageMessageInput{}, ErrConflict
	}
	v.Content, e = base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || base64.StdEncoding.EncodeToString(v.Content) != encoded || ValidateImageMessage(v) != nil {
		return ImageMessageInput{}, ErrConflict
	}
	return v, nil
}
func ImageMessagePayloadDigest(conversation string, v ImageMessageInput) string {
	return messageHash("birdtie.human-image-payload.v1\n" + conversation + "\n" + v.RecipientAccountID + "\n" + v.Purpose + "\n" + v.MIME + "\n" + strconv.FormatInt(v.InputSize, 10) + "\n" + v.InputSHA256 + "\n" + v.PixelRisk + "\n" + strconv.Itoa(len([]byte(v.Caption))) + "\n" + v.Caption)
}
func ImageThumbnailDimensions(w, h int) (int, int) {
	if w < 1 || h < 1 {
		return 0, 0
	}
	if w <= 256 && h <= 256 {
		return w, h
	}
	if w >= h {
		return 256, max(1, (h*256+w/2)/w)
	}
	return max(1, (w*256+h/2)/h), 256
}
func ValidImageDerivative(m ImageDerivativeMetadata, thumbnail bool) bool {
	bound, side := int64(MaxChatImageFullBytes), 8192
	if thumbnail {
		bound, side = MaxChatImageThumbnailBytes, 256
	}
	return (m.MIME == "image/png" || (!thumbnail && m.MIME == "image/jpeg")) && m.ByteSize > 0 && m.ByteSize <= bound && validChatImageHash(m.SHA256) &&
		m.Width > 0 && m.Height > 0 && m.Width <= side && m.Height <= side && int64(m.Width)*int64(m.Height) <= MaxChatImageFullBytes
}
func ValidImageMetadata(m ImageMetadata) bool {
	if m.Schema != ConversationImageSchema || !ValidImageDerivative(m.ImageDerivativeMetadata, false) || !ValidImageDerivative(m.Thumbnail, true) {
		return false
	}
	w, h := ImageThumbnailDimensions(m.Width, m.Height)
	return m.Thumbnail.Width == w && m.Thumbnail.Height == h
}
func ImageVariantMetadata(m ImageMetadata, variant string) (ImageDerivativeMetadata, bool) {
	if !ValidImageMetadata(m) || !ValidChatImageVariant(variant) {
		return ImageDerivativeMetadata{}, false
	}
	if variant == "thumbnail" {
		return m.Thumbnail, true
	}
	return m.ImageDerivativeMetadata, true
}
func ValidImageContent(m ImageDerivativeMetadata, raw []byte, thumbnail bool) bool {
	if !ValidImageDerivative(m, thumbnail) || int64(len(raw)) != m.ByteSize {
		return false
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]) == m.SHA256
}
func NewImageMessageReceipt(owner, conversation, message string, v ImageMessageInput, image ImageMetadata, at time.Time) ImageMessageReceipt {
	return ImageMessageReceipt{Schema: ImageMessageOperationSchema, OwnerID: owner, ConversationID: conversation, RecipientAccountID: v.RecipientAccountID,
		OperationID: v.OperationID, EffectKey: HumanMessageEffectKey(owner, v.OperationID), PayloadDigest: ImageMessagePayloadDigest(conversation, v), MessageID: message,
		InputMIME: v.MIME, InputSize: v.InputSize, InputSHA256: v.InputSHA256, PixelRisk: v.PixelRisk, CaptionSHA256: messageHash(v.Caption), Purpose: v.Purpose, CreatedAt: at.UTC(), Image: image}
}
func ValidImageMessageReceipt(r ImageMessageReceipt, owner, conversation, op string) bool {
	return r.Schema == ImageMessageOperationSchema && r.OwnerID == owner && r.ConversationID == conversation && r.OperationID == op &&
		shareUUID.MatchString(owner) && shareUUID.MatchString(conversation) && shareUUID.MatchString(op) && shareUUID.MatchString(r.MessageID) && shareUUID.MatchString(r.RecipientAccountID) && owner != r.RecipientAccountID &&
		r.EffectKey == HumanMessageEffectKey(owner, op) && validChatImageHash(r.PayloadDigest) && validChatImageHash(r.CaptionSHA256) &&
		ValidImageApproval(ImageMessageInput{OperationID: op, RecipientAccountID: r.RecipientAccountID, Purpose: r.Purpose, PixelRisk: r.PixelRisk, MIME: r.InputMIME, InputSize: r.InputSize, InputSHA256: r.InputSHA256}) &&
		ValidImageMetadata(r.Image) && r.Image.MIME == r.InputMIME && !r.CreatedAt.IsZero() && r.CreatedAt.Year() >= 2020 && r.CreatedAt.Year() <= 9999
}
func ImageReceiptMatchesInput(r ImageMessageReceipt, conversation string, v ImageMessageInput) bool {
	return r.ConversationID == conversation && r.OperationID == v.OperationID && r.RecipientAccountID == v.RecipientAccountID && r.InputMIME == v.MIME && r.InputSize == v.InputSize && r.InputSHA256 == v.InputSHA256 && r.PixelRisk == v.PixelRisk && r.Purpose == v.Purpose && r.CaptionSHA256 == messageHash(v.Caption) && r.PayloadDigest == ImageMessagePayloadDigest(conversation, v)
}
