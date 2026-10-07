package cityseed

import (
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func TestNormalizeActivityOrganizer(t *testing.T) {
	if out, err := NormalizeActivityOrganizer(nil); out != nil || err != nil {
		t.Fatal(out, err)
	}
	for _, kind := range []actorref.Type{actorref.Person, actorref.Community, actorref.Organization, actorref.Business} {
		t.Run(string(kind), func(t *testing.T) {
			in := &actorref.ActorRef{Type: kind, ID: "E626937D-4044-4600-8888-CDF931289B5B"}
			out, err := NormalizeActivityOrganizer(in)
			if err != nil || out == in || out.ID != "e626937d-4044-4600-8888-cdf931289b5b" || out.Type != kind {
				t.Fatal(out, err)
			}
			out.ID = "changed"
			if in.ID != "E626937D-4044-4600-8888-CDF931289B5B" {
				t.Fatal("mutated selector input")
			}
		})
	}
	for _, in := range []actorref.ActorRef{{}, {Type: actorref.Person, ID: "bad"}, {Type: "CITY", ID: "e626937d-4044-4600-8888-cdf931289b5b"}, {Type: actorref.Person, ID: "00000000-0000-0000-0000-000000000000"}} {
		if _, err := NormalizeActivityOrganizer(&in); !errors.Is(err, ErrInvalidOrganizer) {
			t.Fatalf("accepted invalid selector %+v: %v", in, err)
		}
	}
}
