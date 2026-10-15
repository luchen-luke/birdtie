package postgres

import (
	"context"
	lr "github.com/birdtie/birdtie/apps/api/internal/locationreference"
	"time"
)

func (s *Store) ConfigureLocationSearch(p *lr.Pelias) { s.locationSearch.Store(p) }
func (s *Store) SearchLocationReferences(ctx context.Context, q string, n int) (lr.View, error) {
	at := time.Now().UTC()
	v := lr.View{SchemaVersion: "location-search-v1", Status: "UNAVAILABLE", Items: []lr.Candidate{}, ObservedAt: at, ValidUntil: at.Add(time.Minute)}
	if lr.ValidateQuery(q, n) != nil {
		return v, lr.ErrInvalid
	}
	p := s.locationSearch.Load()
	if p == nil {
		return v, nil
	}
	items, e := p.Search(ctx, q, n)
	if e != nil {
		return v, nil
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return v, lr.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	for _, c := range items {
		if !c.Valid() || c.SourceVersion != c.Version() {
			return v, lr.ErrUnavailable
		}
		_, e = tx.Exec(ctx, `INSERT INTO location_references(id,provider,object_kind,object_id)VALUES($1,'openstreetmap',$2,$3)ON CONFLICT(id)DO NOTHING`, c.ReferenceID, c.ObjectKind, c.ObjectID)
		if e != nil {
			return v, lr.ErrUnavailable
		}
		var active bool
		e = tx.QueryRow(ctx, `SELECT status='ACTIVE' AND provider='openstreetmap' AND object_kind=$2 AND object_id=$3 FROM location_references WHERE id=$1 FOR UPDATE`, c.ReferenceID, c.ObjectKind, c.ObjectID).Scan(&active)
		if e != nil {
			return v, lr.ErrUnavailable
		}
		if !active {
			continue
		}
		_, e = tx.Exec(ctx, `INSERT INTO location_reference_observations(reference_id,version,name,address,region,country_code,source_url,attribution,coordinate_system,latitude,longitude,observed_at,valid_until)VALUES($1,$2,$3,$4,$5,$6,$7,$8,'wgs84',$9,$10,$11,$12)ON CONFLICT(reference_id)DO UPDATE SET version=EXCLUDED.version,name=EXCLUDED.name,address=EXCLUDED.address,region=EXCLUDED.region,country_code=EXCLUDED.country_code,source_url=EXCLUDED.source_url,attribution=EXCLUDED.attribution,latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,observed_at=EXCLUDED.observed_at,valid_until=EXCLUDED.valid_until WHERE location_reference_observations.version<>EXCLUDED.version OR location_reference_observations.valid_until<=clock_timestamp()+interval '5 minutes'`, c.ReferenceID, c.SourceVersion, c.Name, c.Address, c.Region, c.CountryCode, c.SourceURL, c.Attribution, c.Latitude, c.Longitude, c.ObservedAt, c.ValidUntil)
		if e != nil {
			return v, lr.ErrUnavailable
		}
		// Return the retained observation's actual deadline, never the unused refresh.
		e = tx.QueryRow(ctx, `SELECT observed_at,valid_until FROM location_reference_observations WHERE reference_id=$1`, c.ReferenceID).Scan(&c.ObservedAt, &c.ValidUntil)
		if e != nil {
			return v, lr.ErrUnavailable
		}
		v.Items = append(v.Items, c)
	}
	if tx.Commit(ctx) != nil {
		return lr.View{}, lr.ErrUnavailable
	}
	v.Status = "SUCCESS"
	if len(v.Items) == 0 {
		v.Status = "EMPTY"
	}
	return v, nil
}
