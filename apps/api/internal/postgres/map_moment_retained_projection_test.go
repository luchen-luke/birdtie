package postgres

import (
	"strings"
	"testing"
)

func TestMapMomentRetainedPointAndInheritedDeadline(t *testing.T) {
	point := retainedActivitySQLPart(t, mapProjectionRowsSQL, "point AS MATERIALIZED", "raw_sources AS MATERIALIZED")
	for _, need := range []string{currentRetainedPlaceVisibilitySQL("p", "stamp.at"), currentRetainedPlaceDeadlineSQL("p", "stamp.at") + " retained_until", retainedPlaceReadBindingSQL("p"), "p.longitude BETWEEN $2 AND $4 AND p.latitude BETWEEN $3 AND $5"} {
		if !strings.Contains(point, need) {
			t.Fatal("map point permission/deadline/proof missing", need)
		}
	}
	moment := retainedActivitySQLPart(t, mapProjectionRowsSQL, "UNION ALL SELECT 'MOMENT'", "UNION ALL SELECT 'ORGANIZATION'")
	if !strings.Contains(moment, "m.revision::text,p.retained_until") || strings.Contains(moment, "p.expires_at") {
		t.Fatal("Moment anchor outlives retained Place evidence")
	}
	for _, need := range []string{"m.visibility='public'", "m.author_confirmed_at<=m.published_at", "author.status='active'", "account_blocks", "'entityRef'", "'detailRef'", "rank<=30"} {
		if !strings.Contains(mapProjectionRowsSQL, need) {
			t.Fatal("existing typed entity/public ACL changed", need)
		}
	}
}
