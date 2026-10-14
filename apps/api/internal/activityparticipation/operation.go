package activityparticipation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

const HumanParticipationOperationSchema = "human.participation_operation.v1"
const HumanParticipationClosureSchema = "human.participation_operation_closure.v1"
const HumanParticipationToolVersion = "human.participation.write.v1"

var ErrOperationConflict = errors.New("participation operation binding changed")
var operationUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var operationHex = regexp.MustCompile(`^[0-9a-f]{64}$`)

type OperationInput struct {
	OperationID string `json:"operationId"`
	Operation   string `json:"operation"`
}
type OperationCloseInput struct {
	Operation     string `json:"operation"`
	RequestDigest string `json:"requestDigest"`
}

// Original causal history. This is neither present participation state nor an
// approval, authority, source snapshot or executable handle.
type OperationReceipt struct {
	SchemaVersion string     `json:"schemaVersion"`
	OwnerID       string     `json:"ownerId"`
	ActivityID    string     `json:"activityId"`
	OperationID   string     `json:"operationId"`
	Operation     string     `json:"operation"`
	EffectKey     string     `json:"effectKey"`
	RequestDigest string     `json:"requestDigest"`
	ToolVersion   string     `json:"toolVersion"`
	Status        string     `json:"status"`
	RecordedAt    time.Time  `json:"recordedAt"`
	EffectID      string     `json:"effectId,omitempty"`
	State         string     `json:"state,omitempty"`
	EffectAt      *time.Time `json:"effectAt,omitempty"`
	Reason        string     `json:"reason,omitempty"`
}

// An owner-only permanent fence acknowledgment deliberately does not disclose
// whether the original effect committed. Closing a key never cancels an RSVP.
type OperationClosure struct {
	SchemaVersion string    `json:"schemaVersion"`
	OwnerID       string    `json:"ownerId"`
	ActivityID    string    `json:"activityId"`
	OperationID   string    `json:"operationId"`
	Operation     string    `json:"operation"`
	EffectKey     string    `json:"effectKey"`
	RequestDigest string    `json:"requestDigest"`
	ToolVersion   string    `json:"toolVersion"`
	Closed        bool      `json:"closed"`
	RecordedAt    time.Time `json:"recordedAt"`
}

type HumanOperationStore interface {
	WriteHumanParticipationOperation(context.Context, ea.Access, string, OperationInput, ea.BoundCondition) (OperationReceipt, bool, error)
	ReadHumanParticipationOperation(context.Context, ea.Access, string, string) (OperationReceipt, error)
	CloseHumanParticipationOperation(context.Context, ea.Access, string, string, OperationCloseInput) (OperationClosure, error)
	ReadHumanParticipationClosure(context.Context, ea.Access, string, string) (OperationClosure, error)
	ValidateHumanParticipationOperationResponse(context.Context, ea.Access, OperationReceipt) error
	ValidateHumanParticipationClosureResponse(context.Context, ea.Access, OperationClosure) error
}

func ValidOperationID(id string) bool {
	return operationUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}
