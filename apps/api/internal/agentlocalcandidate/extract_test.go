package agentlocalcandidate

import (
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"reflect"
	"strings"
	"testing"
)

func TestLocalCandidateTransparentLexicalProposal(t *testing.T) {
	for _, item := range []struct{ text, category string }{{"周六羽毛球练习", "badminton"}, {"Basketball after class", "basketball"}, {"足球训练", "football"}, {"文化交流", "culture"}, {"Sports tomorrow", "sports"}} {
		p, e := Extract(map[string]string{"body": item.text})
		if e != nil || p.Category != item.category || p.Assessment.Semantics != agentconfidence.Ordinal || p.Assessment.Level != agentconfidence.Low || p.Assessment.Value != nil || p.AlgorithmVersion != Version {
			t.Fatal(p, e)
		}
	}
	a, e := Extract(map[string]string{"title": "badminton", "body": "周六羽毛球练习"})
	b, f := Extract(map[string]string{"body": "周六羽毛球练习", "title": "badminton"})
	if e != nil || f != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("deterministic selected fields", a, b, e, f)
	}
}
func TestLocalCandidateRejectsUnknownAmbiguousOrNegated(t *testing.T) {
	for _, text := range []string{"", "天气晴朗", "羽毛球和篮球", "不打羽毛球", "No badminton", "badminton or football", "可能打羽毛球", "badminton?", "badmintonbuddy", "I can't play badminton", "I don’t play badminton", "Perhaps badminton", "如果想打羽毛球", "羽毛球或其他事情"} {
		p, e := Extract(map[string]string{"body": text})
		if e == nil || !reflect.DeepEqual(p, Proposal{}) {
			t.Fatal(text, p, e)
		}
	}
}
func TestLocalCandidateDoesNotLoadOtherFields(t *testing.T) {
	for _, input := range []map[string]string{nil, {}, {"media": "羽毛球"}, {"body": "badminton", "privateLinks": "足球"}, {"title": string([]byte{255})}, {"body": strings.Repeat("a", MaxBytes+1)}} {
		p, e := Extract(input)
		if e == nil || !reflect.DeepEqual(p, Proposal{}) {
			t.Fatal(p, e)
		}
	}
}

func TestLocalCandidateHikingTransparentOrdinal(t *testing.T) {
	for _, text := range []string{"周末徒步记录", "Hiking after class"} {
		p, e := Extract(map[string]string{"body": text})
		if e != nil || p.Category != "hiking" || p.Assessment.Semantics != agentconfidence.Ordinal || p.Assessment.Level != agentconfidence.Low || p.Assessment.Value != nil {
			t.Fatal("hiking must be a transparent ordinal hypothesis", p, e)
		}
	}
}

func TestLocalCandidatePinnedLegacyAndClosedV2(t *testing.T) {
	for _, text := range []string{"徒步", "hiking"} {
		if _, e := ExtractVersion(LegacyVersion, map[string]string{"body": text}); e == nil {
			t.Fatal("legacy algorithm cannot produce hiking")
		}
	}
	// Original v1 approved culture remains exactly v1, even when its source also
	// has a word that only v2 recognizes. V2 correctly treats that as ambiguous.
	p, e := ExtractVersion(LegacyVersion, map[string]string{"body": "culture hiking"})
	if e != nil || p.Category != "culture" || p.AlgorithmVersion != LegacyVersion {
		t.Fatal(p, e)
	}
	if _, e = Extract(map[string]string{"body": "culture hiking"}); e == nil {
		t.Fatal("v2 ambiguous categories")
	}
	for _, text := range []string{"不徒步", "Maybe hiking", "hiking or football", "hikingbuddy", "徒步？"} {
		if _, e := Extract(map[string]string{"body": text}); e == nil {
			t.Fatal("uncertainty/negation must remain closed", text)
		}
	}
	for _, version := range []string{"", "v3", "moment-lexical-category-v2 "} {
		if _, e := ExtractVersion(version, map[string]string{"body": "hiking"}); e == nil {
			t.Fatal("unknown algorithm", version)
		}
	}
	if ValidVersionCategory(LegacyVersion, "hiking") || !ValidVersionCategory(Version, "hiking") || ValidVersionCategory(Version, "health") {
		t.Fatal("closed version/category pair")
	}
}
