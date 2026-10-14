package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Maintenance is explicitly invoked against a named owned disposable database.
// It is not an HTTP capability, scheduler, model event, or production rollout.
type MediaRetentionResult struct {
	PlacePhotos                int  `json:"placePhotos"`
	EntityCovers               int  `json:"entityCovers"`
	PublicMomentPhotos         int  `json:"publicMomentPhotos"`
	PublicMomentPhotoSupported bool `json:"publicMomentPhotoSupported"`
}

var ErrMediaRetentionGuard = errors.New("exact owned media retention database required")

func ValidMediaRetentionOwnership(database, marker string) bool {
	return strings.HasPrefix(database, "birdtie_owned_") && len(database) <= 63 && strings.HasPrefix(marker, "birdtie-human-media-owned:") && len(marker) > len("birdtie-human-media-owned:") && len(marker) <= 160 && !strings.ContainsAny(database+marker, "\x00\r\n")
}
func mediaRetentionOwned(ctx context.Context, tx pgx.Tx, database, marker string) error {
	var actual, comment string
	var ready bool
	e := tx.QueryRow(ctx, `SELECT current_database(),coalesce(shobj_description(oid,'pg_database'),''),to_regprocedure('public.birdtie_purge_human_media_derivatives(integer)') IS NOT NULL FROM pg_database WHERE datname=current_database()`).Scan(&actual, &comment, &ready)
	if e != nil || actual != database || comment != marker || !ready {
		return ErrMediaRetentionGuard
	}
	return nil
}
func mediaRetentionPublicMomentCapability(ctx context.Context, tx pgx.Tx) (bool, error) {
	var table, function bool
	if e := tx.QueryRow(ctx, `SELECT to_regclass('public.public_moment_photos') IS NOT NULL,to_regprocedure('public.birdtie_purge_public_moment_photo_derivatives(integer)') IS NOT NULL`).Scan(&table, &function); e != nil || table != function {
		return false, ErrMediaRetentionGuard
	}
	return table, nil
}
func (s *Store) PurgeHumanMediaDerivatives(ctx context.Context, database, marker string, batch int) (MediaRetentionResult, error) {
	out := MediaRetentionResult{}
	if !ValidMediaRetentionOwnership(database, marker) || batch < 1 || batch > 100 || ctx == nil || ctx.Err() != nil {
		return out, ErrMediaRetentionGuard
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, ErrMediaRetentionGuard
	}
	defer tx.Rollback(context.Background())
	if e = mediaRetentionOwned(ctx, tx, database, marker); e != nil {
		return out, e
	}
	publicReady, e := mediaRetentionPublicMomentCapability(ctx, tx)
	if e != nil {
		return out, e
	}
	out.PublicMomentPhotoSupported = publicReady
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,138))`, database+":"+marker+":retention"); e != nil {
		return out, ErrMediaRetentionGuard
	}
	if e = tx.QueryRow(ctx, `SELECT place_photos,entity_covers FROM birdtie_purge_human_media_derivatives($1)`, batch).Scan(&out.PlacePhotos, &out.EntityCovers); e != nil {
		return MediaRetentionResult{}, errors.New("owned media purge unavailable")
	}
	if publicReady {
		if e = tx.QueryRow(ctx, `SELECT birdtie_purge_public_moment_photo_derivatives($1)`, batch).Scan(&out.PublicMomentPhotos); e != nil {
			return MediaRetentionResult{}, errors.New("owned public Moment photo purge unavailable")
		}
	}
	if e = mediaRetentionOwned(ctx, tx, database, marker); e != nil {
		return MediaRetentionResult{}, e
	}
	if ctx.Err() != nil {
		return MediaRetentionResult{}, ctx.Err()
	}
	if e = tx.Commit(ctx); e != nil {
		return MediaRetentionResult{}, errors.New("owned media purge unavailable")
	}
	return out, nil
}
