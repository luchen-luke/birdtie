package intentconversion

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"time"
)

const Schema = "intent-activity-conversion-v1"
const PreviewTTL = 90 * time.Second

var ErrInvalid = errors.New("invalid intent activity conversion")
var ErrDenied = errors.New("intent conversion denied")
var ErrConflict = errors.New("intent conversion current version conflict")
var ErrUnavailable = errors.New("intent conversion unavailable")

type Access = activeintent.Access
type Choice struct {
	Activity        activityplan.Plan `json:"activity"`
	ParticipationID string            `json:"participationId"`
}
type Envelope struct {
	SchemaVersion string    `json:"schemaVersion"`
	OwnerID       string    `json:"ownerId"`
	AgentID       string    `json:"agentId"`
	ObservedAt    time.Time `json:"observedAt"`
	ModelAccess   bool      `json:"modelAccess"`
	SendAllowed   bool      `json:"sendAllowed"`
}
type List struct {
	Revalidate func(context.Context) error `json:"-"`
	Envelope
	Intent      socialintent.Record `json:"intent"`
	Version     string              `json:"version"`
	Choices     []Choice            `json:"choices"`
	Limit       int                 `json:"limit"`
	Truncated   bool                `json:"truncated"`
	Explanation string              `json:"explanation"`
}
type Input struct {
	ActivityID      string `json:"activityId"`
	ExpectedVersion string `json:"expectedVersion"`
}
type Approval struct {
	PreviewID string `json:"previewId"`
}
type Preview struct {
	Revalidate func(context.Context) error `json:"-"`
	Envelope
	PreviewID   string              `json:"previewId"`
	Intent      socialintent.Record `json:"intent"`
	Version     string              `json:"version"`
	Choice      Choice              `json:"choice"`
	ExpiresAt   time.Time           `json:"expiresAt"`
	Explanation string              `json:"explanation"`
}
type Receipt struct {
	Revalidate func(context.Context) error `json:"-"`
	Envelope
	Intent          socialintent.Record `json:"intent"`
	ActivityID      string              `json:"activityId"`
	ParticipationID string              `json:"participationId"`
	Committed       bool                `json:"committed"`
	Explanation     string              `json:"explanation"`
}
type Gateway interface {
	ListOwn(context.Context, Access, string) (List, error)
	PreviewOwn(context.Context, Access, string, Input) (Preview, error)
	ApproveOwn(context.Context, Access, string, string) (Receipt, error)
}
