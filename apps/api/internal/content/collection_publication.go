package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type CollectionPublicationEntry struct {
	PlaceID            string `json:"placeId"`
	LocationVisibility string `json:"locationVisibility"`
}
type CollectionPublicationInput struct {
	OperationID           string                       `json:"operationId"`
	Revision              int64                        `json:"revision"`
	Entries               []CollectionPublicationEntry `json:"entries"`
	AllowPrivatePlaceCopy bool                         `json:"allowPrivatePlaceCopy,omitempty"`
	JourneySnapshot       *JourneyPublicationInput     `json:"journeySnapshot,omitempty"`
}
type CollectionUnpublishInput struct {
	OperationID string `json:"operationId"`
	Revision    int64  `json:"revision"`
}

// Closing an uncertain operation carries its exact original selectors only.
// It neither approves text/precision nor authorizes another publication.
type CollectionPublicationCloseInput struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Action   string `json:"action"`
	Revision int64  `json:"revision"`
}

// Immutable causality is separate from the collection's later current state.
// Recovering a publication never republishes or revives a withdrawn source.
type CollectionPublicationOperation struct {
	OperationID      string `json:"operationId"`
	Action           string `json:"action"`
	Outcome          string `json:"outcome"`
	ExpectedRevision int64  `json:"expectedRevision"`
	ResultRevision   int64  `json:"resultRevision,omitempty"`
}
type PublicCollectionEntry struct {
	Note               string   `json:"note"`
	LocationVisibility string   `json:"locationVisibility"`
	PlaceID            *string  `json:"placeId,omitempty"`
	PlaceName          string   `json:"placeName,omitempty"`
	Latitude           *float64 `json:"latitude,omitempty"`
	Longitude          *float64 `json:"longitude,omitempty"`
}

// Only explicitly published user text and approved current public Place fields.
// No owner/profile, private media, timestamps, private selectors or model grant.
type PublicCollection struct {
	ID              string                  `json:"id"`
	Kind            string                  `json:"kind"`
	CityID          string                  `json:"cityId"`
	Title           string                  `json:"title"`
	Summary         string                  `json:"summary"`
	Revision        int64                   `json:"revision"`
	SourceExpiresAt time.Time               `json:"sourceExpiresAt"`
	Entries         []PublicCollectionEntry `json:"entries"`
	SourceBinding   string                  `json:"-"`
	ResponseProof   any                     `json:"-"`
	CopyGrant       *CollectionCopyGrant    `json:"copyGrant,omitempty"`
	JourneySnapshot *PublicJourneySnapshot  `json:"journeySnapshot,omitempty"`
}
type CollectionPublicationStore interface {
	PublishHumanCollection(context.Context, [32]byte, identity.Actor, string, string, CollectionPublicationInput) (PersonalCollection, error)
	UnpublishHumanCollection(context.Context, [32]byte, identity.Actor, string, string, CollectionUnpublishInput) (PersonalCollection, error)
	GetHumanCollectionPublicationOperation(context.Context, [32]byte, identity.Actor, string) (PersonalCollection, error)
	CloseHumanCollectionPublicationOperation(context.Context, [32]byte, identity.Actor, string, CollectionPublicationCloseInput) (PersonalCollection, error)
	GetPublicCollection(context.Context, PublicMomentAccess, string, string) (PublicCollection, error)
	RevalidatePublicCollection(context.Context, PublicMomentAccess, PublicCollection) error
}

func NormalizeCollectionPublicationClose(in CollectionPublicationCloseInput) (CollectionPublicationCloseInput, error) {
	if !ValidCollectionKind(in.Kind) || !ValidCollectionID(in.ID) || (in.Action != "PUBLISH" && in.Action != "UNPUBLISH") || in.Revision < 1 || in.Revision >= math.MaxInt64 {
		return CollectionPublicationCloseInput{}, ErrInvalid
	}
	in.ID = strings.ToLower(in.ID)
	return in, nil
}

