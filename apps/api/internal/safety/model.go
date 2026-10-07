package safety

import (
	"context"
	"errors"
	"time"
)

var ErrTargetNotFound = errors.New("report target not available")
var ErrRateLimited = errors.New("report rate limited")

type ReportInput struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Reason     string `json:"reason"`
	Details    string `json:"details"`
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
