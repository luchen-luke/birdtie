package connection

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"regexp"
	"time"
)

const DecisionOperationSchema = "birdtie.connection-decision.v1"

var ErrOperationChanged = errors.New("connection operation binding changed")
var ErrOperationUnavailable = errors.New("connection operation unavailable")
var decisionUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidDecisionID(id string) bool {
	return decisionUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}
func DecisionState(action string) string {
	return map[string]string{"accept": "accepted", "decline": "declined", "withdraw": "withdrawn"}[action]
}
func DecisionDigest(owner, request, action string) (string, error) {
	if !ValidDecisionID(owner) || !ValidDecisionID(request) || DecisionState(action) == "" {
		return "", ErrForbidden
	}
	raw, _ := json.Marshal(struct {
		Owner   string `json:"ownerId"`
		Request string `json:"requestId"`
		Action  string `json:"action"`
	}{owner, request, action})
	sum := sha256.Sum256(append([]byte("birdtie.connection-decision.v1\x00"), raw...))
	return fmt.Sprintf("%x", sum[:]), nil
}

// Immutable causal outcome, not present resource authority or a chat handle.
type DecisionOperationReceipt struct {
	SchemaVersion string    `json:"schemaVersion"`
	OwnerID       string    `json:"ownerId"`
	RequestID     string    `json:"requestId"`
	OperationID   string    `json:"operationId"`
	RequestDigest string    `json:"requestDigest"`
	Action        string    `json:"action"`
	Scope         string    `json:"scope"`
	Status        string    `json:"status"`
	State         string    `json:"state"`
	Reason        string    `json:"reason,omitempty"`
	RecordedAt    time.Time `json:"recordedAt"`
}

func ValidateDecisionReceipt(r DecisionOperationReceipt, owner, request, operation, action string) error {
	d, e := DecisionDigest(owner, request, r.Action)
	if e != nil || !ValidDecisionID(operation) || r.SchemaVersion != DecisionOperationSchema || r.OwnerID != owner || r.RequestID != request || r.OperationID != operation || r.RequestDigest != d || (action != "" && r.Action != action) || (r.Scope != "friend" && r.Scope != "conversation") || r.RecordedAt.IsZero() || r.RecordedAt.Year() < 2000 || r.RecordedAt.Year() > 2200 {
		return ErrOperationUnavailable
	}
	if r.Status == "COMMITTED" && r.State == DecisionState(r.Action) && r.Reason == "" {
		return nil
	}
	if r.Status == "NO_EFFECT" && r.State == "" && (r.Reason == "EXPIRED" || r.Reason == "ALREADY_DECIDED") {
		return nil
	}
	return ErrOperationUnavailable
}

type DecisionOperationStore interface {
	DecideRequestOperation(context.Context, ea.Access, string, string, string) (DecisionOperationReceipt, error)
	ReadRequestDecisionOperation(context.Context, ea.Access, string, string) (DecisionOperationReceipt, error)
}

// A causal receipt is historical, but disclosing it still requires the current
// original Request participants, policy, Block and Session after encoding.
type DecisionOperationResponseStore interface {
	ValidateRequestDecisionOperationResponse(context.Context, ea.Access, DecisionOperationReceipt) error
}
