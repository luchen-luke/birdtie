// Package agentcontextadapter projects an already-authorized exact AGE view.
// It is not a permission resolver, provider payload, grant or knowledge ledger.
package agentcontextadapter

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
)

const Version = "air-context-adapter-v1"
const BudgetUnit = "UTF8_JSON_BYTES_V1"
const MaxEncodedBytes = 32768
const MaxOptionalItems = 19
const Available = "AVAILABLE"
const Unknown = "UNKNOWN"
const NotRequested = "NOT_REQUESTED"
const NotRelevant = "NOT_RELEVANT"
const OmittedBudget = "OMITTED_BUDGET"
const OmittedConfidence = "OMITTED_CONFIDENCE"
const OmittedLimit = "OMITTED_LIMIT"
const OmittedControls = "OMITTED_CONTROLS"

var ErrInvalid = errors.New("invalid authorized context projection")
var ErrBudget = errors.New("context budget cannot contain required anchors")

// These are final encoded view bytes, not measured provider tokens or costs.
// Controls are server-side projection choices, never client source permissions.
// Nil ItemLimit means 19; a non-nil zero retains required anchors only.
// Priority, when provided, is an exact permutation of the five optional families.
// A nil threshold is disabled; a provided threshold only filters memories with
// native DIRECT_DECLARATION metadata. Other families have no confidence score.
type Budget struct {
	MaxEncodedBytes     int
	ItemLimit           *int
	Priority            []string
	ConfidenceThreshold *float64
	RecencyWeight       float64
}

func DefaultBudget() Budget { return Budget{MaxEncodedBytes: 16384} }

type BudgetResult struct {
	Unit                string                    `json:"unit"`
	Limit               int                       `json:"limit"`
	Used                int                       `json:"used"`
	TokenCountStatus    string                    `json:"tokenCountStatus"`
	Omitted             map[string]int            `json:"omitted"`
	ItemLimit           *int                      `json:"itemLimit,omitempty"`
	Priority            []string                  `json:"priority,omitempty"`
	ConfidenceThreshold *float64                  `json:"confidenceThreshold,omitempty"`
	RecencyWeight       float64                   `json:"recencyWeight,omitempty"`
	OmissionReasons     map[string]map[string]int `json:"omissionReasons,omitempty"`
}
type Fact struct {
	Kind             string                      `json:"kind"`
	SourceID         string                      `json:"sourceId"`
	Field            string                      `json:"field,omitempty"`
	Text             string                      `json:"text"`
	ValueStatus      string                      `json:"valueStatus"`
	DeclaredSettings json.RawMessage             `json:"declaredSettings,omitempty"`
	Confidence       *agentconfidence.Assessment `json:"confidence,omitempty"`
}
type Provenance struct {
	Kind   string     `json:"kind"`
	ID     string     `json:"id"`
	Fields []string   `json:"fields"`
	Origin string     `json:"origin"`
	Source acb.Source `json:"source"`
}

// View is ephemeral data, never a sealed BuiltContext or authority object.
// Final encoded bytes include the actual answer, retained facts and provenance.
type View struct {
	SchemaVersion          string                `json:"schemaVersion"`
	AdapterVersion         string                `json:"adapterVersion"`
	Purpose                string                `json:"purpose"`
	TaskID                 string                `json:"taskId"`
	Answer                 string                `json:"answer"`
	City                   acb.ContextCity       `json:"city"`
	Task                   acb.ContextTask       `json:"task"`
	Facts                  []Fact                `json:"facts"`
	Places                 []acb.PublicPlace     `json:"places"`
	Activities             []acb.PublicActivity  `json:"activities"`
	Relationships          []acb.ContextTie      `json:"relationships"`
	Sources                []acb.Source          `json:"sources"`
	Provenance             []Provenance          `json:"provenance"`
	Sections               map[string]string     `json:"sections"`
	RelevanceExcluded      map[string]int        `json:"relevanceExcluded"`
	Budget                 BudgetResult          `json:"budget"`
	ObservedAt             time.Time             `json:"observedAt"`
	ExpiresAt              time.Time             `json:"expiresAt"`
	ModelAccess            string                `json:"modelAccess"`
	MemoryPromotionAllowed bool                  `json:"memoryPromotionAllowed"`
	ContentIsInstruction   bool                  `json:"contentIsInstruction"`
	FieldEvidenceSet       *acb.FieldEvidenceSet `json:"fieldEvidenceSet,omitempty"`
	evidenceInput          *acb.FieldEvidenceSet
	evidenceSelectors      []acb.FieldEvidenceSelector
}
