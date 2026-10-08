package agentworkspace

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

const PlaceOpeningHours = "opening_hours"

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

// A place followup is deliberately narrower than a general conversation
// resolver. Explicit operation words still use their original parser path.
func ParsePlaceFollowup(query string, current *Task) (PlaceFollowup, bool) {
	var none PlaceFollowup
	if current == nil || baseIntent(current) != FindPlace {
		return none, false
	}
	text := strings.TrimSpace(query)
	lower := strings.ToLower(text)
	if text == "" || len(text) > 240 || !utf8.ValidString(text) || strings.HasPrefix(lower, "找地点") {
		return none, false
	}
	topic := current.Filters[PlaceFollowupTopicFilter]
	if topic != "" && topic != PlaceOpeningHours {
		return none, false
	}
	askingHours := containsAny(lower, "几点开门", "几点关门", "开放时间", "营业时间", "开门时间", "闭馆时间", "opening hours", "when does it open", "when is it open", "what time does it open", "what time does it close")
	if askingHours {
		topic = PlaceOpeningHours
	}
	when := activityTime(lower)
	if when == "anytime" && !containsAny(lower, "不限时间", "任何时间", "anytime") {
		when = current.Filters["timePreference"]
	}
	if !oneOf(when, "", "anytime", "today", "tonight", "tomorrow", "weekend") {
		return none, false
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
	pronoun := strings.HasPrefix(lower, "它") || strings.HasPrefix(lower, "这家") || strings.HasPrefix(lower, "这个地方") || strings.HasPrefix(lower, "那里") || strings.HasPrefix(lower, "it ") || strings.HasPrefix(lower, "this place ") || strings.HasPrefix(lower, "when does it ") || strings.HasPrefix(lower, "when is it ") || strings.HasPrefix(lower, "what time does it ")
	if pronoun && askingHours {
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
