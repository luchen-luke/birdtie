package content

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const MomentHistoryPageSize = 20

var ErrMomentHistoryChanged = errors.New("moment_history_changed")

// History is a bounded read of current authority. Following and friends are
// author filters over PUBLIC Moments, never a new Moment audience.
type MomentHistoryQuery struct {
	Scope, CityID, AuthorID, From, To, Status, Cursor string
}

type OwnMomentHistoryPage struct {
	SchemaVersion string    `json:"schemaVersion"`
	Status        string    `json:"status"`
	Items         []Moment  `json:"items"`
	ObservedAt    time.Time `json:"observedAt"`
	SnapshotAt    time.Time `json:"snapshotAt"`
	ValidUntil    time.Time `json:"validUntil"`
	NextCursor    *string   `json:"nextCursor"`
	RefreshCursor string    `json:"refreshCursor"`
	ResponseProof any       `json:"-"`
}

type PublicMomentHistoryPage struct {
	SchemaVersion string                    `json:"schemaVersion"`
	Scope         string                    `json:"scope"`
	CityID        string                    `json:"cityId"`
	AuthorID      string                    `json:"authorId"`
	From          string                    `json:"from"`
	To            string                    `json:"to"`
	Items         []PublicMomentWithContext `json:"items"`
	ObservedAt    time.Time                 `json:"observedAt"`
	SnapshotAt    time.Time                 `json:"snapshotAt"`
	ValidUntil    time.Time                 `json:"validUntil"`
	NextCursor    *string                   `json:"nextCursor"`
	RefreshCursor string                    `json:"refreshCursor"`
	ResponseProof any                       `json:"-"`
}

type MomentHistoryStore interface {
	ReadOwnMomentHistory(context.Context, [32]byte, identity.Actor, MomentHistoryQuery) (OwnMomentHistoryPage, error)
	RevalidateOwnMomentHistory(context.Context, [32]byte, identity.Actor, MomentHistoryQuery, OwnMomentHistoryPage) error
	ReadPublicMomentHistory(context.Context, PublicMomentAccess, MomentHistoryQuery) (PublicMomentHistoryPage, error)
	RevalidatePublicMomentHistory(context.Context, PublicMomentAccess, MomentHistoryQuery, PublicMomentHistoryPage) error
}

func ValidateMomentHistoryQuery(q MomentHistoryQuery, own bool) error {
	if len(q.Cursor) > 4096 {
		return ErrInvalid
	}
	if own {
		if q.Scope != "" || q.CityID != "" || q.AuthorID != "" || q.From != "" || q.To != "" {
			return ErrInvalid
		}
		switch q.Status {
		case "active", "draft", "published":
			return nil
		}
		return ErrInvalid
	}
	if q.Status != "" {
		return ErrInvalid
	}
	switch q.Scope {
	case "city", "following", "friends":
		if q.CityID == "" || q.AuthorID != "" {
			return ErrInvalid
		}
	case "author":
		if !momentUUID.MatchString(q.AuthorID) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if len(q.CityID) > 80 || !utf8.ValidString(q.CityID) || strings.ContainsRune(q.CityID, 0) || strings.TrimSpace(q.CityID) != q.CityID {
		return ErrInvalid
	}
	var from, to time.Time
	var e error
	if q.From != "" {
		from, e = time.Parse(time.RFC3339Nano, q.From)
		if e != nil || !strings.HasSuffix(q.From, "Z") || len(q.From) > 30 {
			return ErrInvalid
		}
	}
	if q.To != "" {
		to, e = time.Parse(time.RFC3339Nano, q.To)
		if e != nil || !strings.HasSuffix(q.To, "Z") || len(q.To) > 30 {
			return ErrInvalid
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return ErrInvalid
	}
	return nil
}

func ValidateOwnMomentHistoryPage(p OwnMomentHistoryPage, q MomentHistoryQuery, owner string) error {
	if p.RefreshCursor == "" || len(p.RefreshCursor) > 4096 {
		return ErrInvalid
	}
	if p.SchemaVersion != "owner-moment-history-v1" || p.Status != q.Status || p.Items == nil || len(p.Items) > MomentHistoryPageSize || p.ObservedAt.IsZero() || p.SnapshotAt.After(p.ObservedAt) || !p.ValidUntil.After(p.ObservedAt) || p.ValidUntil.Sub(p.ObservedAt) > time.Minute || (p.NextCursor != nil && (*p.NextCursor == "" || len(p.Items) != MomentHistoryPageSize)) {
		return ErrInvalid
	}
	ids := map[string]bool{}
	for i, m := range p.Items {
		if !momentUUID.MatchString(m.ID) || ids[m.ID] || m.AuthorAccountID != owner || m.Status == "withdrawn" || (q.Status != "active" && m.Status != q.Status) || m.CreatedAt.After(p.SnapshotAt) {
			return ErrInvalid
		}
		if i > 0 && (m.CreatedAt.After(p.Items[i-1].CreatedAt) || m.CreatedAt.Equal(p.Items[i-1].CreatedAt) && m.ID >= p.Items[i-1].ID) {
			return ErrInvalid
		}
		ids[m.ID] = true
	}
	return nil
}

func ValidatePublicMomentHistoryPage(p PublicMomentHistoryPage, q MomentHistoryQuery) error {
	if p.RefreshCursor == "" || len(p.RefreshCursor) > 4096 {
		return ErrInvalid
	}
	if p.SchemaVersion != "public-moment-history-v1" || p.Scope != q.Scope || p.CityID != q.CityID || p.AuthorID != q.AuthorID || p.From != q.From || p.To != q.To || p.Items == nil || len(p.Items) > MomentHistoryPageSize || p.ObservedAt.IsZero() || p.SnapshotAt.After(p.ObservedAt) || !p.ValidUntil.After(p.ObservedAt) || p.ValidUntil.Sub(p.ObservedAt) > time.Minute || (p.NextCursor != nil && (*p.NextCursor == "" || len(p.Items) != MomentHistoryPageSize)) {
		return ErrInvalid
	}
	ids := map[string]bool{}
	for i, m := range p.Items {
		if ValidatePublicMomentWithContext(m, m.ID) != nil || ids[m.ID] || m.PublishedAt.After(p.SnapshotAt) || (q.CityID != "" && m.CityID != q.CityID) || m.ValidUntil.Before(p.ValidUntil) {
			return ErrInvalid
		}
		if i > 0 && (m.PublishedAt.After(p.Items[i-1].PublishedAt) || m.PublishedAt.Equal(p.Items[i-1].PublishedAt) && m.ID >= p.Items[i-1].ID) {
			return ErrInvalid
		}
		ids[m.ID] = true
	}
	return nil
}
