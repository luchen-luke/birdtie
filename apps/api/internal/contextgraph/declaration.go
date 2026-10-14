package contextgraph

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"time"
)

var (
	ErrDeclaration = errors.New("invalid context declaration")
	ErrUnavailable = errors.New("context source unavailable")
	ErrNotFound    = errors.New("context declaration not found")
)

// Declaration is a private statement by a Person, not proof of residence,
// institution enrollment, Community membership, or content access.
type Declaration struct {
	ContextID  string `json:"contextId"`
	Type       Type   `json:"contextType"`
	SourceKey  string `json:"sourceKey"`
	Label      string `json:"label"`
	Relation   string `json:"relation"`
	Visibility string `json:"visibility"`
}

type DeclarationInput struct {
	Type      Type   `json:"contextType"`
	SourceKey string `json:"sourceKey"`
	Relation  string `json:"relation"`
}

type DeclarationControlInput struct {
	OperationID     string `json:"operationId"`
	Action          string `json:"action"`
	ExpectedVersion int64  `json:"expectedVersion"`
	ContextID       string `json:"contextId,omitempty"`
	Type            Type   `json:"contextType,omitempty"`
	SourceKey       string `json:"sourceKey,omitempty"`
	Relation        string `json:"relation"`
	Approval        string `json:"approval,omitempty"`
}
type DeclarationControlState struct {
	Version       int64                      `json:"version"`
	Items         []Declaration              `json:"items"`
	ObservedAt    time.Time                  `json:"observedAt"`
	ExpiresAt     time.Time                  `json:"expiresAt"`
	ResponseProof identity.HumanControlProof `json:"-"`
}
type DeclarationControlPreview struct {
	identity.HumanControlPreview
	ExpectedVersion int64  `json:"expectedVersion"`
	ContextID       string `json:"contextId,omitempty"`
	Type            Type   `json:"contextType,omitempty"`
	SourceKey       string `json:"sourceKey,omitempty"`
	Relation        string `json:"relation"`
}
type DeclarationControlStore interface {
	ReadDeclarationControl(context.Context, [32]byte, identity.Actor) (DeclarationControlState, error)
	PreviewDeclarationControl(context.Context, [32]byte, identity.Actor, DeclarationControlInput) (DeclarationControlPreview, error)
	CommitDeclarationControl(context.Context, [32]byte, identity.Actor, DeclarationControlInput) (identity.HumanControlReceipt, error)
	ReadDeclarationControlOperation(context.Context, [32]byte, identity.Actor, string) (identity.HumanControlReceipt, error)
	StopDeclarationControlOperation(context.Context, [32]byte, identity.Actor, string) (identity.HumanControlReceipt, error)
}

func NormalizeDeclarationControl(in DeclarationControlInput, commit bool) (DeclarationControlInput, error) {
	if !identity.HumanControlUUID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion > identity.HumanControlMaxVersion || !identity.HumanControlApprovalValid(in.Approval, commit) {
		return in, identity.ErrControlInvalid
	}
	switch in.Action {
	case "DECLARE":
		if in.ContextID != "" || in.ExpectedVersion == identity.HumanControlMaxVersion {
			return in, identity.ErrControlInvalid
		}
		v, e := NormalizeDeclaration(DeclarationInput{in.Type, in.SourceKey, in.Relation})
		if e != nil {
			return in, identity.ErrControlInvalid
		}
		in.Type = v.Type
		in.SourceKey = v.SourceKey
		in.Relation = v.Relation
	case "REMOVE":
		if !identity.HumanControlUUID(in.ContextID) || in.Type != "" || in.SourceKey != "" {
			return in, identity.ErrControlInvalid
		}
		in.Relation = strings.ToLower(strings.TrimSpace(in.Relation))
		switch in.Relation {
		case "current", "past", "destination", "home", "affiliation", "interest":
		default:
			return in, identity.ErrControlInvalid
		}
	default:
		return in, identity.ErrControlInvalid
	}
	return in, nil
}
func DeclarationControlDigest(in DeclarationControlInput) string {
	return identity.HumanControlTupleHash([]any{"human-context-v1", in.OperationID, in.Action, in.ExpectedVersion, in.ContextID, in.Type, identity.HumanControlTextHash(in.SourceKey), in.Relation})
}
func DecodeDeclarationControl(raw []byte, commit bool) (DeclarationControlInput, error) {
	var in DeclarationControlInput
	if json.Unmarshal(raw, &in) != nil {
		return in, identity.ErrControlInvalid
	}
	keys := []string{"operationId", "action", "expectedVersion", "relation"}
	if in.Action == "DECLARE" {
		keys = append(keys, "contextType", "sourceKey")
	} else {
		keys = append(keys, "contextId")
	}
	if commit {
		keys = append(keys, "approval")
	}
	if identity.HumanControlClosedJSON(raw, keys, nil) != nil {
		return in, identity.ErrControlInvalid
	}
	return NormalizeDeclarationControl(in, commit)
}

type DeclarationStore interface {
	ListOwnContextDeclarations(context.Context, string) ([]Declaration, error)
	DeclareContext(context.Context, string, DeclarationInput) (Declaration, error)
	RemoveContextDeclaration(context.Context, string, string, string) error
}

func NormalizeDeclaration(input DeclarationInput) (DeclarationInput, error) {
	input.Type = Type(strings.ToUpper(strings.TrimSpace(string(input.Type))))
	input.SourceKey = strings.TrimSpace(input.SourceKey)
	input.Relation = strings.ToLower(strings.TrimSpace(input.Relation))
	if len([]rune(input.SourceKey)) == 0 || len([]rune(input.SourceKey)) > 160 ||
		strings.ContainsAny(input.SourceKey, "\r\n\x00") {
		return DeclarationInput{}, ErrDeclaration
	}
	switch input.Type {
	case City:
		if input.Relation != "current" && input.Relation != "past" &&
			input.Relation != "destination" && input.Relation != "home" {
			return DeclarationInput{}, ErrDeclaration
		}
	case Institution:
		if input.Relation != "past" && input.Relation != "affiliation" {
			return DeclarationInput{}, ErrDeclaration
		}
	case Online:
		if input.Relation != "interest" {
			return DeclarationInput{}, ErrDeclaration
		}
	default:
		return DeclarationInput{}, ErrDeclaration
	}
	return input, nil
}
