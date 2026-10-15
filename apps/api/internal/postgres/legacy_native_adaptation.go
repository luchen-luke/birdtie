package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
	"github.com/jackc/pgx/v5"
)

// Counts describe the original committed phase, not current publication or
// media availability. Later user edits and deletions never rewrite that fact.
type LegacyNativeCounts struct {
	Cities             int `json:"cities"`
	Places             int `json:"places"`
	Moments            int `json:"moments"`
	Journeys           int `json:"journeys"`
	ThemeMaps          int `json:"themeMaps"`
	SavedPlaces        int `json:"savedPlaces"`
	CityAliases        int `json:"cityAliases"`
	MergedPlaceAliases int `json:"mergedPlaceAliases"`
}
type LegacyNativeAdaptationResult struct {
	RunID    string             `json:"runId"`
	Status   string             `json:"status"`
	Counts   LegacyNativeCounts `json:"counts"`
	Deferred map[string]int     `json:"deferred"`
}
type nativeAdaptationPlan struct {
	original legacymigration.Plan
	catalog  legacycatalog.Plan
	personal legacynative.Plan
	digest   string
	result   LegacyNativeAdaptationResult
}

func prepareNativeAdaptation(run string, source, manifest []byte) (nativeAdaptationPlan, error) {
	p := nativeAdaptationPlan{}
	var e error
	if p.original, e = legacymigration.BuildPlan(run, source, manifest); e != nil {
		return p, ErrLegacyMigrationInvalid
	}
	if p.catalog, e = legacycatalog.Build(run, source, manifest); e != nil {
		return p, ErrLegacyMigrationInvalid
	}
	if p.personal, e = legacynative.Build(run, source, manifest); e != nil {
		return p, ErrLegacyMigrationInvalid
	}
	p.result = LegacyNativeAdaptationResult{RunID: run, Status: "PREPARED_NOT_IMPORTED", Deferred: map[string]int{}}
	p.result.Counts = LegacyNativeCounts{Cities: len(p.catalog.Cities), Places: len(p.catalog.Places), Moments: len(p.personal.Notes), SavedPlaces: len(p.personal.SavedItems)}
	p.result.Counts.CityAliases = len(p.catalog.CityMappings)
	p.result.Counts.MergedPlaceAliases = len(p.catalog.MergedAliases)
	for _, c := range p.personal.Collections {
		if c.Kind == "journey" {
			p.result.Counts.Journeys++
		} else if c.Kind == "theme_map" {
			p.result.Counts.ThemeMaps++
		} else {
			return p, ErrLegacyMigrationInvalid
		}
	}
	for k, v := range p.catalog.Deferred {
		p.result.Deferred["catalog."+k] = v
	}
	for k, v := range p.personal.Deferred {
		p.result.Deferred["personal."+k] = v
	}
	p.digest, e = legacymigration.Digest(map[string]any{"protocol": "birdtie-civu-native-adaptation-v1", "catalog": p.catalog.ProjectionSHA256, "personal": p.personal.ProjectionSHA256})
	if e != nil {
		return p, ErrLegacyMigrationInvalid
	}
	return p, nil
}
func PrepareLegacyNativeAdaptation(run string, source, manifest []byte) (LegacyNativeAdaptationResult, error) {
	p, e := prepareNativeAdaptation(run, source, manifest)
	return p.result, e
}
func nativeAdaptationRead(ctx context.Context, tx pgx.Tx, run string) (LegacyNativeAdaptationResult, string, error) {
	r := LegacyNativeAdaptationResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var counts, deferred []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts,deferred FROM legacy_native_adaptation_runs WHERE run_id=$1`, run).Scan(&digest, &counts, &deferred)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(counts, &r.Counts) != nil || json.Unmarshal(deferred, &r.Deferred) != nil {
		return r, "", ErrLegacyMigrationUnavailable
	}
	var actual LegacyNativeCounts
	if e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='city'),count(*) FILTER(WHERE kind='merged_place') FROM legacy_native_catalog_aliases WHERE run_id=$1`, run).Scan(&actual.CityAliases, &actual.MergedPlaceAliases); e != nil {
		return r, "", ErrLegacyMigrationUnavailable
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='city'),count(*) FILTER(WHERE kind='place'),count(*) FILTER(WHERE kind='moment'),count(*) FILTER(WHERE kind='journey'),count(*) FILTER(WHERE kind='theme_map'),count(*) FILTER(WHERE kind='saved_place') FROM legacy_native_adaptation_entities WHERE run_id=$1`, run).Scan(&actual.Cities, &actual.Places, &actual.Moments, &actual.Journeys, &actual.ThemeMaps, &actual.SavedPlaces)
	if e != nil || actual != r.Counts {
		return r, "", ErrLegacyMigrationUnavailable
	}
	return r, digest, nil
}
func (s *LegacyMigrationImporter) ApplyNativeAdaptation(ctx context.Context, run string, source, manifest []byte) (LegacyNativeAdaptationResult, error) {
	unknown := LegacyNativeAdaptationResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := prepareNativeAdaptation(run, source, manifest)
	if e != nil {
		return unknown, e
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,156))`, legacymigration.SourceSystem); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil {
		return unknown, e
	}
	if !legacyMigrationSameRun(original, p.original) {
		return unknown, ErrLegacyMigrationConflict
	}
	// Collection archives must already exist: source data never bypasses the
	// permanent original owner binding or the immutable deletion record.
	if _, _, e = s.personalArchiveRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	previous, digest, e := nativeAdaptationRead(ctx, tx, run)
	if e == nil {
		if digest != p.digest || previous.Counts != p.result.Counts {
			return unknown, ErrLegacyMigrationConflict
		}
		if e = tx.Commit(ctx); e != nil {
			return unknown, ErrLegacyMigrationUnknown
		}
		return previous, nil
	}
	if !errors.Is(e, ErrLegacyMigrationNotFound) {
		return unknown, e
	}
	counts, _ := json.Marshal(p.result.Counts)
	deferred, _ := json.Marshal(p.result.Deferred)
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_native_adaptation_runs(run_id,protocol,projection_sha256,counts,deferred)VALUES($1,'birdtie-civu-native-adaptation-v1',$2,$3,$4)`, run, p.digest, counts, deferred); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	owners := map[string]string{}
	for _, o := range p.original.Owners {
		var target string
		if e = tx.QueryRow(ctx, `SELECT m.target_id FROM legacy_entity_mappings m JOIN accounts a ON a.id=m.target_id WHERE m.source_system=$1 AND m.entity_kind='user' AND m.source_id=$2 AND m.run_id=$3 AND a.account_type='person' AND a.status IN('suspended','active') FOR SHARE OF a`, legacymigration.SourceSystem, o.SourceID, run).Scan(&target); e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		owners[o.SourceID] = target
	}
	record := func(kind, key, hash, target, owner string) error {
		_, err := tx.Exec(ctx, `INSERT INTO legacy_native_adaptation_entities(run_id,kind,source_key,source_sha256,target_id,owner_account_id)VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid)`, run, kind, key, hash, target, owner)
		return err
	}
	for _, c := range p.catalog.Cities {
		if c.ReuseExisting {
			var same bool
			e = tx.QueryRow(ctx, `SELECT name=$2 AND country_code=$3 AND time_zone=$4 AND publication_status='published' FROM cities WHERE id=$1 FOR SHARE`, c.TargetID, c.Name, c.CountryCode, c.TimeZone).Scan(&same)
			if e != nil || !same {
				return unknown, ErrLegacyMigrationConflict
			}
		} else {
			_, e = tx.Exec(ctx, `INSERT INTO cities(id,name,region,country_code,time_zone,content_status,publication_status,source_label,source_ref,maintainer_label)VALUES($1,$2,$3,$4,$5,'building','published','Civu 城市目录',$6,'Birdtie 迁移维护')`, c.TargetID, c.Name, c.Region, c.CountryCode, c.TimeZone, "civu:geo_area:"+c.SourceID)
		}
		if e == nil {
			e = record("city", c.SourceID, c.SourceSHA256, c.TargetID, "")
		}
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.catalog.Places {
		precision := "none"
		if v.Latitude != nil && v.Longitude != nil {
			precision = "point"
		}
		_, e = tx.Exec(ctx, `INSERT INTO places(id,city_id,name,category_code,summary,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,$2,$3,$4,$5,$6,$7,$8,'draft',$9,$10,'Birdtie migration')`, v.TargetID, v.CityID, v.Name, v.CategoryCode, v.Summary, v.Latitude, v.Longitude, precision, v.SourceLabel, v.SourceRef)
		if e == nil {
			e = record("place", v.SourceID, v.SourceSHA256, v.TargetID, "")
		}
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for source, target := range p.catalog.CityMappings {
		if _, e = tx.Exec(ctx, `INSERT INTO legacy_native_catalog_aliases(run_id,kind,source_id,target_city_id)VALUES($1,'city',$2,$3)`, run, source, target); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for source, canonical := range p.catalog.MergedAliases {
		if _, e = tx.Exec(ctx, `INSERT INTO legacy_native_catalog_aliases(run_id,kind,source_id,canonical_source_place_id)VALUES($1,'merged_place',$2,$3)`, run, source, canonical); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.personal.Notes {
		owner := owners[v.OwnerSourceID]
		if owner == "" {
			return unknown, ErrLegacyMigrationConflict
		}
		_, e = tx.Exec(ctx, `INSERT INTO moments(id,author_account_id,city_id,place_id,title,body,time_precision,location_precision,visibility,status,created_at,updated_at)VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')::uuid,$5,$6,'unknown',$7,'private','draft',coalesce($8,clock_timestamp()),coalesce($9,clock_timestamp()))`, v.TargetID, owner, v.CityID, v.PlaceID, v.Title, v.Body, v.LocationPrecision, v.SourceCreatedAt, v.SourceUpdatedAt)
		if e == nil {
			e = record("moment", v.SourceKey, v.SourceSHA256, v.TargetID, owner)
		}
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.personal.Collections {
		owner := owners[v.OwnerSourceID]
		if owner == "" {
			return unknown, ErrLegacyMigrationConflict
		}
		table, entries, key, err := collectionTables(v.Kind)
		if err != nil {
			return unknown, ErrLegacyMigrationInvalid
		}
		_, e = tx.Exec(ctx, `INSERT INTO `+table+`(id,owner_account_id,city_id,title,summary,visibility,status,created_at,updated_at)VALUES($1,$2,$3,$4,$5,'private','draft',coalesce($6,clock_timestamp()),coalesce($7,clock_timestamp()))`, v.TargetID, owner, v.CityID, v.Title, v.Summary, v.SourceCreatedAt, v.SourceUpdatedAt)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		for i, entry := range v.Entries {
			if _, e = tx.Exec(ctx, `INSERT INTO `+entries+`(`+key+`,city_id,sequence_no,place_id,note,location_visibility)VALUES($1,$2,$3,$4,$5,'hidden')`, v.TargetID, v.CityID, i+1, entry.PlaceID, entry.Note); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
		}
		if e = record(v.Kind, v.SourceKey, v.SourceSHA256, v.TargetID, owner); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.personal.SavedItems {
		owner := owners[v.OwnerSourceID]
		if owner == "" {
			return unknown, ErrLegacyMigrationConflict
		}
		_, e = tx.Exec(ctx, `INSERT INTO saved_items(id,owner_account_id,place_id,created_at)VALUES($1,$2,$3,coalesce($4,clock_timestamp()))`, v.TargetID, owner, v.PlaceID, v.SourceCreatedAt)
		if e == nil {
			e = record("saved_place", v.SourceKey, v.SourceSHA256, v.TargetID, owner)
		}
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.personal.Residuals {
		owner := owners[v.OwnerSourceID]
		if owner == "" {
			return unknown, ErrLegacyMigrationConflict
		}
		_, e = tx.Exec(ctx, `INSERT INTO legacy_native_adaptation_residuals(run_id,kind,source_key,source_sha256,owner_account_id,target_id,reasons)VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7)`, run, v.Kind, v.SourceKey, v.SourceSHA256, owner, v.TargetID, v.Reasons)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	result, digest, e := nativeAdaptationRead(ctx, tx, run)
	if e != nil || digest != p.digest {
		return unknown, ErrLegacyMigrationUnavailable
	}
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return result, nil
}
func (s *LegacyMigrationImporter) InspectNativeAdaptation(ctx context.Context, run string) (LegacyNativeAdaptationResult, error) {
	unknown := LegacyNativeAdaptationResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || !legacymigration.ValidRunID(run) || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	result, _, e := nativeAdaptationRead(ctx, tx, run)
	if e != nil {
		return unknown, e
	}
	if e = tx.Commit(ctx); e != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return result, nil
}
