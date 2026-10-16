package content

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const MapContentSchema = "map-content-v2"
const MapContentPlaceSchema = "map-content-place-moments-v1"
const MapContentMomentSchema = "map-content-moment-v1"

type MapContentBounds struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

func (b MapContentBounds) Valid() bool {
	return !math.IsNaN(b.West) && !math.IsNaN(b.East) && !math.IsNaN(b.South) && !math.IsNaN(b.North) && b.West >= -180 && b.West <= 180 && b.East >= -180 && b.East <= 180 && b.West != b.East && b.South >= -90 && b.North <= 90 && b.South < b.North
}

type MapContentQuery struct {
	CityID           string
	Bounds           MapContentBounds
	Zoom             float64
	FilterScope      string
	ClusterID        string
	Cursor           string
	SourceVersion    string
	SelectedMarkerID string
}

func (q MapContentQuery) Valid() bool {
	return len(q.CityID) <= 180 && utf8.ValidString(q.CityID) && !strings.ContainsAny(q.CityID, "\x00/ ") && q.Bounds.Valid() && !math.IsNaN(q.Zoom) && q.Zoom >= 0 && q.Zoom <= 22 && (q.FilterScope == "" || q.FilterScope == "DEFAULT_CONTENT") && validMapSelectedMarker(q.SelectedMarkerID) && len(q.Cursor) <= 4096 && (q.ClusterID == "" && (q.Cursor == "" && q.SourceVersion == "" || MomentControlHex(q.SourceVersion)) || strings.HasPrefix(q.ClusterID, "cluster:") && MomentControlHex(strings.TrimPrefix(q.ClusterID, "cluster:")) && MomentControlHex(q.SourceVersion))
}

func validMapSelectedMarker(id string) bool {
	if id == "" {
		return true
	}
	for _, prefix := range []string{"place:", "anchor:"} {
		if strings.HasPrefix(id, prefix) {
			return identity.HumanControlUUID(strings.TrimPrefix(id, prefix))
		}
	}
	return strings.HasPrefix(id, "cluster:") && MomentControlHex(strings.TrimPrefix(id, "cluster:"))
}

type MapContentAnchor struct {
	CoordinateSystem string  `json:"coordinateSystem"`
	Precision        string  `json:"precision"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
}

func (a MapContentAnchor) Valid() bool {
	return a.CoordinateSystem == "wgs84" && (a.Precision == "point" || a.Precision == "approximate") && !math.IsNaN(a.Latitude) && !math.IsNaN(a.Longitude) && a.Latitude >= -90 && a.Latitude <= 90 && a.Longitude >= -180 && a.Longitude <= 180
}
func MapContentVisibility(v string) bool {
	return v == "SELF_PRIVATE" || v == "CIVU_PUBLIC_CATALOG" || v == "CIVU_PUBLIC_MOMENT" || v == "BIRDTIE_PUBLIC_MOMENT"
}

type MapContentImage struct {
	Kind             string `json:"kind"`
	ID               string `json:"id"`
	Revision         int64  `json:"revision"`
	VisibilitySource string `json:"visibilitySource"`
	MIMEType         string `json:"mimeType"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ByteCount        int    `json:"byteCount"`
	SHA256           string `json:"sha256"`
	SourceVersion    string `json:"sourceVersion"`
	ContentPath      string `json:"contentPath"`
}

func (i MapContentImage) Valid() bool {
	return (i.Kind == "REFERENCE_IMAGE" || i.Kind == "MOMENT_IMAGE" || i.Kind == "MOMENT_VIDEO_POSTER") && identity.HumanControlUUID(i.ID) && i.Revision > 0 && MapContentVisibility(i.VisibilitySource) && (i.MIMEType == "image/jpeg" || i.MIMEType == "image/png") && i.Width > 0 && i.Height > 0 && i.Width <= 8192 && i.Height <= 8192 && int64(i.Width)*int64(i.Height) <= 48*1024*1024 && i.ByteCount > 0 && i.ByteCount <= 16*1024*1024 && MomentControlHex(i.SHA256) && MomentControlHex(i.SourceVersion) && strings.HasPrefix(i.ContentPath, "/v1/me/map-content/") && !strings.Contains(i.ContentPath, "#")
}

