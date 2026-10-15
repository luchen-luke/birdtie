package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

func readJourneyDetailsTx(ctx context.Context, tx pgx.Tx, owner, id string) (*content.JourneyDetails, error) {
	var schema *string
	v := content.JourneyDetails{Itinerary: []content.JourneyEntry{}}
	e := tx.QueryRow(ctx, `SELECT detail_schema,coalesce(journey_start_date::text,''),coalesce(journey_end_date::text,''),journey_date_label,journey_days FROM journeys WHERE id=$1 AND owner_account_id=$2`, id, owner).Scan(&schema, &v.StartDate, &v.EndDate, &v.DateLabel, &v.Days)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	if schema == nil {
		return nil, nil
	}
	if *schema != content.JourneyDetailsSchema {
		return nil, content.ErrUnavailable
	}
	v.SchemaVersion = *schema
	rows, e := tx.Query(ctx, `SELECT e.material,o.origin FROM journey_itinerary_entries e LEFT JOIN legacy_journey_entry_origins o ON o.journey_id=e.journey_id AND o.entry_id=e.entry_id WHERE e.journey_id=$1 ORDER BY e.sequence_no`, id)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var raw, origin []byte
		var entry content.JourneyEntry
		if rows.Scan(&raw, &origin) != nil || json.Unmarshal(raw, &entry) != nil {
			return nil, content.ErrUnavailable
		}
		if len(origin) > 0 {
			if json.Unmarshal(origin, &entry.Origin) != nil {
				return nil, content.ErrUnavailable
			}
		}
		v.Itinerary = append(v.Itinerary, entry)
	}
	if rows.Err() != nil {
		return nil, content.ErrUnavailable
	}
	rows.Close()
	var source string
	if e = tx.QueryRow(ctx, `SELECT jsonb_build_array(j.xmin::text,(SELECT coalesce(jsonb_agg(jsonb_build_array(e.xmin::text,e.entry_id,e.material,o.xmin::text) ORDER BY e.sequence_no),'[]'::jsonb) FROM journey_itinerary_entries e LEFT JOIN legacy_journey_entry_origins o ON o.journey_id=e.journey_id AND o.entry_id=e.entry_id WHERE e.journey_id=j.id))::text FROM journeys j WHERE j.id=$1 AND j.owner_account_id=$2`, id, owner).Scan(&source); e != nil {
		return nil, content.ErrUnavailable
	}
	v.SourceVersion = identity.HumanControlTextHash(source)
	return &v, nil
}

func writeJourneyDetailsTx(ctx context.Context, tx pgx.Tx, id string, v content.JourneyDetails) error {
	if _, e := tx.Exec(ctx, `DELETE FROM journey_itinerary_entries WHERE journey_id=$1`, id); e != nil {
		return content.ErrUnavailable
	}
	for _, entry := range v.Itinerary {
		entry.Origin = nil
		raw, e := json.Marshal(entry)
		if e != nil {
			return content.ErrInvalid
		}
		_, e = tx.Exec(ctx, `INSERT INTO journey_itinerary_entries(journey_id,entry_id,sequence_no,entry_type,city_id,place_id,stop_id,from_stop_id,to_stop_id,material)VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,$10)`, id, entry.ID, entry.Position, entry.EntryType, entry.CityID, entry.PlaceID, entry.StopID, entry.FromStopID, entry.ToStopID, raw)
		if e != nil {
			return content.ErrUnavailable
		}
	}
	_, e := tx.Exec(ctx, `UPDATE journeys SET detail_schema=$2,journey_start_date=NULLIF($3,'')::date,journey_end_date=NULLIF($4,'')::date,journey_date_label=$5,journey_days=$6 WHERE id=$1`, id, v.SchemaVersion, v.StartDate, v.EndDate, v.DateLabel, v.Days)
	if e != nil {
		return content.ErrUnavailable
	}
	return nil
}

func journeyRichTx(ctx context.Context, tx pgx.Tx, owner, id string) (bool, error) {
	var rich bool
	e := tx.QueryRow(ctx, `SELECT detail_schema IS NOT NULL FROM journeys WHERE id=$1 AND owner_account_id=$2`, id, owner).Scan(&rich)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, content.ErrNotFound
	}
	if e != nil {
		return false, content.ErrUnavailable
	}
	return rich, nil
}

