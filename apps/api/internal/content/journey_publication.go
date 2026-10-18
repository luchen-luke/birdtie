package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const JourneyPublicationSchema = "journey-publication-v1"
const JourneyDiscoverySchema = "journey-discovery-v1"

// Approval is deliberately independent of JourneyDetails. Only explicitly
// selected STOPs and approved public text enter the immutable publication.
type JourneyPublicationStopInput struct {
	EntryID            string `json:"entryId"`
	Position           int    `json:"position"`
	Title              string `json:"title"`
	Note               string `json:"note"`
	LocationVisibility string `json:"locationVisibility"`
	MomentID           string `json:"momentId,omitempty"`
	MomentRevision     int64  `json:"momentRevision,omitempty"`
	CommentMomentID    string `json:"commentMomentId,omitempty"`
}
type JourneyPublicationInput struct {
	Title   string                        `json:"title"`
	Summary string                        `json:"summary"`
	Stops   []JourneyPublicationStopInput `json:"stops"`
}
type PublicJourneyStop struct {
	EntryID            string   `json:"entryId"`
	Position           int      `json:"position"`
	Available          bool     `json:"available"`
	Title              string   `json:"title,omitempty"`
	Note               string   `json:"note,omitempty"`
	LocationVisibility string   `json:"locationVisibility,omitempty"`
	PlaceID            *string  `json:"placeId,omitempty"`
	PlaceName          string   `json:"placeName,omitempty"`
	Latitude           *float64 `json:"latitude,omitempty"`
	Longitude          *float64 `json:"longitude,omitempty"`
	MomentID           string   `json:"momentId,omitempty"`
	MomentRevision     int64    `json:"momentRevision,omitempty"`
	CommentMomentID    string   `json:"commentMomentId,omitempty"`
}
type PublicJourneySnapshot struct {
	SchemaVersion       string              `json:"schemaVersion"`
	PublicationRevision int64               `json:"publicationRevision"`
	SourceRevision      int64               `json:"sourceRevision"`
	PublishedAt         time.Time           `json:"publishedAt"`
	Stops               []PublicJourneyStop `json:"stops"`
}
type JourneyPublishedSummary struct {
	PublicationRevision int64     `json:"publicationRevision"`
	SourceRevision      int64     `json:"sourceRevision"`
	PublishedAt         time.Time `json:"publishedAt"`
	Title               string    `json:"title"`
	Summary             string    `json:"summary"`
	StopCount           int       `json:"stopCount"`
}
type JourneyLifecycle struct {
	State     string `json:"state"`
	TimeZone  string `json:"timeZone"`
	AsOfDate  string `json:"asOfDate"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}
type JourneyDiscoveryPage struct {
	SchemaVersion   string             `json:"schemaVersion"`
	ViewerAccountID string             `json:"viewerAccountId"`
	Items           []PublicCollection `json:"items"`
	NextCursor      string             `json:"nextCursor,omitempty"`
	ObservedAt      time.Time          `json:"observedAt"`
	ValidUntil      time.Time          `json:"validUntil"`
	SourceVersion   string             `json:"sourceVersion"`
	ResponseProof   any                `json:"-"`
}
type JourneyDiscoveryStore interface {
	ListJourneyDiscovery(context.Context, [32]byte, identity.Actor, string) (JourneyDiscoveryPage, error)
	RevalidateJourneyDiscovery(context.Context, [32]byte, identity.Actor, JourneyDiscoveryPage) error
}

func NormalizeJourneyPublication(v JourneyPublicationInput) (JourneyPublicationInput, error) {
	if len(strings.TrimSpace(v.Title)) == 0 || !journeyText(v.Title, 160) || !journeyText(v.Summary, 5000) || len(v.Stops) < 1 || len(v.Stops) > 50 {
		return v, ErrInvalid
	}
	v.Stops = append([]JourneyPublicationStopInput{}, v.Stops...)
	seen := map[string]bool{}
	last := 0
	for _, s := range v.Stops {
		if !ValidCollectionID(s.EntryID) || s.EntryID != strings.ToLower(s.EntryID) || seen[s.EntryID] || s.Position <= last || s.Position > MaxJourneyEntries || !journeyText(s.Title, 160) || !journeyText(s.Note, 2000) || !validCollectionPrecision(s.LocationVisibility) {
			return v, ErrInvalid
		}
		if s.MomentID == "" {
			if s.MomentRevision != 0 || s.CommentMomentID != "" {
				return v, ErrInvalid
			}
		} else if !ValidCollectionID(s.MomentID) || s.MomentID != strings.ToLower(s.MomentID) || s.MomentRevision < 1 || s.CommentMomentID != "" && s.CommentMomentID != s.MomentID {
			return v, ErrInvalid
		}
		seen[s.EntryID] = true
		last = s.Position
	}
	return v, nil
}
func JourneyPublicationDigest(kind, id, action string, revision int64, v *JourneyPublicationInput) [32]byte {
	b, _ := json.Marshal([]any{JourneyPublicationSchema, kind, id, action, revision, v})
	return sha256.Sum256(b)
}

// Old collection approval bytes and digests retain their original decoder.
func DecodeJourneyOrCollectionPublication(raw []byte) (CollectionPublicationInput, error) {
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return CollectionPublicationInput{}, ErrInvalid
	}
	if _, ok := keys["journeySnapshot"]; !ok {
		return DecodeCollectionPublication(raw)
	}
	if len(raw) > 131072 || decodeJourneyClosed(raw, []string{"operationId", "revision", "journeySnapshot"}, []string{"entries"}) != nil {
		return CollectionPublicationInput{}, ErrInvalid
	}
	if decodeJourneyClosed(keys["journeySnapshot"], []string{"title", "summary", "stops"}, nil) != nil {
		return CollectionPublicationInput{}, ErrInvalid
	}
	var nested struct {
		Stops []json.RawMessage `json:"stops"`
	}
	if json.Unmarshal(keys["journeySnapshot"], &nested) != nil {
		return CollectionPublicationInput{}, ErrInvalid
	}
	for _, s := range nested.Stops {
		if decodeCollectionClosed(s, []string{"entryId", "position", "title", "note", "locationVisibility"}, []string{"momentId", "momentRevision", "commentMomentId"}) != nil {
			return CollectionPublicationInput{}, ErrInvalid
		}
	}
	var v CollectionPublicationInput
	if json.Unmarshal(raw, &v) != nil {
		return v, ErrInvalid
	}
	return NormalizeCollectionPublication(v)
}
func ValidatePublicJourney(v PublicCollection) error {
	s := v.JourneySnapshot
	if v.Kind != "journey" || s == nil || s.SchemaVersion != JourneyPublicationSchema || s.PublicationRevision != v.Revision || s.SourceRevision < 1 || s.SourceRevision >= s.PublicationRevision || s.PublishedAt.IsZero() || len(s.Stops) < 1 || len(s.Stops) > 50 || len(v.Entries) != 0 || v.CopyGrant != nil {
		return ErrInvalid
	}
	last := 0
	seen := map[string]bool{}
	for _, e := range s.Stops {
		if !ValidCollectionID(e.EntryID) || seen[e.EntryID] || e.Position <= last || e.Position > MaxJourneyEntries {
			return ErrInvalid
		}
		last = e.Position
		seen[e.EntryID] = true
		if !e.Available {
			if e.Title != "" || e.Note != "" || e.LocationVisibility != "" || e.PlaceID != nil || e.PlaceName != "" || e.Latitude != nil || e.Longitude != nil || e.MomentID != "" || e.MomentRevision != 0 || e.CommentMomentID != "" {
				return ErrInvalid
			}
			continue
		}
		if !journeyText(e.Title, 160) || !journeyText(e.Note, 2000) || !validCollectionPrecision(e.LocationVisibility) || (e.Latitude == nil) != (e.Longitude == nil) {
			return ErrInvalid
		}
		if e.LocationVisibility != "place" {
			if e.PlaceID != nil || e.PlaceName != "" || e.Latitude != nil {
				return ErrInvalid
			}
		} else if e.PlaceID != nil {
			if !ValidCollectionID(*e.PlaceID) || len(e.PlaceName) == 0 || !journeyText(e.PlaceName, 640) {
				return ErrInvalid
			}
		} else if e.PlaceName != "" || e.Latitude != nil {
			return ErrInvalid
		}
		if e.Latitude != nil && (!finiteCollection(*e.Latitude) || *e.Latitude < -90 || *e.Latitude > 90 || !finiteCollection(*e.Longitude) || *e.Longitude < -180 || *e.Longitude > 180) {
			return ErrInvalid
		}
		if e.MomentID == "" {
			if e.MomentRevision != 0 || e.CommentMomentID != "" {
				return ErrInvalid
			}
		} else if !ValidCollectionID(e.MomentID) || e.MomentRevision < 1 || e.CommentMomentID != "" && e.CommentMomentID != e.MomentID {
			return ErrInvalid
		}
	}
	return nil
}
func ValidateJourneyDiscovery(v JourneyDiscoveryPage, owner string) error {
	if v.SchemaVersion != JourneyDiscoverySchema || v.ViewerAccountID != owner || !ValidCollectionID(owner) || v.Items == nil || len(v.Items) > 20 || v.ObservedAt.IsZero() || !v.ValidUntil.After(v.ObservedAt) || len(v.SourceVersion) != 64 || v.NextCursor != "" && !ValidJourneyDiscoveryCursor(v.NextCursor) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, e := range v.Items {
		if seen[e.ID] || ValidatePublicCollection(e, "journey", e.ID) != nil || e.SourceExpiresAt.Before(v.ValidUntil) {
			return ErrInvalid
		}
		seen[e.ID] = true
	}
	return nil
}
func ValidJourneyDiscoveryCursor(s string) bool { return len(s) <= 256 && strings.HasPrefix(s, "jd1.") }
func ValidJourneyOwnerCursor(s string) bool     { return len(s) <= 256 && strings.HasPrefix(s, "jl1.") }

func decodeJourneyClosed(raw []byte, required, optional []string) error {
	if len(raw) < 2 || len(raw) > 131072 {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return ErrInvalid
	}
	allowed, seen := map[string]bool{}, map[string]bool{}
	for _, k := range append(append([]string{}, required...), optional...) {
		allowed[k] = true
	}
	for d.More() {
		token, e = d.Token()
		k, ok := token.(string)
		if e != nil || !ok || !allowed[k] || seen[k] {
			return ErrInvalid
		}
		seen[k] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrInvalid
	}
	for _, k := range required {
		if !seen[k] {
			return ErrInvalid
		}
	}
	return nil
}
