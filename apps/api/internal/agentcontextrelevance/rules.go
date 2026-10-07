package agentcontextrelevance

import (
	"encoding/json"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// lexicalTerms has a fixed bound and treats user instructions as plain text.
// Chinese bigrams cover unspaced queries; explicit sport/time aliases improve
// bilingual literal matches without attributing unrecorded preferences.
func lexicalTerms(query string) []string {
	q := strings.ToLower(query)
	for _, s := range []string{"帮我", "查找", "搜索", "请问", "请", "附近", "安排", "本轮", "明确", "资料", "整理", "按", "的", "找", "我", "帮", "find", "please", "help", "nearby", "looking", "for", "the", "my"} {
		if strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0 {
			q = strings.ReplaceAll(q, s, " ")
		}
	}
	seen := map[string]bool{}
	add := func(s string) {
		if utf8.RuneCountInString(s) >= 2 && len(s) <= 240 {
			seen[s] = true
		}
	}
	for _, word := range strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if word == "find" || word == "please" || word == "help" || word == "nearby" || word == "looking" || word == "for" || word == "the" || word == "my" || word == "me" {
			continue
		}
		add(word)
		r := []rune(word)
		for i := 0; i+1 < len(r); i++ {
			if unicode.Is(unicode.Han, r[i]) && unicode.Is(unicode.Han, r[i+1]) {
				add(string(r[i : i+2]))
			}
		}
	}
	intent := agentworkspace.ParseMVPIntent(query, nil)
	aliases := map[string][]string{"badminton": {"羽毛球", "badminton"}, "basketball": {"篮球", "basketball"}, "football": {"足球", "football"}, "sports": {"运动", "sports"}, "culture": {"文化", "culture"}, "weekend": {"周末", "weekend", "saturday", "sunday"}, "tonight": {"今晚", "tonight"}, "tomorrow": {"明天", "tomorrow"}, "today": {"今天", "today"}}
	for _, k := range []string{intent.Category, intent.TimePreference} {
		for _, s := range aliases[k] {
			add(s)
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
func textMatch(text string, terms []string) []string {
	tokens := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		tokens[w] = true
	}
	lower := strings.ToLower(text)
	out := []string{}
	for _, term := range terms {
		han := strings.IndexFunc(term, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0
		if (han && strings.Contains(lower, term)) || (!han && tokens[term]) {
			out = append(out, term)
		}
	}
	return out
}
func profileText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, " ")
	}
	return ""
}

// Only fields already selected by the exact native approval are visited.
// An explicit question about an empty selected field keeps its true UNKNOWN;
// an unselected or unrelated empty field is never invented or loaded.
func explicitFieldQuery(query, key string) bool {
	aliases := map[string][]string{
		"personalPreferences":    {"个人偏好", "personal preferences"},
		"socialPreferences":      {"社交偏好", "social preferences"},
		"availability":           {"可用时间", "空闲时间", "availability"},
		"preferredActivityTypes": {"活动类型", "活动偏好", "preferred activity types"},
		"travelPreferences":      {"出行偏好", "travel preferences"},
		"interactionPreferences": {"互动偏好", "交流方式", "interaction preferences"},
		"privateCityHistory":     {"城市记录", "城市历史", "private city history"},
		"languagePreferences":    {"语言偏好", "语言设置", "language preferences", "my language"},
		"agentNotes":             {"补充说明", "我的备注", "agent notes"},
	}
	query = strings.ToLower(query)
	if strings.Contains(query, strings.ToLower(key)) {
		return true
	}
	for _, label := range aliases[key] {
		if strings.Contains(query, label) {
			return true
		}
	}
	return false
}

