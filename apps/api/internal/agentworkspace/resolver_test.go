package agentworkspace

import "testing"

func TestResolveIntentFindsBadmintonThisWeekend(t *testing.T) {
	got := ResolveIntent("Find badminton this weekend", nil)
	if !got.Supported || got.Intent != FindActivity || got.Category != "badminton" || got.TimePreference != "weekend" {
		t.Fatalf("unexpected intent: %#v", got)
	}
}

func TestResolveIntentFindsGenericActivitiesFromChineseRequest(t *testing.T) {
	got := ResolveIntent("帮我查找最近有什么活动", nil)
	if !got.Supported || got.Intent != FindActivity || got.Category != "" || got.TimePreference != "anytime" {
		t.Fatalf("unexpected generic Chinese activity intent: %#v", got)
	}
}

func TestResolveIntentSupportsExplicitChineseAreaSearch(t *testing.T) {
	got := ResolveIntent("搜索此区域", nil)
	if !got.Supported || got.Intent != FindActivity {
		t.Fatalf("unexpected Chinese area-search intent: %#v", got)
	}
}

func TestResolveIntentFindsChineseBadmintonThisWeekend(t *testing.T) {
	got := ResolveIntent("帮我找周末的羽毛球活动", nil)
	if !got.Supported || got.Intent != FindActivity || got.Category != "badminton" || got.TimePreference != "weekend" {
		t.Fatalf("unexpected Chinese badminton intent: %#v", got)
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

func TestResolveIntentCarriesChineseCloserFollowUp(t *testing.T) {
	current := &Task{Intent: FindActivity, Filters: map[string]string{
		"category": "badminton", "timePreference": "weekend",
	}}
	got := ResolveIntent("近一点的呢？", current)
	if !got.Supported || got.Intent != FindActivity || got.Category != "badminton" || got.TimePreference != "weekend" || got.DistancePreference != "closer" {
		t.Fatalf("Chinese follow-up did not preserve task filters: %#v", got)
	}
}

func TestResolveIntentCarriesContextForCityCenterFollowUp(t *testing.T) {
	current := &Task{Intent: FindActivity, Filters: map[string]string{
		"category": "", "timePreference": "anytime",
	}}
	got := ResolveIntent("按市中心距离排序", current)
	if !got.Supported || got.Intent != FindActivity || got.TimePreference != "anytime" || got.DistancePreference != "closer" {
		t.Fatalf("follow-up did not preserve activity context: %#v", got)
	}
}

func TestResolveIntentDoesNotInventUnsupportedCapabilities(t *testing.T) {
	got := ResolveIntent("Find a restaurant", nil)
	if got.Supported || got.Intent != UnsupportedIntent {
		t.Fatalf("expected unsupported intent, got %#v", got)
	}
}
