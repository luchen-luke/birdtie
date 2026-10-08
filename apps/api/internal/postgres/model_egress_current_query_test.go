package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
)

func TestCurrentEgressQueryOnlyExportsCurrentNativeUserTurn(t *testing.T) {
	for _, test := range []struct {
		name, original, filters, conversation, want string
		denied                                      bool
	}{
		{"legacy scalar", "找活动", `{}`, `[]`, "找活动", false},
		{"legacy conversation", "找活动", `{}`, `[{"role":"user","text":"找活动"},{"role":"assistant","text":"previous answer"}]`, "找活动", false},
		{"first current question", "找活动", `{"currentQuery":"找活动"}`, `[{"role":"user","text":"找活动"}]`, "找活动", false},
		{"followup", "找活动", `{"currentQuery":"只看周六的"}`, `[{"role":"user","text":"找活动"},{"role":"assistant","text":"private old answer"},{"role":"user","text":"只看周六的"}]`, "只看周六的", false},
		{"after answer", "找活动", `{"currentQuery":"近一点的呢"}`, `[{"role":"user","text":"找活动"},{"role":"assistant","text":"old answer"},{"role":"user","text":"近一点的呢"},{"role":"assistant","text":"current answer"}]`, "近一点的呢", false},
		{"preserves current whitespace", "找活动", `{"currentQuery":" 只看周六的 "}`, `[{"role":"user","text":"只看周六的"}]`, " 只看周六的 ", false},
		{"stale original", "找活动", `{"currentQuery":"找活动"}`, `[{"role":"user","text":"找活动"},{"role":"user","text":"只看周六的"}]`, "", true},
		{"followup filter absent", "找活动", `{}`, `[{"role":"user","text":"找活动"},{"role":"user","text":"只看周六的"}]`, "", true},
		{"last user private canary mismatch", "帮我查询周末羽毛球", `{}`, `[{"role":"user","text":"PRIVATE_CONVERSATION_NOT_EGRESS"}]`, "", true},
		{"current lacks user source", "找活动", `{"currentQuery":"只看周六的"}`, `[{"role":"assistant","text":"只看周六的"}]`, "", true},
		{"empty current", "找活动", `{"currentQuery":""}`, `[]`, "", true},
		{"non-string filter", "找活动", `{"currentQuery":true}`, `[]`, "", true},
		{"invalid conversation", "找活动", `{}`, `{}`, "", true},
		{"empty latest user", "找活动", `{}`, `[{"role":"user","text":""}]`, "", true},
		{"oversized original", strings.Repeat("x", 4097), `{}`, `[]`, "", true},
		{"null character", "find\x00place", `{}`, `[]`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := currentEgressQuery(test.original, []byte(test.filters), []byte(test.conversation))
			if got != test.want || (test.denied && !errors.Is(err, modelegressbudget.ErrDenied)) || (!test.denied && err != nil) {
				t.Fatalf("got query %q, error %v; want %q, denied %v", got, err, test.want, test.denied)
			}
		})
	}
}
