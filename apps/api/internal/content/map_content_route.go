package content

import (
	"context"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const MapContentThreadRouteSchema = "map-content-thread-route-v1"

type MapContentRouteEndpoint struct {
	ViewportPath  string `json:"viewportPath"`
	MarkerID      string `json:"markerId"`
	SourceVersion string `json:"sourceVersion"`
}

// The original same-origin map page is evidence to re-read, never authority.
// In particular, signed continuation cursors retain their original query.
func (v MapContentRouteEndpoint) Query() (MapContentQuery, error) {
	var q MapContentQuery
	u, err := url.Parse(v.ViewportPath)
	if err != nil || len(v.ViewportPath) > 8192 || u.IsAbs() || u.Host != "" || u.User != nil || u.Path != "/v1/me/map-content" || u.RawPath != "" || u.Fragment != "" || u.ForceQuery || !strings.HasPrefix(v.MarkerID, "anchor:") || !identity.HumanControlUUID(strings.TrimPrefix(v.MarkerID, "anchor:")) || !MomentControlHex(v.SourceVersion) {
		return q, ErrInvalid
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return q, ErrInvalid
	}
	allowed := map[string]bool{"cityId": true, "west": true, "south": true, "east": true, "north": true, "zoom": true, "filterScope": true, "cursor": true, "sourceVersion": true, "selectedMarkerId": true}
	for key, list := range values {
		if !allowed[key] || len(list) != 1 {
			return q, ErrInvalid
		}
	}
	q = MapContentQuery{CityID: values.Get("cityId"), FilterScope: values.Get("filterScope"), Cursor: values.Get("cursor"), SourceVersion: values.Get("sourceVersion"), SelectedMarkerID: values.Get("selectedMarkerId")}
	for key, target := range map[string]*float64{"west": &q.Bounds.West, "south": &q.Bounds.South, "east": &q.Bounds.East, "north": &q.Bounds.North, "zoom": &q.Zoom} {
		*target, err = strconv.ParseFloat(values.Get(key), 64)
		if err != nil {
			return q, ErrInvalid
		}
	}
	if !q.Valid() {
		return q, ErrInvalid
	}
	return q, nil
}

type MapContentThreadRouteQuery struct {
	ThreadID         string                  `json:"threadId"`
	ExpectedProvider string                  `json:"expectedProvider"`
	From             MapContentRouteEndpoint `json:"from"`
	To               MapContentRouteEndpoint `json:"to"`
}

func (q MapContentThreadRouteQuery) Valid() bool {
	_, a := q.From.Query()
	_, b := q.To.Query()
	return identity.HumanControlUUID(q.ThreadID) && (q.ExpectedProvider == "amap" || q.ExpectedProvider == "mapbox") && a == nil && b == nil && q.From.MarkerID != q.To.MarkerID
}

// Only server-resolved endpoint facts reach a trusted startup provider. Neither
// this request nor provider selection is accepted from an HTTP caller.
type ThreadRoutePoint struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	City        string  `json:"-"`
	CountryCode string  `json:"-"`
	TimeZone    string  `json:"-"`
}

func (p ThreadRoutePoint) Valid() bool {
	return !math.IsNaN(p.Latitude) && !math.IsInf(p.Latitude, 0) && !math.IsNaN(p.Longitude) && !math.IsInf(p.Longitude, 0) && p.Latitude >= -90 && p.Latitude <= 90 && p.Longitude >= -180 && p.Longitude <= 180
}

type ThreadRouteRequest struct {
	Mode        string
	From, To    ThreadRoutePoint
	DepartureAt time.Time
}

// A client context may narrow the disclosed supplier, never override the
// authoritative endpoint countries. The provider separately validates its full
// supported ISO country inventory. Explicit transfers make no external call.
func ThreadRouteMatchesExpectedProvider(r ThreadRouteRequest, expected string) bool {
	if expected != "amap" && expected != "mapbox" {
		return false
	}
	if r.Mode == "train" || r.Mode == "flight" {
		return true
	}
	region := func(c string) string {
		if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
			return ""
		}
		if c == "CN" {
			return "amap"
		}
		return "mapbox"
	}
	return region(r.From.CountryCode) == expected && region(r.To.CountryCode) == expected
}

