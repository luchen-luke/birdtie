package agentworkspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

const (
	NowPreferenceIntent   = "MANAGE_NOW_PREFERENCE"
	NowPreferenceSchema   = "birdtie.now-explicit-food-preference.v1"
	NowPreferencePurpose  = "LOCAL_NOW_RECOMMENDATION"
	NowPreferenceNature   = "HUMAN_EXPLICIT_DECLARATION"
	NowPreferenceRemember = "REMEMBER"
	NowPreferenceCorrect  = "CORRECT"
	NowPreferenceForget   = "FORGET"
	NowPreferenceView     = "VIEW"
	NowPreferenceConfirm  = "CONFIRM_LOCAL_USE"
	NowPreferenceRejected = "UNSUPPORTED_EXPLICIT_PREFERENCE"
)

var ErrNowPreference = errors.New("invalid explicit Now preference")

type NowPreferencePort interface {
	ExecuteOwnNowPreference(context.Context, arp.Access) (Task, error)
}

// This is a deliberately closed food vocabulary, not a general learner or an
// inference about diet, health, religion, attendance, friends or photographs.
type NowPreferenceItem struct {
	Topic string `json:"topic"`
	Value string `json:"value"`
}
type NowPreferenceCommand struct {
	Action   string
	Items    []NowPreferenceItem
	LocalUse bool
}
type NowPreferenceSource struct {
	TaskID       string `json:"taskId"`
	MessageIndex int    `json:"messageIndex"`
	QueryDigest  string `json:"queryDigest"`
}
type NowPreferenceValue struct {
	Schema         string              `json:"schema"`
	Nature         string              `json:"nature"`
	Topic          string              `json:"topic"`
	Value          string              `json:"value"`
	UsePurpose     string              `json:"usePurpose"`
	ConsentVersion int                 `json:"consentVersion"`
	Declaration    NowPreferenceSource `json:"declaration"`
	Authorization  NowPreferenceSource `json:"authorization"`
}

func NowPreferenceKey(topic string) string {
	if topic == "seafood" || topic == "spicy" {
		return "now.preference.food." + topic
	}
	return ""
}
func NowPreferenceSummary(item NowPreferenceItem) string {
	switch item {
	case NowPreferenceItem{"seafood", "LIKE"}:
		return "我喜欢海鲜"
	case NowPreferenceItem{"seafood", "DISLIKE"}:
		return "我不喜欢海鲜"
	case NowPreferenceItem{"spicy", "LIKE"}:
		return "我喜欢吃辣"
	case NowPreferenceItem{"spicy", "DISLIKE"}:
		return "我不吃辣"
	}
	return ""
}
func nowPreferenceText(query string) (string, bool) {
	if !utf8.ValidString(query) || len(query) == 0 || len(query) > 240 || strings.TrimSpace(query) != query {
		return "", false
	}
	for _, r := range query {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return "", false
		}
	}
	return strings.Trim(strings.ToLower(query), " 。.!！?？"), true
}

