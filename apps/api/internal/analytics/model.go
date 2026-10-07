package analytics

import (
	"context"
	"errors"
)

var ErrForbidden = errors.New("organization analytics forbidden")
var ErrNotFound = errors.New("activity not available for analytics")

type ActivityMetrics struct {
	ActivityID  string          `json:"activityId"`
	Title       string          `json:"title"`
	Impressions int64           `json:"impressions"`
	DetailOpens int64           `json:"detailOpens"`
	RSVPs       int64           `json:"rsvps"`
	Saves       int64           `json:"saves"`
	Sources     []SourceMetrics `json:"sources"`
}

type SourceMetrics struct {
	Source      string `json:"source"`
	Impressions int64  `json:"impressions"`
	DetailOpens int64  `json:"detailOpens"`
	RSVPs       int64  `json:"rsvps"`
	Saves       int64  `json:"saves"`
}

type Store interface {
	RecordActivityEvent(context.Context, string, string, string, string) error
	OrganizationActivityMetrics(context.Context, string, string) ([]ActivityMetrics, error)
}

func ValidViewEvent(kind, source string) bool {
	return (kind == "impression" || kind == "detail") && ValidSource(source)
}

func ValidSource(source string) bool {
	switch source {
	case "now", "agent", "map", "organization", "inbox", "direct", "unknown":
		return true
	default:
		return false
	}
}
