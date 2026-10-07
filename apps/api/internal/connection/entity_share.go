package connection

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"

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
