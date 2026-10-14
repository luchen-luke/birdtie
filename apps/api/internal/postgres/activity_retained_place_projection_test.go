package postgres

import (
	"strings"
	"testing"
)

func retainedActivitySQLPart(t *testing.T, sql, start, end string) string {
	t.Helper()
	i := strings.Index(sql, start)
	if i < 0 {
		t.Fatalf("missing SQL source: %s", start)
	}
	j := strings.Index(sql[i+len(start):], end)
	if j < 0 {
		t.Fatalf("missing next SQL source: %s", end)
	}
	return sql[i : i+len(start)+j]
}

// These are local SQL assembly regressions. They do not claim a PostgreSQL
// execution, lock-race proof, real POI rights review or a native HTTP run.
func TestRetainedActivityPublicPlaceGuardStaysInNullableJoin(t *testing.T) {
	join := retainedActivitySQLPart(t, publishedActivityFrom, "LEFT JOIN places p", "LEFT JOIN organizations o")
	guard := currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")
	if !strings.Contains(join, "p.id = a.place_id") || !strings.Contains(join, "p.publication_status = 'published'") || !strings.Contains(join, "AND "+guard) {
		t.Fatal("ordinary Activity Place copies bypass current retained permissions")
	}
	if strings.Contains(strings.Replace(join, guard, "", 1), "WHERE ") {
		t.Fatal("Place guard moved to WHERE and removed the independent Activity")
	}
	if strings.Count(publishedActivityFrom, guard) != 1 {
		t.Fatal("Place permission guard is duplicated or unbound")
	}
}

func TestRetainedActivityPublicJoinPreservesOriginalActivityAuthority(t *testing.T) {
	for _, required := range []string{
		"WHERE a.publication_status = 'published'",
		"birdtie_activity_visible_to(a.id,$2::uuid)",
		"FROM account_blocks b",
		"LEFT JOIN venues v ON v.place_id=a.venue_place_id AND v.place_id=p.id",
		"vc.status='approved' AND vc.reviewed_by=v.reviewed_by",
	} {
		if !strings.Contains(publishedActivityFrom, required) {
			t.Fatalf("original domain authority changed: %s", required)
		}
	}
	// All original Activity list/detail readers reuse this relation. A missing
	// authorized Place yields their existing empty name/null point projections.
	for _, column := range []string{"COALESCE(p.name, '')", "THEN p.latitude END", "THEN p.longitude END"} {
		if !strings.Contains(activityColumns, column) {
			t.Fatalf("original nullable field projection changed: %s", column)
		}
	}
}

func TestRetainedActivityPublicJoinKeepsLegacyNullRightsUnconfirmed(t *testing.T) {
	join := retainedActivitySQLPart(t, publishedActivityFrom, "LEFT JOIN places p", "LEFT JOIN organizations o")
	for _, required := range []string{
		"NOT EXISTS(SELECT 1 FROM place_sources managed_source",
		"managed_source.permission_id IS NOT NULL",
		"rights.source_url=p.source_ref",
		"rights.state='CONFIRMED' AND rights.revoked_at IS NULL",
		"rights.version=f.permission_version",
		"rights.reviewed_by<>rights.declared_by",
	} {
		if !strings.Contains(join, required) {
			t.Fatalf("retained source rule omitted: %s", required)
		}
	}
}

func TestRetainedActivityNativeLegacyRowsUseSameSourceClock(t *testing.T) {
	legacy := resultProjectionLegacySQL()
	guard := currentRetainedPlaceVisibilitySQL("p", "(SELECT n FROM result_clock)")
	join := retainedActivitySQLPart(t, legacy, "LEFT JOIN places p", "LEFT JOIN organizations o")
	if !strings.Contains(join, guard) {
		t.Fatal("native compatibility Activity copies use another unguarded Place reader")
	}
	if strings.Contains(legacy, "clock_timestamp()") || strings.Contains(legacy, "now()") || strings.Contains(legacy, "statement_timestamp()") {
		t.Fatal("native Activity copies disagree with the original payload clock")
	}
	if !strings.Contains(legacy, "a.id::text IN(SELECT id FROM selected WHERE kind='activity')") {
		t.Fatal("native compatibility rows escaped the same selected entity set")
	}
}

func TestRetainedActivityAnchorUsesInheritedFieldDeadline(t *testing.T) {
	sql := resultProjectionSQL()
	activity := retainedActivitySQLPart(t, sql, "UNION ALL SELECT 'activity'", "UNION ALL SELECT DISTINCT ON(i.owner_account_id)")
	if !strings.Contains(activity, "least(a.ends_at,a.expires_at,p.retained_until,") || strings.Contains(activity, "p.expires_at") {
		t.Fatal("Activity anchor outlives the retained Place field evidence")
	}
	for _, required := range []string{
		"a.modality<>'online'",
		"LEFT JOIN public_places p ON p.id=a.place_id",
		"venues v WHERE v.place_id=p.id AND a.organizer_business IS NOT NULL",
	} {
		if !strings.Contains(activity, required) {
			t.Fatalf("original Activity spatial/venue condition changed: %s", required)
		}
	}
}

func TestRetainedCommunityAnchorUsesInheritedFieldDeadline(t *testing.T) {
	sql := resultProjectionSQL()
	community := retainedActivitySQLPart(t, sql, "UNION ALL SELECT 'community'", "UNION ALL SELECT 'organization'")
	if !strings.Contains(community, "least(co.expires_at,p.retained_until)") || strings.Contains(community, "p.expires_at") {
		t.Fatal("Community anchor outlives the retained Place field evidence")
	}
	for _, required := range []string{
		"LEFT JOIN public_places p ON p.id=co.place_id",
		"co.visibility='public'",
		"co.lifecycle_status='active'",
		"co.owner_confirmed_at IS NOT NULL",
		"FROM account_blocks bl",
	} {
		if !strings.Contains(community, required) {
			t.Fatalf("original Community authority changed: %s", required)
		}
	}
}

func TestRetainedActivityAndCommunityDeadlinesKeepOriginalPlaceProof(t *testing.T) {
	sql := resultProjectionSQL()
	places := retainedActivitySQLPart(t, sql, "public_places AS MATERIALIZED", "public_activities AS MATERIALIZED")
	if !strings.Contains(places, currentRetainedPlaceVisibilitySQL("p", "n")) || !strings.Contains(places, currentRetainedPlaceDeadlineSQL("p", "n")+" retained_until") {
		t.Fatal("inherited deadline is not the same original retained Place proof")
	}
	if !strings.Contains(sql, "least(result_clock.n+interval '30 seconds',city.expires_at,payload.deadline,") {
		t.Fatal("retained fields replaced the original bounded Task/Session result lease")
	}
	for _, forbidden := range []string{"UPDATE agent_tasks", "INSERT INTO agent_tasks", "UPDATE place_source_permissions", "UPDATE agent_memories"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("read projection introduced state writes: %s", forbidden)
		}
	}
}
