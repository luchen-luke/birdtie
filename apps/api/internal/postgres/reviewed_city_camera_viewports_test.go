package postgres

import (
	"os"
	"testing"
)

// Uses actual migrations in the explicitly owned child. No original Civu or
// production data, content, accounts or permission grants are used or changed.
func TestReviewedCityCameraViewportsNativeIdentityAndReplay(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx, pool := f.Context, f.Pool
	raw, err := os.ReadFile("../../migrations/173_reviewed_city_camera_viewports.sql")
	if err != nil {
		t.Fatal(err)
	}
	const shanghai = "civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b"
	const shantou = "civu-geo-7af76973-a56d-4614-a20b-d5934f9ffe9d"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	apply := func(want int64) {
		t.Helper()
		tag, e := pool.Exec(ctx, string(raw))
		if e != nil || tag.RowsAffected() != want {
			t.Fatalf("reviewed camera update: rows=%d want=%d err=%v", tag.RowsAffected(), want, e)
		}
	}
	counts := func() [4]int64 {
		t.Helper()
		var v [4]int64
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts),
          (SELECT count(*) FROM places),(SELECT count(*) FROM moments),
          (SELECT count(*) FROM moment_location_links)`).Scan(&v[0], &v[1], &v[2], &v[3]); e != nil {
			t.Fatal(e)
		}
		return v
	}
	beforeCounts := counts()
	exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)
        VALUES($1,'上海市','CN','GB','Asia/Shanghai','published','Civu 城市目录',
               'civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b','synthetic'),
              ($2,'汕头市','广东省','CN','Asia/Shanghai','published','Civu 城市目录',
               'civu:geo_area:7af76973-a56d-4614-a20b-d5934f9ffe9d','synthetic'),
              ('synthetic-other-shanghai','上海市','CN','CN','Asia/Shanghai','published','Civu 城市目录',
               'civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b','synthetic')`, shanghai, shantou)
	// Preserve a valid existing configuration even when its city is reviewed.
	exec(`UPDATE cities SET map_provider='mapbox',map_center_latitude=23.4,
        map_center_longitude=116.7,map_default_zoom=9,
        map_viewport_source_ref='https://example.invalid/reviewed-before' WHERE id=$1`, shantou)
	apply(0) // country mismatch; existing map; same name but unrelated stable ID.
	exec(`UPDATE cities SET country_code='CN',source_ref='synthetic://wrong-source' WHERE id=$1`, shanghai)
	apply(0)
	exec(`UPDATE cities SET source_ref='civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b',name='wrong city' WHERE id=$1`, shanghai)
	apply(0)
	exec(`UPDATE cities SET name='上海市',source_label='synthetic wrong label' WHERE id=$1`, shanghai)
	apply(0)
	exec(`UPDATE cities SET source_label='Civu 城市目录',publication_status='hidden' WHERE id=$1`, shanghai)
	apply(0)
	exec(`UPDATE cities SET publication_status='published' WHERE id=$1`, shanghai)
	apply(1)
	var retained bool
	if e := pool.QueryRow(ctx, `SELECT map_center_latitude=23.4 AND map_center_longitude=116.7
        AND map_default_zoom=9 AND map_viewport_source_ref='https://example.invalid/reviewed-before'
        FROM cities WHERE id=$1`, shantou).Scan(&retained); e != nil || !retained {
		t.Fatal("existing city camera overwritten", e)
	}
	exec(`UPDATE cities SET map_provider=NULL,map_center_latitude=NULL,map_center_longitude=NULL,
        map_default_zoom=NULL,map_viewport_source_ref=NULL WHERE id=$1`, shantou)
	apply(1)
	for _, id := range []string{shanghai, shantou} {
		city, e := New(pool, false).GetCity(ctx, id)
		if e != nil || city.Map == nil || city.Map.Provider != "mapbox" || city.Map.DefaultZoom != 11 {
			t.Fatal("current catalog did not expose reviewed camera", e)
		}
		wantLat, wantLon := 31.218398, 121.434559
		if id == shantou {
			wantLat, wantLon = 23.371952, 116.668080
		}
		if city.Map.Latitude != wantLat || city.Map.Longitude != wantLon || city.Map.SourceRef != "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/789c9904087846cc3361302857aa2e76b0ae71ff/geojson/ne_10m_populated_places_simple.geojson" {
			t.Fatal("camera coordinates/source not the reviewed public point")
		}
	}
	cityState := func() string {
		t.Helper()
		var state string
		if e := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM cities c`).Scan(&state); e != nil {
			t.Fatal(e)
		}
		return state
	}
	beforeReplay := cityState()
	apply(0)
	if cityState() != beforeReplay || counts() != beforeCounts {
		t.Fatal("replay changed city data or camera repair changed business content")
	}
	var otherMap bool
	if e := pool.QueryRow(ctx, `SELECT map_provider IS NULL FROM cities WHERE id='synthetic-other-shanghai'`).Scan(&otherMap); e != nil || !otherMap {
		t.Fatal("same-name unrelated city was changed", e)
	}
}
