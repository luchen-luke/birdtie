package actorref

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAllActorTypesAndStableIDs(t *testing.T) {
	id := "A1111111-1111-4111-8111-111111111111"
	for _, kind := range []Type{Person, Organization, Business, Community} {
		ref, err := Parse(" "+string(kind)+" ", " "+id+" ")
		if err != nil || ref.Type != kind || ref.ID != "a1111111-1111-4111-8111-111111111111" || !ref.Valid() {
			t.Fatalf("%s: ref=%+v err=%v", kind, ref, err)
		}
		var decoded ActorRef
		encoded, err := json.Marshal(ref)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &decoded); err != nil || !ref.Equal(decoded) {
			t.Fatalf("%s: JSON round trip failed: %s, %v", kind, encoded, err)
		}
	}
}

func TestActorTypeSeparatesSameIDAndRejectsInvalidInput(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	person, _ := Parse("person", id)
	organization, _ := Parse("organization", id)
	if person.Equal(organization) || !person.Equal(ActorRef{Type: Person, ID: id}) {
		t.Fatal("typed equality lost the actor namespace")
	}
	for _, tc := range []struct {
		kind, id string
		want     error
	}{
		{"CITY", id, ErrType},
		{"PLACE", id, ErrType},
		{"", id, ErrType},
		{"PERSON", "", ErrID},
		{"PERSON", "not-a-uuid", ErrID},
		{"PERSON", "11111111-1111-4111-8111-111111111111/extra", ErrID},
	} {
		if _, err := Parse(tc.kind, tc.id); !errors.Is(err, tc.want) {
			t.Errorf("Parse(%q, %q): got %v, want %v", tc.kind, tc.id, err, tc.want)
		}
	}
}

func TestOrganizationActorAndAccountPrincipalStayDistinct(t *testing.T) {
	organizationID := "11111111-1111-4111-8111-111111111111"
	accountID := "22222222-2222-4222-8222-222222222222"
	actor, err := Parse("organization", organizationID)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := ParsePrincipal("organization", accountID)
	if err != nil || actor.ID == principal.ID {
		t.Fatalf("organization actor and Agent account principal conflated: %+v %+v %v", actor, principal, err)
	}
	if !principal.Equal(PrincipalRef{Type: Organization, ID: accountID}) ||
		principal.Equal(PrincipalRef{Type: Person, ID: accountID}) {
		t.Fatal("account principal type boundary failed")
	}
}
