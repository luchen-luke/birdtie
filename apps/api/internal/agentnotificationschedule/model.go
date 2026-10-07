// Package agentnotificationschedule describes explicit ordinary human Inbox
// schedules. It grants neither inference nor a message, push or model tool.
package agentnotificationschedule

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"math"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

const SchemaVersion = "native-notification-schedule-v1"
const MaxBodyBytes = 4096
const MaxContactsPerDay = 20
const MaxOwnersPerRun = 100
const MaxDuration = 30 * 24 * time.Hour
const GapSkip = "SKIP"
const FoldEarlierOnce = "EARLIER_ONCE"
const BudgetWindowHours = 24

var (
	ErrInvalid     = errors.New("通知计划格式无效")
	ErrDenied      = errors.New("当前无权管理通知计划")
	ErrChanged     = errors.New("通知计划或来源已更新")
	ErrUnavailable = errors.New("通知计划暂不可用")
	ErrServerOnly  = errors.New("调度控制只供原生服务使用")
)

type QuietWindow struct {
	StartMinute int `json:"startMinute"`
	EndMinute   int `json:"endMinute"`
}

// Enabled=false stops future scheduled deliveries. Unlike disabling the old
// notification classifier, it never restores/replays pending DIGEST as NORMAL.
type Settings struct {
	Enabled           bool                         `json:"enabled"`
	TimeZone          string                       `json:"timeZone"`
	LocalMinute       int                          `json:"localMinute"`
	GapPolicy         string                       `json:"gapPolicy"`
	FoldPolicy        string                       `json:"foldPolicy"`
	Quiet             *QuietWindow                 `json:"quiet"`
	MaxContactsPerDay int                          `json:"maxContactsPerDay"`
	Categories        []agentnotification.Category `json:"categories"`
}
type PutInput struct {
	ExpectedVersion uint64 `json:"expectedVersion"`
	Settings
	ExpiresAt time.Time `json:"expiresAt"`
}
type Policy struct {
	SchemaVersion     string `json:"schemaVersion"`
	Version           uint64 `json:"version"`
	AgentID           string `json:"agentId"`
	Configured        bool   `json:"configured"`
	Status            string `json:"status"`
	BudgetWindowHours int    `json:"budgetWindowHours"`
	Settings
	ValidFrom *time.Time `json:"validFrom,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}
type Store interface {
	GetOwnNotificationSchedule(context.Context, agentprofile.PrivateAccess) (Policy, error)
	PutOwnNotificationSchedule(context.Context, agentprofile.PrivateAccess, PutInput) (Policy, error)
}

func validID(v string) bool {
	p, e := actorref.ParsePrincipal("PERSON", v)
	return e == nil && p.ID == v && v != "00000000-0000-0000-0000-000000000000"
}
func validTime(v time.Time) bool {
	return !v.IsZero() && v.Year() >= 1 && v.Year() <= 9999 && v.Equal(v.Truncate(time.Microsecond))
}
func Location(v string) (*time.Location, error) {
	if v == "" || len(v) > 64 || v == "Local" || strings.ContainsAny(v, "\\\x00") || strings.HasPrefix(v, "/") || strings.Contains(v, "..") {
		return nil, ErrInvalid
	}
	if v != "UTC" && !strings.Contains(v, "/") {
		return nil, ErrInvalid
	}
	loc, e := time.LoadLocation(v)
	if e != nil {
		return nil, ErrInvalid
	}
	return loc, nil
}
func NormalizeSettings(in Settings) (Settings, error) {
	if _, e := Location(in.TimeZone); e != nil || in.LocalMinute < 0 || in.LocalMinute >= 1440 || in.GapPolicy != GapSkip || in.FoldPolicy != FoldEarlierOnce || in.MaxContactsPerDay < 0 || in.MaxContactsPerDay > MaxContactsPerDay || len(in.Categories) == 0 || len(in.Categories) > 8 {
		return Settings{}, ErrInvalid
	}
	if in.Quiet != nil {
		q := *in.Quiet
		if q.StartMinute < 0 || q.StartMinute >= 1440 || q.EndMinute < 0 || q.EndMinute >= 1440 || q.StartMinute == q.EndMinute {
			return Settings{}, ErrInvalid
		}
		in.Quiet = &q
	}
	in.Categories = append([]agentnotification.Category(nil), in.Categories...)
	seen := map[agentnotification.Category]bool{}
	for _, v := range in.Categories {
		if !agentnotification.ValidCategory(v) || seen[v] {
			return Settings{}, ErrInvalid
		}
		seen[v] = true
	}
	sort.Slice(in.Categories, func(i, j int) bool { return in.Categories[i] < in.Categories[j] })
	return in, nil
}
func NormalizePut(in PutInput, now time.Time) (PutInput, error) {
	if in.ExpectedVersion >= math.MaxInt64 || !validTime(now) || !validTime(in.ExpiresAt) || !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(MaxDuration)) {
		return PutInput{}, ErrInvalid
	}
	s, e := NormalizeSettings(in.Settings)
	if e != nil {
		return PutInput{}, e
	}
	in.Settings = s
	in.ExpiresAt = in.ExpiresAt.UTC()
	return in, nil
}
func DefaultPolicy(agentID string) (Policy, error) {
	if !validID(agentID) {
		return Policy{}, ErrUnavailable
	}
	return Policy{SchemaVersion: SchemaVersion, AgentID: agentID, Status: "UNCONFIGURED", BudgetWindowHours: BudgetWindowHours, Settings: Settings{Categories: []agentnotification.Category{}}}, nil
}
func ValidatePolicy(p Policy) error {
	if p.SchemaVersion != SchemaVersion || !validID(p.AgentID) || p.Version > math.MaxInt64 || p.BudgetWindowHours != BudgetWindowHours {
		return ErrUnavailable
	}
	if !p.Configured {
		if p.Version != 0 || p.Status != "UNCONFIGURED" || p.Enabled || p.TimeZone != "" || p.LocalMinute != 0 || p.GapPolicy != "" || p.FoldPolicy != "" || p.Quiet != nil || p.MaxContactsPerDay != 0 || len(p.Categories) != 0 || p.ValidFrom != nil || p.ExpiresAt != nil || p.UpdatedAt != nil {
			return ErrUnavailable
		}
		return nil
	}
	if p.Version == 0 || p.ValidFrom == nil || p.ExpiresAt == nil || p.UpdatedAt == nil || !validTime(*p.ValidFrom) || !validTime(*p.ExpiresAt) || !validTime(*p.UpdatedAt) || !p.ExpiresAt.After(*p.ValidFrom) || p.ExpiresAt.After(p.ValidFrom.Add(MaxDuration)) || !p.UpdatedAt.Equal(*p.ValidFrom) {
		return ErrUnavailable
	}
	if _, e := NormalizeSettings(p.Settings); e != nil {
		return ErrUnavailable
	}
	switch p.Status {
	case "ACTIVE":
		if !p.Enabled {
			return ErrUnavailable
		}
	case "DISABLED":
		if p.Enabled {
			return ErrUnavailable
		}
	case "EXPIRED", "PAUSED_SESSION":
	default:
		return ErrUnavailable
	}
	return nil
}
func AllowsCategory(s Settings, c agentnotification.Category) bool {
	for _, v := range s.Categories {
		if v == c {
			return true
		}
	}
	return false
}
