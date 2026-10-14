package connection

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type EntityShareAccess struct {
	Actor         identity.Actor
	SessionDigest [32]byte
}
type EntityShareInput struct {
	OperationID string `json:"operationId"`
	Entity      struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"entity"`
}
type EntityShareReceipt struct {
	OperationID string  `json:"operationId"`
	Message     Message `json:"message"`
}
type HumanEntityShareStore interface {
	ShareHumanEntity(context.Context, EntityShareAccess, string, EntityShareInput) (EntityShareReceipt, error)
	GetHumanEntityShare(context.Context, EntityShareAccess, string, string) (EntityShareReceipt, error)
}

var shareUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidEntityShare(in EntityShareInput) bool {
	if !shareUUID.MatchString(in.OperationID) || !shareUUID.MatchString(in.Entity.ID) {
		return false
	}
	switch in.Entity.Type {
	case "activity", "place", "person", "community", "organization", "business", "moment":
		return true
	}
	return false
}

// Closed object reader rejects duplicate fields, case aliases, null and unknown
// ownership selectors before the native sender/source authorization boundary.
func shareObject(raw []byte, required ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ErrConflict
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok {
			return nil, ErrConflict
		}
		if _, seen := out[k]; seen {
			return nil, ErrConflict
		}
		allowed := false
		for _, s := range required {
			if k == s {
				allowed = true
			}
		}
		if !allowed {
			return nil, ErrConflict
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, ErrConflict
		}
		out[k] = v
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(out) != len(required) {
		return nil, ErrConflict
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrConflict
	}
	return out, nil
}
func DecodeEntityShare(raw []byte) (EntityShareInput, error) {
	var in EntityShareInput
	if len(raw) > 2048 {
		return in, ErrConflict
	}
	fields, e := shareObject(raw, "operationId", "entity")
	if e != nil {
		return in, e
	}
	entity, e := shareObject(fields["entity"], "type", "id")
	if e != nil {
		return in, e
	}
	if json.Unmarshal(fields["operationId"], &in.OperationID) != nil || json.Unmarshal(entity["type"], &in.Entity.Type) != nil || json.Unmarshal(entity["id"], &in.Entity.ID) != nil || !ValidEntityShare(in) {
		return EntityShareInput{}, ErrConflict
	}
	return in, nil
}

// The recipient is a current native relationship selector, never an arbitrary
// account ID, caption, URL, permission flag or copied snapshot.
type EntityShareRecipient struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type EntityShareRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type EntityShareControlInput struct {
	OperationID   string         `json:"operationId"`
	Entity        EntityShareRef `json:"entity"`
	SourceVersion string         `json:"sourceVersion,omitempty"`
	Approval      string         `json:"approval,omitempty"`
}
type EntityShareControlPreview struct {
	OperationID        string                     `json:"operationId"`
	Recipient          EntityShareRecipient       `json:"recipient"`
	Entity             EntityShareRef             `json:"entity"`
	EntityTitle        string                     `json:"entityTitle"`
	RecipientAccountID string                     `json:"recipientAccountId"`
	SourceVersion      string                     `json:"sourceVersion"`
	PayloadDigest      string                     `json:"payloadDigest"`
	ObservedAt         time.Time                  `json:"observedAt"`
	ExpiresAt          time.Time                  `json:"expiresAt"`
	Approval           string                     `json:"approval"`
	ResponseProof      identity.HumanControlProof `json:"-"`
}
type EntityShareControlReceipt struct {
	OperationID    string               `json:"operationId"`
	Status         string               `json:"status"`
	Recipient      EntityShareRecipient `json:"recipient"`
	ConversationID string               `json:"conversationId,omitempty"`
	MessageID      string               `json:"messageId,omitempty"`
	Entity         *EntityShareRef      `json:"entity,omitempty"`
	PayloadDigest  string               `json:"payloadDigest,omitempty"`
	CommittedAt    *time.Time           `json:"committedAt,omitempty"`
}
type EntityShareControlStore interface {
	PreviewEntityShareControl(context.Context, EntityShareAccess, EntityShareRecipient, EntityShareControlInput) (EntityShareControlPreview, error)
	CommitEntityShareControl(context.Context, EntityShareAccess, EntityShareRecipient, EntityShareControlInput) (EntityShareControlReceipt, error)
	ReadEntityShareControlOperation(context.Context, EntityShareAccess, EntityShareRecipient, string) (EntityShareControlReceipt, error)
	StopEntityShareControlOperation(context.Context, EntityShareAccess, EntityShareRecipient, string) (EntityShareControlReceipt, error)
}

func ValidEntityShareRecipient(v EntityShareRecipient) bool {
	return (v.Type == "CONVERSATION" || v.Type == "FRIEND_TIE") && identity.HumanControlUUID(v.ID)
}
func ValidEntityShareRef(v EntityShareRef) bool {
	if !identity.HumanControlUUID(v.ID) {
		return false
	}
	switch v.Type {
	case "activity", "place", "person", "community", "organization", "business", "moment":
		return true
	}
	return false
}
func NormalizeEntityShareControl(target EntityShareRecipient, in EntityShareControlInput, commit bool) (EntityShareControlInput, error) {
	if !ValidEntityShareRecipient(target) || !identity.HumanControlUUID(in.OperationID) || !ValidEntityShareRef(in.Entity) || !identity.HumanControlApprovalValid(in.Approval, commit) {
		return in, identity.ErrControlInvalid
	}
	if commit {
		if len(in.SourceVersion) != 64 {
			return in, identity.ErrControlInvalid
		}
		for _, c := range in.SourceVersion {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return in, identity.ErrControlInvalid
			}
		}
	} else if in.SourceVersion != "" {
		return in, identity.ErrControlInvalid
	}
	return in, nil
}
func DecodeEntityShareControl(raw []byte, target EntityShareRecipient, commit bool) (EntityShareControlInput, error) {
	var in EntityShareControlInput
	keys := []string{"operationId", "entity"}
	if commit {
		keys = append(keys, "sourceVersion", "approval")
	}
	if !utf8.Valid(raw) || identity.HumanControlClosedJSON(raw, keys, nil) != nil {
		return in, identity.ErrControlInvalid
	}
	fields, e := shareObject(raw, keys...)
	if e != nil {
		return in, identity.ErrControlInvalid
	}
	if _, e = shareObject(fields["entity"], "type", "id"); e != nil || json.Unmarshal(raw, &in) != nil {
		return in, identity.ErrControlInvalid
	}
	return NormalizeEntityShareControl(target, in, commit)
}
func EntityShareControlDigest(target EntityShareRecipient, in EntityShareControlInput) string {
	return identity.HumanControlTupleHash([]any{"human-entity-share-v1", in.OperationID, target.Type, target.ID, in.Entity.Type, in.Entity.ID})
}
func ValidEntityShareControlReceipt(v EntityShareControlReceipt, target EntityShareRecipient, op string) bool {
	if v.OperationID != op || v.Recipient != target {
		return false
	}
	if v.Status == "STOPPED" {
		return v.ConversationID == "" && v.MessageID == "" && v.Entity == nil && v.PayloadDigest == "" && v.CommittedAt == nil
	}
	return v.Status == "SUCCEEDED" && identity.HumanControlUUID(v.ConversationID) && identity.HumanControlUUID(v.MessageID) && v.Entity != nil && ValidEntityShareRef(*v.Entity) && len(v.PayloadDigest) == 64 && v.CommittedAt != nil
}