// Retained draft references must still be in this exact current itinerary row,
// with its immutable 146/149/158 owner/source chain and original 156 Place.
// Merely knowing an origin or deleted entry ID never permits adding it again.
func journeyDetailsAuthorityTx(ctx context.Context, tx pgx.Tx, owner, id string, in content.CollectionInput) (bool, string, *time.Time, error) {
	if in.Kind != "journey" {
		return false, "", nil, nil
	}
	rich := false
	if id != "" {
		var e error
		rich, e = journeyRichTx(ctx, tx, owner, id)
		if e != nil {
			return true, "", nil, e
		}
	}
	if in.JourneyDetails == nil {
		if rich {
			return true, "", nil, content.ErrConflict
		}
		return false, "", nil, nil
	}
	if id != "" && !rich {
		return true, "", nil, content.ErrConflict
	}
	values := []any{"journey-details-authority-v1", id}
	var deadline *time.Time
	cityIDs := map[string]bool{}
	if in.CityID != "" {
		cityIDs[in.CityID] = true
	}
	for _, entry := range in.JourneyDetails.Itinerary {
		if entry.CityID != "" {
			cityIDs[entry.CityID] = true
		}
	}
	cities := []string{}
	for c := range cityIDs {
		cities = append(cities, c)
	}
	sort.Strings(cities)
	for _, city := range cities {
		var stamp string
		var until *time.Time
		e := tx.QueryRow(ctx, `SELECT xmin::text,expires_at FROM cities WHERE id=$1 AND publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, city).Scan(&stamp, &until)
		if e != nil {
			return true, "", nil, content.ErrConflict
		}
		values = append(values, []any{city, stamp})
		momentControlAddDeadline(&deadline, until)
	}
	entries := append([]content.JourneyEntry{}, in.JourneyDetails.Itinerary...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	for _, entry := range entries {
		if id != "" {
			var removed, wrongType bool
			e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_journey_entry_origins o WHERE o.journey_id=$1 AND o.entry_id=$2 AND NOT EXISTS(SELECT 1 FROM journey_itinerary_entries e WHERE e.journey_id=o.journey_id AND e.entry_id=o.entry_id)),EXISTS(SELECT 1 FROM journey_itinerary_entries e WHERE e.journey_id=$1 AND e.entry_id=$2 AND e.entry_type<>$3 AND EXISTS(SELECT 1 FROM legacy_journey_entry_origins o WHERE o.journey_id=e.journey_id AND o.entry_id=e.entry_id))`, id, entry.ID, entry.EntryType).Scan(&removed, &wrongType)
			if e != nil {
				return true, "", nil, content.ErrUnavailable
			}
			if removed || wrongType {
				return true, "", nil, content.ErrConflict
			}
		}
		if entry.PlaceID == "" {
			continue
		}
		var stamp string
		var until *time.Time
		e := tx.QueryRow(ctx, `SELECT concat_ws(':',p.xmin::text,`+retainedPlaceReadBindingSQL("p")+`),`+currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()")+` FROM places p WHERE p.id=$1 AND($2='' OR p.city_id=$2) AND p.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` FOR SHARE`, entry.PlaceID, entry.CityID).Scan(&stamp, &until)
		if errors.Is(e, pgx.ErrNoRows) && id != "" {
			e = tx.QueryRow(ctx, `SELECT jsonb_build_array(p.xmin::text,m.xmin::text,ar.xmin::text,u.xmin::text,pm.xmin::text,o.xmin::text,`+retainedPlaceReadBindingSQL("p")+`)::text,`+currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()")+` FROM journey_itinerary_entries e JOIN journeys j ON j.id=e.journey_id JOIN legacy_journey_entry_origins o ON o.journey_id=e.journey_id AND o.entry_id=e.entry_id AND o.original_place_id=e.place_id JOIN legacy_journey_adaptations m ON m.target_id=j.id AND m.run_id=o.run_id AND m.source_key=o.source_key JOIN legacy_personal_archives ar ON ar.run_id=m.run_id AND ar.kind='journey' AND ar.source_key=m.source_key AND ar.source_sha256=m.source_sha256 AND ar.owner_account_id=m.owner_account_id AND NOT ar.deleted JOIN legacy_entity_mappings u ON u.source_system=ar.source_system AND u.entity_kind='user' AND u.source_id=ar.source_owner_id AND u.target_id=ar.owner_account_id AND u.owner_account_id=ar.owner_account_id AND u.run_id=ar.run_id JOIN places p ON p.id=e.place_id JOIN legacy_native_adaptation_entities pm ON pm.kind='place' AND pm.target_id=p.id::text AND pm.run_id=m.run_id AND pm.source_key=o.origin->>'sourcePlaceId' WHERE e.journey_id=$1 AND e.entry_id=$2 AND e.place_id=$3 AND j.owner_account_id=$4 AND m.owner_account_id=$4 AND j.visibility='private' AND j.status='draft' AND p.publication_status='draft' AND($5='' OR p.city_id=$5) AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` FOR SHARE OF p`, id, entry.ID, entry.PlaceID, owner, entry.CityID).Scan(&stamp, &until)
		}
		if e != nil {
			return true, "", nil, content.ErrConflict
		}
		values = append(values, []any{entry.ID, entry.PlaceID, stamp})
		momentControlAddDeadline(&deadline, until)
	}
	return true, content.MomentControlHash(values), deadline, nil
}
