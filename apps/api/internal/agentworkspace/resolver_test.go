package agentworkspace

import "testing"

func TestResolveIntentFindsBadmintonThisWeekend(t *testing.T) {
	got := ResolveIntent("Find badminton this weekend", nil)
	if !got.Supported || got.Intent != FindActivity || got.Category != "badminton" || got.TimePreference != "weekend" {
		t.Fatalf("unexpected intent: %#v", got)
	}
}

func TestResolveIntentCarriesContextForCloserFollowUp(t *testing.T) {
	current := &Task{Intent: FindActivity, Filters: map[string]string{
		"category": "badminton", "timePreference": "weekend",
	}}
	got := ResolveIntent("Anything closer?", current)
	if !got.Supported || got.Intent != FindActivity || got.Category != "badminton" || got.TimePreference != "weekend" || got.DistancePreference != "closer" {
		t.Fatalf("follow-up did not preserve task filters: %#v", got)
	}
}

func TestResolveIntentDoesNotInventUnsupportedCapabilities(t *testing.T) {
	got := ResolveIntent("Find a restaurant", nil)
	if got.Supported || got.Intent != UnsupportedIntent {
		t.Fatalf("expected unsupported intent, got %#v", got)
	}
}
