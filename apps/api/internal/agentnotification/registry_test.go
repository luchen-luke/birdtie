package agentnotification

import (
	"encoding/json"
	"errors"
	"testing"
	"unicode"
)

func TestNotificationNativeRegistry(t *testing.T) {
	entries := []struct {
		kind     Kind
		category Category
	}{
		{KindOpportunityAvailable, CategoryActivity}, {KindActivityReminder, CategoryActivity}, {KindActivityChange, CategoryActivity}, {KindActivityCancelled, CategoryActivity}, {KindActivityReview, CategoryActivity}, {KindPlaceReview, CategorySystem},
		{KindDirectMessage, CategoryMessage}, {KindConnectionRequest, CategorySocial}, {KindConnectionDecision, CategorySocial}, {KindCommunityMessage, CategoryCommunity}, {KindActivityMessage, CategoryMessage},
		{KindOrganizationInvitation, CategoryOrganization}, {KindOrganizationMembershipChange, CategoryOrganization}, {KindAgentTaskCompleted, CategoryAgent}, {KindAgentTaskFailed, CategoryAgent},
	}
	entries = append(entries, struct {
		kind     Kind
		category Category
	}{KindBusinessClaimReview, CategoryBusiness}, struct {
		kind     Kind
		category Category
	}{KindBusinessUpdate, CategoryBusiness})
	for _, e := range entries {
		t.Run(string(e.kind), func(t *testing.T) {
			d, err := LookupKind(e.kind)
			if err != nil || d.Kind != e.kind || d.Category != e.category || d.Title == "" || d.Detail == "" {
				t.Fatal("wrong native mapping")
			}
			hasChinese := false
			for _, r := range d.Title {
				if unicode.Is(unicode.Han, r) {
					hasChinese = true
				}
			}
			if !hasChinese {
				t.Fatal("English-only title")
			}
			switch d.LegacyCategory {
			case "needs_attention", "messages", "requests", "agent_updates", "updates":
			default:
				t.Fatal("legacy UI would drop notification")
			}
			if _, err := json.Marshal(d); !errors.Is(err, ErrServerOnly) {
				t.Fatal("native registry became wire authority")
			}
			d.Title = "MUTATED"
			again, _ := LookupKind(e.kind)
			if again.Title == d.Title {
				t.Fatal("registry aliased mutable output")
			}
		})
	}
	for _, kind := range []Kind{"", "unknown", "DIRECT_MESSAGE", "business", "UserQuery"} {
		if d, err := LookupKind(kind); err != ErrInvalid || d != (Descriptor{}) {
			t.Fatal("unknown kind defaulted")
		}
	}
	if d, err := LookupKind(KindBusinessUpdate); err != nil || d.Category != CategoryBusiness || d.Title == "" {
		t.Fatal("explicit public publication mapping unavailable")
	}
	kind := KindDirectMessage
	if _, err := json.Marshal(kind); !errors.Is(err, ErrServerOnly) {
		t.Fatal("native kind serializable")
	}
	if err := json.Unmarshal([]byte(`"direct_message"`), &kind); err != ErrServerOnly || kind != "" {
		t.Fatal("client kind imported")
	}
	d := Descriptor{Kind: KindDirectMessage}
	if err := json.Unmarshal([]byte(`{"Kind":"direct_message"}`), &d); err != ErrServerOnly || d != (Descriptor{}) {
		t.Fatal("client registry imported")
	}
}
