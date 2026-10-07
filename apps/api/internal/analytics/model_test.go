package analytics

import "testing"

func TestViewEventsAreConstrained(t *testing.T) {
	for _, kind := range []string{"impression", "detail"} {
		if !ValidViewEvent(kind, "agent") {
			t.Fatalf("expected %s", kind)
		}
	}
	for _, kind := range []string{"rsvp", "save", "arbitrary"} {
		if ValidViewEvent(kind, "agent") {
			t.Fatalf("public event accepted %s", kind)
		}
	}
	if ValidViewEvent("detail", "raw-location") {
		t.Fatal("unknown source accepted")
	}
	if !ValidSource("organization") || ValidSource("untrusted") {
		t.Fatal("entry source validation failed")
	}
}
