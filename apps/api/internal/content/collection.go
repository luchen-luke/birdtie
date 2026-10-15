package content

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
)

const CollectionPageSize = 20

type CollectionEntry struct {
	PlaceID            string       `json:"placeId"`
	Note               string       `json:"note"`
	LocationVisibility string       `json:"locationVisibility"`
	PlaceName          string       `json:"placeName,omitempty"`
	Available          bool         `json:"available"`
	Latitude           *float64     `json:"latitude,omitempty"`
	Longitude          *float64     `json:"longitude,omitempty"`
	SourceExpiresAt    *time.Time   `json:"sourceExpiresAt,omitempty"`
	LegacyReference    *lpr.Summary `json:"legacyReference,omitempty"`
}
type CollectionInput struct {
	Kind           string            `json:"kind"`
	CityID         string            `json:"cityId"`
	Title          string            `json:"title"`
	Summary        string            `json:"summary"`
	Entries        []CollectionEntry `json:"entries"`
	JourneyDetails *JourneyDetails   `json:"journeyDetails,omitempty"`
}
type CollectionCreate struct {
	CollectionInput
	OperationID string `json:"operationId"`
}
type PersonalCollection struct {
	ID      string `json:"id"`
	OwnerID string `json:"ownerAccountId"`
	CollectionInput
	Visibility                 string                          `json:"visibility"`
	Status                     string                          `json:"status"`
	Revision                   int64                           `json:"revision"`
	CreatedAt                  time.Time                       `json:"createdAt"`
	UpdatedAt                  time.Time                       `json:"updatedAt"`
	PublicationOperation       *CollectionPublicationOperation `json:"publicationOperation,omitempty"`
	ResponseProof              CollectionReadProof             `json:"-"`
	PrivatePlaceCopyRestricted bool                            `json:"privatePlaceCopyRestricted,omitempty"`
}
type CollectionPage struct {
	Items      []PersonalCollection `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
}
type HumanCollectionStore interface {
	CreateHumanCollection(context.Context, [32]byte, identity.Actor, CollectionCreate) (PersonalCollection, error)
	ListHumanCollections(context.Context, [32]byte, identity.Actor, string, string) (CollectionPage, error)
	GetHumanCollection(context.Context, [32]byte, identity.Actor, string, string) (PersonalCollection, error)
	GetHumanCollectionOperation(context.Context, [32]byte, identity.Actor, string) (PersonalCollection, error)
	UpdateHumanCollection(context.Context, [32]byte, identity.Actor, string, int64, CollectionInput) (PersonalCollection, error)
	WithdrawHumanCollection(context.Context, [32]byte, identity.Actor, string, string, int64) error
}

func ValidCollectionKind(kind string) bool { return kind == "journey" || kind == "theme_map" }
func ValidCollectionID(id string) bool     { return momentUUID.MatchString(id) }

// Source labels and availability are authoritative read projections. Clients
// submit only existing Place IDs, private notes and the chosen stop precision.
func NormalizeCollectionInput(in CollectionInput) (CollectionInput, error) {
	in.CityID = strings.TrimSpace(in.CityID)
	in.Title = strings.TrimSpace(in.Title)
	in.Summary = strings.TrimSpace(in.Summary)
	if in.JourneyDetails != nil {
		if in.Kind != "journey" || len(in.Entries) != 0 || len(in.CityID) > 80 || !journeyText(in.CityID, 80) || !journeyText(in.Title, 160) || !journeyText(in.Summary, 5000) {
			return CollectionInput{}, ErrInvalid
		}
		d, e := NormalizeJourneyDetails(*in.JourneyDetails)
		if e != nil {
			return CollectionInput{}, e
		}
		in.JourneyDetails = &d
		in.Entries = []CollectionEntry{}
		return in, nil
	}
	if !ValidCollectionKind(in.Kind) || len(in.CityID) < 1 || len(in.CityID) > 80 || !utf8.ValidString(in.Title) || len(in.Title) < 1 || len(in.Title) > 160 || !utf8.ValidString(in.Summary) || len(in.Summary) > 5000 || len(in.Entries) < 1 || len(in.Entries) > 50 {
		return CollectionInput{}, ErrInvalid
	}
	seen := map[string]bool{}
	in.Entries = append([]CollectionEntry(nil), in.Entries...)
	for i, entry := range in.Entries {
		entry.PlaceID = strings.ToLower(strings.TrimSpace(entry.PlaceID))
		entry.Note = strings.TrimSpace(entry.Note)
		if !ValidCollectionID(entry.PlaceID) || seen[entry.PlaceID] || !utf8.ValidString(entry.Note) || len(entry.Note) > 2000 || entry.PlaceName != "" || entry.Available || entry.Latitude != nil || entry.Longitude != nil || entry.SourceExpiresAt != nil || entry.LegacyReference != nil {
			return CollectionInput{}, ErrInvalid
		}
		if entry.LocationVisibility != "hidden" && entry.LocationVisibility != "city" && entry.LocationVisibility != "place" {
			return CollectionInput{}, ErrInvalid
		}
		seen[entry.PlaceID] = true
		in.Entries[i] = entry
	}
	return in, nil
}
func CollectionInputDigest(in CollectionInput) [32]byte {
	raw, _ := json.Marshal(in)
	return sha256.Sum256(raw)
}