// A matching command prefix is handled even when its contents are unsupported,
// so an untrusted quoted instruction cannot fall through to model egress.
func ParseNowExplicitPreference(query string) (NowPreferenceCommand, bool) {
	text, valid := nowPreferenceText(query)
	if !valid {
		return NowPreferenceCommand{}, false
	}
	if text == "确认保存并用于以后本地推荐" || text == "confirm saving and using for future local recommendations" {
		return NowPreferenceCommand{Action: NowPreferenceConfirm, LocalUse: true}, true
	}
	if text == "看看我记住的口味" || text == "查看我的口味偏好" || text == "show my remembered food preferences" || text == "what food preferences do you remember about me" {
		return NowPreferenceCommand{Action: NowPreferenceView}, true
	}
	for _, f := range []struct{ prefix, topic string }{
		{"忘记我的海鲜偏好", "seafood"}, {"忘记我喜欢海鲜的偏好", "seafood"}, {"忘记我的辣味偏好", "spicy"}, {"忘记我不吃辣的偏好", "spicy"},
		{"forget my seafood preference", "seafood"}, {"forget my spicy food preference", "spicy"},
	} {
		if text == f.prefix {
			return NowPreferenceCommand{Action: NowPreferenceForget, Items: []NowPreferenceItem{{Topic: f.topic}}}, true
		}
	}
	if text == "忘记我的口味偏好" || text == "forget my food preferences" {
		return NowPreferenceCommand{Action: NowPreferenceForget, Items: []NowPreferenceItem{{Topic: "seafood"}, {Topic: "spicy"}}}, true
	}
	if strings.HasPrefix(text, "忘记") || strings.HasPrefix(text, "forget ") {
		return NowPreferenceCommand{Action: NowPreferenceRejected}, true
	}
	var action, rest string
	for _, p := range []struct{ prefix, action string }{{"记住", NowPreferenceRemember}, {"remember ", NowPreferenceRemember}, {"纠正", NowPreferenceCorrect}, {"correct ", NowPreferenceCorrect}} {
		if strings.HasPrefix(text, p.prefix) {
			action, rest = p.action, strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(text, p.prefix), "，,:："))
			break
		}
	}
	if action == "" {
		if text == "我不再喜欢海鲜" || text == "i no longer like seafood" {
			action, rest = NowPreferenceCorrect, text
		} else {
			return NowPreferenceCommand{}, false
		}
	}
	rejected := NowPreferenceCommand{Action: NowPreferenceRejected}
	if rest == "" {
		return rejected, true
	}
	local := false
	for _, suffix := range []string{"并用于以后本地推荐", "and use it for future local recommendations", "and use for future local recommendations"} {
		if strings.HasSuffix(rest, suffix) {
			local = true
			rest = strings.TrimRight(strings.TrimSuffix(rest, suffix), " ，,;；")
			break
		}
	}
	// Literal clauses only: unrelated names, temporary requirements, quotes,
	// causes and third-party preferences do not become a declaration.
	rest = strings.TrimPrefix(rest, "that ")
	parts := strings.FieldsFunc(strings.ReplaceAll(rest, " and ", ","), func(r rune) bool { return strings.ContainsRune("，,；;", r) })
	items := []NowPreferenceItem{}
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		var item NowPreferenceItem
		switch part {
		case "我喜欢海鲜", "喜欢海鲜", "i like seafood", "i like 海鲜", "我喜欢seafood":
			item = NowPreferenceItem{"seafood", "LIKE"}
		case "我不喜欢海鲜", "不喜欢海鲜", "我不再喜欢海鲜", "i do not like seafood", "i don't like seafood", "i no longer like seafood":
			item = NowPreferenceItem{"seafood", "DISLIKE"}
		case "我喜欢吃辣", "喜欢吃辣", "我喜欢辣", "i like spicy food":
			item = NowPreferenceItem{"spicy", "LIKE"}
		case "我不吃辣", "不吃辣", "我不喜欢吃辣", "i do not eat spicy food", "i don't eat spicy food", "i do not like spicy food", "i don't like spicy food", "i do not eat 辣":
			item = NowPreferenceItem{"spicy", "DISLIKE"}
		default:
			return rejected, true
		}
		if seen[item.Topic] {
			return rejected, true
		}
		seen[item.Topic] = true
		items = append(items, item)
	}
	if len(items) == 0 || len(items) > 2 {
		return rejected, true
	}
	return NowPreferenceCommand{Action: action, Items: items, LocalUse: local}, true
}

func NowPreferenceConfirmationText(command NowPreferenceCommand) string {
	if command.Action != NowPreferenceRemember || command.LocalUse || len(command.Items) == 0 {
		return ""
	}
	return "要保存这些口味偏好，并用于以后的本人本地推荐吗？请回复“确认保存并用于以后本地推荐”。仅用于本人的 Birdtie 站内推荐，不发送给联网搜索或模型。当前仅支持海鲜和辣味偏好。"
}

// Confirmation is bound to the immediately preceding original user/assistant
// pair. A generic 'yes', another Task or a later unrelated turn grants nothing.
func ResolveNowPreferenceCommand(task Task) (NowPreferenceCommand, int, int, error) {
	i := len(task.Conversation) - 1
	if i < 0 || task.Conversation[i].Role != "user" {
		return NowPreferenceCommand{}, 0, 0, ErrNowPreference
	}
	command, ok := ParseNowExplicitPreference(task.Conversation[i].Text)
	if !ok {
		return NowPreferenceCommand{}, 0, 0, ErrNowPreference
	}
	if command.Action != NowPreferenceConfirm {
		return command, i, i, nil
	}
	if i < 2 || task.Conversation[i-1].Role != "assistant" || task.Conversation[i-2].Role != "user" {
		return NowPreferenceCommand{}, 0, 0, ErrNowPreference
	}
	previous, ok := ParseNowExplicitPreference(task.Conversation[i-2].Text)
	if !ok || previous.Action != NowPreferenceRemember || previous.LocalUse || task.Conversation[i-1].Text != NowPreferenceConfirmationText(previous) {
		return NowPreferenceCommand{}, 0, 0, ErrNowPreference
	}
	previous.LocalUse = true
	return previous, i - 2, i, nil
}
func NowPreferenceQueryDigest(query string) string {
	h := sha256.Sum256([]byte(query))
	return hex.EncodeToString(h[:])
}
func NowPreferenceSourceForTask(task Task, index int) (NowPreferenceSource, error) {
	if !actionUUID(task.ID) || index < 0 || index >= len(task.Conversation) || task.Conversation[index].Role != "user" {
		return NowPreferenceSource{}, ErrNowPreference
	}
	return NowPreferenceSource{TaskID: task.ID, MessageIndex: index, QueryDigest: NowPreferenceQueryDigest(task.Conversation[index].Text)}, nil
}

