package agentworkspace

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

func replyMembershipTask() Task {
	return Task{
		ID: "11111111-1111-4111-8111-111111111111", PrincipalType: "person",
		PrincipalID:  "22222222-2222-4222-8222-222222222222",
		ActingUserID: "22222222-2222-4222-8222-222222222222",
		ContextType:  "CITY", CityID: "aberdeen-gb",
		Conversation: []Message{
			{Role: "user", Text: "找地点 <>&"},
			{Role: "assistant", Text: "找到 1 个公开地点。"},
			{Role: "user", Text: "再找一个"},
			{Role: "assistant", Text: "找到 1 个公开地点。"},
		},
	}
}

func replyMembershipRefs(kind string, count int) []arp.Ref {
	refs := make([]arp.Ref, count)
	for index := range refs {
		refs[index] = arp.Ref{Type: kind, ID: fmt.Sprintf("33333333-3333-4333-8333-%012d", index+1)}
	}
	return refs
}

func TestReplyTurnDigestCanonicalPrefixOnly(t *testing.T) {
	task := replyMembershipTask()
	digest, err := ReplyTurnDigest(task.Conversation, 1)
	if err != nil {
		t.Fatal(err)
	}
	// The Go encoding/json byte contract preserves role/text field order and
	// escapes HTML characters. The client must hash this exact stored prefix.
	const expected = "850578fb6627f2da1c03b0fae96491a52f33900955ac18e2f1d7327d8497cbe2"
	if digest != expected {
		t.Fatalf("digest = %q, want %q", digest, expected)
	}
	task.Conversation[1].Sources = []AnswerSource{{ID: "citation", Title: "官网", URL: "https://example.org"}}
	task.Conversation[1].SourceRunID = "different-source-run"
	task.Conversation[1].SourceEvidenceDigest = strings.Repeat("a", 64)
	task.Conversation[1].ResultMembership = &ReplyMembership{Schema: "unrelated"}
	task.Conversation[3].Text = "后续回答改变"
	if after, err := ReplyTurnDigest(task.Conversation, 1); err != nil || after != digest {
		t.Fatalf("decorations or later turn changed original digest: %q, %v", after, err)
	}
	task.Conversation[0].Text = "改变原问题"
	if after, err := ReplyTurnDigest(task.Conversation, 1); err != nil || after == digest {
		t.Fatalf("changed prefix retained digest: %q, %v", after, err)
	}
}

func TestReplyTurnDigestRejectsInvalidPrefix(t *testing.T) {
	for _, index := range []int{-1, 0, 4} {
		if _, err := ReplyTurnDigest(replyMembershipTask().Conversation, index); err == nil {
			t.Errorf("accepted index %d", index)
		}
	}
	task := replyMembershipTask()
	task.Conversation[0].Role = "system"
	if _, err := ReplyTurnDigest(task.Conversation, 1); err == nil {
		t.Fatal("accepted unknown role in prefix")
	}
}

func TestReplyMembershipCopiesReferencesAndKeepsStableID(t *testing.T) {
	task := replyMembershipTask()
	refs := replyMembershipRefs("place", 1)
	membership, err := NewReplyMembership(task, 1, "place", refs)
	if err != nil {
		t.Fatal(err)
	}
	if membership.Schema != ReplyMembershipSchema || membership.TaskID != task.ID || membership.CityID != task.CityID ||
		membership.ResultSetID != "reply:"+task.ID+":"+membership.TurnDigest[:24] {
		t.Fatalf("unexpected membership: %+v", membership)
	}
	task.Conversation[1].ResultMembership = membership
	if err := ValidateReplyMembership(task, 1); err != nil {
		t.Fatal(err)
	}
	refs[0].ID = "caller-mutated-ref"
	if membership.Refs[0].ID == refs[0].ID {
		t.Fatal("caller reference slice was retained")
	}
	task.Conversation[1].Sources = []AnswerSource{{ID: "new-citation"}}
	task.Conversation[1].SourceRunID = "new-run"
	task.Conversation[1].SourceEvidenceDigest = strings.Repeat("b", 64)
	if err := ValidateReplyMembership(task, 1); err != nil {
		t.Fatalf("citation changes invalidated role/text membership: %v", err)
	}
}

