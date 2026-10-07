// Package agentlocalcandidate proposes transparent, conservative lexical
// hypotheses. It neither resolves source permission nor stages any candidate.
package agentlocalcandidate

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const LegacyVersion = "moment-lexical-category-v1"
const Version = "moment-lexical-category-v2"
const MaxBytes = 12000

var ErrInvalid = errors.New("本地候选输入无效")
var ErrUnavailable = errors.New("当前文字没有足够明确的单一类别关键词")

type Proposal struct {
	AlgorithmVersion string                     `json:"algorithmVersion"`
	Predicate        string                     `json:"predicate"`
	Category         string                     `json:"category"`
	Assessment       agentconfidence.Assessment `json:"assessment"`
	MatchedTerms     []string                   `json:"matchedTerms"`
	Explanation      string                     `json:"explanation"`
}

// Extract accepts only the already selected title/body supplied internally by
// a current native analysis resolver. Shape checks here are not authorization.
func Extract(selected map[string]string) (Proposal, error) {
	return ExtractVersion(Version, selected)
}

// ValidVersionCategory is a closed vocabulary, never a source grant.
func ValidVersionCategory(version, category string) bool {
	if version != LegacyVersion && version != Version {
		return false
	}
	switch category {
	case "badminton", "basketball", "football", "sports", "culture":
		return true
	case "hiking":
		return version == Version
	default:
		return false
	}
}

// ExtractVersion replays an approved algorithm without upgrading its preview.
func ExtractVersion(version string, selected map[string]string) (Proposal, error) {
	if version != LegacyVersion && version != Version {
		return Proposal{}, ErrInvalid
	}
	if len(selected) < 1 || len(selected) > 2 {
		return Proposal{}, ErrInvalid
	}
	fields := []string{}
	for k, v := range selected {
		if (k != "title" && k != "body") || !utf8.ValidString(v) {
			return Proposal{}, ErrInvalid
		}
		fields = append(fields, k)
	}
	sort.Strings(fields)
	text := ""
	for _, k := range fields {
		text += "\n" + selected[k]
	}
	if len(text) > MaxBytes {
		return Proposal{}, ErrInvalid
	}
	text = strings.ToLower(text)
	// Deliberately fail closed for uncertainty and negation. This bounded rule
	// is not a language model or a general claim that every negation is parsed.
	for _, term := range []string{"不", "没", "无", "否", "或", "也许", "可能", "如果", "要是", "似乎", "？", "?"} {
		if strings.Contains(text, term) {
			return Proposal{}, ErrUnavailable
		}
	}
	for _, term := range []string{"no", "not", "never", "cannot", "maybe", "or", "don't", "won't", "can't", "doesn't", "didn't", "isn't", "aren't", "wasn't", "weren't", "shouldn't", "without", "neither", "nor", "perhaps", "possibly", "might", "if", "don’t", "won’t", "can’t", "doesn’t", "didn’t", "isn’t", "aren’t", "wasn’t", "weren’t", "shouldn’t"} {
		if word(text, term) {
			return Proposal{}, ErrUnavailable
		}
	}
	taxonomy := map[string][]string{"badminton": {"羽毛球", "badminton"}, "basketball": {"篮球", "basketball"}, "football": {"足球", "football"}, "sports": {"运动", "sports"}, "culture": {"文化", "culture"}}
	if version == Version {
		taxonomy["hiking"] = []string{"徒步", "hiking"}
	}
	found := map[string][]string{}
	for category, terms := range taxonomy {
		for _, term := range terms {
			match := strings.Contains(text, term)
			if term[0] < 128 {
				match = word(text, term)
			}
			if match {
				found[category] = append(found[category], term)
			}
		}
	}
	if len(found) != 1 {
		return Proposal{}, ErrUnavailable
	}
	for category, terms := range found {
		sort.Strings(terms)
		return Proposal{AlgorithmVersion: version, Predicate: "ACTIVITY_CATEGORY", Category: category, Assessment: agentconfidence.Assessment{Semantics: agentconfidence.Ordinal, Level: agentconfidence.Low}, MatchedTerms: terms, Explanation: "只检测到所选文字中的类别关键词，形成待核验建议；不证明偏好、参加或到场，LOW不是概率，不会写入正式记忆。"}, nil
	}
	return Proposal{}, ErrUnavailable
}
func word(text, term string) bool {
	from := 0
	for from <= len(text) {
		i := strings.Index(text[from:], term)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(term)
		left, right := false, false
		if i > 0 {
			r, _ := utf8.DecodeLastRuneInString(text[:i])
			left = unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
		}
		if end < len(text) {
			r, _ := utf8.DecodeRuneInString(text[end:])
			right = unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
		}
		if !left && !right {
			return true
		}
		from = end
	}
	return false
}
