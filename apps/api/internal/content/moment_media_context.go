package content

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"time"
)

const OwnedMomentMediaContextSchema = "owned-moment-media-context-v1"

type OwnedMediaContextPlace struct {
	ID              string       `json:"id"`
	Position        int          `json:"position"`
	PlaceID         string       `json:"placeId"`
	LegacyReference *lpr.Summary `json:"legacyReference,omitempty"`
}
type OwnedMediaContext struct {
	ID                 string                   `json:"id"`
	AssetKind          string                   `json:"assetKind"`
	AssetID            string                   `json:"assetId"`
	Revision           int64                    `json:"revision"`
	SourcePosition     int                      `json:"sourcePosition"`
	CapturedDate       *string                  `json:"capturedDate"`
	OriginalMapEnabled bool                     `json:"originalMapEnabled"`
	CoordinateStatus   string                   `json:"coordinateStatus"`
	SourceLatitude     *json.Number             `json:"sourceLatitude,omitempty"`
	SourceLongitude    *json.Number             `json:"sourceLongitude,omitempty"`
	Locations          []OwnedMediaContextPlace `json:"locations"`
}
type OwnedMomentMediaContexts struct {
	SchemaVersion  string                     `json:"schemaVersion"`
	MomentID       string                     `json:"momentId"`
	MomentRevision int64                      `json:"momentRevision"`
	Items          []OwnedMediaContext        `json:"items"`
	SourceVersion  string                     `json:"sourceVersion"`
	ObservedAt     time.Time                  `json:"observedAt"`
	ValidUntil     time.Time                  `json:"validUntil"`
	ResponseProof  identity.HumanControlProof `json:"-"`
}
type OwnedMomentMediaContextStore interface {
	ReadOwnedMomentMediaContexts(context.Context, [32]byte, identity.Actor, string) (OwnedMomentMediaContexts, error)
}
