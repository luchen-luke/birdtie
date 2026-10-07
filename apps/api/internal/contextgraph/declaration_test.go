package contextgraph

import (
	"errors"
	"testing"
)

func TestDeclarationFixtures(t *testing.T) {
	fixtures := []DeclarationInput{
		{City, "aberdeen-gb", "current"},
		{City, "edinburgh-gb", "past"},
		{Institution, "University of Aberdeen", "past"},
		{City, "london-gb", "destination"},
		{Online, "线上羽毛球社群", "interest"},
	}
	for _, fixture := range fixtures {
		got, err := NormalizeDeclaration(fixture)
		if err != nil || got != fixture {
			t.Fatalf("%+v: %+v %v", fixture, got, err)
		}
	}
	for _, bad := range []DeclarationInput{
		{City, "", "current"}, {City, "aberdeen-gb", "interest"},
		{Institution, "University", "current"}, {Online, "Chat", "affiliation"},
		{Community, "group", "interest"}, {Online, "private\nkey", "interest"},
	} {
		if _, err := NormalizeDeclaration(bad); !errors.Is(err, ErrDeclaration) {
			t.Fatalf("accepted invalid declaration: %+v", bad)
		}
	}
}
