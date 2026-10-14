package agentworkspace

import (
	"context"
	"errors"
	"strings"
	"unicode"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

const PlaceAddress = "address"

// A named native place may exist without a current independently permitted
// hours field. This is a specific unavailable fact, never a network fallback.
var ErrLocalPlaceHoursUnavailable = errors.New("local place opening hours evidence unavailable")

// A place query with no usable independently permitted real facts is not a
// successful empty answer. This does not authorize an external fallback.
var ErrLocalPlaceFactsUnavailable = errors.New("local place facts evidence unavailable")

// LocalPlaceQuestion is a bounded fact request, not a semantic search engine.
// Only a literal named address/hours record or a food recommendation can take
// this branch. Current opening status and travel routes are not calculated.
type LocalPlaceQuestion struct {
	Name, Field    string
	Recommendation bool
}

func validLocalPlaceFactName(name string) bool {
	if !ValidPlaceFollowupName(name) {
		return false
	}
	switch strings.ToLower(name) {
	case "it", "they", "them", "this", "that", "this place", "that place", "here", "nearby", "它", "这家", "那里", "这个地方", "那个地方", "这里", "附近":
		return false
	}
	return true
}

func ParseLocalPlaceQuestion(query string) (LocalPlaceQuestion, bool) {
	// Native local-fact replies also parse the raw current query independently.
	// Keep category-wide field requests on the original FindPlace result/source
	// path instead of inventing a literal name such as "显示餐馆".
	if _, objective := parseObjectivePlaceRefinement(query); objective {
		return LocalPlaceQuestion{}, false
	}
	t := strings.TrimSpace(query)
	for _, r := range t {
		if unicode.IsControl(r) {
			return LocalPlaceQuestion{}, false
		}
	}
	lower := strings.ToLower(t)
	if t == "" || len(t) > 240 || containsAny(lower, "记住", "忘记", "纠正", "remember", "forget", "朋友", "friend", "引用", "网页", "webpage", "\"", "“", "”", "新地点", "探索新", "explore new", "new places", "我家", "住址", "my home", "my house", "私人", "private", "忽略", "ignore instructions") {
		return LocalPlaceQuestion{}, false
	}
	for _, exact := range []string{"附近有什么适合我的晚餐", "附近有什么适合我的晚餐？", "推荐晚餐地点", "推荐晚餐地点？", "recommend places for dinner", "what nearby places suit me for dinner?", "what nearby places suit me for dinner"} {
		if lower == exact {
			return LocalPlaceQuestion{Field: "food", Recommendation: true}, true
		}
	}
	for _, request := range []struct{ suffix, field string }{
		{"的地址是什么", PlaceAddress}, {"的地址", PlaceAddress}, {"地址是什么", PlaceAddress},
		{"的营业时间是什么", PlaceOpeningHours}, {"的开放时间是什么", PlaceOpeningHours},
		{"的营业时间", PlaceOpeningHours}, {"的开放时间", PlaceOpeningHours},
		{"营业时间是什么", PlaceOpeningHours}, {"开放时间是什么", PlaceOpeningHours},
	} {
		plain := strings.TrimRight(t, "？?。")
		if strings.HasSuffix(plain, request.suffix) {
			name := strings.TrimSpace(strings.TrimSuffix(plain, request.suffix))
			if validLocalPlaceFactName(name) {
				return LocalPlaceQuestion{Name: name, Field: request.field}, true
			}
			// Do not reinterpret a rejected pronoun such as "这家" as the
			// different literal name "这家的" using a shorter matching suffix.
			return LocalPlaceQuestion{}, false
		}
	}
	for _, request := range []struct{ prefix, field string }{
		{"what is the address of ", PlaceAddress}, {"where is ", PlaceAddress},
		{"what are the opening hours of ", PlaceOpeningHours},
		{"what are the opening hours for ", PlaceOpeningHours},
		{"opening hours of ", PlaceOpeningHours}, {"opening hours for ", PlaceOpeningHours},
	} {
		if strings.HasPrefix(lower, request.prefix) {
			name := strings.TrimSpace(strings.TrimRight(t[len(request.prefix):], "?？。"))
			if validLocalPlaceFactName(name) {
				return LocalPlaceQuestion{Name: name, Field: request.field}, true
			}
		}
	}
	return LocalPlaceQuestion{}, false
}

// This grammar identifies only a literal question about the selected place's
// recorded hours. It does not resolve a place or authorize use of prior state;
// the native owner must still validate the current selected entity and proof.
func IsLocalPlaceHoursFollowupQuery(query string) bool {
	text := strings.ToLower(strings.TrimRight(strings.TrimSpace(query), " ?？!！。."))
	switch text {
	case "它的营业时间", "它的营业时间是什么", "它的开放时间", "它的开放时间是什么",
		"这家的营业时间", "这家的营业时间是什么", "这家的开放时间", "这家的开放时间是什么",
		"这个地方的营业时间", "这个地方的开放时间", "它几点开门", "它几点关门",
		"what are its opening hours", "its opening hours", "what are this place's opening hours",
		"what are the opening hours of it", "what are the opening hours for it",
		"what are the opening hours of this place", "what are the opening hours for this place",
		"what time does it open", "what time does it close", "when does it open":
		return true
	}
	return false
}

// Only a closed request for the selected public place's recorded address.
// This identifies the topic; the native resolver still owns uniqueness,
// canonical identity, current permission and field lifetime checks.
func IsLocalPlaceAddressFollowupQuery(query string) bool {
	text := strings.ToLower(strings.TrimRight(strings.TrimSpace(query), " ?？!！。."))
	switch text {
	case "地址", "地址是什么", "它的地址", "它的地址是什么",
		"这家的地址", "这家的地址是什么", "这个地方的地址", "这个地方的地址是什么",
		"它在哪里", "这家在哪里", "这个地方在哪里",
		"address", "its address", "what is its address", "what is this place's address",
		"what is the address of it", "what is the address of this place",
		"where is it", "where is this place":
		return true
	}
	return false
}

// Implemented by the native owner boundary. A declined local match does not
// authorize a network call, retained source, Memory export or another writer.
type LocalPlaceReplyPort interface {
	TryOwnLocalPlaceReply(context.Context, arp.Access) (LiveReply, bool, error)
}
