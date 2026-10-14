package media

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func taskImageUnitFixture() (agentworkspace.Task, TaskImageInput) {
	t := agentworkspace.Task{ID: "11111111-1111-4111-8111-111111111111", PrincipalType: "person", PrincipalID: "22222222-2222-4222-8222-222222222222", ContextType: "CITY", ContextID: "33333333-3333-4333-8333-333333333333", CityID: "unit-city", Status: agentworkspace.TaskCompleted, UpdatedAt: time.Date(2026, 10, 8, 2, 0, 0, 123000, time.UTC), Conversation: []agentworkspace.Message{{Role: "user", Text: "<当前&问题>\u2028"}, {Role: "assistant", Text: "合成原回答"}}}
	digest, _ := TaskImageMessageDigest(t, 0)
	t.ActingUserID = t.PrincipalID
	p := TaskImageInput{OperationID: "44444444-4444-4444-8444-444444444444", MessageIndex: 0, PrefixDigest: digest, TaskUpdatedAt: t.UpdatedAt, MIME: "image/png", ByteSize: 100, SHA256: strings.Repeat("a", 64), PixelRisk: "UNKNOWN", Purpose: TaskImagePurpose}
	return t, p
}
func TestTaskImageClosedInput(t *testing.T) {
	_, p := taskImageUnitFixture()
	if e := ValidateTaskImageInput(p); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name   string
		change func(*TaskImageInput)
	}{
		{"foreignPurpose", func(p *TaskImageInput) { p.Purpose = PrivateImagePurpose }},
		{"modelPurpose", func(p *TaskImageInput) { p.Purpose = "MODEL_CONTEXT_EGRESS" }},
		{"missingOperation", func(p *TaskImageInput) { p.OperationID = "" }},
		{"negativeIndex", func(p *TaskImageInput) { p.MessageIndex = -1 }},
		{"largeIndex", func(p *TaskImageInput) { p.MessageIndex = 256 }},
		{"missingDigest", func(p *TaskImageInput) { p.PrefixDigest = "" }},
		{"missingSnapshot", func(p *TaskImageInput) { p.TaskUpdatedAt = time.Time{} }},
		{"fakeMime", func(p *TaskImageInput) { p.MIME = "image/svg+xml" }},
		{"largeBytes", func(p *TaskImageInput) { p.ByteSize = MaxImageInputBytes + 1 }},
		{"emptyBytes", func(p *TaskImageInput) { p.ByteSize = 0 }},
		{"fakeScanner", func(p *TaskImageInput) { p.PixelRisk = "CLEAN" }},
		{"missingHash", func(p *TaskImageInput) { p.SHA256 = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := p
			c.change(&v)
			if !errors.Is(ValidateTaskImageInput(v), content.ErrInvalid) {
				t.Fatal("accepted nonclosed upload")
			}
		})
	}
}
func TestTaskImageNativeUserPrefixSurvivesAppendButNeverGrafts(t *testing.T) {
	task, p := taskImageUnitFixture()
	if e := TaskImageMatches(task, p, true); e != nil {
		t.Fatal(e)
	}
	if _, e := TaskImageMessageDigest(task, 1); e == nil {
		t.Fatal("assistant target accepted")
	}
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: "当前后续问题"}, agentworkspace.Message{Role: "assistant", Text: "合成后续回答"})
	task.UpdatedAt = task.UpdatedAt.Add(time.Second)
	if e := TaskImageMatches(task, p, false); e != nil {
		t.Fatal("legitimate original prefix lost", e)
	}
	if e := TaskImageMatches(task, p, true); !errors.Is(e, content.ErrConflict) {
		t.Fatal("stale preview revived")
	}
	task.Conversation[0].Text = "相同索引上的另一条问题"
	if e := TaskImageMatches(task, p, false); !errors.Is(e, content.ErrConflict) {
		t.Fatal("index graft accepted")
	}
}
func TestTaskImageDigestIncludesCompleteNativePrefix(t *testing.T) {
	task, _ := taskImageUnitFixture()
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: "重复问题"})
	before, e := TaskImageMessageDigest(task, 2)
	if e != nil {
		t.Fatal(e)
	}
	task.Conversation[1].Text = "原回答被改变"
	after, _ := TaskImageMessageDigest(task, 2)
	if before == after {
		t.Fatal("earlier role/text not bound")
	}
	task.Conversation[1].Role = "user"
	role, _ := TaskImageMessageDigest(task, 2)
	if role == after {
		t.Fatal("earlier role not bound")
	}
}
func TestTaskImageDoesNotGrantOrganizationOrOnlineAccess(t *testing.T) {
	task, p := taskImageUnitFixture()
	for _, field := range []string{"principal", "online", "city", "context"} {
		t.Run(field, func(t *testing.T) {
			v := task
			switch field {
			case "principal":
				v.PrincipalType = "organization"
			case "online":
				v.ContextType = "ONLINE"
			case "city":
				v.CityID = ""
			case "context":
				v.ContextID = ""
			}
			if !errors.Is(TaskImageMatches(v, p, false), content.ErrNotFound) {
				t.Fatal("unsupported owner/context accepted")
			}
		})
	}
}
