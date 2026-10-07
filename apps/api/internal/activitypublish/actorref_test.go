package activitypublish

import "testing"

func TestOrganizerActorRefContract(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, kind := range []string{"PERSON", "ORGANIZATION", "BUSINESS", "COMMUNITY"} {
		ref, err := (Organizer{Type: kind, ID: id}).ActorRef()
		if err != nil || string(ref.Type) != kind || ref.ID != id {
			t.Fatalf("%s: ref=%+v err=%v", kind, ref, err)
		}
	}
	if _, err := (Organizer{Type: "PLACE", ID: id}).ActorRef(); err == nil {
		t.Fatal("place must not become an actor")
	}
}
