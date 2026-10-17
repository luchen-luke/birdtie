package agentworkspace

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

const (
	PlaceOpeningHours      = "opening_hours"
	PlaceAdmission         = "admission"
	PlaceAccessibility     = "accessibility"
	PlaceTransport         = "transport"
	PlacePublicInformation = "public_information"
)

func ValidPlaceQuestionTopic(topic string) bool {
	return oneOf(topic, "", PlaceAddress, PlaceOpeningHours, PlaceAdmission, PlaceAccessibility, PlaceTransport, PlacePublicInformation)
}

// Search terms contain only a current public place name and fixed topic words.
// The actual question is already bound separately in the original query scope.
func PlaceQuestionSearchTerm(name, topic string) (string, bool) {
	if !ValidPlaceFollowupName(name) || !ValidPlaceQuestionTopic(topic) {
		return "", false
	}
	suffix := map[string]string{
		PlaceOpeningHours:  " opening hours",
		PlaceAdmission:     " tickets admission booking",
		PlaceAccessibility: " accessibility wheelchair access",
		PlaceTransport:     " address directions public transport parking",
	}[topic]
	term := name + suffix
	return term, len(term) <= 240
}

// These server-written Task filters are correlation data, never a read or
// egress grant. The response whitelist excludes them. Native storage re-reads
// the selected public row and its retained generation at every egress fence.
const (
	PlaceFollowupIDFilter         = "publicPlaceFollowupId"
	PlaceFollowupGenerationFilter = "publicPlaceFollowupGeneration"
	PlaceFollowupTopicFilter      = "publicPlaceQuestionTopic"
)

var (
	ErrPlaceFollowupAmbiguous = errors.New("public place followup needs one place")
	ErrPlaceFollowupChanged   = errors.New("public place followup source changed")
	ErrPlaceFollowupNotFound  = errors.New("public place followup not found")
)

// PlaceFollowup is a closed interpretation of the current literal question.
// It carries no entity, transcript, private profile, coordinate or permission.
type PlaceFollowup struct {
	Name, Topic, TimePreference string
	Pronoun                     bool
}

type PublicPlaceFollowupPort interface {
	ContinueOwnPublicPlace(context.Context, arp.Access, string) (Task, error)
}

func ClearPlaceFollowupContext(task *Task) {
	if task == nil {
		return
	}
	delete(task.Filters, PlaceFollowupIDFilter)
	delete(task.Filters, PlaceFollowupGenerationFilter)
	delete(task.Filters, PlaceFollowupTopicFilter)
	delete(task.Filters, SelectedPlaceContextFilter)
	delete(task.Filters, SelectedPlaceDayFilter)
}

