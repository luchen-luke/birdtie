package placehistory

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
)

const PageSchemaVersion = "place-social-history-page-v1"
const PageSize = 5
const PageLease = 2 * time.Minute
const pageStamp = "2006-01-02T15:04:05.000000Z"

var ErrChanged = errors.New("地点公开摘要来源已变化")

// A cursor only locates a page and fences mixed generations. It never grants
// access. Every use repeats current native resource, Block and field ACLs.
type PageCursor struct {
	Version    int    `json:"version"`
	PlaceID    string `json:"placeId"`
	WithAuthor bool   `json:"withAuthor"`
	WindowEnd  string `json:"windowEnd"`
	ValidUntil string `json:"validUntil"`
	BeforeAt   string `json:"beforeAt"`
	BeforeID   string `json:"beforeId"`
	Source     string `json:"source"`
}
type PageQuery struct {
	PlaceID    string
	WithAuthor bool
	Cursor     *PageCursor
}

func PageStamp(t time.Time) string { return t.UTC().Format(pageStamp) }
func CursorTimes(c PageCursor) (time.Time, time.Time, time.Time, error) {
	var values [3]time.Time
	for i, v := range []string{c.WindowEnd, c.ValidUntil, c.BeforeAt} {
		t, e := time.Parse(pageStamp, v)
		if e != nil || PageStamp(t) != v {
			return time.Time{}, time.Time{}, time.Time{}, ErrInvalid
		}
		values[i] = t
	}
	if !values[1].After(values[0]) || values[1].Sub(values[0]) > PageLease || values[2].After(values[0]) || values[2].Before(values[0].Add(-WindowDays*24*time.Hour)) {
		return time.Time{}, time.Time{}, time.Time{}, ErrInvalid
	}
	return values[0], values[1], values[2], nil
}
func ValidatePageCursor(c PageCursor, place string, author bool) error {
	if c.Version != 1 || c.PlaceID != place || !pp.ValidID(place) || c.WithAuthor != author || !pp.ValidID(c.BeforeID) || len(c.Source) != 64 || strings.ToLower(c.Source) != c.Source {
		return ErrInvalid
	}
	if _, e := hex.DecodeString(c.Source); e != nil {
		return ErrInvalid
	}
	_, _, _, e := CursorTimes(c)
	return e
}
func EncodePageCursor(c PageCursor) (string, error) {
	if ValidatePageCursor(c, c.PlaceID, c.WithAuthor) != nil {
		return "", ErrInvalid
	}
	b, e := json.Marshal(c)
	if e != nil {
		return "", ErrInvalid
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func DecodePageCursor(raw, place string, author bool) (PageCursor, error) {
	var c PageCursor
	if len(raw) < 1 || len(raw) > 1024 {
		return c, ErrInvalid
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(raw)
	if e != nil || base64.RawURLEncoding.EncodeToString(b) != raw {
		return c, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return c, ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF || ValidatePageCursor(c, place, author) != nil {
		return c, ErrInvalid
	}
	// Canonical round-trip also rejects duplicate keys, whitespace and alternate
	// escape forms; clients retain the opaque string without rewriting it.
	canonical, e := json.Marshal(c)
	if e != nil || !bytes.Equal(b, canonical) {
		return c, ErrInvalid
	}
	return c, nil
}

type Page struct {
	SchemaVersion string             `json:"schemaVersion"`
	PlaceID       string             `json:"placeId"`
	CityID        string             `json:"cityId"`
	AuthorMode    string             `json:"authorMode"`
	WindowDays    int                `json:"windowDays"`
	WindowStart   time.Time          `json:"windowStart"`
	WindowEnd     time.Time          `json:"windowEnd"`
	ObservedAt    time.Time          `json:"observedAt"`
	ValidUntil    time.Time          `json:"validUntil"`
	SourceVersion string             `json:"sourceVersion"`
	Count         int                `json:"count"`
	Items         []MomentWithAuthor `json:"items"`
	NextCursor    *string            `json:"nextCursor"`
	ResponseProof any                `json:"-"`
}
type PageStore interface {
	ReadPublicPlaceHistoryPage(context.Context, Access, PageQuery) (Page, error)
	RevalidatePublicPlaceHistoryPage(context.Context, Access, PageQuery, Page) error
}

func ValidatePage(p Page, q PageQuery) error {
	mode := "NONE"
	if q.WithAuthor {
		mode = "PUBLIC_PROFILE"
	}
	if p.SchemaVersion != PageSchemaVersion || p.PlaceID != q.PlaceID || !pp.ValidID(q.PlaceID) || p.AuthorMode != mode || p.CityID == "" || len(p.CityID) > 80 || p.WindowDays != WindowDays || p.WindowEnd.IsZero() || p.WindowEnd.After(p.ObservedAt) || !p.WindowStart.Equal(p.WindowEnd.Add(-WindowDays*24*time.Hour)) || !p.ValidUntil.After(p.ObservedAt) || p.ValidUntil.Sub(p.WindowEnd) > PageLease || len(p.SourceVersion) != 64 || strings.ToLower(p.SourceVersion) != p.SourceVersion || p.Count < 0 || p.Items == nil || len(p.Items) > PageSize || len(p.Items) > p.Count {
		return ErrInvalid
	}
	if _, e := hex.DecodeString(p.SourceVersion); e != nil {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for i, m := range p.Items {
		if !pp.ValidID(m.ID) || seen[m.ID] || m.Title == "" || !utf8.ValidString(m.Title) || len(m.Title) > 160 || !utf8.ValidString(m.Excerpt) || len([]rune(m.Excerpt)) > 280 || m.Revision < 2 || m.PublishedAt.Before(p.WindowStart) || m.PublishedAt.After(p.WindowEnd) || content.ValidatePublicMomentAuthor(m.AuthorID, m.AuthorPublicProfile) != nil || (!q.WithAuthor && (m.AuthorID != nil || m.AuthorPublicProfile != nil)) {
			return ErrInvalid
		}
		if i > 0 {
			prev := p.Items[i-1]
			if m.PublishedAt.After(prev.PublishedAt) || (m.PublishedAt.Equal(prev.PublishedAt) && m.ID >= prev.ID) {
				return ErrInvalid
			}
		}
		if q.Cursor != nil {
			_, _, before, e := CursorTimes(*q.Cursor)
			if e != nil || m.PublishedAt.After(before) || (m.PublishedAt.Equal(before) && m.ID >= q.Cursor.BeforeID) {
				return ErrInvalid
			}
		}
		seen[m.ID] = true
	}
	if p.NextCursor != nil {
		if len(p.Items) != PageSize {
			return ErrInvalid
		}
		c, e := DecodePageCursor(*p.NextCursor, p.PlaceID, q.WithAuthor)
		last := p.Items[len(p.Items)-1]
		if e != nil || c.BeforeID != last.ID || c.BeforeAt != PageStamp(last.PublishedAt) || c.WindowEnd != PageStamp(p.WindowEnd) || c.ValidUntil != PageStamp(p.ValidUntil) || c.Source != p.SourceVersion {
			return ErrInvalid
		}
	}
	if q.Cursor != nil && (q.Cursor.WindowEnd != PageStamp(p.WindowEnd) || q.Cursor.ValidUntil != PageStamp(p.ValidUntil) || q.Cursor.Source != p.SourceVersion) {
		return ErrInvalid
	}
	if q.Cursor == nil && p.NextCursor == nil && len(p.Items) != p.Count {
		return ErrInvalid
	}
	return nil
}
