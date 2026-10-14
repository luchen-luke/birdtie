package safety

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrTargetNotFound = errors.New("report target not available")
var ErrRateLimited = errors.New("report rate limited")

type ReportInput struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Reason     string `json:"reason"`
	Details    string `json:"details"`
}

type ReportControlInput struct {
	OperationID string `json:"operationId"`
	Action      string `json:"action"`
	ReportInput
	Approval string `json:"approval,omitempty"`
}
type ReportControlState struct {
	RemainingQuota int                        `json:"remainingQuota"`
	CanSubmit      bool                       `json:"canSubmit"`
	ObservedAt     time.Time                  `json:"observedAt"`
	ExpiresAt      time.Time                  `json:"expiresAt"`
	ResponseProof  identity.HumanControlProof `json:"-"`
}
type ReportControlPreview struct {
	identity.HumanControlPreview
	ReportInput
}
type ReportControlStore interface {
	ReadReportControl(context.Context, [32]byte, identity.Actor) (ReportControlState, error)
	PreviewReportControl(context.Context, [32]byte, identity.Actor, ReportControlInput) (ReportControlPreview, error)
	CommitReportControl(context.Context, [32]byte, identity.Actor, ReportControlInput) (identity.HumanControlReceipt, error)
	ReadReportControlOperation(context.Context, [32]byte, identity.Actor, string) (identity.HumanControlReceipt, error)
	StopReportControlOperation(context.Context, [32]byte, identity.Actor, string) (identity.HumanControlReceipt, error)
}

func NormalizeReportControl(in ReportControlInput, commit bool) (ReportControlInput, error) {
	in.Details = strings.TrimSpace(in.Details)
	if !identity.HumanControlUUID(in.OperationID) || in.Action != "SUBMIT" || !ValidTarget(in.TargetType, in.TargetID) || (in.TargetID != "" && !identity.HumanControlUUID(in.TargetID)) || !ValidReason(in.Reason) || !utf8.ValidString(in.Details) || strings.ContainsRune(in.Details, 0) || len([]rune(in.Details)) < 10 || len([]rune(in.Details)) > 1000 || !identity.HumanControlApprovalValid(in.Approval, commit) {
		return in, identity.ErrControlInvalid
	}
	return in, nil
}
func ReportControlDigest(in ReportControlInput) string {
	return identity.HumanControlTupleHash([]any{"human-report-v1", in.OperationID, in.Action, in.TargetType, in.TargetID, in.Reason, identity.HumanControlTextHash(in.Details)})
}
func DecodeReportControl(raw []byte, commit bool) (ReportControlInput, error) {
	keys := []string{"operationId", "action", "targetType", "targetId", "reason", "details"}
	if commit {
		keys = append(keys, "approval")
	}
	var in ReportControlInput
	if identity.HumanControlClosedJSON(raw, keys, nil) != nil || json.Unmarshal(raw, &in) != nil {
		return in, identity.ErrControlInvalid
	}
	return NormalizeReportControl(in, commit)
}

type Report struct {
	ID         string    `json:"id"`
	TargetType string    `json:"targetType"`
	TargetID   *string   `json:"targetId"`
	Reason     string    `json:"reason"`
	Details    string    `json:"details"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Store interface {
	CreateReport(context.Context, string, ReportInput) (Report, error)
	ListOwnReports(context.Context, string) ([]Report, error)
}

func ValidTarget(kind, id string) bool {
	switch kind {
	case "general":
		return id == ""
	case "activity", "organization", "account", "message", "activity_message", "community_message", "community", "business":
		return id != ""
	default:
		return false
	}
}

func ValidReason(reason string) bool {
	switch reason {
	case "safety", "harassment", "incorrect_information", "technical", "other":
		return true
	default:
		return false
	}
}
