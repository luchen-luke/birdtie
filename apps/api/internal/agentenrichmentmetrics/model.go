// Package agentenrichmentmetrics exposes bounded, content-free observations.
// Counts and metadata never form a permission, model handle or business ledger.
package agentenrichmentmetrics

import (
	"errors"
	"math/big"
	"time"
)

const Schema = "agent.enrichment.observability.v1"
const MaxWindow = 90 * 24 * time.Hour
const MaxRows = 10000
const MaxTraceSteps = 8
const DefaultRetention = 30 * 24 * time.Hour
const MaxTraceLease = 30 * time.Second

var ErrInvalid = errors.New("enrichment observation invalid")
var ErrDenied = errors.New("enrichment observation denied")
var ErrUnavailable = errors.New("enrichment observation unavailable")
var ErrNotFound = errors.New("enrichment observation not found")

const Created = "memory_created"
const Rejected = "memory_rejected"
const Corrected = "memory_corrected"
const Deleted = "memory_deleted"
const Promoted = "candidate_promoted"
const PolicyTriggered = "policy_triggered"

func MetricKinds() []string {
	return []string{Created, Rejected, Corrected, Deleted, Promoted, PolicyTriggered}
}
func ValidLifecycle(s string) bool {
	switch s {
	case Created, Rejected, Corrected, Deleted, Promoted:
		return true
	}
	return false
}

type Window struct {
	Since time.Time
	Until time.Time
}

func (w Window) Valid(now time.Time) bool {
	return !now.IsZero() && !w.Since.IsZero() && w.Since.Before(w.Until) && !w.Until.After(now) && !w.Since.Before(now.Add(-MaxWindow))
}

type Metric struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}
type PolicyCount struct {
	Family      string `json:"family"`
	Version     int64  `json:"version"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
	Count       int64  `json:"count"`
}
type Snapshot struct {
	SchemaVersion            string        `json:"schemaVersion"`
	Window                   Window        `json:"window"`
	Metrics                  []Metric      `json:"metrics"`
	Policies                 []PolicyCount `json:"policies"`
	LegacyUnclassified       int64         `json:"legacyUnclassified"`
	HistoricalClassification string        `json:"historicalClassification"`
	Limit                    int           `json:"limit"`
	Truncated                bool          `json:"truncated"`
	ObservedAt               time.Time     `json:"observedAt"`
	ValidUntil               time.Time     `json:"validUntil"`
	PolicyStatus             string        `json:"policyStatus"`
}
type BudgetWarning struct {
	Scope     string `json:"scope"`
	Dimension string `json:"dimension"`
	Limit     int64  `json:"limit"`
	Allocated int64  `json:"allocated"`
	Status    string `json:"status"`
}

// Compare exact integer arithmetic. Never use floating point or overflow to
// turn a held cost upper bound into provider actual cost.
func Warning(scope, dimension string, limit, used int64) (BudgetWarning, error) {
	switch scope {
	case "TENANT_PERSON", "SUBJECT_PERSON", "ROOT", "TASK":
	default:
		return BudgetWarning{}, ErrInvalid
	}
	switch dimension {
	case "REQUESTS", "INPUT_TOKENS", "OUTPUT_TOKENS", "COST_MICROS":
	default:
		return BudgetWarning{}, ErrInvalid
	}
	if limit <= 0 || used < 0 || used > limit {
		return BudgetWarning{}, ErrInvalid
	}
	status := "NORMAL"
	if used == limit {
		status = "EXHAUSTED"
	} else if new(big.Int).Mul(big.NewInt(used), big.NewInt(100)).Cmp(new(big.Int).Mul(big.NewInt(limit), big.NewInt(80))) >= 0 {
		status = "NEAR_LIMIT"
	}
	return BudgetWarning{scope, dimension, limit, used, status}, nil
}

type MonthlyUsage struct {
	Currency               string    `json:"currency"`
	MonthStart             time.Time `json:"monthStart"`
	KnownActualMicros      int64     `json:"knownActualMicros"`
	HeldUnknownUpperMicros int64     `json:"heldUnknownUpperMicros"`
	UnknownAttempts        int64     `json:"unknownAttempts"`
	Limit                  *int64    `json:"limit"`
	LimitStatus            string    `json:"limitStatus"`
}
type BudgetAlerts struct {
	SchemaVersion string          `json:"schemaVersion"`
	Warnings      []BudgetWarning `json:"warnings"`
	Monthly       MonthlyUsage    `json:"monthly"`
	Evidence      string          `json:"evidence"`
	ObservedAt    time.Time       `json:"observedAt"`
	ValidUntil    time.Time       `json:"validUntil"`
}
type Configuration struct {
	Version             string   `json:"version"`
	Fingerprint         string   `json:"fingerprint"`
	PromptVersion       string   `json:"promptVersion"`
	InputSchemaVersion  string   `json:"inputSchemaVersion"`
	OutputSchemaVersion string   `json:"outputSchemaVersion"`
	PolicyVersion       string   `json:"policyVersion"`
	ToolAllowlist       []string `json:"toolAllowlist"`
	Capabilities        []string `json:"capabilities"`
}
type ModelStep struct {
	Ordinal             int    `json:"ordinal"`
	OperationID         string `json:"operationId"`
	State               string `json:"state"`
	PriceVersion        string `json:"priceVersion"`
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	ModelVersion        string `json:"modelVersion"`
	WireContract        string `json:"wireContract"`
	Currency            string `json:"currency"`
	Evidence            string `json:"evidence"`
	InputTokens         *int64 `json:"inputTokens"`
	OutputTokens        *int64 `json:"outputTokens"`
	ActualCostMicros    *int64 `json:"actualCostMicros"`
	HeldUpperCostMicros int64  `json:"heldUpperCostMicros"`
	UsageStatus         string `json:"usageStatus"`
	ToolDecision        string `json:"toolDecision"`
}
type Trace struct {
	SchemaVersion  string         `json:"schemaVersion"`
	Family         string         `json:"family"`
	RunID          string         `json:"runId"`
	State          string         `json:"state"`
	EventID        *string        `json:"eventId"`
	SourceID       *string        `json:"sourceId"`
	SourceStatus   string         `json:"sourceStatus"`
	Decision       string         `json:"decision"`
	ErrorCode      string         `json:"errorCode"`
	DurationMicros int64          `json:"durationMicros"`
	Configuration  *Configuration `json:"configuration"`
	Steps          []ModelStep    `json:"steps"`
	ObservedAt     time.Time      `json:"observedAt"`
	ValidUntil     time.Time      `json:"validUntil"`
	ModelAccess    string         `json:"modelAccess"`
}
