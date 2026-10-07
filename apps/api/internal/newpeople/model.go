package newpeople

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

var (
	ErrNotFound  = errors.New("new people source not found")
	ErrForbidden = errors.New("new people access forbidden")
	ErrInvalid   = errors.New("invalid new people input")
	ErrChanged   = errors.New("new people source changed")
)

type Consent struct {
	Enabled bool `json:"enabled"`
}

// HumanReceipt is a server-only read receipt, not an invitation or model grant.
// Its native proof contains hashes of current source versions, never source bodies.
type HumanReceipt struct {
	Response  Response
	Proof     string
	Seal      string
	ExpiresAt time.Time
}

func (HumanReceipt) MarshalJSON() ([]byte, error) { return nil, ErrForbidden }

type HumanStore interface {
	ReadHumanNewPeople(context.Context, identity.Actor, [32]byte, string) (HumanReceipt, error)
	RevalidateHumanNewPeople(context.Context, identity.Actor, [32]byte, HumanReceipt) error
}

type DraftInput struct {
	Title           string    `json:"title"`
	Category        string    `json:"category"`
	Modality        string    `json:"modality"`
	CityID          string    `json:"cityId,omitempty"`
	PlaceID         string    `json:"placeId,omitempty"`
	AreaLabel       string    `json:"areaLabel,omitempty"`
	OnlinePlatform  string    `json:"onlinePlatform,omitempty"`
	MinParticipants int       `json:"minParticipants,omitempty"`
	MaxParticipants int       `json:"maxParticipants,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

// Signal is internal matching input. The Store must first verify current
// consent, account/profile status, intent lifecycle, audience, Block, Tie,
// pending requests and published Place/City scope. It is not an API payload.
type Signal struct {
	IntentID        string
	AccountID       string
	DisplayName     string
	Category        string
	Modality        string
	CityID          string
	PlaceID         string
	AreaLabel       string
	OnlinePlatform  string
	MinParticipants int
	MaxParticipants int
}

// Candidate deliberately excludes raw scope/platform text and private data.
// It represents compatible declarations, not a verified identity or friendship.
type Candidate struct {
	SourceIntentID    string   `json:"sourceIntentId"`
	CandidateIntentID string   `json:"candidateIntentId"`
	AccountID         string   `json:"accountId"`
	DisplayName       string   `json:"displayName"`
	Category          string   `json:"category"`
	Modality          string   `json:"modality"`
	ReasonCodes       []string `json:"reasonCodes"`
	Reasons           []string `json:"reasons"`
}

type Response struct {
	Source         string      `json:"source"`
	RuleVersion    string      `json:"ruleVersion"`
	SourceIntentID string      `json:"sourceIntentId"`
	Candidates     []Candidate `json:"candidates"`
	Truncated      bool        `json:"truncated"`
}

type Store interface {
	GetNewPeopleConsent(context.Context, string) (Consent, error)
	SetNewPeopleConsent(context.Context, string, bool) (Consent, error)
	CreateNewPeopleIntent(context.Context, string, DraftInput) (socialintent.Record, error)
	ListNewPeopleIntents(context.Context, string) ([]socialintent.Record, error)
	FindNewPeople(context.Context, string, string) (Response, error)
	InviteNewPeople(context.Context, string, string, string, string) (connection.Request, error)
}