func ValidParticipationOperation(op string) bool { return op == "JOIN" || op == "CANCEL_RSVP" }
func operationHash(s string) string              { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func HumanParticipationEffectKey(owner, op string) string {
	return operationHash("birdtie.human-participation-effect.v1\n" + owner + "\n" + op + "\n" + op + "\nACTIVITY_PARTICIPATION")
}
func operationTime(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 2000 && t.UTC().Year() <= 2200 && t.Nanosecond()%1000 == 0
}
func HumanParticipationRequestDigest(owner, activity string, in OperationInput, b ea.BoundCondition) (string, error) {
	if !ValidOperationID(owner) || !ValidOperationID(activity) || !ValidOperationID(in.OperationID) || !ValidParticipationOperation(in.Operation) || b.Kind != ea.Join || b.Operation != in.Operation || !b.Valid(ea.Ref{Type: "activity", ID: activity}) || !operationTime(b.ValidUntil) {
		return "", ea.ErrInvalid
	}
	return operationHash("birdtie.human-participation-request.v1\n" + owner + "\n" + activity + "\n" + in.OperationID + "\n" + in.Operation + "\n" + HumanParticipationToolVersion + "\n" + b.SourceVersion + "\n" + b.ValidUntil.UTC().Format("2006-01-02T15:04:05.000000Z")), nil
}

func decodeOperationObject(raw []byte, keys ...string) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, ea.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ea.ErrInvalid
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	out := map[string]string{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || !allowed[k] {
			return nil, ea.ErrInvalid
		}
		if _, exists := out[k]; exists {
			return nil, ea.ErrInvalid
		}
		var v any
		if d.Decode(&v) != nil {
			return nil, ea.ErrInvalid
		}
		s, ok := v.(string)
		if !ok {
			return nil, ea.ErrInvalid
		}
		out[k] = s
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(out) != len(keys) {
		return nil, ea.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ea.ErrInvalid
	}
	return out, nil
}
func DecodeOperation(raw []byte) (OperationInput, error) {
	v, e := decodeOperationObject(raw, "operationId", "operation")
	if e != nil {
		return OperationInput{}, e
	}
	in := OperationInput{v["operationId"], v["operation"]}
	if !ValidOperationID(in.OperationID) || !ValidParticipationOperation(in.Operation) {
		return OperationInput{}, ea.ErrInvalid
	}
	return in, nil
}
func DecodeOperationClose(raw []byte) (OperationCloseInput, error) {
	v, e := decodeOperationObject(raw, "operation", "requestDigest")
	if e != nil {
		return OperationCloseInput{}, e
	}
	in := OperationCloseInput{v["operation"], v["requestDigest"]}
	if !ValidParticipationOperation(in.Operation) || !operationHex.MatchString(in.RequestDigest) {
		return OperationCloseInput{}, ea.ErrInvalid
	}
	return in, nil
}
func ValidOperationReceipt(r OperationReceipt, owner, activity, op string) bool {
	if r.SchemaVersion != HumanParticipationOperationSchema || r.OwnerID != owner || r.ActivityID != activity || r.OperationID != op || !ValidOperationID(owner) || !ValidOperationID(activity) || !ValidOperationID(op) || !ValidParticipationOperation(r.Operation) || r.EffectKey != HumanParticipationEffectKey(owner, op) || !operationHex.MatchString(r.RequestDigest) || r.ToolVersion != HumanParticipationToolVersion || !operationTime(r.RecordedAt) {
		return false
	}
	if r.Status == "NO_EFFECT" {
		return r.Reason == "USER_CLOSED" && r.EffectID == "" && r.State == "" && r.EffectAt == nil
	}
	want := map[string]string{"JOIN": "going", "CANCEL_RSVP": "cancelled"}[r.Operation]
	return r.Status == "COMMITTED" && r.Reason == "" && ValidOperationID(r.EffectID) && r.State == want && r.EffectAt != nil && operationTime(*r.EffectAt) && !r.EffectAt.After(r.RecordedAt)
}
func NewOperationClosure(r OperationReceipt) OperationClosure {
	return OperationClosure{HumanParticipationClosureSchema, r.OwnerID, r.ActivityID, r.OperationID, r.Operation, r.EffectKey, r.RequestDigest, r.ToolVersion, true, r.RecordedAt.UTC()}
}
func ValidOperationClosure(r OperationClosure, owner, activity, op string) bool {
	return r.SchemaVersion == HumanParticipationClosureSchema && r.OwnerID == owner && r.ActivityID == activity && r.OperationID == op && ValidOperationID(owner) && ValidOperationID(activity) && ValidOperationID(op) && ValidParticipationOperation(r.Operation) && r.EffectKey == HumanParticipationEffectKey(owner, op) && operationHex.MatchString(r.RequestDigest) && r.ToolVersion == HumanParticipationToolVersion && r.Closed && operationTime(r.RecordedAt) && strings.TrimSpace(r.RequestDigest) == r.RequestDigest
}
