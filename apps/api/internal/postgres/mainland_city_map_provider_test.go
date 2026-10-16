package postgres

import (
	"encoding/json"
	"os"
	"testing"
)

// An independently marked disposable child owns all fixture rows. No Civu,
// production, private coordinate or real account data is read or modified.
func TestMainlandCityMapProviderNativeIdentityReplayAndPreservation(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx, pool := f.Context, f.Pool
	raw, err := os.ReadFile("../../migrations/174_mainland_city_map_provider.sql")
	if err != nil {
		t.Fatal(err)
	}
	const shanghai = "civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b"
	const shantou = "civu-geo-7af76973-a56d-4614-a20b-d5934f9ffe9d"
	const source = "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/789c9904087846cc3361302857aa2e76b0ae71ff/geojson/ne_10m_populated_places_simple.geojson"
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
			t.Fatalf("provider correction rows=%d want=%d err=%v", tag.RowsAffected(), want, e)
		}
	}
	state := func(excludeProvider bool) string {
		t.Helper()
		var value string
		if e := pool.QueryRow(ctx, `SELECT jsonb_agg(CASE WHEN $1 THEN to_jsonb(c)-'map_provider'
            ELSE to_jsonb(c) END ORDER BY id)::text FROM cities c`, excludeProvider).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	counts := func() [4]int64 {
		t.Helper()
		var values [4]int64
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts),
            (SELECT count(*) FROM places),(SELECT count(*) FROM moments),
            (SELECT count(*) FROM moment_location_links)`).Scan(&values[0], &values[1], &values[2], &values[3]); e != nil {
			t.Fatal(e)
		}
		return values
	}
	beforeBusiness := counts()
	exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,
        source_ref,maintainer_label,map_provider,map_center_latitude,map_center_longitude,map_default_zoom,map_viewport_source_ref)
        VALUES($1,'上海市','CN','CN','Asia/Shanghai','published','Civu 城市目录',
        'civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b','synthetic','mapbox',31.218398,121.434559,11,$2),
        ('synthetic-other-shanghai','上海市','CN','CN','Asia/Shanghai','published','Civu 城市目录',
        'civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b','synthetic','mapbox',31.218398,121.434559,11,$2)`, shanghai, source)
	// Each single-field departure from 173 must leave the entire row untouched.
	for _, tc := range []struct{ label, change, restore string }{
		{"country", "country_code='GB'", "country_code='CN'"},
		{"name", "name='a later name'", "name='上海市'"},
		{"identity-source", "source_ref='synthetic://later-source'", "source_ref='civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b'"},
		{"source-label", "source_label='a later reviewer'", "source_label='Civu 城市目录'"},
		{"publication", "publication_status='hidden'", "publication_status='published'"},
		{"provider", "map_provider='amap'", "map_provider='mapbox'"},
		{"latitude", "map_center_latitude=31.218399", "map_center_latitude=31.218398"},
		{"longitude", "map_center_longitude=121.434560", "map_center_longitude=121.434559"},
		{"zoom", "map_default_zoom=12", "map_default_zoom=11"},
		{"camera-source", "map_viewport_source_ref='synthetic://later-camera'", "map_viewport_source_ref='" + source + "'"},
	} {
		exec("UPDATE cities SET "+tc.change+" WHERE id=$1", shanghai)
		before := state(false)
		apply(0)
		if state(false) != before {
			t.Fatalf("mismatch %s was overwritten", tc.label)
		}
		exec("UPDATE cities SET "+tc.restore+" WHERE id=$1", shanghai)
	}
	exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,
        source_ref,maintainer_label,map_provider,map_center_latitude,map_center_longitude,map_default_zoom,map_viewport_source_ref)
        VALUES($1,'汕头市','广东省','CN','Asia/Shanghai','published','Civu 城市目录',
        'civu:geo_area:7af76973-a56d-4614-a20b-d5934f9ffe9d','synthetic','mapbox',23.371952,116.668080,11,$2)`, shantou, source)
	beforeCorrection := state(true)
	apply(2)
	if state(true) != beforeCorrection || counts() != beforeBusiness {
		t.Fatal("provider correction changed other city fields or business content")
	}
	for _, id := range []string{shanghai, shantou} {
		city, e := New(pool, false).GetCity(ctx, id)
		if e != nil || city.CountryCode != "CN" || city.Map == nil || city.Map.Provider != "amap" {
			t.Fatal("catalog did not expose CN/AMap provider", e)
		}
		encoded, e := json.Marshal(city)
		if e != nil {
			t.Fatal(e)
		}
		var dto struct {
			CountryCode string `json:"countryCode"`
			Map         struct {
				Provider string `json:"provider"`
			} `json:"map"`
		}
		if e := json.Unmarshal(encoded, &dto); e != nil || dto.CountryCode != "CN" || dto.Map.Provider != "amap" {
			t.Fatal("existing City wire contract omitted country/provider", e)
		}
	}
	other, e := New(pool, false).GetCity(ctx, "synthetic-other-shanghai")
	if e != nil || other.Map == nil || other.Map.Provider != "mapbox" {
		t.Fatal("same-name unrelated identity was changed", e)
	}
	beforeReplay := state(false)
	apply(0)
	if state(false) != beforeReplay {
		t.Fatal("provider correction replay changed city data")
	}
	t.Log("174: two exact cameras corrected; ten single-field mismatch guards, unrelated identity, all other city fields, business counts and replay preserved")
}
