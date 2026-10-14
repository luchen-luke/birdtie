// Package legacyarchive describes a private historical archive, not a Moment,
// current Place, media permission, or executable material.
package legacyarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const SourceSystem = "hangzhou-civu-20261014"
const PageSize = 20
const MaxVersion int64 = 8999999999999999

var ErrInvalid = errors.New("invalid legacy archive")
var ErrNotFound = errors.New("legacy archive unavailable")
var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var numberRE = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

type SourceSelector struct {
	System string `json:"system"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
}
type Summary struct {
	ID                     string                     `json:"id"`
	Source                 SourceSelector             `json:"source"`
	SourceVersion          int64                      `json:"sourceVersion"`
	SourceSHA256           string                     `json:"sourceSha256"`
	Deleted                bool                       `json:"deleted"`
	Title                  string                     `json:"title"`
	RecordedStartDate      *string                    `json:"recordedStartDate"`
	RecordedEndDate        *string                    `json:"recordedEndDate"`
	SourceVisibility       string                     `json:"sourceVisibility"`
	SourceModerationStatus string                     `json:"sourceModerationStatus"`
	ImportedAt             time.Time                  `json:"importedAt"`
	ResponseProof          identity.HumanControlProof `json:"-"`
}
type Detail struct {
	Summary
	Body          string         `json:"body"`
	Relationships *Relationships `json:"relationships,omitempty"`
}
type Page struct {
	Items         []Summary                  `json:"items"`
	NextCursor    string                     `json:"nextCursor,omitempty"`
	ResponseProof identity.HumanControlProof `json:"-"`
}
type PlaceSelector struct {
	SourcePlaceID *string `json:"sourcePlaceId"`
	RawName       string  `json:"rawName"`
	Position      int64   `json:"position"`
}
type MediaSelector struct {
	SourceMediaID string `json:"sourceMediaId"`
	Position      int64  `json:"position"`
	State         string `json:"state"`
}
type MediaLocation struct {
	SourceMediaID  string       `json:"sourceMediaId"`
	Latitude       *json.Number `json:"latitude"`
	Longitude      *json.Number `json:"longitude"`
	MapEnabled     bool         `json:"mapEnabled"`
	SourcePlaceIDs []string     `json:"sourcePlaceIds"`
	CapturedDate   *string      `json:"capturedDate"`
}
type Thread struct {
	SourceThreadID string `json:"sourceThreadId"`
	Position       *int64 `json:"position"`
	EntryCount     int64  `json:"entryCount"`
	RouteRevision  int64  `json:"routeRevision"`
	CreatedAt      string `json:"createdAt"`
}
type ThreadLeg struct {
	SourceFromNoteID        string  `json:"sourceFromNoteId"`
	SourceToNoteID          string  `json:"sourceToNoteId"`
	FromPosition            int64   `json:"fromPosition"`
	ToPosition              int64   `json:"toPosition"`
	Mode                    string  `json:"mode"`
	SourceFromAnchorPlaceID *string `json:"sourceFromAnchorPlaceId"`
	SourceToAnchorPlaceID   *string `json:"sourceToAnchorPlaceId"`
	CreatedAt               string  `json:"createdAt"`
	UpdatedAt               string  `json:"updatedAt"`
}
type Relationships struct {
	Places         []PlaceSelector `json:"places"`
	Media          []MediaSelector `json:"media"`
	MediaLocations []MediaLocation `json:"mediaLocations"`
	Thread         *Thread         `json:"thread"`
	ThreadLegs     []ThreadLeg     `json:"threadLegs"`
}
type HumanStore interface {
	ListHumanLegacyArchives(context.Context, [32]byte, identity.Actor, string) (Page, error)
	ReadHumanLegacyArchive(context.Context, [32]byte, identity.Actor, string) (Detail, error)
	FinalizeHumanLegacyArchiveRead(context.Context, [32]byte, identity.Actor, Detail) error
	FinalizeHumanLegacyArchivePage(context.Context, [32]byte, identity.Actor, Page) error
}

func UUID(v string) bool          { return uuidRE.MatchString(v) }
func text(v string) bool          { return utf8.ValidString(v) && !strings.ContainsRune(v, 0) }
func optionalText(v *string) bool { return v == nil || text(*v) }
func optionalID(v *string) bool   { return v == nil || UUID(*v) }
func ValidateSummary(v Summary) error {
	if !UUID(v.ID) || v.Source.System != SourceSystem || v.Source.Kind != "note" || !UUID(v.Source.ID) || v.SourceVersion < 1 || v.SourceVersion > MaxVersion || !shaRE.MatchString(v.SourceSHA256) || v.ImportedAt.IsZero() || !text(v.Title) || !optionalText(v.RecordedStartDate) || !optionalText(v.RecordedEndDate) || !text(v.SourceVisibility) || !text(v.SourceModerationStatus) {
		return ErrInvalid
	}
	if v.Deleted && (v.Title != "" || v.RecordedStartDate != nil || v.RecordedEndDate != nil || v.SourceVisibility != "" || v.SourceModerationStatus != "") {
		return ErrInvalid
	}
	return nil
}
func ValidateRelationships(v Relationships) error {
	if v.Places == nil || v.Media == nil || v.MediaLocations == nil || v.ThreadLegs == nil {
		return ErrInvalid
	}
	placePositions, mediaPositions := map[int64]bool{}, map[int64]bool{}
	for _, p := range v.Places {
		if !optionalID(p.SourcePlaceID) || !text(p.RawName) || p.Position < 0 || placePositions[p.Position] {
			return ErrInvalid
		}
		placePositions[p.Position] = true
	}
	for _, m := range v.Media {
		if !UUID(m.SourceMediaID) || m.Position < 0 || m.State != "UNAVAILABLE" || mediaPositions[m.Position] {
			return ErrInvalid
		}
		mediaPositions[m.Position] = true
	}
	locations := map[string]bool{}
	for _, m := range v.MediaLocations {
		if !UUID(m.SourceMediaID) || locations[m.SourceMediaID] || m.SourcePlaceIDs == nil || !optionalText(m.CapturedDate) {
			return ErrInvalid
		}
		locations[m.SourceMediaID] = true
		for _, n := range []*json.Number{m.Latitude, m.Longitude} {
			if n != nil && !numberRE.MatchString(n.String()) {
				return ErrInvalid
			}
		}
		for _, id := range m.SourcePlaceIDs {
			if !UUID(id) {
				return ErrInvalid
			}
		}
	}
	if v.Thread != nil {
		t := v.Thread
		if !UUID(t.SourceThreadID) || t.EntryCount < 0 || t.RouteRevision < 0 || !text(t.CreatedAt) || (t.Position != nil && *t.Position < 0) {
			return ErrInvalid
		}
	} else if len(v.ThreadLegs) > 0 {
		return ErrInvalid
	}
	positions := map[int64]bool{}
	for _, l := range v.ThreadLegs {
		if !UUID(l.SourceFromNoteID) || !UUID(l.SourceToNoteID) || l.FromPosition < 0 || l.ToPosition < 0 || positions[l.FromPosition] || !text(l.Mode) || !optionalID(l.SourceFromAnchorPlaceID) || !optionalID(l.SourceToAnchorPlaceID) || !text(l.CreatedAt) || !text(l.UpdatedAt) {
			return ErrInvalid
		}
		positions[l.FromPosition] = true
	}
	return nil
}
func ValidateDetail(v Detail) error {
	if e := ValidateSummary(v.Summary); e != nil {
		return e
	}
	if !text(v.Body) {
		return ErrInvalid
	}
	if v.Deleted {
		if v.Body != "" || v.Relationships != nil {
			return ErrInvalid
		}
		return nil
	}
	if v.Relationships == nil {
		return ErrInvalid
	}
	return ValidateRelationships(*v.Relationships)
}
func DecodeRelationships(raw []byte) (Relationships, error) {
	var v Relationships
	scan := json.NewDecoder(bytes.NewReader(raw))
	scan.UseNumber()
	if !uniqueJSON(scan, 0) || scan.Decode(new(any)) != io.EOF {
		return v, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	if e := d.Decode(&v); e != nil {
		return v, ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return v, ErrInvalid
	}
	return v, ValidateRelationships(v)
}
func uniqueJSON(d *json.Decoder, depth int) bool {
	if depth > 64 {
		return false
	}
	t, e := d.Token()
	if e != nil {
		return false
	}
	delimiter, ok := t.(json.Delim)
	if !ok {
		return true
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			s, ok := key.(string)
			if e != nil || !ok || seen[s] {
				return false
			}
			seen[s] = true
			if !uniqueJSON(d, depth+1) {
				return false
			}
		}
		last, e := d.Token()
		return e == nil && last == json.Delim('}')
	case '[':
		for d.More() {
			if !uniqueJSON(d, depth+1) {
				return false
			}
		}
		last, e := d.Token()
		return e == nil && last == json.Delim(']')
	}
	return false
}
