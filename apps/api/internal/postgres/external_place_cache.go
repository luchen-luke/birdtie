package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/jackc/pgx/v5"
)

const externalPlaceLicenseURL = "https://opendatacommons.org/licenses/odbl/1-0/"
const externalPlaceAttribution = "© OpenStreetMap contributors (ODbL 1.0)"

var externalPlaceNodeID = regexp.MustCompile(`^osm:node:([1-9][0-9]{0,18})$`)

var _ agentworkspace.PublicOnlinePlaceCache = (*Store)(nil)

func externalCacheScopeValid(q agentworkspace.OnlinePlaceCacheScope) bool {
	return cityseed.SourceTextValid(q.CityID, 80) && q.Provider == "openstreetmap" && q.CategoryCode == "restaurant" &&
		externalPlacePointValid(q.Latitude, q.Longitude) && q.RadiusMeters >= 1 && q.RadiusMeters <= 6000 && cityseed.SourceTextValid(q.PolicyVersion, 100) && len(q.CacheKey()) <= 512
}
func externalPlacePointValid(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
func externalPlaceDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const r = math.Pi / 180
	h := math.Pow(math.Sin((lat2-lat1)*r/2), 2) + math.Cos(lat1*r)*math.Cos(lat2*r)*math.Pow(math.Sin((lon2-lon1)*r/2), 2)
	return 6371000 * 2 * math.Asin(math.Sqrt(math.Max(0, math.Min(1, h))))
}

// This is the durable ODbL public-source contract. Freshness is deliberately
// separate from source rights: old records are readable with a STALE label.
func externalPlaceSearchValid(v agentworkspace.OnlinePlaceSearch, now time.Time, unavailable bool) bool {
	if v.Schema != "birdtie-online-places-v2" || !cityseed.SourceTextValid(v.CityID, 80) || v.Provider != "openstreetmap" ||
		v.LicenseURL != externalPlaceLicenseURL || v.Attribution != externalPlaceAttribution ||
		(v.Scope != "city" && v.Scope != "viewport") || v.ObservedAt.IsZero() || v.ObservedAt.After(now) || !v.ExpiresAt.After(v.ObservedAt) ||
		v.ExpiresAt.Sub(v.ObservedAt) > 24*time.Hour || len(v.Places) > 15 || v.CacheVersion < 0 {
		return false
	}
	if v.Status != "SUCCESS" && v.Status != "EMPTY" && !(unavailable && v.Status == "UNAVAILABLE") {
		return false
	}
	if (v.Status == "SUCCESS") != (len(v.Places) > 0) {
		return false
	}
	seen := map[string]bool{}
	for _, p := range v.Places {
		id := externalPlaceNodeID.FindStringSubmatch(p.ID)
		if len(id) != 2 || seen[p.ID] || p.CityID != v.CityID || p.CategoryCode != "restaurant" || p.ReviewStatus != "UNREVIEWED" ||
			p.SourceURL != "https://www.openstreetmap.org/node/"+id[1] || p.Attribution != externalPlaceAttribution || !cityseed.SourceTextValid(p.Name, 150) ||
			(p.AddressLabel != "" && !cityseed.SourceTextValid(p.AddressLabel, 240)) || p.Location.Precision != "point" || p.Location.CoordinateSystem != "wgs84" ||
			p.Location.Latitude == nil || p.Location.Longitude == nil || !externalPlacePointValid(*p.Location.Latitude, *p.Location.Longitude) ||
			p.ObservedAt.IsZero() || !p.ObservedAt.Equal(v.ObservedAt) || !p.ExpiresAt.Equal(v.ExpiresAt) || !p.ExpiresAt.After(p.ObservedAt) {
			return false
		}
		seen[p.ID] = true
	}
	return true
}

func copyExternalPlaceSearch(v agentworkspace.OnlinePlaceSearch) (agentworkspace.OnlinePlaceSearch, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return agentworkspace.OnlinePlaceSearch{}, arp.ErrInvalid
	}
	var out agentworkspace.OnlinePlaceSearch
	if json.Unmarshal(raw, &out) != nil {
		return out, arp.ErrInvalid
	}
	return out, nil
}
func publicExternalCacheSearch(q agentworkspace.OnlinePlaceCacheScope, v agentworkspace.OnlinePlaceSearch, now time.Time) (agentworkspace.OnlinePlaceSearch, error) {
	if !externalCacheScopeValid(q) || !externalPlaceSearchValid(v, now, false) || v.CityID != q.CityID || v.Scope != "city" || now.Sub(v.ObservedAt) > time.Minute || !v.ExpiresAt.After(now) {
		return agentworkspace.OnlinePlaceSearch{}, arp.ErrInvalid
	}
	for _, p := range v.Places {
		if externalPlaceDistance(q.Latitude, q.Longitude, *p.Location.Latitude, *p.Location.Longitude) > float64(q.RadiusMeters) {
			return agentworkspace.OnlinePlaceSearch{}, arp.ErrInvalid
		}
	}
	out, e := copyExternalPlaceSearch(v)
	if e != nil {
		return out, e
	}
	out.Query, out.RequestID, out.SnapshotID, out.TaskID, out.TurnDigest = "", "", "", "", ""
	out.MessageIndex, out.CacheVersion, out.Origin, out.Freshness = -1, 0, "", ""
	out.RefreshFailed = false
	return out, nil
}

