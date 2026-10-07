package placehistory

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"time"
	"unicode/utf8"
)

const SchemaVersion = "place-social-history-v1"
const WindowDays = 30
const MaxMoments = 5

var ErrInvalid = errors.New("地点公开摘要参数无效")
var ErrNotFound = errors.New("地点当前不可见")
var ErrUnavailable = errors.New("地点公开摘要暂不可用")

type Access struct {
	SessionDigest [32]byte
	Actor         identity.Actor
}

func ValidateAccess(a Access) error {
	if a.Actor.ID == "" && a.Actor.AccountType == "" && a.SessionDigest == ([32]byte{}) {
		return nil
	}
	if !pp.ValidID(a.Actor.ID) || a.SessionDigest == ([32]byte{}) {
		return ErrInvalid
	}
	switch a.Actor.AccountType {
	case "person", "organization", "business":
		return nil
	default:
		return ErrInvalid
	}
}

type Moment struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Excerpt     string    `json:"excerpt"`
	Revision    int64     `json:"revision"`
	PublishedAt time.Time `json:"publishedAt"`
}

// Activity buckets describe public retained arrangements only. There are no
// attendees, participants, inferred preferences or private activity IDs here.
type ActivityPattern struct {
	Category     string `json:"category"`
	DayKind      string `json:"dayKind"`
	Arrangements int    `json:"arrangements"`
}
type Summary struct {
	SchemaVersion     string            `json:"schemaVersion"`
	PlaceID           string            `json:"placeId"`
	CityID            string            `json:"cityId"`
	WindowDays        int               `json:"windowDays"`
	WindowStart       time.Time         `json:"windowStart"`
	CheckedAt         time.Time         `json:"checkedAt"`
	RecentMomentCount int               `json:"recentMomentCount"`
	RecentMoments     []Moment          `json:"recentMoments"`
	ActivityPatterns  []ActivityPattern `json:"activityPatterns"`
	Suitability       *pp.Public        `json:"suitability"`
}
type Store interface {
	GetPublicPlaceSocialHistory(context.Context, Access, string) (Summary, error)
}

func ValidateSummary(s Summary) error {
	if s.SchemaVersion != SchemaVersion || !pp.ValidID(s.PlaceID) || s.CityID == "" || len(s.CityID) > 80 || s.WindowDays != WindowDays || s.CheckedAt.IsZero() || !s.WindowStart.Equal(s.CheckedAt.Add(-WindowDays*24*time.Hour)) || s.RecentMomentCount < 0 || len(s.RecentMoments) > MaxMoments || len(s.RecentMoments) > s.RecentMomentCount || len(s.ActivityPatterns) > 200 {
		return ErrInvalid
	}
	if s.RecentMoments == nil || s.ActivityPatterns == nil {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for i, m := range s.RecentMoments {
		if !pp.ValidID(m.ID) || seen[m.ID] || !utf8.ValidString(m.Title) || m.Title == "" || len(m.Title) > 160 || !utf8.ValidString(m.Excerpt) || len([]rune(m.Excerpt)) > 280 || m.Revision < 2 || m.PublishedAt.Before(s.WindowStart) || m.PublishedAt.After(s.CheckedAt) {
			return ErrInvalid
		}
		if i > 0 && s.RecentMoments[i-1].PublishedAt.Before(m.PublishedAt) {
			return ErrInvalid
		}
		seen[m.ID] = true
	}
	buckets := map[string]bool{}
	for _, p := range s.ActivityPatterns {
		key := p.Category + ":" + p.DayKind
		if len(p.Category) > 80 || !utf8.ValidString(p.Category) || p.Arrangements < 1 || p.Arrangements > 1000000 || (p.DayKind != "WEEKEND" && p.DayKind != "WEEKDAY") || buckets[key] {
			return ErrInvalid
		}
		buckets[key] = true
	}
	if s.Suitability != nil && (s.Suitability.PlaceID != s.PlaceID || s.Suitability.CityID != s.CityID || !s.Suitability.CheckedAt.Equal(s.CheckedAt) || pp.ValidatePublic(*s.Suitability, s.CheckedAt) != nil) {
		return ErrInvalid
	}
	return nil
}
