package content

import "testing"

func TestMomentHistoryQueryKeepsPublicAudienceAndClosedScopes(t *testing.T) {
	if ValidateMomentHistoryQuery(MomentHistoryQuery{Scope: "city", CityID: "aberdeen-gb", From: "2026-01-01T00:00:00.123000Z"}, false) != nil {
		t.Fatal("valid client UTC fractional timestamp rejected")
	}
	for _, q := range []MomentHistoryQuery{{Status: "active"}, {Status: "draft"}, {Status: "published"}} {
		if ValidateMomentHistoryQuery(q, true) != nil {
			t.Fatal("owner status rejected")
		}
	}
	for _, q := range []MomentHistoryQuery{{Scope: "city", CityID: "aberdeen-gb"}, {Scope: "following", CityID: "aberdeen-gb"}, {Scope: "friends", CityID: "aberdeen-gb"}, {Scope: "author", AuthorID: "11111111-1111-4111-8111-111111111111"}} {
		if ValidateMomentHistoryQuery(q, false) != nil {
			t.Fatal("current public query rejected")
		}
	}
	for _, q := range []MomentHistoryQuery{{Scope: "followers", CityID: "aberdeen-gb"}, {Scope: "friends"}, {Scope: "city", CityID: "aberdeen-gb", Status: "private"}, {Scope: "author", AuthorID: "not-an-id"}, {Scope: "city", CityID: "aberdeen-gb", From: "2026-01-02T00:00:00Z", To: "2026-01-01T00:00:00Z"}, {Scope: "city", CityID: "aberdeen-gb", From: "2026-01-01"}} {
		if ValidateMomentHistoryQuery(q, false) == nil {
			t.Fatal("invalid or audience-widening query accepted")
		}
	}
}