type MapContentMomentSelector struct {
	ID               string `json:"id"`
	Revision         int64  `json:"revision"`
	VisibilitySource string `json:"visibilitySource"`
	DetailPath       string `json:"detailPath"`
}
type MapContentPin struct {
	RepresentativeMarkerID string                     `json:"representativeMarkerId,omitempty"`
	RepresentativePlaceID  string                     `json:"representativePlaceId,omitempty"`
	RepresentativeMomentID string                     `json:"representativeMomentId,omitempty"`
	MarkerID               string                     `json:"markerId"`
	Kind                   string                     `json:"kind"`
	EntityID               string                     `json:"entityId"`
	AnchorID               string                     `json:"anchorId,omitempty"`
	MomentID               string                     `json:"momentId,omitempty"`
	EntryID                string                     `json:"entryId,omitempty"`
	ThreadID               string                     `json:"threadId,omitempty"`
	AnchorCount            int                        `json:"anchorCount"`
	MembersPath            string                     `json:"membersPath,omitempty"`
	Bounds                 *MapContentBounds          `json:"bounds,omitempty"`
	PlaceID                string                     `json:"placeId"`
	Name                   string                     `json:"name"`
	CategoryCode           string                     `json:"categoryCode"`
	CityID                 *string                    `json:"cityId"`
	Anchor                 MapContentAnchor           `json:"anchor"`
	VisibilitySources      []string                   `json:"visibilitySources"`
	SourceVersion          string                     `json:"sourceVersion"`
	MomentCount            int                        `json:"momentCount"`
	Moments                []MapContentMomentSelector `json:"moments"`
	MomentsTruncated       bool                       `json:"momentsTruncated"`
	Cover                  *MapContentImage           `json:"cover"`
}
type MapContentPage struct {
	ViewportPath      string                     `json:"viewportPath"`
	TotalPointCount   int                        `json:"totalPointCount"`
	Zoom              float64                    `json:"zoom"`
	FilterScope       string                     `json:"filterScope"`
	TotalContentCount int                        `json:"totalContentCount"`
	TotalAnchorCount  int                        `json:"totalAnchorCount"`
	NextCursor        *string                    `json:"nextCursor"`
	SchemaVersion     string                     `json:"schemaVersion"`
	Scope             string                     `json:"scope"`
	ViewerAccountID   string                     `json:"viewerAccountId"`
	CityID            string                     `json:"cityId"`
	Bounds            MapContentBounds           `json:"bounds"`
	ObservedAt        time.Time                  `json:"observedAt"`
	ValidUntil        time.Time                  `json:"validUntil"`
	SourceVersion     string                     `json:"sourceVersion"`
	Pins              []MapContentPin            `json:"pins"`
	Truncated         bool                       `json:"truncated"`
	ResponseProof     identity.HumanControlProof `json:"-"`
}

func mapContentLease(at, until, now time.Time, source string) bool {
	return !at.IsZero() && !at.After(now.Add(time.Second)) && until.After(now) && until.After(at) && until.Sub(at) <= 30*time.Second && MomentControlHex(source)
}
func (v MapContentPage) Valid(now time.Time) bool {
	if v.SchemaVersion != MapContentSchema || v.Scope != "MAP_CONTENT" || !identity.HumanControlUUID(v.ViewerAccountID) || !(MapContentQuery{CityID: v.CityID, Bounds: v.Bounds, Zoom: v.Zoom, FilterScope: v.FilterScope}).Valid() || v.Pins == nil || len(v.Pins) > 40 || v.FilterScope != "DEFAULT_CONTENT" || v.TotalContentCount < 0 || v.TotalAnchorCount < len(v.Pins) || !mapContentLease(v.ObservedAt, v.ValidUntil, now, v.SourceVersion) {
		return false
	}
	seen := map[string]bool{}
	for _, p := range v.Pins {
		if !p.validIdentity() || seen[p.MarkerID] || p.Name == "" || !utf8.ValidString(p.Name) || len(p.Name) > 16000 || !p.Anchor.Valid() || !MomentControlHex(p.SourceVersion) || len(p.VisibilitySources) == 0 || len(p.VisibilitySources) > 4 || p.Moments == nil || len(p.Moments) > 20 || p.MomentCount < 1 || p.AnchorCount < 1 || p.MomentCount < len(p.Moments) || p.MomentsTruncated != (p.MomentCount > len(p.Moments)) {
			return false
		}
		seen[p.MarkerID] = true
		scopes := map[string]bool{}
		for _, s := range p.VisibilitySources {
			if !MapContentVisibility(s) || scopes[s] {
				return false
			}
			scopes[s] = true
		}
		ms := map[string]bool{}
		for _, m := range p.Moments {
			if !identity.HumanControlUUID(m.ID) || ms[m.ID] || m.Revision < 1 || m.VisibilitySource == "CIVU_PUBLIC_CATALOG" || !scopes[m.VisibilitySource] || m.DetailPath != "/v1/me/map-content/moments/"+m.ID {
				return false
			}
			ms[m.ID] = true
		}
		if p.Cover != nil && (!p.Cover.Valid() || !scopes[p.Cover.VisibilitySource]) {
			return false
		}
	}
	return true
}

