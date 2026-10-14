package agentworkspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// A v2 search is a retained public-data snapshot. ExpiresAt is a freshness
// deadline, not a deletion deadline or native Place publication authority.
type OnlinePlaceSearch struct {
	Schema        string                   `json:"schema"`
	RequestID     string                   `json:"requestId"`
	CityID        string                   `json:"cityId"`
	Query         string                   `json:"query"`
	Status        string                   `json:"status"`
	Provider      string                   `json:"provider"`
	Attribution   string                   `json:"attribution"`
	Scope         string                   `json:"scope"`
	ObservedAt    time.Time                `json:"observedAt"`
	ExpiresAt     time.Time                `json:"expiresAt"`
	Places        []ExternalPlaceCandidate `json:"places"`
	SnapshotID    string                   `json:"snapshotId,omitempty"`
	TaskID        string                   `json:"taskId,omitempty"`
	MessageIndex  int                      `json:"messageIndex"`
	TurnDigest    string                   `json:"turnDigest,omitempty"`
	CacheVersion  int64                    `json:"cacheVersion"`
	Origin        string                   `json:"origin,omitempty"`
	Freshness     string                   `json:"freshness,omitempty"`
	LicenseURL    string                   `json:"licenseUrl,omitempty"`
	RefreshFailed bool                     `json:"refreshFailed,omitempty"`
}

type OnlinePlaceCacheScope struct {
	CityID, Provider, CategoryCode string
	Latitude, Longitude            float64
	RadiusMeters                   int
	PolicyVersion                  string
}

// Cache identity contains public catalog coordinates and policy only. User
// text, account identity, phone location and viewport never enter this key.
func (s OnlinePlaceCacheScope) CacheKey() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%g|%g|%d|%s", s.CityID, s.Provider, s.CategoryCode, s.Latitude, s.Longitude, s.RadiusMeters, s.PolicyVersion))))
}

func OnlinePlaceTurnDigest(query string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(query)))
}

type PublicOnlinePlaceCache interface {
	ReadPublicOnlinePlaceCache(context.Context, OnlinePlaceCacheScope) (*OnlinePlaceSearch, bool, error)
	StorePublicOnlinePlaceCache(context.Context, OnlinePlaceCacheScope, *OnlinePlaceSearch) (*OnlinePlaceSearch, error)
}

type OwnOnlinePlaceReplyStore interface {
	CompleteOwnOnlinePlaceReply(context.Context, arp.Access, OnlinePlaceSearch, string) (Task, error)
}

// Continue from the exact completed owned Task in one native transaction.
// The old ExpectedTask is checked before either new message is written.
type OwnOnlinePlaceFollowupStore interface {
	CompleteOwnOnlinePlaceFollowup(context.Context, arp.Access, string, OnlinePlaceSearch, string) (Task, error)
}

type ExternalPlaceCandidate struct {
	ID           string              `json:"id"`
	CityID       string              `json:"cityId"`
	Name         string              `json:"name"`
	CategoryCode string              `json:"categoryCode"`
	AddressLabel string              `json:"addressLabel"`
	Location     foundation.Location `json:"location"`
	SourceURL    string              `json:"sourceUrl"`
	Attribution  string              `json:"attribution"`
	ReviewStatus string              `json:"reviewStatus"`
	ObservedAt   time.Time           `json:"observedAt"`
	ExpiresAt    time.Time           `json:"expiresAt"`
}
