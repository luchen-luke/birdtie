package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool            *pgxpool.Pool
	devPhoneEnabled bool
}

func New(pool *pgxpool.Pool, devPhoneEnabled bool) *Store {
	return &Store{pool: pool, devPhoneEnabled: devPhoneEnabled}
}

const cityColumns = `id, name, region, country_code, time_zone, content_status,
    source_label, source_ref, maintainer_label, updated_at, verified_at, expires_at,
    map_provider, map_center_latitude, map_center_longitude, map_default_zoom,
    map_viewport_source_ref`

func (s *Store) ListCities(ctx context.Context) ([]foundation.City, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cityColumns+`
        FROM cities WHERE publication_status = 'published' ORDER BY name, id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cities := make([]foundation.City, 0)
	for rows.Next() {
		city, err := scanCity(rows)
		if err != nil {
			return nil, err
		}
		cities = append(cities, city)
	}
	return cities, rows.Err()
}

func (s *Store) GetCity(ctx context.Context, id string) (foundation.City, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+cityColumns+`
        FROM cities WHERE id = $1 AND publication_status = 'published'`, id)
	return scanCity(row)
}

const placeColumns = `p.id, p.city_id, p.name, p.category_code, p.summary,
    p.coordinate_system, p.location_precision, p.latitude, p.longitude,
    p.source_label, p.source_ref, p.maintainer_label,
    p.updated_at, p.verified_at, p.expires_at`

func (s *Store) ListPlaces(ctx context.Context, cityID, query string) ([]foundation.Place, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+placeColumns+`
        FROM places p JOIN cities c ON c.id = p.city_id
        WHERE p.city_id = $1 AND p.publication_status = 'published'
          AND c.publication_status = 'published'
          AND ($2 = '' OR strpos(lower(p.name), lower($2)) > 0
            OR strpos(lower(p.summary), lower($2)) > 0)
        ORDER BY p.name, p.id LIMIT 100`, cityID, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	places := make([]foundation.Place, 0)
	for rows.Next() {
		place, err := scanPlace(rows)
		if err != nil {
			return nil, err
		}
		places = append(places, place)
	}
	return places, rows.Err()
}

func (s *Store) GetPlace(ctx context.Context, id string) (foundation.Place, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+placeColumns+`
        FROM places p JOIN cities c ON c.id = p.city_id
        WHERE p.id = $1 AND p.publication_status = 'published'
          AND c.publication_status = 'published'`, id)
	return scanPlace(row)
}

type scanner interface {
	Scan(...any) error
}

func scanCity(row scanner) (foundation.City, error) {
	var city foundation.City
	var provider, sourceRef *string
	var latitude, longitude, zoom *float64
	err := row.Scan(
		&city.ID, &city.Name, &city.Region, &city.CountryCode,
		&city.TimeZone, &city.ContentStatus, &city.Source.Label,
		&city.Source.Reference, &city.Source.Maintainer,
		&city.Source.UpdatedAt, &city.Source.VerifiedAt, &city.Source.ExpiresAt,
		&provider, &latitude, &longitude, &zoom, &sourceRef,
	)
	if err != nil {
		return foundation.City{}, notFound(err)
	}
	city.Source.SetFreshness(time.Now().UTC())
	if provider != nil && latitude != nil && longitude != nil && zoom != nil && sourceRef != nil {
		city.Map = &foundation.CityMap{
			Provider: *provider, Latitude: *latitude, Longitude: *longitude,
			DefaultZoom: *zoom, SourceRef: *sourceRef,
		}
	}
	return city, nil
}

func scanPlace(row scanner) (foundation.Place, error) {
	var place foundation.Place
	err := row.Scan(
		&place.ID, &place.CityID, &place.Name, &place.CategoryCode,
		&place.Summary, &place.Location.CoordinateSystem,
		&place.Location.Precision, &place.Location.Latitude,
		&place.Location.Longitude, &place.Source.Label,
		&place.Source.Reference, &place.Source.Maintainer,
		&place.Source.UpdatedAt, &place.Source.VerifiedAt,
		&place.Source.ExpiresAt,
	)
	if err != nil {
		return foundation.Place{}, notFound(err)
	}
	if place.Location.Precision != "point" || place.Location.CoordinateSystem != "wgs84" {
		place.Location.Latitude = nil
		place.Location.Longitude = nil
	}
	place.Source.SetFreshness(time.Now().UTC())
	return place, nil
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return foundation.ErrNotFound
	}
	return err
}
