package entityaction

import (
	"strings"
	"testing"
	"time"
)

func TestEntityActionClosedProjection(t *testing.T) {
	n := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for _, kind := range []string{"person", "activity", "place", "community", "organization", "business"} {
		ref := Ref{Type: kind, ID: "22e9cd18-babb-4359-9d1e-53593bedff47"}
		v := Project(ref, Facts{}, n, n.Add(30*time.Second), "测试来源", strings.Repeat("a", 64))
		if !v.Valid() {
			t.Fatalf("%s", kind)
		}
		for _, a := range v.Actions {
			if a.State != Unavailable {
				t.Fatal("implicit permission")
			}
		}
	}
	v := Project(Ref{"activity", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanShare: true, CanJoin: true}, n, n.Add(20*time.Second), "测试来源", strings.Repeat("a", 64))
	if !v.Valid() {
		t.Fatal("valid")
	}
	v.Actions[0].State = Available
	if v.Valid() {
		t.Fatal("activity cannot connect")
	}
}

func TestEntityActionNoWireApprovalOrGenericVerb(t *testing.T) {
	n := time.Now().UTC()
	v := Project(Ref{"person", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanConnect: true}, n, n.Add(time.Second), "测试来源", strings.Repeat("a", 64))
	v.Actions[0].Kind = "EXECUTE"
	if v.Valid() {
		t.Fatal("unknown action")
	}
	v = Project(v.Entity, Facts{}, n, n.Add(31*time.Second), "测试来源", strings.Repeat("a", 64))
	if v.Valid() {
		t.Fatal("unbounded")
	}
	v = Project(v.Entity, Facts{}, n, n.Add(time.Second), "测试来源", strings.Repeat("a", 64))
	v.Actions[1] = v.Actions[0]
	if v.Valid() {
		t.Fatal("duplicate")
	}
}

func TestEntityActionInvitationOperationsClosed(t *testing.T) {
	n := time.Now().UTC()
	makeView := func() View {
		return Project(Ref{"community", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanJoin: true, Invited: true}, n, n.Add(time.Second), "合成邀请", strings.Repeat("a", 64))
	}
	v := makeView()
	if !v.Valid() || v.Actions[3].Operation != "ACCEPT_INVITATION" || len(v.Actions[3].AllowedOperations) != 2 || v.Actions[3].AllowedOperations[1] != "DECLINE_INVITATION" {
		t.Fatal("exact invitation choices")
	}
	for _, ops := range [][]string{nil, {}, {"ACCEPT_INVITATION", "ACCEPT_INVITATION"}, {"ACCEPT_INVITATION", "EXECUTE"}, {"DECLINE_INVITATION", "ACCEPT_INVITATION"}, {"ACCEPT_INVITATION", "LEAVE"}} {
		v = makeView()
		v.Actions[3].AllowedOperations = ops
		if v.Valid() {
			t.Fatal("invalid operations accepted", ops)
		}
	}
}

func TestEntityActionExportAndConversationOperationsClosed(t *testing.T) {
	n := time.Now().UTC()
	for _, c := range []struct {
		ref           Ref
		facts         Facts
		index         int
		first, second string
	}{
		{Ref{"place", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanShare: true, CanExport: true}, 2, "CHOOSE_RECIPIENT", "EXPORT_PUBLIC"},
		{Ref{"person", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanConnect: true, CanRequestConversation: true}, 0, "REQUEST_FRIEND", "REQUEST_CONVERSATION"},
	} {
		makeView := func() View {
			return Project(c.ref, c.facts, n, n.Add(time.Second), "闭集具体操作", strings.Repeat("a", 64))
		}
		v := makeView()
		if !v.Valid() || len(v.Actions[c.index].AllowedOperations) != 2 || v.Actions[c.index].AllowedOperations[1] != c.second {
			t.Fatal("missing exact pair", v)
		}
		for _, ops := range [][]string{{}, {c.first, c.first}, {c.second, c.first}, {c.first, "EXECUTE"}, {c.first, "DECLINE_INVITATION"}} {
			v = makeView()
			v.Actions[c.index].AllowedOperations = ops
			if v.Valid() {
				t.Fatal("invalid closed operation accepted", ops)
			}
		}
	}
}

func TestEntityActionAvailableOperationReasonConsistent(t *testing.T) {
	n := time.Now().UTC()
	for _, c := range []struct {
		ref   Ref
		facts Facts
		kind  string
		word  string
	}{
		{Ref{"place", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanExport: true}, Share, "系统分享"},
		{Ref{"person", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanRequestConversation: true}, Connect, "私信申请"},
		{Ref{"person", "22e9cd18-babb-4359-9d1e-53593bedff47"}, Facts{CanRequestConversation: true, Connected: true, Pending: true}, Connect, "私信申请"},
	} {
		v := Project(c.ref, c.facts, n, n.Add(time.Second), "当前明确资料", strings.Repeat("a", 64))
		for _, a := range v.Actions {
			if a.Kind == c.kind && (a.State != Available || !strings.Contains(a.Reason, c.word) || strings.Contains(a.Reason, "不支持") || a.Reason == "已经是好友。" || a.Reason == "已有待处理的连接申请。") {
				t.Fatalf("available operation with contradictory consequence: %+v", a)
			}
		}
	}
}
