package saved

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const MaxSearchBytes = 240
const MaxSearchCursorBytes = 1024

type Query struct {
	Text   string `json:"q"`
	Kind   string `json:"kind"`
	CityID string `json:"cityId"`
}

// Query searches current readable entity labels, including the person's exact
// retained imported references. Private bookmark notes are not search inputs,
// ranking or Agent sources.
type HumanSearchPageStore interface {
	SearchHumanSavedPage(context.Context, [32]byte, identity.Actor, Query, string) (Page, error)
	ValidateHumanSavedSearchPage(context.Context, [32]byte, identity.Actor, Query, string, Page) error
}

func NormalizeQuery(q Query) (Query, error) {
	for _, s := range []string{q.Text, q.Kind, q.CityID} {
		if !utf8.ValidString(s) {
			return Query{}, ErrInvalidPage
		}
		for _, r := range s {
			if unicode.IsControl(r) {
				return Query{}, ErrInvalidPage
			}
		}
	}
	q.Text, q.Kind, q.CityID = strings.TrimSpace(q.Text), strings.TrimSpace(q.Kind), strings.TrimSpace(q.CityID)
	if len(q.Text) > MaxSearchBytes || len(q.CityID) > 80 || q.Kind != "" && q.Kind != "place" && q.Kind != "activity" && q.Kind != "group" {
		return Query{}, ErrInvalidPage
	}
	return q, nil
}
func (q Query) Filtered() bool   { return q.Text != "" || q.Kind != "" || q.CityID != "" }
func (q Query) Digest() [32]byte { b, _ := json.Marshal(q); return sha256.Sum256(b) }

var searchCursorAlphabet = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ValidSearchCursor(cursor string) bool {
	if cursor == "" {
		return true
	}
	if len(cursor) > MaxSearchCursorBytes || !searchCursorAlphabet.MatchString(cursor) {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(cursor)
	return e == nil && len(b) >= 28 && base64.RawURLEncoding.EncodeToString(b) == cursor
}
func ValidateSearchPage(p Page, q Query, cursor string) error {
	n, e := NormalizeQuery(q)
	if e != nil || n != q || !q.Filtered() || !ValidSearchCursor(cursor) {
		return ErrInvalidPage
	}
	plain := p
	plain.NextCursor = nil
	if ValidatePage(plain, "") != nil {
		return ErrInvalidPage
	}
	for _, i := range p.Items {
		if (!i.Available && i.LegacyReference == nil) || q.Kind != "" && i.Kind != q.Kind || q.CityID != "" && i.CityID != q.CityID {
			return ErrInvalidPage
		}
	}
	if p.NextCursor != nil && (len(p.Items) != PageSize || *p.NextCursor == "" || *p.NextCursor == cursor || !ValidSearchCursor(*p.NextCursor)) {
		return ErrInvalidPage
	}
	return nil
}
