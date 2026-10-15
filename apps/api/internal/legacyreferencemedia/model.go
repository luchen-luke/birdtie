// Package legacyreferencemedia preserves exact historical image purposes without
// turning a stored URL, old review, or original public flag into current consent.
package legacyreferencemedia

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const Protocol = "birdtie-legacy-reference-media-v1"

var ErrInvalid = errors.New("legacy_reference_media_input_rejected")

type Counts struct {
	PrivateImages              int `json:"privateImages"`
	ReusedImages               int `json:"reusedImages"`
	PlaceImages                int `json:"placeImages"`
	PlaceCovers                int `json:"placeCovers"`
	PlaceGalleryImages         int `json:"placeGalleryImages"`
	PlaceRelations             int `json:"placeRelations"`
	ReusedPlaceReferences      int `json:"reusedPlaceReferences"`
	PlaceTombstones            int `json:"placeTombstones"`
	JourneyMedia               int `json:"journeyMedia"`
	MomentCovers               int `json:"momentCovers"`
	RejectedPlaceImages        int `json:"rejectedPlaceImages"`
	SystemArtworkArchives      int `json:"systemArtworkArchives"`
	RelationTombstones         int `json:"relationTombstones"`
	ArchivedDeletedNoteAssets  int `json:"archivedDeletedNoteAssets"`
	ArchivedUnreferencedAssets int `json:"archivedUnreferencedAssets"`
}
type Asset struct {
	ID, SourceID, SourceSHA256, OwnerSourceID, InputSHA256, MIME, ReuseNoteID string
	InputBytes                                                                int64
	System                                                                    bool
}
type Reference struct {
	ID, Kind, TargetType, SourceID, SourceSHA256, ParentSourceID, ParentSHA256, AssetSourceID, OwnerSourceID string
	SourceType, ReviewStatus, RightsBasis, Attribution, SourceNoteID                                         string
	Position                                                                                                 int64
	IsCover, Tombstone                                                                                       bool
	Publishable                                                                                              *bool
}
type Artwork struct{ SourceID, SourceSHA256, GeoID, GeoSHA256, Version, OwnerSourceID string }
type Plan struct {
	RunID                                                                                                 string      `json:"runId"`
	Counts                                                                                                Counts      `json:"counts"`
	SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, MatcherFileSHA256, SealSHA256, ProjectionSHA256 string      `json:"-"`
	Assets                                                                                                []Asset     `json:"-"`
	References                                                                                            []Reference `json:"-"`
	Artworks                                                                                              []Artwork   `json:"-"`
}

func (Plan) String() string      { return "verified private historical image purposes" }
func (Asset) String() string     { return "private historical image material" }
func (Reference) String() string { return "private historical image purpose" }
func StableID(kind, id string) string {
	s := sha256.Sum256([]byte(Protocol + "\x00" + kind + "\x00" + id))
	s[6] = (s[6] & 15) | 128
	s[8] = (s[8] & 63) | 128
	h := hex.EncodeToString(s[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
