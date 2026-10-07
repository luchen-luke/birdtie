package socialcontext

import (
	"context"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"
)

var ErrNotFound = errors.New("shared social context unavailable")
var ErrChanged = errors.New("shared history current sources changed")
var ErrUnavailable = errors.New("shared history native current sources unavailable")

type Disclosure struct {
	MutualTies        bool `json:"mutualTies"`
	SharedCommunities bool `json:"sharedCommunities"`
	SharedActivities  bool `json:"sharedActivities"`
}

type Reference struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	StartsAt *time.Time `json:"startsAt,omitempty"`
	EndsAt   *time.Time `json:"endsAt,omitempty"`
	TimeZone string     `json:"timeZone,omitempty"`
	Modality string     `json:"modality,omitempty"`
}

type PlaceReference struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	ActivityIDs []string `json:"activityIds"`
}

type Signals struct {
	MutualCount          int              `json:"mutualCount"`
	Communities          []Reference      `json:"communities"`
	Activities           []Reference      `json:"activities"`
	CommunitiesTruncated bool             `json:"communitiesTruncated"`
	ActivitiesTruncated  bool             `json:"activitiesTruncated"`
	Places               []PlaceReference `json:"places,omitempty"`
	PlacesTruncated      bool             `json:"placesTruncated,omitempty"`
	Schema               string           `json:"schema,omitempty"`
	ViewerID             string           `json:"viewerId,omitempty"`
	TargetID             string           `json:"targetId,omitempty"`
	ObservedAt           time.Time        `json:"observedAt,omitempty"`
	ValidUntil           time.Time        `json:"validUntil,omitempty"`
	Attendance           string           `json:"attendance,omitempty"`
	Visit                string           `json:"visit,omitempty"`
}

const HistorySchema = "shared-relationship-history-v1"

type CurrentAccess struct {
	ViewerID      string
	TargetID      string
	SessionDigest [32]byte
}
type CurrentValidation func(context.Context) error
type CurrentStore interface {
	SharedSocialContextCurrent(context.Context, CurrentAccess) (Signals, CurrentValidation, error)
}

var historyUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidID(id string) bool {
	return historyUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

// This is a strict response shape check, not current permission or authority.
// The native callback must still run after encoding before response bytes leave.
func ValidateCurrent(v Signals, a CurrentAccess) error {
	if !ValidID(a.ViewerID) || !ValidID(a.TargetID) || a.ViewerID == a.TargetID || a.SessionDigest == ([32]byte{}) || v.ViewerID != a.ViewerID || v.TargetID != a.TargetID || v.Schema != HistorySchema || v.Attendance != "UNKNOWN" || v.Visit != "UNKNOWN" || v.ObservedAt.IsZero() || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > 30*time.Second || v.MutualCount < 0 || len(v.Communities) > 50 || len(v.Activities) > 50 || len(v.Places) > 50 || v.Communities == nil || v.Activities == nil {
		return ErrUnavailable
	}
	ids := map[string]bool{}
	text := func(t string) bool { return t != "" && utf8.ValidString(t) && utf8.RuneCountInString(t) <= 4096 }
	for _, r := range v.Communities {
		if !ValidID(r.ID) || !text(r.Title) || ids[r.ID] || r.StartsAt != nil || r.EndsAt != nil || r.TimeZone != "" || r.Modality != "" {
			return ErrUnavailable
		}
		ids[r.ID] = true
	}
	acts := map[string]bool{}
	for _, r := range v.Activities {
		if !ValidID(r.ID) || !text(r.Title) || acts[r.ID] || r.StartsAt == nil || r.EndsAt == nil || !r.EndsAt.After(*r.StartsAt) || r.TimeZone == "" || (r.Modality != "in_person" && r.Modality != "online" && r.Modality != "hybrid" && r.Modality != "unspecified") {
			return ErrUnavailable
		}
		acts[r.ID] = true
	}
	places := map[string]bool{}
	for _, r := range v.Places {
		if !ValidID(r.ID) || !text(r.Title) || places[r.ID] || len(r.ActivityIDs) == 0 || len(r.ActivityIDs) > 50 {
			return ErrUnavailable
		}
		places[r.ID] = true
		seen := map[string]bool{}
		for _, id := range r.ActivityIDs {
			if !acts[id] || seen[id] {
				return ErrUnavailable
			}
			seen[id] = true
		}
	}
	return nil
}

type Store interface {
	OwnSocialDisclosure(context.Context, string) (Disclosure, error)
	SetSocialDisclosure(context.Context, string, Disclosure) (Disclosure, error)
	SharedSocialContext(context.Context, string, string) (Signals, error)
}