type ThreadRouteGeometry struct {
	Status, Mode, Provider, CoordinateSystem, GeometryKind string
	Segments                                               [][]ThreadRoutePoint
	DistanceMeters, DurationSeconds                        int
}
type ThreadRouteProvider interface {
	Plan(context.Context, ThreadRouteRequest) (ThreadRouteGeometry, error)
}

type MapContentThreadRoute struct {
	SchemaVersion     string                     `json:"schemaVersion"`
	ViewerAccountID   string                     `json:"viewerAccountId"`
	ThreadID          string                     `json:"threadId"`
	FromMarkerID      string                     `json:"fromMarkerId"`
	ToMarkerID        string                     `json:"toMarkerId"`
	FromSourceVersion string                     `json:"fromSourceVersion"`
	ToSourceVersion   string                     `json:"toSourceVersion"`
	Status            string                     `json:"status"`
	Mode              string                     `json:"mode"`
	Provider          string                     `json:"provider"`
	CoordinateSystem  string                     `json:"coordinateSystem"`
	GeometryKind      string                     `json:"geometryKind"`
	Segments          [][]ThreadRoutePoint       `json:"segments"`
	DistanceMeters    int                        `json:"distanceMeters"`
	DurationSeconds   int                        `json:"durationSeconds"`
	SourceVersion     string                     `json:"sourceVersion"`
	ObservedAt        time.Time                  `json:"observedAt"`
	ValidUntil        time.Time                  `json:"validUntil"`
	ResponseProof     identity.HumanControlProof `json:"-"`
}

func (v MapContentThreadRoute) Valid(now time.Time) bool {
	if v.SchemaVersion != MapContentThreadRouteSchema || !identity.HumanControlUUID(v.ViewerAccountID) || !identity.HumanControlUUID(v.ThreadID) || !strings.HasPrefix(v.FromMarkerID, "anchor:") || !identity.HumanControlUUID(strings.TrimPrefix(v.FromMarkerID, "anchor:")) || !strings.HasPrefix(v.ToMarkerID, "anchor:") || !identity.HumanControlUUID(strings.TrimPrefix(v.ToMarkerID, "anchor:")) || v.FromMarkerID == v.ToMarkerID || !MomentControlHex(v.FromSourceVersion) || !MomentControlHex(v.ToSourceVersion) || !mapContentLease(v.ObservedAt, v.ValidUntil, now, v.SourceVersion) || v.CoordinateSystem != "wgs84" || v.Segments == nil {
		return false
	}
	if v.Status == "BLOCKED" || v.Status == "NOT_AVAILABLE" {
		return len(v.Segments) == 0 && v.DistanceMeters == 0 && v.DurationSeconds == 0 && v.GeometryKind == "" && len(v.Mode) <= 32 && len(v.Provider) <= 64
	}
	if v.Status != "AVAILABLE" || len(v.Provider) < 1 || len(v.Provider) > 64 || len(v.Segments) < 1 || len(v.Segments) > 32 {
		return false
	}
	if v.GeometryKind == "TRANSFER" {
		if (v.Mode != "train" && v.Mode != "flight") || v.Provider != "saved-transfer" || v.DistanceMeters != 0 || v.DurationSeconds != 0 {
			return false
		}
	} else if v.GeometryKind != "ROAD" || (v.Mode != "walking" && v.Mode != "cycling" && v.Mode != "driving" && v.Mode != "transit") || v.DistanceMeters <= 0 || v.DistanceMeters > 40000000 || v.DurationSeconds <= 0 || v.DurationSeconds > 7*86400 {
		return false
	}
	total := 0
	for _, segment := range v.Segments {
		if len(segment) < 2 || len(segment) > 12000 {
			return false
		}
		total += len(segment)
		if total > 24000 {
			return false
		}
		for _, p := range segment {
			if !p.Valid() {
				return false
			}
		}
	}
	return true
}

type MapContentThreadRoutePlan struct {
	Route   MapContentThreadRoute
	Request ThreadRouteRequest
	CanPlan bool
}
type HumanMapContentThreadRouteStore interface {
	ReadHumanMapContentThreadRoutePlan(context.Context, [32]byte, identity.Actor, MapContentThreadRouteQuery) (MapContentThreadRoutePlan, error)
}
