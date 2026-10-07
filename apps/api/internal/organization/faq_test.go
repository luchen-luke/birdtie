package organization

import (
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

func TestBuildAnswerUsesOnlyVerifiedPublishedSources(t *testing.T) {
	organizationID := "org-1"
	activityID := "activity-1"
	profile := PublicProfile{ID: organizationID, VerificationStatus: "verified",
		Description:        "阿伯丁学生羽毛球社团。",
		OfficialLinks:      []string{"https://example.org/society"},
		UpcomingActivities: []foundation.Activity{{ID: activityID, Title: "周末羽毛球", Schedule: "10月3日11:00", PlaceName: "体育馆"}}}
	faqs := []FAQ{
		{ID: "faq-1", Question: "如何报名？", Answer: "请在活动详情报名。", Published: true},
		{ID: "faq-2", Question: "退款政策？", Answer: "没有退款。", Published: false},
	}

	faq := BuildAnswer("如何报名", profile, faqs)
	if faq.Status != "known" || faq.Answer != "请在活动详情报名。" ||
		len(faq.Sources) != 1 || faq.Sources[0].Type != "faq" || faq.Sources[0].ID != "faq-1" {
		t.Fatalf("published FAQ answer = %#v", faq)
	}
	activity := BuildAnswer("近期有什么活动？", profile, faqs)
	if activity.Status != "known" || len(activity.Sources) != 1 ||
		activity.Sources[0].Type != "activity" || activity.Sources[0].ID != activityID ||
		!strings.Contains(activity.Answer, "10月3日11:00") {
		t.Fatalf("activity answer = %#v", activity)
	}
	link := BuildAnswer("官网", profile, faqs)
	if link.Status != "known" || len(link.Sources) != 1 || link.Sources[0].URL != "https://example.org/society" {
		t.Fatalf("link answer = %#v", link)
	}
	for _, query := range []string{"退款政策？", "周末羽毛球退款政策", "附近有免费羽毛球吗"} {
		got := BuildAnswer(query, profile, faqs)
		if got.Status != "unknown" || len(got.Sources) != 0 {
			t.Errorf("query %q should be unknown, got %#v", query, got)
		}
	}
	profile.VerificationStatus = "unverified"
	got := BuildAnswer("如何报名？", profile, faqs)
	if got.Status != "unknown" || len(got.Sources) != 0 || strings.Contains(got.Answer, faqs[0].Answer) {
		t.Fatalf("unverified organization leaked answer: %#v", got)
	}
}
