// Package agentconfidence describes confidence semantics without estimating
// scores or granting any source, model, candidate or action permission.
package agentconfidence

import (
	"errors"
	"math"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

const SchemaV1 = "agent-memory-confidence-v1"

var (
	ErrInvalid     = errors.New("invalid memory confidence")
	ErrForbidden   = errors.New("memory confidence forbidden")
	ErrNotFound    = errors.New("memory confidence not found")
	ErrConflict    = errors.New("memory confidence version conflict")
	ErrUnavailable = errors.New("memory confidence unavailable")
)

type Semantics string

const (
	DirectDeclaration     Semantics = "DIRECT_DECLARATION"
	UncalibratedScore     Semantics = "UNCALIBRATED_SCORE"
	Ordinal               Semantics = "ORDINAL"
	CalibratedProbability Semantics = "CALIBRATED_PROBABILITY"
)

type OrdinalLevel string

const (
	Low    OrdinalLevel = "LOW"
	Medium OrdinalLevel = "MEDIUM"
	High   OrdinalLevel = "HIGH"
)

// Assessment is descriptive metadata, never an authorization or threshold for
// promoting a candidate. Ordinal has no numeric probability. Calibration has
// no current trusted provider; a caller's numeric score cannot enable it.
type Assessment struct {
	Semantics Semantics    `json:"semantics"`
	Value     *float64     `json:"value,omitempty"`
	Level     OrdinalLevel `json:"level,omitempty"`
}

func NormalizeAssessment(input Assessment) (Assessment, error) {
	if input.Value != nil && (math.IsNaN(*input.Value) || math.IsInf(*input.Value, 0) || *input.Value < 0 || *input.Value > 1) {
		return Assessment{}, ErrInvalid
	}
	switch input.Semantics {
	case DirectDeclaration:
		if input.Value == nil || *input.Value != 1 || input.Level != "" {
			return Assessment{}, ErrInvalid
		}
	case UncalibratedScore:
		if input.Value == nil || input.Level != "" {
			return Assessment{}, ErrInvalid
		}
	case Ordinal:
		if input.Value != nil || (input.Level != Low && input.Level != Medium && input.Level != High) {
			return Assessment{}, ErrInvalid
		}
	case CalibratedProbability:
		if input.Value == nil || input.Level != "" {
			return Assessment{}, ErrInvalid
		}
		return Assessment{}, ErrUnavailable
	default:
		return Assessment{}, ErrInvalid
	}
	if input.Value != nil {
		value := *input.Value
		if value == 0 {
			value = 0
		}
		input.Value = &value
	}
	return input, nil
}
func NewDirectDeclaration() Assessment {
	value := 1.0
	return Assessment{Semantics: DirectDeclaration, Value: &value}
}
func NewUncalibratedScore(value float64) (Assessment, error) {
	return NormalizeAssessment(Assessment{Semantics: UncalibratedScore, Value: &value})
}
func NewOrdinal(level OrdinalLevel) (Assessment, error) {
	return NormalizeAssessment(Assessment{Semantics: Ordinal, Level: level})
}
func NewCalibratedProbability(value float64) (Assessment, error) {
	return NormalizeAssessment(Assessment{Semantics: CalibratedProbability, Value: &value})
}
func Description(input Assessment) (string, error) {
	input, err := NormalizeAssessment(input)
	if err != nil {
		return "", err
	}
	switch input.Semantics {
	case DirectDeclaration:
		return "本人明确填写；数值1标记直接声明，不代表正确概率。", nil
	case UncalibratedScore:
		return "未校准评分，不代表正确概率。", nil
	case Ordinal:
		return "置信等级不代表正确概率。", nil
	default:
		return "", ErrUnavailable
	}
}

// OwnMemoryView is a current, human-only metadata projection of the existing
// Memory row. It is not a second Memory record, source text or model context.
type OwnMemoryView struct {
	SchemaVersion string                 `json:"schemaVersion"`
	MemoryID      string                 `json:"memoryId"`
	MemoryVersion int64                  `json:"memoryVersion"`
	AgentID       string                 `json:"agentId"`
	OwnerType     actorref.Type          `json:"ownerType"`
	OwnerID       string                 `json:"ownerId"`
	SourceType    agentmemory.SourceType `json:"sourceType"`
	Assessment    Assessment             `json:"assessment"`
	Explanation   string                 `json:"explanation"`
	ValidFrom     time.Time              `json:"validFrom"`
	ValidUntil    time.Time              `json:"validUntil"`
	ReadAt        time.Time              `json:"readAt"`
}

func canonicalID(id string) bool {
	normalized, err := agentmemory.NormalizeMemoryID(id)
	return err == nil && normalized == id
}
func validTime(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 && t.Year() >= 1 && t.Year() <= 9999
}
func ValidateOwnMemoryView(view OwnMemoryView) error {
	if view.SchemaVersion != SchemaV1 || !canonicalID(view.MemoryID) || !canonicalID(view.AgentID) || !canonicalID(view.OwnerID) || view.OwnerType != actorref.Person || view.MemoryVersion <= 0 || view.SourceType != agentmemory.SourceExplicit ||
		!validTime(view.ValidFrom) || !validTime(view.ValidUntil) || !validTime(view.ReadAt) || !view.ValidUntil.After(view.ValidFrom) || view.ValidUntil.Sub(view.ValidFrom) > agentmemory.MaxValidity || view.ValidFrom.After(view.ReadAt) || !view.ValidUntil.After(view.ReadAt) || view.Assessment.Semantics != DirectDeclaration {
		return ErrInvalid
	}
	description, err := Description(view.Assessment)
	if err != nil || view.Explanation != description {
		return ErrInvalid
	}
	return nil
}