func (s *Store) ReadPublicOnlinePlaceCache(ctx context.Context, q agentworkspace.OnlinePlaceCacheScope) (*agentworkspace.OnlinePlaceSearch, bool, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, false, arp.ErrUnavailable
	}
	if !externalCacheScopeValid(q) {
		return nil, false, arp.ErrInvalid
	}
	// The cache is public; no owner/session/query is a database selection key.
	var raw []byte
	var version int64
	var now time.Time
	e := s.pool.QueryRow(ctx, `SELECT sc.payload,sc.version,clock_timestamp() FROM external_public_place_search_cache sc
 JOIN cities c ON c.id=sc.city_id AND c.publication_status='published'
 WHERE sc.cache_key=$1 AND sc.city_id=$2 AND sc.provider=$3 AND sc.category_code=$4
 AND sc.center_latitude=$5 AND sc.center_longitude=$6 AND sc.radius_meters=$7 AND sc.policy_version=$8
 AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())`, q.CacheKey(), q.CityID, q.Provider, q.CategoryCode, q.Latitude, q.Longitude, q.RadiusMeters, q.PolicyVersion).Scan(&raw, &version, &now)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	out, e := decodePublicExternalCache(q, raw, version, now)
	return out, e == nil, e
}
func decodePublicExternalCache(q agentworkspace.OnlinePlaceCacheScope, raw []byte, version int64, now time.Time) (*agentworkspace.OnlinePlaceSearch, error) {
	var v agentworkspace.OnlinePlaceSearch
	if json.Unmarshal(raw, &v) != nil || !externalPlaceSearchValid(v, now, false) || v.CityID != q.CityID || v.Scope != "city" || version < 1 ||
		v.Query != "" || v.RequestID != "" || v.TaskID != "" || v.SnapshotID != "" || v.TurnDigest != "" {
		return nil, arp.ErrChanged
	}
	for _, p := range v.Places {
		if externalPlaceDistance(q.Latitude, q.Longitude, *p.Location.Latitude, *p.Location.Longitude) > float64(q.RadiusMeters) {
			return nil, arp.ErrChanged
		}
	}
	v.CacheVersion, v.Origin, v.Freshness = version, "CACHE", "FRESH"
	if !v.ExpiresAt.After(now) {
		v.Freshness = "STALE"
	}
	return &v, nil
}

func (s *Store) StorePublicOnlinePlaceCache(ctx context.Context, q agentworkspace.OnlinePlaceCacheScope, search *agentworkspace.OnlinePlaceSearch) (*agentworkspace.OnlinePlaceSearch, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, arp.ErrUnavailable
	}
	if search == nil {
		return nil, arp.ErrInvalid
	}
	v, e := publicExternalCacheSearch(q, *search, time.Now().UTC())
	if e != nil {
		return nil, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	out, e := storePublicExternalCacheTx(ctx, tx, q, v)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return out, ctx.Err()
}

func storePublicExternalCacheTx(ctx context.Context, tx pgx.Tx, q agentworkspace.OnlinePlaceCacheScope, v agentworkspace.OnlinePlaceSearch) (*agentworkspace.OnlinePlaceSearch, error) {
	var city string
	// A hidden/moved/expired catalog scope cannot receive a fresh cache record.
	if e := tx.QueryRow(ctx, `SELECT id FROM cities WHERE id=$1 AND publication_status='published'
 AND map_center_latitude=$2 AND map_center_longitude=$3 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, q.CityID, q.Latitude, q.Longitude).Scan(&city); e != nil {
		return nil, e
	}
	for _, p := range v.Places {
		public := p
		public.CityID = "" // City association belongs to the search batch, not global identity.
		raw, e := json.Marshal(public)
		if e != nil {
			return nil, e
		}
		id := strings.TrimPrefix(p.ID, "osm:")
		id = strings.Replace(id, ":", "/", 1)
		if _, e = tx.Exec(ctx, `INSERT INTO external_public_places(provider,provider_place_id,payload,license_url,license_version,attribution,observed_at,fresh_until)
 VALUES($1,$2,$3,$4,'ODbL-1.0',$5,$6,$7) ON CONFLICT(provider,provider_place_id) DO UPDATE SET
 payload=EXCLUDED.payload,observed_at=EXCLUDED.observed_at,fresh_until=EXCLUDED.fresh_until,version=external_public_places.version+1
 WHERE external_public_places.observed_at<=EXCLUDED.observed_at`, q.Provider, id, raw, externalPlaceLicenseURL, externalPlaceAttribution, p.ObservedAt, p.ExpiresAt); e != nil {
			return nil, e
		}
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	var version int64
	e = tx.QueryRow(ctx, `INSERT INTO external_public_place_search_cache(cache_key,city_id,provider,category_code,center_latitude,center_longitude,radius_meters,policy_version,status,payload,observed_at,fresh_until)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(cache_key) DO UPDATE SET status=EXCLUDED.status,payload=EXCLUDED.payload,
 observed_at=EXCLUDED.observed_at,fresh_until=EXCLUDED.fresh_until,version=external_public_place_search_cache.version+1
 WHERE external_public_place_search_cache.observed_at<=EXCLUDED.observed_at RETURNING version`, q.CacheKey(), q.CityID, q.Provider, q.CategoryCode, q.Latitude, q.Longitude, q.RadiusMeters, q.PolicyVersion, v.Status, raw, v.ObservedAt, v.ExpiresAt).Scan(&version)
	if e != nil {
		return nil, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return nil, e
	}
	return decodePublicExternalCache(q, raw, version, now)
}
