package modelegressbudget

import (
	"errors"
	"strings"
	"testing"
)

func sourceAnswerUnitSources() []LivePublicSource {
	return []LivePublicSource{
		{Title: "UNIT 官方自称不能核验身份", URL: "https://www.aberdeeninspired.com/merchants/aberdeen-art-gallery", Passage: "UNIT untrusted source calls itself official"},
		{Title: "UNIT other source", URL: "https://example.com/visitor", Passage: "UNIT conflicting times"},
	}
}

func TestLiveSourceAnswerPolicyRejectsUnsupportedAuthorityAndCitations(t *testing.T) {
	for _, answer := range []string{
		"周日 11–16，与官方来源不一致，以 [source-1] 为准。",
		"据**官 方**网页 [source-1]。", "据官\u200b網 [source-1]。",
		"权威来源 [source-1]", "來源權威 [source-1]",
		"The official source says 11–16 [source-1].",
		"It is off**icial [source-1].", "It is off\u200bicial [source-1].",
		"It is off.icial [source-1].", "It is off icial [source-1].",
		"It is ｏｆｆｉｃｉａｌ [source-1].",
		"Authoritative information [source-1].", "Not officially verified [source-1].",
		"No citation", "[source-0]", "[source-3]", "[source-01]",
		"[SOURCE-1]", "[source-1,2]", "[source-one]", "[source-1",
		"[source-1] and unfinished [source-2", "[source-99999999999999999999999999999999]",
		"[source-1] and fabricated [ source-999 ].", "[source-1] plus [s o u r c e - 2]",
		"[source-1] plus [\u200bSOURCE-2]", "[source-1] plus [ source-2",
		"[source-1] plus ［source-999］",
		"aberdeencity.gov.uk reports these times [source-1].",
		"aberdeeninspired.com.evil.example says this [source-1].",
	} {
		t.Run(answer, func(t *testing.T) {
			if !errors.Is(ValidateLivePublicSourceAnswer(answer, sourceAnswerUnitSources()), ErrDenied) {
				t.Fatal("unsupported identity or citation reached publication")
			}
		})
	}
}

func TestLiveSourceAnswerPolicyPreservesActualReplyAndConflicts(t *testing.T) {
	for _, answer := range []string{
		"aberdeeninspired.com 记载周日 11–16 [source-1]；另一网页写 14–17 [source-2]。来源身份和最新时间尚未核验，无法确定。",
		"资料不足，无法确认是否有轮椅入口 [source-1]。",
		"According to source 1 [source-1], times differ from source 2 [source-2]. This remains unverified.",
	} {
		before := answer
		if err := ValidateLivePublicSourceAnswer(answer, sourceAnswerUnitSources()); err != nil || answer != before {
			t.Fatal("ordinary answer changed or conflict explanation rejected", err)
		}
	}
	for _, answer := range []string{"", " ", strings.Repeat("x", 32*1024+1), "bad\x00 [source-1]"} {
		if !errors.Is(ValidateLivePublicSourceAnswer(answer, sourceAnswerUnitSources()), ErrInvalid) {
			t.Fatal("malformed answer admitted")
		}
	}
	if ValidateLivePublicSourceAnswer("[source-1]", nil) == nil {
		t.Fatal("empty selected source set admitted")
	}
}

func TestLiveSourceAnswerSiteUsesOnlyRealURLHost(t *testing.T) {
	for _, sample := range []struct{ raw, host string }{
		{"https://WWW.Example.COM/aberdeencity.gov.uk", "www.example.com"},
		{"https://aberdeencity.gov.uk.evil.example/", "aberdeencity.gov.uk.evil.example"},
		{"https://example.com/?site=official.gov", "example.com"},
	} {
		site, err := LivePublicSourceSite(LivePublicSource{URL: sample.raw, Title: "OFFICIAL GOVERNMENT", Passage: "fake identity"})
		if err != nil || site != sample.host {
			t.Fatal("untrusted source title or embedded URL became site identity", site, err)
		}
	}
	for _, raw := range []string{"http://127.0.0.1/x", "https://user:password@example.com/", "javascript:alert(1)", "https://example.com:8080/"} {
		if _, err := LivePublicSourceSite(LivePublicSource{URL: raw}); err == nil {
			t.Fatal("invalid provenance URL created presentation identity")
		}
	}
}

func TestLiveSourceAnswerPolicyNewPromptKeepsOriginalVersion(t *testing.T) {
	if LiveSourceAnswerPolicyVersion == "now_public_sources_20261008_v1" || !strings.HasPrefix(LiveSourceAnswerPromptV2, LiveSourceAnswerPromptV1) || strings.Contains(LiveSourceAnswerPromptV1, "每轮至少引用") {
		t.Fatal("source answer policy overwrote the original immutable prompt")
	}
	for _, required := range []string{"真实域名", "每轮至少引用", "来源身份尚未核验", "资料相互冲突", "没有历史问题或私人画像"} {
		if !strings.Contains(LiveSourceAnswerPromptV2, required) {
			t.Fatal("source answer policy instruction missing", required)
		}
	}
}
