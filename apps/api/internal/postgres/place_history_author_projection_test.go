package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"strings"
	"testing"
	"time"
)

func TestPlaceHistoryAuthorProjectionPreservesWindowAndFilteredCounts(t *testing.T) {
	sql := placeHistoryWithAuthorPayloadSQL()
	for _, required := range []string{"interval '30 days'", "SELECT count(*) FROM recent", "LIMIT 5", "ORDER BY published_at DESC,id DESC", "'authorId'", "'authorPublicProfile'", "author_binding", "account_blocks", "m.location_precision='place'", currentRetainedPlaceVisibilitySQL("p", "clock.at")} {
		if !strings.Contains(sql, required) {
			t.Fatalf("missing bounded/current authority %s", required)
		}
	}
	if strings.Contains(placeHistoryPayloadSQL, "'authorId'") {
		t.Fatal("legacy author-free projection changed")
	}
	if !strings.Contains(placeHistoryFinalCurrentSQL, currentRetainedPlaceVisibilitySQL("p", "cl.at")) {
		t.Fatal("final deadline/current fence omitted")
	}
	for _, query := range []string{sql, placeHistoryFinalCurrentSQL} {
		for _, forbidden := range []string{"INSERT INTO", "UPDATE moments", "UPDATE places", "UPDATE agent_tasks"} {
			if strings.Contains(query, forbidden) {
				t.Fatal("projection wrote original state")
			}
		}
	}
}

func TestPlaceHistoryAuthorBindingIgnoresOnlyReadClock(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	id := "11111111-1111-4111-8111-111111111111"
	m := ph.Moment{ID: id, Title: "公开动态", Excerpt: "原节选", Revision: 2, PublishedAt: now.Add(-time.Hour)}
	s := ph.SummaryWithAuthor{Summary: ph.Summary{SchemaVersion: ph.AuthorSchemaVersion, PlaceID: id, CityID: "test", WindowDays: 30, WindowStart: now.Add(-30 * 24 * time.Hour), CheckedAt: now, RecentMomentCount: 1, RecentMoments: []ph.Moment{m}, ActivityPatterns: []ph.ActivityPattern{}}, RecentMoments: []ph.MomentWithAuthor{{Moment: m}}}
	base, e := placeHistoryAuthorBinding(s, "original-native-epoch")
	if e != nil {
		t.Fatal(e)
	}
	clock := s
	clock.CheckedAt = now.Add(time.Second)
	clock.WindowStart = clock.CheckedAt.Add(-30 * 24 * time.Hour)
	if fresh, e := placeHistoryAuthorBinding(clock, "original-native-epoch"); e != nil || fresh != base {
		t.Fatal("read clock changed native generation")
	}
	for _, mode := range []string{"epoch", "count", "text", "author", "revision", "empty_proof"} {
		t.Run(mode, func(t *testing.T) {
			changed := s
			changed.RecentMoments = append([]ph.MomentWithAuthor{}, s.RecentMoments...)
			proof := "original-native-epoch"
			switch mode {
			case "epoch":
				proof = "next-native-epoch"
			case "count":
				changed.RecentMomentCount++
			case "text":
				changed.RecentMoments[0].Excerpt = "改后节选"
			case "author":
				changed.RecentMoments[0].AuthorID = &id
				changed.RecentMoments[0].AuthorPublicProfile = &content.PublicMomentAuthor{AccountID: id, DisplayName: "公开作者"}
			case "revision":
				changed.RecentMoments[0].Revision++
			case "empty_proof":
				proof = ""
			}
			fresh, e := placeHistoryAuthorBinding(changed, proof)
			if mode == "empty_proof" {
				if e == nil {
					t.Fatal("missing native proof accepted")
				}
				return
			}
			if e != nil || fresh == base {
				t.Fatal("source change did not invalidate proof")
			}
		})
	}
}
