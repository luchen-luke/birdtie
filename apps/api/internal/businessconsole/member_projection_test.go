package businessconsole

import (
	"strings"
	"testing"
)

func TestBusinessMemberDisplayNamePreservesHumanProfileBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"approved ordinary label", "  合成成员姓名  ", "合成成员姓名"},
		{"original ASCII byte limit", strings.Repeat("n", 80), strings.Repeat("n", 80)},
		{"original UTF8 byte limit", strings.Repeat("中", 26) + "nn", strings.Repeat("中", 26) + "nn"},
		{"no invented missing name", "", ""},
		{"no whitespace label", "   ", ""},
		{"short legacy value", "a", ""},
		{"no truncation of long identity", strings.Repeat("n", 81), ""},
		{"bytes rather than runes", strings.Repeat("中", 27), ""},
		{"no NUL", "名字\x00", ""},
		{"no embedded line break", "成员\n名字", ""},
		{"controls rejected before trim", "\t成员姓名", ""},
		{"invalid UTF8", string([]byte{'x', 0xff}), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeMemberDisplayName(tc.input); got != tc.want {
				t.Fatalf("ordinary Profile label boundary: got %q want %q", got, tc.want)
			}
		})
	}
}
