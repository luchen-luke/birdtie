package agentworkspace

import "testing"

func TestRelationshipIntentIsExplicitSelfOnly(t *testing.T) {
	for _, q := range []string{"我的关系信号", "我最近常和谁联系", "who do I often coordinate with"} {
		intent := ParseMVPIntent(q, nil)
		if intent.Operation != PersonalRelationshipContext || !intent.Supported {
			t.Fatalf("%s %+v", q, intent)
		}
	}
	for _, q := range []string{"他的关系信号", "看看她的私聊", "帮我找周末的羽毛球"} {
		if ParseMVPIntent(q, nil).Operation == PersonalRelationshipContext {
			t.Fatal("inferred private relationship request", q)
		}
	}
}
