package postgres

import (
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
)

func TestModelLiveReplySourcesPreserveOriginalPrefixAndRealSites(t *testing.T) {
	selected := []modelegressbudget.LivePublicSource{
		{Title: "UNIT claimed official", URL: "https://aberdeeninspired.com/gallery", Passage: "UNIT source one"},
		{Title: "UNIT source two", URL: "https://example.com/other", Passage: "UNIT source two"},
	}
	got, err := liveReplyAnswerSources(modelegressbudget.LiveSourceAnswerPolicyVersion, "Times disagree [source-2] [source-1].", selected)
	if err != nil || len(got) != 2 || got[0].ID != "source-1" || got[0].URL != selected[0].URL || got[0].Title != selected[0].Title || got[0].Site != "aberdeeninspired.com" || got[1].ID != "source-2" || got[1].Site != "example.com" {
		t.Fatal("reply presentation reordered citations or upgraded source identity", got, err)
	}
	selected[0].Title = "caller mutation"
	if got[0].Title == selected[0].Title || got[0].PublishedAt != "" {
		t.Fatal("reply source copy changed or invented publication time")
	}
	for _, answer := range []string{"官方来源 [source-1]", "[source-3]", "no citations"} {
		if got, err := liveReplyAnswerSources(modelegressbudget.LiveSourceAnswerPolicyVersion, answer, selected); !errors.Is(err, modelegressbudget.ErrDenied) || got != nil {
			t.Fatal("unsupported final reply gained sources or publication")
		}
	}
}

func TestModelLiveReplySourcesOldPromptContractRemainsIndependent(t *testing.T) {
	selected := []modelegressbudget.LivePublicSource{{Title: "UNIT old source", URL: "https://example.com/old", Passage: "UNIT old selection"}}
	for _, version := range []string{"now_public_sources_20261008_v1", "unit-original-native-prompt"} {
		got, err := liveReplyAnswerSources(version, "UNIT original no-citation TEXT", selected)
		if err != nil || len(got) != 1 || got[0].Site != "example.com" {
			t.Fatal("v2 rewrote an original immutable output contract", err)
		}
	}
	if _, err := liveReplyAnswerSources(modelegressbudget.LiveSourceAnswerPolicyVersion, "UNIT original no-citation TEXT", selected); !errors.Is(err, modelegressbudget.ErrDenied) {
		t.Fatal("native v2 prompt accepted legacy unreferenced text")
	}
}
