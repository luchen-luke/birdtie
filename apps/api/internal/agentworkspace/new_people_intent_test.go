package agentworkspace

import "testing"

func TestNewPeopleRoutesExplicitCompanionQueries(t *testing.T) {
	for _, q := range []string{"找新朋友", "想认识新朋友", "帮我找伙伴", "找搭子打羽毛球", "meet new people", "find a companion"} {
		if got := ParseMVPIntent(q, nil); got.Operation != FindNewPeople || !got.Supported {
			t.Fatalf("%q: %+v", q, got)
		}
	}
	for _, q := range []string{"帮我找周末的羽毛球", "我的关系信号", "找公开社群", "发消息给朋友"} {
		if got := ParseMVPIntent(q, nil); got.Operation == FindNewPeople {
			t.Fatalf("unrequested newpeople: %q", q)
		}
	}
}