func TestReplyMembershipRejectsChangedBindings(t *testing.T) {
	cases := map[string]func(*Task){
		"prefix-user":      func(task *Task) { task.Conversation[0].Text = "different question" },
		"prefix-assistant": func(task *Task) { task.Conversation[1].Text = "different answer" },
		"prefix-role":      func(task *Task) { task.Conversation[0].Role = "tool" },
		"task-id":          func(task *Task) { task.ID = "44444444-4444-4444-8444-444444444444" },
		"city-id":          func(task *Task) { task.CityID = "edinburgh-gb" },
		"schema":           func(task *Task) { task.Conversation[1].ResultMembership.Schema = "old-schema" },
		"membership-task":  func(task *Task) { task.Conversation[1].ResultMembership.TaskID = "other-task" },
		"membership-city":  func(task *Task) { task.Conversation[1].ResultMembership.CityID = "other-city" },
		"digest":           func(task *Task) { task.Conversation[1].ResultMembership.TurnDigest = strings.Repeat("a", 64) },
		"stable-result-id": func(task *Task) { task.Conversation[1].ResultMembership.ResultSetID = "latest-result" },
		"kind":             func(task *Task) { task.Conversation[1].ResultMembership.Kind = "business" },
		"ref-duplicate":    func(task *Task) { m := task.Conversation[1].ResultMembership; m.Refs = append(m.Refs, m.Refs[0]) },
		"ref-mixed-kind":   func(task *Task) { task.Conversation[1].ResultMembership.Refs[0].Type = "activity" },
		"ref-invalid":      func(task *Task) { task.Conversation[1].ResultMembership.Refs[0].ID = " invalid " },
		"ref-over-bound": func(task *Task) {
			task.Conversation[1].ResultMembership.Refs = replyMembershipRefs("place", MaxReplyRefs+1)
		},
		"no-membership": func(task *Task) { task.Conversation[1].ResultMembership = nil },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			task := replyMembershipTask()
			membership, err := NewReplyMembership(task, 1, "place", replyMembershipRefs("place", 1))
			if err != nil {
				t.Fatal(err)
			}
			task.Conversation[1].ResultMembership = membership
			change(&task)
			if err := ValidateReplyMembership(task, 1); err == nil {
				t.Fatal("accepted changed binding")
			}
		})
	}
}

func TestReplyMembershipCreationBoundsAndPersonalCity(t *testing.T) {
	for _, kind := range []string{"activity", "place", "organization"} {
		for _, count := range []int{0, MaxReplyRefs} {
			membership, err := NewReplyMembership(replyMembershipTask(), 1, kind, replyMembershipRefs(kind, count))
			if err != nil || membership == nil || membership.Refs == nil || len(membership.Refs) != count {
				t.Fatalf("kind=%s count=%d: %+v, %v", kind, count, membership, err)
			}
		}
	}
	cases := map[string]func(*Task){
		"organization-principal": func(task *Task) { task.PrincipalType = "organization" },
		"online-context":         func(task *Task) { task.ContextType = "ONLINE" },
		"missing-context":        func(task *Task) { task.ContextType = "" },
		"missing-city":           func(task *Task) { task.CityID = "" },
		"large-city":             func(task *Task) { task.CityID = strings.Repeat("a", 161) },
		"invalid-city":           func(task *Task) { task.CityID = " city " },
		"invalid-task-id":        func(task *Task) { task.ID = "local-task" },
		"zero-task-id":           func(task *Task) { task.ID = "00000000-0000-0000-0000-000000000000" },
		"invalid-principal-id":   func(task *Task) { task.PrincipalID = "owner" },
		"invalid-acting-id":      func(task *Task) { task.ActingUserID = "actor" },
		"different-actor":        func(task *Task) { task.ActingUserID = "44444444-4444-4444-8444-444444444444" },
		"no-prior-user": func(task *Task) {
			task.Conversation = []Message{{Role: "assistant", Text: "answer"}, {Role: "assistant", Text: "answer"}}
		},
		"empty-user": func(task *Task) { task.Conversation[0].Text = "  " },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			task := replyMembershipTask()
			change(&task)
			if _, err := NewReplyMembership(task, 1, "place", nil); err == nil {
				t.Fatal("accepted invalid personal city task")
			}
		})
	}
	for _, kind := range []string{"", "person", "business", "opportunity", "PLACE"} {
		if _, err := NewReplyMembership(replyMembershipTask(), 1, kind, nil); err == nil {
			t.Errorf("accepted kind %q", kind)
		}
	}
	if _, err := NewReplyMembership(replyMembershipTask(), 1, "place", replyMembershipRefs("place", MaxReplyRefs+1)); err == nil {
		t.Fatal("accepted more than the reference bound")
	}
	for _, index := range []int{-1, 0, 4} {
		if err := ValidateReplyMembership(replyMembershipTask(), index); err == nil {
			t.Errorf("validated invalid index %d", index)
		}
	}
}

