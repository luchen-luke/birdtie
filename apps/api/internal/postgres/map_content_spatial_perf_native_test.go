package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Real PostgreSQL query/aggregation proof on an independently marked child.
// Synthetic drafts are never published; an unrelated viewer must see none.
func TestMapContent171NativeSpatialPressure(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	owner, _ := legacyMigrationUUID()
	peer, _ := legacyMigrationUUID()
	for _, id := range []string{owner, peer} {
		if _, e := f.Pool.Exec(ctx, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, id); e != nil {
			t.Fatal(e)
		}
	}
	digest := sha256.Sum256([]byte("synthetic-map171-pressure"))
	if _, e := f.Pool.Exec(ctx, `WITH cl AS(SELECT clock_timestamp() at) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)SELECT $1,$2,'synthetic_test',at+interval '1 hour',at+interval '1 hour' FROM cl`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	seed := func(prefix, account string, first, last int, lat, lon float64) {
		t.Helper()
		if _, e := f.Pool.Exec(ctx, `INSERT INTO places(id,name,category_code,latitude,longitude,coordinate_system,location_precision,publication_status,source_label,source_ref,maintainer_label)
 SELECT md5($1||':place:'||i)::uuid,'Synthetic pressure point','park',$4+(i%100)*0.001,$5+(i/100)*0.001,'wgs84','point','draft','LOCAL_SYNTHETIC_FIXTURE','disposable://map171-pressure','synthetic' FROM generate_series($2::int,$3::int)i`, prefix, first, last, lat, lon); e != nil {
			t.Fatal("seed places", e)
		}
		if _, e := f.Pool.Exec(ctx, `INSERT INTO moments(id,author_account_id,title,body,status,visibility,location_precision)
 SELECT md5($1||':moment:'||i)::uuid,$4,'','Synthetic pressure content','draft','private','none' FROM generate_series($2::int,$3::int)i`, prefix, first, last, account); e != nil {
			t.Fatal("seed moments", e)
		}
		if _, e := f.Pool.Exec(ctx, `INSERT INTO moment_location_links(id,moment_id,kind,place_id,precision,position)
 SELECT md5($1||':link:'||i)::uuid,md5($1||':moment:'||i)::uuid,'PLACE',md5($1||':place:'||i)::uuid,'place',0 FROM generate_series($2::int,$3::int)i`, prefix, first, last); e != nil {
			t.Fatal("seed links", e)
		}
	}
	seed("outside", owner, 1, 10000, 45, 80)
	seed("denied", peer, 1, 100, 10, 20)
	s := New(f.Pool, true)
	a := identity.Actor{ID: owner, AccountType: "person"}
	q := content.MapContentQuery{Bounds: content.MapContentBounds{West: 19, South: 9, East: 21, North: 11}, Zoom: 0}
	last := 0
	for _, n := range []int{100, 1000, 10000} {
		seed("inside", owner, last+1, n, 10, 20)
		last = n
		for _, table := range []string{"places", "moments", "moment_location_links", "moment_location_epochs"} {
			if _, e := f.Pool.Exec(ctx, "ANALYZE "+table); e != nil {
				t.Fatal(e)
			}
		}
		readCtx, frame, e := s.beginMapContent(ctx, digest, a)
		if e != nil {
			t.Fatal(e)
		}
		var raw []byte
		if e = frame.tx.QueryRow(readCtx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+mapContentSpatialSQL(), owner, q.Bounds.West, q.Bounds.South, q.Bounds.East, q.Bounds.North).Scan(&raw); e != nil {
			_ = frame.tx.Rollback(ctx)
			t.Fatal("explain spatial", e)
		}
		var plans []map[string]any
		if json.Unmarshal(raw, &plans) != nil || len(plans) != 1 {
			t.Fatal("explain json")
		}
		indexes := map[string]bool{}
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				if n == 10000 {
					if duration, ok := x["Actual Total Time"].(float64); ok {
						loops, _ := x["Actual Loops"].(float64)
						if duration*loops > 50 {
							t.Logf("SQL_NODE kind=%v relation=%v cte=%v rows=%v loops=%v timeMs=%.3f removed=%v", x["Node Type"], x["Relation Name"], x["CTE Name"], x["Actual Rows"], loops, duration, x["Rows Removed by Join Filter"])
						}
					}
				}
				if name, ok := x["Index Name"].(string); ok {
					indexes[name] = true
				}
				for _, vv := range x {
					walk(vv)
				}
			case []any:
				for _, vv := range x {
					walk(vv)
				}
			}
		}
		walk(plans[0])
		indexNames := []string{}
		for k := range indexes {
			indexNames = append(indexNames, k)
		}
		sort.Strings(indexNames)
		started := time.Now()
		leaves, source, _, _, e := readMapSpatial(readCtx, frame, q)
		if e != nil || len(leaves) != n {
			t.Fatal("viewport ACL complete count", e, len(leaves), n)
		}
		spatialMS := float64(time.Since(started).Microseconds()) / 1000
		reader := historyReader(frame)
		_ = frame.tx.Rollback(ctx)
		started = time.Now()
		page, e := s.ReadHumanMapContent(ctx, digest, a, q)
		if e != nil || !page.Valid(time.Now()) || page.TotalAnchorCount != n || page.TotalContentCount != n || len(page.Pins) > 40 || len(page.Pins) != 40 || page.TotalPointCount != n || page.Pins[0].Kind != "PLACE_CONTENT" {
			t.Fatal("bounded complete wire page", e, page.TotalAnchorCount, page.TotalContentCount, len(page.Pins))
		}
		apiMS := float64(time.Since(started).Microseconds()) / 1000
		members := q
		members.SourceVersion = source
		first := page
		if first.NextCursor == nil {
			t.Fatal("first viewport page missing continuation")
		}
		members.Cursor = *first.NextCursor
		second, e := s.ReadHumanMapContent(ctx, digest, a, members)
		if e != nil || len(second.Pins) != 40 || second.Pins[0].MarkerID == first.Pins[0].MarkerID {
			t.Fatal("member continuation", e)
		}
		// Exercise the actual signed terminal cursor against the complete SQL
		// set without issuing 250 redundant full proof reads for 10k markers.
		before := strings.TrimPrefix(leaves[n-41].pin.MarkerID, "place:")
		cursor, e := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: owner, Reader: reader, Query: "viewport:" + content.MomentControlHash([]any{q.Bounds, q.Zoom, q.CityID, "DEFAULT_CONTENT"}), Source: source, Before: before, Until: page.ValidUntil})
		if e != nil {
			t.Fatal(e)
		}
		members.Cursor = cursor
		end, e := s.ReadHumanMapContent(ctx, digest, a, members)
		if e != nil || len(end.Pins) != 40 || end.NextCursor != nil || end.Pins[39].MarkerID != leaves[n-1].pin.MarkerID {
			t.Fatal("terminal members accessible", e, len(end.Pins))
		}
		t.Logf("SQL_PRESSURE candidates=%d outside=10000 denied=100 resultRows=%.0f explainMs=%.3f readSpatialMs=%.3f apiMs=%.3f markers=%d membersFirst=%d membersSecond=%d membersLast=%d indexes=%s", n, plans[0]["Plan"].(map[string]any)["Actual Rows"], plans[0]["Execution Time"], spatialMS, apiMS, len(page.Pins), len(first.Pins), len(second.Pins), len(end.Pins), fmt.Sprint(indexNames))
	}
}
