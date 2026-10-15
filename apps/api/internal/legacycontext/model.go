// Package legacycontext adapts preserved private relationships without granting public rights.
package legacycontext

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
	"time"
)

const Protocol = "birdtie-legacy-native-context-v1"

var ErrInvalid = errors.New("legacy_context_source_invalid")

type Counts struct {
	Places           int `json:"places"`
	SavedItems       int `json:"savedItems"`
	NoteLocations    int `json:"noteLocations"`
	MediaContexts    int `json:"mediaContexts"`
	MediaPlaces      int `json:"mediaPlaces"`
	Threads          int `json:"threads"`
	Members          int `json:"members"`
	Legs             int `json:"legs"`
	ThreadTombstones int `json:"threadTombstones"`
}
type Place struct {
	legacycatalog.Place
	SourceRegion, SourceCountryCode, SourceAreaKind string
}
type Origin struct{ SourceKey, OwnerSourceID, SourceSHA256, TargetID, ParentKey, ParentSHA256 string }
type NoteLocations struct {
	Origin
	Original legacynative.Note
	Items    []Location
}
type Location struct {
	ID, PlaceID, SourcePlaceID, SourceSHA256 string
	SourcePosition                           int64
}
type MediaContext struct {
	Origin
	MomentID, SourceMediaID, SourceMediaSHA256 string
	SourcePosition                             int64
	CapturedDate                               *string
	OriginalMapEnabled                         bool
	Latitude, Longitude                        *json.Number
	Places                                     []Location
}
type Thread struct {
	Origin
	SourceCreatedAt *time.Time
	RouteRevision   int64
	Members         []Member
	Legs            []Leg
}
type Member struct {
	Origin
	MomentID                                  string
	Position                                  int
	InboundOrigin, InboundMode                string
	DurationMinutes, EstimatedDurationMinutes *int64
}
type Leg struct {
	Origin
	FromMomentID, ToMomentID         string
	FromPosition, ToPosition         int
	Mode, FromPlaceID, ToPlaceID     string
	SourceCreatedAt, SourceUpdatedAt *time.Time
}
type Plan struct {
	RunID                                                                  string                   `json:"runId"`
	SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, ProjectionSHA256 string                   `json:"-"`
	Places                                                                 []Place                  `json:"-"`
	SavedItems                                                             []legacynative.SavedItem `json:"-"`
	NoteLocations                                                          []NoteLocations          `json:"-"`
	MediaContexts                                                          []MediaContext           `json:"-"`
	Threads                                                                []Thread                 `json:"-"`
	Counts                                                                 Counts                   `json:"counts"`
	Deferred                                                               map[string]int           `json:"deferred"`
}

func (Plan) String() string { return "private native context plan; NOT_IMPORTED" }