func project(input acb.Bundle) (View, error) {
	if input.Mode != acb.MachineTaskContext || input.City == nil || input.Task == nil || input.Task.Query != input.CurrentQuery || input.ModelAccess != "UNAVAILABLE" || input.MemoryPromotionAllowed || len(input.Profile) > 3 || len(input.Memories) > 3 || len(input.Places) > 5 || len(input.Activities) > 5 || len(input.Relationships) > 3 || len(input.Policies) > 1 || len(input.Sources) > 20 || len(input.CurrentQuery) > 240 || !utf8.ValidString(input.CurrentQuery) {
		return View{}, acb.ErrDenied
	}
	b := cloneBundle(input)
	b.Profile = map[string]json.RawMessage{}
	b.Memories = nil
	b.Places = nil
	b.Activities = nil
	b.Relationships = nil
	b.Policies = nil
	b.Sources = nil
	b.FieldEvidenceSet = nil
	v := View{Context: b, Matches: []Match{}, Method: Method}
	terms := lexicalTerms(input.CurrentQuery)
	keep := map[string]bool{"CURRENT_TASK_REQUEST\x00" + input.TaskID: true, "PUBLIC_CITY\x00" + input.CityID: true}
	add := func(kind, id, field, text string, force bool, reason string) bool {
		matches := textMatch(text, terms)
		if !force && len(matches) == 0 {
			v.Excluded++
			return false
		}
		keep[kind+"\x00"+id] = true
		v.Matches = append(v.Matches, Match{Kind: kind, SourceID: id, Field: field, Terms: matches, Score: len(matches), Reason: reason})
		return true
	}
	for _, key := range acb.PrivateFieldKeys() {
		explicit := explicitFieldQuery(input.CurrentQuery, key)
		reason := "明确字段的文本匹配"
		if explicit {
			reason = "明确询问已批准字段；空值保持未知"
		}
		if raw, ok := input.Profile[key]; ok && add("PURPOSE_PRIVATE_PROFILE", input.Agent.AgentID, key, profileText(raw), explicit, reason) {
			v.Context.Profile[key] = append(json.RawMessage(nil), raw...)
		}
	}
	for _, m := range input.Memories {
		if add("PURPOSE_EXPLICIT_MEMORY", m.ID, "", m.Summary, false, "明确记录的文本匹配") {
			v.Context.Memories = append(v.Context.Memories, m)
		}
	}
	for _, p := range input.Places {
		if add("PUBLIC_PLACE", p.ID, "", p.Name+" "+p.Category, false, "公开地点的文本匹配") {
			v.Context.Places = append(v.Context.Places, p)
		}
	}
	for _, a := range input.Activities {
		if add("PUBLIC_ACTIVITY", a.ID, "", a.Title+" "+a.Category, false, "公开活动的文本匹配") {
			v.Context.Activities = append(v.Context.Activities, a)
		}
	}
	intent := agentworkspace.ParseMVPIntent(input.CurrentQuery, nil)
	for _, tie := range input.Relationships {
		if add("PURPOSE_RELATIONSHIP_TIE", tie.ID, "", "", intent.Operation == agentworkspace.PersonalRelationshipContext || strings.Contains(input.CurrentQuery, tie.ID), "仅明确本人好友状态请求") {
			v.Context.Relationships = append(v.Context.Relationships, tie)
		}
	}
	for _, p := range input.Policies {
		add("PURPOSE_POLICY_SETTINGS", input.Agent.AgentID+":"+string(p.Family), "", "", true, "当前任务的已声明操作边界")
		v.Context.Policies = append(v.Context.Policies, p)
	}
	section := func(before, after int) string {
		if before == 0 {
			return "NOT_REQUESTED"
		}
		if after == 0 {
			return "NOT_RELEVANT"
		}
		return "AVAILABLE"
	}
	v.Context.Sections = acb.Sections{Profile: section(len(input.Profile), len(v.Context.Profile)), Memories: section(len(input.Memories), len(v.Context.Memories)), Places: section(len(input.Places), len(v.Context.Places)), Activities: section(len(input.Activities), len(v.Context.Activities)), Relationships: section(len(input.Relationships), len(v.Context.Relationships)), Policies: section(len(input.Policies), len(v.Context.Policies))}
	for _, src := range input.Sources {
		if keep[src.Kind+"\x00"+src.ID] {
			src.RowToken = ""
			v.Context.Sources = append(v.Context.Sources, src)
			delete(keep, src.Kind+"\x00"+src.ID)
		}
	}
	if len(keep) != 0 {
		return View{}, acb.ErrDenied
	}
	sort.SliceStable(v.Matches, func(i, j int) bool {
		a, b := v.Matches[i], v.Matches[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Kind+"\x00"+a.SourceID+"\x00"+a.Field < b.Kind+"\x00"+b.SourceID+"\x00"+b.Field
	})
	// Each family order follows relevance score then stable ID, independent of
	// map iteration; the retained complete seal remains in Result.original.
	scores := map[string]int{}
	for _, m := range v.Matches {
		scores[m.Kind+"\x00"+m.SourceID] = m.Score
	}
	less := func(kind, a, b string) bool {
		sa, sb := scores[kind+"\x00"+a], scores[kind+"\x00"+b]
		if sa != sb {
			return sa > sb
		}
		return a < b
	}
	sort.Slice(v.Context.Memories, func(i, j int) bool {
		return less("PURPOSE_EXPLICIT_MEMORY", v.Context.Memories[i].ID, v.Context.Memories[j].ID)
	})
	sort.Slice(v.Context.Places, func(i, j int) bool { return less("PUBLIC_PLACE", v.Context.Places[i].ID, v.Context.Places[j].ID) })
	sort.Slice(v.Context.Activities, func(i, j int) bool {
		return less("PUBLIC_ACTIVITY", v.Context.Activities[i].ID, v.Context.Activities[j].ID)
	})
	if input.FieldEvidenceSet != nil {
		if acb.FieldEvidenceMatchesBundle(input) != nil {
			return View{}, acb.ErrDenied
		}
		var err error
		v.Context.FieldEvidenceSet, err = acb.FilterFieldEvidence(input.FieldEvidenceSet, acb.BundleEvidenceSelectors(v.Context), "FILTERED_CONTEXT")
		if err != nil {
			return View{}, acb.ErrDenied
		}
	}
	return v, nil
}
