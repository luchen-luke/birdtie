// Package legacymap restores independently proven legacy read visibility.
// It never creates a Birdtie publication or changes a private native record.
package legacymap

import "errors"

const Protocol = "birdtie-civu-map-visibility-v1"

var ErrInvalid = errors.New("legacy_map_source_invalid")

type Counts struct {
	Places           int `json:"places"`
	PublicPlaces     int `json:"publicPlaces"`
	CoordinatePlaces int `json:"coordinatePlaces"`
	PublicMoments    int `json:"publicMoments"`
}

type Place struct {
	SourceID, SourceSHA256, TargetID string
	Public                           bool
	Latitude, Longitude              *float64
	CoordinateProof                  string
}

type Plan struct {
	RunID, SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, ProjectionSHA256 string
	Places                                                                        []Place `json:"-"`
	Counts                                                                        Counts
	Deferred                                                                      map[string]int
}
