// Package legacynative builds private, editable Birdtie projections from a
// verified Civu preservation snapshot. It neither publishes catalog/content nor
// treats an old source reference, visibility or media URL as current authority.
package legacynative

import (
	"errors"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

const Protocol = "birdtie-civu-native-drafts-v1"

// StableTargetID exposes the original target namespace without changing any
// 156 projection or original receipt. Incremental adapters reuse its identity.
func StableTargetID(kind, owner, key string) string { return stableTarget(kind, owner, key) }

var ErrInvalid = errors.New("legacy_native_projection_invalid")

type Counts struct {
	SourceNotes          int `json:"sourceNotes"`
	SourceCollections    int `json:"sourceCollections"`
	SourceSavedItems     int `json:"sourceSavedItems"`
	NoteTombstones       int `json:"noteTombstones"`
	CollectionTombstones int `json:"collectionTombstones"`
	NativeNotes          int `json:"nativeNotes"`
	NativeJourneys       int `json:"nativeJourneys"`
	NativeThemeMaps      int `json:"nativeThemeMaps"`
	NativeSavedItems     int `json:"nativeSavedItems"`
	GeneratedTitles      int `json:"generatedTitles"`
	DeferredNotes        int `json:"deferredNotes"`
	DeferredCollections  int `json:"deferredCollections"`
	DeferredSavedItems   int `json:"deferredSavedItems"`
	PartialNotes         int `json:"partialNotes"`
	PartialCollections   int `json:"partialCollections"`
}

// Arrays are intentionally excluded from ordinary diagnostics/CLI JSON. A DB
// writer must bind each record to the original owner/source archive and preserve
// that immutable archive, including residual fields, in the same import run.
type Plan struct {
	RunID              string         `json:"runId"`
	SourceFileSHA256   string         `json:"-"`
	ManifestFileSHA256 string         `json:"-"`
	SnapshotSHA256     string         `json:"-"`
	ProjectionSHA256   string         `json:"-"`
	Notes              []Note         `json:"-"`
	Collections        []Collection   `json:"-"`
	SavedItems         []SavedItem    `json:"-"`
	Residuals          []Residual     `json:"-"`
	Deferred           map[string]int `json:"deferred"`
	Counts             Counts         `json:"counts"`
}

type Note struct {
	SourceID                string
	SourceKey               string
	OwnerSourceID           string
	SourceSHA256            string
	SourceVersion           int64
	TargetID                string
	CityID                  string
	PlaceID                 string
	Title                   string
	Body                    string
	TimePrecision           string
	LocationPrecision       string
	Visibility              string
	Status                  string
	GeneratedTitle          bool
	AnchorSource            string
	SourceDestinationID     *string
	SourceDestinationSource string
	SourceCreatedAt         *time.Time
	SourceUpdatedAt         *time.Time
	RecordedStartDate       *string
	RecordedEndDate         *string
	ResidualReasons         []string
}

type EntrySource struct {
	NativePosition          int
	SourcePosition          int64
	SourcePlaceID           string
	Section                 string
	CreatorNote             string
	CollaborationDisclosure bool
	SourcePrimaryMomentID   *string
}

type Collection struct {
	Kind            string
	SourceID        string
	SourceKey       string
	OwnerSourceID   string
	SourceSHA256    string
	SourceRevision  *int64
	TargetID        string
	CityID          string
	Title           string
	Summary         string
	Visibility      string
	Status          string
	Entries         []content.CollectionEntry
	EntrySources    []EntrySource
	SourceCreatedAt *time.Time
	SourceUpdatedAt *time.Time
	ResidualReasons []string
}

type SavedItem struct {
	SourceID        string
	SourceKey       string
	OwnerSourceID   string
	SourceSHA256    string
	TargetID        string
	PlaceID         string
	CityID          string
	Title           string
	SourceCreatedAt *time.Time
}

// TargetID is empty when a live record cannot be represented safely. Reasons
// describe preserved source fields; they contain no source text or coordinates.
// Deleted records get only a tombstone reason and never a native target.
type Residual struct {
	Kind          string
	SourceKey     string
	OwnerSourceID string
	SourceSHA256  string
	TargetID      string
	Reasons       []string
}

func (p Plan) String() string {
	return fmt.Sprintf("private native legacy plan: notes=%d collections=%d saved=%d; NOT_IMPORTED", len(p.Notes), len(p.Collections), len(p.SavedItems))
}

func (Note) String() string       { return "private native legacy note" }
func (Collection) String() string { return "private native legacy collection" }
func (SavedItem) String() string  { return "private native legacy saved reference" }
func (Residual) String() string   { return "private native legacy residual" }