func (p MapContentPin) validIdentity() bool {
	switch p.Kind {
	case "PLACE_CONTENT":
		return identity.HumanControlUUID(p.PlaceID) && p.MarkerID == "place:"+p.PlaceID && p.EntityID == p.PlaceID && p.MomentID == "" && p.AnchorID == "" && p.AnchorCount == 1
	case "MOMENT_ANCHOR":
		return p.PlaceID == "" && identity.HumanControlUUID(p.AnchorID) && identity.HumanControlUUID(p.MomentID) && p.EntryID == p.MomentID && p.EntityID == p.MomentID && p.MarkerID == "anchor:"+p.AnchorID && p.AnchorCount == 1 && (p.ThreadID == "" || identity.HumanControlUUID(p.ThreadID))
	case "CLUSTER":
		return p.PlaceID == "" && p.EntityID == p.MarkerID && strings.HasPrefix(p.MarkerID, "cluster:") && MomentControlHex(strings.TrimPrefix(p.MarkerID, "cluster:")) && p.AnchorCount > 1 && p.Bounds != nil && p.Bounds.Valid() && (p.RepresentativeMarkerID == "" || validMapSelectedMarker(p.RepresentativeMarkerID)) && strings.HasPrefix(p.MembersPath, "/v1/me/map-content?")
	}
	return false
}

type MapContentLocation struct {
	PlaceID string            `json:"placeId"`
	Name    string            `json:"name"`
	Anchor  *MapContentAnchor `json:"anchor"`
}
type MapContentMedia struct {
	Kind          string           `json:"kind"`
	ID            string           `json:"id"`
	Revision      int64            `json:"revision"`
	MIMEType      string           `json:"mimeType"`
	Width         int              `json:"width"`
	Height        int              `json:"height"`
	ByteCount     int              `json:"byteCount"`
	SHA256        string           `json:"sha256"`
	SourceVersion string           `json:"sourceVersion"`
	ContentPath   string           `json:"contentPath"`
	DurationMs    *int64           `json:"durationMs"`
	VideoCodec    *string          `json:"videoCodec"`
	AudioCodec    *string          `json:"audioCodec"`
	Poster        *MapContentImage `json:"poster"`
}
type MapContentAuthor struct {
	AccountID   string       `json:"accountId"`
	DisplayName string       `json:"displayName"`
	Avatar      *av.Metadata `json:"avatar"`
}

func MapContentAuthorAvatarPath(momentID, mediaID, source string) string {
	return "/v1/me/map-content/moments/" + momentID + "/author-avatar/" + mediaID + "/content?sourceVersion=" + source
}
func (a MapContentAuthor) ValidForMoment(momentID string) bool {
	if a.Valid() {
		return true
	}
	if a.Avatar == nil || !identity.HumanControlUUID(momentID) {
		return false
	}
	m := *a.Avatar
	prefix := "/v1/me/map-content/moments/" + momentID + "/author-avatar/"
	tail := strings.TrimPrefix(m.ThumbnailPath, prefix)
	parts := strings.Split(tail, "/content?sourceVersion=")
	if !strings.HasPrefix(m.ThumbnailPath, prefix) || len(parts) != 2 || !identity.HumanControlUUID(parts[0]) || !MomentControlHex(parts[1]) {
		return false
	}
	m.ThumbnailPath = av.Path(m.AccountID, m.Version)
	a.Avatar = &m
	return a.Valid()
}
func (a MapContentAuthor) Valid() bool {
	return identity.HumanControlUUID(a.AccountID) && a.DisplayName != "" && len(a.DisplayName) <= 16000 && utf8.ValidString(a.DisplayName) && (a.Avatar == nil || a.Avatar.Valid() && a.Avatar.AccountID == a.AccountID)
}

type MapContentMoment struct {
	Author           *MapContentAuthor          `json:"author"`
	CreatedAtSource  string                     `json:"createdAtSource"`
	PublishedAt      *time.Time                 `json:"publishedAt"`
	SchemaVersion    string                     `json:"schemaVersion"`
	ViewerAccountID  string                     `json:"viewerAccountId"`
	ID               string                     `json:"id"`
	Revision         int64                      `json:"revision"`
	Status           string                     `json:"status"`
	Visibility       string                     `json:"visibility"`
	VisibilitySource string                     `json:"visibilitySource"`
	CanEdit          bool                       `json:"canEdit"`
	Title            string                     `json:"title"`
	Body             string                     `json:"body"`
	CreatedAt        time.Time                  `json:"createdAt"`
	Locations        []MapContentLocation       `json:"locations"`
	Media            []MapContentMedia          `json:"media"`
	ObservedAt       time.Time                  `json:"observedAt"`
	ValidUntil       time.Time                  `json:"validUntil"`
	SourceVersion    string                     `json:"sourceVersion"`
	ResponseProof    identity.HumanControlProof `json:"-"`
}

