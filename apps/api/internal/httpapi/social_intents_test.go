package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

func TestSocialIntentAudienceInputContract(t *testing.T) {
	const accountID = "22222222-2222-4222-8222-222222222222"
	const communityID = "33333333-3333-4333-8333-333333333333"
	base := socialintent.DraftInput{
		Type: "FIND_COMPANION", Title: "周末羽毛球", Constraints: json.RawMessage(`{}`),
		Modality: "ONLINE", ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	for _, tc := range []struct {
		name     string
		audience string
		city     string
		group    string
		invitees []string
		want     bool
	}{
		{"private", "PRIVATE", "", "", nil, true},
		{"friends", "FRIENDS", "", "", nil, true},
		{"public", "PUBLIC", "", "", nil, true},
		{"local target", "LOCAL", "aberdeen-gb", "", nil, true},
		{"local no city", "LOCAL", "", "", nil, false},
		{"community target", "COMMUNITY", "", communityID, nil, true},
		{"community no target", "COMMUNITY", "", "", nil, false},
		{"invitee", "INVITE_ONLY", "", "", []string{accountID}, true},
		{"invitee empty", "INVITE_ONLY", "", "", nil, false},
		{"invitee duplicate", "INVITE_ONLY", "", "", []string{accountID, accountID}, false},
		{"public with city", "PUBLIC", "aberdeen-gb", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.Audience, input.CityID, input.CommunityID, input.InviteeIDs =
				tc.audience, tc.city, tc.group, tc.invitees
			if got := validSocialIntentDraft(&input); got != tc.want {
				t.Fatalf("valid=%t want=%t", got, tc.want)
			}
		})
	}
}
