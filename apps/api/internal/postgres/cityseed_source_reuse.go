package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
)

type citySeedSourceReuse struct {
	placeID    string
	validUntil time.Time
}

var errCurrentCitySeedSourceDuplicate = errors.Join(cityseed.ErrDuplicate, cityseed.ErrSourceAlreadyCurrent)

// External identity is already canonical in place_external_refs. Reuse is
// checked in Submit's original authorised transaction before any new content
// write; an existing pending row never masks a still-current published Place.
func currentCitySeedSourceReuseTx(ctx context.Context, tx pgx.Tx, cityID string, input cityseed.SubmitInput, at time.Time) (citySeedSourceReuse, error) {
	if input.ProviderCode == "" || input.ProviderPlaceID == "" {
		return citySeedSourceReuse{}, nil
	}
	var placeID, boundCity, sourceURL string
	err := tx.QueryRow(ctx, `SELECT r.place_id,p.city_id,r.source_url
 FROM place_external_refs r JOIN places p ON p.id=r.place_id
 WHERE r.provider_code=$1 AND r.provider_place_id=$2 FOR SHARE OF r,p`, input.ProviderCode, input.ProviderPlaceID).Scan(&placeID, &boundCity, &sourceURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return citySeedSourceReuse{}, nil
	}
	if err != nil {
		return citySeedSourceReuse{}, err
	}
	if boundCity != cityID || !egressUUID(placeID) {
		return citySeedSourceReuse{}, cityseed.ErrConflict
	}
	if sourceURL != input.SourceURL {
		return citySeedSourceReuse{}, nil
	}
	place, err := scanPlace(tx.QueryRow(ctx, `SELECT `+placeColumns+`
 FROM places p JOIN cities city ON city.id=p.city_id
 WHERE p.id=$1 AND p.city_id=$2 AND p.source_ref=$3
 AND p.publication_status='published' AND city.publication_status='published'
 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())
 AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp())
 AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+`
 FOR SHARE OF p,city`, placeID, cityID, sourceURL))
	if errors.Is(err, foundation.ErrNotFound) {
		return citySeedSourceReuse{}, nil
	}
	if err != nil {
		return citySeedSourceReuse{}, err
	}
	// Read the full existing supported field set, not only keys in this new
	// observation. Otherwise a removed hours tag could be called unchanged.
	keys := []string{"name", "address", "coordinates", "food_seafood", "food_spicy", "opening_hours"}
	fields, err := ReadPlaceFieldEvidenceTx(ctx, tx, placeID, keys, at)
	if err != nil {
		return citySeedSourceReuse{}, err
	}
	until, same := sameCurrentCitySeedSourceFacts(place, input, fields, at)
	if !same {
		return citySeedSourceReuse{}, nil
	}
	// The city is locked above; its expiry is a separate final native fence.
	var cityUntil *time.Time
	if err = tx.QueryRow(ctx, `SELECT expires_at FROM cities WHERE id=$1 AND publication_status='published' FOR SHARE`, cityID).Scan(&cityUntil); err != nil {
		return citySeedSourceReuse{}, err
	}
	if cityUntil != nil {
		until = retryMinimum(until, *cityUntil)
	}
	if !until.After(at) {
		return citySeedSourceReuse{}, nil
	}
	return citySeedSourceReuse{placeID: placeID, validUntil: until}, nil
}

// A new observation time/expiry is not a grant to refresh retained data. Only
// the same complete objective facts under the current independently reviewed
// source licence are a duplicate. Changes/unknowns retain the original review
// path, and this helper never modifies the canonical Place or field records.
func sameCurrentCitySeedSourceFacts(place foundation.Place, input cityseed.SubmitInput, fields []cityseed.PlaceFieldEvidence, at time.Time) (time.Time, bool) {
	if at.IsZero() || input.Rights == nil || len(input.Fields) == 0 || len(input.Fields) > 6 ||
		place.ID == "" || place.Name != input.Name || place.CategoryCode != input.CategoryCode ||
		place.Summary != input.Summary || place.AddressLabel != input.AddressLabel ||
		place.Source.Reference != input.SourceURL || place.Location.Precision != input.LocationPrecision ||
		!reflect.DeepEqual(place.Location.Latitude, input.Latitude) || !reflect.DeepEqual(place.Location.Longitude, input.Longitude) {
		return time.Time{}, false
	}
	if _, matches := localFieldsForPlace(place, fields); !matches {
		return time.Time{}, false
	}
	until := time.Time{}
	if place.Source.ExpiresAt != nil {
		until = *place.Source.ExpiresAt
	}
	seen := map[string]bool{}
	for _, claim := range input.Fields {
		if seen[claim.Key] || !cityseed.ValidPlaceFieldValue(claim.Key, claim.Value) || claim.ConflictState != "NONE" {
			return time.Time{}, false
		}
		seen[claim.Key] = true
	}
	for _, field := range fields {
		if field.Permission.SourceURL == input.SourceURL && !seen[field.FieldKey] {
			return time.Time{}, false
		}
	}
	for _, claim := range input.Fields {
		matched := false
		for _, field := range fields {
			if field.FieldKey != claim.Key || field.Permission.SourceURL != input.SourceURL {
				continue
			}
			permission := field.Permission
			if field.PlaceID != place.ID || field.ID == "" || field.Version < 1 || field.RowToken == "" ||
				field.PermissionID != permission.ID || field.PermissionVersion != permission.Version || field.SourceCandidateID != permission.CandidateID ||
				permission.SourceURL != input.SourceURL || !cityseed.PermissionUsable(permission, at) ||
				permission.Acquisition != input.Rights.Acquisition || permission.OriginProvider != input.Rights.OriginProvider ||
				permission.LicenseURL != input.Rights.LicenseURL || permission.LicenseVersion != input.Rights.LicenseVersion || permission.Attribution != input.Rights.Attribution ||
				!cityseed.ValidPlaceFieldValue(field.FieldKey, field.Value) || field.ConflictState != "NONE" ||
				field.ObservedAt.IsZero() || field.ObservedAt.After(at) || field.VerifiedAt.IsZero() || field.VerifiedAt.Before(field.ObservedAt) || field.VerifiedAt.After(at) ||
				field.ValidFrom.IsZero() || field.ValidFrom.After(at) || !field.ValidTo.After(at) || !field.RefreshDue.After(at) {
				return time.Time{}, false
			}
			var current, incoming any
			if json.Unmarshal(field.Value, &current) != nil || json.Unmarshal(claim.Value, &incoming) != nil || !reflect.DeepEqual(current, incoming) {
				return time.Time{}, false
			}
			until = retryMinimum(until, retryMinimum(field.ValidTo, retryMinimum(field.RefreshDue, permission.RetentionUntil)))
			matched = true
		}
		if !matched {
			return time.Time{}, false
		}
	}
	return until, until.After(at)
}
