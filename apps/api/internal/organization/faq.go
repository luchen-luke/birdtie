package organization

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
)

var ErrFAQNotFound = errors.New("organization FAQ not found")
var ErrFAQConflict = errors.New("organization FAQ conflict")

type FAQ struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Question       string    `json:"question"`
	Answer         string    `json:"answer"`
	Published      bool      `json:"published"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type FAQInput struct {
	Question  string `json:"question"`
	Answer    string `json:"answer"`
	Published bool   `json:"published"`
}

type AnswerSource struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
	URL   string `json:"url,omitempty"`
}

type AgentAnswer struct {
	OrganizationID string         `json:"organizationId"`
	Status         string         `json:"status"`
	Answer         string         `json:"answer"`
	Sources        []AnswerSource `json:"sources"`
	Mode           string         `json:"mode"`
}

type FAQStore interface {
	ListFAQs(context.Context, string, string) ([]FAQ, error)
	CreateFAQ(context.Context, string, string, FAQInput) (FAQ, error)
	UpdateFAQ(context.Context, string, string, string, FAQInput) (FAQ, error)
	DeleteFAQ(context.Context, string, string, string) error
	AnswerOrganization(context.Context, string, string, string) (AgentAnswer, error)
}

func normalizeQuestion(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if !unicode.IsSpace(r) && !unicode.IsPunct(r) && !unicode.IsSymbol(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func unknownAnswer(organizationID, explanation string) AgentAnswer {
	return AgentAnswer{OrganizationID: organizationID, Status: "unknown",
		Answer: explanation, Sources: []AnswerSource{}, Mode: "verified_rules"}
}

func matchesPrompt(question string, prompts ...string) bool {
	for _, prompt := range prompts {
		if question == prompt {
			return true
		}
	}
	return false
}

// BuildAnswer uses only published FAQ text and a verified public profile.
// Narrow matching deliberately returns unknown instead of guessing a policy.
func BuildAnswer(query string, profile PublicProfile, faqs []FAQ) AgentAnswer {
	if profile.VerificationStatus != "verified" {
		return unknownAnswer(profile.ID, "该组织尚未通过 Birdtie 身份核验，组织 Agent 暂不能确认其政策或活动安排。")
	}
	question := normalizeQuestion(query)
	if question == "" {
		return unknownAnswer(profile.ID, "请提出一个具体问题。")
	}
	for _, faq := range faqs {
		if faq.Published && normalizeQuestion(faq.Question) == question {
			return AgentAnswer{OrganizationID: profile.ID, Status: "known", Answer: faq.Answer,
				Sources: []AnswerSource{{Type: "faq", ID: faq.ID, Label: faq.Question}}, Mode: "verified_rules"}
		}
	}
	if matchesPrompt(question, "介绍一下你们", "组织介绍", "你们是谁", "about", "aboutyou") {
		if profile.Description != "" {
			return AgentAnswer{OrganizationID: profile.ID, Status: "known", Answer: profile.Description,
				Sources: []AnswerSource{{Type: "profile", ID: profile.ID, Label: "组织公开资料"}}, Mode: "verified_rules"}
		}
	}
	if matchesPrompt(question, "官网", "官方网站", "组织链接", "网站", "website") {
		for _, link := range profile.OfficialLinks {
			if strings.HasPrefix(link, "https://") {
				return AgentAnswer{OrganizationID: profile.ID, Status: "known", Answer: "组织提供的链接：" + link,
					Sources: []AnswerSource{{Type: "link", ID: profile.ID, Label: "组织提供的链接", URL: link}}, Mode: "verified_rules"}
			}
		}
	}
	for _, activity := range profile.UpcomingActivities {
		title := normalizeQuestion(activity.Title)
		if title != "" && matchesPrompt(question, title, title+"时间", title+"地点",
			title+"什么时候", title+"在哪里") {
			answer := activity.Title + "：" + activity.Schedule
			if activity.PlaceName != "" {
				answer += "，地点：" + activity.PlaceName
			}
			return AgentAnswer{OrganizationID: profile.ID, Status: "known", Answer: answer,
				Sources: []AnswerSource{{Type: "activity", ID: activity.ID, Label: activity.Title}}, Mode: "verified_rules"}
		}
	}
	if matchesPrompt(question, "近期有什么活动", "最近有什么活动", "有哪些活动", "活动安排",
		"下次活动", "upcomingevents", "events", "activities") {
		if len(profile.UpcomingActivities) > 0 {
			activity := profile.UpcomingActivities[0]
			answer := "最近的公开活动是" + activity.Title + "，时间：" + activity.Schedule
			if activity.PlaceName != "" {
				answer += "，地点：" + activity.PlaceName
			}
			return AgentAnswer{OrganizationID: profile.ID, Status: "known", Answer: answer,
				Sources: []AnswerSource{{Type: "activity", ID: activity.ID, Label: activity.Title}}, Mode: "verified_rules"}
		}
	}
	return unknownAnswer(profile.ID, "目前没有可引用的组织公开资料可以回答这个问题，请向主办方确认。")
}
