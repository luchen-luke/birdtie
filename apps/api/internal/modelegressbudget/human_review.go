package modelegressbudget

import (
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"time"
)

const HumanSchemaVersion = "air.model_egress_human.v1"

// These are owner-visible selectors and receipts, never transferable permits.
type HumanOption struct {
	RootTraceID          string                 `json:"rootTraceId"`
	TaskID               string                 `json:"taskId"`
	TaskQuery            string                 `json:"taskQuery"`
	PriceVersion         string                 `json:"priceVersion"`
	ConfigurationVersion string                 `json:"configurationVersion"`
	PromptVersion        string                 `json:"promptVersion"`
	InputSchemaVersion   string                 `json:"inputSchemaVersion"`
	OutputSchemaVersion  string                 `json:"outputSchemaVersion"`
	Destination          modelcapability.Key    `json:"destination"`
	Region               modelcapability.Region `json:"region"`
	Retention            string                 `json:"retention"`
	Currency             string                 `json:"currency"`
	Evidence             string                 `json:"evidence"`
	MaxOutputTokens      int                    `json:"maxOutputTokens"`
	MaxDeadlineAt        time.Time              `json:"maxDeadlineAt"`
}
type HumanOptions struct {
	SchemaVersion string        `json:"schemaVersion"`
	OwnerID       string        `json:"ownerId"`
	ObservedAt    time.Time     `json:"observedAt"`
	Options       []HumanOption `json:"options"`
	ModelAccess   string        `json:"modelAccess"`
}
type HumanReceipt struct {
	SchemaVersion   string     `json:"schemaVersion"`
	OwnerID         string     `json:"ownerId"`
	PreviewID       string     `json:"previewId"`
	RootTraceID     string     `json:"rootTraceId"`
	TaskID          string     `json:"taskId"`
	PriceVersion    string     `json:"priceVersion"`
	MaxOutputTokens int        `json:"maxOutputTokens"`
	Status          string     `json:"status"`
	Revision        int64      `json:"revision"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	ApprovedAt      *time.Time `json:"approvedAt,omitempty"`
	RevokedAt       *time.Time `json:"revokedAt,omitempty"`
	Reviewable      bool       `json:"reviewable"`
	Approvable      bool       `json:"approvable"`
	Revocable       bool       `json:"revocable"`
	ModelAccess     string     `json:"modelAccess"`
	// Rendering is explicit at the HTTP boundary, never JSON authority.
	Preview *Preview `json:"-"`
}
