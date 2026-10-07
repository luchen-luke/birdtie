package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"testing"
)

func TestSocialOrganizerValidation(t *testing.T) {
	actor := "11111111-1111-4111-8111-111111111111"
	other := "22222222-2222-4222-8222-222222222222"
	cases := []struct {
		name string
		in   activitypublish.Input
		want string
	}{
		{"person members only", activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: actor}, Visibility: "organizer_members"}, "person_members_only_invalid"},
		{"different person", activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: other}, Visibility: "public"}, "person_organizer_mismatch"},
		{"member community option", activitypublish.Input{Organizer: activitypublish.Organizer{Type: "COMMUNITY", ID: other}, Visibility: "organizer_members"}, ""},
		{"invite person", activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: actor}, Visibility: "invite_only"}, ""},
		{"unknown actor", activitypublish.Input{Organizer: activitypublish.Organizer{Type: "CITY", ID: other}, Visibility: "public"}, "organizer_type_invalid"},
		{"invalid actor id", activitypublish.Input{Organizer: activitypublish.Organizer{Type: "COMMUNITY", ID: "not-an-id"}, Visibility: "public"}, "organizer_id_invalid"},
		{"normalized legacy type", activitypublish.Input{Organizer: activitypublish.Organizer{Type: " organization ", ID: other}, Visibility: "public"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeActivityOrganizer(&tc.in, actor, true); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if tc.want == "" && tc.in.Organizer.Type == " organization " {
				t.Fatal("organizer type was not normalized")
			}
		})
	}
}