// Decode the deliberately small closed selector object. Duplicate, omitted,
// null and unknown keys cannot change what the user chose to end and verify.
func DecodeCollectionPublicationClose(raw []byte) (CollectionPublicationCloseInput, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return CollectionPublicationCloseInput{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return CollectionPublicationCloseInput{}, ErrInvalid
	}
	seen := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		key, ok := t.(string)
		if e != nil || !ok || seen[key] || (key != "kind" && key != "id" && key != "action" && key != "revision") {
			return CollectionPublicationCloseInput{}, ErrInvalid
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return CollectionPublicationCloseInput{}, ErrInvalid
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(seen) != 4 {
		return CollectionPublicationCloseInput{}, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return CollectionPublicationCloseInput{}, ErrInvalid
	}
	var in CollectionPublicationCloseInput
	if json.Unmarshal(raw, &in) != nil {
		return CollectionPublicationCloseInput{}, ErrInvalid
	}
	return NormalizeCollectionPublicationClose(in)
}

func NormalizeCollectionPublication(in CollectionPublicationInput) (CollectionPublicationInput, error) {
	if in.JourneySnapshot != nil {
		if !ValidCollectionID(in.OperationID) || in.Revision < 1 || in.Revision >= math.MaxInt64 || len(in.Entries) != 0 || in.AllowPrivatePlaceCopy {
			return CollectionPublicationInput{}, ErrInvalid
		}
		v, e := NormalizeJourneyPublication(*in.JourneySnapshot)
		if e != nil {
			return CollectionPublicationInput{}, e
		}
		in.JourneySnapshot = &v
		in.OperationID = strings.ToLower(in.OperationID)
		in.Entries = []CollectionPublicationEntry{}
		return in, nil
	}
	if !ValidCollectionID(in.OperationID) || in.Revision < 1 || in.Revision >= math.MaxInt64 || len(in.Entries) < 1 || len(in.Entries) > 50 {
		return CollectionPublicationInput{}, ErrInvalid
	}
	if in.AllowPrivatePlaceCopy && in.Revision >= CollectionCopyMaxVersion {
		return CollectionPublicationInput{}, ErrInvalid
	}
	in.OperationID = strings.ToLower(in.OperationID)
	in.Entries = append([]CollectionPublicationEntry(nil), in.Entries...)
	seen := map[string]bool{}
	for i, e := range in.Entries {
		e.PlaceID = strings.ToLower(e.PlaceID)
		if !ValidCollectionID(e.PlaceID) || seen[e.PlaceID] || !validCollectionPrecision(e.LocationVisibility) {
			return CollectionPublicationInput{}, ErrInvalid
		}
		seen[e.PlaceID] = true
		in.Entries[i] = e
	}
	return in, nil
}
func validCollectionPrecision(v string) bool { return v == "hidden" || v == "city" || v == "place" }
func CollectionPublicationDigest(kind, id, action string, revision int64, entries []CollectionPublicationEntry) [32]byte {
	b, _ := json.Marshal([]any{"collection-publication-v1", kind, id, action, revision, entries})
	return sha256.Sum256(b)
}
func ValidatePublicCollection(v PublicCollection, kind, id string) error {
	if v.JourneySnapshot != nil {
		if v.ID != id || v.Kind != kind || !ValidCollectionID(id) || len(v.CityID) > 80 || !journeyText(v.Title, 160) || len(strings.TrimSpace(v.Title)) == 0 || !journeyText(v.Summary, 5000) || v.SourceExpiresAt.IsZero() || len(v.SourceBinding) != 64 {
			return ErrInvalid
		}
		return ValidatePublicJourney(v)
	}
	if v.ID != id || v.Kind != kind || !ValidCollectionID(v.ID) || !ValidCollectionKind(v.Kind) || v.CityID == "" || len(v.CityID) > 80 || len(v.Title) < 1 || len(v.Title) > 160 || !utf8.ValidString(v.Title) || len(v.Summary) > 5000 || !utf8.ValidString(v.Summary) || v.Revision < 2 || v.SourceExpiresAt.IsZero() || v.SourceBinding == "" || len(v.Entries) < 1 || len(v.Entries) > 50 {
		return ErrInvalid
	}
	for _, e := range v.Entries {
		if !validCollectionPrecision(e.LocationVisibility) || len(e.Note) > 2000 || !utf8.ValidString(e.Note) || (e.Latitude == nil) != (e.Longitude == nil) {
			return ErrInvalid
		}
		if e.LocationVisibility != "place" {
			if e.PlaceID != nil || e.PlaceName != "" || e.Latitude != nil || e.Longitude != nil {
				return ErrInvalid
			}
			continue
		}
		if e.PlaceID == nil || !ValidCollectionID(*e.PlaceID) || e.PlaceName == "" || len(e.PlaceName) > 640 || !utf8.ValidString(e.PlaceName) {
			return ErrInvalid
		}
		if e.Latitude != nil && (!finiteCollection(*e.Latitude) || *e.Latitude < -90 || *e.Latitude > 90 || !finiteCollection(*e.Longitude) || *e.Longitude < -180 || *e.Longitude > 180) {
			return ErrInvalid
		}
	}
	if v.CopyGrant != nil && ValidateCollectionCopyGrant(*v.CopyGrant, v) != nil {
		return ErrInvalid
	}
	return nil
}
func finiteCollection(v float64) bool { return !math.IsInf(v, 0) && !math.IsNaN(v) }