func (v MapContentMoment) Valid(now time.Time) bool {
	if v.Author != nil && !v.Author.ValidForMoment(v.ID) || (v.CreatedAtSource != "" && v.CreatedAtSource != "NATIVE" && v.CreatedAtSource != "CIVU_ORIGINAL" && v.CreatedAtSource != "UNKNOWN") || (v.PublishedAt != nil && (v.PublishedAt.IsZero() || v.PublishedAt.After(now))) {
		return false
	}
	if v.SchemaVersion != MapContentMomentSchema || !identity.HumanControlUUID(v.ViewerAccountID) || !identity.HumanControlUUID(v.ID) || v.Revision < 1 || (v.Status != "draft" && v.Status != "published") || (v.Visibility != "private" && v.Visibility != "public") || !MapContentVisibility(v.VisibilitySource) || v.VisibilitySource == "CIVU_PUBLIC_CATALOG" || !utf8.ValidString(v.Title) || !utf8.ValidString(v.Body) || len(v.Title) > 16000 || len(v.Body) > 128*1024 || v.CreatedAt.IsZero() || v.Locations == nil || len(v.Locations) > 50 || v.Media == nil || len(v.Media) > 200 || !mapContentLease(v.ObservedAt, v.ValidUntil, now, v.SourceVersion) {
		return false
	}
	for _, l := range v.Locations {
		if !identity.HumanControlUUID(l.PlaceID) || l.Name == "" || !utf8.ValidString(l.Name) || (l.Anchor != nil && !l.Anchor.Valid()) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, m := range v.Media {
		if !identity.HumanControlUUID(m.ID) || seen[m.ID] || m.Revision < 1 || m.ByteCount < 1 || m.Width < 1 || m.Height < 1 || !MomentControlHex(m.SHA256) || !MomentControlHex(m.SourceVersion) || !strings.HasPrefix(m.ContentPath, "/v1/me/map-content/moments/"+v.ID+"/media/"+m.ID+"/content?sourceVersion=") {
			return false
		}
		seen[m.ID] = true
		if m.Kind == "IMAGE" {
			if m.MIMEType != "image/jpeg" && m.MIMEType != "image/png" || m.ByteCount > 16*1024*1024 || m.DurationMs != nil || m.VideoCodec != nil || m.AudioCodec != nil || m.Poster != nil {
				return false
			}
		} else if m.Kind == "VIDEO" {
			if m.MIMEType != "video/mp4" || m.ByteCount > 64*1024*1024 || m.DurationMs == nil || *m.DurationMs < 1 || *m.DurationMs > 120000 || m.VideoCodec == nil || *m.VideoCodec != "h264" || m.AudioCodec == nil || (*m.AudioCodec != "aac" && *m.AudioCodec != "none") || m.Poster == nil || !m.Poster.Valid() {
				return false
			}
		} else {
			return false
		}
	}
	return true
}

type MapContentPlacePage struct {
	SchemaVersion   string                     `json:"schemaVersion"`
	ViewerAccountID string                     `json:"viewerAccountId"`
	PlaceID         string                     `json:"placeId"`
	Items           []MapContentMoment         `json:"items"`
	ObservedAt      time.Time                  `json:"observedAt"`
	ValidUntil      time.Time                  `json:"validUntil"`
	SourceVersion   string                     `json:"sourceVersion"`
	NextCursor      *string                    `json:"nextCursor"`
	ResponseProof   identity.HumanControlProof `json:"-"`
}

func (v MapContentPlacePage) Valid(now time.Time) bool {
	if v.SchemaVersion != MapContentPlaceSchema || !identity.HumanControlUUID(v.ViewerAccountID) || !identity.HumanControlUUID(v.PlaceID) || v.Items == nil || len(v.Items) > 20 || !mapContentLease(v.ObservedAt, v.ValidUntil, now, v.SourceVersion) {
		return false
	}
	for _, m := range v.Items {
		if !m.Valid(now) || m.ViewerAccountID != v.ViewerAccountID {
			return false
		}
	}
	return v.NextCursor == nil || (len(v.Items) == 20 && len(*v.NextCursor) > 0 && len(*v.NextCursor) <= 4096)
}

type MapContentBinary struct {
	Bytes         []byte
	MIMEType      string
	SHA256        string
	ValidUntil    time.Time
	ResponseProof identity.HumanControlProof
}
type HumanMapContentStore interface {
	ReadHumanMapContent(context.Context, [32]byte, identity.Actor, MapContentQuery) (MapContentPage, error)
	ReadHumanMapContentPlace(context.Context, [32]byte, identity.Actor, string, string) (MapContentPlacePage, error)
	ReadHumanMapContentMoment(context.Context, [32]byte, identity.Actor, string) (MapContentMoment, error)
	ReadHumanMapContentBinary(context.Context, [32]byte, identity.Actor, string, string, string, string, string) (MapContentBinary, error)
}
