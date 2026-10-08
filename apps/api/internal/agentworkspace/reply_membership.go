package agentworkspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

const (
	ReplyMembershipSchema = "agent-reply-membership-v1"
	MaxReplyRefs          = 30
	MaxMessageResults     = 30
)

var ErrReplyMembership = errors.New("invalid agent reply membership")

// ReplyTurnDigest identifies the original role/text prefix through one stored
// assistant message. Citations and membership are excluded so decorating a
// stored reply cannot rewrite its identity. A digest never grants access.
func ReplyTurnDigest(messages []Message, index int) (string, error) {
	if index < 0 || index >= len(messages) || messages[index].Role != "assistant" {
		return "", ErrReplyMembership
	}
	type turnMessage struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	prefix := make([]turnMessage, index+1)
	for i, message := range messages[:index+1] {
		if message.Role != "user" && message.Role != "assistant" {
			return "", ErrReplyMembership
		}
		prefix[i] = turnMessage{Role: message.Role, Text: message.Text}
	}
	raw, err := json.Marshal(prefix)
	if err != nil {
		return "", ErrReplyMembership
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func replyTaskValid(task Task) bool {
	return task.PrincipalType == "person" && task.ContextType == "CITY" &&
		actionUUID(task.ID) && actionUUID(task.PrincipalID) && actionUUID(task.ActingUserID) &&
		strings.EqualFold(task.PrincipalID, task.ActingUserID) &&
		(arp.Query{CityID: task.CityID, Kind: "place"}).Valid()
}

func replyKindValid(kind string) bool {
	return kind == "activity" || kind == "place" || kind == "organization"
}

// NewReplyMembership records bounded native references for one reply. Its
// caller must supply refs from the original authorized native projection;
// neither this record nor structural validation provides a read permission.
func NewReplyMembership(task Task, index int, kind string, refs []arp.Ref) (*ReplyMembership, error) {
	if !replyTaskValid(task) || !replyKindValid(kind) || len(refs) > MaxReplyRefs {
		return nil, ErrReplyMembership
	}
	digest, err := ReplyTurnDigest(task.Conversation, index)
	if err != nil {
		return nil, err
	}
	lastUser := ""
	for _, message := range task.Conversation[:index] {
		if message.Role == "user" {
			lastUser = message.Text
		}
	}
	if !answerText(strings.TrimSpace(lastUser), 4096, true) {
		return nil, ErrReplyMembership
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.Type != kind || !arp.ValidRef(ref) || seen[ref.Key()] {
			return nil, ErrReplyMembership
		}
		seen[ref.Key()] = true
	}
	return &ReplyMembership{
		Schema: ReplyMembershipSchema, TaskID: task.ID, CityID: task.CityID,
		Kind: kind, TurnDigest: digest, ResultSetID: "reply:" + task.ID + ":" + digest[:24],
		Refs: append([]arp.Ref{}, refs...),
	}, nil
}

// ValidateReplyMembership checks correlation with persisted task data only.
// The native source and its current ACL/expiry must still be revalidated.
func ValidateReplyMembership(task Task, index int) error {
	if index < 0 || index >= len(task.Conversation) {
		return ErrReplyMembership
	}
	membership := task.Conversation[index].ResultMembership
	if membership == nil || membership.Schema != ReplyMembershipSchema ||
		membership.TaskID != task.ID || membership.CityID != task.CityID {
		return ErrReplyMembership
	}
	expected, err := NewReplyMembership(task, index, membership.Kind, membership.Refs)
	if err != nil || membership.TurnDigest != expected.TurnDigest ||
		membership.ResultSetID != expected.ResultSetID {
		return ErrReplyMembership
	}
	return nil
}

// LatestReplyMemberships returns eligible indexes in conversation order. Old
// messages without valid membership remain text/citations only.
func LatestReplyMemberships(task Task) []int {
	indexes := make([]int, 0, MaxMessageResults)
	for index := len(task.Conversation) - 1; index >= 0 && len(indexes) < MaxMessageResults; index-- {
		if ValidateReplyMembership(task, index) == nil {
			indexes = append(indexes, index)
		}
	}
	for left, right := 0, len(indexes)-1; left < right; left, right = left+1, right-1 {
		indexes[left], indexes[right] = indexes[right], indexes[left]
	}
	return indexes
}

// NativeRuleReply describes the native result count without inferred facts.
func NativeRuleReply(kind string, count int) string {
	if !replyKindValid(kind) || count < 0 || count > MaxReplyRefs {
		return ""
	}
	label := map[string]string{"activity": "当前可见活动", "place": "公开地点", "organization": "公开组织"}[kind]
	if count == 0 {
		if kind == "activity" {
			return "当前没有符合条件的可见活动。"
		}
		return "当前没有符合条件的" + label + "。"
	}
	return fmt.Sprintf("找到 %d 个%s。", count, label)
}