// Parsing checks the native Memory shape and closed declaration scope only.
// The caller must still resolve both retained source Tasks under current own
// ACL and revalidate the source/session/version at the response fence.
func ParseNowPreferenceMemory(record agentmemory.Record, now time.Time) (NowPreferenceValue, error) {
	if record.Status != agentmemory.StatusActive || record.ValidFrom.After(now) || !record.ValidUntil.After(now) {
		return NowPreferenceValue{}, ErrNowPreference
	}
	return ParseOwnStoredNowPreferenceMemory(record)
}

// Owner correction/deletion can inspect an expired declaration's shape. This
// does not make it fresh, active or eligible for a Context Builder consumer.
func ParseOwnStoredNowPreferenceMemory(record agentmemory.Record) (NowPreferenceValue, error) {
	var value NowPreferenceValue
	if agentmemory.ValidateRecord(record) != nil || record.MemoryType != agentmemory.TypePreference || record.SourceType != agentmemory.SourceExplicit || (record.Status != agentmemory.StatusActive && record.Status != agentmemory.StatusExpired) || record.Visibility != agentmemory.VisibilityPrivate {
		return value, ErrNowPreference
	}
	raw, e := agentmemory.NormalizeStructuredValue(record.StructuredValue)
	if e != nil {
		return value, ErrNowPreference
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil {
		return NowPreferenceValue{}, ErrNowPreference
	}
	if value.Schema != NowPreferenceSchema || value.Nature != NowPreferenceNature || value.UsePurpose != NowPreferencePurpose || value.ConsentVersion != 1 || record.MemoryKey != NowPreferenceKey(value.Topic) || NowPreferenceSummary(NowPreferenceItem{value.Topic, value.Value}) == "" || record.Summary != NowPreferenceSummary(NowPreferenceItem{value.Topic, value.Value}) {
		return NowPreferenceValue{}, ErrNowPreference
	}
	for _, source := range []NowPreferenceSource{value.Declaration, value.Authorization} {
		b, e := hex.DecodeString(source.QueryDigest)
		if !actionUUID(source.TaskID) || source.MessageIndex < 0 || source.MessageIndex > 10000 || e != nil || len(b) != 32 || strings.ToLower(source.QueryDigest) != source.QueryDigest {
			return NowPreferenceValue{}, ErrNowPreference
		}
	}
	return value, nil
}

func ValidateNowPreferenceSources(value NowPreferenceValue, declaration, authorization Task) error {
	if declaration.PrincipalType != "person" || authorization.PrincipalType != "person" || declaration.ContextType != "CITY" || authorization.ContextType != "CITY" || !actionUUID(declaration.PrincipalID) || declaration.PrincipalID != authorization.PrincipalID || declaration.ActingUserID != declaration.PrincipalID || authorization.ActingUserID != authorization.PrincipalID {
		return ErrNowPreference
	}
	for _, pair := range []struct {
		s NowPreferenceSource
		t Task
	}{{value.Declaration, declaration}, {value.Authorization, authorization}} {
		s, e := NowPreferenceSourceForTask(pair.t, pair.s.MessageIndex)
		if e != nil || s != pair.s {
			return ErrNowPreference
		}
	}
	decl, ok := ParseNowExplicitPreference(declaration.Conversation[value.Declaration.MessageIndex].Text)
	if !ok || (decl.Action != NowPreferenceRemember && decl.Action != NowPreferenceCorrect) {
		return ErrNowPreference
	}
	matched := false
	for _, item := range decl.Items {
		if item.Topic == value.Topic && item.Value == value.Value {
			matched = true
		}
	}
	if !matched {
		return ErrNowPreference
	}
	i := value.Authorization.MessageIndex
	grant, ok := ParseNowExplicitPreference(authorization.Conversation[i].Text)
	if !ok {
		return ErrNowPreference
	}
	if grant.Action == NowPreferenceConfirm {
		copyTask := authorization
		copyTask.Conversation = authorization.Conversation[:i+1]
		var e error
		grant, _, _, e = ResolveNowPreferenceCommand(copyTask)
		if e != nil {
			return e
		}
	}
	if !grant.LocalUse || (grant.Action != NowPreferenceRemember && grant.Action != NowPreferenceCorrect) {
		return ErrNowPreference
	}
	for _, item := range grant.Items {
		if item.Topic == value.Topic {
			return nil
		}
	}
	return ErrNowPreference
}