func TestReplyMembershipLatestBoundsAndOrder(t *testing.T) {
	task := replyMembershipTask()
	task.Conversation = nil
	for turn := 0; turn < MaxMessageResults+5; turn++ {
		task.Conversation = append(task.Conversation, Message{Role: "user", Text: fmt.Sprintf("question %d", turn)}, Message{Role: "assistant", Text: "answer"})
		index := len(task.Conversation) - 1
		membership, err := NewReplyMembership(task, index, "place", replyMembershipRefs("place", 1))
		if err != nil {
			t.Fatal(err)
		}
		task.Conversation[index].ResultMembership = membership
	}
	indexes := LatestReplyMemberships(task)
	if len(indexes) != MaxMessageResults || indexes[0] != 11 || indexes[len(indexes)-1] != 69 {
		t.Fatalf("latest indexes = %v", indexes)
	}
	for i := 1; i < len(indexes); i++ {
		if indexes[i] <= indexes[i-1] {
			t.Fatalf("indexes are not chronological: %v", indexes)
		}
	}
	// Invalid and absent memberships are omitted; valid older turns fill the
	// bound without placing the latest result under either missing message.
	task.Conversation[69].ResultMembership.ResultSetID = "invalid"
	task.Conversation[67].ResultMembership = nil
	indexes = LatestReplyMemberships(task)
	if len(indexes) != MaxMessageResults || indexes[0] != 7 || indexes[len(indexes)-1] != 65 {
		t.Fatalf("latest eligible indexes = %v", indexes)
	}
	if got := LatestReplyMemberships(replyMembershipTask()); !reflect.DeepEqual(got, []int{}) {
		t.Fatalf("legacy messages acquired memberships: %v", got)
	}
}

func TestReplyMembershipNativeRuleReply(t *testing.T) {
	for kind, label := range map[string]string{"activity": "当前可见活动", "place": "公开地点", "organization": "公开组织"} {
		emptyLabel := label
		if kind == "activity" {
			emptyLabel = "可见活动"
		}
		if got := NativeRuleReply(kind, 0); got != "当前没有符合条件的"+emptyLabel+"。" {
			t.Errorf("empty %s reply = %q", kind, got)
		}
		if got := NativeRuleReply(kind, 2); got != "找到 2 个"+label+"。" {
			t.Errorf("counted %s reply = %q", kind, got)
		}
		for _, count := range []int{-1, MaxReplyRefs + 1} {
			if got := NativeRuleReply(kind, count); got != "" {
				t.Errorf("invalid count %d reply = %q", count, got)
			}
		}
	}
	if got := NativeRuleReply("person", 1); got != "" {
		t.Errorf("unsupported kind reply = %q", got)
	}
}
