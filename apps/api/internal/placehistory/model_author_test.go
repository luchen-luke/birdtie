package placehistory

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"testing"
	"time"
)

func TestPlaceHistoryAuthorKeepsOriginalBoundsAndLegacyWire(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	id := "11111111-1111-4111-8111-111111111111"
	m := Moment{ID: id, Title: "公开动态", Excerpt: "分享", Revision: 2, PublishedAt: now}
	legacy := Summary{SchemaVersion: SchemaVersion, PlaceID: id, CityID: "test", WindowDays: 30, WindowStart: now.Add(-30 * 24 * time.Hour), CheckedAt: now, RecentMomentCount: 1, RecentMoments: []Moment{m}, ActivityPatterns: []ActivityPattern{}}
	v2 := SummaryWithAuthor{Summary: legacy, RecentMoments: []MomentWithAuthor{{Moment: m}}, SourceBinding: "native-proof"}
	v2.SchemaVersion = AuthorSchemaVersion
	if ValidateSummaryWithAuthor(v2) != nil {
		t.Fatal("valid nullable author summary")
	}
	raw, _ := json.Marshal(v2)
	var wire map[string]json.RawMessage
	if json.Unmarshal(raw, &wire) != nil || len(wire) != 10 {
		t.Fatal("outer summary contract changed", string(raw))
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(wire["recentMoments"], &rows) != nil || len(rows) != 1 || len(rows[0]) != 7 || string(rows[0]["authorId"]) != "null" || string(rows[0]["authorPublicProfile"]) != "null" {
		t.Fatal("closed per-row nullable contract", string(raw))
	}
	for _, mode := range []string{"window", "too_many", "bad_author", "wrong_row", "no_proof"} {
		t.Run(mode, func(t *testing.T) {
			s := v2
			s.RecentMoments = append([]MomentWithAuthor{}, v2.RecentMoments...)
			switch mode {
			case "window":
				s.WindowDays = 31
			case "too_many":
				s.RecentMoments = make([]MomentWithAuthor, 6)
			case "bad_author":
				s.RecentMoments[0].AuthorPublicProfile = &content.PublicMomentAuthor{AccountID: id, DisplayName: "公开作者"}
			case "wrong_row":
				s.RecentMoments[0].ID = "22222222-2222-4222-8222-222222222222"
			case "no_proof":
				s.SourceBinding = ""
			}
			if ValidateSummaryWithAuthor(s) == nil {
				t.Fatal("invalid summary accepted")
			}
		})
	}
	if ValidateSummary(legacy) != nil {
		t.Fatal("original schema no longer valid")
	}
}