func ValidPlaceFollowupName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name || len(name) > 160 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// The current question resolves only against an existing public Place task.
// No free transcript or private asset becomes model input. Explicit operations
// and private questions still use their original parser/permission paths.
func ParsePlaceFollowup(query string, current *Task) (PlaceFollowup, bool) {
	var none PlaceFollowup
	if current == nil || baseIntent(current) != FindPlace {
		return none, false
	}
	// The original HTTP handler checks this resolver before ParseMVPIntent.
	// A complete category+objective-fields request starts a current category
	// refinement; it is never a pronoun or alias for the old selected Place.
	if _, objective := parseObjectivePlaceRefinement(query); objective {
		return none, false
	}
	text := strings.TrimSpace(query)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"找", "寻找", "find ", "search ", "发布", "创建", "发起"} {
		if strings.HasPrefix(lower, prefix) {
			return none, false
		}
	}
	if text == "" || len(text) > 240 || !utf8.ValidString(text) || strings.HasPrefix(lower, "找地点") || containsAny(lower, "私聊", "私人", "私有", "画像", "记忆", "用户住址", "发消息", "发布", "创建", "发起", "帮我买", "帮我预订", "帮我预约", "替我", "private", "profile", "memory", "publish", "create", "send message", "book it", "reserve it") {
		return none, false
	}
	topic := current.Filters[PlaceFollowupTopicFilter]
	if !ValidPlaceQuestionTopic(topic) {
		return none, false
	}
	askingAddress := IsLocalPlaceAddressFollowupQuery(text)
	askingHours := containsAny(lower, "几点开门", "几点关门", "开放时间", "营业时间", "开门时间", "闭馆时间", "opening hours", "when does it open", "when is it open", "what time does it open", "what time does it close")
	askingAdmission := containsAny(lower, "门票", "票价", "买票", "收费", "免费", "需要预约", "需要预订", "ticket", "admission", "entry fee", "free entry", "need to book")
	askingAccess := containsAny(lower, "无障碍", "輪椅", "轮椅", "wheelchair", "accessible", "accessibility")
	askingTransport := containsAny(lower, "地址", "怎么去", "如何去", "公交", "停车", "在哪里", "directions", "get there", "public transport", "parking", "where is it")
	knownQuestion := askingAddress || askingHours || askingAdmission || askingAccess || askingTransport
	switch {
	case askingAddress:
		topic = PlaceAddress
	case askingHours:
		topic = PlaceOpeningHours
	case askingAdmission:
		topic = PlaceAdmission
	case askingAccess:
		topic = PlaceAccessibility
	case askingTransport:
		topic = PlaceTransport
	}
	when := activityTime(lower)
	if when == "anytime" && !containsAny(lower, "不限时间", "任何时间", "anytime") {
		when = current.Filters["timePreference"]
	}
	if !oneOf(when, "", "anytime", "today", "tonight", "tomorrow", "weekend") {
		return none, false
	}
	// A literal new place name is not a pronoun for the prior result. Resolve it
	// through the original named-place path and its canonical city/version proof.
	if literal, ok := ParseLocalPlaceQuestion(text); ok && !literal.Recommendation {
		return PlaceFollowup{Name: literal.Name, Topic: literal.Field, TimePreference: when}, true
	}
	trimmed := strings.Trim(text, " ?？!！。.")
	name, named := "", false
	if strings.HasPrefix(trimmed, "那") && strings.HasSuffix(trimmed, "呢") {
		name, named = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "那"), "呢")), true
	} else if strings.HasPrefix(strings.ToLower(trimmed), "what about ") {
		name, named = strings.TrimSpace(trimmed[len("what about "):]), true
	}
	if named {
		if !ValidPlaceFollowupName(name) || containsAny(strings.ToLower(name), "找地点", "发布", "创建", "发起", "publish", "create", "host ") {
			return none, false
		}
		return PlaceFollowup{Name: name, Topic: topic, TimePreference: when}, true
	}
	pronoun := strings.HasPrefix(lower, "它") || strings.HasPrefix(lower, "这家") || strings.HasPrefix(lower, "这个地方") || strings.HasPrefix(lower, "那里") || strings.HasPrefix(lower, "it ") || strings.HasPrefix(lower, "this place ") || strings.HasPrefix(lower, "is it ") || strings.HasPrefix(lower, "does it ") || strings.HasPrefix(lower, "can it ") || containsAny(lower, " is it ", " does it ", " can it ", " do they ")
	question := containsAny(lower, "?", "？", "吗", "呢", "怎样", "为什么", "如何", "是否", "what ", "when ", "where ", "why ", "how ", "is it ", "does it ", "can it ")
	if knownQuestion || (pronoun && question) {
		if !knownQuestion {
			topic = PlacePublicInformation
		}
		return PlaceFollowup{Topic: topic, TimePreference: when, Pronoun: true}, true
	}
	return none, false
}

func placeFollowupIntent(v PlaceFollowup, current *Task) MVPIntent {
	term := v.Name
	if v.Pronoun {
		term = current.Filters["searchTerm"]
	}
	location := current.Filters["locationPreference"]
	if location == "" {
		location = "city"
	}
	return MVPIntent{Operation: FindPlace, Target: FindPlace, SearchTerm: term, TimePreference: v.TimePreference, LocationPreference: location, Supported: true}
}
