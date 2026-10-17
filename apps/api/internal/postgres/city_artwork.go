package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityartwork"
	"github.com/jackc/pgx/v5"
)

// A metadata read never loads the archived image. Every byte/descriptor read
// re-resolves this exact public city and reviewed system source before cache use.
const cityArtworkSourceSQL = ` FROM city_artwork_projections p
 JOIN cities c ON c.id=p.city_id AND c.publication_status='published'
 JOIN legacy_system_artwork_archives a ON a.run_id=p.run_id AND a.source_geo_id=p.source_geo_id
   AND a.source_media_id=p.source_media_id AND a.source_version=p.source_version
 JOIN legacy_native_catalog_aliases n ON n.run_id=a.run_id AND n.kind='city'
   AND n.source_id=a.source_geo_id AND n.target_city_id=c.id
 JOIN legacy_reference_media_assets m ON m.run_id=a.run_id AND m.source_media_id=a.source_media_id
   AND m.input_sha256=p.input_sha256 AND m.system_archive
   AND m.validation_kind='AUTHENTICATED_SYSTEM_ARCHIVE_ONLY'
   AND m.owner_account_id IS NULL AND m.material_id IS NULL
 WHERE p.active`

// Include the derivative algorithm in the version so a later encoder cannot
// reuse the cache identity of different bytes for the same original.
const cityArtworkVersionSQL = `encode(sha256(convert_to(p.source_version||':'||p.input_sha256||':souvenir324-v1','UTF8')),'hex')`

const cityArtworkMetadataSQL = `(SELECT jsonb_build_object('version',` + cityArtworkVersionSQL + `,
 'descriptorPath','/v1/cities/'||c.id||'/artwork/'||` + cityArtworkVersionSQL + `||'/descriptor')` +
	cityArtworkSourceSQL + ` AND c.id=cities.id)`

type cityArtworkCached struct {
	content cityartwork.Content
	used    time.Time
}

var cityArtworkCache = struct {
	sync.Mutex
	items map[string]cityArtworkCached
	bytes int
}{items: map[string]cityArtworkCached{}}
var cityArtworkBuildSlots = make(chan struct{}, 2)

func cityArtworkCacheGet(key string) (cityartwork.Content, bool) {
	cityArtworkCache.Lock()
	defer cityArtworkCache.Unlock()
	v, ok := cityArtworkCache.items[key]
	if ok {
		v.used = time.Now()
		cityArtworkCache.items[key] = v
	}
	return v.content, ok
}
func cityArtworkCachePut(key string, v cityartwork.Content) {
	cityArtworkCache.Lock()
	defer cityArtworkCache.Unlock()
	if old, ok := cityArtworkCache.items[key]; ok {
		cityArtworkCache.bytes -= len(old.content.Bytes)
	}
	cityArtworkCache.items[key] = cityArtworkCached{v, time.Now()}
	cityArtworkCache.bytes += len(v.Bytes)
	for len(cityArtworkCache.items) > 32 || cityArtworkCache.bytes > 8*1024*1024 {
		oldest := ""
		var at time.Time
		for k, x := range cityArtworkCache.items {
			if oldest == "" || x.used.Before(at) {
				oldest = k
				at = x.used
			}
		}
		cityArtworkCache.bytes -= len(cityArtworkCache.items[oldest].content.Bytes)
		delete(cityArtworkCache.items, oldest)
	}
}

func (s *Store) ValidateCityArtwork(ctx context.Context, city, version string) error {
	if !cityartwork.ValidCity(city) || !cityartwork.ValidVersion(version) || ctx.Err() != nil {
		return cityartwork.ErrNotFound
	}
	var sha string
	e := s.pool.QueryRow(ctx, `SELECT p.input_sha256`+cityArtworkSourceSQL+` AND c.id=$1 AND `+cityArtworkVersionSQL+`=$2`, city, version).Scan(&sha)
	if errors.Is(e, pgx.ErrNoRows) {
		return cityartwork.ErrNotFound
	}
	if e != nil {
		return cityartwork.ErrUnavailable
	}
	return nil
}

func (s *Store) ReadCityArtwork(ctx context.Context, city, version string) (cityartwork.Content, error) {
	if e := s.ValidateCityArtwork(ctx, city, version); e != nil {
		return cityartwork.Content{}, e
	}
	key := city + ":" + version
	if v, ok := cityArtworkCacheGet(key); ok {
		return v, nil
	}
	select {
	case cityArtworkBuildSlots <- struct{}{}:
		defer func() { <-cityArtworkBuildSlots }()
	case <-ctx.Done():
		return cityartwork.Content{}, cityartwork.ErrUnavailable
	}
	if v, ok := cityArtworkCacheGet(key); ok {
		return v, nil
	}
	var raw []byte
	var inputSHA string
	e := s.pool.QueryRow(ctx, `SELECT m.original,p.input_sha256`+cityArtworkSourceSQL+` AND c.id=$1 AND `+cityArtworkVersionSQL+`=$2 AND m.input_bytes BETWEEN 1 AND 16777216`, city, version).Scan(&raw, &inputSHA)
	defer clear(raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return cityartwork.Content{}, cityartwork.ErrNotFound
	}
	if e != nil {
		return cityartwork.Content{}, cityartwork.ErrUnavailable
	}
	v, e := cityartwork.Build(ctx, city, version, inputSHA, raw)
	if e != nil {
		return cityartwork.Content{}, e
	}
	if e = s.ValidateCityArtwork(ctx, city, version); e != nil {
		return cityartwork.Content{}, e
	}
	cityArtworkCachePut(key, v)
	return v, nil
}

func cityArtworkProjectionVersion(source, inputSHA string) string {
	sum := sha256.Sum256([]byte(source + ":" + inputSHA + ":souvenir324-v1"))
	return hex.EncodeToString(sum[:])
}
