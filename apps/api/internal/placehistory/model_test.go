package placehistory

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"strings"
	"testing"
	"time"
)

func TestPlaceHistoryClosedSummary(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	id := "11111111-1111-4111-8111-111111111111"
	base := Summary{SchemaVersion: SchemaVersion, PlaceID: id, CityID: "test", WindowDays: 30, WindowStart: now.Add(-30 * 24 * time.Hour), CheckedAt: now, RecentMomentCount: 1, RecentMoments: []Moment{{ID: id, Title: "已公开", Excerpt: "节选", Revision: 2, PublishedAt: now}}, ActivityPatterns: []ActivityPattern{}}
	if ValidateSummary(base) != nil {
		t.Fatal("valid closed public summary")
	}
	cases := map[string]func(*Summary){"window": func(s *Summary) { s.WindowDays = 31 }, "nil": func(s *Summary) { s.RecentMoments = nil }, "future": func(s *Summary) { s.RecentMoments[0].PublishedAt = now.Add(time.Second) }, "old": func(s *Summary) { s.RecentMoments[0].PublishedAt = s.WindowStart.Add(-time.Second) }, "draftrev": func(s *Summary) { s.RecentMoments[0].Revision = 1 }, "target": func(s *Summary) { s.PlaceID = "bad" }, "count": func(s *Summary) { s.RecentMomentCount = 0 }, "long": func(s *Summary) { s.RecentMoments[0].Excerpt = strings.Repeat("中", 281) }, "attendance": func(s *Summary) {
		s.ActivityPatterns = []ActivityPattern{{Category: "sports", DayKind: "ATTENDED", Arrangements: 1}}
	}}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := base
			s.RecentMoments = append([]Moment{}, base.RecentMoments...)
			change(&s)
			if ValidateSummary(s) == nil {
				t.Fatal("invalid public summary accepted")
			}
		})
	}
	raw, _ := json.Marshal(base)
	for _, forbidden := range []string{"author", "occurred", "latitude", "longitude", "participant", "profile", "memory", "context"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("private field", forbidden)
		}
	}
}
func TestMomentPublicationClosedInput(t *testing.T) {
	token := "mp1.1.2." + strings.Repeat("a", 64)
	valid := `{"revision":1,"snapshot":"` + token + `","confirmPublic":true}`
	in, e := content.DecodeMomentPublication([]byte(valid))
	if e != nil || in.Revision != 1 || !in.ConfirmPublic {
		t.Fatal(e)
	}
	for _, raw := range []string{`null`, `[]`, valid + ` {}`, `{"revision":1,"revision":2,"snapshot":"` + token + `","confirmPublic":true}`, strings.Replace(valid, `"revision":1`, `"Revision":1`, 1), strings.Replace(valid, `"revision":1`, `"revision":1.0`, 1), strings.Replace(valid, `"revision":1`, `"revision":null`, 1), strings.Replace(valid, `true`, `false`, 1), strings.Replace(valid, `"confirmPublic":true`, `"confirmPublic":true,"ownerId":"x"`, 1), strings.Replace(valid, token, "secret", 1), "\xff"} {
		if _, e = content.DecodeMomentPublication([]byte(raw)); e == nil {
			t.Errorf("invalid accepted: %q", raw)
		}
	}
}
