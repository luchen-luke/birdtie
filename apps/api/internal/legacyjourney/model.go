// Package legacyjourney derives editable native schedules from the verified
// private archive, retaining original source identities without activating any
// old publication, collaboration, media, account or Agent permission.
package legacyjourney

import "github.com/birdtie/birdtie/apps/api/internal/content"

const Protocol = "birdtie-legacy-journey-details-v1"

type Counts struct {
	Journeys       int `json:"journeys"`
	Entries        int `json:"entries"`
	Stops          int `json:"stops"`
	Arrangements   int `json:"arrangements"`
	Notes          int `json:"notes"`
	Tombstones     int `json:"tombstones"`
	UnmappedPlaces int `json:"unmappedPlaces"`
	UnmappedCities int `json:"unmappedCities"`
}
type Origin struct {
	EntryID string
	PlaceID string
	Value   content.JourneyEntryOrigin
}
type Journey struct {
	SourceKey, SourceOwnerID, SourceSHA256, TargetID, CityID, Title string
	Details                                                         content.JourneyDetails
	Origins                                                         []Origin
}
type Plan struct {
	RunID                                                                  string    `json:"runId"`
	SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, ProjectionSHA256 string    `json:"-"`
	Counts                                                                 Counts    `json:"counts"`
	Journeys                                                               []Journey `json:"-"`
}

func (Plan) String() string    { return "verified private Journey schedule plan" }
func (Journey) String() string { return "private Journey schedule" }
