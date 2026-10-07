package connection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"strings"
	"time"
)

const HumanMessageOperationSchema = "human.message_operation.v1"
const HumanMessageToolVersion = "human.message.send.v1"

type MessageOperationInput struct {
	OperationID string `json:"operationId"`
	Body        string `json:"body"`
}

// Metadata of the immutable original message, not an execution capability.
type MessageOperationReceipt struct {
	Schema         string    `json:"schemaVersion"`
	OwnerID        string    `json:"ownerId"`
	ConversationID string    `json:"conversationId"`
	OperationID    string    `json:"operationId"`
	EffectKey      string    `json:"effectKey"`
	PayloadDigest  string    `json:"payloadDigest"`
	ToolVersion    string    `json:"toolVersion"`
	MessageID      string    `json:"messageId"`
	CreatedAt      time.Time `json:"createdAt"`
}
type HumanMessageOperationStore interface {
	SendHumanMessageOperation(context.Context, ea.Access, string, MessageOperationInput) (MessageOperationReceipt, error)
	ReadHumanMessageOperation(context.Context, ea.Access, string, string) (MessageOperationReceipt, error)
	ValidateHumanMessageOperationResponse(context.Context, ea.Access, MessageOperationReceipt) error
}

func messageHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }

// A one-action human send uses the same UUID as logical operation and action.
// Neither parameters nor tool versions form a new effect address.
func HumanMessageEffectKey(owner, op string) string {
	return messageHash("birdtie.human-message-effect.v1\n" + owner + "\n" + op + "\n" + op + "\nPLAIN_MESSAGE_SEND")
}
func HumanMessagePayloadDigest(conversation, body string) string {
	return messageHash("birdtie.human-message-payload.v1\n" + conversation + "\n" + body)
}
func ValidMessageOperation(in MessageOperationInput) bool {
	return shareUUID.MatchString(in.OperationID) && in.Body != "" && strings.TrimSpace(in.Body) == in.Body && len(in.Body) <= 2000 && !strings.ContainsRune(in.Body, 0)
}
func DecodeMessageOperation(raw []byte) (MessageOperationInput, error) {
	var v MessageOperationInput
	if len(raw) > 16384 {
		return v, ErrConflict
	}
	f, e := shareObject(raw, "operationId", "body")
	if e != nil {
		return v, e
	}
	if json.Unmarshal(f["operationId"], &v.OperationID) != nil || json.Unmarshal(f["body"], &v.Body) != nil || !ValidMessageOperation(v) {
		return MessageOperationInput{}, ErrConflict
	}
	return v, nil
}
func NewMessageOperationReceipt(owner, op string, m Message) MessageOperationReceipt {
	return MessageOperationReceipt{HumanMessageOperationSchema, owner, m.ConversationID, op, HumanMessageEffectKey(owner, op), HumanMessagePayloadDigest(m.ConversationID, m.Body), HumanMessageToolVersion, m.ID, m.CreatedAt.UTC()}
}
func ValidMessageOperationReceipt(v MessageOperationReceipt, owner, conversation, op string) bool {
	return v.Schema == HumanMessageOperationSchema && v.OwnerID == owner && v.ConversationID == conversation && v.OperationID == op && v.ToolVersion == HumanMessageToolVersion && v.EffectKey == HumanMessageEffectKey(owner, op) && shareUUID.MatchString(owner) && shareUUID.MatchString(conversation) && shareUUID.MatchString(op) && shareUUID.MatchString(v.MessageID) && len(v.PayloadDigest) == 64 && isMessageHex(v.PayloadDigest) && !v.CreatedAt.IsZero() && v.CreatedAt.Year() >= 1 && v.CreatedAt.Year() <= 9999
}
func isMessageHex(v string) bool {
	_, e := hex.DecodeString(v)
	return e == nil && strings.ToLower(v) == v
}
